//go:build unix

package pagebtree

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"slices"

	"golang.org/x/sys/unix"
)

var ErrDatabaseLocked = errors.New("mmap tree database is already locked")

const (
	metaMagic        = "COWBTREE"
	metaVersion      = uint64(1)
	metaMagicOffset  = 0
	metaVersionOff   = 8
	metaRootOff      = 16
	metaNextPageOff  = 24
	metaLengthOff    = 32
	metaRevisionOff  = 40
	metaDegreeOff    = 48
	metaMaxPagesOff  = 56
	metaChecksumOff  = 64
	metaFreeCountOff = 72
	metaFreeListOff  = 80
	metaPageCount    = 2
	minMmapPageCount = 2
)

const firstTreePageID = PageID(metaPageCount)
const maxMetaFreePages = (PageSize - metaFreeListOff) / 8

type MmapOptions struct {
	Degree        int
	MaxPages      int
	AccessPattern MmapAccessPattern
}

type mmapArena struct {
	file         *os.File
	data         []byte
	maxPages     int
	locked       bool
	readOnly     bool
	syncObserver func(string)
}

type MmapAccessPattern int

const (
	MmapAccessDefault MmapAccessPattern = iota
	MmapAccessRandom
	MmapAccessSequential
	MmapAccessWillNeed
)

func OpenMmap(path string, options MmapOptions) (*Tree, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file, true); err != nil {
		file.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		unlockFile(file)
		file.Close()
		return nil, err
	}

	maxPages := options.MaxPages
	if info.Size() > 0 {
		if info.Size()%PageSize != 0 {
			unlockFile(file)
			file.Close()
			return nil, fmt.Errorf("mmap tree file size %d is not page aligned", info.Size())
		}
		existingPages := int(info.Size()/PageSize) - metaPageCount
		if maxPages < existingPages {
			maxPages = existingPages
		}
	}
	if maxPages < minMmapPageCount {
		maxPages = 1024
	}

	size := int64((maxPages + metaPageCount) * PageSize)
	if err := file.Truncate(size); err != nil {
		unlockFile(file)
		file.Close()
		return nil, err
	}

	data, err := unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unlockFile(file)
		file.Close()
		return nil, err
	}

	arena := &mmapArena{
		file:     file,
		data:     data,
		maxPages: maxPages,
		locked:   true,
	}
	if err := arena.advise(options.AccessPattern); err != nil {
		arena.close()
		return nil, err
	}

	tree := &Tree{
		pages:    map[PageID]*page{},
		nextPage: firstTreePageID,
		degree:   normalizeDegree(options.Degree),
		arena:    arena,
	}

	if arena.initialized() {
		if err := tree.loadMeta(); err != nil {
			arena.close()
			return nil, err
		}
		return tree, nil
	}

	if err := tree.Sync(); err != nil {
		arena.close()
		return nil, err
	}
	return tree, nil
}

func OpenMmapReadOnly(path string) (*Tree, error) {
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file, false); err != nil {
		file.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		unlockFile(file)
		file.Close()
		return nil, err
	}
	if info.Size() == 0 || info.Size()%PageSize != 0 {
		unlockFile(file)
		file.Close()
		return nil, fmt.Errorf("mmap tree file size %d is not page aligned", info.Size())
	}

	size := int(info.Size())
	data, err := unix.Mmap(int(file.Fd()), 0, size, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		unlockFile(file)
		file.Close()
		return nil, err
	}

	arena := &mmapArena{
		file:     file,
		data:     data,
		maxPages: int(info.Size()/PageSize) - metaPageCount,
		locked:   true,
		readOnly: true,
	}
	tree := &Tree{
		pages:    map[PageID]*page{},
		nextPage: firstTreePageID,
		arena:    arena,
		readOnly: true,
	}
	if err := tree.loadMeta(); err != nil {
		arena.close()
		return nil, err
	}
	return tree, nil
}

func (a *mmapArena) initialized() bool {
	for index := 0; index < metaPageCount; index++ {
		if _, ok := readMetaPage(a.data[index*PageSize : (index+1)*PageSize]); ok {
			return true
		}
	}
	return false
}

func (a *mmapArena) pageBytes(id PageID) ([]byte, error) {
	if id < firstTreePageID || int(id) >= a.maxPages+metaPageCount {
		return nil, fmt.Errorf("page id %d outside mmap capacity %d", id, a.maxPages)
	}
	start := int(id) * PageSize
	return a.data[start : start+PageSize], nil
}

func (a *mmapArena) syncDataPages(nextPage PageID) error {
	if a == nil || a.readOnly || nextPage <= firstTreePageID {
		return nil
	}
	end := int(nextPage) * PageSize
	if end > len(a.data) {
		return fmt.Errorf("next page %d exceeds mmap size %d", nextPage, len(a.data))
	}
	if a.syncObserver != nil {
		a.syncObserver("data")
	}
	if err := a.msyncRange(int(firstTreePageID)*PageSize, end); err != nil {
		return err
	}
	return a.file.Sync()
}

func (a *mmapArena) syncMetaPage(index int) error {
	if a == nil || a.readOnly {
		return nil
	}
	if index < 0 || index >= metaPageCount {
		return fmt.Errorf("metadata page index %d outside range", index)
	}
	start := index * PageSize
	if a.syncObserver != nil {
		a.syncObserver("meta")
	}
	if err := a.msyncRange(start, start+PageSize); err != nil {
		return err
	}
	return a.file.Sync()
}

func (a *mmapArena) msyncRange(start, end int) error {
	if start < 0 || end < start || end > len(a.data) {
		return fmt.Errorf("msync range [%d,%d) outside mmap size %d", start, end, len(a.data))
	}
	if start == end {
		return nil
	}
	osPageSize := unix.Getpagesize()
	alignedStart := start - start%osPageSize
	alignedEnd := end
	if remainder := alignedEnd % osPageSize; remainder != 0 {
		alignedEnd += osPageSize - remainder
	}
	if alignedEnd > len(a.data) {
		alignedEnd = len(a.data)
	}
	return unix.Msync(a.data[alignedStart:alignedEnd], unix.MS_SYNC)
}

func (a *mmapArena) advise(pattern MmapAccessPattern) error {
	if a == nil || len(a.data) == 0 {
		return nil
	}
	advice, err := mmapAdvice(pattern)
	if err != nil {
		return err
	}
	return unix.Madvise(a.data, advice)
}

func mmapAdvice(pattern MmapAccessPattern) (int, error) {
	switch pattern {
	case MmapAccessDefault:
		return unix.MADV_NORMAL, nil
	case MmapAccessRandom:
		return unix.MADV_RANDOM, nil
	case MmapAccessSequential:
		return unix.MADV_SEQUENTIAL, nil
	case MmapAccessWillNeed:
		return unix.MADV_WILLNEED, nil
	default:
		return 0, fmt.Errorf("unknown mmap access pattern %d", pattern)
	}
}

func (a *mmapArena) close() error {
	if a == nil {
		return nil
	}

	var errs []error
	if err := unix.Munmap(a.data); err != nil {
		errs = append(errs, err)
	}
	if a.locked {
		if err := unlockFile(a.file); err != nil {
			errs = append(errs, err)
		}
		a.locked = false
	}
	if err := a.file.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func lockFile(file *os.File, exclusive bool) error {
	mode := unix.LOCK_SH | unix.LOCK_NB
	if exclusive {
		mode = unix.LOCK_EX | unix.LOCK_NB
	}
	err := unix.Flock(int(file.Fd()), mode)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return ErrDatabaseLocked
	}
	return err
}

func unlockFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

type metaRecord struct {
	root     PageID
	nextPage PageID
	length   int
	revision uint64
	degree   int
	maxPages int
	free     []PageID
}

func (t *Tree) loadMeta() error {
	var records []metaRecord
	maxNextPage := firstTreePageID
	for index := 0; index < metaPageCount; index++ {
		record, ok := readMetaPage(t.arena.data[index*PageSize : (index+1)*PageSize])
		if !ok {
			continue
		}
		records = append(records, record)
		if record.nextPage > maxNextPage {
			maxNextPage = record.nextPage
		}
	}
	if len(records) == 0 {
		return fmt.Errorf("no valid mmap tree metadata page")
	}
	slices.SortFunc(records, func(left, right metaRecord) int {
		return compareUint64Desc(left.revision, right.revision)
	})
	if maxNextPage < firstTreePageID {
		maxNextPage = firstTreePageID
	}

	t.pages = map[PageID]*page{}
	for id := firstTreePageID; id < maxNextPage; id++ {
		data, err := t.arena.pageBytes(id)
		if err != nil {
			return err
		}
		t.pages[id] = &page{id: id, data: data}
	}

	var lastErr error
	for _, record := range records {
		t.applyMetaRecord(record)
		if err := t.validateReachablePages(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no usable mmap tree metadata page")
}

func (t *Tree) applyMetaRecord(record metaRecord) {
	t.root = record.root
	t.nextPage = record.nextPage
	t.length = record.length
	t.revision = record.revision
	t.degree = normalizeDegree(record.degree)
	t.free = append([]PageID(nil), record.free...)
	if t.nextPage < firstTreePageID {
		t.nextPage = firstTreePageID
	}
}

func compareUint64Desc(left, right uint64) int {
	switch {
	case left > right:
		return -1
	case left < right:
		return 1
	default:
		return 0
	}
}

func (t *Tree) persistMeta() {
	if t.arena == nil || t.arena.readOnly {
		return
	}

	index := int(t.revision % metaPageCount)
	writeMetaPage(t.arena.data[index*PageSize:(index+1)*PageSize], metaRecord{
		root:     t.root,
		nextPage: t.nextPage,
		length:   t.length,
		revision: t.revision,
		degree:   t.degree,
		maxPages: t.arena.maxPages,
		free:     t.free,
	})
}

func (t *Tree) syncMmap() error {
	if t.arena == nil || t.arena.readOnly {
		return nil
	}
	if err := t.arena.syncDataPages(t.nextPage); err != nil {
		return err
	}
	t.persistMeta()
	return t.arena.syncMetaPage(int(t.revision % metaPageCount))
}

func (t *Tree) Advise(pattern MmapAccessPattern) error {
	if t.arena == nil {
		return nil
	}
	return t.arena.advise(pattern)
}

func readMetaPage(data []byte) (metaRecord, bool) {
	if len(data) < PageSize {
		return metaRecord{}, false
	}
	if string(data[metaMagicOffset:metaMagicOffset+len(metaMagic)]) != metaMagic {
		return metaRecord{}, false
	}
	if binary.LittleEndian.Uint64(data[metaVersionOff:]) != metaVersion {
		return metaRecord{}, false
	}
	want := binary.LittleEndian.Uint32(data[metaChecksumOff:])
	got := metaChecksum(data)
	if got != want {
		return metaRecord{}, false
	}
	freeCount := int(binary.LittleEndian.Uint64(data[metaFreeCountOff:]))
	if freeCount > maxMetaFreePages {
		return metaRecord{}, false
	}
	free := make([]PageID, 0, freeCount)
	for i := 0; i < freeCount; i++ {
		offset := metaFreeListOff + i*8
		free = append(free, PageID(binary.LittleEndian.Uint64(data[offset:])))
	}
	return metaRecord{
		root:     PageID(binary.LittleEndian.Uint64(data[metaRootOff:])),
		nextPage: PageID(binary.LittleEndian.Uint64(data[metaNextPageOff:])),
		length:   int(binary.LittleEndian.Uint64(data[metaLengthOff:])),
		revision: binary.LittleEndian.Uint64(data[metaRevisionOff:]),
		degree:   int(binary.LittleEndian.Uint64(data[metaDegreeOff:])),
		maxPages: int(binary.LittleEndian.Uint64(data[metaMaxPagesOff:])),
		free:     free,
	}, true
}

func writeMetaPage(data []byte, record metaRecord) {
	if len(record.free) > maxMetaFreePages {
		panic("freelist too large for educational meta-page encoding")
	}

	clear(data)
	copy(data[metaMagicOffset:], metaMagic)
	binary.LittleEndian.PutUint64(data[metaVersionOff:], metaVersion)
	binary.LittleEndian.PutUint64(data[metaRootOff:], uint64(record.root))
	binary.LittleEndian.PutUint64(data[metaNextPageOff:], uint64(record.nextPage))
	binary.LittleEndian.PutUint64(data[metaLengthOff:], uint64(record.length))
	binary.LittleEndian.PutUint64(data[metaRevisionOff:], record.revision)
	binary.LittleEndian.PutUint64(data[metaDegreeOff:], uint64(record.degree))
	binary.LittleEndian.PutUint64(data[metaMaxPagesOff:], uint64(record.maxPages))
	binary.LittleEndian.PutUint64(data[metaFreeCountOff:], uint64(len(record.free)))
	for i, id := range record.free {
		offset := metaFreeListOff + i*8
		binary.LittleEndian.PutUint64(data[offset:], uint64(id))
	}
	binary.LittleEndian.PutUint32(data[metaChecksumOff:], metaChecksum(data))
}

func metaChecksum(data []byte) uint32 {
	checksum := crc32.NewIEEE()
	_, _ = checksum.Write(data[:metaChecksumOff])
	_, _ = checksum.Write(data[metaChecksumOff+4 : PageSize])
	return checksum.Sum32()
}

func (t *Tree) validateReachablePages() error {
	seen := map[PageID]bool{}
	return t.validatePage(t.root, seen)
}

func (t *Tree) validatePage(id PageID, seen map[PageID]bool) error {
	if id == 0 || seen[id] {
		return nil
	}
	seen[id] = true

	p := t.pages[id]
	if p == nil {
		return fmt.Errorf("reachable page %d is missing", id)
	}
	if !p.validChecksum() {
		return fmt.Errorf("%w: page %d", ErrPageChecksum, id)
	}
	if p.isLeaf() {
		for _, entry := range p.leafEntries() {
			if err := t.validateOverflowValue(entry.value, entry.slotFlags, seen); err != nil {
				return err
			}
		}
		return nil
	}
	if !p.isBranch() {
		return fmt.Errorf("page %d has invalid flags %x", id, p.flags())
	}
	for _, child := range p.childIDs() {
		if err := t.validatePage(child, seen); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tree) validateOverflowValue(raw []byte, flags uint16, seen map[PageID]bool) error {
	ref, ok := decodeOverflowRef(raw, flags)
	if !ok {
		return nil
	}
	var length int
	for id := ref.first; id != 0; {
		if seen[id] {
			return fmt.Errorf("overflow chain loops through page %d", id)
		}
		seen[id] = true
		p := t.pages[id]
		if p == nil {
			return fmt.Errorf("reachable overflow page %d is missing", id)
		}
		if !p.validChecksum() {
			return fmt.Errorf("%w: page %d", ErrPageChecksum, id)
		}
		if !p.isOverflow() {
			return fmt.Errorf("overflow page %d has invalid flags %x", id, p.flags())
		}
		if p.overflowPayloadLen() > overflowPayloadSize {
			return fmt.Errorf("overflow page %d payload length %d exceeds capacity", id, p.overflowPayloadLen())
		}
		length += p.overflowPayloadLen()
		id = p.overflowNext()
	}
	if length < ref.length {
		return fmt.Errorf("overflow chain length %d is shorter than referenced length %d", length, ref.length)
	}
	return nil
}
