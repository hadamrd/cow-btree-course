//go:build unix

package pagebtree

import (
	"bytes"
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
