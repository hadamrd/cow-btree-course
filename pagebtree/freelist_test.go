package pagebtree

import (
	"fmt"
	"testing"
)

func TestReadTransactionPinsRetiredPagesUntilClose(t *testing.T) {
	tree := New(2)
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("before-%02d", i)))
	}

	reader := tree.Snapshot()
	tree.Put("key-10", []byte("after-10"))
	tree.Put("key-41", []byte("after-41"))

	stats := tree.Stats()
	if stats.ActiveReaders != 1 {
		t.Fatalf("ActiveReaders = %d, want 1", stats.ActiveReaders)
	}
	if stats.RetiredPages == 0 {
		t.Fatalf("RetiredPages = 0, want copied old pages to be retired while reader is open")
	}
	if stats.FreePages != 0 {
		t.Fatalf("FreePages = %d, want 0 while reader pins retired pages", stats.FreePages)
	}

	got, ok := reader.Get("key-10")
	if !ok || string(got) != "before-10" {
		t.Fatalf("reader Get(key-10) = %q, %v; want before-10, true", got, ok)
	}

	reader.Close()
	stats = tree.Stats()
	if stats.ActiveReaders != 0 {
		t.Fatalf("ActiveReaders after Close = %d, want 0", stats.ActiveReaders)
	}
	if stats.RetiredPages != 0 {
		t.Fatalf("RetiredPages after Close = %d, want 0", stats.RetiredPages)
	}
	if stats.FreePages == 0 {
		t.Fatalf("FreePages after Close = 0, want retired pages released to freelist")
	}
}

func TestFreelistReusesPagesOnlyAfterReadersClose(t *testing.T) {
	tree := New(2)
	for i := 0; i < 60; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("before-%02d", i)))
	}

	reader := tree.Snapshot()
	tree.Put("key-30", []byte("after-30"))
	pinned := tree.Stats()
	if pinned.FreePages != 0 {
		t.Fatalf("FreePages with active reader = %d, want 0", pinned.FreePages)
	}

	reader.Close()
	released := tree.Stats()
	if released.FreePages == 0 {
		t.Fatalf("FreePages after reader close = 0, want pages available for reuse")
	}
	allocatedBeforeReuse := released.AllocatedPages

	tree.Put("key-61", []byte("after-61"))
	reused := tree.Stats()
	if reused.ReusedPages == 0 {
		t.Fatalf("ReusedPages = 0, want next write to reuse at least one free page")
	}
	if reused.AllocatedPages > allocatedBeforeReuse+1 {
		t.Fatalf("AllocatedPages grew from %d to %d despite available freelist pages", allocatedBeforeReuse, reused.AllocatedPages)
	}
}
