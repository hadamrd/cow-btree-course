package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func TestRunPunchesMmapFreePagesAndPrintsJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "punch.db")
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	for i := range 8 {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
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

	var report punchReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.Path != path {
		t.Fatalf("Path = %q, want %q", report.Path, path)
	}
	if report.HolePunchProfile.Platform == "" {
		t.Fatalf("HolePunchProfile = %+v, want platform", report.HolePunchProfile)
	}
	if !report.HolePunchProfile.RequiresPageAlignedRanges {
		t.Fatalf("RequiresPageAlignedRanges = false, want true")
	}
	if report.BeforeSpace.LogicalFileBytes == 0 || report.AfterSpace.LogicalFileBytes == 0 {
		t.Fatalf("space stats before/after = %+v/%+v, want logical bytes", report.BeforeSpace, report.AfterSpace)
	}
	if report.BeforeSpace.MappedBytes != report.AfterSpace.MappedBytes {
		t.Fatalf("mapped bytes changed = %d -> %d, want stable logical file mapping", report.BeforeSpace.MappedBytes, report.AfterSpace.MappedBytes)
	}
	if report.PunchStats.PunchedPages != 0 || report.PunchStats.Ranges != 0 {
		t.Fatalf("PunchStats = %+v, want no free ranges for fresh small db", report.PunchStats)
	}
}

func TestRunWritesTraceJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "punch.db")
	tracePath := filepath.Join(dir, "punch.jsonl")
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{Degree: 2, MaxPages: 32})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	for i := range 8 {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--trace", tracePath, path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report punchReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.TracePath != tracePath {
		t.Fatalf("TracePath = %q, want %q", report.TracePath, tracePath)
	}
	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if !bytes.Contains(traceBytes, []byte(`"kind":"mmap-punch-begin"`)) {
		t.Fatalf("trace %q does not contain mmap-punch-begin", string(traceBytes))
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

func TestRunRejectsMissingTracePath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--trace"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--trace") {
		t.Fatalf("stderr = %q, want trace usage error", stderr.String())
	}
}
