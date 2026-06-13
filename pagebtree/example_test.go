package pagebtree_test

import (
	"fmt"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func ExampleTree_Put() {
	tree := pagebtree.New(2)
	tree.Put("hello", []byte("world"))

	value, ok := tree.Get("hello")
	fmt.Println(ok, string(value))
	// Output:
	// true world
}

func ExampleTree_Snapshot() {
	tree := pagebtree.New(2)
	tree.Put("color", []byte("blue"))
	snapshot := tree.Snapshot()

	tree.Put("color", []byte("green"))

	oldValue, _ := snapshot.Get("color")
	newValue, _ := tree.Get("color")

	fmt.Println(string(oldValue))
	fmt.Println(string(newValue))
	// Output:
	// blue
	// green
}
