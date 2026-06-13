package btree_test

import (
	"fmt"

	"github.com/hadamrd/cow-btree-course/btree"
)

func ExampleTree_Snapshot() {
	tree := btree.New[int, string](2)
	tree.Set(1, "one")
	snapshot := tree.Snapshot()

	tree.Set(1, "ONE")

	oldValue, _ := snapshot.Get(1)
	newValue, _ := tree.Get(1)

	fmt.Println(oldValue)
	fmt.Println(newValue)
	// Output:
	// one
	// ONE
}

func ExampleTree_Range() {
	tree := btree.New[int, string](2)
	for _, key := range []int{3, 1, 2} {
		tree.Set(key, fmt.Sprintf("v%d", key))
	}

	tree.Range(func(key int, value string) bool {
		fmt.Printf("%d=%s\n", key, value)
		return true
	})
	// Output:
	// 1=v1
	// 2=v2
	// 3=v3
}
