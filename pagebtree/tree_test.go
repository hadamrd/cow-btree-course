package pagebtree

import (
	"fmt"
	"reflect"
	"testing"
)

func TestPutAndGetKeysFromPageBackedTree(t *testing.T) {
	tree := New(2)

	for i := 0; i < 80; i++ {
		old, replaced := tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
		if replaced {
			t.Fatalf("Put reported replacement for new key %02d with old value %q", i, old)
		}
	}

	if tree.Len() != 80 {
		t.Fatalf("Len() = %d, want 80", tree.Len())
	}

	for i := 0; i < 80; i++ {
		got, ok := tree.Get(fmt.Sprintf("key-%02d", i))
		if !ok {
			t.Fatalf("Get missed key-%02d", i)
		}
		if want := fmt.Sprintf("value-%02d", i); string(got) != want {
			t.Fatalf("Get(key-%02d) = %q, want %q", i, got, want)
		}
	}
}

func TestPutReplacesExistingKey(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))

	old, replaced := tree.Put("alpha", []byte("two"))
	if !replaced {
		t.Fatalf("Put did not report replacement")
	}
	if string(old) != "one" {
		t.Fatalf("old value = %q, want %q", old, "one")
	}
	if tree.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", tree.Len())
	}

	got, ok := tree.Get("alpha")
	if !ok || string(got) != "two" {
		t.Fatalf("Get(alpha) = %q, %v; want two, true", got, ok)
	}
}

func TestRangeReturnsSortedKeysFromPages(t *testing.T) {
	tree := New(2)
	for _, key := range []string{"delta", "alpha", "charlie", "bravo"} {
		tree.Put(key, []byte(key+"-value"))
	}

	var got []string
	tree.Range(func(key string, value []byte) bool {
		got = append(got, key)
		return true
	})

	want := []string{"alpha", "bravo", "charlie", "delta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Range keys = %v, want %v", got, want)
	}
}

func TestSnapshotKeepsOldRootAfterCopyOnWritePuts(t *testing.T) {
	tree := New(2)
	for i := 0; i < 30; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("before-%02d", i)))
	}
	snapshot := tree.Snapshot()
	oldStats := snapshot.Stats()

	for i := 10; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("after-%02d", i)))
	}

	if snapshot.Stats().Root != oldStats.Root {
		t.Fatalf("snapshot root changed from %d to %d", oldStats.Root, snapshot.Stats().Root)
	}
	if tree.Stats().Root == snapshot.Stats().Root {
		t.Fatalf("current root reused snapshot root %d after writes", tree.Stats().Root)
	}

	for i := 0; i < 30; i++ {
		got, ok := snapshot.Get(fmt.Sprintf("key-%02d", i))
		if !ok {
			t.Fatalf("snapshot missed key-%02d", i)
		}
		if want := fmt.Sprintf("before-%02d", i); string(got) != want {
			t.Fatalf("snapshot Get(key-%02d) = %q, want %q", i, got, want)
		}
	}
	for i := 30; i < 40; i++ {
		if got, ok := snapshot.Get(fmt.Sprintf("key-%02d", i)); ok {
			t.Fatalf("snapshot saw future key-%02d with value %q", i, got)
		}
	}
}

func TestCopyOnWriteSharesUntouchedPageSubtrees(t *testing.T) {
	tree := New(2)
	for i := 0; i < 60; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	before := tree.Snapshot()

	tree.Put("key-99", []byte("new-right-edge"))

	if before.root == tree.root {
		t.Fatalf("root page id was reused after write; want a copied root page")
	}
	if !sharesAtLeastOnePage(before.root, tree.root, tree.pages) {
		t.Fatalf("write copied every reachable page; want untouched pages to remain shared")
	}
}

func TestStatsExposePageBackedStructure(t *testing.T) {
	tree := New(2)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte("value"))
	}

	stats := tree.Stats()
	if stats.Len != 40 {
		t.Fatalf("Len = %d, want 40", stats.Len)
	}
	if stats.Height < 2 {
		t.Fatalf("Height = %d, want at least 2", stats.Height)
	}
	if stats.Pages <= 1 {
		t.Fatalf("Pages = %d, want more than 1", stats.Pages)
	}
	if stats.Root == 0 {
		t.Fatalf("Root = 0, want allocated page id")
	}
	if stats.Revision != 40 {
		t.Fatalf("Revision = %d, want 40", stats.Revision)
	}
}

func sharesAtLeastOnePage(leftRoot, rightRoot PageID, pages map[PageID]*page) bool {
	seen := map[PageID]bool{}
	collectPageIDs(leftRoot, pages, seen)
	return hasSeenPageID(rightRoot, pages, seen)
}

func collectPageIDs(root PageID, pages map[PageID]*page, seen map[PageID]bool) {
	if root == 0 || seen[root] {
		return
	}
	seen[root] = true
	for _, child := range pages[root].children {
		collectPageIDs(child, pages, seen)
	}
}

func hasSeenPageID(root PageID, pages map[PageID]*page, seen map[PageID]bool) bool {
	if root == 0 {
		return false
	}
	if seen[root] {
		return true
	}
	for _, child := range pages[root].children {
		if hasSeenPageID(child, pages, seen) {
			return true
		}
	}
	return false
}
