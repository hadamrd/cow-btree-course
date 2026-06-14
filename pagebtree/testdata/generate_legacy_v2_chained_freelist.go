//go:build ignore

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"runtime"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

const (
	pageSize              = 4096
	pageHeaderSize        = 20
	freelistPageCapacity  = (pageSize - pageHeaderSize) / 8
	metaMagic             = "COWBTREE"
	metaPageCount         = 2
	metaMagicOffset       = 0
	metaVersionOff        = 8
	metaRevisionOff       = 48
	metaChecksumOff       = 72
	metaFreeCountOff      = 80
	metaFreeListOff       = 88
	headerFlagsOffset     = 0
	headerSlotCountOffset = 2
	headerLeftmostOffset  = 8
	headerChecksumOffset  = 16
	flagFreelist          = uint16(0x08)
)

func main() {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate generator source")
	}
	dir := filepath.Dir(source)
	out := filepath.Join(dir, "mmap-v2-chained-freelist.db")
	tmp := out + ".tmp"
	removeGeneratedFiles(tmp)

	tree, err := pagebtree.OpenMmap(tmp, pagebtree.MmapOptions{Degree: 2, MaxPages: 768})
	if err != nil {
		panic(fmt.Errorf("create mmap fixture: %w", err))
	}
	tree.Put("anchor", []byte("survivor"))
	tree.Put("large", bytes.Repeat([]byte("x"), pageSize*640))
	if _, deleted := tree.Delete("large"); !deleted {
		panic("delete large: key not found")
	}
	report := tree.Audit()
	if !report.Valid() {
		panic(fmt.Errorf("audit generated fixture: %w", report.Error))
	}
	if len(report.FreePageIDs) <= freelistPageCapacity {
		panic(fmt.Errorf("generated fixture free pages = %d, want above one freelist page capacity %d", len(report.FreePageIDs), freelistPageCapacity))
	}
	if err := tree.Close(); err != nil {
		panic(fmt.Errorf("close mmap fixture: %w", err))
	}

	data, err := os.ReadFile(tmp)
	if err != nil {
		panic(fmt.Errorf("read mmap fixture: %w", err))
	}
	index, freeRoot, freeCount := newestFreelistMeta(data)
	if index < 0 {
		panic("generated fixture has no valid freelist metadata")
	}
	if freeRoot == 0 {
		panic("generated fixture has no freelist root")
	}
	if freeCount <= freelistPageCapacity {
		panic(fmt.Errorf("generated fixture free count = %d, want chained freelist", freeCount))
	}
	pages, records := freelistChainSummary(data, freeRoot)
	if pages < 2 {
		panic(fmt.Errorf("generated fixture freelist pages = %d, want at least 2", pages))
	}
	if records != int(freeCount) {
		panic(fmt.Errorf("generated fixture freelist records = %d, want %d", records, freeCount))
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		panic(fmt.Errorf("write mmap fixture: %w", err))
	}
	removeGeneratedFiles(tmp)
}

func newestFreelistMeta(data []byte) (int, uint64, uint64) {
	bestIndex := -1
	var bestRevision uint64
	var bestRoot uint64
	var bestCount uint64
	for index := 0; index < metaPageCount; index++ {
		page := data[index*pageSize : (index+1)*pageSize]
		if string(page[metaMagicOffset:metaMagicOffset+len(metaMagic)]) != metaMagic {
			continue
		}
		if binary.LittleEndian.Uint32(page[metaChecksumOff:]) != metaChecksum(page) {
			continue
		}
		version := binary.LittleEndian.Uint64(page[metaVersionOff:])
		if version != 2 {
			continue
		}
		revision := binary.LittleEndian.Uint64(page[metaRevisionOff:])
		if bestIndex >= 0 && revision <= bestRevision {
			continue
		}
		bestIndex = index
		bestRevision = revision
		bestRoot = binary.LittleEndian.Uint64(page[metaFreeListOff:])
		bestCount = binary.LittleEndian.Uint64(page[metaFreeCountOff:])
	}
	return bestIndex, bestRoot, bestCount
}

func freelistChainSummary(data []byte, root uint64) (int, int) {
	pages := 0
	records := 0
	seen := map[uint64]bool{}
	for id := root; id != 0; {
		if seen[id] {
			panic(fmt.Errorf("freelist chain loops through page %d", id))
		}
		seen[id] = true
		start := id * pageSize
		end := start + pageSize
		if end > uint64(len(data)) {
			panic(fmt.Errorf("freelist page %d outside file", id))
		}
		page := data[start:end]
		if binary.LittleEndian.Uint32(page[headerChecksumOffset:]) != pageChecksum(page) {
			panic(fmt.Errorf("freelist page %d checksum invalid", id))
		}
		if flags := binary.LittleEndian.Uint16(page[headerFlagsOffset:]); flags != flagFreelist {
			panic(fmt.Errorf("freelist page %d flags = %x", id, flags))
		}
		count := int(binary.LittleEndian.Uint16(page[headerSlotCountOffset:]))
		if count == 0 {
			panic(fmt.Errorf("freelist page %d is empty", id))
		}
		pages++
		records += count
		id = binary.LittleEndian.Uint64(page[headerLeftmostOffset:])
	}
	return pages, records
}

func removeGeneratedFiles(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + ".readers")
	_ = os.Remove(path + ".writer")
}

func metaChecksum(data []byte) uint32 {
	checksum := crc32.NewIEEE()
	_, _ = checksum.Write(data[:metaChecksumOff])
	_, _ = checksum.Write(data[metaChecksumOff+4 : pageSize])
	return checksum.Sum32()
}

func pageChecksum(data []byte) uint32 {
	checksum := crc32.NewIEEE()
	_, _ = checksum.Write(data[:headerChecksumOffset])
	_, _ = checksum.Write(data[headerChecksumOffset+4 : pageSize])
	return checksum.Sum32()
}
