package pagebtree_test

import (
	"bytes"
	"errors"
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

func ExampleTree_PutBytes() {
	tree := pagebtree.New(2)
	tree.PutBytes([]byte{0x00, 0xff}, []byte("high"))
	tree.PutBytes([]byte{0x00, 0x10}, []byte("low"))

	tree.RangeBytes(func(key []byte, value []byte) bool {
		fmt.Printf("%x %s\n", key, value)
		return true
	})
	// Output:
	// 0010 low
	// 00ff high
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

func ExampleTree_Batch() {
	tree := pagebtree.New(2)
	tree.Put("alpha", []byte("one"))
	before := tree.Revision()

	batch := tree.Batch()
	batch.Put("alpha", []byte("two"))
	batch.Put("bravo", []byte("three"))
	batch.Commit()

	alpha, _ := tree.Get("alpha")
	bravo, _ := tree.Get("bravo")
	fmt.Println(string(alpha), string(bravo))
	fmt.Println(tree.Revision() - before)
	// Output:
	// two three
	// 1
}

func ExampleWriteBatch_DeleteRange() {
	tree := pagebtree.New(2)
	for _, key := range []string{"alpha", "bravo", "charlie", "delta"} {
		tree.Put(key, []byte(key+"-value"))
	}

	batch := tree.Batch()
	batch.DeleteRange("bravo", "delta")
	batch.Commit()

	tree.Range(func(key string, value []byte) bool {
		fmt.Println(key, string(value))
		return true
	})
	// Output:
	// alpha alpha-value
	// delta delta-value
}

func ExampleTree_BeginReadWrite() {
	tree := pagebtree.New(2)
	tree.Put("alpha", []byte("one"))
	tree.Put("bravo", []byte("two"))

	tx := tree.BeginReadWrite()
	tx.Put("alpha", []byte("updated"))
	tx.Delete("bravo")
	tx.Put("charlie", []byte("three"))

	value, ok := tx.Get("alpha")
	fmt.Println(ok, string(value))
	_, visibleBeforeCommit := tree.Get("charlie")
	fmt.Println(visibleBeforeCommit)

	tx.Commit()
	tree.RangeBetween("alpha", "delta", func(key string, value []byte) bool {
		fmt.Println(key, string(value))
		return true
	})
	// Output:
	// true updated
	// false
	// alpha updated
	// charlie three
}

func ExampleReadWriteTx_CursorBetween() {
	tree := pagebtree.New(2)
	for _, key := range []string{"alpha", "bravo", "charlie", "delta"} {
		tree.Put(key, []byte(key+"-value"))
	}

	tx := tree.BeginReadWrite()
	cursor := tx.CursorBetween("bravo", "delta")
	old, deleted := cursor.Delete()
	cursor.Close()

	_, visibleBeforeCommit := tree.Get("bravo")
	tx.Commit()
	_, visibleAfterCommit := tree.Get("bravo")

	fmt.Println(deleted, string(old))
	fmt.Println(visibleBeforeCommit)
	fmt.Println(visibleAfterCommit)
	// Output:
	// true bravo-value
	// true
	// false
}

func ExampleReadWriteTx_CommitDetailed_conflict() {
	tree := pagebtree.New(2)
	tree.Put("alpha", []byte("one"))

	tx := tree.BeginReadWrite()
	tx.Put("bravo", []byte("two"))
	tree.Put("alpha", []byte("outside"))

	_, err := tx.CommitDetailed()
	_, visible := tree.Get("bravo")
	fmt.Println(errors.Is(err, pagebtree.ErrTxConflict))
	fmt.Println(visible)
	// Output:
	// true
	// false
}

func ExampleWriteBatch_CommitDetailed() {
	tree := pagebtree.New(2)
	tree.Put("alpha", []byte("one"))

	batch := tree.Batch()
	batch.Put("alpha", []byte("two"))
	batch.Delete("missing")
	result, _ := batch.CommitDetailed()

	fmt.Println(result.Changed)
	for _, op := range result.Operations {
		fmt.Println(op.Kind, op.Key, op.Existed, string(op.OldValue), op.Changed)
	}
	// Output:
	// true
	// put alpha true one true
	// delete missing false  false
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

func ExampleTree_CursorBetween() {
	tree := pagebtree.New(2)
	for _, key := range []string{"alpha", "bravo", "charlie", "delta"} {
		tree.Put(key, []byte(key+"-value"))
	}

	cursor := tree.CursorBetween("bravo", "delta")
	defer cursor.Close()

	for cursor.Valid() {
		fmt.Println(cursor.Key(), string(cursor.Value()))
		if !cursor.Next() {
			break
		}
	}
	// Output:
	// bravo bravo-value
	// charlie charlie-value
}

func ExampleCursor_Prev() {
	tree := pagebtree.New(2)
	for _, key := range []string{"alpha", "bravo", "charlie", "delta"} {
		tree.Put(key, []byte(key+"-value"))
	}

	cursor := tree.CursorBetween("bravo", "delta")
	defer cursor.Close()

	for ok := cursor.Last(); ok; ok = cursor.Prev() {
		fmt.Println(cursor.Key(), string(cursor.Value()))
	}
	// Output:
	// charlie charlie-value
	// bravo bravo-value
}

func ExampleCursor_Delete() {
	tree := pagebtree.New(2)
	for _, key := range []string{"alpha", "bravo", "charlie"} {
		tree.Put(key, []byte(key+"-value"))
	}

	cursor := tree.Cursor()
	defer cursor.Close()

	cursor.Seek("bravo")
	old, deleted := cursor.Delete()
	fmt.Println(string(old), deleted)
	_, ok := tree.Get("bravo")
	fmt.Println(ok)
	fmt.Println(cursor.Key(), string(cursor.Value()))
	// Output:
	// bravo-value true
	// false
	// bravo bravo-value
}

func ExampleMmapTraceJSONLExporter() {
	var out bytes.Buffer
	exporter := pagebtree.NewMmapTraceJSONLExporter(&out)
	hook := exporter.Hook()

	hook(pagebtree.MmapTraceEvent{
		Kind:          pagebtree.MmapTraceSyncDataRange,
		Revision:      3,
		StartPage:     5,
		EndPage:       7,
		DurationNanos: 99,
		MetadataSlot:  -1,
	})
	if err := exporter.Err(); err != nil {
		panic(err)
	}

	fmt.Print(out.String())
	// Output:
	// {"kind":"mmap-sync-data-range","revision":3,"start_page":5,"end_page":7,"duration_nanos":99}
}
