package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func TestRunPrintsAuditJSONForMmapDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.db")
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	for i := range 40 {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	tree.Put("large", bytes.Repeat([]byte("x"), pagebtree.PageSize*2+31))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var report inspectReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if !report.Valid || report.Error != "" {
		t.Fatalf("valid/error = %v/%q, want valid with no error", report.Valid, report.Error)
	}
	if report.Stats.Storage != "mmap" {
		t.Fatalf("storage = %q, want mmap", report.Stats.Storage)
	}
	if report.Stats.SyncedRevision != report.Stats.Revision {
		t.Fatalf("synced revision = %d, want revision %d", report.Stats.SyncedRevision, report.Stats.Revision)
	}
	if len(report.ReachablePageIDs) != report.Stats.Pages {
		t.Fatalf("reachable page IDs = %d, want Stats.Pages %d", len(report.ReachablePageIDs), report.Stats.Pages)
	}
	if report.Stats.OverflowPages == 0 {
		t.Fatalf("OverflowPages = 0, want large value evidence")
	}
}

func TestRunRejectsWrongArgumentCount(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}
