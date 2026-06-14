//go:build unix

package pagebtree

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestMmapWriteBatchCommitDetailedRollsBackOnPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 4})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}

	beforeRevision := tree.Revision()
	beforeRoot := tree.root
	beforeNextPage := tree.nextPage
	beforeDirty := len(tree.arena.dirtyPages)
	forced := fmt.Errorf("forced growth panic")
	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == mmapFaultBeforeRemap {
			return forced
		}
		return nil
	}

	batch := tree.Batch()
	for i := 0; i < 32; i++ {
		batch.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	result, err := batch.CommitDetailed()
	if !errors.Is(err, ErrBatchPanic) {
		t.Fatalf("CommitDetailed panic error = %v, want ErrBatchPanic", err)
	}
	if result.Changed {
		t.Fatalf("CommitDetailed Changed after panic = true, want false")
	}
	if got := tree.Revision(); got != beforeRevision {
		t.Fatalf("Revision after batch panic = %d, want %d", got, beforeRevision)
	}
	if tree.root != beforeRoot || tree.nextPage != beforeNextPage {
		t.Fatalf("tree geometry after batch panic = root:%d next:%d, want root:%d next:%d", tree.root, tree.nextPage, beforeRoot, beforeNextPage)
	}
	if got, ok := tree.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get(alpha) after batch panic = %q, %v; want one, true", got, ok)
	}
	if _, ok := tree.Get("key-00"); ok {
		t.Fatalf("Get(key-00) after batch panic = true, want rollback")
	}
	if got := len(tree.arena.dirtyPages); got != beforeDirty {
		t.Fatalf("dirty pages after batch panic = %d, want %d", got, beforeDirty)
	}
}
