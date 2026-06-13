//go:build !unix

package pagebtree

import "errors"

var ErrDatabaseLocked = errors.New("mmap tree database is already locked")

type MmapOptions struct {
	Degree            int
	MaxPages          int
	AccessPattern     MmapAccessPattern
	PageCacheCapacity int
}

type MmapAccessPattern int

const (
	MmapAccessDefault MmapAccessPattern = iota
	MmapAccessRandom
	MmapAccessSequential
	MmapAccessWillNeed
)

type mmapArena struct{}

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

func (t *Tree) Advise(pattern MmapAccessPattern) error {
	return nil
}

func (t *Tree) MmapCacheStats() (MmapCacheStats, error) {
	return MmapCacheStats{}, nil
}

func (a *mmapArena) close() error {
	return nil
}

func (t *Tree) persistMeta() {}

func (t *Tree) loadMeta() error {
	return errors.New("mmap page storage is only available on Unix-like platforms")
}
