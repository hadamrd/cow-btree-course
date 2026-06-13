//go:build !unix

package pagebtree

import "errors"

type MmapOptions struct {
	Degree   int
	MaxPages int
}

type mmapArena struct{}

func OpenMmap(path string, options MmapOptions) (*Tree, error) {
	return nil, errors.New("mmap page storage is only available on Unix-like platforms")
}

func (a *mmapArena) pageBytes(id PageID) ([]byte, error) {
	return nil, errors.New("mmap page storage is only available on Unix-like platforms")
}

func (a *mmapArena) sync() error {
	return nil
}

func (a *mmapArena) close() error {
	return nil
}

func (t *Tree) persistMeta() {}

func (t *Tree) loadMeta() error {
	return errors.New("mmap page storage is only available on Unix-like platforms")
}
