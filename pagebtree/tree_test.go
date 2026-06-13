package pagebtree

import (
	"bytes"
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
