package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func main() {
	path := filepath.Join(os.TempDir(), "cow-btree-mdb-kernel.db")
	_ = os.Remove(path)
	_ = os.Remove(path + ".readers")
	_ = os.Remove(path + ".writer")

	writer, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{
		Degree:            2,
		MaxPages:          64,
		PageCacheCapacity: 16,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer writer.Close()

	for i := 0; i < 18; i++ {
		writer.Put(fmt.Sprintf("uid=%02d", i), []byte(fmt.Sprintf("entry-%02d", i)))
	}
	if err := writer.Sync(); err != nil {
		log.Fatal(err)
	}

	reader, err := pagebtree.OpenMmapReadOnly(path)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	writer.Put("uid=03", []byte("entry-03-updated"))
	writer.Delete("uid=07")
	if err := writer.Check(); err != nil {
		log.Fatal(err)
	}

	readerValue, readerOK := reader.Get("uid=03")
	writerValue, writerOK := writer.Get("uid=03")
	deletedValue, deletedOK := reader.Get("uid=07")
	readerStats, err := writer.MmapReaderStats()
	if err != nil {
		log.Fatal(err)
	}

	profile := writer.MDBKernelProfile()
	fmt.Printf("db: %s\n", path)
	fmt.Printf("profile: %+v\n", profile)
	fmt.Printf("reader table: %+v\n", readerStats)
	fmt.Printf("read-only uid=03: %q ok=%v\n", readerValue, readerOK)
	fmt.Printf("writer uid=03:    %q ok=%v\n", writerValue, writerOK)
	fmt.Printf("read-only uid=07 before writer sync: %q ok=%v\n", deletedValue, deletedOK)
}
