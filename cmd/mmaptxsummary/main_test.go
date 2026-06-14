package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleTxWorkloadReport = `{
  "path_redacted": true,
  "trace_path_redacted": true,
  "transactions": 8,
  "delete_every": 2,
  "committed": 4,
  "conflicted": 4,
  "deleted_committed_keys": 2,
  "reopened_committed_keys": 2,
  "reopened_deleted_keys": 0,
  "reopened_conflicted_keys": 0,
  "reopened_outside_keys": 4,
  "readers": 2,
  "active_readers_observed": 2,
  "reader_pinned_retired_pages": 19,
  "reader_pinned_free_pages": 1,
  "reader_pinned_reclaim_pressure": {
    "ReaderPinnedRetiredPages": 19,
    "BlockedByReaders": true
  },
  "final_stats": {
    "Len": 8,
    "Revision": 10,
    "SyncedRevision": 10,
    "RetiredPages": 0,
    "FreePages": 20
  },
  "transaction_conflict_kind": "mmap-tx-conflict"
}`

func TestRunSummarizesMmapTxWorkloadReportAsMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "redacted-tx.json")
	if err := os.WriteFile(path, []byte(sampleTxWorkloadReport), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{path}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"| Report | Tx | Delete every | Readers | Committed | Conflicted | Deleted committed | Reopened committed | Reopened deleted | Reopened conflicted | Outside keys | Synced revision | Pinned retired | Pinned free | Blocked by readers | Final retired | Final free | Conflict kind |",
		"| redacted-tx.json | 8 | 2 | 2/2 | 4 | 4 | 2 | 2 | 0 | 0 | 4 | 10/10 | 19 | 1 | true | 0 | 20 | mmap-tx-conflict |",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestRunReadsMmapTxWorkloadReportFromStdin(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(sampleTxWorkloadReport), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "| stdin | 8 | 2 | 2/2 |") {
		t.Fatalf("stdout missing stdin report row:\n%s", stdout.String())
	}
}

func TestRunPrefersMmapTxWorkloadLabelOverInputName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "redacted-tx.json")
	report := strings.Replace(sampleTxWorkloadReport, `"path_redacted": true,`, `"label": "reader-pinned-local",
  "path_redacted": true,`, 1)
	if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{path}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "| reader-pinned-local | 8 | 2 | 2/2 |") {
		t.Fatalf("stdout missing label report row:\n%s", output)
	}
	if strings.Contains(output, "redacted-tx.json") {
		t.Fatalf("stdout used input name despite label:\n%s", output)
	}
}

func TestRunSummarizesMmapTxWorkloadReaderProcesses(t *testing.T) {
	report := strings.Replace(sampleTxWorkloadReport, `"path_redacted": true,`, `"label": "process-pinned-local",
  "path_redacted": true,`, 1)
	report = strings.Replace(report, `"readers": 2,`, `"readers": 0,
  "reader_processes": 1,`, 1)
	report = strings.Replace(report, `"active_readers_observed": 2,`, `"active_readers_observed": 1,`, 1)

	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(report), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "| process-pinned-local | 8 | 2 | 0+1/1 |") {
		t.Fatalf("stdout missing reader-process count:\n%s", stdout.String())
	}
}

func TestRunAcceptsMultipleMmapTxWorkloadReports(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	if err := os.WriteFile(first, []byte(sampleTxWorkloadReport), 0o644); err != nil {
		t.Fatalf("write first report: %v", err)
	}
	secondReport := strings.ReplaceAll(sampleTxWorkloadReport, `"transactions": 8`, `"transactions": 6`)
	secondReport = strings.ReplaceAll(secondReport, `"committed": 4`, `"committed": 3`)
	secondReport = strings.ReplaceAll(secondReport, `"conflicted": 4`, `"conflicted": 3`)
	if err := os.WriteFile(second, []byte(secondReport), 0o644); err != nil {
		t.Fatalf("write second report: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{first, second}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	output := stdout.String()
	if strings.Count(output, "| mmap-tx-conflict |") != 2 {
		t.Fatalf("report row count = %d, want 2:\n%s", strings.Count(output, "| mmap-tx-conflict |"), output)
	}
	if !strings.Contains(output, "| second.json | 6 | 2 | 2/2 | 3 | 3 |") {
		t.Fatalf("stdout missing second report:\n%s", output)
	}
}

func TestRunRejectsInvalidMmapTxWorkloadJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(`{"transactions":`), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run exit = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "decode") {
		t.Fatalf("stderr = %q, want decode error", stderr.String())
	}
}
