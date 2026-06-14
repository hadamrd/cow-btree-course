package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func TestRunEmitsWorkloadTraceJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.db")
	var stdout, stderr bytes.Buffer

	if code := run([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), path) {
		t.Fatalf("stderr = %q, want traced database path", stderr.String())
	}

	events := decodeTraceEvents(t, stdout.String())
	if len(events) == 0 {
		t.Fatalf("trace output is empty")
	}
	wantKinds := []pagebtree.MmapTraceEventKind{
		pagebtree.MmapTraceSyncBegin,
		pagebtree.MmapTraceSyncDataRange,
		pagebtree.MmapTraceGrowthBegin,
		pagebtree.MmapTraceCompactBegin,
		pagebtree.MmapTraceCompactEnd,
	}
	for _, kind := range wantKinds {
		if !hasTraceKind(events, kind) {
			t.Fatalf("trace kinds missing %s in %v", kind, traceKinds(events))
		}
	}
	if strings.Contains(stdout.String(), "trace-key") || strings.Contains(stdout.String(), "trace-value") {
		t.Fatalf("trace output leaked demo keys or values: %q", stdout.String())
	}
}

func TestRunRejectsTooManyArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"one.db", "two.db"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func decodeTraceEvents(t *testing.T, output string) []pagebtree.MmapTraceEvent {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	events := make([]pagebtree.MmapTraceEvent, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var event pagebtree.MmapTraceEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode trace line %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func hasTraceKind(events []pagebtree.MmapTraceEvent, kind pagebtree.MmapTraceEventKind) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func traceKinds(events []pagebtree.MmapTraceEvent) []pagebtree.MmapTraceEventKind {
	kinds := make([]pagebtree.MmapTraceEventKind, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}
