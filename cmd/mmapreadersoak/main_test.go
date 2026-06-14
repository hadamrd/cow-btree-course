package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandRunsReaderSoakAndPrintsJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "soak.db")
	cmd := exec.Command("go", "run", ".", "--readers", "2", "--rounds", "2", "--keys", "24", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run mmapreadersoak failed: %v\n%s", err, output)
	}

	var report soakReport
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", string(output), err)
	}
	if report.Path != path {
		t.Fatalf("Path = %q, want %q", report.Path, path)
	}
	if report.Readers != 2 || report.Rounds != 2 || report.Keys != 24 {
		t.Fatalf("report config = readers:%d rounds:%d keys:%d, want 2/2/24", report.Readers, report.Rounds, report.Keys)
	}
	if report.ActiveReadersObserved != 2 {
		t.Fatalf("ActiveReadersObserved = %d, want 2", report.ActiveReadersObserved)
	}
	if report.ActiveReadersAfterOneRelease != 1 {
		t.Fatalf("ActiveReadersAfterOneRelease = %d, want 1", report.ActiveReadersAfterOneRelease)
	}
	if report.PinnedRounds != 2 {
		t.Fatalf("PinnedRounds = %d, want 2", report.PinnedRounds)
	}
	if report.MaxRetiredPagesWhilePinned == 0 {
		t.Fatalf("MaxRetiredPagesWhilePinned = 0, want retired pages pinned by child readers")
	}
	if report.MaxFreePagesWhilePinned != 0 {
		t.Fatalf("MaxFreePagesWhilePinned = %d, want no free pages while child readers pin retired pages", report.MaxFreePagesWhilePinned)
	}
	if report.FinalRetiredPages != 0 {
		t.Fatalf("FinalRetiredPages = %d, want reclaimed retired pages after all readers exit", report.FinalRetiredPages)
	}
	if report.FinalFreePages == 0 {
		t.Fatalf("FinalFreePages = 0, want reusable pages after all readers exit")
	}
}

func TestRunRejectsInvalidSoakArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--readers", "0", "db"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--readers") {
		t.Fatalf("stderr = %q, want readers validation", stderr.String())
	}
}
