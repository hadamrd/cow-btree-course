package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	path := filepath.Join(os.TempDir(), "cow-btree-mmap-trace.db")
	if len(args) > 1 {
		fmt.Fprintf(stderr, "usage: mmaptrace-demo [DB.db]\n")
		return 2
	}
	if len(args) == 1 {
		path = args[0]
	}
	_ = os.Remove(path)
	_ = os.Remove(path + ".readers")
	_ = os.Remove(path + ".writer")

	exporter := pagebtree.NewMmapTraceJSONLExporter(stdout)
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{
		Degree:    2,
		MaxPages:  8,
		TraceHook: exporter.Hook(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "mmap trace demo: %v\n", err)
		return 1
	}

	for i := 0; i < 120; i++ {
		tree.Put(fmt.Sprintf("trace-key-%02d", i), []byte(fmt.Sprintf("trace-value-%02d", i)))
	}
	if err := tree.Sync(); err != nil {
		_ = tree.Close()
		fmt.Fprintf(stderr, "mmap trace demo: sync: %v\n", err)
		return 1
	}
	for i := 0; i < 110; i++ {
		tree.Delete(fmt.Sprintf("trace-key-%02d", i))
	}
	if err := tree.Sync(); err != nil {
		_ = tree.Close()
		fmt.Fprintf(stderr, "mmap trace demo: delete sync: %v\n", err)
		return 1
	}
	if err := tree.Compact(); err != nil {
		_ = tree.Close()
		fmt.Fprintf(stderr, "mmap trace demo: compact: %v\n", err)
		return 1
	}
	if err := tree.Close(); err != nil {
		fmt.Fprintf(stderr, "mmap trace demo: close: %v\n", err)
		return 1
	}
	if err := exporter.Err(); err != nil {
		fmt.Fprintf(stderr, "mmap trace demo: export: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "wrote mmap workload trace JSONL for %s\n", path)
	return 0
}
