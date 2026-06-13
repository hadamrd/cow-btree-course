//go:build unix

package pagebtree

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
