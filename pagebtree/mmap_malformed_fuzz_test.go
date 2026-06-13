//go:build unix

package pagebtree

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMmapMalformedPageGeneratorRejectsOrChecks(t *testing.T) {
	runMmapMalformedPageGenerator(t, []byte{
		0, 11, 19, 3,
		3, 2, 0, 255,
		4, 6, 1, 128,
		5, 9, 8, 7,
		6, 13, 21, 34,
		7, 5, 1, 99,
	})
}

func FuzzMmapMalformedPageGeneratorRejectsOrChecks(f *testing.F) {
	f.Add([]byte{0, 0, 0, 7, 3, 2, 255, 4, 6, 9})
	f.Add([]byte{1, 0, 2, 31, 4, 1, 5, 2, 6, 3, 7, 4})
	f.Add([]byte{2, 9, 9, 9, 5, 1, 2, 3, 0, 255, 255})
	f.Fuzz(func(t *testing.T, data []byte) {
		runMmapMalformedPageGenerator(t, data)
	})
}

func runMmapMalformedPageGenerator(t *testing.T, data []byte) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "course.db")
	record := createMmapMalformedSeed(t, path)
	reader := modelReader{data: data}
	for step := 0; step < 12 && reader.hasMore(); step++ {
		applyMmapMalformedMutation(t, path, record, &reader)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		return
	}
	defer func() {
		_ = reopened.Close()
	}()
	if err := reopened.Check(); err != nil {
		t.Fatalf("OpenMmap accepted malformed image but Check rejected it: %v", err)
	}
}

func createMmapMalformedSeed(t *testing.T, path string) metaRecord {
	t.Helper()

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 256})
	if err != nil {
		t.Fatalf("OpenMmap seed create: %v", err)
	}
	for i := 0; i < 40; i++ {
		value := []byte(fmt.Sprintf("value-%02d", i))
		if i%8 == 0 {
			value = make([]byte, PageSize+64+i)
			for j := range value {
				value[j] = 'a' + byte((i+j)%26)
			}
		}
		tree.Put(fmt.Sprintf("key-%02d", i), value)
	}
	for i := 3; i < 40; i += 11 {
		tree.Delete(fmt.Sprintf("key-%02d", i))
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync seed database: %v", err)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check seed database: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close seed database: %v", err)
	}
	return keepOnlyNewestMetaPage(t, path)
}

func applyMmapMalformedMutation(t *testing.T, path string, record metaRecord, reader *modelReader) {
	t.Helper()

	switch reader.next() % 8 {
	case 0:
		flipMmapFileByte(t, path, malformedFileOffset(t, path, reader), reader.next())
	case 1:
		corruptMetaPage(t, path, int(reader.next()%metaPageCount))
	case 2:
		zeroMmapFilePage(t, path, malformedTreePageID(record, reader))
	case 3:
		mutatePage(t, path, malformedTreePageID(record, reader), func(p *page) {
			binary.LittleEndian.PutUint16(p.data[headerFlagsOffset:], uint16(reader.next())<<8|uint16(reader.next()))
			p.updateChecksum()
		})
	case 4:
		mutatePage(t, path, malformedTreePageID(record, reader), func(p *page) {
			binary.LittleEndian.PutUint16(p.data[headerSlotCountOffset:], uint16(reader.next())<<8|uint16(reader.next()))
			p.updateChecksum()
		})
	case 5:
		mutatePage(t, path, malformedTreePageID(record, reader), func(p *page) {
			binary.LittleEndian.PutUint16(p.data[headerFreeUpperOffset:], uint16(malformedIndex(reader, PageSize*2)))
			p.updateChecksum()
		})
	case 6:
		mutatePage(t, path, malformedTreePageID(record, reader), func(p *page) {
			child := PageID(malformedIndex(reader, int64(record.nextPage+4)))
			encodePageID(p.data[headerLeftmostOffset:headerLeftmostOffset+8], child)
			p.updateChecksum()
		})
	case 7:
		truncateMmapFile(t, path, malformedFileOffset(t, path, reader))
	}
}

func malformedTreePageID(record metaRecord, reader *modelReader) PageID {
	if record.nextPage <= firstTreePageID {
		return firstTreePageID
	}
	count := int64(record.nextPage - firstTreePageID)
	return firstTreePageID + PageID(malformedIndex(reader, count))
}

func malformedFileOffset(t *testing.T, path string, reader *modelReader) int64 {
	t.Helper()

	size := fileSize(t, path)
	if size <= 0 {
		return 0
	}
	return malformedIndex(reader, size)
}

func malformedIndex(reader *modelReader, n int64) int64 {
	if n <= 1 {
		return 0
	}
	value := uint64(reader.next()) |
		uint64(reader.next())<<8 |
		uint64(reader.next())<<16 |
		uint64(reader.next())<<24
	return int64(value % uint64(n))
}

func flipMmapFileByte(t *testing.T, path string, offset int64, value byte) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("OpenFile flip byte: %v", err)
	}
	defer file.Close()
	if _, err := file.WriteAt([]byte{value}, offset); err != nil {
		t.Fatalf("WriteAt flip byte %d: %v", offset, err)
	}
	if err := file.Sync(); err != nil {
		t.Fatalf("Sync flip byte: %v", err)
	}
}

func zeroMmapFilePage(t *testing.T, path string, id PageID) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("OpenFile zero page: %v", err)
	}
	defer file.Close()
	if _, err := file.WriteAt(make([]byte, PageSize), int64(id)*PageSize); err != nil {
		t.Fatalf("WriteAt zero page %d: %v", id, err)
	}
	if err := file.Sync(); err != nil {
		t.Fatalf("Sync zero page: %v", err)
	}
}

func truncateMmapFile(t *testing.T, path string, size int64) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("OpenFile truncate: %v", err)
	}
	defer file.Close()
	if err := file.Truncate(size); err != nil {
		t.Fatalf("Truncate malformed mmap file to %d: %v", size, err)
	}
	if err := file.Sync(); err != nil {
		t.Fatalf("Sync truncated mmap file: %v", err)
	}
}
