package main

import (
	"fmt"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func main() {
	tree := pagebtree.New(2)

	for _, key := range []string{"k08", "k03", "k10", "k01", "k06", "k14", "k04", "k07", "k13"} {
		tree.Put(key, []byte("value-"+key))
	}

	snapshot := tree.Snapshot()
	tree.Put("k06", []byte("updated-k06"))
	tree.Put("k20", []byte("value-k20"))

	fmt.Println("page-backed current tree:")
	tree.Range(func(key string, value []byte) bool {
		fmt.Printf("  %s -> %s\n", key, value)
		return true
	})

	oldValue, _ := snapshot.Get("k06")
	newValue, _ := tree.Get("k06")

	fmt.Println()
	fmt.Printf("snapshot root page: %d, k06 = %q\n", snapshot.Stats().Root, oldValue)
	fmt.Printf("current  root page: %d, k06 = %q\n", tree.Stats().Root, newValue)
	fmt.Printf("stats: %+v\n", tree.Stats())
}
