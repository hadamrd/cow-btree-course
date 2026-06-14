package pagebtree

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
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

func TestMDBKernelProfileDescribesMemoryCoreWithoutMmapMechanics(t *testing.T) {
	tree := NewWithOptions(2, Options{PageCacheCapacity: 4})
	for i := 0; i < 16; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	for i := 0; i < 2; i++ {
		got, ok := tree.Get("key-11")
		if !ok || string(got) != "value-11" {
			t.Fatalf("Get(key-11) = %q, %v; want value-11, true", got, ok)
		}
	}

	profile := tree.MDBKernelProfile()
	if profile.Storage != "memory" {
		t.Fatalf("Storage = %q, want memory", profile.Storage)
	}
	if profile.KeyOrder != KeyOrderBytewise {
		t.Fatalf("KeyOrder = %d, want bytewise", profile.KeyOrder)
	}
	if !profile.SlottedPages || !profile.BPlusTreePages || !profile.CopyOnWrite {
		t.Fatalf("page flags = slotted:%v bplus:%v cow:%v; want all true", profile.SlottedPages, profile.BPlusTreePages, profile.CopyOnWrite)
	}
	if profile.DualCheckedMetaPages || profile.SerializedWriter || profile.ReaderTable {
		t.Fatalf("mmap flags = meta:%v writer:%v readers:%v; want all false for memory tree", profile.DualCheckedMetaPages, profile.SerializedWriter, profile.ReaderTable)
	}
	if !profile.ReaderPinnedRecycling || profile.PersistedReclaimRecords {
		t.Fatalf("reclaim flags = reader-pinned:%v persisted:%v; want in-memory pinning without persisted reclaim records", profile.ReaderPinnedRecycling, profile.PersistedReclaimRecords)
	}
	if profile.KernelPageCache || profile.RawHeapPageCache {
		t.Fatalf("cache flags = kernel:%v raw-heap:%v; want no mmap kernel cache and no duplicate raw heap cache", profile.KernelPageCache, profile.RawHeapPageCache)
	}
	if !profile.DerivedBranchRoutingCache || profile.DerivedBranchRoutingCacheCapacity != 4 {
		t.Fatalf("derived cache = enabled:%v capacity:%d; want enabled capacity 4", profile.DerivedBranchRoutingCache, profile.DerivedBranchRoutingCacheCapacity)
	}
	if profile.DerivedBranchRoutingCacheHits == 0 || profile.DerivedBranchRoutingCacheMisses == 0 {
		t.Fatalf("derived cache hits/misses = %d/%d; want both nonzero", profile.DerivedBranchRoutingCacheHits, profile.DerivedBranchRoutingCacheMisses)
	}
	if profile.MaxMappedPages != 0 || profile.DirtyPages != 0 {
		t.Fatalf("mapped fields = max:%d dirty:%d; want zero for memory tree", profile.MaxMappedPages, profile.DirtyPages)
	}
}

func TestStatsReportsReachablePageByteFill(t *testing.T) {
	tree := New(2)
	for i := range 18 {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	snapshot := tree.Snapshot()
	defer snapshot.Close()

	tree.Put("large", bytes.Repeat([]byte("x"), PageSize*2+77))
	stats := tree.Stats()
	if stats.LeafPages == 0 {
		t.Fatalf("LeafPages = 0, want reachable leaves")
	}
	if stats.BranchPages == 0 {
		t.Fatalf("BranchPages = 0, want reachable branches")
	}
	if stats.OverflowPages == 0 {
		t.Fatalf("OverflowPages = 0, want large value overflow pages")
	}
	if stats.PageBytesCapacity != stats.Pages*PageSize {
		t.Fatalf("PageBytesCapacity = %d, want Pages*PageSize %d", stats.PageBytesCapacity, stats.Pages*PageSize)
	}
	if stats.PageBytesUsed <= 0 || stats.PageBytesUsed > stats.PageBytesCapacity {
		t.Fatalf("PageBytesUsed = %d outside (0,%d]", stats.PageBytesUsed, stats.PageBytesCapacity)
	}
	if stats.PageBytesFree != stats.PageBytesCapacity-stats.PageBytesUsed {
		t.Fatalf("PageBytesFree = %d, want capacity-used %d", stats.PageBytesFree, stats.PageBytesCapacity-stats.PageBytesUsed)
	}
	if stats.LeafBytesUsed == 0 || stats.BranchBytesUsed == 0 || stats.OverflowBytesUsed == 0 {
		t.Fatalf("byte buckets leaf=%d branch=%d overflow=%d, want all nonzero", stats.LeafBytesUsed, stats.BranchBytesUsed, stats.OverflowBytesUsed)
	}

	snapshotStats := snapshot.Stats()
	if snapshotStats.OverflowPages != 0 {
		t.Fatalf("snapshot OverflowPages = %d, want old root without large value overflow", snapshotStats.OverflowPages)
	}
	if snapshotStats.PageBytesCapacity != snapshotStats.Pages*PageSize {
		t.Fatalf("snapshot PageBytesCapacity = %d, want Pages*PageSize %d", snapshotStats.PageBytesCapacity, snapshotStats.Pages*PageSize)
	}
}

func TestLeafSplitBalancesEncodedBytes(t *testing.T) {
	tree := New(3)
	large := bytes.Repeat([]byte("l"), 1500)
	small := []byte("s")
	for i := 0; i < 6; i++ {
		value := small
		if i < 2 {
			value = large
		}
		tree.Put(fmt.Sprintf("key-%02d", i), value)
	}

	root := tree.pages[tree.root]
	if !root.isBranch() {
		t.Fatalf("root is leaf after overflow insert; want branch")
	}
	_, children := root.branchParts()
	if len(children) != 2 {
		t.Fatalf("root children = %d, want 2", len(children))
	}
	left := tree.pages[children[0]]
	right := tree.pages[children[1]]
	if got, want := int(left.slotCount()), 2; got != want {
		t.Fatalf("left leaf slot count = %d, want %d for byte-balanced split", got, want)
	}
	if got, want := int(right.slotCount()), 4; got != want {
		t.Fatalf("right leaf slot count = %d, want %d for byte-balanced split", got, want)
	}
	if left.slottedBytesUsed() < right.slottedBytesUsed() {
		t.Fatalf("left leaf bytes = %d, right leaf bytes = %d; want large records kept on heavier left side", left.slottedBytesUsed(), right.slottedBytesUsed())
	}
}

func TestBranchSplitBalancesEncodedBytes(t *testing.T) {
	largeA := "a" + strings.Repeat("x", 1000)
	largeB := "b" + strings.Repeat("x", 1000)
	keys := []string{largeA, largeB, "c", "d", "e", "f"}

	if got, want := branchSplitIndex(keys, 3), 2; got != want {
		t.Fatalf("branch split index = %d, want %d for byte-balanced separators", got, want)
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

func TestDeleteRemovesKeyAndKeepsSnapshotVersion(t *testing.T) {
	tree := New(2)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	snapshot := tree.Snapshot()
	beforeRoot := tree.Stats().Root

	old, deleted := tree.Delete("key-17")
	if !deleted {
		t.Fatalf("Delete did not report deleting key-17")
	}
	if string(old) != "value-17" {
		t.Fatalf("Delete old value = %q, want value-17", old)
	}
	if tree.Len() != 39 {
		t.Fatalf("Len after delete = %d, want 39", tree.Len())
	}
	if _, ok := tree.Get("key-17"); ok {
		t.Fatalf("current tree still contains deleted key")
	}
	if got, ok := snapshot.Get("key-17"); !ok || string(got) != "value-17" {
		t.Fatalf("snapshot Get(key-17) = %q, %v; want value-17, true", got, ok)
	}
	if tree.Stats().Root == beforeRoot {
		t.Fatalf("Delete reused old root page id %d; want copy-on-write root", beforeRoot)
	}
	snapshot.Close()
}

func TestDeleteMergesUnderfullLeafAndKeepsSnapshotVersion(t *testing.T) {
	tree := New(3)
	for i := 0; i < 12; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	snapshot := tree.Snapshot()

	beforeLeaves := make([]PageID, 0)
	collectLeavesInOrder(tree.pages, tree.root, &beforeLeaves)
	if len(beforeLeaves) < 3 {
		t.Fatalf("leaf count before delete = %d, want at least 3 leaves", len(beforeLeaves))
	}

	if old, deleted := tree.Delete("key-03"); !deleted || string(old) != "value-03" {
		t.Fatalf("Delete(key-03) = %q, %v; want value-03, true", old, deleted)
	}
	if old, deleted := tree.Delete("key-04"); !deleted || string(old) != "value-04" {
		t.Fatalf("Delete(key-04) = %q, %v; want value-04, true", old, deleted)
	}

	afterLeaves := make([]PageID, 0)
	collectLeavesInOrder(tree.pages, tree.root, &afterLeaves)
	if len(afterLeaves) != len(beforeLeaves)-1 {
		t.Fatalf("leaf count after merge = %d, want %d; leaves before %v after %v", len(afterLeaves), len(beforeLeaves)-1, beforeLeaves, afterLeaves)
	}
	if _, ok := tree.Get("key-03"); ok {
		t.Fatalf("current tree still contains deleted key-03")
	}
	if got, ok := snapshot.Get("key-03"); !ok || string(got) != "value-03" {
		t.Fatalf("snapshot Get(key-03) = %q, %v; want value-03, true", got, ok)
	}
	if got, ok := snapshot.Get("key-04"); !ok || string(got) != "value-04" {
		t.Fatalf("snapshot Get(key-04) = %q, %v; want value-04, true", got, ok)
	}
	wantKeys := append(sequentialKeys(3), sequentialKeys(12)[5:]...)
	var gotKeys []string
	tree.Range(func(key string, value []byte) bool {
		gotKeys = append(gotKeys, key)
		return true
	})
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("Range after leaf merge = %v, want %v", gotKeys, wantKeys)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after leaf merge: %v", err)
	}

	snapshot.Close()
}

func TestDeleteRedistributesUnderfullLeafWhenMergeCannotFit(t *testing.T) {
	tree := New(3)
	seedLeafRedistributionTree(tree)
	snapshot := tree.Snapshot()

	old, deleted := tree.Delete("key-05")
	if !deleted || string(old) != "value-05" {
		t.Fatalf("Delete(key-05) = %q, %v; want value-05, true", old, deleted)
	}

	leaves := make([]PageID, 0)
	collectLeavesInOrder(tree.pages, tree.root, &leaves)
	if len(leaves) != 2 {
		t.Fatalf("leaf count after redistribute = %d, want 2 leaves: %v", len(leaves), leaves)
	}
	for _, id := range leaves {
		if got := int(tree.pages[id].slotCount()); got < minKeys(tree.degree) {
			t.Fatalf("leaf %d has %d keys after redistribute, want at least %d", id, got, minKeys(tree.degree))
		}
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after redistribute: %v", err)
	}
	if got, ok := snapshot.Get("key-05"); !ok || string(got) != "value-05" {
		t.Fatalf("snapshot Get(key-05) = %q, %v; want value-05, true", got, ok)
	}
	wantKeys := append(sequentialKeys(5), "key-06")
	var gotKeys []string
	tree.Range(func(key string, value []byte) bool {
		gotKeys = append(gotKeys, key)
		return true
	})
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("Range after redistribute = %v, want %v", gotKeys, wantKeys)
	}
	snapshot.Close()
}

func TestDeleteMergesLowByteFillLeafAtMinimumKeyCount(t *testing.T) {
	tree := New(3)
	small := []byte("s")
	for i := 0; i < 6; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), small)
	}
	beforeLeaves := make([]PageID, 0)
	collectLeavesInOrder(tree.pages, tree.root, &beforeLeaves)
	if len(beforeLeaves) != 2 {
		t.Fatalf("leaf count before delete = %d, want 2: %v", len(beforeLeaves), beforeLeaves)
	}

	old, deleted := tree.Delete("key-03")
	if !deleted || string(old) != "s" {
		t.Fatalf("Delete(key-03) = %q, %v; want s, true", old, deleted)
	}

	afterLeaves := make([]PageID, 0)
	collectLeavesInOrder(tree.pages, tree.root, &afterLeaves)
	if len(afterLeaves) != 1 {
		t.Fatalf("leaf count after low-fill merge = %d, want 1: %v", len(afterLeaves), afterLeaves)
	}
	wantKeys := []string{"key-00", "key-01", "key-02", "key-04", "key-05"}
	var gotKeys []string
	tree.Range(func(key string, value []byte) bool {
		gotKeys = append(gotKeys, key)
		return true
	})
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("Range after low-fill leaf merge = %v, want %v", gotKeys, wantKeys)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after low-fill leaf merge: %v", err)
	}
}

func TestDeleteLeafRedistributionBalancesEncodedBytes(t *testing.T) {
	tree := New(3)
	leftID := PageID(1)
	childID := PageID(2)
	tree.nextPage = 3
	left := tree.newPage(leftID, flagLeaf)
	child := tree.newPage(childID, flagLeaf)
	tree.pages[leftID] = left
	tree.pages[childID] = child
	large := bytes.Repeat([]byte("l"), 1500)
	small := []byte("s")
	tree.writeLeafEntries(left, []leafEntry{
		{key: "key-00", value: large},
		{key: "key-01", value: large},
		{key: "key-02", value: small},
		{key: "key-03", value: small},
		{key: "key-04", value: small},
	})
	tree.writeLeafEntries(child, []leafEntry{{key: "key-05", value: small}})

	children := tree.mergeUnderfullLeaf([]PageID{leftID, childID}, 1)
	if len(children) != 2 {
		t.Fatalf("children after redistribution = %v, want two leaves", children)
	}
	redistributedLeft := tree.pages[children[0]]
	redistributedChild := tree.pages[children[1]]
	if got, want := int(redistributedLeft.slotCount()), 2; got != want {
		t.Fatalf("left redistributed slot count = %d, want %d for byte-balanced split", got, want)
	}
	if got, want := int(redistributedChild.slotCount()), 4; got != want {
		t.Fatalf("right redistributed slot count = %d, want %d for byte-balanced split", got, want)
	}
	if redistributedLeft.slottedBytesUsed() < redistributedChild.slottedBytesUsed() {
		t.Fatalf("left redistributed bytes = %d, right = %d; want large records kept on heavier left side", redistributedLeft.slottedBytesUsed(), redistributedChild.slottedBytesUsed())
	}
}

func TestDeleteMergesByteUnderfullLeafAtMinimumKeyCount(t *testing.T) {
	tree := New(3)
	leftID := PageID(1)
	childID := PageID(2)
	left := tree.newPage(leftID, flagLeaf)
	child := tree.newPage(childID, flagLeaf)
	tree.pages[leftID] = left
	tree.pages[childID] = child
	small := []byte("s")
	tree.writeLeafEntries(left, []leafEntry{
		{key: "key-00", value: small},
		{key: "key-01", value: small},
	})
	tree.writeLeafEntries(child, []leafEntry{
		{key: "key-02", value: small},
		{key: "key-03", value: small},
	})

	children := tree.mergeUnderfullLeaf([]PageID{leftID, childID}, 1)
	if len(children) != 1 {
		t.Fatalf("children after byte-underfull merge = %v, want one merged leaf", children)
	}
	if got, want := int(tree.pages[children[0]].slotCount()), 4; got != want {
		t.Fatalf("merged leaf slot count = %d, want %d", got, want)
	}
}

func TestDeleteMergesUnderfullBranchAndCollapsesRoot(t *testing.T) {
	tree := New(3)
	seedBranchMergeAfterDeleteTree(tree)
	snapshot := tree.Snapshot()

	if got := tree.Stats().Height; got != 3 {
		t.Fatalf("seeded tree height = %d, want 3", got)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check seeded tree before branch merge: %v", err)
	}
	old, deleted := tree.Delete("key-08")
	if !deleted || string(old) != "value-08" {
		t.Fatalf("Delete(key-08) = %q, %v; want value-08, true", old, deleted)
	}

	stats := tree.Stats()
	if stats.Height != 2 {
		t.Fatalf("height after branch merge = %d, want 2", stats.Height)
	}
	root := tree.pages[tree.root]
	if !root.isBranch() {
		t.Fatalf("root after branch merge is not a branch")
	}
	for _, child := range root.childIDs() {
		if tree.pages[child].isBranch() {
			t.Fatalf("root still points at branch child %d after absorbable branch merge", child)
		}
	}
	if got, ok := snapshot.Get("key-08"); !ok || string(got) != "value-08" {
		t.Fatalf("snapshot Get(key-08) = %q, %v; want value-08, true", got, ok)
	}
	if _, ok := tree.Get("key-08"); ok {
		t.Fatalf("current tree still contains deleted key-08")
	}
	wantKeys := append(sequentialKeys(8), sequentialKeys(12)[9:]...)
	var gotKeys []string
	tree.Range(func(key string, value []byte) bool {
		gotKeys = append(gotKeys, key)
		return true
	})
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("Range after branch merge = %v, want %v", gotKeys, wantKeys)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after branch merge: %v", err)
	}
	snapshot.Close()
}

func TestDeleteMergesUnderfullBranchWithRightSibling(t *testing.T) {
	tree := New(3)
	seedBranchMergeAfterDeleteTree(tree)

	if old, deleted := tree.Delete("key-04"); !deleted || string(old) != "value-04" {
		t.Fatalf("Delete(key-04) = %q, %v; want value-04, true", old, deleted)
	}
	if got := tree.Stats().Height; got != 2 {
		t.Fatalf("height after right-sibling branch merge = %d, want 2", got)
	}
	wantKeys := append(sequentialKeys(4), sequentialKeys(12)[5:]...)
	var gotKeys []string
	tree.Range(func(key string, value []byte) bool {
		gotKeys = append(gotKeys, key)
		return true
	})
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("Range after right-sibling branch merge = %v, want %v", gotKeys, wantKeys)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after right-sibling branch merge: %v", err)
	}
}

func TestDeleteRedistributesUnderfullBranchWhenMergeCannotFit(t *testing.T) {
	tree := New(3)
	seedBranchRedistributionAfterDeleteTree(tree)
	snapshot := tree.Snapshot()

	if got := tree.Stats().Height; got != 3 {
		t.Fatalf("seeded tree height = %d, want 3", got)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check seeded tree before branch redistribution: %v", err)
	}
	old, deleted := tree.Delete("key-10")
	if !deleted || string(old) != "value-10" {
		t.Fatalf("Delete(key-10) = %q, %v; want value-10, true", old, deleted)
	}
	if got, ok := snapshot.Get("key-10"); !ok || string(got) != "value-10" {
		t.Fatalf("snapshot Get(key-10) = %q, %v; want value-10, true", got, ok)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after branch redistribution: %v", err)
	}

	stats := tree.Stats()
	if stats.Height != 3 {
		t.Fatalf("height after branch redistribution = %d, want 3", stats.Height)
	}
	root := tree.pages[tree.root]
	for _, child := range root.childIDs() {
		branch := tree.pages[child]
		if !branch.isBranch() {
			t.Fatalf("root child %d is not a branch after redistribution", child)
		}
		if got := int(branch.slotCount()); got < minKeys(tree.degree) {
			t.Fatalf("branch %d has %d keys after redistribution, want at least %d", child, got, minKeys(tree.degree))
		}
	}
	wantKeys := append(sequentialKeys(10), sequentialKeys(16)[11:]...)
	var gotKeys []string
	tree.Range(func(key string, value []byte) bool {
		gotKeys = append(gotKeys, key)
		return true
	})
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("Range after branch redistribution = %v, want %v", gotKeys, wantKeys)
	}
	snapshot.Close()
}

func TestDeleteBranchRedistributionBalancesEncodedBytes(t *testing.T) {
	tree := New(3)
	leafIDs := make([]PageID, 0, 7)
	for i := 0; i < 7; i++ {
		id := PageID(i + 1)
		key := fmt.Sprintf("key-%02d", i)
		if i >= 4 {
			key += strings.Repeat("x", 1000)
		}
		leaf := tree.newPage(id, flagLeaf)
		tree.pages[id] = leaf
		tree.writeLeafEntries(leaf, []leafEntry{{key: key, value: []byte("value")}})
		leafIDs = append(leafIDs, id)
	}
	leftBranchID := PageID(20)
	childBranchID := PageID(21)
	leftBranch := tree.newPage(leftBranchID, flagBranch)
	childBranch := tree.newPage(childBranchID, flagBranch)
	tree.pages[leftBranchID] = leftBranch
	tree.pages[childBranchID] = childBranch
	tree.writeBranchChildren(leftBranch, leafIDs[:5])
	tree.writeBranchChildren(childBranch, leafIDs[5:])

	children := tree.mergeUnderfullBranch([]PageID{leftBranchID, childBranchID}, 1)
	if len(children) != 2 {
		t.Fatalf("children after branch redistribution = %v, want two branches", children)
	}
	redistributedLeft := tree.pages[children[0]]
	redistributedChild := tree.pages[children[1]]
	if got, want := int(redistributedLeft.slotCount()), 3; got != want {
		t.Fatalf("left redistributed branch keys = %d, want %d for byte-balanced split", got, want)
	}
	if got, want := int(redistributedChild.slotCount()), 2; got != want {
		t.Fatalf("right redistributed branch keys = %d, want %d for byte-balanced split", got, want)
	}
}

func TestDeleteBorrowsBranchChildBeforeDescent(t *testing.T) {
	tree := New(3)
	seedBranchRedistributionAfterDeleteTree(tree)

	if got := branchChildCountsBelowRoot(tree); !reflect.DeepEqual(got, []int{5, 3}) {
		t.Fatalf("seeded branch child counts = %v, want [5 3]", got)
	}
	if old, deleted := tree.Delete("key-10"); !deleted || string(old) != "value-10" {
		t.Fatalf("Delete(key-10) = %q, %v; want value-10, true", old, deleted)
	}

	if got := branchChildCountsBelowRoot(tree); !reflect.DeepEqual(got, []int{4, 3}) {
		t.Fatalf("branch child counts after pre-descent borrow = %v, want [4 3]", got)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after pre-descent branch borrow: %v", err)
	}
}

func seedLeafRedistributionTree(tree *Tree) {
	leftID := tree.allocPage()
	rightID := tree.allocPage()
	rootID := tree.allocPage()
	left := tree.newPage(leftID, flagLeaf)
	right := tree.newPage(rightID, flagLeaf)
	root := tree.newPage(rootID, flagBranch)
	tree.pages[leftID] = left
	tree.pages[rightID] = right
	tree.pages[rootID] = root
	tree.writeLeafEntries(left, []leafEntry{
		{key: "key-00", value: []byte("value-00")},
		{key: "key-01", value: []byte("value-01")},
		{key: "key-02", value: []byte("value-02")},
		{key: "key-03", value: []byte("value-03")},
		{key: "key-04", value: []byte("value-04")},
	})
	tree.writeLeafEntries(right, []leafEntry{
		{key: "key-05", value: []byte("value-05")},
		{key: "key-06", value: []byte("value-06")},
	})
	left.setNextLeaf(rightID)
	mustWriteBranchParts(root, []string{"key-05"}, []PageID{leftID, rightID})
	tree.root = rootID
	tree.length = 7
	tree.revision = 1
}

func seedBranchMergeAfterDeleteTree(tree *Tree) {
	leafIDs := make([]PageID, 0, 6)
	for leafIndex := 0; leafIndex < 6; leafIndex++ {
		id := tree.allocPage()
		leaf := tree.newPage(id, flagLeaf)
		tree.pages[id] = leaf
		base := leafIndex * 2
		tree.writeLeafEntries(leaf, []leafEntry{
			{key: fmt.Sprintf("key-%02d", base), value: []byte(fmt.Sprintf("value-%02d", base))},
			{key: fmt.Sprintf("key-%02d", base+1), value: []byte(fmt.Sprintf("value-%02d", base+1))},
		})
		if len(leafIDs) > 0 {
			tree.pages[leafIDs[len(leafIDs)-1]].setNextLeaf(id)
		}
		leafIDs = append(leafIDs, id)
	}

	leftBranchID := tree.allocPage()
	rightBranchID := tree.allocPage()
	rootID := tree.allocPage()
	leftBranch := tree.newPage(leftBranchID, flagBranch)
	rightBranch := tree.newPage(rightBranchID, flagBranch)
	root := tree.newPage(rootID, flagBranch)
	tree.pages[leftBranchID] = leftBranch
	tree.pages[rightBranchID] = rightBranch
	tree.pages[rootID] = root
	mustWriteBranchParts(leftBranch, []string{"key-02", "key-04"}, leafIDs[:3])
	mustWriteBranchParts(rightBranch, []string{"key-08", "key-10"}, leafIDs[3:])
	mustWriteBranchParts(root, []string{"key-06"}, []PageID{leftBranchID, rightBranchID})
	tree.root = rootID
	tree.length = 12
	tree.revision = 1
}

func seedBranchRedistributionAfterDeleteTree(tree *Tree) {
	leafIDs := make([]PageID, 0, 8)
	for leafIndex := 0; leafIndex < 8; leafIndex++ {
		id := tree.allocPage()
		leaf := tree.newPage(id, flagLeaf)
		tree.pages[id] = leaf
		base := leafIndex * 2
		tree.writeLeafEntries(leaf, []leafEntry{
			{key: fmt.Sprintf("key-%02d", base), value: []byte(fmt.Sprintf("value-%02d", base))},
			{key: fmt.Sprintf("key-%02d", base+1), value: []byte(fmt.Sprintf("value-%02d", base+1))},
		})
		if len(leafIDs) > 0 {
			tree.pages[leafIDs[len(leafIDs)-1]].setNextLeaf(id)
		}
		leafIDs = append(leafIDs, id)
	}

	leftBranchID := tree.allocPage()
	rightBranchID := tree.allocPage()
	rootID := tree.allocPage()
	leftBranch := tree.newPage(leftBranchID, flagBranch)
	rightBranch := tree.newPage(rightBranchID, flagBranch)
	root := tree.newPage(rootID, flagBranch)
	tree.pages[leftBranchID] = leftBranch
	tree.pages[rightBranchID] = rightBranch
	tree.pages[rootID] = root
	mustWriteBranchParts(leftBranch, []string{"key-02", "key-04", "key-06", "key-08"}, leafIDs[:5])
	mustWriteBranchParts(rightBranch, []string{"key-12", "key-14"}, leafIDs[5:])
	mustWriteBranchParts(root, []string{"key-10"}, []PageID{leftBranchID, rightBranchID})
	tree.root = rootID
	tree.length = 16
	tree.revision = 1
}

func branchChildCountsBelowRoot(tree *Tree) []int {
	root := tree.pages[tree.root]
	counts := make([]int, 0, len(root.childIDs()))
	for _, id := range root.childIDs() {
		counts = append(counts, len(tree.pages[id].childIDs()))
	}
	return counts
}

func seedUnderfullBranchTree(tree *Tree) {
	seedBranchMergeAfterDeleteTree(tree)
	root := tree.pages[tree.root]
	_, children := root.branchParts()
	right := tree.pages[children[1]]
	rightChildren := right.childIDs()
	tree.pages[rightChildren[1]].setNextLeaf(0)
	mustWriteBranchParts(right, []string{"key-08"}, rightChildren[:2])
	tree.length = 10
}

func seedUnderfullLeafTree(tree *Tree) {
	leftID := tree.allocPage()
	rightID := tree.allocPage()
	rootID := tree.allocPage()
	left := tree.newPage(leftID, flagLeaf)
	right := tree.newPage(rightID, flagLeaf)
	root := tree.newPage(rootID, flagBranch)
	tree.pages[leftID] = left
	tree.pages[rightID] = right
	tree.pages[rootID] = root
	tree.writeLeafEntries(left, []leafEntry{
		{key: "key-00", value: []byte("value-00")},
		{key: "key-01", value: []byte("value-01")},
		{key: "key-02", value: []byte("value-02")},
		{key: "key-03", value: []byte("value-03")},
		{key: "key-04", value: []byte("value-04")},
	})
	tree.writeLeafEntries(right, []leafEntry{
		{key: "key-05", value: []byte("value-05")},
	})
	left.setNextLeaf(rightID)
	mustWriteBranchParts(root, []string{"key-05"}, []PageID{leftID, rightID})
	tree.root = rootID
	tree.length = 6
	tree.revision = 1
}

func TestDeleteMissingKeyDoesNotPublishNewRevision(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))
	before := tree.Stats()

	old, deleted := tree.Delete("missing")
	if deleted {
		t.Fatalf("Delete reported deleting missing key with old value %q", old)
	}
	after := tree.Stats()
	if after.Revision != before.Revision {
		t.Fatalf("Revision after missing delete = %d, want %d", after.Revision, before.Revision)
	}
	if after.Root != before.Root {
		t.Fatalf("Root after missing delete = %d, want %d", after.Root, before.Root)
	}
}

func TestDeleteCollapsesTreeAfterRemovingAllKeys(t *testing.T) {
	tree := New(2)
	for i := 0; i < 30; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	for i := 0; i < 30; i++ {
		if _, deleted := tree.Delete(fmt.Sprintf("key-%02d", i)); !deleted {
			t.Fatalf("Delete missed key-%02d", i)
		}
	}

	if tree.Len() != 0 {
		t.Fatalf("Len after deleting all keys = %d, want 0", tree.Len())
	}
	if tree.Stats().Root != 0 {
		t.Fatalf("Root after deleting all keys = %d, want 0", tree.Stats().Root)
	}
	if _, ok := tree.Get("key-00"); ok {
		t.Fatalf("Get found key after deleting all keys")
	}
}

func TestDeleteRetiresOverflowPagesAfterReadersClose(t *testing.T) {
	tree := New(2)
	large := bytes.Repeat([]byte("x"), PageSize*2+99)
	tree.Put("large", large)
	snapshot := tree.Snapshot()

	old, deleted := tree.Delete("large")
	if !deleted {
		t.Fatalf("Delete did not report deleting large key")
	}
	if !bytes.Equal(old, large) {
		t.Fatalf("Delete large old value mismatch: got len %d, want len %d", len(old), len(large))
	}
	if _, ok := tree.Get("large"); ok {
		t.Fatalf("current tree still contains deleted large key")
	}
	withReader := tree.Stats()
	if withReader.RetiredPages == 0 {
		t.Fatalf("RetiredPages = 0 with reader open after deleting overflow value")
	}
	if withReader.FreePages != 0 {
		t.Fatalf("FreePages = %d with reader open, want 0", withReader.FreePages)
	}
	if got, ok := snapshot.Get("large"); !ok || !bytes.Equal(got, large) {
		t.Fatalf("snapshot large value mismatch after delete: len %d, ok %v", len(got), ok)
	}
	snapshot.Close()
	if tree.Stats().FreePages == 0 {
		t.Fatalf("FreePages = 0 after closing snapshot, want deleted overflow pages reusable")
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

func TestRangeFromStartsAtLowerBound(t *testing.T) {
	tree := New(2)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var got []string
	tree.RangeFrom("key-17", func(key string, value []byte) bool {
		got = append(got, key)
		return true
	})

	want := sequentialKeys(40)[17:]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RangeFrom(key-17) = %v, want %v", got, want)
	}
}

func TestRangeFromUsesLowerBoundWhenKeyIsAbsent(t *testing.T) {
	tree := New(2)
	for _, i := range []int{0, 5, 10, 20, 30} {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var got []string
	tree.RangeFrom("key-12", func(key string, value []byte) bool {
		got = append(got, key)
		return true
	})

	want := []string{"key-20", "key-30"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RangeFrom(key-12) = %v, want %v", got, want)
	}
}

func TestRangeBetweenVisitsExclusiveUpperBound(t *testing.T) {
	tree := New(2)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var got []string
	tree.RangeBetween("key-17", "key-23", func(key string, value []byte) bool {
		got = append(got, key)
		return true
	})

	want := sequentialKeys(40)[17:23]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RangeBetween(key-17,key-23) = %v, want %v", got, want)
	}
}

func TestRangeBetweenEmptyWhenStartReachesEnd(t *testing.T) {
	tree := New(2)
	for i := 0; i < 10; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var got []string
	tree.RangeBetween("key-07", "key-07", func(key string, value []byte) bool {
		got = append(got, key)
		return true
	})

	if len(got) != 0 {
		t.Fatalf("RangeBetween with empty bounds visited %v, want none", got)
	}
}

func TestSnapshotRangeFromReadsOldRootLowerBound(t *testing.T) {
	tree := New(2)
	for i := 0; i < 30; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("before-%02d", i)))
	}
	snapshot := tree.Snapshot()
	defer snapshot.Close()

	for i := 20; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("after-%02d", i)))
	}

	var got []string
	snapshot.RangeFrom("key-25", func(key string, value []byte) bool {
		got = append(got, fmt.Sprintf("%s=%s", key, value))
		return true
	})

	want := []string{
		"key-25=before-25",
		"key-26=before-26",
		"key-27=before-27",
		"key-28=before-28",
		"key-29=before-29",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot RangeFrom(key-25) = %v, want %v", got, want)
	}
}

func TestSnapshotRangeBetweenReadsOldRootBounds(t *testing.T) {
	tree := New(2)
	for i := 0; i < 30; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("before-%02d", i)))
	}
	snapshot := tree.Snapshot()
	defer snapshot.Close()

	for i := 20; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("after-%02d", i)))
	}

	var got []string
	snapshot.RangeBetween("key-24", "key-28", func(key string, value []byte) bool {
		got = append(got, fmt.Sprintf("%s=%s", key, value))
		return true
	})

	want := []string{
		"key-24=before-24",
		"key-25=before-25",
		"key-26=before-26",
		"key-27=before-27",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot RangeBetween(key-24,key-28) = %v, want %v", got, want)
	}
}

func TestLeafSplitsMaintainNextLeafLinks(t *testing.T) {
	tree := New(2)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	got := leafChainKeys(tree.pages, tree.root)
	want := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		want = append(want, fmt.Sprintf("key-%02d", i))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("leaf chain keys = %v, want %v", got, want)
	}
}

func TestDeleteRelinksCurrentLeafChain(t *testing.T) {
	tree := New(2)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	for _, i := range []int{2, 3, 4, 17, 18, 35} {
		if _, deleted := tree.Delete(fmt.Sprintf("key-%02d", i)); !deleted {
			t.Fatalf("Delete missed key-%02d", i)
		}
	}

	got := leafChainKeys(tree.pages, tree.root)
	var want []string
	deleted := map[int]bool{2: true, 3: true, 4: true, 17: true, 18: true, 35: true}
	for i := 0; i < 40; i++ {
		if !deleted[i] {
			want = append(want, fmt.Sprintf("key-%02d", i))
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("leaf chain after delete = %v, want %v", got, want)
	}
}

func TestSnapshotLeafLinksStayStableDuringConcurrentWrite(t *testing.T) {
	tree := New(2)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	snapshot := tree.Snapshot()
	before := leafChainKeys(snapshot.pages, snapshot.root)

	tree.Put("key-99", []byte("new-right-edge"))

	after := leafChainKeys(snapshot.pages, snapshot.root)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("snapshot leaf chain changed from %v to %v", before, after)
	}
	if !reflect.DeepEqual(after, sequentialKeys(40)) {
		t.Fatalf("snapshot leaf chain after concurrent write = %v, want original keys", after)
	}
	snapshot.Close()

	wantCurrent := append(sequentialKeys(40), "key-99")
	if got := leafChainKeys(tree.pages, tree.root); !reflect.DeepEqual(got, wantCurrent) {
		t.Fatalf("current leaf chain after closing snapshot = %v, want %v", got, wantCurrent)
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

func TestCheckAcceptsValidTree(t *testing.T) {
	tree := New(2)
	large := bytes.Repeat([]byte("x"), PageSize*2+41)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	tree.Put("large", large)
	if _, deleted := tree.Delete("key-03"); !deleted {
		t.Fatalf("Delete(key-03) = false, want true")
	}

	if err := tree.Check(); err != nil {
		t.Fatalf("Check valid tree: %v", err)
	}
}

func TestCheckRejectsOpenTreeWithMismatchedBranchSeparator(t *testing.T) {
	tree := New(2)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	root := tree.pages[tree.root]
	if !root.isBranch() || root.slotCount() == 0 {
		t.Fatalf("root does not have a separator after many inserts")
	}
	corruptInMemoryBranchSlotKey(root, 0, "key-00")

	err := tree.Check()
	if err == nil {
		t.Fatalf("Check succeeded with mismatched branch separator")
	}
	if !errors.Is(err, ErrTreeInvariant) {
		t.Fatalf("Check mismatched separator error = %v, want ErrTreeInvariant", err)
	}
}

func TestCheckRejectsOpenTreeWithUnderfullNonRootLeaf(t *testing.T) {
	tree := New(3)
	seedUnderfullLeafTree(tree)

	err := tree.Check()
	if err == nil {
		t.Fatalf("Check accepted underfull non-root leaf")
	}
	if !errors.Is(err, ErrTreeInvariant) {
		t.Fatalf("Check underfull leaf error = %v, want ErrTreeInvariant", err)
	}
}

func TestCheckRejectsOpenTreeWithUnderfullNonRootBranch(t *testing.T) {
	tree := New(3)
	seedUnderfullBranchTree(tree)

	err := tree.Check()
	if err == nil {
		t.Fatalf("Check accepted underfull non-root branch")
	}
	if !errors.Is(err, ErrTreeInvariant) {
		t.Fatalf("Check underfull branch error = %v, want ErrTreeInvariant", err)
	}
}

func TestCheckAllowsDeferredLeafRelinkWhileReaderIsActive(t *testing.T) {
	tree := New(2)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	snapshot := tree.Snapshot()
	defer snapshot.Close()
	tree.Put("key-40", []byte("value-40"))

	if err := tree.Check(); err != nil {
		t.Fatalf("Check with active reader and deferred leaf relink: %v", err)
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

func TestRangeBetweenStopsBeforeReadingOutOfBoundLeafCellValue(t *testing.T) {
	leaf := newPage(1, flagLeaf)
	mustWriteLeafEntries(leaf, []leafEntry{
		{key: "alpha", value: []byte("one")},
		{key: "bravo", value: []byte("two")},
		{key: "charlie", value: []byte("three")},
	})
	corruptSlotValueLen(leaf, 2, PageSize)

	var got []string
	rangePageBetween(map[PageID]*page{1: leaf}, 1, "alpha", "charlie", func(key string, value []byte) bool {
		got = append(got, fmt.Sprintf("%s=%s", key, value))
		return true
	})

	want := []string{"alpha=one", "bravo=two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rangePageBetween(alpha,charlie) = %v, want %v", got, want)
	}
}

func TestRangeFromStartsBeforeReadingLowerLeafCellValue(t *testing.T) {
	leaf := newPage(1, flagLeaf)
	mustWriteLeafEntries(leaf, []leafEntry{
		{key: "alpha", value: []byte("one")},
		{key: "bravo", value: []byte("two")},
		{key: "charlie", value: []byte("three")},
	})
	corruptSlotValueLen(leaf, 0, PageSize)

	var got []string
	rangePageFrom(map[PageID]*page{1: leaf}, 1, "bravo", func(key string, value []byte) bool {
		got = append(got, fmt.Sprintf("%s=%s", key, value))
		return true
	})

	want := []string{"bravo=two", "charlie=three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rangePageFrom(bravo) = %v, want %v", got, want)
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

func TestGetCachesBranchRoutingMetadata(t *testing.T) {
	tree := New(2)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if tree.Stats().Height < 2 {
		t.Fatalf("Height = %d, want a tree with branch pages", tree.Stats().Height)
	}

	if _, ok := tree.Get("key-17"); !ok {
		t.Fatalf("first Get(key-17) missed")
	}
	afterFirst := tree.Stats()
	if afterFirst.PageCacheEntries == 0 {
		t.Fatalf("PageCacheEntries = 0, want cached branch routing metadata")
	}
	if afterFirst.PageCacheMisses == 0 {
		t.Fatalf("PageCacheMisses = 0, want first lookup to decode branch routing")
	}

	if _, ok := tree.Get("key-17"); !ok {
		t.Fatalf("second Get(key-17) missed")
	}
	afterSecond := tree.Stats()
	if afterSecond.PageCacheHits <= afterFirst.PageCacheHits {
		t.Fatalf("PageCacheHits did not increase: before %d after %d", afterFirst.PageCacheHits, afterSecond.PageCacheHits)
	}
	if afterSecond.PageCacheMisses != afterFirst.PageCacheMisses {
		t.Fatalf("PageCacheMisses changed on cached lookup: before %d after %d", afterFirst.PageCacheMisses, afterSecond.PageCacheMisses)
	}
}

func TestPageCacheCapacityBoundsBranchRoutingEntries(t *testing.T) {
	tree := NewWithOptions(2, Options{PageCacheCapacity: 1})
	for i := 0; i < 80; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if tree.Stats().Height < 3 {
		t.Fatalf("Height = %d, want a tree with multiple branch pages", tree.Stats().Height)
	}

	for _, key := range []string{"key-05", "key-35", "key-75"} {
		if _, ok := tree.Get(key); !ok {
			t.Fatalf("Get(%q) missed", key)
		}
	}

	stats := tree.Stats()
	if stats.PageCacheCapacity != 1 {
		t.Fatalf("PageCacheCapacity = %d, want 1", stats.PageCacheCapacity)
	}
	if stats.PageCacheEntries != 1 {
		t.Fatalf("PageCacheEntries = %d, want bounded cache to keep 1 entry", stats.PageCacheEntries)
	}
	if stats.PageCacheEvictions == 0 {
		t.Fatalf("PageCacheEvictions = 0, want eviction after visiting multiple branch pages")
	}
}

func TestPageCacheCanBeDisabled(t *testing.T) {
	tree := NewWithOptions(2, Options{PageCacheCapacity: -1})
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	if _, ok := tree.Get("key-17"); !ok {
		t.Fatalf("Get(key-17) missed")
	}
	stats := tree.Stats()
	if stats.PageCacheCapacity != 0 {
		t.Fatalf("PageCacheCapacity = %d, want disabled cache capacity 0", stats.PageCacheCapacity)
	}
	if stats.PageCacheEntries != 0 {
		t.Fatalf("PageCacheEntries = %d, want disabled cache to keep no entries", stats.PageCacheEntries)
	}
	if stats.PageCacheHits != 0 {
		t.Fatalf("PageCacheHits = %d, want disabled cache to record no hits", stats.PageCacheHits)
	}
	if stats.PageCacheMisses == 0 {
		t.Fatalf("PageCacheMisses = 0, want disabled cache to still count decoded branch misses")
	}
}

func TestPageCacheRefreshesBranchRoutingWhenChecksumChanges(t *testing.T) {
	cache := pageCache{}
	branch := newPage(9, flagBranch)
	mustWriteBranchParts(branch, []string{"bravo", "delta"}, []PageID{10, 20, 30})

	if got := cache.searchBranchChild(branch, "charlie"); got != 20 {
		t.Fatalf("first cached child = %d, want 20", got)
	}
	if got := cache.searchBranchChild(branch, "charlie"); got != 20 {
		t.Fatalf("second cached child = %d, want 20", got)
	}
	if cache.stats.Hits == 0 {
		t.Fatalf("cache hits = 0, want repeated lookup to hit")
	}

	mustWriteBranchParts(branch, []string{"bravo", "delta"}, []PageID{10, 200, 30})
	if got := cache.searchBranchChild(branch, "charlie"); got != 200 {
		t.Fatalf("refreshed cached child = %d, want 200", got)
	}
	if cache.stats.Invalidations == 0 {
		t.Fatalf("cache invalidations = 0, want checksum change to refresh entry")
	}
}

func TestPageCacheEvictsLeastRecentlyUsedBranch(t *testing.T) {
	cache := newPageCache(2)
	first := newPage(1, flagBranch)
	mustWriteBranchParts(first, []string{"m"}, []PageID{10, 11})
	second := newPage(2, flagBranch)
	mustWriteBranchParts(second, []string{"m"}, []PageID{20, 21})
	third := newPage(3, flagBranch)
	mustWriteBranchParts(third, []string{"m"}, []PageID{30, 31})

	cache.searchBranchChild(first, "a")
	cache.searchBranchChild(second, "a")
	cache.searchBranchChild(first, "a")
	cache.searchBranchChild(third, "a")

	if _, ok := cache.branches[first.id]; !ok {
		t.Fatalf("recently used branch %d was evicted", first.id)
	}
	if _, ok := cache.branches[second.id]; ok {
		t.Fatalf("least recently used branch %d remained cached", second.id)
	}
	if _, ok := cache.branches[third.id]; !ok {
		t.Fatalf("new branch %d was not cached", third.id)
	}
	if cache.stats.Evictions != 1 {
		t.Fatalf("evictions = %d, want 1", cache.stats.Evictions)
	}
}

func TestRangeBetweenStopsBeforeReadingOutOfBoundBranchChildValue(t *testing.T) {
	left := newPage(10, flagLeaf)
	mustWriteLeafEntries(left, []leafEntry{{key: "alpha", value: []byte("one")}})
	right := newPage(20, flagLeaf)
	mustWriteLeafEntries(right, []leafEntry{{key: "bravo", value: []byte("two")}})
	branch := newPage(9, flagBranch)
	mustWriteBranchParts(branch, []string{"bravo"}, []PageID{10, 20})
	corruptSlotValueLen(branch, 0, PageSize)

	pages := map[PageID]*page{
		9:  branch,
		10: left,
		20: right,
	}
	var got []string
	rangePageBetween(pages, 9, "alpha", "bravo", func(key string, value []byte) bool {
		got = append(got, fmt.Sprintf("%s=%s", key, value))
		return true
	})

	want := []string{"alpha=one"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rangePageBetween(alpha,bravo) = %v, want %v", got, want)
	}
}

func TestLargeValuesUseOverflowPagesAndRemainCopyOnWrite(t *testing.T) {
	tree := New(2)
	large := bytes.Repeat([]byte("x"), PageSize*2+123)

	tree.Put("large", large)

	got, ok := tree.Get("large")
	if !ok {
		t.Fatalf("Get missed large value")
	}
	if !bytes.Equal(got, large) {
		t.Fatalf("large value length/content mismatch: got len %d, want len %d", len(got), len(large))
	}
	got[0] = 'X'
	again, ok := tree.Get("large")
	if !ok || !bytes.Equal(again, large) {
		t.Fatalf("large value read leaked mutable storage")
	}
	if tree.Stats().Pages < 3 {
		t.Fatalf("Pages = %d, want leaf plus overflow pages", tree.Stats().Pages)
	}

	snapshot := tree.Snapshot()
	updated := bytes.Repeat([]byte("y"), PageSize+77)
	tree.Put("large", updated)
	withReader := tree.Stats()
	if withReader.RetiredPages == 0 {
		t.Fatalf("RetiredPages = 0 with reader open after replacing overflow value")
	}
	if withReader.FreePages != 0 {
		t.Fatalf("FreePages = %d with reader open, want 0", withReader.FreePages)
	}

	old, ok := snapshot.Get("large")
	if !ok || !bytes.Equal(old, large) {
		t.Fatalf("snapshot large value mismatch: len %d, ok %v", len(old), ok)
	}
	current, ok := tree.Get("large")
	if !ok || !bytes.Equal(current, updated) {
		t.Fatalf("current large value mismatch after update: len %d, ok %v", len(current), ok)
	}
	snapshot.Close()
	afterClose := tree.Stats()
	if afterClose.FreePages == 0 {
		t.Fatalf("FreePages = 0 after reader close, want retired overflow pages reusable")
	}
}

func TestInlineValueThatLooksLikeOverflowReferenceStaysInline(t *testing.T) {
	tree := New(2)
	value := []byte("OVF1not-a-page-ref!!")
	if len(value) != overflowRefSize {
		t.Fatalf("test value length = %d, want %d", len(value), overflowRefSize)
	}

	tree.Put("looks-like-ref", value)

	got, ok := tree.Get("looks-like-ref")
	if !ok {
		t.Fatalf("Get missed inline value")
	}
	if !bytes.Equal(got, value) {
		t.Fatalf("Get returned %q, want %q", got, value)
	}
}

func TestMediumValuesSpillToOverflowWhenLeafRunsOutOfBytes(t *testing.T) {
	tree := New(4)
	values := map[string][]byte{
		"k1": bytes.Repeat([]byte("a"), 1800),
		"k2": bytes.Repeat([]byte("b"), 1800),
		"k3": bytes.Repeat([]byte("c"), 1800),
	}

	for key, value := range values {
		tree.Put(key, value)
	}

	for key, want := range values {
		got, ok := tree.Get(key)
		if !ok {
			t.Fatalf("Get missed %s", key)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("Get(%s) length/content mismatch: got len %d, want len %d", key, len(got), len(want))
		}
	}
	if tree.Stats().Pages < 2 {
		t.Fatalf("Pages = %d, want leaf plus spill overflow pages", tree.Stats().Pages)
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

func leafChainKeys(pages map[PageID]*page, root PageID) []string {
	leaf := leftmostLeafID(pages, root)
	var out []string
	seen := map[PageID]bool{}
	for leaf != 0 {
		if seen[leaf] {
			out = append(out, fmt.Sprintf("cycle-at-%d", leaf))
			return out
		}
		seen[leaf] = true
		p := pages[leaf]
		for _, entry := range p.leafEntries() {
			out = append(out, entry.key)
		}
		leaf = p.nextLeaf()
	}
	return out
}

func leftmostLeafID(pages map[PageID]*page, root PageID) PageID {
	for root != 0 {
		p := pages[root]
		if p.isLeaf() {
			return root
		}
		root = p.leftmostChild()
	}
	return 0
}

func sequentialKeys(count int) []string {
	keys := make([]string, 0, count)
	for i := 0; i < count; i++ {
		keys = append(keys, fmt.Sprintf("key-%02d", i))
	}
	return keys
}

func corruptSlotValueLen(p *page, index int, valueLen int) {
	slot := p.readSlot(index)
	slot.valueLen = uint16(valueLen)
	p.writeSlot(index, slot)
	p.updateChecksum()
}

func corruptInMemoryBranchSlotKey(p *page, index int, key string) {
	slot := p.readSlot(index)
	copy(p.data[slot.offset:slot.offset+slot.keyLen], key)
	p.updateChecksum()
}
