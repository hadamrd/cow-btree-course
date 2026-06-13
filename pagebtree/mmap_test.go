//go:build unix

package pagebtree

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestMmapTreePersistsKeysAcrossCloseAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()

	for i := 0; i < 40; i++ {
		got, ok := reopened.Get(fmt.Sprintf("key-%02d", i))
		if !ok {
			t.Fatalf("reopened tree missed key-%02d", i)
		}
		if want := fmt.Sprintf("value-%02d", i); string(got) != want {
			t.Fatalf("reopened Get(key-%02d) = %q, want %q", i, got, want)
		}
	}
	if reopened.Stats().Storage != "mmap" {
		t.Fatalf("Storage = %q, want mmap", reopened.Stats().Storage)
	}
}

func TestMmapReopenDoesNotExpandExistingFileFromMaxPagesHint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 4})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}
	sizeBefore := fileSize(t, path)

	reopened, err := OpenMmap(path, MmapOptions{MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap reopen with larger hint: %v", err)
	}
	defer reopened.Close()

	if got := fileSize(t, path); got != sizeBefore {
		t.Fatalf("file size after reopen with larger MaxPages = %d, want unchanged %d", got, sizeBefore)
	}
	if got, ok := reopened.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("reopened Get(alpha) = %q, %v; want one, true", got, ok)
	}
}

func TestMmapRejectsExistingFileBelowMinimumSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")
	createSparseFile(t, path, PageSize)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with undersized existing file")
	}
	if !errors.Is(err, ErrMetaInvariant) {
		t.Fatalf("OpenMmap undersized file error = %v, want ErrMetaInvariant", err)
	}
	if !strings.Contains(err.Error(), "below minimum") {
		t.Fatalf("OpenMmap undersized file error = %v, want minimum-size detail", err)
	}
}

func TestMmapReadOnlyRejectsExistingFileBelowMinimumSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")
	createSparseFile(t, path, PageSize)

	reader, err := OpenMmapReadOnly(path)
	if err == nil {
		reader.Close()
		t.Fatalf("OpenMmapReadOnly succeeded with undersized existing file")
	}
	if !errors.Is(err, ErrMetaInvariant) {
		t.Fatalf("OpenMmapReadOnly undersized file error = %v, want ErrMetaInvariant", err)
	}
	if !strings.Contains(err.Error(), "below minimum") {
		t.Fatalf("OpenMmapReadOnly undersized file error = %v, want minimum-size detail", err)
	}
}

func TestMmapTreePersistsLargeOverflowValueAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")
	large := bytes.Repeat([]byte("z"), PageSize*2+321)

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("large", large)
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()

	got, ok := reopened.Get("large")
	if !ok {
		t.Fatalf("reopened tree missed large value")
	}
	if !bytes.Equal(got, large) {
		t.Fatalf("reopened large value mismatch: got len %d, want len %d", len(got), len(large))
	}
}

func TestMmapTreePersistsDeleteAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	old, deleted := tree.Delete("key-17")
	if !deleted {
		t.Fatalf("Delete did not report deleting key-17")
	}
	if string(old) != "value-17" {
		t.Fatalf("Delete old value = %q, want value-17", old)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()

	if _, ok := reopened.Get("key-17"); ok {
		t.Fatalf("reopened tree found deleted key-17")
	}
	if reopened.Len() != 39 {
		t.Fatalf("reopened Len = %d, want 39", reopened.Len())
	}
	if got, ok := reopened.Get("key-18"); !ok || string(got) != "value-18" {
		t.Fatalf("reopened Get(key-18) = %q, %v; want value-18, true", got, ok)
	}
}

func TestMmapTreePersistsLeafMergeAfterDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 3, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 12; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	beforeLeaves := make([]PageID, 0)
	collectLeavesInOrder(tree.pages, tree.root, &beforeLeaves)
	if len(beforeLeaves) < 3 {
		t.Fatalf("leaf count before delete = %d, want at least 3 leaves", len(beforeLeaves))
	}

	for _, key := range []string{"key-03", "key-04"} {
		if old, deleted := tree.Delete(key); !deleted || string(old) != "value-"+key[4:] {
			t.Fatalf("Delete(%s) = %q, %v; want matching old value, true", key, old, deleted)
		}
	}
	afterLeaves := make([]PageID, 0)
	collectLeavesInOrder(tree.pages, tree.root, &afterLeaves)
	if len(afterLeaves) != len(beforeLeaves)-1 {
		t.Fatalf("leaf count after merge = %d, want %d; leaves before %v after %v", len(afterLeaves), len(beforeLeaves)-1, beforeLeaves, afterLeaves)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after leaf merge: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()

	if err := reopened.Check(); err != nil {
		t.Fatalf("Check after reopen: %v", err)
	}
	for _, key := range []string{"key-03", "key-04"} {
		if _, ok := reopened.Get(key); ok {
			t.Fatalf("reopened tree found deleted %s", key)
		}
	}
	wantKeys := append(sequentialKeys(3), sequentialKeys(12)[5:]...)
	var gotKeys []string
	reopened.Range(func(key string, value []byte) bool {
		gotKeys = append(gotKeys, key)
		return true
	})
	if !slices.Equal(gotKeys, wantKeys) {
		t.Fatalf("reopened Range after leaf merge = %v, want %v", gotKeys, wantKeys)
	}
}

func TestMmapTreePersistsLeafRedistributionAfterDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 3, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	seedLeafRedistributionTree(tree)

	if old, deleted := tree.Delete("key-05"); !deleted || string(old) != "value-05" {
		t.Fatalf("Delete(key-05) = %q, %v; want value-05, true", old, deleted)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after redistribute: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()

	if err := reopened.Check(); err != nil {
		t.Fatalf("Check after reopen: %v", err)
	}
	if _, ok := reopened.Get("key-05"); ok {
		t.Fatalf("reopened tree found deleted key-05")
	}
	for _, id := range reopened.pages[reopened.root].childIDs() {
		if got := int(reopened.pages[id].slotCount()); got < minKeys(reopened.degree) {
			t.Fatalf("reopened leaf %d has %d keys, want at least %d", id, got, minKeys(reopened.degree))
		}
	}
	wantKeys := append(sequentialKeys(5), "key-06")
	var gotKeys []string
	reopened.Range(func(key string, value []byte) bool {
		gotKeys = append(gotKeys, key)
		return true
	})
	if !slices.Equal(gotKeys, wantKeys) {
		t.Fatalf("reopened Range after redistribute = %v, want %v", gotKeys, wantKeys)
	}
}

func TestMmapTreePersistsBranchMergeAfterDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 3, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	seedBranchMergeAfterDeleteTree(tree)
	if got := tree.Stats().Height; got != 3 {
		t.Fatalf("seeded mmap tree height = %d, want 3", got)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check seeded mmap tree before branch merge: %v", err)
	}

	if old, deleted := tree.Delete("key-08"); !deleted || string(old) != "value-08" {
		t.Fatalf("Delete(key-08) = %q, %v; want value-08, true", old, deleted)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after branch merge: %v", err)
	}
	if got := tree.Stats().Height; got != 2 {
		t.Fatalf("height after branch merge = %d, want 2", got)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()

	if err := reopened.Check(); err != nil {
		t.Fatalf("Check after reopen: %v", err)
	}
	if got := reopened.Stats().Height; got != 2 {
		t.Fatalf("reopened height after branch merge = %d, want 2", got)
	}
	if _, ok := reopened.Get("key-08"); ok {
		t.Fatalf("reopened tree found deleted key-08")
	}
	wantKeys := append(sequentialKeys(8), sequentialKeys(12)[9:]...)
	var gotKeys []string
	reopened.Range(func(key string, value []byte) bool {
		gotKeys = append(gotKeys, key)
		return true
	})
	if !slices.Equal(gotKeys, wantKeys) {
		t.Fatalf("reopened Range after branch merge = %v, want %v", gotKeys, wantKeys)
	}
}

func TestMmapTreePersistsBranchRedistributionAfterDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 3, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	seedBranchRedistributionAfterDeleteTree(tree)
	if err := tree.Check(); err != nil {
		t.Fatalf("Check seeded mmap tree before branch redistribution: %v", err)
	}

	if old, deleted := tree.Delete("key-10"); !deleted || string(old) != "value-10" {
		t.Fatalf("Delete(key-10) = %q, %v; want value-10, true", old, deleted)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after branch redistribution: %v", err)
	}
	if got := tree.Stats().Height; got != 3 {
		t.Fatalf("height after branch redistribution = %d, want 3", got)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()

	if err := reopened.Check(); err != nil {
		t.Fatalf("Check after reopen: %v", err)
	}
	root := reopened.pages[reopened.root]
	for _, child := range root.childIDs() {
		branch := reopened.pages[child]
		if !branch.isBranch() {
			t.Fatalf("reopened root child %d is not a branch", child)
		}
		if got := int(branch.slotCount()); got < minKeys(reopened.degree) {
			t.Fatalf("reopened branch %d has %d keys, want at least %d", child, got, minKeys(reopened.degree))
		}
	}
	if _, ok := reopened.Get("key-10"); ok {
		t.Fatalf("reopened tree found deleted key-10")
	}
	wantKeys := append(sequentialKeys(10), sequentialKeys(16)[11:]...)
	var gotKeys []string
	reopened.Range(func(key string, value []byte) bool {
		gotKeys = append(gotKeys, key)
		return true
	})
	if !slices.Equal(gotKeys, wantKeys) {
		t.Fatalf("reopened Range after branch redistribution = %v, want %v", gotKeys, wantKeys)
	}
}

func TestMmapTreePersistsBranchBorrowBeforeDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 3, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	seedBranchRedistributionAfterDeleteTree(tree)
	if got := branchChildCountsBelowRoot(tree); !slices.Equal(got, []int{5, 3}) {
		t.Fatalf("seeded mmap branch child counts = %v, want [5 3]", got)
	}

	if old, deleted := tree.Delete("key-10"); !deleted || string(old) != "value-10" {
		t.Fatalf("Delete(key-10) = %q, %v; want value-10, true", old, deleted)
	}
	if got := branchChildCountsBelowRoot(tree); !slices.Equal(got, []int{4, 3}) {
		t.Fatalf("mmap branch child counts after borrow = %v, want [4 3]", got)
	}
	if err := tree.Check(); err != nil {
		t.Fatalf("Check after branch borrow: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()

	if got := branchChildCountsBelowRoot(reopened); !slices.Equal(got, []int{4, 3}) {
		t.Fatalf("reopened branch child counts after borrow = %v, want [4 3]", got)
	}
	if err := reopened.Check(); err != nil {
		t.Fatalf("Check after reopen: %v", err)
	}
}

func TestMmapTreeRejectsUnderfullNonRootLeaf(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 3, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	seedUnderfullLeafTree(tree)
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}
	keepOnlyNewestMetaPage(t, path)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with underfull non-root leaf")
	}
	if !errors.Is(err, ErrTreeInvariant) {
		t.Fatalf("OpenMmap underfull leaf error = %v, want ErrTreeInvariant", err)
	}
}

func TestMmapTreeRejectsUnderfullNonRootBranch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 3, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	seedUnderfullBranchTree(tree)
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}
	keepOnlyNewestMetaPage(t, path)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with underfull non-root branch")
	}
	if !errors.Is(err, ErrTreeInvariant) {
		t.Fatalf("OpenMmap underfull branch error = %v, want ErrTreeInvariant", err)
	}
}

func TestMmapCloseWaitsForActiveSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("first Put replaced existing key")
	}

	snapshot := tree.Snapshot()
	err = tree.Close()
	if !errors.Is(err, ErrActiveReaders) {
		t.Fatalf("Close with active snapshot error = %v, want ErrActiveReaders", err)
	}

	if got, ok := tree.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("tree Get after refused Close = %q, %v; want one, true", got, ok)
	}
	if got, ok := snapshot.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("snapshot Get after refused Close = %q, %v; want one, true", got, ok)
	}

	snapshot.Close()
	if err := tree.Close(); err != nil {
		t.Fatalf("Close after snapshot release: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap after close: %v", err)
	}
	defer reopened.Close()
	if got, ok := reopened.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("reopened Get(alpha) = %q, %v; want one, true", got, ok)
	}
}

func TestMmapCloseAfterSyncFailureMarksHandleClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 1024})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("first Put replaced existing key")
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	badPage := PageID(len(tree.arena.data) / PageSize)
	tree.nextPage = badPage + 1
	tree.arena.markDirtyPage(badPage)

	err = tree.Close()
	if err == nil {
		t.Fatalf("Close with invalid dirty page sync succeeded, want error")
	}
	if !tree.closed {
		t.Fatalf("tree.closed after failed close-time Sync = false, want true because arena resources were released")
	}
	if tree.arena.data != nil {
		t.Fatalf("arena data after failed close-time Sync is still retained, want nil")
	}
	if tree.arena.file != nil {
		t.Fatalf("arena file after failed close-time Sync is still retained, want nil")
	}
	if tree.arena.locked {
		t.Fatalf("arena locked after failed close-time Sync = true, want false")
	}
	if stats := tree.Stats(); stats != (Stats{}) {
		t.Fatalf("Stats after failed close-time Sync = %+v, want zero value", stats)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("second Close after failed close-time Sync = %v, want nil", err)
	}
}

func TestMmapSnapshotAfterCloseIsInert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("first Put replaced existing key")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	snapshot := tree.Snapshot()
	if snapshot == nil {
		t.Fatalf("Snapshot after Close returned nil, want inert snapshot")
	}
	if !snapshot.closed {
		t.Fatalf("Snapshot after Close is open, want inert closed snapshot")
	}
	if snapshot.tree != nil {
		t.Fatalf("Snapshot after Close kept tree pointer, want no tree")
	}
	if tree.activeReaderCount() != 0 {
		t.Fatalf("activeReaderCount after post-close Snapshot = %d, want 0", tree.activeReaderCount())
	}
	if snapshot.Len() != 0 {
		t.Fatalf("post-close snapshot Len = %d, want 0", snapshot.Len())
	}
	if got, ok := snapshot.Get("alpha"); ok || got != nil {
		t.Fatalf("post-close snapshot Get = %q, %v; want nil, false", got, ok)
	}
	visited := false
	snapshot.Range(func(string, []byte) bool {
		visited = true
		return true
	})
	if visited {
		t.Fatalf("post-close snapshot Range visited keys")
	}
	if stats := snapshot.Stats(); stats != (Stats{}) {
		t.Fatalf("post-close snapshot Stats = %+v, want zero value", stats)
	}
	snapshot.Close()
	if tree.activeReaderCount() != 0 {
		t.Fatalf("activeReaderCount after inert snapshot Close = %d, want 0", tree.activeReaderCount())
	}
}

func TestMmapClosedTreeMaintenanceAPIsAreNoOps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("first Put replaced existing key")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync after Close = %v, want nil", err)
	}
	if err := tree.Advise(MmapAccessSequential); err != nil {
		t.Fatalf("Advise after Close = %v, want nil", err)
	}
	if err := tree.WarmMmapTree(); err != nil {
		t.Fatalf("WarmMmapTree after Close = %v, want nil", err)
	}
	if err := tree.DropMmapCache(); err != nil {
		t.Fatalf("DropMmapCache after Close = %v, want nil", err)
	}
	stats, err := tree.MmapCacheStats()
	if err != nil {
		t.Fatalf("MmapCacheStats after Close error = %v, want nil", err)
	}
	if stats != (MmapCacheStats{}) {
		t.Fatalf("MmapCacheStats after Close = %+v, want zero value", stats)
	}
}

func TestMmapClosedTreeStatsIsZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("first Put replaced existing key")
	}
	if stats := tree.Stats(); stats.Root == 0 || stats.Storage != "mmap" {
		t.Fatalf("open tree Stats = %+v, want mmap stats with root", stats)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if stats := tree.Stats(); stats != (Stats{}) {
		t.Fatalf("Stats after Close = %+v, want zero value", stats)
	}
}

func TestMmapCloseClearsReleasedArenaResources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("first Put replaced existing key")
	}
	if len(tree.arena.data) == 0 {
		t.Fatalf("open tree has no mmap data")
	}
	if tree.arena.file == nil {
		t.Fatalf("open tree has no mmap file")
	}
	if !tree.arena.locked {
		t.Fatalf("open tree is not marked locked")
	}

	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if tree.arena.data != nil {
		t.Fatalf("arena data after Close is still retained, want nil")
	}
	if tree.arena.file != nil {
		t.Fatalf("arena file after Close is still retained, want nil")
	}
	if tree.arena.locked {
		t.Fatalf("arena locked after Close = true, want false")
	}
	if tree.arena.dirtyPages != nil {
		t.Fatalf("arena dirtyPages after Close = %v, want nil", tree.arena.dirtyPages)
	}
}

func TestMmapTreeGrowsMappingWhenPageCapacityIsExceeded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 4})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	initialPages := tree.arena.maxPages
	for i := 0; i < 80; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if tree.arena.maxPages <= initialPages {
		t.Fatalf("mmap maxPages = %d, want growth beyond initial %d", tree.arena.maxPages, initialPages)
	}
	if got, ok := tree.Get("key-00"); !ok || string(got) != "value-00" {
		t.Fatalf("Get(key-00) after grow = %q, %v; want value-00, true", got, ok)
	}
	if got, ok := tree.Get("key-79"); !ok || string(got) != "value-79" {
		t.Fatalf("Get(key-79) after grow = %q, %v; want value-79, true", got, ok)
	}
	grownPages := tree.arena.maxPages
	if err := tree.Close(); err != nil {
		t.Fatalf("Close grown tree: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat grown file: %v", err)
	}
	if wantMin := int64((grownPages + metaPageCount) * PageSize); info.Size() < wantMin {
		t.Fatalf("grown file size = %d, want at least %d", info.Size(), wantMin)
	}

	reopened, err := OpenMmap(path, MmapOptions{MaxPages: 4})
	if err != nil {
		t.Fatalf("OpenMmap reopen grown file: %v", err)
	}
	defer reopened.Close()
	if reopened.arena.maxPages < grownPages {
		t.Fatalf("reopened maxPages = %d, want at least grown pages %d", reopened.arena.maxPages, grownPages)
	}
	for _, i := range []int{0, 37, 79} {
		key := fmt.Sprintf("key-%02d", i)
		want := fmt.Sprintf("value-%02d", i)
		got, ok := reopened.Get(key)
		if !ok || string(got) != want {
			t.Fatalf("reopened Get(%s) = %q, %v; want %q, true", key, got, ok, want)
		}
	}
}

func TestMmapGrowthMapsReplacementBeforeUnmappingOldMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 4})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("first Put replaced existing key")
	}
	oldData := tree.arena.data
	oldMaxPages := tree.arena.maxPages

	originalMmap := mmapBytes
	originalMunmap := munmapBytes
	defer func() {
		mmapBytes = originalMmap
		munmapBytes = originalMunmap
	}()

	var events []string
	mmapBytes = func(fd int, offset int64, length, prot, flags int) ([]byte, error) {
		events = append(events, "mmap")
		return nil, errors.New("forced replacement mmap failure")
	}
	munmapBytes = func(data []byte) error {
		events = append(events, "munmap")
		return nil
	}

	err = tree.remapMmap(oldMaxPages * 2)
	if err == nil {
		t.Fatalf("remapMmap succeeded with forced replacement mmap failure")
	}
	if got, want := fmt.Sprint(events), "[mmap]"; got != want {
		t.Fatalf("remap events = %s, want %s", got, want)
	}
	if len(tree.arena.data) != len(oldData) || &tree.arena.data[0] != &oldData[0] {
		t.Fatalf("arena data changed after failed remap")
	}
	if tree.arena.maxPages != oldMaxPages {
		t.Fatalf("maxPages after failed remap = %d, want %d", tree.arena.maxPages, oldMaxPages)
	}
	if got, ok := tree.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get after failed remap = %q, %v; want one, true", got, ok)
	}
}

func TestMmapGrowthRestoresFileSizeWhenOldUnmapFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 4})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("first Put replaced existing key")
	}
	oldData := tree.arena.data
	oldMaxPages := tree.arena.maxPages
	oldSize := fileSize(t, path)

	originalMmap := mmapBytes
	originalMunmap := munmapBytes
	defer func() {
		mmapBytes = originalMmap
		munmapBytes = originalMunmap
	}()

	var events []string
	mmapBytes = func(fd int, offset int64, length, prot, flags int) ([]byte, error) {
		events = append(events, "mmap")
		return make([]byte, length), nil
	}
	munmapBytes = func(data []byte) error {
		events = append(events, "munmap")
		return errors.New("forced old munmap failure")
	}

	err = tree.remapMmap(oldMaxPages * 2)
	if err == nil {
		t.Fatalf("remapMmap succeeded with forced old munmap failure")
	}
	if got, want := fmt.Sprint(events), "[mmap munmap munmap]"; got != want {
		t.Fatalf("remap events = %s, want %s", got, want)
	}
	if len(tree.arena.data) != len(oldData) || &tree.arena.data[0] != &oldData[0] {
		t.Fatalf("arena data changed after old munmap failure")
	}
	if tree.arena.maxPages != oldMaxPages {
		t.Fatalf("maxPages after old munmap failure = %d, want %d", tree.arena.maxPages, oldMaxPages)
	}
	if got := fileSize(t, path); got != oldSize {
		t.Fatalf("file size after old munmap failure = %d, want restored size %d", got, oldSize)
	}
	if got, ok := tree.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get after old munmap failure = %q, %v; want one, true", got, ok)
	}
}

func TestMmapShrinkMapsReplacementBeforeUnmappingOldMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("first Put replaced existing key")
	}
	oldData := tree.arena.data
	oldMaxPages := tree.arena.maxPages
	oldSize := fileSize(t, path)

	originalMmap := mmapBytes
	originalMunmap := munmapBytes
	defer func() {
		mmapBytes = originalMmap
		munmapBytes = originalMunmap
	}()

	var events []string
	mmapBytes = func(fd int, offset int64, length, prot, flags int) ([]byte, error) {
		events = append(events, "mmap")
		return nil, errors.New("forced replacement mmap failure")
	}
	munmapBytes = func(data []byte) error {
		events = append(events, "munmap")
		return nil
	}

	err = tree.shrinkMmap(minMmapPageCount)
	if err == nil {
		t.Fatalf("shrinkMmap succeeded with forced replacement mmap failure")
	}
	if got, want := fmt.Sprint(events), "[mmap]"; got != want {
		t.Fatalf("shrink events = %s, want %s", got, want)
	}
	if len(tree.arena.data) != len(oldData) || &tree.arena.data[0] != &oldData[0] {
		t.Fatalf("arena data changed after failed shrink")
	}
	if tree.arena.maxPages != oldMaxPages {
		t.Fatalf("maxPages after failed shrink = %d, want %d", tree.arena.maxPages, oldMaxPages)
	}
	if got := fileSize(t, path); got != oldSize {
		t.Fatalf("file size after failed shrink = %d, want restored size %d", got, oldSize)
	}
	if got, ok := tree.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get after failed shrink = %q, %v; want one, true", got, ok)
	}
}

func TestMmapTreeGrowsMappingForOverflowPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")
	large := bytes.Repeat([]byte("x"), PageSize*9+123)

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 4})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	initialPages := tree.arena.maxPages
	tree.Put("small", []byte("before-grow"))
	tree.Put("large", large)
	if tree.arena.maxPages <= initialPages {
		t.Fatalf("mmap maxPages = %d, want growth beyond initial %d", tree.arena.maxPages, initialPages)
	}
	if got, ok := tree.Get("small"); !ok || string(got) != "before-grow" {
		t.Fatalf("Get(small) after overflow grow = %q, %v; want before-grow, true", got, ok)
	}
	if got, ok := tree.Get("large"); !ok || !bytes.Equal(got, large) {
		t.Fatalf("Get(large) after overflow grow len = %d, %v; want len %d, true", len(got), ok, len(large))
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close grown overflow tree: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{MaxPages: 4})
	if err != nil {
		t.Fatalf("OpenMmap reopen grown overflow file: %v", err)
	}
	defer reopened.Close()
	if got, ok := reopened.Get("large"); !ok || !bytes.Equal(got, large) {
		t.Fatalf("reopened Get(large) len = %d, %v; want len %d, true", len(got), ok, len(large))
	}
}

func TestMmapGrowthSyncsDirectoryAfterTruncate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 4})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	var syncedDirs []string
	tree.arena.dirSyncObserver = func(path string) {
		syncedDirs = append(syncedDirs, path)
	}

	for i := 0; i < 80; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if len(syncedDirs) == 0 {
		t.Fatalf("growth directory syncs = 0, want at least one sync after file growth")
	}
	for _, synced := range syncedDirs {
		if synced != filepath.Dir(path) {
			t.Fatalf("growth synced directory %q, want %q", synced, filepath.Dir(path))
		}
	}
}

func TestMmapCreateSyncsParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	var syncedDirs []string
	oldSyncDirectoryPath := syncDirectoryPath
	syncDirectoryPath = func(path string) error {
		syncedDirs = append(syncedDirs, path)
		return nil
	}
	defer func() {
		syncDirectoryPath = oldSyncDirectoryPath
	}()

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 4})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	if got, want := syncedDirs, []string{filepath.Dir(path)}; !slices.Equal(got, want) {
		t.Fatalf("creation directory syncs = %v, want %v", got, want)
	}
}

func TestMmapTreePersistsLeafNextLinksAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if got, want := leafChainKeys(tree.pages, tree.root), sequentialKeys(40); !slices.Equal(got, want) {
		t.Fatalf("leaf chain before reopen = %v, want %v", got, want)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()
	if got, want := leafChainKeys(reopened.pages, reopened.root), sequentialKeys(40); !slices.Equal(got, want) {
		t.Fatalf("leaf chain after reopen = %v, want %v", got, want)
	}
}

func TestMmapTreeRejectsLeafNextLinkThatSkipsReachableLeaf(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	leafIDs := make([]PageID, 0)
	collectLeavesInOrder(tree.pages, tree.root, &leafIDs)
	if len(leafIDs) < 2 {
		t.Fatalf("leaf count = %d, want at least 2 leaves", len(leafIDs))
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	corruptLeafNext(t, path, leafIDs[0], 0)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with corrupt leaf next link")
	}
	if !errors.Is(err, ErrTreeInvariant) {
		t.Fatalf("OpenMmap leaf link error = %v, want ErrTreeInvariant", err)
	}
}

func TestMmapRangeAdvisesNextLeafPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64, AccessPattern: MmapAccessRandom})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	leafIDs := make([]PageID, 0)
	collectLeavesInOrder(tree.pages, tree.root, &leafIDs)
	leafSet := map[PageID]bool{}
	for _, id := range leafIDs {
		leafSet[id] = true
	}

	var advised []pageRange
	tree.arena.adviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern != MmapAccessWillNeed {
			return
		}
		advised = append(advised, pageRange{start: start, end: end})
	}

	var got []string
	tree.Range(func(key string, value []byte) bool {
		got = append(got, key)
		return true
	})

	if !slices.Equal(got, sequentialKeys(40)) {
		t.Fatalf("Range keys = %v, want sequential keys", got)
	}
	if len(advised) == 0 {
		t.Fatalf("Range did not advise any next leaf pages")
	}
	for _, r := range advised {
		for id := r.start; id < r.end; id++ {
			if !leafSet[id] {
				t.Fatalf("Range advised page %d in range [%d,%d), want only leaf pages from %v", id, r.start, r.end, leafIDs)
			}
		}
	}
	if advised[0].start != leafIDs[1] {
		t.Fatalf("first advised range starts at page %d, want second leaf page %d", advised[0].start, leafIDs[1])
	}
}

func TestMmapRangeAvoidsLeafPrefetchWhileReadersAreActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64, AccessPattern: MmapAccessRandom})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	snapshot := tree.Snapshot()
	defer snapshot.Close()

	tree.Put("key-99", []byte("new-right-edge"))

	var advised []PageID
	tree.arena.adviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern == MmapAccessWillNeed {
			advised = append(advised, start)
		}
	}

	var got []string
	tree.Range(func(key string, value []byte) bool {
		got = append(got, key)
		return true
	})

	want := append(sequentialKeys(40), "key-99")
	if !slices.Equal(got, want) {
		t.Fatalf("Range with active reader = %v, want current keys %v", got, want)
	}
	if len(advised) != 0 {
		t.Fatalf("Range advised leaf pages with active reader: %v", advised)
	}
}

func TestMmapRangeFromStartsAtLowerBoundAndPrefetchesNextLeaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64, AccessPattern: MmapAccessRandom})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var advised []pageRange
	tree.arena.adviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern == MmapAccessWillNeed {
			advised = append(advised, pageRange{start: start, end: end})
		}
	}

	var got []string
	tree.RangeFrom("key-17", func(key string, value []byte) bool {
		got = append(got, key)
		return true
	})

	want := sequentialKeys(40)[17:]
	if !slices.Equal(got, want) {
		t.Fatalf("RangeFrom(key-17) = %v, want %v", got, want)
	}
	if len(advised) == 0 {
		t.Fatalf("RangeFrom did not advise any next leaf pages")
	}

	startLeaf := leafForKey(tree.pages, tree.root, "key-17")
	if startLeaf == 0 {
		t.Fatalf("leafForKey(key-17) returned 0")
	}
	wantFirstAdvice := tree.pages[startLeaf].nextLeaf()
	if wantFirstAdvice == 0 {
		t.Fatalf("start leaf %d has no next leaf; test needs multiple leaves", startLeaf)
	}
	if advised[0].start != wantFirstAdvice {
		t.Fatalf("first advised range starts at page %d, want next leaf %d after start leaf %d", advised[0].start, wantFirstAdvice, startLeaf)
	}
}

func TestMmapRangePrefetchWindowCanBeSized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64, RangePrefetchLeafWindow: 1})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var advised []pageRange
	tree.arena.adviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern == MmapAccessWillNeed {
			advised = append(advised, pageRange{start: start, end: end})
		}
	}

	tree.RangeFrom("key-17", func(key string, value []byte) bool {
		return false
	})

	if len(advised) != 1 {
		t.Fatalf("advised ranges = %+v, want exactly 1 hint from configured range prefetch window", advised)
	}
	if got := advised[0].end - advised[0].start; got != 1 {
		t.Fatalf("advised range = [%d,%d), want exactly 1 page from configured range prefetch window", advised[0].start, advised[0].end)
	}
	stats := tree.Stats()
	if stats.RangePrefetchLeafWindow != 1 {
		t.Fatalf("RangePrefetchLeafWindow = %d, want 1", stats.RangePrefetchLeafWindow)
	}
	if stats.RangePrefetchHints != 1 {
		t.Fatalf("RangePrefetchHints = %d, want 1", stats.RangePrefetchHints)
	}
}

func TestMmapRangePrefetchStatsCountPagesCovered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64, RangePrefetchLeafWindow: 2})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var hintCalls int
	var pagesCovered int
	tree.arena.adviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern == MmapAccessWillNeed {
			hintCalls++
			pagesCovered += int(end - start)
		}
	}

	tree.RangeFrom("key-17", func(key string, value []byte) bool {
		return false
	})

	stats := tree.Stats()
	if stats.RangePrefetchHints != hintCalls {
		t.Fatalf("RangePrefetchHints = %d, want observed hint calls %d", stats.RangePrefetchHints, hintCalls)
	}
	if stats.RangePrefetchPages != pagesCovered {
		t.Fatalf("RangePrefetchPages = %d, want observed pages covered %d", stats.RangePrefetchPages, pagesCovered)
	}
	if stats.RangePrefetchPages < stats.RangePrefetchHints {
		t.Fatalf("RangePrefetchPages = %d, RangePrefetchHints = %d; pages covered cannot be less than hint calls", stats.RangePrefetchPages, stats.RangePrefetchHints)
	}
}

func TestMmapRangePrefetchCanBeDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64, RangePrefetchLeafWindow: -1})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var advised []PageID
	tree.arena.adviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern == MmapAccessWillNeed {
			advised = append(advised, start)
		}
	}

	tree.RangeFrom("key-17", func(key string, value []byte) bool {
		return true
	})

	if len(advised) != 0 {
		t.Fatalf("advised pages = %v, want no leaf prefetch hints when disabled", advised)
	}
	stats := tree.Stats()
	if stats.RangePrefetchLeafWindow != 0 {
		t.Fatalf("RangePrefetchLeafWindow = %d, want disabled window 0", stats.RangePrefetchLeafWindow)
	}
	if stats.RangePrefetchHints != 0 {
		t.Fatalf("RangePrefetchHints = %d, want no prefetch hints when disabled", stats.RangePrefetchHints)
	}
}

func TestMmapRangeBetweenStopsBeforeEndAndBoundsPrefetch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64, AccessPattern: MmapAccessRandom})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var advised []pageRange
	tree.arena.adviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern == MmapAccessWillNeed {
			advised = append(advised, pageRange{start: start, end: end})
		}
	}

	var got []string
	tree.RangeBetween("key-17", "key-23", func(key string, value []byte) bool {
		got = append(got, key)
		return true
	})

	want := sequentialKeys(40)[17:23]
	if !slices.Equal(got, want) {
		t.Fatalf("RangeBetween(key-17,key-23) = %v, want %v", got, want)
	}
	for _, r := range advised {
		for id := r.start; id < r.end; id++ {
			first, ok := firstLeafKey(tree.pages[id])
			if !ok {
				t.Fatalf("advised page %d in range [%d,%d) is not a non-empty leaf", id, r.start, r.end)
			}
			if first >= "key-23" {
				t.Fatalf("advised leaf page %d starts at %s, want prefetch strictly before end key-23", id, first)
			}
		}
	}
}

func TestMmapTreePageCacheCapacityBoundsBranchRoutingEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128, PageCacheCapacity: 1})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
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
		t.Fatalf("PageCacheEntries = %d, want bounded mmap cache to keep 1 entry", stats.PageCacheEntries)
	}
	if stats.PageCacheEvictions == 0 {
		t.Fatalf("PageCacheEvictions = 0, want eviction after visiting multiple branch pages")
	}
}

func TestMmapCompactTruncatesTrailingFreePagesAndPersistsNextPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 12; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	firstTail := appendFreeTailPage(t, tree)
	appendFreeTailPage(t, tree)
	appendFreeTailPage(t, tree)
	beforeNext := tree.nextPage
	beforeSize := fileSize(t, path)

	if err := tree.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	afterSize := fileSize(t, path)
	if afterSize >= beforeSize {
		t.Fatalf("file size after Compact = %d, want less than %d", afterSize, beforeSize)
	}
	if tree.nextPage != firstTail {
		t.Fatalf("nextPage after Compact = %d, want %d", tree.nextPage, firstTail)
	}
	if tree.nextPage >= beforeNext {
		t.Fatalf("nextPage after Compact = %d, want less than previous %d", tree.nextPage, beforeNext)
	}
	for _, id := range tree.free {
		if id >= tree.nextPage {
			t.Fatalf("free page %d remains beyond compacted nextPage %d", id, tree.nextPage)
		}
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close compacted tree: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen compacted tree: %v", err)
	}
	defer reopened.Close()
	if reopened.nextPage != firstTail {
		t.Fatalf("reopened nextPage = %d, want %d", reopened.nextPage, firstTail)
	}
	for i := 0; i < 12; i++ {
		key := fmt.Sprintf("key-%02d", i)
		if got, ok := reopened.Get(key); !ok || string(got) != fmt.Sprintf("value-%02d", i) {
			t.Fatalf("reopened Get(%s) = %q, %v", key, got, ok)
		}
	}
}

func TestMmapCompactRestoresStateWhenMetaFlushFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	for i := 0; i < 12; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	firstTail := appendFreeTailPage(t, tree)
	appendFreeTailPage(t, tree)
	beforeNext := tree.nextPage
	beforeMaxPages := tree.arena.maxPages
	beforeFree := append([]PageID(nil), tree.free...)
	clear(tree.arena.dirtyPages)

	metaIndex := int(tree.Revision() % metaPageCount)
	metaPage := tree.arena.data[metaIndex*PageSize : (metaIndex+1)*PageSize]
	beforeMeta := cloneBytes(metaPage)

	if err := tree.arena.file.Close(); err != nil {
		t.Fatalf("Close backing file before forced compact meta sync failure: %v", err)
	}

	if err := tree.Compact(); err == nil {
		t.Fatalf("Compact succeeded with closed backing file")
	}
	if !bytes.Equal(metaPage, beforeMeta) {
		t.Fatalf("metadata page changed after failed compact metadata sync")
	}
	if tree.nextPage != beforeNext {
		t.Fatalf("nextPage after failed Compact = %d, want restored %d", tree.nextPage, beforeNext)
	}
	if tree.arena.maxPages != beforeMaxPages {
		t.Fatalf("maxPages after failed Compact = %d, want restored %d", tree.arena.maxPages, beforeMaxPages)
	}
	if !slices.Equal(tree.free, beforeFree) {
		t.Fatalf("free list after failed Compact = %v, want restored %v", tree.free, beforeFree)
	}
	if tree.pages[firstTail] == nil {
		t.Fatalf("tail page %d removed from page map after failed Compact", firstTail)
	}
}

func TestMmapCompactTruncatesUnusedMappedCapacity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	for i := 0; i < 12; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	beforeSize := fileSize(t, path)
	beforeNext := tree.nextPage
	var syncedDirs []string
	tree.arena.dirSyncObserver = func(path string) {
		syncedDirs = append(syncedDirs, path)
	}

	if err := tree.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	afterSize := fileSize(t, path)
	if afterSize >= beforeSize {
		t.Fatalf("file size after Compact = %d, want less than %d", afterSize, beforeSize)
	}
	if tree.nextPage != beforeNext {
		t.Fatalf("nextPage after capacity-only Compact = %d, want unchanged %d", tree.nextPage, beforeNext)
	}
	wantSize := int64((int(tree.nextPage-firstTreePageID) + metaPageCount) * PageSize)
	minSize := int64((minMmapPageCount + metaPageCount) * PageSize)
	if wantSize < minSize {
		wantSize = minSize
	}
	if afterSize != wantSize {
		t.Fatalf("file size after Compact = %d, want %d", afterSize, wantSize)
	}
	if got, want := syncedDirs, []string{filepath.Dir(path)}; !slices.Equal(got, want) {
		t.Fatalf("compact directory syncs = %v, want %v", got, want)
	}
}

func TestMmapCompactWaitsForActiveReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	for i := 0; i < 12; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	appendFreeTailPage(t, tree)
	appendFreeTailPage(t, tree)
	beforeNext := tree.nextPage
	beforeSize := fileSize(t, path)

	snapshot := tree.Snapshot()
	if err := tree.Compact(); err != nil {
		t.Fatalf("Compact with active reader: %v", err)
	}
	if tree.nextPage != beforeNext {
		t.Fatalf("nextPage with active reader = %d, want unchanged %d", tree.nextPage, beforeNext)
	}
	if got := fileSize(t, path); got != beforeSize {
		t.Fatalf("file size with active reader = %d, want unchanged %d", got, beforeSize)
	}

	snapshot.Close()
	if err := tree.Compact(); err != nil {
		t.Fatalf("Compact after reader close: %v", err)
	}
	if tree.nextPage >= beforeNext {
		t.Fatalf("nextPage after reader close = %d, want less than %d", tree.nextPage, beforeNext)
	}
	if got := fileSize(t, path); got >= beforeSize {
		t.Fatalf("file size after reader close = %d, want less than %d", got, beforeSize)
	}
}

func TestMmapSyncFlushesDataPagesBeforePublishingMeta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	tree.Put("alpha", []byte("one"))
	metaIndex := int(tree.Revision() % metaPageCount)
	if record, ok := readMetaPage(tree.arena.data[metaIndex*PageSize : (metaIndex+1)*PageSize]); ok && record.revision == tree.Revision() {
		t.Fatalf("metadata page for revision %d was published before Sync", tree.Revision())
	}

	var events []string
	tree.arena.syncObserver = func(event string) {
		events = append(events, event)
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	want := []string{"data", "meta"}
	if !slices.Equal(events, want) {
		t.Fatalf("sync events = %v, want %v", events, want)
	}
	record, ok := readMetaPage(tree.arena.data[metaIndex*PageSize : (metaIndex+1)*PageSize])
	if !ok {
		t.Fatalf("metadata page %d is not valid after Sync", metaIndex)
	}
	if record.revision != tree.Revision() {
		t.Fatalf("metadata revision after Sync = %d, want %d", record.revision, tree.Revision())
	}
}

func TestMmapSyncRestoresMetaPageWhenMetaFlushFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}

	tree.Put("bravo", []byte("two"))
	clear(tree.arena.dirtyPages)
	metaIndex := int(tree.Revision() % metaPageCount)
	metaPage := tree.arena.data[metaIndex*PageSize : (metaIndex+1)*PageSize]
	before := cloneBytes(metaPage)

	if err := tree.arena.file.Close(); err != nil {
		t.Fatalf("Close backing file before forced meta sync failure: %v", err)
	}

	if err := tree.Sync(); err == nil {
		t.Fatalf("Sync succeeded with closed backing file")
	}
	if !bytes.Equal(metaPage, before) {
		t.Fatalf("metadata page changed after failed metadata sync")
	}
	if record, ok := readMetaPage(metaPage); ok && record.revision == tree.Revision() {
		t.Fatalf("failed metadata sync left revision %d readable in mapped metadata page", record.revision)
	}
}

func TestMmapSyncSpillsFreelistTooLargeForMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 1024})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer func() {
		tree.free = nil
		_ = tree.Close()
	}()

	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	clear(tree.arena.dirtyPages)

	metaIndex := int(tree.Revision() % metaPageCount)
	metaPage := tree.arena.data[metaIndex*PageSize : (metaIndex+1)*PageSize]
	tree.free = make([]PageID, maxMetaFreePages+1)
	for i := range tree.free {
		tree.free[i] = firstTreePageID + 1 + PageID(i)
	}
	tree.nextPage = firstTreePageID + 1 + PageID(len(tree.free))

	var syncErr error
	var panicValue any
	func() {
		defer func() {
			panicValue = recover()
		}()
		syncErr = tree.Sync()
	}()
	if panicValue != nil {
		t.Fatalf("Sync panicked for oversized freelist: %v", panicValue)
	}
	if syncErr != nil {
		t.Fatalf("Sync oversized freelist error = %v, want nil", syncErr)
	}
	record, ok := readMetaPage(metaPage)
	if !ok {
		t.Fatalf("metadata page is not readable after oversized freelist Sync")
	}
	if record.freeCount != len(tree.free) {
		t.Fatalf("metadata free count = %d, want %d", record.freeCount, len(tree.free))
	}
	if record.freeRoot == 0 {
		t.Fatalf("metadata free root = 0, want freelist page root for oversized freelist")
	}
}

func TestMmapSyncPersistsFreelistLargerThanMetadataPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 2048})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("first Put replaced existing key")
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	clear(tree.arena.dirtyPages)

	freeCount := maxMetaFreePages + 17
	tree.free = make([]PageID, freeCount)
	for i := range tree.free {
		tree.free[i] = firstTreePageID + 1 + PageID(i)
	}
	tree.nextPage = firstTreePageID + 1 + PageID(freeCount)

	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync with large freelist: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close after large freelist Sync: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()

	if got := reopened.Stats().FreePages; got != freeCount {
		t.Fatalf("FreePages after reopen = %d, want %d", got, freeCount)
	}
	allocatedBeforeReuse := reopened.Stats().AllocatedPages
	reopened.Put("bravo", []byte("two"))
	afterReuse := reopened.Stats()
	if afterReuse.ReusedPages == 0 {
		t.Fatalf("ReusedPages after reopened write = 0, want persisted large freelist reuse")
	}
	if afterReuse.AllocatedPages > allocatedBeforeReuse+1 {
		t.Fatalf("AllocatedPages grew from %d to %d despite persisted large freelist", allocatedBeforeReuse, afterReuse.AllocatedPages)
	}
}

func TestMmapSyncReclaimsObsoleteFreelistPagesAfterBothMetaPagesAdvance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 4096})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("first Put replaced existing key")
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	clear(tree.arena.dirtyPages)

	freeCount := maxMetaFreePages + 17
	tree.free = make([]PageID, freeCount)
	for i := range tree.free {
		tree.free[i] = firstTreePageID + 1 + PageID(i)
	}
	tree.nextPage = firstTreePageID + 1 + PageID(freeCount)

	if err := tree.Sync(); err != nil {
		t.Fatalf("first large-freelist Sync: %v", err)
	}
	firstGeneration := append([]PageID(nil), tree.metaFreelistPages...)
	if len(firstGeneration) == 0 {
		t.Fatalf("first large-freelist Sync did not create freelist pages")
	}

	tree.Put("bravo", []byte("two"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("second large-freelist Sync: %v", err)
	}
	for _, id := range firstGeneration {
		if slices.Contains(tree.free, id) {
			t.Fatalf("freelist page %d became reusable while older metadata can still reference it", id)
		}
	}

	tree.Put("charlie", []byte("three"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("third large-freelist Sync: %v", err)
	}
	for _, id := range firstGeneration {
		if !slices.Contains(tree.free, id) {
			t.Fatalf("obsolete freelist page %d was not reclaimed after both metadata pages advanced", id)
		}
	}
}

func TestMmapSyncFlushesOnlyDirtyDataPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	tree.Put("alpha", []byte("one"))
	var flushed []PageID
	tree.arena.dataSyncObserver = func(start, end PageID) {
		for id := start; id < end; id++ {
			flushed = append(flushed, id)
		}
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync first write: %v", err)
	}
	if !slices.Equal(flushed, []PageID{tree.root}) {
		t.Fatalf("flushed data pages after first write = %v, want only root page %d", flushed, tree.root)
	}

	flushed = nil
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync without writes: %v", err)
	}
	if len(flushed) != 0 {
		t.Fatalf("flushed data pages without writes = %v, want none", flushed)
	}
}

func TestMmapAccessAdviceKeepsReadsWorking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64, AccessPattern: MmapAccessRandom})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	for _, pattern := range []MmapAccessPattern{MmapAccessRandom, MmapAccessSequential, MmapAccessWillNeed, MmapAccessDefault, MmapAccessNormal} {
		if err := tree.Advise(pattern); err != nil {
			t.Fatalf("Advise(%v): %v", pattern, err)
		}
		got, ok := tree.Get("alpha")
		if !ok || string(got) != "one" {
			t.Fatalf("Get(alpha) after Advise(%v) = %q, %v; want one, true", pattern, got, ok)
		}
	}

	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestMmapAdviseAlsoAdvisesBackingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64, AccessPattern: MmapAccessRandom})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	var advised []pageRange
	tree.arena.fileAdviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern == MmapAccessSequential {
			advised = append(advised, pageRange{start: start, end: end})
		}
	}
	if err := tree.Advise(MmapAccessSequential); err != nil {
		t.Fatalf("Advise(sequential): %v", err)
	}

	want := []pageRange{{start: 0, end: PageID(len(tree.arena.data) / PageSize)}}
	if !slices.Equal(advised, want) {
		t.Fatalf("file advice ranges = %+v, want %+v", advised, want)
	}
}

func TestMmapDefaultsToRandomAccessAdvice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	if tree.arena.accessPattern != MmapAccessRandom {
		t.Fatalf("default access pattern = %v, want MmapAccessRandom", tree.arena.accessPattern)
	}
}

func TestMmapCanOptIntoNormalKernelAccessAdvice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64, AccessPattern: MmapAccessNormal})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	if tree.arena.accessPattern != MmapAccessNormal {
		t.Fatalf("explicit normal access pattern = %v, want MmapAccessNormal", tree.arena.accessPattern)
	}
}

func TestMmapReadOnlyAccessAdviceKeepsReadsWorking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	writer, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	writer.Put("alpha", []byte("one"))
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	reader, err := OpenMmapReadOnly(path)
	if err != nil {
		t.Fatalf("OpenMmapReadOnly: %v", err)
	}
	defer reader.Close()

	if err := reader.Advise(MmapAccessRandom); err != nil {
		t.Fatalf("read-only Advise(random): %v", err)
	}
	got, ok := reader.Get("alpha")
	if !ok || string(got) != "one" {
		t.Fatalf("read-only Get(alpha) after advice = %q, %v; want one, true", got, ok)
	}
}

func TestMmapReadOnlyDefaultsToRandomAccessAdvice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	writer, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	writer.Put("alpha", []byte("one"))
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	reader, err := OpenMmapReadOnly(path)
	if err != nil {
		t.Fatalf("OpenMmapReadOnly: %v", err)
	}
	defer reader.Close()

	if reader.arena.accessPattern != MmapAccessRandom {
		t.Fatalf("read-only default access pattern = %v, want MmapAccessRandom", reader.arena.accessPattern)
	}
}

func TestMmapDropCacheSyncsBeforeDontNeedAdvice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	for i := 0; i < 20; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var events []string
	tree.arena.syncObserver = func(event string) {
		events = append(events, event)
	}
	tree.arena.adviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern == mmapAccessDontNeed {
			events = append(events, fmt.Sprintf("drop:%d-%d", start, end))
		}
	}

	if err := tree.DropMmapCache(); err != nil {
		t.Fatalf("DropMmapCache: %v", err)
	}
	want := []string{"data", "meta", fmt.Sprintf("drop:%d-%d", firstTreePageID, tree.nextPage)}
	if !slices.Equal(events, want) {
		t.Fatalf("DropMmapCache events = %v, want %v", events, want)
	}
	if len(tree.arena.dirtyPages) != 0 {
		t.Fatalf("dirty pages after DropMmapCache = %v, want none", tree.arena.dirtyPages)
	}
	if got, ok := tree.Get("key-09"); !ok || string(got) != "value-09" {
		t.Fatalf("Get after DropMmapCache = %q, %v; want value-09, true", got, ok)
	}
}

func TestMmapDropCacheAdvisesBackingFileAfterSync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()
	for i := 0; i < 20; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var events []string
	tree.arena.syncObserver = func(event string) {
		events = append(events, event)
	}
	tree.arena.fileAdviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern == mmapAccessDontNeed {
			events = append(events, fmt.Sprintf("file-drop:%d-%d", start, end))
		}
	}

	if err := tree.DropMmapCache(); err != nil {
		t.Fatalf("DropMmapCache: %v", err)
	}
	want := []string{"data", "meta", fmt.Sprintf("file-drop:%d-%d", firstTreePageID, tree.nextPage)}
	if !slices.Equal(events, want) {
		t.Fatalf("DropMmapCache events = %v, want %v", events, want)
	}
}

func TestMmapWarmTreeAdvisesOnlyReachablePages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 256})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	defer tree.Close()

	for i := 0; i < 48; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	large := bytes.Repeat([]byte("x"), PageSize*2+77)
	tree.Put("large", large)
	for i := 0; i < 8; i++ {
		if _, deleted := tree.Delete(fmt.Sprintf("key-%02d", i)); !deleted {
			t.Fatalf("Delete(key-%02d) = false, want true", i)
		}
	}
	if len(tree.free) == 0 {
		t.Fatalf("test setup produced no reusable pages")
	}

	var advised []pageRange
	tree.arena.adviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern == MmapAccessWillNeed {
			advised = append(advised, pageRange{start: start, end: end})
		}
	}

	if err := tree.WarmMmapTree(); err != nil {
		t.Fatalf("WarmMmapTree: %v", err)
	}
	if len(advised) == 0 {
		t.Fatalf("WarmMmapTree issued no WILLNEED advice")
	}

	warmed := map[PageID]bool{}
	for _, r := range advised {
		for id := r.start; id < r.end; id++ {
			warmed[id] = true
		}
	}
	for _, id := range tree.free {
		if warmed[id] {
			t.Fatalf("WarmMmapTree advised reusable page %d from free list %v", id, tree.free)
		}
	}
	stats := tree.Stats()
	if len(warmed) != stats.Pages {
		t.Fatalf("WarmMmapTree advised %d pages, want reachable page count %d", len(warmed), stats.Pages)
	}
	if stats.MmapWarmupHints != len(advised) {
		t.Fatalf("MmapWarmupHints = %d, want observed hint calls %d", stats.MmapWarmupHints, len(advised))
	}
	if stats.MmapWarmupPages != len(warmed) {
		t.Fatalf("MmapWarmupPages = %d, want observed warmed pages %d", stats.MmapWarmupPages, len(warmed))
	}
	if got, ok := tree.Get("large"); !ok || !bytes.Equal(got, large) {
		t.Fatalf("Get(large) after WarmMmapTree len = %d, %v; want len %d, true", len(got), ok, len(large))
	}
}

func TestMmapReadOnlyDropCacheSkipsSyncAndAdvisesPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	reader, err := OpenMmapReadOnly(path)
	if err != nil {
		t.Fatalf("OpenMmapReadOnly: %v", err)
	}
	defer reader.Close()

	var events []string
	reader.arena.syncObserver = func(event string) {
		events = append(events, event)
	}
	reader.arena.adviceObserver = func(pattern MmapAccessPattern, start, end PageID) {
		if pattern == mmapAccessDontNeed {
			events = append(events, fmt.Sprintf("drop:%d-%d", start, end))
		}
	}

	if err := reader.DropMmapCache(); err != nil {
		t.Fatalf("read-only DropMmapCache: %v", err)
	}
	want := []string{fmt.Sprintf("drop:%d-%d", firstTreePageID, reader.nextPage)}
	if !slices.Equal(events, want) {
		t.Fatalf("read-only DropMmapCache events = %v, want %v", events, want)
	}
	if got, ok := reader.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("read-only Get after DropMmapCache = %q, %v; want one, true", got, ok)
	}
}

func TestMmapCacheStatsReportsKernelResidency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64, AccessPattern: MmapAccessRandom})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	defer tree.Close()

	for i := 0; i < 20; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got, ok := tree.Get("key-10"); !ok || string(got) != "value-10" {
		t.Fatalf("Get(key-10) = %q, %v; want value-10, true", got, ok)
	}

	stats, err := tree.MmapCacheStats()
	if err != nil {
		t.Fatalf("MmapCacheStats: %v", err)
	}
	wantMappedBytes := (64 + metaPageCount) * PageSize
	if stats.MappedBytes != wantMappedBytes {
		t.Fatalf("MappedBytes = %d, want %d", stats.MappedBytes, wantMappedBytes)
	}
	if stats.MappedDatabasePages != 64+metaPageCount {
		t.Fatalf("MappedDatabasePages = %d, want %d", stats.MappedDatabasePages, 64+metaPageCount)
	}
	if stats.OSPageSize != unix.Getpagesize() {
		t.Fatalf("OSPageSize = %d, want %d", stats.OSPageSize, unix.Getpagesize())
	}
	wantOSPages := (stats.MappedBytes + stats.OSPageSize - 1) / stats.OSPageSize
	if stats.OSPages != wantOSPages {
		t.Fatalf("OSPages = %d, want %d", stats.OSPages, wantOSPages)
	}
	if stats.ResidentOSPages <= 0 {
		t.Fatalf("ResidentOSPages = %d, want at least one resident mapped page", stats.ResidentOSPages)
	}
	if stats.ResidentOSPages > stats.OSPages {
		t.Fatalf("ResidentOSPages = %d, want <= OSPages %d", stats.ResidentOSPages, stats.OSPages)
	}
}

func TestMmapReadOnlyCacheStatsReportsKernelResidency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	writer, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	writer.Put("alpha", []byte("one"))
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	reader, err := OpenMmapReadOnly(path)
	if err != nil {
		t.Fatalf("OpenMmapReadOnly: %v", err)
	}
	defer reader.Close()
	if got, ok := reader.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get(alpha) = %q, %v; want one, true", got, ok)
	}

	stats, err := reader.MmapCacheStats()
	if err != nil {
		t.Fatalf("read-only MmapCacheStats: %v", err)
	}
	if stats.MappedBytes == 0 || stats.OSPages == 0 {
		t.Fatalf("read-only cache stats = %+v, want mapped pages", stats)
	}
}

func TestMemoryTreeMmapCacheStatsIsEmpty(t *testing.T) {
	stats, err := New(2).MmapCacheStats()
	if err != nil {
		t.Fatalf("memory MmapCacheStats: %v", err)
	}
	if stats != (MmapCacheStats{}) {
		t.Fatalf("memory cache stats = %+v, want zero stats", stats)
	}
}

func TestMmapTreeStoresSlottedPageBytesInFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 4, MaxPages: 16})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	tree.Put("bravo", []byte("two"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if want := int64((16 + metaPageCount) * PageSize); info.Size() != want {
		t.Fatalf("file size = %d, want %d", info.Size(), want)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(raw, []byte("alphaone")) {
		t.Fatalf("mmap file does not contain slotted leaf cell bytes for alpha/one")
	}
	if !bytes.Contains(raw, []byte("bravotwo")) {
		t.Fatalf("mmap file does not contain slotted leaf cell bytes for bravo/two")
	}
}

func TestMmapTreeFallsBackToOlderValidMetaPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync older root: %v", err)
	}
	tree.Put("bravo", []byte("two"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	corruptMetaPage(t, path, 0)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen after corrupt latest meta: %v", err)
	}
	defer reopened.Close()

	if got, ok := reopened.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("alpha after fallback = %q, %v; want one, true", got, ok)
	}
	if got, ok := reopened.Get("bravo"); ok {
		t.Fatalf("bravo should be absent after falling back to older meta, got %q", got)
	}
	if reopened.Revision() != 1 {
		t.Fatalf("fallback revision = %d, want 1", reopened.Revision())
	}
}

func TestMmapTreeFallsBackWhenNewestRootPageIsCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	olderRoot := tree.Stats().Root
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync older root: %v", err)
	}
	tree.Put("bravo", []byte("two"))
	newestRoot := tree.Stats().Root
	if newestRoot == olderRoot {
		t.Fatalf("newest root reused older root %d; want copy-on-write root", olderRoot)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	corruptPagePayload(t, path, newestRoot)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen after corrupt newest root page: %v", err)
	}
	defer reopened.Close()

	if got, ok := reopened.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("alpha after root-page fallback = %q, %v; want one, true", got, ok)
	}
	if got, ok := reopened.Get("bravo"); ok {
		t.Fatalf("bravo should be absent after falling back to older root, got %q", got)
	}
	if reopened.Revision() != 1 {
		t.Fatalf("fallback revision = %d, want 1", reopened.Revision())
	}
}

func TestMmapTreeUsesNewestValidMetaPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync older root: %v", err)
	}
	tree.Put("bravo", []byte("two"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	corruptMetaPage(t, path, 1)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen after corrupt older meta: %v", err)
	}
	defer reopened.Close()

	for key, want := range map[string]string{"alpha": "one", "bravo": "two"} {
		got, ok := reopened.Get(key)
		if !ok || string(got) != want {
			t.Fatalf("%s after reopen = %q, %v; want %q, true", key, got, ok, want)
		}
	}
	if reopened.Revision() != 2 {
		t.Fatalf("latest revision = %d, want 2", reopened.Revision())
	}
}

func TestMmapTreePersistsFreelistAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 60; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("before-%02d", i)))
	}

	reader := tree.Snapshot()
	tree.Put("key-30", []byte("after-30"))
	reader.Close()
	beforeClose := tree.Stats()
	if beforeClose.FreePages == 0 {
		t.Fatalf("FreePages before close = 0, want pages available to persist")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap reopen: %v", err)
	}
	defer reopened.Close()

	afterReopen := reopened.Stats()
	if afterReopen.FreePages != beforeClose.FreePages {
		t.Fatalf("FreePages after reopen = %d, want %d", afterReopen.FreePages, beforeClose.FreePages)
	}
	allocatedBeforeReuse := afterReopen.AllocatedPages

	reopened.Put("key-61", []byte("after-61"))
	afterReuse := reopened.Stats()
	if afterReuse.ReusedPages == 0 {
		t.Fatalf("ReusedPages after reopen write = 0, want persisted freelist reuse")
	}
	if afterReuse.AllocatedPages > allocatedBeforeReuse+1 {
		t.Fatalf("AllocatedPages grew from %d to %d despite persisted freelist pages", allocatedBeforeReuse, afterReuse.AllocatedPages)
	}
}

func TestMmapTreeRejectsFreelistEntryForReachablePage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	record := replaceNewestMetaFreeList(t, path, nil)
	replaceNewestMetaFreeList(t, path, []PageID{record.root})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with reachable root in persisted freelist")
	}
	if !errors.Is(err, ErrFreelist) {
		t.Fatalf("OpenMmap reachable freelist error = %v, want ErrFreelist", err)
	}
}

func TestMmapTreeRejectsDuplicatePersistedFreelistEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 30; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	snapshot := tree.Snapshot()
	tree.Put("key-10", []byte("updated"))
	snapshot.Close()
	free := append([]PageID(nil), tree.free...)
	if len(free) == 0 {
		t.Fatalf("tree did not produce a free page after snapshot release")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	replaceNewestMetaFreeList(t, path, []PageID{free[0], free[0]})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with duplicate persisted freelist entries")
	}
	if !errors.Is(err, ErrFreelist) {
		t.Fatalf("OpenMmap duplicate freelist error = %v, want ErrFreelist", err)
	}
}

func TestMmapTreeRejectsOutOfRangePersistedFreelistEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	record := replaceNewestMetaFreeList(t, path, nil)
	replaceNewestMetaFreeList(t, path, []PageID{record.nextPage})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with out-of-range persisted freelist entry")
	}
	if !errors.Is(err, ErrFreelist) {
		t.Fatalf("OpenMmap out-of-range freelist error = %v, want ErrFreelist", err)
	}
}

func TestMmapTreeRejectsPersistedFreelistEntryBeyondDeclaredCapacity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 30; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	snapshot := tree.Snapshot()
	tree.Put("key-10", []byte("updated"))
	snapshot.Close()
	if len(tree.free) == 0 {
		t.Fatalf("tree did not produce a free page after snapshot release")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	replaceNewestMetaRecord(t, path, func(record metaRecord) metaRecord {
		record.maxPages = int(record.nextPage - firstTreePageID)
		record.free = []PageID{firstTreePageID + PageID(record.maxPages)}
		return record
	})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with persisted freelist entry beyond declared capacity")
	}
	if !errors.Is(err, ErrFreelist) {
		t.Fatalf("OpenMmap freelist capacity error = %v, want ErrFreelist", err)
	}
	if !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("OpenMmap freelist capacity error = %v, want capacity detail", err)
	}
}

func TestMmapTreeRejectsMetadataLengthThatDoesNotMatchReachableKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 30; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	replaceNewestMetaRecord(t, path, func(record metaRecord) metaRecord {
		record.length++
		return record
	})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with mismatched metadata length")
	}
	if !errors.Is(err, ErrMetaInvariant) {
		t.Fatalf("OpenMmap metadata length error = %v, want ErrMetaInvariant", err)
	}
}

func TestMmapTreeRejectsMetadataRootOutsideNextPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	replaceNewestMetaRecord(t, path, func(record metaRecord) metaRecord {
		record.root = record.nextPage
		return record
	})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with metadata root outside nextPage")
	}
	if !errors.Is(err, ErrMetaInvariant) {
		t.Fatalf("OpenMmap root bounds error = %v, want ErrMetaInvariant", err)
	}
}

func TestMmapTreeRejectsMetadataNextPageBeyondMappedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 8})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	replaceNewestMetaRecord(t, path, func(record metaRecord) metaRecord {
		record.nextPage = PageID(fileSize(t, path)/PageSize) + 1
		return record
	})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with metadata nextPage beyond mapped file")
	}
	if !errors.Is(err, ErrMetaInvariant) {
		t.Fatalf("OpenMmap nextPage bounds error = %v, want ErrMetaInvariant", err)
	}
}

func TestMmapTreeRejectsMetadataNextPageBeyondDeclaredCapacity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	replaceNewestMetaRecord(t, path, func(record metaRecord) metaRecord {
		record.maxPages = int(record.nextPage-firstTreePageID) - 1
		return record
	})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with metadata nextPage beyond declared maxPages")
	}
	if !errors.Is(err, ErrMetaInvariant) {
		t.Fatalf("OpenMmap maxPages bounds error = %v, want ErrMetaInvariant", err)
	}
}

func TestMmapTreeRejectsUnsupportedMetadataVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	replaceNewestMetaBytes(t, path, func(data []byte) {
		binary.LittleEndian.PutUint64(data[metaVersionOff:], metaVersion+1)
	})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with unsupported metadata version")
	}
	if !errors.Is(err, ErrMetaInvariant) {
		t.Fatalf("OpenMmap unsupported metadata version error = %v, want ErrMetaInvariant", err)
	}
	if !strings.Contains(err.Error(), "metadata version") {
		t.Fatalf("OpenMmap unsupported metadata version error = %v, want metadata version detail", err)
	}
}

func TestMmapTreeRejectsMetadataPageSizeMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	replaceNewestMetaBytes(t, path, func(data []byte) {
		binary.LittleEndian.PutUint64(data[metaPageSizeOff:], uint64(PageSize*2))
	})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with mismatched metadata page size")
	}
	if !errors.Is(err, ErrMetaInvariant) {
		t.Fatalf("OpenMmap metadata page size error = %v, want ErrMetaInvariant", err)
	}
	if !strings.Contains(err.Error(), "page size") {
		t.Fatalf("OpenMmap metadata page size error = %v, want page size detail", err)
	}
}

func TestMmapTreeRejectsMetadataDegreeBelowMinimum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	replaceNewestMetaRecord(t, path, func(record metaRecord) metaRecord {
		record.degree = 1
		return record
	})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with metadata degree below minimum")
	}
	if !errors.Is(err, ErrMetaInvariant) {
		t.Fatalf("OpenMmap metadata degree error = %v, want ErrMetaInvariant", err)
	}
	if !strings.Contains(err.Error(), "degree") {
		t.Fatalf("OpenMmap metadata degree error = %v, want degree detail", err)
	}
}

func TestMmapTreeRejectsMetadataDegreeBeyondPageCapacity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	replaceNewestMetaRecord(t, path, func(record metaRecord) metaRecord {
		record.degree = 10_000
		return record
	})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with metadata degree beyond page capacity")
	}
	if !errors.Is(err, ErrMetaInvariant) {
		t.Fatalf("OpenMmap metadata degree capacity error = %v, want ErrMetaInvariant", err)
	}
	if !strings.Contains(err.Error(), "degree") {
		t.Fatalf("OpenMmap metadata degree capacity error = %v, want degree detail", err)
	}
}

func TestMmapTreeTakesExclusiveFileLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	first, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap first: %v", err)
	}

	second, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err == nil {
		second.Close()
		first.Close()
		t.Fatalf("second OpenMmap unexpectedly acquired the same database lock")
	}
	if !errors.Is(err, ErrDatabaseLocked) {
		first.Close()
		t.Fatalf("second OpenMmap error = %v, want ErrDatabaseLocked", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}

	reopened, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap after close: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close reopened: %v", err)
	}
}

func TestMmapReadOnlyOpensShareFileLockAndBlockWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	writer, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap writer: %v", err)
	}
	writer.Put("alpha", []byte("one"))
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	firstReader, err := OpenMmapReadOnly(path)
	if err != nil {
		t.Fatalf("OpenMmapReadOnly first: %v", err)
	}
	secondReader, err := OpenMmapReadOnly(path)
	if err != nil {
		firstReader.Close()
		t.Fatalf("OpenMmapReadOnly second: %v", err)
	}

	for name, reader := range map[string]*Tree{"first": firstReader, "second": secondReader} {
		got, ok := reader.Get("alpha")
		if !ok || string(got) != "one" {
			secondReader.Close()
			firstReader.Close()
			t.Fatalf("%s reader Get(alpha) = %q, %v; want one, true", name, got, ok)
		}
	}

	blockedWriter, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err == nil {
		blockedWriter.Close()
		secondReader.Close()
		firstReader.Close()
		t.Fatalf("writer unexpectedly opened while shared readers were active")
	}
	if !errors.Is(err, ErrDatabaseLocked) {
		secondReader.Close()
		firstReader.Close()
		t.Fatalf("writer while readers active error = %v, want ErrDatabaseLocked", err)
	}

	if err := secondReader.Close(); err != nil {
		firstReader.Close()
		t.Fatalf("Close second reader: %v", err)
	}
	if err := firstReader.Close(); err != nil {
		t.Fatalf("Close first reader: %v", err)
	}

	reopenedWriter, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap writer after readers close: %v", err)
	}
	if err := reopenedWriter.Close(); err != nil {
		t.Fatalf("Close reopened writer: %v", err)
	}
}

func TestMmapReadOnlyRejectsMutations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	writer, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap writer: %v", err)
	}
	writer.Put("alpha", []byte("one"))
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	reader, err := OpenMmapReadOnly(path)
	if err != nil {
		t.Fatalf("OpenMmapReadOnly: %v", err)
	}
	defer reader.Close()

	if old, replaced := reader.Put("alpha", []byte("two")); replaced || old != nil {
		t.Fatalf("read-only Put = %q, %v; want nil, false", old, replaced)
	}
	if old, deleted := reader.Delete("alpha"); deleted || old != nil {
		t.Fatalf("read-only Delete = %q, %v; want nil, false", old, deleted)
	}
	got, ok := reader.Get("alpha")
	if !ok || string(got) != "one" {
		t.Fatalf("read-only Get(alpha) after rejected mutations = %q, %v; want one, true", got, ok)
	}
}

func TestMmapTreeRejectsCorruptReachableDataPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	root := tree.Stats().Root
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	corruptMetaPage(t, path, 0)
	corruptPagePayload(t, path, root)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with corrupt reachable data page")
	}
	if !errors.Is(err, ErrPageChecksum) {
		t.Fatalf("OpenMmap corrupt data page error = %v, want ErrPageChecksum", err)
	}
}

func TestMmapTreeRejectsCorruptReachableChildPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	root := tree.pages[tree.root]
	child := root.leftmostChild()
	if child == 0 {
		t.Fatalf("root has no leftmost child after many inserts")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	corruptPagePayload(t, path, child)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with corrupt reachable child page")
	}
	if !errors.Is(err, ErrPageChecksum) {
		t.Fatalf("OpenMmap corrupt child error = %v, want ErrPageChecksum", err)
	}
}

func TestMmapTreeRejectsCorruptReachableOverflowPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("large", bytes.Repeat([]byte("o"), PageSize*2+17))
	root := tree.pages[tree.root]
	ref, ok := root.overflowRef("large")
	if !ok {
		t.Fatalf("large value was not stored as an overflow reference")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	corruptMetaPage(t, path, 0)
	corruptPagePayload(t, path, ref.first)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with corrupt reachable overflow page")
	}
	if !errors.Is(err, ErrPageChecksum) {
		t.Fatalf("OpenMmap corrupt overflow error = %v, want ErrPageChecksum", err)
	}
}

func TestMmapTreeRejectsOverflowPageAsTreeRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("large", bytes.Repeat([]byte("o"), PageSize*2+17))
	root := tree.pages[tree.root]
	ref, ok := root.overflowRef("large")
	if !ok {
		t.Fatalf("large value was not stored as an overflow reference")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	replaceNewestMetaRecord(t, path, func(record metaRecord) metaRecord {
		record.root = ref.first
		return record
	})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with overflow page as tree root")
	}
	if !errors.Is(err, ErrTreeInvariant) {
		t.Fatalf("OpenMmap overflow root error = %v, want ErrTreeInvariant", err)
	}
	if !strings.Contains(err.Error(), "not a tree page") {
		t.Fatalf("OpenMmap overflow root error = %v, want tree-page detail", err)
	}
}

func TestMmapTreeRejectsMissingReachableOverflowPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	value := bytes.Repeat([]byte("o"), PageSize*2+17)
	tree.Put("large", value)
	root := tree.pages[tree.root]
	if _, ok := root.overflowRef("large"); !ok {
		t.Fatalf("large value was not stored as an overflow reference")
	}
	missing := tree.nextPage
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	corruptLeafOverflowRefFirst(t, path, root.id, "large", missing)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with missing reachable overflow page")
	}
	if !errors.Is(err, ErrOverflowInvariant) {
		t.Fatalf("OpenMmap missing overflow error = %v, want ErrOverflowInvariant", err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("OpenMmap missing overflow error = %v, want missing detail", err)
	}
}

func TestMmapTreeRejectsOverflowReferenceToNonOverflowPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	value := bytes.Repeat([]byte("o"), PageSize*2+17)
	tree.Put("large", value)
	root := tree.pages[tree.root]
	if _, ok := root.overflowRef("large"); !ok {
		t.Fatalf("large value was not stored as an overflow reference")
	}
	spareLeaf := appendFreeTailPage(t, tree)
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	corruptLeafOverflowRefFirst(t, path, root.id, "large", spareLeaf)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with overflow reference to non-overflow page")
	}
	if !errors.Is(err, ErrOverflowInvariant) {
		t.Fatalf("OpenMmap non-overflow page error = %v, want ErrOverflowInvariant", err)
	}
	if !strings.Contains(err.Error(), "not an overflow page") {
		t.Fatalf("OpenMmap non-overflow page error = %v, want page-kind detail", err)
	}
}

func TestMmapTreeRejectsOverflowReferenceWithoutFirstPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	value := bytes.Repeat([]byte("o"), PageSize*2+17)
	tree.Put("large", value)
	root := tree.pages[tree.root]
	if _, ok := root.overflowRef("large"); !ok {
		t.Fatalf("large value was not stored as an overflow reference")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	corruptLeafOverflowRef(t, path, root.id, "large", func(ref *overflowRef) {
		ref.first = 0
		ref.length = 0
	})

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with overflow reference missing first page")
	}
	if !errors.Is(err, ErrOverflowInvariant) {
		t.Fatalf("OpenMmap missing first overflow page error = %v, want ErrOverflowInvariant", err)
	}
	if !strings.Contains(err.Error(), "first page") {
		t.Fatalf("OpenMmap missing first overflow page error = %v, want first-page detail", err)
	}
}

func TestMmapTreeRejectsOverflowChainLongerThanReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	value := bytes.Repeat([]byte("o"), PageSize*2+17)
	tree.Put("large", value)
	root := tree.pages[tree.root]
	ref, ok := root.overflowRef("large")
	if !ok {
		t.Fatalf("large value was not stored as an overflow reference")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	corruptLeafOverflowRefLength(t, path, root.id, "large", ref.length-1)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with overflow chain longer than reference")
	}
	if !errors.Is(err, ErrOverflowInvariant) {
		t.Fatalf("OpenMmap overflow length error = %v, want ErrOverflowInvariant", err)
	}
}

func TestMmapTreeRejectsValidChecksumLeafWithInvalidSlotLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	newest := keepOnlyNewestMetaPage(t, path)
	corruptPageSlotValueLen(t, path, newest.root, 0, PageSize)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with invalid reachable leaf slot layout")
	}
	if !errors.Is(err, ErrPageLayout) {
		t.Fatalf("OpenMmap invalid leaf slot layout error = %v, want ErrPageLayout", err)
	}
}

func TestMmapTreeRejectsValidChecksumBranchWithInvalidChildSlotLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if !tree.pages[tree.root].isBranch() {
		t.Fatalf("root is not a branch after many inserts")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	newest := keepOnlyNewestMetaPage(t, path)
	corruptPageSlotValueLen(t, path, newest.root, 0, 9)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with invalid reachable branch slot layout")
	}
	if !errors.Is(err, ErrPageLayout) {
		t.Fatalf("OpenMmap invalid branch slot layout error = %v, want ErrPageLayout", err)
	}
}

func TestMmapTreeRejectsBranchThatReferencesChildTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if !tree.pages[tree.root].isBranch() {
		t.Fatalf("root is not a branch after many inserts")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	newest := keepOnlyNewestMetaPage(t, path)
	corruptBranchSlotChildToLeftmost(t, path, newest.root, 0)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with duplicated branch child")
	}
	if !errors.Is(err, ErrTreeInvariant) {
		t.Fatalf("OpenMmap duplicated branch child error = %v, want ErrTreeInvariant", err)
	}
}

func TestMmapTreeRejectsBranchChildOutsideAllocatedRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if !tree.pages[tree.root].isBranch() {
		t.Fatalf("root is not a branch after many inserts")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	newest := keepOnlyNewestMetaPage(t, path)
	corruptBranchLeftmostChild(t, path, newest.root, newest.nextPage)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with branch child outside allocated range")
	}
	if !errors.Is(err, ErrTreeInvariant) {
		t.Fatalf("OpenMmap missing branch child error = %v, want ErrTreeInvariant", err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("OpenMmap missing branch child error = %v, want missing detail", err)
	}
}

func TestMmapTreeRejectsBranchSeparatorThatDoesNotMatchRightChild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 40; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	root := tree.pages[tree.root]
	if !root.isBranch() || root.slotCount() == 0 {
		t.Fatalf("root does not have a separator after many inserts")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	newest := keepOnlyNewestMetaPage(t, path)
	corruptBranchSlotKey(t, path, newest.root, 0, "key-00")

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with mismatched branch separator")
	}
	if !errors.Is(err, ErrTreeInvariant) {
		t.Fatalf("OpenMmap mismatched branch separator error = %v, want ErrTreeInvariant", err)
	}
}

func TestMmapTreeRejectsLeafKeyOutsideBranchBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "course.db")

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 12; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}

	var leafID PageID
	var slotIndex int
	var replacement string
	for _, p := range tree.pages {
		if !p.isBranch() || p.slotCount() == 0 {
			continue
		}
		child := p.leftmostChild()
		leaf := tree.pages[child]
		if leaf == nil || !leaf.isLeaf() || leaf.slotCount() == 0 {
			continue
		}
		slotIndex = int(leaf.slotCount()) - 1
		current := leaf.readCellKey(slotIndex)
		replacement = p.readCellKey(0)
		if len(current) == len(replacement) && current < replacement {
			leafID = child
			break
		}
	}
	if leafID == 0 {
		t.Fatalf("did not find a leaf child with a parent upper bound")
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close create: %v", err)
	}

	keepOnlyNewestMetaPage(t, path)
	corruptLeafSlotKey(t, path, leafID, slotIndex, replacement)

	reopened, err := OpenMmap(path, MmapOptions{})
	if err == nil {
		reopened.Close()
		t.Fatalf("OpenMmap succeeded with leaf key outside branch bounds")
	}
	if !errors.Is(err, ErrTreeInvariant) {
		t.Fatalf("OpenMmap leaf bound error = %v, want ErrTreeInvariant", err)
	}
	if !strings.Contains(err.Error(), "outside branch bounds") {
		t.Fatalf("OpenMmap leaf bound error = %v, want bounds detail", err)
	}
}

func corruptMetaPage(t *testing.T, path string, metaIndex int) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("OpenFile corrupt: %v", err)
	}
	defer file.Close()

	offset := int64(metaIndex * PageSize)
	if _, err := file.WriteAt([]byte("BROKEN!!"), offset); err != nil {
		t.Fatalf("WriteAt corrupt meta %d: %v", metaIndex, err)
	}
	if err := file.Sync(); err != nil {
		t.Fatalf("Sync corrupt meta %d: %v", metaIndex, err)
	}
}

func keepOnlyNewestMetaPage(t *testing.T, path string) metaRecord {
	t.Helper()

	newestIndex, record := newestMetaPage(t, path)
	for index := 0; index < metaPageCount; index++ {
		if index != newestIndex {
			corruptMetaPage(t, path, index)
		}
	}
	return record
}

func replaceNewestMetaFreeList(t *testing.T, path string, free []PageID) metaRecord {
	t.Helper()

	return replaceNewestMetaRecord(t, path, func(record metaRecord) metaRecord {
		record.free = append([]PageID(nil), free...)
		return record
	})
}

func replaceNewestMetaRecord(t *testing.T, path string, rewrite func(metaRecord) metaRecord) metaRecord {
	t.Helper()

	index, record := newestMetaPage(t, path)
	for candidateIndex := 0; candidateIndex < metaPageCount; candidateIndex++ {
		if candidateIndex != index {
			corruptMetaPage(t, path, candidateIndex)
		}
	}

	record = rewrite(record)
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("OpenFile rewrite meta: %v", err)
	}
	defer file.Close()

	buf := make([]byte, PageSize)
	if err := writeMetaPage(buf, record); err != nil {
		t.Fatalf("writeMetaPage rewrite meta %d: %v", index, err)
	}
	if _, err := file.WriteAt(buf, int64(index*PageSize)); err != nil {
		t.Fatalf("WriteAt rewrite meta %d: %v", index, err)
	}
	if err := file.Sync(); err != nil {
		t.Fatalf("Sync rewrite meta %d: %v", index, err)
	}
	return record
}

func replaceNewestMetaBytes(t *testing.T, path string, rewrite func([]byte)) {
	t.Helper()

	index, _ := newestMetaPage(t, path)
	for candidateIndex := 0; candidateIndex < metaPageCount; candidateIndex++ {
		if candidateIndex != index {
			corruptMetaPage(t, path, candidateIndex)
		}
	}

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("OpenFile rewrite raw meta: %v", err)
	}
	defer file.Close()

	buf := make([]byte, PageSize)
	if _, err := file.ReadAt(buf, int64(index*PageSize)); err != nil {
		t.Fatalf("ReadAt rewrite raw meta %d: %v", index, err)
	}
	rewrite(buf)
	binary.LittleEndian.PutUint32(buf[metaChecksumOff:], metaChecksum(buf))
	if _, err := file.WriteAt(buf, int64(index*PageSize)); err != nil {
		t.Fatalf("WriteAt rewrite raw meta %d: %v", index, err)
	}
	if err := file.Sync(); err != nil {
		t.Fatalf("Sync rewrite raw meta %d: %v", index, err)
	}
}

func newestMetaPage(t *testing.T, path string) (int, metaRecord) {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open newest meta: %v", err)
	}

	type candidate struct {
		index  int
		record metaRecord
	}
	var candidates []candidate
	for index := 0; index < metaPageCount; index++ {
		buf := make([]byte, PageSize)
		if _, err := file.ReadAt(buf, int64(index*PageSize)); err != nil {
			file.Close()
			t.Fatalf("ReadAt newest meta %d: %v", index, err)
		}
		record, ok := readMetaPage(buf)
		if ok {
			candidates = append(candidates, candidate{index: index, record: record})
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close newest meta reader: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatalf("no valid metadata pages found")
	}
	slices.SortFunc(candidates, func(left, right candidate) int {
		return compareUint64Desc(left.record.revision, right.record.revision)
	})
	return candidates[0].index, candidates[0].record
}

func corruptPagePayload(t *testing.T, path string, id PageID) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("OpenFile corrupt page %d: %v", id, err)
	}
	defer file.Close()

	offset := int64(id)*PageSize + PageSize - 1
	if _, err := file.WriteAt([]byte{0xff}, offset); err != nil {
		t.Fatalf("WriteAt corrupt page %d: %v", id, err)
	}
	if err := file.Sync(); err != nil {
		t.Fatalf("Sync corrupt page %d: %v", id, err)
	}
}

func corruptPageSlotValueLen(t *testing.T, path string, id PageID, slotIndex int, valueLen int) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("OpenFile corrupt page slot %d: %v", id, err)
	}
	defer file.Close()

	buf := make([]byte, PageSize)
	offset := int64(id) * PageSize
	if _, err := file.ReadAt(buf, offset); err != nil {
		t.Fatalf("ReadAt corrupt page slot %d: %v", id, err)
	}
	p := &page{id: id, data: buf}
	slot := p.readSlot(slotIndex)
	slot.valueLen = uint16(valueLen)
	p.writeSlot(slotIndex, slot)
	p.updateChecksum()

	if _, err := file.WriteAt(buf, offset); err != nil {
		t.Fatalf("WriteAt corrupt page slot %d: %v", id, err)
	}
	if err := file.Sync(); err != nil {
		t.Fatalf("Sync corrupt page slot %d: %v", id, err)
	}
}

func corruptBranchSlotChildToLeftmost(t *testing.T, path string, id PageID, slotIndex int) {
	t.Helper()

	mutatePage(t, path, id, func(p *page) {
		slot := p.readSlot(slotIndex)
		valueStart := int(slot.offset) + int(slot.keyLen)
		encodePageID(p.data[valueStart:valueStart+8], p.leftmostChild())
		p.updateChecksum()
	})
}

func corruptBranchLeftmostChild(t *testing.T, path string, id, child PageID) {
	t.Helper()

	mutatePage(t, path, id, func(p *page) {
		p.setLeftmostChild(child)
		p.updateChecksum()
	})
}

func corruptBranchSlotKey(t *testing.T, path string, id PageID, slotIndex int, key string) {
	t.Helper()

	mutatePage(t, path, id, func(p *page) {
		slot := p.readSlot(slotIndex)
		if len(key) != int(slot.keyLen) {
			t.Fatalf("replacement key length = %d, want %d", len(key), slot.keyLen)
		}
		copy(p.data[int(slot.offset):int(slot.offset)+int(slot.keyLen)], key)
		p.updateChecksum()
	})
}

func corruptLeafSlotKey(t *testing.T, path string, id PageID, slotIndex int, key string) {
	t.Helper()

	mutatePage(t, path, id, func(p *page) {
		slot := p.readSlot(slotIndex)
		if len(key) != int(slot.keyLen) {
			t.Fatalf("replacement key length = %d, want %d", len(key), slot.keyLen)
		}
		copy(p.data[int(slot.offset):int(slot.offset)+int(slot.keyLen)], key)
		p.updateChecksum()
	})
}

func corruptLeafOverflowRefLength(t *testing.T, path string, id PageID, key string, length int) {
	t.Helper()

	corruptLeafOverflowRef(t, path, id, key, func(ref *overflowRef) {
		ref.length = length
	})
}

func corruptLeafOverflowRefFirst(t *testing.T, path string, id PageID, key string, first PageID) {
	t.Helper()

	corruptLeafOverflowRef(t, path, id, key, func(ref *overflowRef) {
		ref.first = first
	})
}

func corruptLeafOverflowRef(t *testing.T, path string, id PageID, key string, rewrite func(*overflowRef)) {
	t.Helper()

	mutatePage(t, path, id, func(p *page) {
		index, found := p.searchSlot(key)
		if !found {
			t.Fatalf("key %q not found in page %d", key, id)
		}
		slot := p.readSlot(index)
		raw := p.readCellValue(index)
		ref, ok := decodeOverflowRef(raw, slot.flags)
		if !ok {
			t.Fatalf("key %q is not an overflow reference", key)
		}
		rewrite(&ref)
		valueStart := int(slot.offset) + int(slot.keyLen)
		copy(p.data[valueStart:valueStart+overflowRefSize], encodeOverflowRef(ref))
		p.updateChecksum()
	})
}

func corruptLeafNext(t *testing.T, path string, id PageID, next PageID) {
	t.Helper()

	mutatePage(t, path, id, func(p *page) {
		p.setLeftmostChild(next)
		p.updateChecksum()
	})
}

func appendFreeTailPage(t *testing.T, tree *Tree) PageID {
	t.Helper()

	id := tree.nextPage
	if err := tree.growMmapForPage(id); err != nil {
		t.Fatalf("grow mmap for tail page %d: %v", id, err)
	}
	page := tree.newPage(id, flagLeaf)
	tree.pages[id] = page
	tree.free = append(tree.free, id)
	tree.nextPage++
	return id
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s): %v", path, err)
	}
	return info.Size()
}

func createSparseFile(t *testing.T, path string, size int64) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("OpenFile create sparse file: %v", err)
	}
	if err := file.Truncate(size); err != nil {
		file.Close()
		t.Fatalf("Truncate sparse file to %d: %v", size, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close sparse file: %v", err)
	}
}

func mutatePage(t *testing.T, path string, id PageID, mutate func(*page)) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("OpenFile mutate page %d: %v", id, err)
	}
	defer file.Close()

	buf := make([]byte, PageSize)
	offset := int64(id) * PageSize
	if _, err := file.ReadAt(buf, offset); err != nil {
		t.Fatalf("ReadAt mutate page %d: %v", id, err)
	}
	mutate(&page{id: id, data: buf})
	if _, err := file.WriteAt(buf, offset); err != nil {
		t.Fatalf("WriteAt mutate page %d: %v", id, err)
	}
	if err := file.Sync(); err != nil {
		t.Fatalf("Sync mutate page %d: %v", id, err)
	}
}
