package pagebtree

import "slices"

const DefaultRangePrefetchLeafWindow = 2

type pageRange struct {
	start PageID
	end   PageID
}

func searchPage(pages map[PageID]*page, root PageID, key string, compare func(string, string) int) ([]byte, bool) {
	return searchPageWithCache(pages, root, key, compare, nil)
}

func searchPageWithCache(pages map[PageID]*page, root PageID, key string, compare func(string, string) int, cache *pageCache) ([]byte, bool) {
	for root != 0 {
		p := pages[root]
		if p.isLeaf() {
			raw, flags, found := p.searchLeafCellWithComparator(key, compare)
			if !found {
				return nil, false
			}
			return resolveLeafValue(pages, raw, flags), true
		}

		if cache != nil {
			root = cache.searchBranchChild(p, key, compare)
		} else {
			root = p.searchBranchChildWithComparator(key, compare)
		}
	}
	return nil, false
}

func rangePage(pages map[PageID]*page, root PageID, compare func(string, string) int, visit func(string, []byte) bool) bool {
	if root == 0 {
		return true
	}

	p := pages[root]
	if p.isLeaf() {
		return rangeLeafSlots(pages, p, 0, "", false, compare, visit)
	}

	for i := 0; i <= int(p.slotCount()); i++ {
		if !rangePage(pages, p.branchChild(i), compare, visit) {
			return false
		}
	}
	return true
}

func rangePageFrom(pages map[PageID]*page, root PageID, start string, compare func(string, string) int, visit func(string, []byte) bool) bool {
	if root == 0 {
		return true
	}

	p := pages[root]
	if p.isLeaf() {
		index, _ := p.searchSlotWithComparator(start, compare)
		return rangeLeafSlots(pages, p, index, "", false, compare, visit)
	}

	index := branchChildIndex(p, start, compare)
	for ; index <= int(p.slotCount()); index++ {
		if !rangePageFrom(pages, p.branchChild(index), start, compare, visit) {
			return false
		}
	}
	return true
}

func rangePageBetween(pages map[PageID]*page, root PageID, start, end string, compare func(string, string) int, visit func(string, []byte) bool) bool {
	if root == 0 || compare(start, end) >= 0 {
		return true
	}

	p := pages[root]
	if p.isLeaf() {
		index, _ := p.searchSlotWithComparator(start, compare)
		return rangeLeafSlots(pages, p, index, end, true, compare, visit)
	}

	index := branchChildIndex(p, start, compare)
	for ; index <= int(p.slotCount()); index++ {
		if branchChildLowerBoundAtOrAfter(p, index, end, compare) {
			return true
		}
		if !rangePageBetween(pages, p.branchChild(index), start, end, compare, visit) {
			return false
		}
	}
	return true
}

func rangeLinkedLeaves(pages map[PageID]*page, root PageID, compare func(string, string) int, prefetchWindow int, prefetch func(PageID, PageID), visit func(string, []byte) bool) bool {
	leaf := leftmostLeaf(pages, root)
	seen := map[PageID]bool{}
	for leaf != 0 {
		if seen[leaf] {
			return true
		}
		seen[leaf] = true

		p := pages[leaf]
		if p == nil || !p.isLeaf() {
			return false
		}
		prefetchNextLeafRanges(pages, p.nextLeaf(), prefetchWindow, prefetch)
		if !rangeLeafSlots(pages, p, 0, "", false, compare, visit) {
			return true
		}
		leaf = p.nextLeaf()
	}
	return true
}

func rangeLinkedLeavesFrom(pages map[PageID]*page, root PageID, start string, compare func(string, string) int, prefetchWindow int, prefetch func(PageID, PageID), visit func(string, []byte) bool) bool {
	leaf := leafForKey(pages, root, start, compare)
	seen := map[PageID]bool{}
	firstLeaf := true
	for leaf != 0 {
		if seen[leaf] {
			return true
		}
		seen[leaf] = true

		p := pages[leaf]
		if p == nil || !p.isLeaf() {
			return false
		}
		prefetchNextLeafRanges(pages, p.nextLeaf(), prefetchWindow, prefetch)

		index := 0
		if firstLeaf {
			index, _ = p.searchSlotWithComparator(start, compare)
			firstLeaf = false
		}
		if !rangeLeafSlots(pages, p, index, "", false, compare, visit) {
			return true
		}
		leaf = p.nextLeaf()
	}
	return true
}

func rangeLinkedLeavesBetween(pages map[PageID]*page, root PageID, start, end string, compare func(string, string) int, prefetchWindow int, prefetch func(PageID, PageID), visit func(string, []byte) bool) bool {
	if compare(start, end) >= 0 {
		return true
	}
	leaf := leafForKey(pages, root, start, compare)
	seen := map[PageID]bool{}
	firstLeaf := true
	for leaf != 0 {
		if seen[leaf] {
			return true
		}
		seen[leaf] = true

		p := pages[leaf]
		if p == nil || !p.isLeaf() {
			return false
		}
		if first, ok := firstLeafKey(p); ok && compare(first, end) >= 0 {
			return true
		}
		prefetchNextLeafRangesBefore(pages, p.nextLeaf(), prefetchWindow, end, compare, prefetch)

		index := 0
		if firstLeaf {
			index, _ = p.searchSlotWithComparator(start, compare)
			firstLeaf = false
		}
		if !rangeLeafSlots(pages, p, index, end, true, compare, visit) {
			return true
		}
		leaf = p.nextLeaf()
	}
	return true
}

func rangeLeafSlots(pages map[PageID]*page, p *page, startIndex int, end string, hasEnd bool, compare func(string, string) int, visit func(string, []byte) bool) bool {
	for i := startIndex; i < int(p.slotCount()); i++ {
		if hasEnd && p.compareCellKeyWithComparator(i, end, compare) >= 0 {
			return true
		}
		slot := p.readSlot(i)
		key := p.readCellKey(i)
		value := p.readCellValue(i)
		if !visit(key, resolveLeafValue(pages, value, slot.flags)) {
			return false
		}
	}
	return true
}

func prefetchNextLeafRanges(pages map[PageID]*page, next PageID, window int, prefetch func(PageID, PageID)) {
	if prefetch == nil || window <= 0 {
		return
	}
	seen := map[PageID]bool{}
	var current pageRange
	flush := func() {
		if current.start != 0 {
			prefetch(current.start, current.end)
			current = pageRange{}
		}
	}
	for i := 0; i < window && next != 0; i++ {
		if seen[next] {
			break
		}
		seen[next] = true

		p := pages[next]
		if p == nil || !p.isLeaf() {
			break
		}
		if current.start == 0 {
			current = pageRange{start: next, end: next + 1}
		} else if next == current.end {
			current.end = next + 1
		} else {
			flush()
			current = pageRange{start: next, end: next + 1}
		}
		next = p.nextLeaf()
	}
	flush()
}

func prefetchNextLeafRangesBefore(pages map[PageID]*page, next PageID, window int, end string, compare func(string, string) int, prefetch func(PageID, PageID)) {
	if prefetch == nil || window <= 0 {
		return
	}
	seen := map[PageID]bool{}
	var current pageRange
	flush := func() {
		if current.start != 0 {
			prefetch(current.start, current.end)
			current = pageRange{}
		}
	}
	for i := 0; i < window && next != 0; i++ {
		if seen[next] {
			break
		}
		seen[next] = true

		p := pages[next]
		if p == nil || !p.isLeaf() {
			break
		}
		first, ok := firstLeafKey(p)
		if !ok || compare(first, end) >= 0 {
			break
		}
		if current.start == 0 {
			current = pageRange{start: next, end: next + 1}
		} else if next == current.end {
			current.end = next + 1
		} else {
			flush()
			current = pageRange{start: next, end: next + 1}
		}
		next = p.nextLeaf()
	}
	flush()
}

func leftmostLeaf(pages map[PageID]*page, root PageID) PageID {
	for root != 0 {
		p := pages[root]
		if p == nil {
			return 0
		}
		if p.isLeaf() {
			return root
		}
		root = p.leftmostChild()
	}
	return 0
}

func leafForKey(pages map[PageID]*page, root PageID, key string, compare func(string, string) int) PageID {
	for root != 0 {
		p := pages[root]
		if p == nil {
			return 0
		}
		if p.isLeaf() {
			return root
		}
		root = p.searchBranchChildWithComparator(key, compare)
	}
	return 0
}

func firstLeafKey(p *page) (string, bool) {
	if p == nil || !p.isLeaf() || p.slotCount() == 0 {
		return "", false
	}
	return p.readCellKey(0), true
}

func childIndex(keys []string, key string, compare func(string, string) int) int {
	index, found := slices.BinarySearchFunc(keys, key, compare)
	if found {
		return index + 1
	}
	return index
}

func branchChildIndex(p *page, key string, compare func(string, string) int) int {
	index, found := p.searchSlotWithComparator(key, compare)
	if found {
		return index + 1
	}
	return index
}

func branchChildLowerBoundAtOrAfter(p *page, childIndex int, key string, compare func(string, string) int) bool {
	if childIndex == 0 {
		return false
	}
	return p.compareCellKeyWithComparator(childIndex-1, key, compare) >= 0
}

func compareStrings(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
