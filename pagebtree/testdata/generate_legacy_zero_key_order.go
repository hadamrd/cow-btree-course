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
	pageSize        = 4096
	metaMagic       = "COWBTREE"
	metaPageCount   = 2
	metaMagicOffset = 0
	metaChecksumOff = 72
	metaKeyOrderOff = 76
)

func main() {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate generator source")
	}
	dir := filepath.Dir(source)
	out := filepath.Join(dir, "mmap-v2-legacy-zero-key-order.db")
	tmp := out + ".tmp"
	removeGeneratedFiles(tmp)

	tree, err := pagebtree.OpenMmap(tmp, pagebtree.MmapOptions{Degree: 2, MaxPages: 16})
	if err != nil {
		panic(fmt.Errorf("create mmap fixture: %w", err))
	}
	tree.PutBytes([]byte{0x00, 0xff}, []byte("high"))
	tree.PutBytes([]byte{0x00, 0x10}, []byte("low"))
	tree.PutBytes([]byte{0x01}, []byte("one"))
	if err := tree.Close(); err != nil {
		panic(fmt.Errorf("close mmap fixture: %w", err))
	}

	data, err := os.ReadFile(tmp)
	if err != nil {
		panic(fmt.Errorf("read mmap fixture: %w", err))
	}
	for index := 0; index < metaPageCount; index++ {
		page := data[index*pageSize : (index+1)*pageSize]
		if string(page[metaMagicOffset:metaMagicOffset+len(metaMagic)]) != metaMagic {
			continue
		}
		binary.LittleEndian.PutUint32(page[metaKeyOrderOff:], 0)
		binary.LittleEndian.PutUint32(page[metaChecksumOff:], metaChecksum(page))
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
