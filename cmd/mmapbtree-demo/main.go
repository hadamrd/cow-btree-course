package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func main() {
	path := filepath.Join(os.TempDir(), "cow-btree-course-demo.db")
	_ = os.Remove(path)

	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{
		Degree:   2,
		MaxPages: 128,
	})
	if err != nil {
		log.Fatal(err)
	}

	for i := 0; i < 24; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if _, deleted := tree.Delete("key-05"); !deleted {
		log.Fatal("key-05 was not deleted before close")
	}
	if err := tree.Close(); err != nil {
		log.Fatal(err)
	}

	reopened, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Advise(pagebtree.MmapAccessRandom); err != nil {
		log.Fatal(err)
	}

	value, ok := reopened.Get("key-17")
	if !ok {
		log.Fatal("key-17 not found after reopen")
	}
	if _, ok := reopened.Get("key-05"); ok {
		log.Fatal("key-05 was found after delete and reopen")
	}
	beforeCompact, err := os.Stat(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := reopened.Compact(); err != nil {
		log.Fatal(err)
	}
	afterCompact, err := os.Stat(path)
	if err != nil {
		log.Fatal(err)
	}
	cacheStats, err := reopened.MmapCacheStats()
	if err != nil {
		log.Fatal(err)
	}
	if err := reopened.DropMmapCache(); err != nil {
		log.Fatal(err)
	}
	afterDropStats, err := reopened.MmapCacheStats()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("mmap file: %s\n", path)
	fmt.Printf("file size before compact: %d bytes\n", beforeCompact.Size())
	fmt.Printf("file size after compact:  %d bytes\n", afterCompact.Size())
	fmt.Printf("key-17: %s\n", value)
	fmt.Println("key-05: deleted")
	fmt.Printf("stats: %+v\n", reopened.Stats())
	fmt.Printf("cache before drop: %+v\n", cacheStats)
	fmt.Printf("cache after drop:  %+v\n", afterDropStats)
}
