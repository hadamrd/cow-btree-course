//go:build unix

package pagebtree

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
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
	metaChecksumOff  = 64
	metaFreeCountOff = 72
	metaFreeListOff  = 80
	metaPageCount    = 2
	minMmapPageCount = 2
)

const firstTreePageID = PageID(metaPageCount)
const maxMetaFreePages = (PageSize - metaFreeListOff) / 8

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

	tree.persistMeta()
	if err := tree.Sync(); err != nil {
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
	var newest metaRecord
	found := false
	for index := 0; index < metaPageCount; index++ {
		record, ok := readMetaPage(t.arena.data[index*PageSize : (index+1)*PageSize])
		if !ok {
			continue
		}
		if !found || record.revision > newest.revision {
			newest = record
			found = true
		}
	}
	if !found {
		return fmt.Errorf("no valid mmap tree metadata page")
	}

	t.root = newest.root
	t.nextPage = newest.nextPage
	t.length = newest.length
	t.revision = newest.revision
	t.degree = normalizeDegree(newest.degree)
	t.free = append([]PageID(nil), newest.free...)
	if t.nextPage < firstTreePageID {
		t.nextPage = firstTreePageID
	}

	for id := firstTreePageID; id < t.nextPage; id++ {
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
