package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hadamrd/cow-btree-course/pagebtree"
)

func TestPrintProfileNamesKernelAndBytePolicyFields(t *testing.T) {
	var out bytes.Buffer
	printProfile(&out, pagebtree.MDBKernelProfile{
		Storage:                       "mmap",
		PageSize:                      pagebtree.PageSize,
		MaxMappedPages:                64,
		SlottedPages:                  true,
		CopyOnWrite:                   true,
		ByteAwareSplitPoints:          true,
		ByteAwareDeleteRedistribution: true,
		ByteFitDeleteMerges:           true,
		ConfigurableRepairFill:        true,
		MinRepairPageFillPercent:      40,
		KernelPageCache:               true,
		RawHeapPageCache:              false,
		DerivedBranchRoutingCache:     true,
		DerivedBranchRoutingCacheHits: 3,
	})

	text := out.String()
	for _, want := range []string{
		"storage: mmap",
		"page size: 4096",
		"byte-aware split points: true",
		"byte-aware delete redistribution: true",
		"byte-fit delete merges: true",
		"min repair page fill: 40%",
		"kernel page cache: true",
		"raw heap page cache: false",
		"derived routing cache: true",
		"routing cache hits: 3",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("profile output missing %q:\n%s", want, text)
		}
	}
}
