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
