//go:build unix

package pagebtree

import (
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

func runMmapSyncProcessCrash(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	runMmapProcessCrashChild(t, path, fault, "^TestMmapSyncProcessCrashMatrixClassifiesRecoveryRoot$", "sync")
}

func runMmapGrowthProcessCrash(t *testing.T, path string, fault mmapFaultPoint) {
	t.Helper()

	runMmapProcessCrashChild(t, path, fault, "^TestMmapGrowthProcessCrashMatrixClassifiesOldRoot$", "growth")
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
