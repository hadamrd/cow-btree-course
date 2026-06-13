package pagebtree

import (
	"path/filepath"
	"testing"
)

func TestWriteBatchPublishesMultipleMutationsAsOneRevision(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))
	beforeRevision := tree.Revision()
	snapshot := tree.Snapshot()
	defer snapshot.Close()

	batch := tree.Batch()
	batch.Put("alpha", []byte("two"))
	batch.Put("bravo", []byte("three"))
	batch.Delete("missing")

	if got, ok := tree.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get(alpha) before Commit = %q, %v; want one, true", got, ok)
	}
	if got, ok := tree.Get("bravo"); ok {
		t.Fatalf("Get(bravo) before Commit = %q, true; want staged key hidden", got)
	}
	if got := tree.Revision(); got != beforeRevision {
		t.Fatalf("Revision before Commit = %d, want %d", got, beforeRevision)
	}

	if changed := batch.Commit(); !changed {
		t.Fatalf("Commit changed = false, want true")
	}
	if got := tree.Revision(); got != beforeRevision+1 {
		t.Fatalf("Revision after Commit = %d, want %d", got, beforeRevision+1)
	}
	if got, ok := tree.Get("alpha"); !ok || string(got) != "two" {
		t.Fatalf("Get(alpha) after Commit = %q, %v; want two, true", got, ok)
	}
	if got, ok := tree.Get("bravo"); !ok || string(got) != "three" {
		t.Fatalf("Get(bravo) after Commit = %q, %v; want three, true", got, ok)
	}
	if got, ok := snapshot.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("snapshot Get(alpha) after Commit = %q, %v; want one, true", got, ok)
	}
	if _, ok := snapshot.Get("bravo"); ok {
		t.Fatalf("snapshot Get(bravo) after Commit = true; want old snapshot without bravo")
	}
}

func TestWriteBatchRollbackAndEmptyCommitDoNotPublishRevision(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))
	beforeRevision := tree.Revision()

	rolledBack := tree.Batch()
	rolledBack.Put("alpha", []byte("two"))
	rolledBack.Delete("alpha")
	rolledBack.Rollback()
	if changed := rolledBack.Commit(); changed {
		t.Fatalf("Commit after Rollback changed = true, want false")
	}
	if got := tree.Revision(); got != beforeRevision {
		t.Fatalf("Revision after Rollback = %d, want %d", got, beforeRevision)
	}
	if got, ok := tree.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get(alpha) after Rollback = %q, %v; want one, true", got, ok)
	}

	empty := tree.Batch()
	if changed := empty.Commit(); changed {
		t.Fatalf("empty Commit changed = true, want false")
	}
	if got := tree.Revision(); got != beforeRevision {
		t.Fatalf("Revision after empty Commit = %d, want %d", got, beforeRevision)
	}
}

func TestMmapWriteBatchPersistsOneRevisionAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	tree.Put("bravo", []byte("two"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	beforeRevision := tree.Revision()

	batch := tree.Batch()
	batch.Delete("alpha")
	batch.Put("charlie", []byte("three"))
	if changed := batch.Commit(); !changed {
		t.Fatalf("Commit changed = false, want true")
	}
	if got := tree.Revision(); got != beforeRevision+1 {
		t.Fatalf("Revision after mmap batch Commit = %d, want %d", got, beforeRevision+1)
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync after batch: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close after batch: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()
	if _, ok := reopened.Get("alpha"); ok {
		t.Fatalf("reopened Get(alpha) = true; want deleted")
	}
	if got, ok := reopened.Get("bravo"); !ok || string(got) != "two" {
		t.Fatalf("reopened Get(bravo) = %q, %v; want two, true", got, ok)
	}
	if got, ok := reopened.Get("charlie"); !ok || string(got) != "three" {
		t.Fatalf("reopened Get(charlie) = %q, %v; want three, true", got, ok)
	}
	if got := reopened.Revision(); got != beforeRevision+1 {
		t.Fatalf("reopened Revision = %d, want %d", got, beforeRevision+1)
	}
}
