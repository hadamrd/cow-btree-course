package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func main() {
	path := filepath.Join(os.TempDir(), "cow-btree-mmap-trace.db")
	_ = os.Remove(path)
	_ = os.Remove(path + ".readers")
	_ = os.Remove(path + ".writer")

	exporter := pagebtree.NewMmapTraceJSONLExporter(os.Stdout)
	tree, err := pagebtree.OpenMmap(path, pagebtree.MmapOptions{
		Degree:    2,
		MaxPages:  64,
		TraceHook: exporter.Hook(),
	})
	if err != nil {
		log.Fatal(err)
	}

	for i := 0; i < 24; i++ {
		tree.Put(fmt.Sprintf("trace-key-%02d", i), []byte(fmt.Sprintf("trace-value-%02d", i)))
	}
	if err := tree.Sync(); err != nil {
		_ = tree.Close()
		log.Fatal(err)
	}
	if err := tree.Close(); err != nil {
		log.Fatal(err)
	}
	if err := exporter.Err(); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "wrote mmap trace JSONL for %s\n", path)
}
