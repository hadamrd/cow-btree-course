package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

type platformReport struct {
	Profile pagebtree.MmapPlatformCapability `json:"profile"`
}

func buildReport() platformReport {
	return platformReport{
		Profile: pagebtree.MmapPlatformProfile(),
	}
}

func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(buildReport()); err != nil {
		fmt.Fprintf(os.Stderr, "mmapplatform: %v\n", err)
		os.Exit(1)
	}
}
