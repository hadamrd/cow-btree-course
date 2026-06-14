package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandSimulatesMetadataAndRootTears(t *testing.T) {
	for _, mode := range []string{"metadata", "root"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), mode+".db")
			cmd := exec.Command("go", "run", ".", "--mode", mode, path)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go run mmaptearlab %s failed: %v\n%s", mode, err, output)
			}

			var report tearReport
			if err := json.Unmarshal(output, &report); err != nil {
				t.Fatalf("invalid JSON %q: %v", string(output), err)
			}
			if report.Path != path {
				t.Fatalf("Path = %q, want %q", report.Path, path)
			}
			if report.Mode != mode {
				t.Fatalf("Mode = %q, want %q", report.Mode, mode)
			}
			if report.OlderRevision == 0 || report.NewerRevision <= report.OlderRevision {
				t.Fatalf("revisions older/newer = %d/%d, want advancing revisions", report.OlderRevision, report.NewerRevision)
			}
			if report.OlderRoot == 0 || report.NewerRoot == 0 || report.OlderRoot == report.NewerRoot {
				t.Fatalf("roots older/newer = %d/%d, want distinct nonzero copy-on-write roots", report.OlderRoot, report.NewerRoot)
			}
			if report.RecoveredRevision != report.OlderRevision {
				t.Fatalf("RecoveredRevision = %d, want older revision %d", report.RecoveredRevision, report.OlderRevision)
			}
			if !report.RecoveredOldKey {
				t.Fatalf("RecoveredOldKey = false, want old durable key recovered")
			}
			if report.RecoveredNewKey {
				t.Fatalf("RecoveredNewKey = true, want newest torn key absent after fallback")
			}
			if !report.FellBackToOlderRoot {
				t.Fatalf("FellBackToOlderRoot = false, want fallback evidence")
			}
			if report.OpenError != "" {
				t.Fatalf("OpenError = %q, want empty", report.OpenError)
			}
		})
	}
}

func TestRunRejectsUnknownMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--mode", "bad", "db"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "mode") {
		t.Fatalf("stderr = %q, want mode error", stderr.String())
	}
}
