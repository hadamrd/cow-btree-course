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

func TestRunPrintsOptionalSpaceSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspect.db")
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{Degree: 2, MaxPages: 128})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	for i := range 32 {
		tree.Put(fmt.Sprintf("key-%02d", i), []byte(fmt.Sprintf("value-%02d", i)))
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--space", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report inspectReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.SpaceStats == nil {
		t.Fatalf("SpaceStats = nil, want section with --space")
	}
	if report.SpaceStats.LogicalFileBytes == 0 || report.SpaceStats.AllocatedBytes == 0 {
		t.Fatalf("SpaceStats = %+v, want logical and allocated bytes", *report.SpaceStats)
	}
	wantSparseBytes := report.SpaceStats.LogicalFileBytes - report.SpaceStats.AllocatedBytes
	if wantSparseBytes < 0 {
		wantSparseBytes = 0
	}
	if report.SpaceStats.SparseBytes != wantSparseBytes {
		t.Fatalf("SparseBytes = %d, want max(logical-allocated,0) %d", report.SpaceStats.SparseBytes, wantSparseBytes)
	}
	if report.HolePunchProfile == nil {
		t.Fatalf("HolePunchProfile = nil, want section with --space")
	}
	if report.HolePunchProfile.Platform == "" || !report.HolePunchProfile.RequiresPageAlignedRanges {
		t.Fatalf("HolePunchProfile = %+v, want platform and page-aligned range contract", *report.HolePunchProfile)
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

func TestRunPrintsMetadataPageSummaries(t *testing.T) {
	path := copyInspectFixture(t, "mmap-v2-chained-freelist.db")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--pages", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report inspectReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if len(report.MetadataPageIDs) < 2 {
		t.Fatalf("MetadataPageIDs = %v, want chained freelist metadata pages", report.MetadataPageIDs)
	}
	metadataSummaries := 0
	for _, summary := range report.PageSummaries {
		if summary.Role != "metadata" {
			continue
		}
		metadataSummaries++
		if summary.Kind != "freelist" || summary.MetadataRecords == 0 {
			t.Fatalf("metadata summary = %+v, want freelist metadata records", summary)
		}
	}
	if metadataSummaries != len(report.MetadataPageIDs) {
		t.Fatalf("metadata summaries = %d, want MetadataPageIDs count %d", metadataSummaries, len(report.MetadataPageIDs))
	}
}

func TestRunCorrelatesTraceJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inspect.db")
	tracePath := filepath.Join(dir, "trace.jsonl")
	var trace bytes.Buffer
	exporter := pagebtree.NewMmapTraceJSONLExporter(&trace)
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{
		Degree:    2,
		MaxPages:  8,
		TraceHook: exporter.Hook(),
	})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	for i := range 120 {
		tree.Put(fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("value-%03d", i)))
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync after put: %v", err)
	}
	for i := range 100 {
		tree.Delete(fmt.Sprintf("key-%03d", i))
	}
	if err := tree.Sync(); err != nil {
		t.Fatalf("Sync after delete: %v", err)
	}
	if err := tree.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	traceHook := exporter.Hook()
	traceHook(pagebtree.MmapTraceEvent{Kind: pagebtree.MmapTracePunchBegin, Revision: tree.Revision()})
	traceHook(pagebtree.MmapTraceEvent{
		Kind:         pagebtree.MmapTracePunchRange,
		Revision:     tree.Revision(),
		StartPage:    5,
		EndPage:      8,
		PunchedPages: 3,
		PunchedBytes: 3 * pagebtree.PageSize,
	})
	traceHook(pagebtree.MmapTraceEvent{
		Kind:         pagebtree.MmapTracePunchRange,
		Revision:     tree.Revision(),
		StartPage:    11,
		EndPage:      13,
		PunchedPages: 2,
		PunchedBytes: 2 * pagebtree.PageSize,
	})
	traceHook(pagebtree.MmapTraceEvent{
		Kind:                    pagebtree.MmapTracePunchEnd,
		Revision:                tree.Revision(),
		FreePages:               9,
		SkippedRecoverablePages: 4,
		PunchRanges:             2,
		PunchedPages:            5,
		PunchedBytes:            5 * pagebtree.PageSize,
	})
	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := exporter.Err(); err != nil {
		t.Fatalf("trace exporter Err: %v", err)
	}
	if err := os.WriteFile(tracePath, trace.Bytes(), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--trace", tracePath, path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report inspectReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.TraceSummary == nil {
		t.Fatalf("TraceSummary = nil, want section with --trace")
	}
	summary := report.TraceSummary
	if summary.Path != tracePath {
		t.Fatalf("TraceSummary.Path = %q, want %q", summary.Path, tracePath)
	}
	if summary.Events == 0 {
		t.Fatalf("TraceSummary.Events = 0, want trace events")
	}
	for _, kind := range []string{"mmap-sync-end", "mmap-growth-begin", "mmap-compact-end"} {
		if summary.KindCounts[kind] == 0 {
			t.Fatalf("TraceSummary.KindCounts[%q] = 0 in %#v", kind, summary.KindCounts)
		}
	}
	if summary.DirtyDataRanges == 0 || summary.DirtyDataPages == 0 {
		t.Fatalf("dirty range summary = ranges %d pages %d, want nonzero", summary.DirtyDataRanges, summary.DirtyDataPages)
	}
	if summary.PunchRanges != 2 || summary.PunchedPages != 5 || summary.PunchedBytes != 5*pagebtree.PageSize {
		t.Fatalf("punch summary = ranges %d pages %d bytes %d, want 2/5/%d", summary.PunchRanges, summary.PunchedPages, summary.PunchedBytes, 5*pagebtree.PageSize)
	}
	if len(summary.Timeline) == 0 {
		t.Fatalf("TraceSummary.Timeline = empty, want ordered phase evidence")
	}
	if summary.Timeline[0].EventIndex != 1 || summary.Timeline[0].Kind == "" {
		t.Fatalf("first timeline phase = %+v, want first event index and kind", summary.Timeline[0])
	}
	var sawPunchRange bool
	for _, phase := range summary.Timeline {
		if phase.Kind == string(pagebtree.MmapTracePunchRange) && phase.StartPage == 5 && phase.EndPage == 8 && phase.Pages == 3 {
			sawPunchRange = true
		}
	}
	if !sawPunchRange {
		t.Fatalf("TraceSummary.Timeline missing punch range phase: %+v", summary.Timeline)
	}
	if summary.SkippedRecoverablePages != 4 || summary.MaxPunchRangePages != 3 {
		t.Fatalf("punch skipped/max range = %d/%d, want 4/3", summary.SkippedRecoverablePages, summary.MaxPunchRangePages)
	}
	if !summary.MatchesCurrentRevision || !summary.MatchesCurrentRoot || !summary.MatchesCurrentNextPage {
		t.Fatalf("trace/current match flags = revision %v root %v nextPage %v, summary=%+v stats=%+v",
			summary.MatchesCurrentRevision, summary.MatchesCurrentRoot, summary.MatchesCurrentNextPage, *summary, report.Stats)
	}
	if summary.HasFailures {
		t.Fatalf("HasFailures = true, reasons=%v", summary.FailureReasons)
	}
}

func copyInspectFixture(t *testing.T, name string) string {
	t.Helper()
	source := filepath.Join("..", "..", "pagebtree", "testdata", name)
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("ReadFile fixture %s: %v", source, err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile fixture copy: %v", err)
	}
	return path
}

func TestRunCorrelatesTracePunchFailures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inspect.db")
	tracePath := filepath.Join(dir, "trace.jsonl")
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{Degree: 2, MaxPages: 16})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	tree.Put("key", []byte("value"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var trace bytes.Buffer
	exporter := pagebtree.NewMmapTraceJSONLExporter(&trace)
	exporter.Hook()(pagebtree.MmapTraceEvent{
		Kind:                    pagebtree.MmapTracePunchFailed,
		Revision:                1,
		StartPage:               6,
		EndPage:                 8,
		FreePages:               5,
		SkippedRecoverablePages: 1,
		PunchRanges:             1,
		PunchedPages:            3,
		PunchedBytes:            3 * pagebtree.PageSize,
		Reason:                  "punch denied",
	})
	if err := exporter.Err(); err != nil {
		t.Fatalf("trace exporter Err: %v", err)
	}
	if err := os.WriteFile(tracePath, trace.Bytes(), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--trace", tracePath, path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report inspectReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	summary := report.TraceSummary
	if summary == nil {
		t.Fatalf("TraceSummary = nil, want section with --trace")
	}
	if !summary.HasFailures || summary.PunchFailures != 1 {
		t.Fatalf("failure summary = has %v punch %d reasons=%v", summary.HasFailures, summary.PunchFailures, summary.FailureReasons)
	}
	if len(summary.FailureReasons) != 1 || summary.FailureReasons[0] != "punch denied" {
		t.Fatalf("FailureReasons = %v, want punch denied", summary.FailureReasons)
	}
	if summary.SkippedRecoverablePages != 1 || summary.PunchedPages != 3 || summary.PunchedBytes != 3*pagebtree.PageSize {
		t.Fatalf("punch failure counters = skipped %d pages %d bytes %d", summary.SkippedRecoverablePages, summary.PunchedPages, summary.PunchedBytes)
	}
	if len(summary.Timeline) != 1 {
		t.Fatalf("Timeline = %+v, want one failure phase", summary.Timeline)
	}
	if phase := summary.Timeline[0]; phase.Kind != string(pagebtree.MmapTracePunchFailed) || phase.Reason != "punch denied" || phase.Pages != 2 {
		t.Fatalf("failure timeline phase = %+v, want value-free failed range with reason", phase)
	}
}

func TestInspectTraceBoundsTimeline(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "long-trace.jsonl")
	var trace bytes.Buffer
	exporter := pagebtree.NewMmapTraceJSONLExporter(&trace)
	for i := range traceTimelineLimit + 5 {
		exporter.Hook()(pagebtree.MmapTraceEvent{
			Kind:     pagebtree.MmapTraceSyncBegin,
			Revision: uint64(i + 1),
			Root:     pagebtree.PageID(i + 2),
			NextPage: pagebtree.PageID(i + 3),
		})
	}
	if err := exporter.Err(); err != nil {
		t.Fatalf("trace exporter Err: %v", err)
	}
	if err := os.WriteFile(tracePath, trace.Bytes(), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	summary, err := inspectTrace(tracePath, pagebtree.Stats{})
	if err != nil {
		t.Fatalf("inspectTrace: %v", err)
	}
	if got := len(summary.Timeline); got != traceTimelineLimit {
		t.Fatalf("Timeline length = %d, want bounded %d", got, traceTimelineLimit)
	}
	if !summary.TimelineTruncated {
		t.Fatalf("TimelineTruncated = false, want true for oversized trace")
	}
	last := summary.Timeline[len(summary.Timeline)-1]
	if last.EventIndex != traceTimelineLimit {
		t.Fatalf("last retained EventIndex = %d, want %d", last.EventIndex, traceTimelineLimit)
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

func TestRunRejectsMalformedTraceJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inspect.db")
	tracePath := filepath.Join(dir, "bad-trace.jsonl")
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{Degree: 2, MaxPages: 16})
	if err != nil {
		t.Fatalf("OpenMmap: %v", err)
	}
	tree.Put("key", []byte("value"))
	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.WriteFile(tracePath, []byte("{bad json}\n"), 0o644); err != nil {
		t.Fatalf("write malformed trace: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--trace", tracePath, path}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run exit = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "trace") || !strings.Contains(stderr.String(), "line 1") {
		t.Fatalf("stderr = %q, want trace line error", stderr.String())
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
