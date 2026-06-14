//go:build ignore

package main

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"runtime"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

const (
	pageSize         = 4096
	metaMagic        = "COWBTREE"
	metaPageCount    = 2
	metaMagicOffset  = 0
	metaVersionOff   = 8
	metaChecksumOff  = 72
	metaFreeCountOff = 80
)

func main() {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate generator source")
	}
	dir := filepath.Dir(source)
	out := filepath.Join(dir, "mmap-v1-inline-freelist.db")
	tmp := out + ".tmp"
	removeGeneratedFiles(tmp)

	tree, err := pagebtree.OpenMmap(tmp, pagebtree.MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		panic(fmt.Errorf("create mmap fixture: %w", err))
	}
	for i := 0; i < 48; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	for i := 0; i < 18; i++ {
		key := fmt.Sprintf("key-%02d", i)
		if _, deleted := tree.Delete(key); !deleted {
			panic(fmt.Errorf("delete %s: key not found", key))
		}
	}
	report := tree.Audit()
	if !report.Valid() {
		panic(fmt.Errorf("audit generated fixture: %w", report.Error))
	}
	if len(report.FreePageIDs) == 0 {
		panic("generated fixture has no reusable pages")
	}
	if err := tree.Close(); err != nil {
		panic(fmt.Errorf("close mmap fixture: %w", err))
	}

	data, err := os.ReadFile(tmp)
	if err != nil {
		panic(fmt.Errorf("read mmap fixture: %w", err))
	}
	sawInlineFreelist := false
	for index := 0; index < metaPageCount; index++ {
		page := data[index*pageSize : (index+1)*pageSize]
		if string(page[metaMagicOffset:metaMagicOffset+len(metaMagic)]) != metaMagic {
			continue
		}
		freeCount := binary.LittleEndian.Uint64(page[metaFreeCountOff:])
		sawInlineFreelist = sawInlineFreelist || freeCount > 0
		binary.LittleEndian.PutUint64(page[metaVersionOff:], 1)
		binary.LittleEndian.PutUint32(page[metaChecksumOff:], metaChecksum(page))
	}
	if !sawInlineFreelist {
		panic("generated fixture metadata has no inline freelist")
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		panic(fmt.Errorf("write mmap fixture: %w", err))
	}
	removeGeneratedFiles(tmp)
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
