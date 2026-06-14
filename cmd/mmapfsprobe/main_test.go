package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func TestRunFilesystemProbePrintsSpaceEvidence(t *testing.T) {
	if !pagebtree.MmapPlatformProfile().MmapSupported {
		t.Skip("mmap filesystem probe requires mmap support")
	}
	path := filepath.Join(t.TempDir(), "probe.db")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--keys", "96", "--value-bytes", "256", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var report fsProbeReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.Path != path {
		t.Fatalf("Path = %q, want %q", report.Path, path)
	}
	if report.KeysInserted != 96 || report.KeysDeleted == 0 {
		t.Fatalf("key counts inserted/deleted = %d/%d, want inserted 96 and some deleted", report.KeysInserted, report.KeysDeleted)
	}
	if report.ValueBytes != 256 {
		t.Fatalf("ValueBytes = %d, want 256", report.ValueBytes)
	}
	if report.Platform.GOOS == "" || report.Platform.HolePunch.Platform == "" {
		t.Fatalf("Platform = %+v, want populated platform and hole-punch profile", report.Platform)
	}
	if report.AfterInsert.Space.LogicalFileBytes == 0 {
		t.Fatalf("after insert space = %+v, want logical file bytes", report.AfterInsert.Space)
	}
	if report.AfterInsert.Space.FilesystemType == "" && report.AfterInsert.Space.FilesystemTypeID == 0 {
		t.Fatalf("after insert filesystem identity missing: %+v", report.AfterInsert.Space)
	}
	if report.AfterDelete.Stats.FreePages == 0 {
		t.Fatalf("after delete free pages = 0, want reusable pages")
	}
	if report.AfterDelete.Space.LogicalFileBytes == 0 || report.AfterCompact.Space.LogicalFileBytes == 0 || report.AfterPunch.Space.LogicalFileBytes == 0 {
		t.Fatalf("space phases missing logical bytes: %+v %+v %+v", report.AfterDelete.Space, report.AfterCompact.Space, report.AfterPunch.Space)
	}
	if report.AfterCompact.Space.LogicalFileBytes > report.AfterDelete.Space.LogicalFileBytes {
		t.Fatalf("compact grew logical file bytes from %d to %d", report.AfterDelete.Space.LogicalFileBytes, report.AfterCompact.Space.LogicalFileBytes)
	}
	if report.AfterPunch.Space.LogicalFileBytes != report.AfterCompact.Space.LogicalFileBytes {
		t.Fatalf("punch changed logical file bytes from %d to %d", report.AfterCompact.Space.LogicalFileBytes, report.AfterPunch.Space.LogicalFileBytes)
	}
	if report.PunchStats.FreePages == 0 {
		t.Fatalf("PunchStats.FreePages = 0, want free pages considered")
	}
}

func TestRunFilesystemProbeRejectsInvalidCounts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--keys", "1", "probe.db"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--keys") {
		t.Fatalf("stderr = %q, want --keys validation", stderr.String())
	}
}

func TestRunFilesystemProbeRefusesExistingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "probe.db")
	if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{path}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run exit = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("stderr = %q, want existing-path error", stderr.String())
	}
}
