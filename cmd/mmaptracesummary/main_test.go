package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSummarizesTraceJSONLAsMarkdown(t *testing.T) {
	input := strings.Join([]string{
		`{"kind":"mmap-sync-begin","revision":7,"root":3,"next_page":12}`,
		`{"kind":"mmap-sync-data-range","revision":7,"start_page":4,"end_page":8,"duration_nanos":1234}`,
		`{"kind":"mmap-punch-range","start_page":20,"end_page":23}`,
		`{"kind":"mmap-punch-failed","punch_ranges":1,"punched_pages":3,"punched_bytes":12288,"reason":"operation not supported"}`,
		`{"kind":"mmap-sync-end","revision":7,"root":3,"next_page":12}`,
	}, "\n")

	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"# mmap Trace Summary",
		"| Events | 5 |",
		"| Dirty data ranges | 1 |",
		"| Dirty data pages | 4 |",
		"| Punch ranges | 1 |",
		"| Punched pages | 3 |",
		"| Failures | 1 |",
		"| mmap-punch-failed | 1 |",
		"| 4 | mmap-punch-failed | 0 | 0 | 0 | 3 | 0 | operation not supported |",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("summary missing %q in:\n%s", want, output)
		}
	}
}

func TestRunSummarizesTransactionConflicts(t *testing.T) {
	input := strings.Join([]string{
		`{"kind":"mmap-sync-begin","revision":7,"root":3,"next_page":12}`,
		`{"kind":"mmap-tx-conflict","revision":8,"root":4,"next_page":13,"reason":"read-write transaction conflict"}`,
		`{"kind":"mmap-tx-conflict","revision":9,"root":5,"next_page":14,"reason":"tenant|writer conflict"}`,
	}, "\n")

	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"| Transaction conflicts | 2 |",
		"| Transaction conflict reasons | read-write transaction conflict; tenant\\|writer conflict |",
		"| mmap-tx-conflict | 2 |",
		"| 2 | mmap-tx-conflict | 8 | 4 | 13 | 0 | 0 | read-write transaction conflict |",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("summary missing %q in:\n%s", want, output)
		}
	}
}

func TestRunReadsTraceFilesAndHonorsTimelineLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	content := strings.Join([]string{
		`{"kind":"mmap-sync-begin","revision":1}`,
		`{"kind":"mmap-sync-data-range","revision":1,"start_page":2,"end_page":4}`,
		`{"kind":"mmap-sync-meta-published","revision":1,"metadata_slot":1}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile trace: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--limit", "2", path}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "| Inputs | trace.jsonl |") {
		t.Fatalf("summary missing input basename:\n%s", output)
	}
	if !strings.Contains(output, "| Timeline truncated | true |") {
		t.Fatalf("summary missing truncation flag:\n%s", output)
	}
	timeline := output[strings.Index(output, "## Timeline"):]
	if strings.Contains(timeline, "mmap-sync-meta-published") {
		t.Fatalf("timeline includes event beyond limit:\n%s", output)
	}
}

func TestRunRejectsMalformedTraceJSONLWithLineNumber(t *testing.T) {
	input := strings.NewReader("{bad-json}\n")
	var stdout, stderr bytes.Buffer
	code := run(nil, input, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("run exit = 0, want failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty on error", stdout.String())
	}
	if !strings.Contains(stderr.String(), "line 1") {
		t.Fatalf("stderr = %q, want line number", stderr.String())
	}
}
