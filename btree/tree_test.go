package btree

import (
	"cmp"
	"fmt"
	"reflect"
	"testing"
)

func TestSetAndGetManyKeys(t *testing.T) {
	tree := New[int, string](2)

	for i := 0; i < 50; i++ {
		old, replaced := tree.Set(i, fmt.Sprintf("value-%02d", i))
		if replaced {
			t.Fatalf("Set(%d) reported replacement with old value %q", i, old)
		}
	}

	if tree.Len() != 50 {
		t.Fatalf("Len() = %d, want 50", tree.Len())
	}

	for i := 0; i < 50; i++ {
		got, ok := tree.Get(i)
		if !ok {
			t.Fatalf("Get(%d) missed inserted key", i)
		}
		if want := fmt.Sprintf("value-%02d", i); got != want {
			t.Fatalf("Get(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestSetReplacesExistingKeyWithoutGrowingTree(t *testing.T) {
	tree := New[string, int](3)
	tree.Set("alpha", 1)

	old, replaced := tree.Set("alpha", 2)
	if !replaced {
		t.Fatalf("Set existing key did not report replacement")
	}
	if old != 1 {
		t.Fatalf("old value = %d, want 1", old)
	}
	if tree.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", tree.Len())
	}
	got, ok := tree.Get("alpha")
	if !ok || got != 2 {
		t.Fatalf("Get(alpha) = %d, %v; want 2, true", got, ok)
	}
}

func TestRangeVisitsKeysInSortedOrder(t *testing.T) {
	tree := New[int, string](2)
	for _, key := range []int{9, 1, 7, 3, 5, 2, 8, 6, 4} {
		tree.Set(key, fmt.Sprintf("%d", key))
	}

	var got []int
	tree.Range(func(key int, value string) bool {
		got = append(got, key)
		return true
	})

	want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Range keys = %v, want %v", got, want)
	}
}

func TestRangeStopsWhenVisitorReturnsFalse(t *testing.T) {
	tree := New[int, string](2)
	for i := 1; i <= 10; i++ {
		tree.Set(i, fmt.Sprintf("%d", i))
	}

	var got []int
	tree.Range(func(key int, value string) bool {
		got = append(got, key)
		return len(got) < 3
	})

	want := []int{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Range keys = %v, want %v", got, want)
	}
}

func TestSnapshotSeesTheOldRootAfterLaterWrites(t *testing.T) {
	tree := New[int, string](2)
	for i := 0; i < 20; i++ {
		tree.Set(i, fmt.Sprintf("before-%02d", i))
	}
	snap := tree.Snapshot()

	for i := 10; i < 30; i++ {
		tree.Set(i, fmt.Sprintf("after-%02d", i))
	}

	for i := 0; i < 20; i++ {
		got, ok := snap.Get(i)
		if !ok {
			t.Fatalf("snapshot missed key %d", i)
		}
		if want := fmt.Sprintf("before-%02d", i); got != want {
			t.Fatalf("snapshot Get(%d) = %q, want %q", i, got, want)
		}
	}

	for i := 20; i < 30; i++ {
		if got, ok := snap.Get(i); ok {
			t.Fatalf("snapshot unexpectedly saw key %d with value %q", i, got)
		}
	}
}

func TestSnapshotRangeUsesTheCapturedVersion(t *testing.T) {
	tree := New[int, string](3)
	for i := 1; i <= 5; i++ {
		tree.Set(i, fmt.Sprintf("v%d", i))
	}
	snap := tree.Snapshot()

	tree.Set(0, "new-left")
	tree.Set(6, "new-right")

	var got []int
	snap.Range(func(key int, value string) bool {
		got = append(got, key)
		return true
	})

	want := []int{1, 2, 3, 4, 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot Range keys = %v, want %v", got, want)
	}
}

func TestStatsExposeStructureForLearning(t *testing.T) {
	tree := New[int, string](2)
	for i := 0; i < 40; i++ {
		tree.Set(i, fmt.Sprintf("%d", i))
	}

	stats := tree.Stats()
	if stats.Len != 40 {
		t.Fatalf("stats Len = %d, want 40", stats.Len)
	}
	if stats.Height < 2 {
		t.Fatalf("stats Height = %d, want at least 2 after many inserts", stats.Height)
	}
	if stats.Nodes <= 1 {
		t.Fatalf("stats Nodes = %d, want more than one node", stats.Nodes)
	}
	if stats.Revision != 40 {
		t.Fatalf("stats Revision = %d, want 40", stats.Revision)
	}
}

func TestCopyOnWriteKeepsUntouchedSubtreesShared(t *testing.T) {
	tree := New[int, string](2)
	for i := 0; i < 30; i++ {
		tree.Set(i, fmt.Sprintf("%d", i))
	}
	before := tree.Snapshot()

	tree.Set(100, "new-right-edge")

	if before.root == tree.root {
		t.Fatalf("root pointer was reused after write; want a copied root")
	}
	if !sharesAtLeastOneNode(before.root, tree.root) {
		t.Fatalf("write copied every node; want untouched subtrees to remain shared")
	}
}

func sharesAtLeastOneNode[K cmp.Ordered, V any](left, right *node[K, V]) bool {
	seen := map[*node[K, V]]bool{}
	collectNodes(left, seen)
	return hasSeenNode(right, seen)
}

func collectNodes[K cmp.Ordered, V any](n *node[K, V], seen map[*node[K, V]]bool) {
	if n == nil {
		return
	}
	seen[n] = true
	for _, child := range n.children {
		collectNodes(child, seen)
	}
}

func hasSeenNode[K cmp.Ordered, V any](n *node[K, V], seen map[*node[K, V]]bool) bool {
	if n == nil {
		return false
	}
	if seen[n] {
		return true
	}
	for _, child := range n.children {
		if hasSeenNode(child, seen) {
			return true
		}
	}
	return false
}
