//go:build !unix

package pagebtree

import "errors"

var ErrDatabaseLocked = errors.New("mmap tree database is already locked")

type MmapOptions struct {
	Degree                  int
	MaxPages                int
	AccessPattern           MmapAccessPattern
	PageCacheCapacity       int
	RangePrefetchLeafWindow int
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

type readerTable struct{}

func (r *readerTable) oldest(maxRevision uint64) (uint64, bool, error) {
	return 0, false, nil
}

func (r *readerTable) stats(maxRevision uint64) (MmapReaderStats, error) {
	return MmapReaderStats{}, nil
}

func (r *readerTable) cleanStale(maxRevision uint64) (int, error) {
	return 0, nil
}

type mmapArena struct {
	readerTable   *readerTable
	maxPages      int
	accessPattern MmapAccessPattern
	dirtyPages    map[PageID]bool
}

func OpenMmap(path string, options MmapOptions) (*Tree, error) {
	return nil, errors.New("mmap page storage is only available on Unix-like platforms")
}

func OpenMmapReadOnly(path string) (*Tree, error) {
	return nil, errors.New("mmap page storage is only available on Unix-like platforms")
}

func (a *mmapArena) pageBytes(id PageID) ([]byte, error) {
	return nil, errors.New("mmap page storage is only available on Unix-like platforms")
}

func (a *mmapArena) markDirtyPage(id PageID) {}

func (a *mmapArena) advisePageRange(startPage, endPage PageID, pattern MmapAccessPattern) error {
	return nil
}

func (t *Tree) growMmapForPage(id PageID) error {
	return nil
}

func (t *Tree) syncMmap() error {
	return nil
}

func (t *Tree) compactMmapTail() error {
	return nil
}

func (t *Tree) Advise(pattern MmapAccessPattern) error {
	return nil
}

func (t *Tree) DropMmapCache() error {
	return nil
}

func (t *Tree) MmapCacheStats() (MmapCacheStats, error) {
	return MmapCacheStats{}, nil
}

func (t *Tree) MmapReaderStats() (MmapReaderStats, error) {
	return MmapReaderStats{}, nil
}

func (t *Tree) CleanStaleMmapReaders() (int, error) {
	return 0, nil
}

func normalizeMmapAccessPattern(pattern MmapAccessPattern) MmapAccessPattern {
	if pattern == MmapAccessDefault {
		return MmapAccessRandom
	}
	return pattern
}

func (a *mmapArena) close() error {
	return nil
}

func (t *Tree) persistMeta() error {
	return nil
}

func (t *Tree) loadMeta() error {
	return errors.New("mmap page storage is only available on Unix-like platforms")
}
