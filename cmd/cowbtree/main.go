package main

import (
	"fmt"

	"github.com/hadamrd/cow-btree-course/btree"
)

func main() {
	tree := btree.New[int, string](2)
	for _, key := range []int{8, 3, 10, 1, 6, 14, 4, 7, 13} {
		tree.Set(key, fmt.Sprintf("value-%02d", key))
	}

	before := tree.Snapshot()
	tree.Set(6, "updated")
	tree.Set(20, "new")

	fmt.Println("current tree:")
	tree.Range(func(key int, value string) bool {
		fmt.Printf("  %02d -> %s\n", key, value)
		return true
	})

	old, _ := before.Get(6)
	now, _ := tree.Get(6)

	fmt.Println()
	fmt.Printf("snapshot revision: %d, key 6 = %q\n", before.Revision(), old)
	fmt.Printf("current  revision: %d, key 6 = %q\n", tree.Revision(), now)
	fmt.Printf("stats: %+v\n", tree.Stats())
}
