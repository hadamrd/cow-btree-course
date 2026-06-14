package pagebtree

import (
	"fmt"
	"testing"
)

func TestCursorSeekAndNextReadStableSnapshot(t *testing.T) {
	tree := New(3)
	for i := 0; i < 24; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	cursor := tree.Cursor()
	if !cursor.Seek("key-05") {
		t.Fatalf("Seek(key-05) = false, want true")
	}
	if got := cursor.Key(); got != "key-05" {
		t.Fatalf("cursor key = %q, want key-05", got)
	}
	if got := string(cursor.Value()); got != "value-05" {
		t.Fatalf("cursor value = %q, want value-05", got)
	}
	if got := tree.Stats().ActiveReaders; got != 1 {
		t.Fatalf("active readers with open cursor = %d, want 1", got)
	}

	tree.Put("key-05", []byte("new-value-05"))
	tree.Delete("key-06")

	if got := cursor.Key(); got != "key-05" {
		t.Fatalf("cursor key after mutation = %q, want key-05", got)
	}
	if got := string(cursor.Value()); got != "value-05" {
		t.Fatalf("cursor value after mutation = %q, want old value-05", got)
	}
	if !cursor.Next() {
		t.Fatalf("Next after key-05 = false, want true")
	}
	if got := cursor.Key(); got != "key-06" {
		t.Fatalf("cursor next key after delete = %q, want snapshot key-06", got)
	}
	if got := string(cursor.Value()); got != "value-06" {
		t.Fatalf("cursor next value after delete = %q, want snapshot value-06", got)
	}

	cursor.Close()
	if got := tree.Stats().ActiveReaders; got != 0 {
		t.Fatalf("active readers after cursor close = %d, want 0", got)
	}
	if cursor.Next() {
		t.Fatalf("Next after Close = true, want false")
	}
}

func TestCursorSeekMissingStartsAtNextKey(t *testing.T) {
	tree := New(2)
	for _, key := range []string{"alpha", "bravo", "delta", "echo"} {
		tree.Put(key, []byte(key+"-value"))
	}
	cursor := tree.Cursor()
	defer cursor.Close()

	if !cursor.Seek("charlie") {
		t.Fatalf("Seek(charlie) = false, want true")
	}
	if got := cursor.Key(); got != "delta" {
		t.Fatalf("cursor key = %q, want delta", got)
	}
	if got := string(cursor.Value()); got != "delta-value" {
		t.Fatalf("cursor value = %q, want delta-value", got)
	}
}

func TestCursorFirstAndEnd(t *testing.T) {
	tree := New(2)
	tree.Put("bravo", []byte("two"))
	tree.Put("alpha", []byte("one"))

	cursor := tree.Cursor()
	defer cursor.Close()

	if !cursor.First() {
		t.Fatalf("First = false, want true")
	}
	if got := cursor.Key(); got != "alpha" {
		t.Fatalf("first key = %q, want alpha", got)
	}
	if !cursor.Next() {
		t.Fatalf("Next after first = false, want true")
	}
	if got := cursor.Key(); got != "bravo" {
		t.Fatalf("second key = %q, want bravo", got)
	}
	if cursor.Next() {
		t.Fatalf("Next after last = true, want false")
	}
	if cursor.Key() != "" || cursor.Value() != nil {
		t.Fatalf("invalid cursor key/value = %q/%v, want empty/nil", cursor.Key(), cursor.Value())
	}
	if cursor.Seek("zulu") {
		t.Fatalf("Seek past end = true, want false")
	}
}

func TestCursorLastAndPrevReadStableSnapshot(t *testing.T) {
	tree := New(3)
	for i := 0; i < 12; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	cursor := tree.Cursor()
	if !cursor.Last() {
		t.Fatalf("Last = false, want true")
	}
	if got := cursor.Key(); got != "key-11" {
		t.Fatalf("last key = %q, want key-11", got)
	}

	tree.Put("key-10", []byte("new-value-10"))
	tree.Delete("key-09")

	var got []string
	var values []string
	for cursor.Valid() {
		got = append(got, cursor.Key())
		values = append(values, string(cursor.Value()))
		if !cursor.Prev() {
			break
		}
	}
	wantKeys := []string{"key-11", "key-10", "key-09", "key-08", "key-07", "key-06", "key-05", "key-04", "key-03", "key-02", "key-01", "key-00"}
	if fmt.Sprint(got) != fmt.Sprint(wantKeys) {
		t.Fatalf("reverse cursor keys = %v, want %v", got, wantKeys)
	}
	if values[1] != "value-10" || values[2] != "value-09" {
		t.Fatalf("reverse cursor old values around mutation = %v, want value-10/value-09", values[:3])
	}
	cursor.Close()
}

func TestBoundedCursorLastAndPrevStopAtLowerBound(t *testing.T) {
	tree := New(3)
	for i := 0; i < 14; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	cursor := tree.CursorBetween("key-05", "key-09")
	defer cursor.Close()

	if !cursor.Last() {
		t.Fatalf("Last on bounded cursor = false, want true")
	}
	var got []string
	for cursor.Valid() {
		got = append(got, cursor.Key())
		if !cursor.Prev() {
			break
		}
	}
	want := []string{"key-08", "key-07", "key-06", "key-05"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("bounded reverse cursor keys = %v, want %v", got, want)
	}
}

func TestTreeCursorBetweenStopsAtExclusiveEnd(t *testing.T) {
	tree := New(3)
	for i := 0; i < 14; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	cursor := tree.CursorBetween("key-05", "key-09")
	defer cursor.Close()

	var got []string
	for cursor.Valid() {
		got = append(got, cursor.Key())
		if !cursor.Next() {
			break
		}
	}
	want := []string{"key-05", "key-06", "key-07", "key-08"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("CursorBetween keys = %v, want %v", got, want)
	}
	if got := tree.Stats().ActiveReaders; got != 1 {
		t.Fatalf("active readers with bounded cursor = %d, want 1", got)
	}

	cursor.Close()
	if got := tree.Stats().ActiveReaders; got != 0 {
		t.Fatalf("active readers after bounded cursor close = %d, want 0", got)
	}
}

func TestSnapshotCursorBetweenBorrowsSnapshotAndReadsOldBounds(t *testing.T) {
	tree := New(3)
	for i := 0; i < 14; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	snapshot := tree.Snapshot()
	if got := tree.Stats().ActiveReaders; got != 1 {
		t.Fatalf("active readers after snapshot = %d, want 1", got)
	}

	tree.Put("key-06", []byte("new-value-06"))
	tree.Delete("key-07")
	cursor := snapshot.CursorBetween("key-06", "key-08")

	var got []string
	var values []string
	for cursor.Valid() {
		got = append(got, cursor.Key())
		values = append(values, string(cursor.Value()))
		if !cursor.Next() {
			break
		}
	}
	wantKeys := []string{"key-06", "key-07"}
	wantValues := []string{"value-06", "value-07"}
	if fmt.Sprint(got) != fmt.Sprint(wantKeys) || fmt.Sprint(values) != fmt.Sprint(wantValues) {
		t.Fatalf("snapshot CursorBetween keys/values = %v/%v, want %v/%v", got, values, wantKeys, wantValues)
	}

	cursor.Close()
	if got := tree.Stats().ActiveReaders; got != 1 {
		t.Fatalf("active readers after borrowed bounded cursor close = %d, want snapshot still pinned", got)
	}
	snapshot.Close()
	if got := tree.Stats().ActiveReaders; got != 0 {
		t.Fatalf("active readers after snapshot close = %d, want 0", got)
	}
}

func TestSnapshotCursorDoesNotRegisterAnotherReader(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))
	snapshot := tree.Snapshot()
	if got := tree.Stats().ActiveReaders; got != 1 {
		t.Fatalf("active readers after snapshot = %d, want 1", got)
	}

	cursor := snapshot.Cursor()
	if got := tree.Stats().ActiveReaders; got != 1 {
		t.Fatalf("active readers after snapshot cursor = %d, want still 1", got)
	}
	if !cursor.First() || cursor.Key() != "alpha" {
		t.Fatalf("snapshot cursor First = %v key %q, want alpha", cursor.Valid(), cursor.Key())
	}

	cursor.Close()
	if got := tree.Stats().ActiveReaders; got != 1 {
		t.Fatalf("active readers after cursor close = %d, want snapshot still pinned", got)
	}
	snapshot.Close()
	if got := tree.Stats().ActiveReaders; got != 0 {
		t.Fatalf("active readers after snapshot close = %d, want 0", got)
	}
}

func TestCursorDeleteRemovesCurrentLiveKeyAndKeepsSnapshotIteration(t *testing.T) {
	tree := New(3)
	for i := 0; i < 12; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	beforeRevision := tree.Revision()

	cursor := tree.Cursor()
	defer cursor.Close()
	if !cursor.Seek("key-05") {
		t.Fatalf("Seek(key-05) = false, want true")
	}

	old, deleted := cursor.Delete()
	if !deleted || string(old) != "value-05" {
		t.Fatalf("cursor Delete = %q, %v; want value-05, true", old, deleted)
	}
	if _, ok := tree.Get("key-05"); ok {
		t.Fatalf("tree still contains key-05 after cursor Delete")
	}
	if tree.Len() != 11 {
		t.Fatalf("tree Len after cursor Delete = %d, want 11", tree.Len())
	}
	if tree.Revision() == beforeRevision {
		t.Fatalf("tree revision did not advance after cursor Delete")
	}

	if got := cursor.Key(); got != "key-05" {
		t.Fatalf("cursor key after Delete = %q, want snapshot key-05", got)
	}
	if got := string(cursor.Value()); got != "value-05" {
		t.Fatalf("cursor value after Delete = %q, want snapshot value-05", got)
	}
	if !cursor.Next() || cursor.Key() != "key-06" {
		t.Fatalf("cursor Next after Delete = %v key %q, want key-06", cursor.Valid(), cursor.Key())
	}
}

func TestCursorDeleteRequiresOwnedWritableCursor(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))
	snapshot := tree.Snapshot()
	borrowed := snapshot.Cursor()
	if !borrowed.First() {
		t.Fatalf("borrowed cursor First = false, want true")
	}
	if old, deleted := borrowed.Delete(); deleted || old != nil {
		t.Fatalf("borrowed cursor Delete = %q, %v; want nil, false", old, deleted)
	}
	if _, ok := tree.Get("alpha"); !ok {
		t.Fatalf("borrowed cursor deleted alpha; want no mutation")
	}
	borrowed.Close()
	snapshot.Close()

	readOnly := &Tree{readOnly: true}
	readOnly.pages = map[PageID]*page{}
	readOnlyCursor := readOnly.Cursor()
	if old, deleted := readOnlyCursor.Delete(); deleted || old != nil {
		t.Fatalf("read-only cursor Delete = %q, %v; want nil, false", old, deleted)
	}
	readOnlyCursor.Close()
}
