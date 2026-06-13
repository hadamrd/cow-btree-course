//go:build unix

package pagebtree

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"slices"
	"unsafe"

	"golang.org/x/sys/unix"
)

var (
	ErrDatabaseLocked = errors.New("mmap tree database is already locked")
	ErrMetaInvariant  = errors.New("metadata invariant invalid")
)

const (
	metaMagic        = "COWBTREE"
	metaVersion      = uint64(1)
	metaMagicOffset  = 0
	metaVersionOff   = 8
	metaPageSizeOff  = 16
	metaRootOff      = 24
	metaNextPageOff  = 32
	metaLengthOff    = 40
	metaRevisionOff  = 48
	metaDegreeOff    = 56
	metaMaxPagesOff  = 64
	metaChecksumOff  = 72
	metaFreeCountOff = 80
	metaFreeListOff  = 88
	metaPageCount    = 2
	minMmapPageCount = 2
)

const firstTreePageID = PageID(metaPageCount)
const maxMetaFreePages = (PageSize - metaFreeListOff) / 8

type MmapOptions struct {
	Degree                  int
	MaxPages                int
	AccessPattern           MmapAccessPattern
	PageCacheCapacity       int
	RangePrefetchLeafWindow int
}

type mmapArena struct {
	file             *os.File
	data             []byte
	maxPages         int
	locked           bool
	readOnly         bool
	accessPattern    MmapAccessPattern
	dirtyPages       map[PageID]bool
	syncObserver     func(string)
	dataSyncObserver func(start, end PageID)
	adviceObserver   func(pattern MmapAccessPattern, start, end PageID)
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

	existingSize := info.Size()
	maxPages := options.MaxPages
	if existingSize > 0 {
		if existingSize%PageSize != 0 {
			unlockFile(file)
			file.Close()
			return nil, fmt.Errorf("mmap tree file size %d is not page aligned", existingSize)
		}
		existingPages := int(existingSize/PageSize) - metaPageCount
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
		file:       file,
		data:       data,
		maxPages:   maxPages,
		locked:     true,
		dirtyPages: map[PageID]bool{},
	}
	if err := arena.advise(options.AccessPattern); err != nil {
		arena.close()
		return nil, err
	}

	tree := &Tree{
		pages:                   map[PageID]*page{},
		nextPage:                firstTreePageID,
		degree:                  normalizeDegree(options.Degree),
		arena:                   arena,
		pageCache:               newPageCache(options.PageCacheCapacity),
		rangePrefetchLeafWindow: normalizeRangePrefetchLeafWindow(options.RangePrefetchLeafWindow),
	}

	if existingSize > 0 {
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
		pages:                   map[PageID]*page{},
		nextPage:                firstTreePageID,
		arena:                   arena,
		readOnly:                true,
		pageCache:               newPageCache(DefaultPageCacheCapacity),
		rangePrefetchLeafWindow: DefaultRangePrefetchLeafWindow,
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

func (t *Tree) growMmapForPage(id PageID) error {
	if t.arena == nil || int(id) < t.arena.maxPages+metaPageCount {
		return nil
	}
	if t.arena.readOnly {
		return fmt.Errorf("cannot grow read-only mmap tree")
	}

	neededPages := int(id) - metaPageCount + 1
	newMaxPages := t.arena.maxPages
	if newMaxPages < minMmapPageCount {
		newMaxPages = minMmapPageCount
	}
	for newMaxPages < neededPages {
		newMaxPages *= 2
	}
	return t.remapMmap(newMaxPages)
}

func (t *Tree) remapMmap(newMaxPages int) error {
	if newMaxPages <= t.arena.maxPages {
		return nil
	}
	if err := t.arena.syncDataPages(t.nextPage); err != nil {
		return err
	}

	newSize := int64((newMaxPages + metaPageCount) * PageSize)
	if err := t.arena.file.Truncate(newSize); err != nil {
		return err
	}
	if err := unix.Munmap(t.arena.data); err != nil {
		return err
	}

	data, err := unix.Mmap(int(t.arena.file.Fd()), 0, int(newSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return err
	}
	t.arena.data = data
	t.arena.maxPages = newMaxPages
	if err := t.arena.advise(t.arena.accessPattern); err != nil {
		return err
	}
	return t.rebindMmapPages()
}

func (t *Tree) rebindMmapPages() error {
	for id, p := range t.pages {
		data, err := t.arena.pageBytes(id)
		if err != nil {
			return err
		}
		p.data = data
	}
	return nil
}

func (t *Tree) compactMmapTail() error {
	if t.arena == nil || t.arena.readOnly {
		return nil
	}
	trimmedNextPage := t.trailingFreeNextPage()
	newMaxPages := int(trimmedNextPage - firstTreePageID)
	if newMaxPages < minMmapPageCount {
		newMaxPages = minMmapPageCount
	}
	oldMaxPages := t.arena.maxPages
	if trimmedNextPage == t.nextPage && newMaxPages >= oldMaxPages {
		return nil
	}

	t.removeFreePagesAtOrAbove(trimmedNextPage)
	for id := range t.pages {
		if id >= trimmedNextPage {
			delete(t.pages, id)
		}
	}
	for id := range t.arena.dirtyPages {
		if id >= trimmedNextPage {
			delete(t.arena.dirtyPages, id)
		}
	}
	t.nextPage = trimmedNextPage

	t.arena.maxPages = newMaxPages

	if err := t.arena.syncDataPages(t.nextPage); err != nil {
		t.arena.maxPages = oldMaxPages
		return err
	}
	t.persistMeta()
	if err := t.arena.syncMetaPage(int(t.revision % metaPageCount)); err != nil {
		t.arena.maxPages = oldMaxPages
		return err
	}
	if newMaxPages >= oldMaxPages {
		return nil
	}
	return t.shrinkMmap(newMaxPages)
}

func (t *Tree) trailingFreeNextPage() PageID {
	free := map[PageID]bool{}
	for _, id := range t.free {
		free[id] = true
	}

	next := t.nextPage
	for next > firstTreePageID && free[next-1] {
		next--
	}
	return next
}

func (t *Tree) removeFreePagesAtOrAbove(limit PageID) {
	kept := t.free[:0]
	for _, id := range t.free {
		if id < limit {
			kept = append(kept, id)
		}
	}
	t.free = kept
}

func (t *Tree) shrinkMmap(newMaxPages int) error {
	newSize := int64((newMaxPages + metaPageCount) * PageSize)
	if err := t.arena.file.Truncate(newSize); err != nil {
		return err
	}
	if err := unix.Munmap(t.arena.data); err != nil {
		return err
	}

	data, err := unix.Mmap(int(t.arena.file.Fd()), 0, int(newSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return err
	}
	t.arena.data = data
	t.arena.maxPages = newMaxPages
	if err := t.arena.advise(t.arena.accessPattern); err != nil {
		return err
	}
	return t.rebindMmapPages()
}

func (a *mmapArena) syncDataPages(nextPage PageID) error {
	if a == nil || a.readOnly || nextPage <= firstTreePageID || len(a.dirtyPages) == 0 {
		return nil
	}
	ids := make([]PageID, 0, len(a.dirtyPages))
	for id := range a.dirtyPages {
		if id >= firstTreePageID && id < nextPage {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		clear(a.dirtyPages)
		return nil
	}
	slices.Sort(ids)

	if a.syncObserver != nil {
		a.syncObserver("data")
	}

	for startIndex := 0; startIndex < len(ids); {
		start := ids[startIndex]
		end := start + 1
		nextIndex := startIndex + 1
		for nextIndex < len(ids) && ids[nextIndex] == end {
			end++
			nextIndex++
		}
		if err := a.syncDataPageRange(start, end); err != nil {
			return err
		}
		startIndex = nextIndex
	}
	if err := a.file.Sync(); err != nil {
		return err
	}
	clear(a.dirtyPages)
	return nil
}

func (a *mmapArena) syncDataPageRange(startPage, endPage PageID) error {
	if startPage < firstTreePageID || endPage < startPage {
		return fmt.Errorf("invalid data page sync range [%d,%d)", startPage, endPage)
	}
	endByte := int(endPage) * PageSize
	if endByte > len(a.data) {
		return fmt.Errorf("data page sync range [%d,%d) exceeds mmap size %d", startPage, endPage, len(a.data))
	}
	if a.dataSyncObserver != nil {
		a.dataSyncObserver(startPage, endPage)
	}
	return a.msyncRange(int(startPage)*PageSize, endByte)
}

func (a *mmapArena) markDirtyPage(id PageID) {
	if a == nil || a.readOnly {
		return
	}
	if a.dirtyPages == nil {
		a.dirtyPages = map[PageID]bool{}
	}
	a.dirtyPages[id] = true
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
	a.accessPattern = pattern
	return unix.Madvise(a.data, advice)
}

func (a *mmapArena) advisePageRange(startPage, endPage PageID, pattern MmapAccessPattern) error {
	if a == nil || len(a.data) == 0 {
		return nil
	}
	if startPage < firstTreePageID || endPage < startPage {
		return fmt.Errorf("invalid mmap advice page range [%d,%d)", startPage, endPage)
	}
	endByte := int(endPage) * PageSize
	if endByte > len(a.data) {
		return fmt.Errorf("mmap advice page range [%d,%d) exceeds mmap size %d", startPage, endPage, len(a.data))
	}
	advice, err := mmapAdvice(pattern)
	if err != nil {
		return err
	}
	if a.adviceObserver != nil {
		a.adviceObserver(pattern, startPage, endPage)
	}
	return a.madviseRange(int(startPage)*PageSize, endByte, advice)
}

func (a *mmapArena) madviseRange(start, end, advice int) error {
	if start < 0 || end < start || end > len(a.data) {
		return fmt.Errorf("madvise range [%d,%d) outside mmap size %d", start, end, len(a.data))
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
	return unix.Madvise(a.data[alignedStart:alignedEnd], advice)
}

func (a *mmapArena) cacheStats() (MmapCacheStats, error) {
	if a == nil || len(a.data) == 0 {
		return MmapCacheStats{}, nil
	}
	osPageSize := unix.Getpagesize()
	osPages := divideRoundUp(len(a.data), osPageSize)
	vec := make([]byte, osPages)
	_, _, errno := unix.Syscall(
		unix.SYS_MINCORE,
		uintptr(unsafe.Pointer(&a.data[0])),
		uintptr(len(a.data)),
		uintptr(unsafe.Pointer(&vec[0])),
	)
	if errno != 0 {
		return MmapCacheStats{}, errno
	}

	resident := 0
	for _, value := range vec {
		if value&1 == 1 {
			resident++
		}
	}
	return MmapCacheStats{
		MappedBytes:         len(a.data),
		MappedDatabasePages: divideRoundUp(len(a.data), PageSize),
		OSPageSize:          osPageSize,
		OSPages:             osPages,
		ResidentOSPages:     resident,
	}, nil
}

func divideRoundUp(value, by int) int {
	if value == 0 {
		return 0
	}
	return (value + by - 1) / by
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
	var lastMetaErr error
	maxNextPage := firstTreePageID
	for index := 0; index < metaPageCount; index++ {
		record, ok, err := readMetaPageChecked(t.arena.data[index*PageSize : (index+1)*PageSize])
		if err != nil {
			lastMetaErr = err
			continue
		}
		if !ok {
			continue
		}
		if err := t.validateMetaBounds(record); err != nil {
			lastMetaErr = err
			continue
		}
		records = append(records, record)
		if record.nextPage > maxNextPage {
			maxNextPage = record.nextPage
		}
	}
	if len(records) == 0 {
		if lastMetaErr != nil {
			return lastMetaErr
		}
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
		reachable, err := t.validateReachablePages()
		if err != nil {
			lastErr = err
			continue
		}
		if err := t.validateFreelist(record.free, reachable); err != nil {
			lastErr = err
			continue
		}
		if err := t.validateLeafLinks(); err != nil {
			lastErr = err
			continue
		}
		if err := t.validateMetaInvariants(record); err != nil {
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

// MmapCacheStats reports kernel page-cache residency for an mmap-backed tree.
func (t *Tree) MmapCacheStats() (MmapCacheStats, error) {
	if t == nil || t.arena == nil {
		return MmapCacheStats{}, nil
	}
	return t.arena.cacheStats()
}

func readMetaPage(data []byte) (metaRecord, bool) {
	record, ok, err := readMetaPageChecked(data)
	if err != nil {
		return metaRecord{}, false
	}
	return record, ok
}

func readMetaPageChecked(data []byte) (metaRecord, bool, error) {
	if len(data) < PageSize {
		return metaRecord{}, false, fmt.Errorf("%w: metadata page length %d below page size %d", ErrMetaInvariant, len(data), PageSize)
	}
	if isZeroPage(data[:PageSize]) {
		return metaRecord{}, false, nil
	}
	if string(data[metaMagicOffset:metaMagicOffset+len(metaMagic)]) != metaMagic {
		return metaRecord{}, false, fmt.Errorf("%w: metadata magic mismatch", ErrMetaInvariant)
	}
	if binary.LittleEndian.Uint64(data[metaVersionOff:]) != metaVersion {
		return metaRecord{}, false, fmt.Errorf("%w: metadata version %d unsupported", ErrMetaInvariant, binary.LittleEndian.Uint64(data[metaVersionOff:]))
	}
	if binary.LittleEndian.Uint64(data[metaPageSizeOff:]) != PageSize {
		return metaRecord{}, false, fmt.Errorf("%w: metadata page size %d does not match %d", ErrMetaInvariant, binary.LittleEndian.Uint64(data[metaPageSizeOff:]), PageSize)
	}
	want := binary.LittleEndian.Uint32(data[metaChecksumOff:])
	got := metaChecksum(data)
	if got != want {
		return metaRecord{}, false, fmt.Errorf("%w: metadata checksum mismatch", ErrMetaInvariant)
	}
	freeCount := int(binary.LittleEndian.Uint64(data[metaFreeCountOff:]))
	if freeCount > maxMetaFreePages {
		return metaRecord{}, false, fmt.Errorf("%w: metadata free count %d exceeds %d", ErrMetaInvariant, freeCount, maxMetaFreePages)
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
	}, true, nil
}

func isZeroPage(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

func writeMetaPage(data []byte, record metaRecord) {
	if len(record.free) > maxMetaFreePages {
		panic("freelist too large for educational meta-page encoding")
	}

	clear(data)
	copy(data[metaMagicOffset:], metaMagic)
	binary.LittleEndian.PutUint64(data[metaVersionOff:], metaVersion)
	binary.LittleEndian.PutUint64(data[metaPageSizeOff:], PageSize)
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

func (t *Tree) validateReachablePages() (map[PageID]bool, error) {
	seen := map[PageID]bool{}
	if err := t.validatePage(t.root, seen); err != nil {
		return nil, err
	}
	return seen, nil
}

func (t *Tree) validateFreelist(free []PageID, reachable map[PageID]bool) error {
	seenFree := map[PageID]bool{}
	for _, id := range free {
		if id < firstTreePageID || id >= t.nextPage {
			return fmt.Errorf("%w: page %d outside reusable range [%d,%d)", ErrFreelist, id, firstTreePageID, t.nextPage)
		}
		if reachable[id] {
			return fmt.Errorf("%w: page %d is still reachable", ErrFreelist, id)
		}
		if seenFree[id] {
			return fmt.Errorf("%w: page %d appears more than once", ErrFreelist, id)
		}
		seenFree[id] = true
	}
	return nil
}

func (t *Tree) validateMetaInvariants(record metaRecord) error {
	if err := t.validateMetaBounds(record); err != nil {
		return err
	}
	if record.length < 0 {
		return fmt.Errorf("%w: length %d is negative", ErrMetaInvariant, record.length)
	}
	if err := validatePersistedDegree(record.degree); err != nil {
		return err
	}
	keyCount := t.countReachableKeys(t.root, map[PageID]bool{})
	if keyCount != record.length {
		return fmt.Errorf("%w: length %d does not match reachable key count %d", ErrMetaInvariant, record.length, keyCount)
	}
	return nil
}

func validatePersistedDegree(degree int) error {
	if degree < 2 {
		return fmt.Errorf("%w: degree %d below minimum 2", ErrMetaInvariant, degree)
	}
	if degree > maxPageDegree {
		return fmt.Errorf("%w: degree %d exceeds page capacity %d", ErrMetaInvariant, degree, maxPageDegree)
	}
	return nil
}

func (t *Tree) validateMetaBounds(record metaRecord) error {
	mappedNextPage := PageID(len(t.arena.data) / PageSize)
	if record.maxPages < minMmapPageCount {
		return fmt.Errorf("%w: max pages %d below minimum %d", ErrMetaInvariant, record.maxPages, minMmapPageCount)
	}
	if record.nextPage > firstTreePageID+PageID(record.maxPages) {
		return fmt.Errorf("%w: next page %d beyond metadata capacity %d", ErrMetaInvariant, record.nextPage, record.maxPages)
	}
	if record.nextPage < firstTreePageID {
		return fmt.Errorf("%w: next page %d before first tree page %d", ErrMetaInvariant, record.nextPage, firstTreePageID)
	}
	if record.nextPage > mappedNextPage {
		return fmt.Errorf("%w: next page %d beyond mapped page count %d", ErrMetaInvariant, record.nextPage, mappedNextPage)
	}
	if record.root != 0 && (record.root < firstTreePageID || record.root >= record.nextPage) {
		return fmt.Errorf("%w: root page %d outside allocated range [%d,%d)", ErrMetaInvariant, record.root, firstTreePageID, record.nextPage)
	}
	return nil
}

func (t *Tree) validateLeafLinks() error {
	leaves := make([]PageID, 0)
	collectLeavesInOrder(t.pages, t.root, &leaves)
	for i, id := range leaves {
		want := PageID(0)
		if i+1 < len(leaves) {
			want = leaves[i+1]
		}
		got := t.pages[id].nextLeaf()
		if got != want {
			return fmt.Errorf("%w: leaf page %d next leaf %d, want %d", ErrTreeInvariant, id, got, want)
		}
	}
	return nil
}

func (t *Tree) countReachableKeys(id PageID, seen map[PageID]bool) int {
	if id == 0 || seen[id] {
		return 0
	}
	seen[id] = true

	p := t.pages[id]
	if p == nil {
		return 0
	}
	if p.isLeaf() {
		return int(p.slotCount())
	}
	count := 0
	for _, child := range p.childIDs() {
		count += t.countReachableKeys(child, seen)
	}
	return count
}

func (t *Tree) validatePage(id PageID, seen map[PageID]bool) error {
	if id == 0 {
		return nil
	}
	if seen[id] {
		return fmt.Errorf("%w: page %d is reachable through multiple tree paths", ErrTreeInvariant, id)
	}
	seen[id] = true

	p := t.pages[id]
	if p == nil {
		return fmt.Errorf("reachable page %d is missing", id)
	}
	if !p.validChecksum() {
		return fmt.Errorf("%w: page %d", ErrPageChecksum, id)
	}
	if err := p.validateLayout(); err != nil {
		return err
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
	children := p.childIDs()
	for index, child := range children {
		if child == 0 {
			return fmt.Errorf("%w: branch page %d child %d is zero", ErrTreeInvariant, id, index)
		}
		if err := t.validatePage(child, seen); err != nil {
			return err
		}
		if index == 0 {
			continue
		}
		separator := p.readCellKey(index - 1)
		first, ok := t.firstKey(child)
		if !ok {
			return fmt.Errorf("%w: branch page %d child %d has no first key", ErrTreeInvariant, id, index)
		}
		if separator != first {
			return fmt.Errorf("%w: branch page %d separator %q does not match child %d first key %q", ErrTreeInvariant, id, separator, index, first)
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
		if err := p.validateLayout(); err != nil {
			return err
		}
		length += p.overflowPayloadLen()
		id = p.overflowNext()
	}
	if length != ref.length {
		return fmt.Errorf("%w: chain length %d does not match referenced length %d", ErrOverflowInvariant, length, ref.length)
	}
	return nil
}
