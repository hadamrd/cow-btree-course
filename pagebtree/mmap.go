//go:build unix

package pagebtree

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

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
	minMmapPageCount = 2
)

type MmapOptions struct {
	Degree   int
	MaxPages int
}

type mmapArena struct {
	file     *os.File
	data     []byte
	maxPages int
}

func OpenMmap(path string, options MmapOptions) (*Tree, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	maxPages := options.MaxPages
	if info.Size() > 0 {
		if info.Size()%PageSize != 0 {
			file.Close()
			return nil, fmt.Errorf("mmap tree file size %d is not page aligned", info.Size())
		}
		existingPages := int(info.Size()/PageSize) - 1
		if maxPages < existingPages {
			maxPages = existingPages
		}
	}
	if maxPages < minMmapPageCount {
		maxPages = 1024
	}

	size := int64((maxPages + 1) * PageSize)
	if err := file.Truncate(size); err != nil {
		file.Close()
		return nil, err
	}

	data, err := unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		file.Close()
		return nil, err
	}

	arena := &mmapArena{
		file:     file,
		data:     data,
		maxPages: maxPages,
	}

	tree := &Tree{
		pages:    map[PageID]*page{},
		nextPage: 1,
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

	tree.persistMeta()
	if err := tree.Sync(); err != nil {
		arena.close()
		return nil, err
	}
	return tree, nil
}

func (a *mmapArena) initialized() bool {
	return string(a.data[metaMagicOffset:metaMagicOffset+len(metaMagic)]) == metaMagic
}

func (a *mmapArena) pageBytes(id PageID) ([]byte, error) {
	if id == 0 || int(id) > a.maxPages {
		return nil, fmt.Errorf("page id %d outside mmap capacity %d", id, a.maxPages)
	}
	start := int(id) * PageSize
	return a.data[start : start+PageSize], nil
}

func (a *mmapArena) sync() error {
	if a == nil {
		return nil
	}
	if err := unix.Msync(a.data, unix.MS_SYNC); err != nil {
		return err
	}
	return a.file.Sync()
}

func (a *mmapArena) close() error {
	if a == nil {
		return nil
	}

	var errs []error
	if err := a.sync(); err != nil {
		errs = append(errs, err)
	}
	if err := unix.Munmap(a.data); err != nil {
		errs = append(errs, err)
	}
	if err := a.file.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (t *Tree) loadMeta() error {
	if string(t.arena.data[metaMagicOffset:metaMagicOffset+len(metaMagic)]) != metaMagic {
		return fmt.Errorf("invalid mmap tree magic")
	}
	version := binary.LittleEndian.Uint64(t.arena.data[metaVersionOff:])
	if version != metaVersion {
		return fmt.Errorf("unsupported mmap tree version %d", version)
	}

	t.root = PageID(binary.LittleEndian.Uint64(t.arena.data[metaRootOff:]))
	t.nextPage = PageID(binary.LittleEndian.Uint64(t.arena.data[metaNextPageOff:]))
	t.length = int(binary.LittleEndian.Uint64(t.arena.data[metaLengthOff:]))
	t.revision = binary.LittleEndian.Uint64(t.arena.data[metaRevisionOff:])
	t.degree = normalizeDegree(int(binary.LittleEndian.Uint64(t.arena.data[metaDegreeOff:])))
	if t.nextPage == 0 {
		t.nextPage = 1
	}

	for id := PageID(1); id < t.nextPage; id++ {
		data, err := t.arena.pageBytes(id)
		if err != nil {
			return err
		}
		t.pages[id] = &page{id: id, data: data}
	}
	return nil
}

func (t *Tree) persistMeta() {
	if t.arena == nil {
		return
	}

	copy(t.arena.data[metaMagicOffset:], metaMagic)
	binary.LittleEndian.PutUint64(t.arena.data[metaVersionOff:], metaVersion)
	binary.LittleEndian.PutUint64(t.arena.data[metaRootOff:], uint64(t.root))
	binary.LittleEndian.PutUint64(t.arena.data[metaNextPageOff:], uint64(t.nextPage))
	binary.LittleEndian.PutUint64(t.arena.data[metaLengthOff:], uint64(t.length))
	binary.LittleEndian.PutUint64(t.arena.data[metaRevisionOff:], t.revision)
	binary.LittleEndian.PutUint64(t.arena.data[metaDegreeOff:], uint64(t.degree))
	binary.LittleEndian.PutUint64(t.arena.data[metaMaxPagesOff:], uint64(t.arena.maxPages))
}
