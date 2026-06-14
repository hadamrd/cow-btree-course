package main

import (
	"fmt"
	"os"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s SRC.db DST.db\n", os.Args[0])
		os.Exit(2)
	}

	result, err := pagebtree.CopyCompactMmap(os.Args[1], os.Args[2], pagebtree.MmapOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "mmap copy compact: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("copied %d keys\n", result.Keys)
	fmt.Printf("source:      %d bytes, %d mapped data pages\n", result.SourceFileBytes, result.SourceAllocatedPages)
	fmt.Printf("destination: %d bytes, %d mapped data pages\n", result.DestinationFileBytes, result.DestinationAllocatedPages)
}
