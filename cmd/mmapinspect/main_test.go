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
	if report.KeySample != nil {
		t.Fatalf("KeySample = %+v, want nil without --keys", report.KeySample)
	}
	if report.PageSummaries != nil {
		t.Fatalf("PageSummaries = %+v, want nil without --pages", report.PageSummaries)
	}
}

func TestRunPrintsPersistedKeyOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.db")
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{
		Degree:   2,
		MaxPages: 128,
		KeyOrder: pagebtree.KeyOrderReverse,
	})
	if err != nil {
		t.Fatalf("OpenMmap reverse: %v", err)
	}
	for _, key := range []string{"alpha", "bravo", "charlie"} {
		tree.Put(key, []byte("value-"+key))
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close reverse: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--keys", "2", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report inspectReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.KeyOrder != pagebtree.KeyOrderReverse {
		t.Fatalf("KeyOrder = %d, want reverse", report.KeyOrder)
	}
	if report.KeyOrderName != "reverse" {
		t.Fatalf("KeyOrderName = %q, want reverse", report.KeyOrderName)
	}
	if report.KeyComparator != pagebtree.KeyComparatorReverse {
		t.Fatalf("KeyComparator = %d, want reverse", report.KeyComparator)
	}
	if report.KeyComparatorName != "reverse" {
		t.Fatalf("KeyComparatorName = %q, want reverse", report.KeyComparatorName)
	}
	if report.KeySample == nil {
		t.Fatalf("KeySample = nil, want section with --keys")
	}
	wantFirst := []string{"charlie", "bravo"}
	if fmt.Sprint(report.KeySample.First) != fmt.Sprint(wantFirst) {
		t.Fatalf("KeySample.First = %v, want %v", report.KeySample.First, wantFirst)
	}
	wantLast := []string{"bravo", "alpha"}
	if fmt.Sprint(report.KeySample.Last) != fmt.Sprint(wantLast) {
		t.Fatalf("KeySample.Last = %v, want %v", report.KeySample.Last, wantLast)
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

func TestRunPrintsKeySample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.db")
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	for i := range 6 {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--keys=2", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report inspectReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.KeySample == nil {
		t.Fatalf("KeySample = nil, want section with --keys")
	}
	if report.KeySample.Limit != 2 {
		t.Fatalf("KeySample.Limit = %d, want 2", report.KeySample.Limit)
	}
	if !report.KeySample.Truncated {
		t.Fatalf("KeySample.Truncated = false, want true")
	}
	wantFirst := []string{"key-00", "key-01"}
	if fmt.Sprint(report.KeySample.First) != fmt.Sprint(wantFirst) {
		t.Fatalf("KeySample.First = %v, want %v", report.KeySample.First, wantFirst)
	}
	wantLast := []string{"key-04", "key-05"}
	if fmt.Sprint(report.KeySample.Last) != fmt.Sprint(wantLast) {
		t.Fatalf("KeySample.Last = %v, want %v", report.KeySample.Last, wantLast)
	}
}

func TestRunPrintsPageSummaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.db")
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	for i := range 40 {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	tree.Put("large", bytes.Repeat([]byte("x"), pagebtree.PageSize*2+17))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--pages", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report inspectReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if len(report.PageSummaries) < report.Stats.Pages {
		t.Fatalf("PageSummaries = %d, want at least Stats.Pages %d", len(report.PageSummaries), report.Stats.Pages)
	}
	reachableSummaries := 0
	var sawRootBranch, sawLeaf, sawOverflow bool
	for _, summary := range report.PageSummaries {
		if summary.Role == "reachable" {
			reachableSummaries++
		}
		if summary.ID == report.Stats.Root && summary.Role == "reachable" && summary.Kind == "branch" && len(summary.Children) > 0 {
			sawRootBranch = true
		}
		if summary.Role == "reachable" && summary.Kind == "leaf" && summary.Keys > 0 {
			sawLeaf = true
		}
		if summary.Role == "reachable" && summary.Kind == "overflow" && summary.BytesUsed > 0 {
			sawOverflow = true
		}
	}
	if reachableSummaries != report.Stats.Pages {
		t.Fatalf("reachable page summaries = %d, want Stats.Pages %d", reachableSummaries, report.Stats.Pages)
	}
	if !sawRootBranch || !sawLeaf || !sawOverflow {
		t.Fatalf("page summaries missing root/leaf/overflow evidence: root=%v leaf=%v overflow=%v summaries=%+v", sawRootBranch, sawLeaf, sawOverflow, report.PageSummaries)
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

func TestRunRejectsInvalidKeySampleLimit(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--keys=0", "db"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "positive") {
		t.Fatalf("stderr = %q, want positive limit error", stderr.String())
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
