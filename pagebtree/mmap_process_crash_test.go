//go:build unix

package pagebtree

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const (
	mmapCrashChildEnv      = "COWBTREE_MMAP_CRASH_CHILD"
	mmapCrashPathEnv       = "COWBTREE_MMAP_CRASH_PATH"
	mmapCrashFaultEnv      = "COWBTREE_MMAP_CRASH_FAULT"
	mmapCrashChildExitCode = 77

	mmapReaderHoldChildEnv   = "COWBTREE_MMAP_READER_HOLD_CHILD"
	mmapReaderHoldReadyEnv   = "COWBTREE_MMAP_READER_HOLD_READY"
	mmapReaderHoldReleaseEnv = "COWBTREE_MMAP_READER_HOLD_RELEASE"
)

const (
	mmapObsoleteFreelistBeforeBothSlotsAdvance mmapFaultPoint = "obsolete-freelist-before-both-slots-advance"
	mmapObsoleteFreelistAfterBothSlotsAdvance  mmapFaultPoint = "obsolete-freelist-after-both-slots-advance"
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

func TestMmapWriteBatchCommitSyncProcessCrashMatrixClassifiesRecoveryRoot(t *testing.T) {
	if os.Getenv(mmapCrashChildEnv) == "1" {
		runMmapBatchCommitSyncProcessCrashChild(t)
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
			runMmapBatchCommitSyncProcessCrash(t, path, tt.fault)
			assertMmapBatchCommitSyncProcessCrashRecovered(t, path, tt.fault, tt.wantNewRoot)
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

func TestMmapLegacyMetadataUpgradeProcessCrashMatrixClassifiesRecoveryRoot(t *testing.T) {
	if os.Getenv(mmapCrashChildEnv) == "1" {
		runMmapLegacyUpgradeProcessCrashChild(t)
		return
	}

	tests := []struct {
		name        string
		fault       mmapFaultPoint
		wantUpgrade bool
	}{
		{
			name:        "before data sync",
			fault:       mmapFaultBeforeDataSync,
			wantUpgrade: false,
		},
		{
			name:        "after metadata write",
			fault:       mmapFaultAfterMetaWrite,
			wantUpgrade: true,
		},
		{
			name:        "before metadata sync",
			fault:       mmapFaultBeforeMetaSync,
			wantUpgrade: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := copyMmapFixture(t, "testdata/mmap-v1-inline-freelist.db")
			_, legacy := newestMetaPage(t, path)
			if legacy.version != 1 {
				t.Fatalf("legacy fixture metadata version = %d, want 1", legacy.version)
			}
			runMmapLegacyUpgradeProcessCrash(t, path, tt.fault)
			assertMmapLegacyUpgradeProcessCrashRecovered(t, path, tt.fault, legacy, tt.wantUpgrade)
		})
	}
}

func TestMmapObsoleteFreelistProcessCrashClassifiesMetadataGenerationReclaim(t *testing.T) {
	if os.Getenv(mmapCrashChildEnv) == "1" {
		runMmapObsoleteFreelistProcessCrashChild(t)
		return
	}

	tests := []struct {
		name        string
		fault       mmapFaultPoint
		wantReclaim bool
		wantCharlie bool
	}{
		{
			name:        "before both metadata slots advance",
			fault:       mmapObsoleteFreelistBeforeBothSlotsAdvance,
			wantReclaim: false,
			wantCharlie: false,
		},
		{
			name:        "after both metadata slots advance",
			fault:       mmapObsoleteFreelistAfterBothSlotsAdvance,
			wantReclaim: true,
			wantCharlie: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "course.db")
			runMmapObsoleteFreelistProcessCrash(t, path, tt.fault)
			assertMmapObsoleteFreelistProcessCrashRecovered(t, path, tt.fault, tt.wantReclaim, tt.wantCharlie)
		})
	}
}

func TestMmapReadOnlyChildProcessPinsRecyclingUntilRelease(t *testing.T) {
	if os.Getenv(mmapReaderHoldChildEnv) == "1" {
		runMmapReadOnlyHoldChildProcess(t)
		return
	}

	path := filepath.Join(t.TempDir(), "course.db")

	writer, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 24; i++ {
		writer.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := writer.Sync(); err != nil {
		writer.Close()
		t.Fatalf("initial Sync: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close initial writer: %v", err)
	}

	readyPath := filepath.Join(t.TempDir(), "reader.ready")
	releasePath := filepath.Join(t.TempDir(), "reader.release")
	child := runMmapReadOnlyHoldChild(t, path, readyPath, releasePath)
	defer child.cleanup()
	waitForMmapReaderHoldFile(t, readyPath)

	writer, err = OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap concurrent writer: %v", err)
	}
	defer writer.Close()

	stats, err := writer.MmapReaderStats()
	if err != nil {
		t.Fatalf("MmapReaderStats with child reader: %v", err)
	}
	if stats.ActiveSlots != 1 {
		t.Fatalf("ActiveSlots with child reader = %d, want 1", stats.ActiveSlots)
	}

	for i := 0; i < 12; i++ {
		if _, ok := writer.Delete(fmt.Sprintf("key-%02d", i)); !ok {
			t.Fatalf("Delete key-%02d = false, want true", i)
		}
	}
	pinned := writer.Stats()
	if pinned.RetiredPages == 0 || pinned.FreePages != 0 {
		t.Fatalf("writer stats with child reader = retired:%d free:%d, want retired pinned and no free pages", pinned.RetiredPages, pinned.FreePages)
	}

	if err := os.WriteFile(releasePath, []byte("release"), 0o644); err != nil {
		t.Fatalf("release child reader: %v", err)
	}
	child.wait()

	writer.Put("key-99", []byte("value-99"))
	released := writer.Stats()
	if released.RetiredPages != 0 || released.FreePages == 0 {
		t.Fatalf("writer stats after child release = retired:%d free:%d, want retired reclaimed into free list", released.RetiredPages, released.FreePages)
	}
}

func TestMmapReadOnlyChildProcessesPinRecyclingAcrossMutations(t *testing.T) {
	if os.Getenv(mmapReaderHoldChildEnv) == "1" {
		runMmapReadOnlyHoldChildProcess(t)
		return
	}

	path := filepath.Join(t.TempDir(), "course.db")

	writer, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 256})
	if err != nil {
		t.Fatalf("OpenMmap create: %v", err)
	}
	for i := 0; i < 48; i++ {
		writer.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := writer.Sync(); err != nil {
		writer.Close()
		t.Fatalf("initial Sync: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close initial writer: %v", err)
	}

	children := runMmapReadOnlyHoldChildren(t, path, 4)
	defer children.cleanup()

	writer, err = OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 256})
	if err != nil {
		t.Fatalf("OpenMmap concurrent writer: %v", err)
	}
	defer writer.Close()

	stats, err := writer.MmapReaderStats()
	if err != nil {
		t.Fatalf("MmapReaderStats with child readers: %v", err)
	}
	if stats.ActiveSlots != 4 {
		t.Fatalf("ActiveSlots with child readers = %d, want 4", stats.ActiveSlots)
	}

	for round := 0; round < 3; round++ {
		for i := 0; i < 16; i++ {
			key := fmt.Sprintf("key-%02d", round*16+i)
			if _, ok := writer.Delete(key); !ok {
				t.Fatalf("round %d Delete(%s) = false, want true", round, key)
			}
		}
		pinned := writer.Stats()
		if pinned.RetiredPages == 0 || pinned.FreePages != 0 {
			t.Fatalf("round %d writer stats = retired:%d free:%d, want retired pinned and no free pages", round, pinned.RetiredPages, pinned.FreePages)
		}
	}

	children.releaseOne()
	stats, err = writer.MmapReaderStats()
	if err != nil {
		t.Fatalf("MmapReaderStats after first child release: %v", err)
	}
	if stats.ActiveSlots != 3 {
		t.Fatalf("ActiveSlots after first child release = %d, want 3", stats.ActiveSlots)
	}
	if pinned := writer.Stats(); pinned.RetiredPages == 0 || pinned.FreePages != 0 {
		t.Fatalf("writer stats while remaining children live = retired:%d free:%d, want still pinned", pinned.RetiredPages, pinned.FreePages)
	}

	children.releaseAll()
	writer.Put("key-99", []byte("value-99"))
	released := writer.Stats()
	if released.RetiredPages != 0 || released.FreePages == 0 {
		t.Fatalf("writer stats after all children release = retired:%d free:%d, want retired reclaimed into free list", released.RetiredPages, released.FreePages)
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

func runMmapBatchCommitSyncProcessCrash(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	runMmapProcessCrashChild(t, path, fault, "^TestMmapWriteBatchCommitSyncProcessCrashMatrixClassifiesRecoveryRoot$", "batch-sync")
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

func runMmapLegacyUpgradeProcessCrash(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	runMmapProcessCrashChild(t, path, fault, "^TestMmapLegacyMetadataUpgradeProcessCrashMatrixClassifiesRecoveryRoot$", "legacy-upgrade")
}

func runMmapObsoleteFreelistProcessCrash(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	runMmapProcessCrashChild(t, path, fault, "^TestMmapObsoleteFreelistProcessCrashClassifiesMetadataGenerationReclaim$", "obsolete-freelist")
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

type mmapReadOnlyHoldChild struct {
	t       *testing.T
	cmd     *exec.Cmd
	release string
	output  *bytes.Buffer
	waited  bool
}

type mmapReadOnlyHoldChildren struct {
	t        *testing.T
	children []*mmapReadOnlyHoldChild
}

func runMmapReadOnlyHoldChild(t *testing.T, path, readyPath, releasePath string) *mmapReadOnlyHoldChild {
	t.Helper()

	var output bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestMmapReadOnlyChildProcessPinsRecyclingUntilRelease$")
	cmd.Env = append(os.Environ(),
		mmapReaderHoldChildEnv+"=1",
		mmapCrashPathEnv+"="+path,
		mmapReaderHoldReadyEnv+"="+readyPath,
		mmapReaderHoldReleaseEnv+"="+releasePath,
	)
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start read-only hold child: %v", err)
	}
	return &mmapReadOnlyHoldChild{t: t, cmd: cmd, release: releasePath, output: &output}
}

func runMmapReadOnlyHoldChildren(t *testing.T, path string, count int) *mmapReadOnlyHoldChildren {
	t.Helper()

	group := &mmapReadOnlyHoldChildren{t: t}
	for i := 0; i < count; i++ {
		dir := t.TempDir()
		readyPath := filepath.Join(dir, fmt.Sprintf("reader-%02d.ready", i))
		releasePath := filepath.Join(dir, fmt.Sprintf("reader-%02d.release", i))
		child := runMmapReadOnlyHoldChild(t, path, readyPath, releasePath)
		group.children = append(group.children, child)
		waitForMmapReaderHoldFile(t, readyPath)
	}
	return group
}

func (g *mmapReadOnlyHoldChildren) releaseOne() {
	g.t.Helper()

	if len(g.children) == 0 {
		g.t.Fatalf("releaseOne called with no live children")
	}
	child := g.children[0]
	g.children = g.children[1:]
	child.releaseAndWait()
}

func (g *mmapReadOnlyHoldChildren) releaseAll() {
	g.t.Helper()

	for len(g.children) > 0 {
		g.releaseOne()
	}
}

func (g *mmapReadOnlyHoldChildren) cleanup() {
	g.t.Helper()

	for _, child := range g.children {
		child.cleanup()
	}
	g.children = nil
}

func (c *mmapReadOnlyHoldChild) releaseAndWait() {
	c.t.Helper()

	if err := os.WriteFile(c.release, []byte("release"), 0o644); err != nil {
		c.t.Fatalf("release read-only hold child: %v", err)
	}
	c.wait()
}

func (c *mmapReadOnlyHoldChild) wait() {
	c.t.Helper()

	if c.waited {
		return
	}
	c.waited = true
	if err := c.cmd.Wait(); err != nil {
		c.t.Fatalf("read-only hold child failed: %v\n%s", err, c.output.String())
	}
}

func (c *mmapReadOnlyHoldChild) cleanup() {
	c.t.Helper()

	if c.waited {
		return
	}
	c.waited = true
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
}

func waitForMmapReaderHoldFile(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for reader hold file %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runMmapReadOnlyHoldChildProcess(t *testing.T) {
	t.Helper()

	path := os.Getenv(mmapCrashPathEnv)
	readyPath := os.Getenv(mmapReaderHoldReadyEnv)
	releasePath := os.Getenv(mmapReaderHoldReleaseEnv)
	if path == "" || readyPath == "" || releasePath == "" {
		t.Fatalf("missing reader hold child env path=%q ready=%q release=%q", path, readyPath, releasePath)
	}

	reader, err := OpenMmapReadOnly(path)
	if err != nil {
		t.Fatalf("OpenMmapReadOnly child: %v", err)
	}
	defer reader.Close()
	got, ok := reader.Get("key-00")
	if !ok || string(got) != "value-00" {
		t.Fatalf("child reader Get(key-00) = %q, %v; want value-00, true", got, ok)
	}
	if err := os.WriteFile(readyPath, []byte("ready"), 0o644); err != nil {
		t.Fatalf("write reader hold ready file: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(releasePath); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for release file %s", releasePath)
		}
		time.Sleep(10 * time.Millisecond)
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

func runMmapBatchCommitSyncProcessCrashChild(t *testing.T) {
	t.Helper()

	path := os.Getenv(mmapCrashPathEnv)
	fault := mmapFaultPoint(os.Getenv(mmapCrashFaultEnv))
	if path == "" || fault == "" {
		t.Fatalf("missing batch-sync crash child env path=%q fault=%q", path, fault)
	}
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 64})
	if err != nil {
		t.Fatalf("OpenMmap batch-sync child create: %v", err)
	}
	tree.Put("alpha", []byte("one"))
	tree.Put("remove", []byte("old"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("initial batch-sync child Sync: %v", err)
	}

	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == fault {
			os.Exit(mmapCrashChildExitCode)
		}
		return nil
	}
	batch := tree.Batch()
	batch.Put("bravo", []byte("two"))
	batch.Delete("remove")
	if _, err := batch.CommitSyncDetailed(); err != nil {
		t.Fatalf("batch-sync child CommitSyncDetailed before process crash: %v", err)
	}
	t.Fatalf("batch-sync child CommitSyncDetailed completed without hitting fault %s", fault)
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

func runMmapLegacyUpgradeProcessCrashChild(t *testing.T) {
	t.Helper()

	path := os.Getenv(mmapCrashPathEnv)
	fault := mmapFaultPoint(os.Getenv(mmapCrashFaultEnv))
	if path == "" || fault == "" {
		t.Fatalf("missing legacy-upgrade crash child env path=%q fault=%q", path, fault)
	}
	tree, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap legacy-upgrade child open: %v", err)
	}
	defer tree.Close()
	if !tree.metadataUpgradePending {
		t.Fatalf("legacy-upgrade child metadataUpgradePending = false, want legacy upgrade pending")
	}
	tree.arena.markDirtyPage(tree.root)
	tree.arena.faultInjector = func(point mmapFaultPoint) error {
		if point == fault {
			os.Exit(mmapCrashChildExitCode)
		}
		return nil
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("legacy-upgrade child Sync before process crash: %v", err)
	}
	t.Fatalf("legacy-upgrade child Sync completed without hitting fault %s", fault)
}

func runMmapObsoleteFreelistProcessCrashChild(t *testing.T) {
	t.Helper()

	path := os.Getenv(mmapCrashPathEnv)
	fault := mmapFaultPoint(os.Getenv(mmapCrashFaultEnv))
	if path == "" || fault == "" {
		t.Fatalf("missing obsolete-freelist crash child env path=%q fault=%q", path, fault)
	}
	tree, err := OpenMmap(path, MmapOptions{Degree: 2, MaxPages: 4096})
	if err != nil {
		t.Fatalf("OpenMmap obsolete-freelist child create: %v", err)
	}
	defer tree.Close()

	if _, replaced := tree.Put("alpha", []byte("one")); replaced {
		t.Fatalf("obsolete-freelist child first Put replaced existing key")
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("obsolete-freelist child initial Sync: %v", err)
	}
	clear(tree.arena.dirtyPages)

	freeCount := obsoleteFreelistProcessFreeCount()
	tree.free = make([]PageID, freeCount)
	for i := range tree.free {
		tree.free[i] = firstTreePageID + 1 + PageID(i)
	}
	tree.nextPage = firstTreePageID + 1 + PageID(freeCount)

	if err := tree.Sync(); err != nil {
		t.Fatalf("obsolete-freelist child first large-freelist Sync: %v", err)
	}
	if len(tree.metaFreelistPages) == 0 {
		t.Fatalf("obsolete-freelist child first large-freelist Sync did not create metadata pages")
	}

	tree.Put("bravo", []byte("two"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("obsolete-freelist child second large-freelist Sync: %v", err)
	}
	if fault == mmapObsoleteFreelistBeforeBothSlotsAdvance {
		os.Exit(mmapCrashChildExitCode)
	}

	tree.Put("charlie", []byte("three"))
	if err := tree.Sync(); err != nil {
		t.Fatalf("obsolete-freelist child third large-freelist Sync: %v", err)
	}
	if fault == mmapObsoleteFreelistAfterBothSlotsAdvance {
		os.Exit(mmapCrashChildExitCode)
	}
	t.Fatalf("obsolete-freelist child completed without hitting fault %s", fault)
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

func assertMmapBatchCommitSyncProcessCrashRecovered(t *testing.T, path string, fault mmapFaultPoint, wantNewRoot bool) {
	t.Helper()

	recovered, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap after batch-sync process crash at %s: %v", fault, err)
	}
	defer recovered.Close()

	if err := recovered.Check(); err != nil {
		t.Fatalf("Check after batch-sync process crash at %s: %v", fault, err)
	}
	if got, ok := recovered.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get(alpha) after batch-sync process crash at %s = %q, %v; want one, true", fault, got, ok)
	}

	gotBravo, okBravo := recovered.Get("bravo")
	gotRemove, okRemove := recovered.Get("remove")
	if wantNewRoot {
		if !okBravo || string(gotBravo) != "two" {
			t.Fatalf("Get(bravo) after batch-sync process crash at %s = %q, %v; want two, true", fault, gotBravo, okBravo)
		}
		if okRemove {
			t.Fatalf("Get(remove) after batch-sync process crash at %s = %q, true; want deleted", fault, gotRemove)
		}
		return
	}
	if okBravo {
		t.Fatalf("Get(bravo) after batch-sync process crash at %s = %q, true; want old root without bravo", fault, gotBravo)
	}
	if !okRemove || string(gotRemove) != "old" {
		t.Fatalf("Get(remove) after batch-sync process crash at %s = %q, %v; want old, true", fault, gotRemove, okRemove)
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

func assertMmapLegacyUpgradeProcessCrashRecovered(t *testing.T, path string, fault mmapFaultPoint, legacy metaRecord, wantUpgrade bool) {
	t.Helper()

	upgradeIndex := int((legacy.revision + 1) % metaPageCount)
	upgraded, upgradedOK := metaPageRecordAt(t, path, upgradeIndex)
	if wantUpgrade {
		if !upgradedOK {
			t.Fatalf("upgrade metadata slot %d missing after process crash at %s", upgradeIndex, fault)
		}
		if upgraded.version != 2 || upgraded.revision != legacy.revision+1 {
			t.Fatalf("upgrade metadata after process crash at %s = version %d revision %d, want version 2 revision %d", fault, upgraded.version, upgraded.revision, legacy.revision+1)
		}
		if upgraded.freeCount != legacy.freeCount {
			t.Fatalf("upgrade metadata free count after process crash at %s = %d, want %d", fault, upgraded.freeCount, legacy.freeCount)
		}
	} else if upgradedOK && upgraded.revision == legacy.revision+1 {
		t.Fatalf("process crash at %s left checksum-valid upgraded metadata in slot %d: %+v", fault, upgradeIndex, upgraded)
	}

	recovered, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap after legacy-upgrade process crash at %s: %v", fault, err)
	}
	defer recovered.Close()
	if err := recovered.Check(); err != nil {
		t.Fatalf("Check after legacy-upgrade process crash at %s: %v", fault, err)
	}
	if got, ok := recovered.Get("key-19"); !ok || string(got) != "value-19" {
		t.Fatalf("Get(key-19) after legacy-upgrade process crash at %s = %q, %v; want value-19, true", fault, got, ok)
	}
	if got := recovered.Stats().FreePages; got != int(legacy.freeCount) {
		t.Fatalf("FreePages after legacy-upgrade process crash at %s = %d, want %d", fault, got, legacy.freeCount)
	}
	if wantUpgrade {
		if recovered.metadataUpgradePending {
			t.Fatalf("metadataUpgradePending after recovered upgraded metadata at %s = true, want false", fault)
		}
		if got := recovered.Revision(); got != legacy.revision+1 {
			t.Fatalf("Revision after recovered upgraded metadata at %s = %d, want %d", fault, got, legacy.revision+1)
		}
		return
	}
	if !recovered.metadataUpgradePending {
		t.Fatalf("metadataUpgradePending after old metadata recovery at %s = false, want retryable upgrade pending", fault)
	}
	if got := recovered.Revision(); got != legacy.revision {
		t.Fatalf("Revision after old metadata recovery at %s = %d, want %d", fault, got, legacy.revision)
	}
}

func assertMmapObsoleteFreelistProcessCrashRecovered(t *testing.T, path string, fault mmapFaultPoint, wantReclaim bool, wantCharlie bool) {
	t.Helper()

	recovered, err := OpenMmap(path, MmapOptions{})
	if err != nil {
		t.Fatalf("OpenMmap after obsolete-freelist process crash at %s: %v", fault, err)
	}
	defer recovered.Close()

	if err := recovered.Check(); err != nil {
		t.Fatalf("Check after obsolete-freelist process crash at %s: %v", fault, err)
	}
	if got, ok := recovered.Get("alpha"); !ok || string(got) != "one" {
		t.Fatalf("Get(alpha) after obsolete-freelist process crash at %s = %q, %v; want one, true", fault, got, ok)
	}
	if got, ok := recovered.Get("bravo"); !ok || string(got) != "two" {
		t.Fatalf("Get(bravo) after obsolete-freelist process crash at %s = %q, %v; want two, true", fault, got, ok)
	}
	got, ok := recovered.Get("charlie")
	if wantCharlie {
		if !ok || string(got) != "three" {
			t.Fatalf("Get(charlie) after obsolete-freelist process crash at %s = %q, %v; want three, true", fault, got, ok)
		}
	} else if ok {
		t.Fatalf("Get(charlie) after obsolete-freelist process crash at %s = %q, true; want absent", fault, got)
	}

	for _, id := range obsoleteFreelistProcessFirstGenerationPages() {
		reusable := slices.Contains(recovered.free, id)
		if wantReclaim && !reusable {
			t.Fatalf("obsolete metadata page %d after process crash at %s is not reusable", id, fault)
		}
		if !wantReclaim && reusable {
			t.Fatalf("still-referenced metadata page %d after process crash at %s became reusable", id, fault)
		}
	}
}

func obsoleteFreelistProcessFreeCount() int {
	return maxMetaFreePages + 17
}

func obsoleteFreelistProcessFirstGenerationPages() []PageID {
	freeCount := obsoleteFreelistProcessFreeCount()
	pageCount := divideRoundUp(freeCount, freelistPageCapacity)
	first := firstTreePageID + 1 + PageID(freeCount)
	ids := make([]PageID, pageCount)
	for i := range ids {
		ids[i] = first + PageID(i)
	}
	return ids
}
