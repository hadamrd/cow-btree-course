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
	if stats.Keys != 40 {
		t.Fatalf("Keys = %d, want 40 leaf records", stats.Keys)
	}
	if stats.Separators == 0 {
		t.Fatalf("Separators = 0, want branch separators after splits")
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

func TestLeafPagesUseSlottedLayout(t *testing.T) {
	tree := New(4)
	tree.Put("alpha", []byte("one"))
	tree.Put("bravo", []byte("two"))
	tree.Put("charlie", []byte("three"))

	root := tree.pages[tree.root]
	if !root.isLeaf() {
		t.Fatalf("root should still be a leaf for three keys")
	}
	if got := root.slotCount(); got != 3 {
		t.Fatalf("slot count = %d, want 3", got)
	}

	first := root.readSlot(0)
	second := root.readSlot(1)
	third := root.readSlot(2)
	if int(first.offset) < pageHeaderSize+3*slotSize {
		t.Fatalf("first cell offset %d overlaps header+slots", first.offset)
	}
	if second.offset >= first.offset {
		t.Fatalf("cells should grow left from page end: second offset %d, first offset %d", second.offset, first.offset)
	}
	if third.offset >= second.offset {
		t.Fatalf("cells should keep growing left: third offset %d, second offset %d", third.offset, second.offset)
	}
	if root.freeUpper() != third.offset {
		t.Fatalf("freeUpper = %d, want newest cell offset %d", root.freeUpper(), third.offset)
	}

	key, value := root.readCell(0)
	if key != "alpha" || string(value) != "one" {
		t.Fatalf("slot 0 cell = %q/%q, want alpha/one", key, value)
	}
}

func TestBranchPagesUseSlottedSeparatorsAndChildPageIDs(t *testing.T) {
	tree := New(2)
	for i := 0; i < 20; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	root := tree.pages[tree.root]
	if !root.isBranch() {
		t.Fatalf("root should be a branch after enough inserts")
	}
	if root.leftmostChild() == 0 {
		t.Fatalf("branch root leftmost child page id is zero")
	}
	if root.slotCount() == 0 {
		t.Fatalf("branch root has no separator slots")
	}

	key, encodedChild := root.readCell(0)
	if key == "" {
		t.Fatalf("branch separator key is empty")
	}
	if got := decodePageID(encodedChild); got == 0 {
		t.Fatalf("branch separator child page id decoded to zero")
	}
}

func TestLeafSlotSearchReadsOnlySelectedCellValue(t *testing.T) {
	leaf := newPage(1, flagLeaf)
	mustWriteLeafEntries(leaf, []leafEntry{
		{key: "alpha", value: []byte("one")},
		{key: "bravo", value: []byte("two")},
		{key: "charlie", value: []byte("three")},
	})

	value, found := leaf.searchLeafValue("bravo")
	if !found {
		t.Fatalf("searchLeafValue missed existing key")
	}
	if string(value) != "two" {
		t.Fatalf("searchLeafValue returned %q, want two", value)
	}

	value[0] = 'X'
	again, found := leaf.searchLeafValue("bravo")
	if !found || string(again) != "two" {
		t.Fatalf("searchLeafValue leaked page memory through returned slice: %q, %v", again, found)
	}

	if _, found := leaf.searchLeafValue("beta"); found {
		t.Fatalf("searchLeafValue found absent key")
	}
}

func TestBranchSlotSearchChoosesChildPageID(t *testing.T) {
	branch := newPage(9, flagBranch)
	mustWriteBranchParts(branch, []string{"bravo", "delta"}, []PageID{10, 20, 30})

	cases := []struct {
		key  string
		want PageID
	}{
		{key: "alpha", want: 10},
		{key: "bravo", want: 20},
		{key: "charlie", want: 20},
		{key: "delta", want: 30},
		{key: "echo", want: 30},
	}
	for _, tc := range cases {
		if got := branch.searchBranchChild(tc.key); got != tc.want {
			t.Fatalf("searchBranchChild(%q) = %d, want %d", tc.key, got, tc.want)
		}
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
	for _, child := range pages[root].childIDs() {
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
	for _, child := range pages[root].childIDs() {
		if hasSeenPageID(child, pages, seen) {
			return true
		}
	}
	return false
}
