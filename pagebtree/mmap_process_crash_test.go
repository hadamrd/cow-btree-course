//go:build unix

package pagebtree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	mmapCrashChildEnv      = "COWBTREE_MMAP_CRASH_CHILD"
	mmapCrashPathEnv       = "COWBTREE_MMAP_CRASH_PATH"
	mmapCrashFaultEnv      = "COWBTREE_MMAP_CRASH_FAULT"
	mmapCrashChildExitCode = 77
)

func TestMmapSyncProcessCrashMatrixClassifiesRecoveryRoot(t *testing.T) {
	if os.Getenv(mmapCrashChildEnv) == "1" {
		runMmapSyncProcessCrashChild(t)
		return
	}

	tests := []struct {
		name        string
		fault       mmapFaultPoint
		wantNewRoot bool
	}{
		{
			name:        "before data sync",
			fault:       mmapFaultBeforeDataSync,
			wantNewRoot: false,
		},
		{
			name:        "after metadata write",
			fault:       mmapFaultAfterMetaWrite,
			wantNewRoot: true,
		},
		{
			name:        "before metadata sync",
			fault:       mmapFaultBeforeMetaSync,
			wantNewRoot: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "course.db")
			runMmapSyncProcessCrash(t, path, tt.fault)
			assertMmapProcessCrashRecoveryRoot(t, path, tt.fault, tt.wantNewRoot)
		})
	}
}

func TestMmapTxSyncProcessCrashMatrixClassifiesRecoveryRoot(t *testing.T) {
	if os.Getenv(mmapCrashChildEnv) == "1" {
		runMmapTxSyncProcessCrashChild(t)
		return
	}

	tests := []struct {
		name        string
		fault       mmapFaultPoint
		wantNewRoot bool
	}{
		{
			name:        "before data sync",
			fault:       mmapFaultBeforeDataSync,
			wantNewRoot: false,
		},
		{
			name:        "after metadata write",
			fault:       mmapFaultAfterMetaWrite,
			wantNewRoot: true,
		},
		{
			name:        "before metadata sync",
			fault:       mmapFaultBeforeMetaSync,
			wantNewRoot: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "course.db")
			runMmapTxSyncProcessCrash(t, path, tt.fault)
			assertMmapProcessCrashRecoveryRoot(t, path, tt.fault, tt.wantNewRoot)
		})
	}
}

func TestMmapGrowthProcessCrashMatrixClassifiesOldRoot(t *testing.T) {
	if os.Getenv(mmapCrashChildEnv) == "1" {
		runMmapGrowthProcessCrashChild(t)
		return
	}

	tests := []struct {
		name  string
		fault mmapFaultPoint
	}{
		{
			name:  "before file size sync",
			fault: mmapFaultBeforeFileSizeSync,
		},
		{
			name:  "before directory sync",
			fault: mmapFaultBeforeDirectorySync,
		},
		{
			name:  "before replacement remap",
			fault: mmapFaultBeforeRemap,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "course.db")
			runMmapGrowthProcessCrash(t, path, tt.fault)
			assertMmapProcessCrashRecoveryRoot(t, path, tt.fault, false)
		})
	}
}

func TestMmapCompactShrinkProcessCrashMatrixClassifiesCompactedRoot(t *testing.T) {
	if os.Getenv(mmapCrashChildEnv) == "1" {
		runMmapCompactShrinkProcessCrashChild(t)
		return
	}

	tests := []struct {
		name  string
		fault mmapFaultPoint
	}{
		{
			name:  "before file size sync",
			fault: mmapFaultBeforeFileSizeSync,
		},
		{
			name:  "before directory sync",
			fault: mmapFaultBeforeDirectorySync,
		},
		{
			name:  "before replacement remap",
			fault: mmapFaultBeforeRemap,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "course.db")
			runMmapCompactShrinkProcessCrash(t, path, tt.fault)
			assertMmapCompactShrinkProcessCrashRecovered(t, path, tt.fault)
		})
	}
}

func TestMmapLargeFreelistProcessCrashMatrixClassifiesRecoveryRoot(t *testing.T) {
	if os.Getenv(mmapCrashChildEnv) == "1" {
		runMmapLargeFreelistProcessCrashChild(t)
		return
	}

	tests := []struct {
		name         string
		fault        mmapFaultPoint
		wantFreelist bool
	}{
		{
			name:         "before data sync",
			fault:        mmapFaultBeforeDataSync,
			wantFreelist: false,
		},
		{
			name:         "after metadata write",
			fault:        mmapFaultAfterMetaWrite,
			wantFreelist: true,
		},
		{
			name:         "before metadata sync",
			fault:        mmapFaultBeforeMetaSync,
			wantFreelist: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "course.db")
			runMmapLargeFreelistProcessCrash(t, path, tt.fault)
			assertMmapLargeFreelistProcessCrashRecovered(t, path, tt.fault, tt.wantFreelist)
		})
	}
}

func TestMmapLargeReclaimProcessCrashMatrixClassifiesReaderPinnedRetiredPages(t *testing.T) {
	if os.Getenv(mmapCrashChildEnv) == "1" {
		runMmapLargeReclaimProcessCrashChild(t)
		return
	}

	tests := []struct {
		name        string
		fault       mmapFaultPoint
		wantReclaim bool
	}{
		{
			name:        "before data sync",
			fault:       mmapFaultBeforeDataSync,
			wantReclaim: false,
		},
		{
			name:        "after metadata write",
			fault:       mmapFaultAfterMetaWrite,
			wantReclaim: true,
		},
		{
			name:        "before metadata sync",
			fault:       mmapFaultBeforeMetaSync,
			wantReclaim: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "course.db")
			reader := runMmapLargeReclaimProcessCrash(t, path, tt.fault)
			defer reader.Close()
			assertMmapLargeReclaimProcessCrashRecovered(t, path, tt.fault, tt.wantReclaim)
		})
	}
}

func runMmapSyncProcessCrash(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	runMmapProcessCrashChild(t, path, fault, "^TestMmapSyncProcessCrashMatrixClassifiesRecoveryRoot$", "sync")
}

func runMmapTxSyncProcessCrash(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	runMmapProcessCrashChild(t, path, fault, "^TestMmapTxSyncProcessCrashMatrixClassifiesRecoveryRoot$", "tx-sync")
}

func runMmapGrowthProcessCrash(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	runMmapProcessCrashChild(t, path, fault, "^TestMmapGrowthProcessCrashMatrixClassifiesOldRoot$", "growth")
}

func runMmapCompactShrinkProcessCrash(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	runMmapProcessCrashChild(t, path, fault, "^TestMmapCompactShrinkProcessCrashMatrixClassifiesCompactedRoot$", "compact-shrink")
}

func runMmapLargeFreelistProcessCrash(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	runMmapProcessCrashChild(t, path, fault, "^TestMmapLargeFreelistProcessCrashMatrixClassifiesRecoveryRoot$", "large-freelist")
}

func runMmapLargeReclaimProcessCrash(t *testing.T, path string, fault mmapFaultPoint) *Tree {
	t.Helper()

	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 2048})
	if err != nil {
		t.Fatalf("OpenMmap large-reclaim parent create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		tree.Close()
		t.Fatalf("initial large-reclaim parent Sync: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close large-reclaim parent writer: %v", err)
	}

	reader, err := OpenMmapReadOnly(path)
	if err != nil {
		t.Fatalf("OpenMmapReadOnly large-reclaim parent: %v", err)
	}

	runMmapProcessCrashChild(t, path, fault, "^TestMmapLargeReclaimProcessCrashMatrixClassifiesReaderPinnedRetiredPages$", "large-reclaim")
	return reader
}

func runMmapProcessCrashChild(t *testing.T, path string, fault mmapFaultPoint, testPattern string, label string) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run="+testPattern)
	cmd.Env = append(os.Environ(),
		mmapCrashChildEnv+"=1",
		mmapCrashPathEnv+"="+path,
		mmapCrashFaultEnv+"="+string(fault),
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s crash child for %s exited successfully; output:\n%s", label, fault, output)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("%s crash child for %s failed without exit status: %v\n%s", label, fault, err, output)
	}
	if got := exitErr.ExitCode(); got != mmapCrashChildExitCode {
		t.Fatalf("%s crash child for %s exit code = %d, want %d; output:\n%s", label, fault, got, mmapCrashChildExitCode, output)
	}
}

func runMmapSyncProcessCrashChild(t *testing.T) {
	t.Helper()

	path := os.Getenv(mmapCrashPathEnv)
	fault := mmapFaultPoint(os.Getenv(mmapCrashFaultEnv))
	if path == "" || fault == "" {
		t.Fatalf("missing crash child env path=%q fault=%q", path, fault)
	}
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap child create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial child Sync: %v", err)
	}
	tree.Put("bravo", []byte("two"))
	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == fault {
			os.Exit(mmapCrashChildExitCode)
		}
		return nil
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("child Sync before process crash: %v", err)
	}
	t.Fatalf("child Sync completed without hitting fault %s", fault)
}

func runMmapTxSyncProcessCrashChild(t *testing.T) {
	t.Helper()

	path := os.Getenv(mmapCrashPathEnv)
	fault := mmapFaultPoint(os.Getenv(mmapCrashFaultEnv))
	if path == "" || fault == "" {
		t.Fatalf("missing tx-sync crash child env path=%q fault=%q", path, fault)
	}
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap tx-sync child create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial tx-sync child Sync: %v", err)
	}

	tx := tree.BeginReadWrite()
	tx.Put("bravo", []byte("two"))
	if changed := tx.Commit(); !changed {
		t.Fatalf("tx-sync child transaction commit changed = false, want true")
	}
	if got, ok := tree.Get("bravo"); !ok || string(got) != "two" {
		t.Fatalf("tx-sync child live Get(bravo) = %q, %v; want two, true", got, ok)
	}

	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == fault {
			os.Exit(mmapCrashChildExitCode)
		}
		return nil
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("tx-sync child Sync before process crash: %v", err)
	}
	t.Fatalf("tx-sync child Sync completed without hitting fault %s", fault)
}

func runMmapGrowthProcessCrashChild(t *testing.T) {
	t.Helper()

	path := os.Getenv(mmapCrashPathEnv)
	fault := mmapFaultPoint(os.Getenv(mmapCrashFaultEnv))
	if path == "" || fault == "" {
		t.Fatalf("missing growth crash child env path=%q fault=%q", path, fault)
	}
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 4})
	if err != nil {
		t.Fatalf("OpenMmap growth child create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial growth child Sync: %v", err)
	}
	oldMaxPages := tree.arena.maxPages
	tree.Put("bravo", []byte("two"))
	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == fault {
			os.Exit(mmapCrashChildExitCode)
		}
		return nil
	}
	if err := tree.remapMmap(oldMaxPages * 2); err != nil {
		t.Fatalf("growth child remap before process crash: %v", err)
	}
	t.Fatalf("growth child remap completed without hitting fault %s", fault)
}

func runMmapCompactShrinkProcessCrashChild(t *testing.T) {
	t.Helper()

	path := os.Getenv(mmapCrashPathEnv)
	fault := mmapFaultPoint(os.Getenv(mmapCrashFaultEnv))
	if path == "" || fault == "" {
		t.Fatalf("missing compact-shrink crash child env path=%q fault=%q", path, fault)
	}
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap compact-shrink child create: %v", err)
	}
	for i := 0; i < 12; i++ {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == fault {
			os.Exit(mmapCrashChildExitCode)
		}
		return nil
	}
	if err := tree.Compact(); err != nil {
		t.Fatalf("compact-shrink child Compact before process crash: %v", err)
	}
	t.Fatalf("compact-shrink child Compact completed without hitting fault %s", fault)
}

func runMmapLargeFreelistProcessCrashChild(t *testing.T) {
	t.Helper()

	path := os.Getenv(mmapCrashPathEnv)
	fault := mmapFaultPoint(os.Getenv(mmapCrashFaultEnv))
	if path == "" || fault == "" {
		t.Fatalf("missing large-freelist crash child env path=%q fault=%q", path, fault)
	}
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 2048})
	if err != nil {
		t.Fatalf("OpenMmap large-freelist child create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial large-freelist child Sync: %v", err)
	}
	clear(tree.arena.dirtyPages)

	freeCount := maxMetaFreePages + 17
	tree.free = make([]PageID, freeCount)
	for i := range tree.free {
		tree.free[i] = firstTreePageID + 1 + PageID(i)
	}
	tree.nextPage = firstTreePageID + 1 + PageID(freeCount)
	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == fault {
			os.Exit(mmapCrashChildExitCode)
		}
		return nil
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("large-freelist child Sync before process crash: %v", err)
	}
	t.Fatalf("large-freelist child Sync completed without hitting fault %s", fault)
}

func runMmapLargeReclaimProcessCrashChild(t *testing.T) {
	t.Helper()

	path := os.Getenv(mmapCrashPathEnv)
	fault := mmapFaultPoint(os.Getenv(mmapCrashFaultEnv))
	if path == "" || fault == "" {
		t.Fatalf("missing large-reclaim crash child env path=%q fault=%q", path, fault)
	}
	tree, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap large-reclaim child open: %v", err)
	}
	defer tree.Close()

	clear(tree.arena.dirtyPages)
	tree.revision++
	retiredCount := reclaimPageCapacity + 17
	tree.retired = make([]retiredPage, retiredCount)
	for i := range tree.retired {
		tree.retired[i] = retiredPage{
			id:       firstTreePageID + 1 + PageID(i),
			revision: tree.revision,
		}
	}
	tree.nextPage = firstTreePageID + 1 + PageID(retiredCount)
	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == fault {
			os.Exit(mmapCrashChildExitCode)
		}
		return nil
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("large-reclaim child Sync before process crash: %v", err)
	}
	t.Fatalf("large-reclaim child Sync completed without hitting fault %s", fault)
}

func assertMmapProcessCrashRecoveryRoot(t *testing.T, path string, fault mmapFaultPoint, wantNewRoot bool) {
	t.Helper()

	recovered, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap after process crash at %s: %v", fault, err)
	}
	defer recovered.Close()

	if err := recovered.Check(); err != nil {
		t.Fatalf("Check after process crash at %s: %v", fault, err)
	}
	if got, ok := recovered.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get(alpha) after process crash at %s = %q, %v; want one, true", fault, got, ok)
	}
	got, ok := recovered.Get("bravo")
	if wantNewRoot {
		if !ok || string(got) != "two" {
			t.Fatalf("Get(bravo) after process crash at %s = %q, %v; want two, true", fault, got, ok)
		}
		return
	}
	if ok {
		t.Fatalf("Get(bravo) after process crash at %s = %q, true; want old root without bravo", fault, got)
	}
}

func assertMmapCompactShrinkProcessCrashRecovered(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	recovered, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap after compact-shrink process crash at %s: %v", fault, err)
	}
	defer recovered.Close()

	if err := recovered.Check(); err != nil {
		t.Fatalf("Check after compact-shrink process crash at %s: %v", fault, err)
	}
	for i := 0; i < 12; i++ {
		key := fmt.Sprintf("key-%02d", i)
		if got, ok := recovered.Get(key); !ok || string(got) != fmt.Sprintf("value-%02d", i) {
			t.Fatalf("Get(%s) after compact-shrink process crash at %s = %q, %v", key, fault, got, ok)
		}
	}
	if got, want := fileSize(t, path), int64(recovered.nextPage)*PageSize; got != want {
		t.Fatalf("file size after compact-shrink process crash at %s = %d, want compacted %d", fault, got, want)
	}
}

func assertMmapLargeFreelistProcessCrashRecovered(t *testing.T, path string, fault mmapFaultPoint, wantFreelist bool) {
	t.Helper()

	recovered, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap after large-freelist process crash at %s: %v", fault, err)
	}
	defer recovered.Close()

	if err := recovered.Check(); err != nil {
		t.Fatalf("Check after large-freelist process crash at %s: %v", fault, err)
	}
	if got, ok := recovered.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get(alpha) after large-freelist process crash at %s = %q, %v; want one, true", fault, got, ok)
	}
	if !wantFreelist {
		if got := recovered.Stats().FreePages; got != 0 {
			t.Fatalf("FreePages after large-freelist process crash at %s = %d, want old metadata with none", fault, got)
		}
		return
	}
	freeCount := maxMetaFreePages + 17
	if got := recovered.Stats().FreePages; got != freeCount {
		t.Fatalf("FreePages after large-freelist process crash at %s = %d, want %d", fault, got, freeCount)
	}
	allocatedBeforeReuse := recovered.Stats().AllocatedPages
	recovered.Put("bravo", []byte("two"))
	afterReuse := recovered.Stats()
	if afterReuse.ReusedPages == 0 {
		t.Fatalf("ReusedPages after large-freelist process crash at %s = 0, want persisted freelist reuse", fault)
	}
	if afterReuse.AllocatedPages > allocatedBeforeReuse+1 {
		t.Fatalf("AllocatedPages after large-freelist process crash at %s grew from %d to %d despite persisted freelist", fault, allocatedBeforeReuse, afterReuse.AllocatedPages)
	}
}

func assertMmapLargeReclaimProcessCrashRecovered(t *testing.T, path string, fault mmapFaultPoint, wantReclaim bool) {
	t.Helper()

	recovered, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap after large-reclaim process crash at %s: %v", fault, err)
	}
	defer recovered.Close()

	if err := recovered.Check(); err != nil {
		t.Fatalf("Check after large-reclaim process crash at %s: %v", fault, err)
	}
	if got, ok := recovered.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get(alpha) after large-reclaim process crash at %s = %q, %v; want one, true", fault, got, ok)
	}
	if !wantReclaim {
		stats := recovered.Stats()
		if stats.RetiredPages != 0 || stats.FreePages != 0 {
			t.Fatalf("Stats after large-reclaim process crash at %s = retired:%d free:%d; want old metadata with none", fault, stats.RetiredPages, stats.FreePages)
		}
		return
	}

	retiredCount := reclaimPageCapacity + 17
	stats := recovered.Stats()
	if stats.RetiredPages != retiredCount {
		t.Fatalf("RetiredPages after large-reclaim process crash at %s = %d, want %d", fault, stats.RetiredPages, retiredCount)
	}
	if stats.FreePages != 0 {
		t.Fatalf("FreePages after large-reclaim process crash at %s = %d, want live reader-pinned retired pages", fault, stats.FreePages)
	}
	recovered.Put("bravo", []byte("two"))
	afterWrite := recovered.Stats()
	if afterWrite.FreePages != 0 {
		t.Fatalf("FreePages after write with live reader after large-reclaim process crash at %s = %d, want pinned pages", fault, afterWrite.FreePages)
	}
	if afterWrite.RetiredPages < retiredCount {
		t.Fatalf("RetiredPages after write with live reader after large-reclaim process crash at %s = %d, want at least %d", fault, afterWrite.RetiredPages, retiredCount)
	}
}
