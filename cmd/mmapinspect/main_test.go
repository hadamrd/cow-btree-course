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
	if report.ReaderStats != nil {
		t.Fatalf("ReaderStats = %+v, want nil without --readers", report.ReaderStats)
	}
	if report.CacheStats != nil {
		t.Fatalf("CacheStats = %+v, want nil without --cache", report.CacheStats)
	}
}

func TestRunPrintsOptionalReaderAndCacheSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.db")
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	for i := range 24 {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	pinned, err := pagebtree.OpenMmapReadOnly(path)
	if err != nil {
		t.Fatalf("OpenMmapReadOnly pinned reader: %v", err)
	}
	defer pinned.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--readers", "--cache", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report inspectReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.ReaderStats == nil {
		t.Fatalf("ReaderStats = nil, want section with --readers")
	}
	if report.ReaderStats.ActiveSlots < 2 {
		t.Fatalf("ActiveSlots = %d, want pinned reader plus inspector", report.ReaderStats.ActiveSlots)
	}
	if !report.ReaderStats.HasOldestRevision {
		t.Fatalf("HasOldestRevision = false, want true with active readers")
	}
	if report.CacheStats == nil {
		t.Fatalf("CacheStats = nil, want section with --cache")
	}
	if report.CacheStats.MappedDatabasePages == 0 || report.CacheStats.OSPages == 0 {
		t.Fatalf("cache stats = %+v, want mapped pages and OS pages", *report.CacheStats)
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

func TestRunRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--bogus", "db"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unknown option") {
		t.Fatalf("stderr = %q, want unknown option", stderr.String())
	}
}
