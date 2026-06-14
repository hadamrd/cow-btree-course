package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPrintsTransactionWorkloadJSONAndTrace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "txworkload.db")
	tracePath := filepath.Join(dir, "txworkload.jsonl")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--transactions", "6", "--trace", tracePath, path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var report txWorkloadReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.Path != path || report.TracePath != tracePath {
		t.Fatalf("report paths = %q/%q, want %q/%q", report.Path, report.TracePath, path, tracePath)
	}
	if report.Transactions != 6 {
		t.Fatalf("Transactions = %d, want 6", report.Transactions)
	}
	if report.Committed != 3 || report.Conflicted != 3 {
		t.Fatalf("Committed/Conflicted = %d/%d, want 3/3", report.Committed, report.Conflicted)
	}
	if report.ReopenedCommittedKeys != 3 {
		t.Fatalf("ReopenedCommittedKeys = %d, want 3", report.ReopenedCommittedKeys)
	}
	if report.ReopenedConflictedKeys != 0 {
		t.Fatalf("ReopenedConflictedKeys = %d, want 0", report.ReopenedConflictedKeys)
	}
	if report.FinalStats.SyncedRevision != report.FinalStats.Revision {
		t.Fatalf("final SyncedRevision/Revision = %d/%d, want equal", report.FinalStats.SyncedRevision, report.FinalStats.Revision)
	}

	trace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	for _, want := range []string{
		`"kind":"mmap-tx-conflict"`,
		`"kind":"mmap-sync-end"`,
	} {
		if !bytes.Contains(trace, []byte(want)) {
			t.Fatalf("trace missing %s in:\n%s", want, string(trace))
		}
	}
	if bytes.Contains(trace, []byte("tx-key-")) || bytes.Contains(trace, []byte("outside-")) {
		t.Fatalf("trace leaked workload keys: %s", string(trace))
	}
}

func TestRunCanRedactReportPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "txworkload.db")
	tracePath := filepath.Join(dir, "txworkload.jsonl")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--transactions", "2", "--trace", tracePath, "--redact-path", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report txWorkloadReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.Path != "" || report.TracePath != "" {
		t.Fatalf("redacted report paths = %q/%q, want empty", report.Path, report.TracePath)
	}
	if !report.PathRedacted || !report.TracePathRedacted {
		t.Fatalf("redaction flags = path:%v trace:%v, want true/true", report.PathRedacted, report.TracePathRedacted)
	}
	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw JSON: %v", err)
	}
	if _, ok := raw["path"]; ok {
		t.Fatalf("redacted JSON includes path field: %s", stdout.String())
	}
	if _, ok := raw["trace_path"]; ok {
		t.Fatalf("redacted JSON includes trace_path field: %s", stdout.String())
	}
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("trace file was not written at real path: %v", err)
	}
}

func TestRunCanLabelShareableReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "txworkload.db")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--transactions", "2", "--label", "reader-pinned-local", "--redact-path", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report txWorkloadReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.Label != "reader-pinned-local" {
		t.Fatalf("Label = %q, want reader-pinned-local", report.Label)
	}
	if report.Path != "" || !report.PathRedacted {
		t.Fatalf("path redaction = %q/%v, want redacted path", report.Path, report.PathRedacted)
	}
}

func TestRunCanMixCommittedDeletesIntoWorkload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "txworkload.db")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--transactions", "8", "--delete-every", "2", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report txWorkloadReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.Transactions != 8 || report.DeleteEvery != 2 {
		t.Fatalf("Transactions/DeleteEvery = %d/%d, want 8/2", report.Transactions, report.DeleteEvery)
	}
	if report.Committed != 4 || report.Conflicted != 4 {
		t.Fatalf("Committed/Conflicted = %d/%d, want 4/4", report.Committed, report.Conflicted)
	}
	if report.DeletedCommittedKeys != 2 {
		t.Fatalf("DeletedCommittedKeys = %d, want 2", report.DeletedCommittedKeys)
	}
	if report.ReopenedCommittedKeys != 2 {
		t.Fatalf("ReopenedCommittedKeys = %d, want 2 surviving keys", report.ReopenedCommittedKeys)
	}
	if report.ReopenedDeletedKeys != 0 {
		t.Fatalf("ReopenedDeletedKeys = %d, want 0", report.ReopenedDeletedKeys)
	}
	if report.ReopenedConflictedKeys != 0 {
		t.Fatalf("ReopenedConflictedKeys = %d, want 0", report.ReopenedConflictedKeys)
	}
	if report.ReopenedOutsideKeys != 4 {
		t.Fatalf("ReopenedOutsideKeys = %d, want 4", report.ReopenedOutsideKeys)
	}
	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw JSON: %v", err)
	}
	if _, ok := raw["reopened_deleted_keys"]; !ok {
		t.Fatalf("report omitted explicit reopened_deleted_keys zero: %s", stdout.String())
	}
}

func TestRunCanReportReaderPinnedTransactionWorkload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "txworkload.db")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--transactions", "8", "--delete-every", "2", "--readers", "2", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}

	var report txWorkloadReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if report.Readers != 2 || report.ActiveReadersObserved != 2 {
		t.Fatalf("Readers/ActiveReadersObserved = %d/%d, want 2/2", report.Readers, report.ActiveReadersObserved)
	}
	if report.ReaderPinnedRetiredPages == 0 {
		t.Fatalf("ReaderPinnedRetiredPages = 0, want retired pages blocked by readers")
	}
	if !report.ReaderPinnedReclaimPressure.BlockedByReaders {
		t.Fatalf("ReaderPinnedReclaimPressure = %+v, want blocked-by-readers", report.ReaderPinnedReclaimPressure)
	}
	if report.ReaderPinnedReclaimPressure.ReaderPinnedRetiredPages != report.ReaderPinnedRetiredPages {
		t.Fatalf("ReaderPinnedReclaimPressure.ReaderPinnedRetiredPages = %d, want %d", report.ReaderPinnedReclaimPressure.ReaderPinnedRetiredPages, report.ReaderPinnedRetiredPages)
	}
	if report.FinalStats.RetiredPages != 0 {
		t.Fatalf("FinalStats.RetiredPages = %d, want reclaimed after readers close", report.FinalStats.RetiredPages)
	}
	if report.FinalStats.FreePages == 0 {
		t.Fatalf("FinalStats.FreePages = 0, want reusable pages after readers close")
	}
}

func TestRunRejectsExistingDatabaseArtifacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "txworkload.db")
	if err := os.WriteFile(path, []byte("already here"), 0o644); err != nil {
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
	if !strings.Contains(stderr.String(), "refusing existing") {
		t.Fatalf("stderr = %q, want existing-artifact refusal", stderr.String())
	}
}

func TestRunRejectsInvalidArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--transactions", "0", "db"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--transactions") {
		t.Fatalf("stderr = %q, want transactions validation", stderr.String())
	}
}

func TestRunRejectsWhitespacePaddedLabel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--label", " reader-pinned-local ", "db"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--label") {
		t.Fatalf("stderr = %q, want label validation", stderr.String())
	}
}
