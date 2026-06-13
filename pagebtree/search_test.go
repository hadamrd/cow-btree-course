package pagebtree

import (
	"reflect"
	"testing"
)

func TestPrefetchNextLeavesCoalescesAdjacentLeafPages(t *testing.T) {
	pages := map[PageID]*page{
		10: newPage(10, flagLeaf),
		11: newPage(11, flagLeaf),
		12: newPage(12, flagLeaf),
		14: newPage(14, flagLeaf),
	}
	pages[10].setNextLeaf(11)
	pages[11].setNextLeaf(12)
	pages[12].setNextLeaf(14)

	var ranges []pageRange
	prefetchNextLeafRanges(pages, 10, 4, func(start, end PageID) {
		ranges = append(ranges, pageRange{start: start, end: end})
	})

	want := []pageRange{
		{start: 10, end: 13},
		{start: 14, end: 15},
	}
	if !reflect.DeepEqual(ranges, want) {
		t.Fatalf("prefetch ranges = %+v, want %+v", ranges, want)
	}
}
