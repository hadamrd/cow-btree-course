package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleProbeReport = `{
  "path": "/tmp/probe-apfs.db",
  "keys_inserted": 256,
  "keys_deleted": 128,
  "value_bytes": 512,
  "platform": {
    "GOOS": "darwin",
    "MmapSupported": true
  },
  "after_insert": {
    "stats": {
      "Len": 256,
      "FreePages": 0,
      "RetiredPages": 0
    },
    "space": {
      "LogicalFileBytes": 1048576,
      "AllocatedBytes": 917504,
      "SparseBytes": 131072,
      "FilesystemType": "apfs",
      "FilesystemTypeID": 17,
      "MountPath": "/System/Volumes/Data",
      "MountSource": "/dev/disk3s5",
      "MountOptions": "local,journaled"
    }
  },
  "after_delete": {
    "stats": {
      "Len": 128,
      "FreePages": 12,
      "RetiredPages": 0
    },
    "space": {
      "LogicalFileBytes": 1048576,
      "AllocatedBytes": 917504,
      "SparseBytes": 131072,
      "FilesystemType": "apfs",
      "FilesystemTypeID": 17,
      "MountPath": "/System/Volumes/Data",
      "MountSource": "/dev/disk3s5",
      "MountOptions": "local,journaled"
    }
  },
  "after_compact": {
    "stats": {
      "Len": 128,
      "FreePages": 4,
      "RetiredPages": 0
    },
    "space": {
      "LogicalFileBytes": 786432,
      "AllocatedBytes": 720896,
      "SparseBytes": 65536,
      "FilesystemType": "apfs",
      "FilesystemTypeID": 17,
      "MountPath": "/System/Volumes/Data",
      "MountSource": "/dev/disk3s5",
      "MountOptions": "local,journaled"
    }
  },
  "after_punch": {
    "stats": {
      "Len": 128,
      "FreePages": 4,
      "RetiredPages": 0
    },
    "space": {
      "LogicalFileBytes": 786432,
      "AllocatedBytes": 704512,
      "SparseBytes": 81920,
      "FilesystemType": "apfs",
      "FilesystemTypeID": 17,
      "MountPath": "/System/Volumes/Data",
      "MountSource": "/dev/disk3s5",
      "MountOptions": "local,journaled"
    }
  },
  "punch_stats": {
    "FreePages": 4,
    "SkippedRecoverablePages": 1,
    "Ranges": 2,
    "PunchedPages": 3,
    "PunchedBytes": 12288
  },
  "punch_error": "mmap sparse hole punching is unsupported"
}`

func TestRunSummarizesFilesystemProbeFileAsMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "probe.json")
	if err := os.WriteFile(path, []byte(sampleProbeReport), 0o644); err != nil {
		t.Fatalf("write probe file: %v", err)
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
		"| Report | GOOS | FS | Mount | Mount options | Keys | Value bytes | Phase | Len | Free pages | Retired pages | Logical bytes | Allocated bytes | Sparse bytes | Punched pages | Punch error |",
		"| probe-apfs.db | darwin | apfs (17) | /System/Volumes/Data [/dev/disk3s5] | local,journaled | 256/128 | 512 | after_insert | 256 | 0 | 0 | 1048576 | 917504 | 131072 |  |  |",
		"| probe-apfs.db | darwin | apfs (17) | /System/Volumes/Data [/dev/disk3s5] | local,journaled | 256/128 | 512 | after_punch | 128 | 4 | 0 | 786432 | 704512 | 81920 | 3 | mmap sparse hole punching is unsupported |",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestRunReadsFilesystemProbeReportFromStdin(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(sampleProbeReport), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "| probe-apfs.db |") {
		t.Fatalf("stdout missing parsed probe report: %q", stdout.String())
	}
}

func TestRunAcceptsMultipleFilesystemProbeFiles(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	if err := os.WriteFile(first, []byte(sampleProbeReport), 0o644); err != nil {
		t.Fatalf("write first probe file: %v", err)
	}
	secondReport := strings.ReplaceAll(sampleProbeReport, "/tmp/probe-apfs.db", "/tmp/probe-ext4.db")
	secondReport = strings.ReplaceAll(secondReport, `"GOOS": "darwin"`, `"GOOS": "linux"`)
	secondReport = strings.ReplaceAll(secondReport, `"FilesystemType": "apfs"`, `"FilesystemType": "ext4"`)
	if err := os.WriteFile(second, []byte(secondReport), 0o644); err != nil {
		t.Fatalf("write second probe file: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{first, second}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	output := stdout.String()
	if strings.Count(output, "| after_insert |") != 2 {
		t.Fatalf("after_insert row count = %d, want 2:\n%s", strings.Count(output, "| after_insert |"), output)
	}
	if !strings.Contains(output, "| probe-ext4.db | linux | ext4 (17) | /System/Volumes/Data [/dev/disk3s5] | local,journaled |") {
		t.Fatalf("stdout missing second report:\n%s", output)
	}
}

func TestRunRejectsInvalidFilesystemProbeJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(`{"path":`), &stdout, &stderr)
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
