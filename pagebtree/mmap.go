//go:build unix

package pagebtree

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"unsafe"

	"golang.org/x/sys/unix"
)

var (
	ErrDatabaseLocked = errors.New("mmap tree database is already locked")
	ErrMetaInvariant  = errors.New("metadata invariant invalid")
)

var (
	mmapBytes   = unix.Mmap
	munmapBytes = unix.Munmap
)

var syncDirectoryPath = syncDirectoryPathOS

var readOnlyBeforeLoadMeta func(*Tree)

const (
	metaMagic        = "COWBTREE"
	metaVersion      = uint64(3)
	minMetaVersion   = uint64(1)
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
	file               *os.File
	writerLock         *os.File
	readerTable        *readerTable
	path               string
	data               []byte
	maxPages           int
	locked             bool
	readOnly           bool
	accessPattern      MmapAccessPattern
	dirtyPages         map[PageID]bool
	syncObserver       func(string)
	dataSyncObserver   func(start, end PageID)
	dirSyncObserver    func(path string)
	adviceObserver     func(pattern MmapAccessPattern, start, end PageID)
	fileAdviceObserver func(pattern MmapAccessPattern, start, end PageID)
}

type MmapAccessPattern int

const (
	MmapAccessDefault MmapAccessPattern = iota
	MmapAccessRandom
	MmapAccessSequential
	MmapAccessWillNeed
	MmapAccessNormal
	mmapAccessDontNeed
)

func OpenMmap(path string, options MmapOptions) (*Tree, error) {
	writerLock, err := openWriterLock(path)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		closeWriterLock(writerLock)
		return nil, err
	}
	if err := lockFile(file, false); err != nil {
		closeWriterLock(writerLock)
		file.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		unlockFile(file)
		closeWriterLock(writerLock)
		file.Close()
		return nil, err
	}

	existingSize := info.Size()
	maxPages := options.MaxPages
	if existingSize > 0 {
		if err := validateExistingMmapFileSize(existingSize); err != nil {
			unlockFile(file)
			closeWriterLock(writerLock)
			file.Close()
			return nil, err
		}
		maxPages = int(existingSize/PageSize) - metaPageCount
	} else if maxPages < minMmapPageCount {
		maxPages = 1024
	}

	size := int64((maxPages + metaPageCount) * PageSize)
	if err := file.Truncate(size); err != nil {
		unlockFile(file)
		closeWriterLock(writerLock)
		file.Close()
		return nil, err
	}

	data, err := mmapBytes(int(file.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unlockFile(file)
		closeWriterLock(writerLock)
		file.Close()
		return nil, err
	}
	readerTable, err := openReaderTable(path)
	if err != nil {
		munmapBytes(data)
		unlockFile(file)
		closeWriterLock(writerLock)
		file.Close()
		return nil, err
	}

	arena := &mmapArena{
		file:        file,
		writerLock:  writerLock,
		readerTable: readerTable,
		path:        path,
		data:        data,
		maxPages:    maxPages,
		locked:      true,
		dirtyPages:  map[PageID]bool{},
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
	if err := arena.syncDirectory(); err != nil {
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
	if err := validateExistingMmapFileSize(info.Size()); err != nil {
		unlockFile(file)
		file.Close()
		return nil, err
	}

	size := int(info.Size())
	data, err := mmapBytes(int(file.Fd()), 0, size, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		unlockFile(file)
		file.Close()
		return nil, err
	}

	arena := &mmapArena{
		file:     file,
		path:     path,
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
	readerTable, err := openReaderTable(path)
	if err != nil {
		arena.close()
		return nil, err
	}
	arena.readerTable = readerTable
	if err := readerTable.claim(0); err != nil {
		arena.close()
		return nil, err
	}
	if readOnlyBeforeLoadMeta != nil {
		readOnlyBeforeLoadMeta(tree)
	}
	if err := arena.advise(MmapAccessDefault); err != nil {
		arena.close()
		return nil, err
	}
	if err := tree.loadMeta(); err != nil {
		arena.close()
		return nil, err
	}
	if err := readerTable.updateRevision(tree.revision); err != nil {
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

func validateExistingMmapFileSize(size int64) error {
	if size%PageSize != 0 {
		return fmt.Errorf("mmap tree file size %d is not page aligned", size)
	}
	minSize := int64((metaPageCount + minMmapPageCount) * PageSize)
	if size < minSize {
		return fmt.Errorf("%w: mmap tree file size %d below minimum %d", ErrMetaInvariant, size, minSize)
	}
	return nil
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
	oldSize := int64(len(t.arena.data))
	if err := t.arena.file.Truncate(newSize); err != nil {
		return err
	}
	if err := t.arena.syncFileSize(); err != nil {
		return err
	}

	data, err := mmapBytes(int(t.arena.file.Fd()), 0, int(newSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return err
	}
	oldData := t.arena.data
	if err := munmapBytes(oldData); err != nil {
		_ = munmapBytes(data)
		if restoreErr := t.arena.file.Truncate(oldSize); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		if syncErr := t.arena.syncFileSize(); syncErr != nil {
			return errors.Join(err, syncErr)
		}
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

	oldNextPage := t.nextPage
	oldFree := append([]PageID(nil), t.free...)
	oldMetaFreelistRoot := t.metaFreelistRoot
	oldMetaFreelistPages := append([]PageID(nil), t.metaFreelistPages...)
	oldDirtyPages := cloneDirtyPages(t.arena.dirtyPages)
	removedPages := map[PageID]*page{}
	restoreState := func() {
		t.nextPage = oldNextPage
		t.free = oldFree
		t.metaFreelistRoot = oldMetaFreelistRoot
		t.metaFreelistPages = oldMetaFreelistPages
		t.arena.maxPages = oldMaxPages
		t.arena.dirtyPages = cloneDirtyPages(oldDirtyPages)
		for id, p := range removedPages {
			t.pages[id] = p
		}
	}

	t.removeFreePagesAtOrAbove(trimmedNextPage)
	for id := range t.pages {
		if id >= trimmedNextPage {
			removedPages[id] = t.pages[id]
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

	restoreFreelistPages, err := t.prepareMetaFreelistPages()
	if err != nil {
		restoreState()
		return err
	}
	if err := t.arena.syncDataPages(t.nextPage); err != nil {
		restoreFreelistPages()
		restoreState()
		return err
	}
	if err := t.publishMeta(); err != nil {
		restoreFreelistPages()
		restoreState()
		return err
	}
	t.reclaimObsoleteMetaFreelistPages()
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
	oldSize := int64(len(t.arena.data))
	if err := t.arena.file.Truncate(newSize); err != nil {
		return err
	}
	if err := t.arena.syncFileSize(); err != nil {
		return err
	}

	data, err := mmapBytes(int(t.arena.file.Fd()), 0, int(newSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		if restoreErr := t.arena.file.Truncate(oldSize); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		if syncErr := t.arena.syncFileSize(); syncErr != nil {
			return errors.Join(err, syncErr)
		}
		return err
	}
	oldData := t.arena.data
	if err := munmapBytes(oldData); err != nil {
		_ = munmapBytes(data)
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

func cloneDirtyPages(dirty map[PageID]bool) map[PageID]bool {
	if dirty == nil {
		return nil
	}
	clone := make(map[PageID]bool, len(dirty))
	for id, value := range dirty {
		clone[id] = value
	}
	return clone
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

func (a *mmapArena) syncFileSize() error {
	if a == nil || a.readOnly {
		return nil
	}
	if err := a.file.Sync(); err != nil {
		return err
	}
	return a.syncDirectory()
}

func (a *mmapArena) syncDirectory() error {
	if a == nil || a.path == "" {
		return nil
	}
	dir := filepath.Dir(a.path)
	if a.dirSyncObserver != nil {
		a.dirSyncObserver(dir)
	}
	return syncDirectoryPath(dir)
}

func syncDirectoryPathOS(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
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
	normalized := normalizeMmapAccessPattern(pattern)
	advice, err := mmapAdvice(normalized)
	if err != nil {
		return err
	}
	if err := a.adviseFileRange(0, PageID(len(a.data)/PageSize), normalized); err != nil {
		return err
	}
	a.accessPattern = normalized
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
	normalized := normalizeMmapAccessPattern(pattern)
	advice, err := mmapAdvice(normalized)
	if err != nil {
		return err
	}
	if err := a.adviseFileRange(startPage, endPage, normalized); err != nil {
		return err
	}
	if a.adviceObserver != nil {
		a.adviceObserver(normalized, startPage, endPage)
	}
	return a.madviseRange(int(startPage)*PageSize, endByte, advice)
}

func (a *mmapArena) adviseFileRange(startPage, endPage PageID, pattern MmapAccessPattern) error {
	if a == nil || a.file == nil || len(a.data) == 0 {
		return nil
	}
	if endPage < startPage {
		return fmt.Errorf("invalid file advice page range [%d,%d)", startPage, endPage)
	}
	mappedPages := PageID(len(a.data) / PageSize)
	if endPage > mappedPages {
		return fmt.Errorf("file advice page range [%d,%d) exceeds mmap page count %d", startPage, endPage, mappedPages)
	}
	if startPage == endPage {
		return nil
	}
	if a.fileAdviceObserver != nil {
		a.fileAdviceObserver(pattern, startPage, endPage)
	}
	offset := int64(startPage) * PageSize
	length := int64(endPage-startPage) * PageSize
	return fadviseFileRange(a.file, offset, length, pattern)
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
	case MmapAccessDefault, MmapAccessRandom:
		return unix.MADV_RANDOM, nil
	case MmapAccessNormal:
		return unix.MADV_NORMAL, nil
	case MmapAccessSequential:
		return unix.MADV_SEQUENTIAL, nil
	case MmapAccessWillNeed:
		return unix.MADV_WILLNEED, nil
	case mmapAccessDontNeed:
		return unix.MADV_DONTNEED, nil
	default:
		return 0, fmt.Errorf("unknown mmap access pattern %d", pattern)
	}
}

func normalizeMmapAccessPattern(pattern MmapAccessPattern) MmapAccessPattern {
	if pattern == MmapAccessDefault {
		return MmapAccessRandom
	}
	return pattern
}

func (a *mmapArena) close() error {
	if a == nil {
		return nil
	}

	var errs []error
	if a.readerTable != nil {
		if err := a.readerTable.close(); err != nil {
			errs = append(errs, err)
		} else {
			a.readerTable = nil
		}
	}
	if len(a.data) > 0 {
		if err := munmapBytes(a.data); err != nil {
			errs = append(errs, err)
		} else {
			a.data = nil
			a.dirtyPages = nil
		}
	}
	if a.locked && a.file != nil {
		if err := unlockFile(a.file); err != nil {
			errs = append(errs, err)
		} else {
			a.locked = false
		}
	}
	if a.file != nil {
		if err := a.file.Close(); err != nil {
			errs = append(errs, err)
		} else {
			a.file = nil
		}
	}
	if a.writerLock != nil {
		if err := closeWriterLock(a.writerLock); err != nil {
			errs = append(errs, err)
		} else {
			a.writerLock = nil
		}
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
	version       uint64
	root          PageID
	nextPage      PageID
	length        int
	revision      uint64
	degree        int
	maxPages      int
	free          []PageID
	retired       []retiredPage
	freeCount     int
	freeRoot      PageID
	freelistPages []PageID
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
		freelistPages, err := t.resolveMetaFreelist(&record)
		if err != nil {
			lastErr = err
			continue
		}
		reachable, err := t.validateReachablePages()
		if err != nil {
			lastErr = err
			continue
		}
		owned := clonePageSet(reachable)
		var overlapErr error
		for _, id := range freelistPages {
			if owned[id] {
				overlapErr = fmt.Errorf("%w: freelist metadata page %d overlaps reachable page", ErrFreelist, id)
				break
			}
			owned[id] = true
		}
		if overlapErr != nil {
			lastErr = overlapErr
			continue
		}
		reclaimable := clonePageSet(owned)
		if err := t.validateFreelist(record.free, record.maxPages, reclaimable); err != nil {
			lastErr = err
			continue
		}
		if err := t.validateRetiredPages(record.retired, record.maxPages, reclaimable); err != nil {
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
		t.free = append([]PageID(nil), record.free...)
		t.retired = append([]retiredPage(nil), record.retired...)
		t.metaFreelistRoot = record.freeRoot
		t.metaFreelistPages = append([]PageID(nil), freelistPages...)
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
	t.free = nil
	t.retired = nil
	t.metaFreelistRoot = 0
	t.metaFreelistPages = nil
	if t.nextPage < firstTreePageID {
		t.nextPage = firstTreePageID
	}
}

func clonePageSet(values map[PageID]bool) map[PageID]bool {
	out := make(map[PageID]bool, len(values))
	for id, value := range values {
		out[id] = value
	}
	return out
}

func (t *Tree) resolveMetaFreelist(record *metaRecord) ([]PageID, error) {
	if record.freeRoot == 0 {
		record.free = append([]PageID(nil), record.free...)
		record.retired = append([]retiredPage(nil), record.retired...)
		record.freelistPages = nil
		return nil, nil
	}
	if record.version >= 3 {
		return t.resolveMetaReclaim(record)
	}
	maxOwnedPage := firstTreePageID + PageID(record.maxPages)
	free := make([]PageID, 0, record.freeCount)
	freelistPages := make([]PageID, 0)
	seen := map[PageID]bool{}
	for id := record.freeRoot; id != 0; {
		if id < firstTreePageID || id >= record.nextPage || id >= maxOwnedPage {
			return nil, fmt.Errorf("%w: freelist metadata page %d outside allocated range", ErrFreelist, id)
		}
		if seen[id] {
			return nil, fmt.Errorf("%w: freelist metadata chain loops through page %d", ErrFreelist, id)
		}
		seen[id] = true
		p := t.pages[id]
		if p == nil {
			return nil, fmt.Errorf("%w: freelist metadata page %d is missing", ErrFreelist, id)
		}
		if !p.validChecksum() {
			return nil, fmt.Errorf("%w: page %d", ErrPageChecksum, id)
		}
		if err := p.validateLayout(); err != nil {
			return nil, err
		}
		if p.flags() != flagFreelist {
			return nil, fmt.Errorf("%w: page %d in freelist metadata chain is not a freelist page", ErrFreelist, id)
		}
		freelistPages = append(freelistPages, id)
		for _, freeID := range p.freelistIDs() {
			if len(free) >= record.freeCount {
				return nil, fmt.Errorf("%w: freelist metadata chain has more than %d entries", ErrFreelist, record.freeCount)
			}
			free = append(free, freeID)
		}
		id = p.freelistNext()
	}
	if len(free) != record.freeCount {
		return nil, fmt.Errorf("%w: freelist metadata chain has %d entries, want %d", ErrFreelist, len(free), record.freeCount)
	}
	record.free = free
	record.freelistPages = freelistPages
	return freelistPages, nil
}

func (t *Tree) resolveMetaReclaim(record *metaRecord) ([]PageID, error) {
	maxOwnedPage := firstTreePageID + PageID(record.maxPages)
	free := make([]PageID, 0, record.freeCount)
	retired := make([]retiredPage, 0)
	reclaimPages := make([]PageID, 0)
	seen := map[PageID]bool{}
	total := 0
	for id := record.freeRoot; id != 0; {
		if id < firstTreePageID || id >= record.nextPage || id >= maxOwnedPage {
			return nil, fmt.Errorf("%w: reclaim metadata page %d outside allocated range", ErrFreelist, id)
		}
		if seen[id] {
			return nil, fmt.Errorf("%w: reclaim metadata chain loops through page %d", ErrFreelist, id)
		}
		seen[id] = true
		p := t.pages[id]
		if p == nil {
			return nil, fmt.Errorf("%w: reclaim metadata page %d is missing", ErrFreelist, id)
		}
		if !p.validChecksum() {
			return nil, fmt.Errorf("%w: page %d", ErrPageChecksum, id)
		}
		if err := p.validateLayout(); err != nil {
			return nil, err
		}
		if p.flags() != flagReclaim {
			return nil, fmt.Errorf("%w: page %d in reclaim metadata chain is not a reclaim page", ErrFreelist, id)
		}
		reclaimPages = append(reclaimPages, id)
		for _, entry := range p.reclaimRecords() {
			if total >= record.freeCount {
				return nil, fmt.Errorf("%w: reclaim metadata chain has more than %d entries", ErrFreelist, record.freeCount)
			}
			total++
			if entry.revision == 0 {
				free = append(free, entry.id)
				continue
			}
			retired = append(retired, retiredPage{id: entry.id, revision: entry.revision})
		}
		id = p.reclaimNext()
	}
	if total != record.freeCount {
		return nil, fmt.Errorf("%w: reclaim metadata chain has %d entries, want %d", ErrFreelist, total, record.freeCount)
	}
	record.free = free
	record.retired = retired
	record.freelistPages = reclaimPages
	return reclaimPages, nil
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

func (t *Tree) persistMeta() error {
	if t.arena == nil || t.arena.readOnly {
		return nil
	}

	index := int(t.revision % metaPageCount)
	version := uint64(2)
	if len(t.retired) > 0 {
		version = metaVersion
	}
	return writeMetaPage(t.arena.data[index*PageSize:(index+1)*PageSize], metaRecord{
		version:  version,
		root:     t.root,
		nextPage: t.nextPage,
		length:   t.length,
		revision: t.revision,
		degree:   t.degree,
		maxPages: t.arena.maxPages,
		free:     t.free,
		retired:  t.retired,
		freeRoot: t.metaFreelistRoot,
	})
}

func (t *Tree) syncMmap() error {
	if t.arena == nil || t.arena.readOnly {
		return nil
	}
	restoreFreelistPages, err := t.prepareMetaFreelistPages()
	if err != nil {
		return err
	}
	if err := t.arena.syncDataPages(t.nextPage); err != nil {
		restoreFreelistPages()
		return err
	}
	if err := t.publishMeta(); err != nil {
		restoreFreelistPages()
		return err
	}
	t.reclaimObsoleteMetaFreelistPages()
	return nil
}

func (t *Tree) prepareMetaFreelistPages() (func(), error) {
	oldNextPage := t.nextPage
	oldMetaFreelistRoot := t.metaFreelistRoot
	oldMetaFreelistPages := append([]PageID(nil), t.metaFreelistPages...)
	oldDirtyPages := cloneDirtyPages(t.arena.dirtyPages)
	addedPages := map[PageID]*page{}
	restore := func() {
		t.nextPage = oldNextPage
		t.metaFreelistRoot = oldMetaFreelistRoot
		t.metaFreelistPages = oldMetaFreelistPages
		t.arena.dirtyPages = cloneDirtyPages(oldDirtyPages)
		for id := range addedPages {
			delete(t.pages, id)
		}
	}

	if len(t.retired) == 0 {
		return t.prepareMetaFreePages(addedPages, restore)
	}

	records := reclaimRecordsFor(t.free, t.retired)
	pageCount := divideRoundUp(len(records), reclaimPageCapacity)
	ids := make([]PageID, pageCount)
	for i := range ids {
		id := t.nextPage
		if err := t.growMmapForPage(id); err != nil {
			restore()
			return func() {}, err
		}
		t.nextPage++
		p := t.newPage(id, flagReclaim)
		t.pages[id] = p
		addedPages[id] = p
		ids[i] = id
	}
	for i, id := range ids {
		next := PageID(0)
		if i+1 < len(ids) {
			next = ids[i+1]
		}
		start := i * reclaimPageCapacity
		end := start + reclaimPageCapacity
		if end > len(records) {
			end = len(records)
		}
		writeReclaimPage(t.pages[id], next, records[start:end])
		t.arena.markDirtyPage(id)
	}
	t.metaFreelistRoot = ids[0]
	t.metaFreelistPages = ids
	return restore, nil
}

func (t *Tree) prepareMetaFreePages(addedPages map[PageID]*page, restore func()) (func(), error) {
	if len(t.free) <= maxMetaFreePages {
		t.metaFreelistRoot = 0
		t.metaFreelistPages = nil
		return restore, nil
	}

	pageCount := divideRoundUp(len(t.free), freelistPageCapacity)
	ids := make([]PageID, pageCount)
	for i := range ids {
		id := t.nextPage
		if err := t.growMmapForPage(id); err != nil {
			restore()
			return func() {}, err
		}
		t.nextPage++
		p := t.newPage(id, flagFreelist)
		t.pages[id] = p
		addedPages[id] = p
		ids[i] = id
	}
	for i, id := range ids {
		next := PageID(0)
		if i+1 < len(ids) {
			next = ids[i+1]
		}
		start := i * freelistPageCapacity
		end := start + freelistPageCapacity
		if end > len(t.free) {
			end = len(t.free)
		}
		writeFreelistPage(t.pages[id], next, t.free[start:end])
		t.arena.markDirtyPage(id)
	}
	t.metaFreelistRoot = ids[0]
	t.metaFreelistPages = ids
	return restore, nil
}

func reclaimRecordsFor(free []PageID, retired []retiredPage) []reclaimRecord {
	records := make([]reclaimRecord, 0, len(free)+len(retired))
	for _, id := range free {
		records = append(records, reclaimRecord{id: id})
	}
	for _, retired := range retired {
		records = append(records, reclaimRecord{id: retired.id, revision: retired.revision})
	}
	return records
}

func (t *Tree) reclaimObsoleteMetaFreelistPages() {
	if t.arena == nil {
		return
	}
	referenced := map[PageID]bool{}
	for index := 0; index < metaPageCount; index++ {
		record, ok, err := readMetaPageChecked(t.arena.data[index*PageSize : (index+1)*PageSize])
		if err != nil || !ok || record.freeRoot == 0 {
			continue
		}
		t.collectFreelistChainLenient(record.freeRoot, referenced)
	}
	for _, id := range t.metaFreelistPages {
		referenced[id] = true
	}

	freeSet := map[PageID]bool{}
	for _, id := range t.free {
		freeSet[id] = true
	}
	for id, p := range t.pages {
		if id < firstTreePageID || p == nil || !isMetadataReclaimPage(p) || referenced[id] || freeSet[id] {
			continue
		}
		t.free = append(t.free, id)
		freeSet[id] = true
	}
}

func isMetadataReclaimPage(p *page) bool {
	switch p.flags() {
	case flagFreelist, flagReclaim:
		return true
	default:
		return false
	}
}

func (t *Tree) collectFreelistChainLenient(root PageID, out map[PageID]bool) {
	seen := map[PageID]bool{}
	for id := root; id != 0; {
		if seen[id] {
			return
		}
		seen[id] = true
		out[id] = true
		p := t.pages[id]
		if p == nil || !p.validChecksum() || p.validateLayout() != nil {
			return
		}
		switch p.flags() {
		case flagFreelist:
			id = p.freelistNext()
		case flagReclaim:
			id = p.reclaimNext()
		default:
			return
		}
	}
}

func (t *Tree) publishMeta() error {
	if t.arena == nil || t.arena.readOnly {
		return nil
	}
	index := int(t.revision % metaPageCount)
	metaPage := t.arena.data[index*PageSize : (index+1)*PageSize]
	previous := cloneBytes(metaPage)
	if err := t.persistMeta(); err != nil {
		copy(metaPage, previous)
		return err
	}
	if err := t.arena.syncMetaPage(index); err != nil {
		copy(metaPage, previous)
		return err
	}
	return nil
}

func (t *Tree) Advise(pattern MmapAccessPattern) error {
	if t == nil || t.closed || t.arena == nil {
		return nil
	}
	return t.arena.advise(pattern)
}

// DropMmapCache asks the kernel to evict clean mmap-backed tree pages.
//
// Writable trees sync first so MADV_DONTNEED is only applied to clean pages.
// Read-only mmap handles skip the sync and issue the advice directly.
// Memory-backed trees treat this as a no-op.
func (t *Tree) DropMmapCache() error {
	if t == nil || t.closed || t.arena == nil {
		return nil
	}
	if !t.readOnly {
		if err := t.Sync(); err != nil {
			return err
		}
	}
	if t.nextPage <= firstTreePageID {
		return nil
	}
	return t.arena.advisePageRange(firstTreePageID, t.nextPage, mmapAccessDontNeed)
}

// MmapCacheStats reports kernel page-cache residency for an mmap-backed tree.
func (t *Tree) MmapCacheStats() (MmapCacheStats, error) {
	if t == nil || t.closed || t.arena == nil {
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
	version := binary.LittleEndian.Uint64(data[metaVersionOff:])
	if version < minMetaVersion || version > metaVersion {
		return metaRecord{}, false, fmt.Errorf("%w: metadata version %d unsupported", ErrMetaInvariant, version)
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
	if version == 1 && freeCount > maxMetaFreePages {
		return metaRecord{}, false, fmt.Errorf("%w: metadata free count %d exceeds %d", ErrMetaInvariant, freeCount, maxMetaFreePages)
	}
	free := []PageID(nil)
	freeRoot := PageID(0)
	rootField := PageID(binary.LittleEndian.Uint64(data[metaFreeListOff:]))
	if version >= 3 {
		if freeCount > 0 && rootField == 0 {
			return metaRecord{}, false, fmt.Errorf("%w: metadata reclaim list has zero root", ErrMetaInvariant)
		}
		freeRoot = rootField
	} else if freeCount <= maxMetaFreePages {
		free = make([]PageID, 0, freeCount)
		for i := 0; i < freeCount; i++ {
			offset := metaFreeListOff + i*8
			free = append(free, PageID(binary.LittleEndian.Uint64(data[offset:])))
		}
	} else {
		freeRoot = rootField
		if freeRoot == 0 {
			return metaRecord{}, false, fmt.Errorf("%w: metadata reclaim list has zero root", ErrMetaInvariant)
		}
	}
	return metaRecord{
		version:   version,
		root:      PageID(binary.LittleEndian.Uint64(data[metaRootOff:])),
		nextPage:  PageID(binary.LittleEndian.Uint64(data[metaNextPageOff:])),
		length:    int(binary.LittleEndian.Uint64(data[metaLengthOff:])),
		revision:  binary.LittleEndian.Uint64(data[metaRevisionOff:]),
		degree:    int(binary.LittleEndian.Uint64(data[metaDegreeOff:])),
		maxPages:  int(binary.LittleEndian.Uint64(data[metaMaxPagesOff:])),
		free:      free,
		freeCount: freeCount,
		freeRoot:  freeRoot,
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

func writeMetaPage(data []byte, record metaRecord) error {
	version := record.version
	if version == 0 {
		version = metaVersion
	}
	if version < 3 && len(record.retired) > 0 {
		return fmt.Errorf("%w: metadata version %d cannot encode retired pages", ErrMetaInvariant, version)
	}
	reclaimCount := len(record.free) + len(record.retired)
	if version < 3 && record.freeRoot == 0 && len(record.retired) == 0 {
		reclaimCount = len(record.free)
	}
	if version >= 3 && record.freeRoot == 0 && reclaimCount > 0 {
		return fmt.Errorf("%w: metadata reclaim count %d needs reclaim root", ErrMetaInvariant, reclaimCount)
	}
	if record.freeRoot == 0 && reclaimCount > maxMetaFreePages {
		return fmt.Errorf("%w: metadata reclaim count %d exceeds inline capacity %d", ErrMetaInvariant, reclaimCount, maxMetaFreePages)
	}
	if record.freeRoot != 0 && reclaimCount == 0 {
		return fmt.Errorf("%w: metadata reclaim root without records", ErrMetaInvariant)
	}

	clear(data)
	copy(data[metaMagicOffset:], metaMagic)
	binary.LittleEndian.PutUint64(data[metaVersionOff:], version)
	binary.LittleEndian.PutUint64(data[metaPageSizeOff:], PageSize)
	binary.LittleEndian.PutUint64(data[metaRootOff:], uint64(record.root))
	binary.LittleEndian.PutUint64(data[metaNextPageOff:], uint64(record.nextPage))
	binary.LittleEndian.PutUint64(data[metaLengthOff:], uint64(record.length))
	binary.LittleEndian.PutUint64(data[metaRevisionOff:], record.revision)
	binary.LittleEndian.PutUint64(data[metaDegreeOff:], uint64(record.degree))
	binary.LittleEndian.PutUint64(data[metaMaxPagesOff:], uint64(record.maxPages))
	binary.LittleEndian.PutUint64(data[metaFreeCountOff:], uint64(reclaimCount))
	if record.freeRoot != 0 {
		binary.LittleEndian.PutUint64(data[metaFreeListOff:], uint64(record.freeRoot))
	} else {
		for i, id := range record.free {
			offset := metaFreeListOff + i*8
			binary.LittleEndian.PutUint64(data[offset:], uint64(id))
		}
	}
	binary.LittleEndian.PutUint32(data[metaChecksumOff:], metaChecksum(data))
	return nil
}

func metaChecksum(data []byte) uint32 {
	checksum := crc32.NewIEEE()
	_, _ = checksum.Write(data[:metaChecksumOff])
	_, _ = checksum.Write(data[metaChecksumOff+4 : PageSize])
	return checksum.Sum32()
}

func (t *Tree) validateFreelist(free []PageID, maxPages int, reachable map[PageID]bool) error {
	seenFree := map[PageID]bool{}
	maxReusablePage := firstTreePageID + PageID(maxPages)
	for _, id := range free {
		if id >= maxReusablePage {
			return fmt.Errorf("%w: page %d beyond metadata capacity %d", ErrFreelist, id, maxPages)
		}
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
		reachable[id] = true
	}
	return nil
}

func (t *Tree) validateRetiredPages(retired []retiredPage, maxPages int, claimed map[PageID]bool) error {
	seenRetired := map[PageID]bool{}
	maxReusablePage := firstTreePageID + PageID(maxPages)
	for _, retired := range retired {
		id := retired.id
		if retired.revision == 0 {
			return fmt.Errorf("%w: retired page %d has zero revision", ErrFreelist, id)
		}
		if id >= maxReusablePage {
			return fmt.Errorf("%w: retired page %d beyond metadata capacity %d", ErrFreelist, id, maxPages)
		}
		if id < firstTreePageID || id >= t.nextPage {
			return fmt.Errorf("%w: retired page %d outside reusable range [%d,%d)", ErrFreelist, id, firstTreePageID, t.nextPage)
		}
		if claimed[id] {
			return fmt.Errorf("%w: retired page %d overlaps reachable or reusable page", ErrFreelist, id)
		}
		if seenRetired[id] {
			return fmt.Errorf("%w: retired page %d appears more than once", ErrFreelist, id)
		}
		seenRetired[id] = true
		claimed[id] = true
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
