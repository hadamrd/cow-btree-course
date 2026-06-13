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

func ExampleTree_RangeFrom() {
	tree := pagebtree.New(2)
	for _, key := range []string{"alpha", "bravo", "charlie", "delta"} {
		tree.Put(key, []byte(key+"-value"))
	}

	tree.RangeFrom("charlie", func(key string, value []byte) bool {
		fmt.Println(key, string(value))
		return true
	})
	// Output:
	// charlie charlie-value
	// delta delta-value
}

func ExampleTree_RangeBetween() {
	tree := pagebtree.New(2)
	for _, key := range []string{"alpha", "bravo", "charlie", "delta"} {
		tree.Put(key, []byte(key+"-value"))
	}

	tree.RangeBetween("bravo", "delta", func(key string, value []byte) bool {
		fmt.Println(key, string(value))
		return true
	})
	// Output:
	// bravo bravo-value
	// charlie charlie-value
}

func ExampleTree_Cursor() {
	tree := pagebtree.New(2)
	for _, key := range []string{"alpha", "bravo", "charlie", "delta"} {
		tree.Put(key, []byte(key+"-value"))
	}

	cursor := tree.Cursor()
	defer cursor.Close()

	for ok := cursor.Seek("bravo"); ok; ok = cursor.Next() {
		fmt.Println(cursor.Key(), string(cursor.Value()))
	}
	// Output:
	// bravo bravo-value
	// charlie charlie-value
	// delta delta-value
}
