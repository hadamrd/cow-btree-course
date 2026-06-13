//go:build unix

package pagebtree

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

	for _, pattern := range []MmapAccessPattern{MmapAccessRandom, MmapAccessSequential, MmapAccessWillNeed, MmapAccessDefault} {
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
