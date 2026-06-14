package pagebtree

import (
	"errors"
	"fmt"
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

func TestWriteBatchCommitDetailedReportsOperationResults(t *testing.T) {
	tree := New(2)
	tree.Put("alpha", []byte("one"))
	beforeRevision := tree.Revision()

	batch := tree.Batch()
	batch.Put("alpha", []byte("two"))
	batch.Delete("missing")
	batch.Delete("alpha")
	batch.Put("bravo", []byte("three"))

	result, err := batch.CommitDetailed()
	if err != nil {
		t.Fatalf("CommitDetailed error: %v", err)
	}
	if !result.Changed {
		t.Fatalf("CommitDetailed Changed = false, want true")
	}
	if got := tree.Revision(); got != beforeRevision+1 {
		t.Fatalf("Revision after CommitDetailed = %d, want %d", got, beforeRevision+1)
	}
	if len(result.Operations) != 4 {
		t.Fatalf("CommitDetailed operations = %d, want 4", len(result.Operations))
	}

	check := func(index int, kind BatchOperation, key string, existed bool, old string, changed bool) {
		t.Helper()
		got := result.Operations[index]
		if got.Kind != kind || got.Key != key || got.Existed != existed || got.Changed != changed {
			t.Fatalf("operation %d = %+v, want kind=%s key=%s existed=%v changed=%v", index, got, kind, key, existed, changed)
		}
		if old == "" {
			if got.OldValue != nil {
				t.Fatalf("operation %d OldValue = %q, want nil", index, got.OldValue)
			}
			return
		}
		if string(got.OldValue) != old {
			t.Fatalf("operation %d OldValue = %q, want %q", index, got.OldValue, old)
		}
		got.OldValue[0] = 'X'
	}

	check(0, BatchPutOperation, "alpha", true, "one", true)
	check(1, BatchDeleteOperation, "missing", false, "", false)
	check(2, BatchDeleteOperation, "alpha", true, "two", true)
	check(3, BatchPutOperation, "bravo", false, "", true)

	if got, ok := tree.Get("alpha"); ok {
		t.Fatalf("Get(alpha) after CommitDetailed = %q, true; want deleted", got)
	}
	if got, ok := tree.Get("bravo"); !ok || string(got) != "three" {
		t.Fatalf("Get(bravo) after CommitDetailed = %q, %v; want three, true", got, ok)
	}
}

func TestWriteBatchDeleteRangePublishesOneRevision(t *testing.T) {
	tree := New(3)
	for i := 0; i < 12; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	beforeRevision := tree.Revision()
	snapshot := tree.Snapshot()
	defer snapshot.Close()

	batch := tree.Batch()
	batch.DeleteRange("key-04", "key-08")

	for i := 4; i < 8; i++ {
		if got, ok := tree.Get(fmt.Sprintf("key-%02d", i)); !ok || string(got) != fmt.Sprintf("value-%02d", i) {
			t.Fatalf("Get before range Commit key-%02d = %q, %v; want visible old value", i, got, ok)
		}
	}
	result, err := batch.CommitDetailed()
	if err != nil {
		t.Fatalf("CommitDetailed DeleteRange: %v", err)
	}
	if !result.Changed {
		t.Fatalf("DeleteRange Changed = false, want true")
	}
	if got := tree.Revision(); got != beforeRevision+1 {
		t.Fatalf("Revision after DeleteRange = %d, want %d", got, beforeRevision+1)
	}
	if len(result.Operations) != 4 {
		t.Fatalf("DeleteRange operation rows = %d, want 4", len(result.Operations))
	}
	for i := 4; i < 8; i++ {
		key := fmt.Sprintf("key-%02d", i)
		if _, ok := tree.Get(key); ok {
			t.Fatalf("tree still contains %s after DeleteRange", key)
		}
		if got, ok := snapshot.Get(key); !ok || string(got) != fmt.Sprintf("value-%02d", i) {
			t.Fatalf("snapshot Get(%s) after DeleteRange = %q, %v; want old value", key, got, ok)
		}
		row := result.Operations[i-4]
		if row.Kind != BatchDeleteOperation || row.Key != key || !row.Existed || !row.Changed || string(row.OldValue) != fmt.Sprintf("value-%02d", i) {
			t.Fatalf("DeleteRange row %d = %+v, want delete %s old value", i-4, row, key)
		}
	}
	if got, ok := tree.Get("key-03"); !ok || string(got) != "value-03" {
		t.Fatalf("Get(key-03) after DeleteRange = %q, %v; want value-03, true", got, ok)
	}
	if got, ok := tree.Get("key-08"); !ok || string(got) != "value-08" {
		t.Fatalf("Get(key-08) after DeleteRange = %q, %v; want value-08, true", got, ok)
	}
}

func TestWriteBatchDeleteRangeUsesKeysVisibleWhenStaged(t *testing.T) {
	tree := New(3)
	for i := 0; i < 10; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	batch := tree.Batch()
	batch.DeleteRange("key-03", "key-06")
	tree.Put("key-04-extra", []byte("late"))

	if changed := batch.Commit(); !changed {
		t.Fatalf("DeleteRange Commit changed = false, want true")
	}
	if _, ok := tree.Get("key-04-extra"); !ok {
		t.Fatalf("DeleteRange removed key staged after range expansion; want late key still present")
	}
	for i := 3; i < 6; i++ {
		if _, ok := tree.Get(fmt.Sprintf("key-%02d", i)); ok {
			t.Fatalf("DeleteRange left key-%02d present", i)
		}
	}
}

func TestWriteBatchCommitDetailedReturnsExplicitNoOpErrors(t *testing.T) {
	tree := New(2)
	rolledBack := tree.Batch()
	rolledBack.Put("alpha", []byte("one"))
	rolledBack.Rollback()
	if _, err := rolledBack.CommitDetailed(); !errors.Is(err, ErrBatchClosed) {
		t.Fatalf("CommitDetailed after Rollback error = %v, want ErrBatchClosed", err)
	}

	readOnly := &Tree{readOnly: true}
	readOnlyBatch := readOnly.Batch()
	readOnlyBatch.Put("alpha", []byte("one"))
	if _, err := readOnlyBatch.CommitDetailed(); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("CommitDetailed read-only error = %v, want ErrReadOnly", err)
	}

	closed := New(2)
	closed.closed = true
	closedBatch := closed.Batch()
	closedBatch.Put("alpha", []byte("one"))
	if _, err := closedBatch.CommitDetailed(); !errors.Is(err, ErrTreeClosed) {
		t.Fatalf("CommitDetailed closed tree error = %v, want ErrTreeClosed", err)
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

func TestMmapWriteBatchDeleteRangePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 3, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 16; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	beforeRevision := tree.Revision()

	batch := tree.Batch()
	batch.DeleteRange("key-05", "key-10")
	if changed := batch.Commit(); !changed {
		t.Fatalf("DeleteRange Commit changed = false, want true")
	}
	if got := tree.Revision(); got != beforeRevision+1 {
		t.Fatalf("Revision after mmap DeleteRange = %d, want %d", got, beforeRevision+1)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after mmap DeleteRange: %v", err)
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync after mmap DeleteRange: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close after mmap DeleteRange: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()
	for i := 5; i < 10; i++ {
		if _, ok := reopened.Get(fmt.Sprintf("key-%02d", i)); ok {
			t.Fatalf("reopened tree still contains key-%02d", i)
		}
	}
	if got, ok := reopened.Get("key-10"); !ok || string(got) != "value-10" {
		t.Fatalf("reopened Get(key-10) = %q, %v; want value-10, true", got, ok)
	}
}
