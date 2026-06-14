package main

import (
	"fmt"
	"os"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s DB.db\n", os.Args[0])
		os.Exit(2)
	}

	result, err := pagebtree.CompactMmapFile(os.Args[1], pagebtree.MmapOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "mmap compact: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("compacted %d keys\n", result.Keys)
	fmt.Printf("source:      %d bytes, %d mapped data pages\n", result.SourceFileBytes, result.SourceAllocatedPages)
	fmt.Printf("replacement: %d bytes, %d mapped data pages\n", result.DestinationFileBytes, result.DestinationAllocatedPages)
}
