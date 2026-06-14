package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleBenchOutput = `goos: darwin
goarch: arm64
pkg: github.com/hadamrd/cow-btree-course/pagebtree
cpu: Apple M3
BenchmarkPageTreeGet-10                 1000       123.4 ns/op        32 B/op         1 allocs/op
BenchmarkMmapTreeRangeBetween-10         500      4567 ns/op       128.0 keys/op      64 B/op         2 allocs/op
PASS
ok  	github.com/hadamrd/cow-btree-course/pagebtree	1.234s
`

func TestRunSummarizesBenchmarkFileAsMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bench.out")
	if err := os.WriteFile(path, []byte(sampleBenchOutput), 0o644); err != nil {
		t.Fatalf("write benchmark file: %v", err)
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
		"| Benchmark | Iterations | ns/op | B/op | allocs/op | keys/op |",
		"| PageTreeGet | 1000 | 123.4 | 32 | 1 |  |",
		"| MmapTreeRangeBetween | 500 | 4567 | 64 | 2 | 128.0 |",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestRunReadsBenchmarkOutputFromStdin(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(sampleBenchOutput), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "| PageTreeGet |") {
		t.Fatalf("stdout missing parsed benchmark: %q", stdout.String())
	}
}

func TestRunRejectsBenchmarkOutputWithoutRows(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader("PASS\n"), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run exit = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no benchmark rows") {
		t.Fatalf("stderr = %q, want no benchmark rows error", stderr.String())
	}
}

func TestRunRejectsTooManyArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"one.out", "two.out"}, nil, &stdout, &stderr)
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
