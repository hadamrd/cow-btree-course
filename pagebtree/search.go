package pagebtree

import "slices"

const DefaultRangePrefetchLeafWindow = 2

func searchPage(pages map[PageID]*page, root PageID, key string) ([]byte, bool) {
	return searchPageWithCache(pages, root, key, nil)
}

func searchPageWithCache(pages map[PageID]*page, root PageID, key string, cache *pageCache) ([]byte, bool) {
	for root != 0 {
		p := pages[root]
		if p.isLeaf() {
			raw, flags, found := p.searchLeafCell(key)
			if !found {
				return nil, false
			}
			return resolveLeafValue(pages, raw, flags), true
		}

		if cache != nil {
			root = cache.searchBranchChild(p, key)
		} else {
			root = p.searchBranchChild(key)
		}
	}
	return nil, false
}

func rangePage(pages map[PageID]*page, root PageID, visit func(string, []byte) bool) bool {
	if root == 0 {
		return true
	}

	p := pages[root]
	if p.isLeaf() {
		return rangeLeafSlots(pages, p, 0, "", false, visit)
	}

	for i := 0; i <= int(p.slotCount()); i++ {
		if !rangePage(pages, p.branchChild(i), visit) {
			return false
		}
	}
	return true
}

func rangePageFrom(pages map[PageID]*page, root PageID, start string, visit func(string, []byte) bool) bool {
	if root == 0 {
		return true
	}

	p := pages[root]
	if p.isLeaf() {
		index, _ := p.searchSlot(start)
		return rangeLeafSlots(pages, p, index, "", false, visit)
	}

	index := branchChildIndex(p, start)
	for ; index <= int(p.slotCount()); index++ {
		if !rangePageFrom(pages, p.branchChild(index), start, visit) {
			return false
		}
	}
	return true
}

func rangePageBetween(pages map[PageID]*page, root PageID, start, end string, visit func(string, []byte) bool) bool {
	if root == 0 || compareStrings(start, end) >= 0 {
		return true
	}

	p := pages[root]
	if p.isLeaf() {
		index, _ := p.searchSlot(start)
		return rangeLeafSlots(pages, p, index, end, true, visit)
	}

	index := branchChildIndex(p, start)
	for ; index <= int(p.slotCount()); index++ {
		if branchChildLowerBoundAtOrAfter(p, index, end) {
			return true
		}
		if !rangePageBetween(pages, p.branchChild(index), start, end, visit) {
			return false
		}
	}
	return true
}

func rangeLinkedLeaves(pages map[PageID]*page, root PageID, prefetchWindow int, prefetch func(PageID), visit func(string, []byte) bool) bool {
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
		prefetchNextLeaves(pages, p.nextLeaf(), prefetchWindow, prefetch)
		if !rangeLeafSlots(pages, p, 0, "", false, visit) {
			return true
		}
		leaf = p.nextLeaf()
	}
	return true
}

func rangeLinkedLeavesFrom(pages map[PageID]*page, root PageID, start string, prefetchWindow int, prefetch func(PageID), visit func(string, []byte) bool) bool {
	leaf := leafForKey(pages, root, start)
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
		prefetchNextLeaves(pages, p.nextLeaf(), prefetchWindow, prefetch)

		index := 0
		if firstLeaf {
			index, _ = p.searchSlot(start)
			firstLeaf = false
		}
		if !rangeLeafSlots(pages, p, index, "", false, visit) {
			return true
		}
		leaf = p.nextLeaf()
	}
	return true
}

func rangeLinkedLeavesBetween(pages map[PageID]*page, root PageID, start, end string, prefetchWindow int, prefetch func(PageID), visit func(string, []byte) bool) bool {
	if compareStrings(start, end) >= 0 {
		return true
	}
	leaf := leafForKey(pages, root, start)
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
		if first, ok := firstLeafKey(p); ok && compareStrings(first, end) >= 0 {
			return true
		}
		prefetchNextLeavesBefore(pages, p.nextLeaf(), prefetchWindow, end, prefetch)

		index := 0
		if firstLeaf {
			index, _ = p.searchSlot(start)
			firstLeaf = false
		}
		if !rangeLeafSlots(pages, p, index, end, true, visit) {
			return true
		}
		leaf = p.nextLeaf()
	}
	return true
}

func rangeLeafSlots(pages map[PageID]*page, p *page, startIndex int, end string, hasEnd bool, visit func(string, []byte) bool) bool {
	for i := startIndex; i < int(p.slotCount()); i++ {
		if hasEnd && p.compareCellKey(i, end) >= 0 {
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

func prefetchNextLeaves(pages map[PageID]*page, next PageID, window int, prefetch func(PageID)) {
	if prefetch == nil || window <= 0 {
		return
	}
	seen := map[PageID]bool{}
	for i := 0; i < window && next != 0; i++ {
		if seen[next] {
			return
		}
		seen[next] = true

		p := pages[next]
		if p == nil || !p.isLeaf() {
			return
		}
		prefetch(next)
		next = p.nextLeaf()
	}
}

func prefetchNextLeavesBefore(pages map[PageID]*page, next PageID, window int, end string, prefetch func(PageID)) {
	if prefetch == nil || window <= 0 {
		return
	}
	seen := map[PageID]bool{}
	for i := 0; i < window && next != 0; i++ {
		if seen[next] {
			return
		}
		seen[next] = true

		p := pages[next]
		if p == nil || !p.isLeaf() {
			return
		}
		first, ok := firstLeafKey(p)
		if !ok || compareStrings(first, end) >= 0 {
			return
		}
		prefetch(next)
		next = p.nextLeaf()
	}
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

func leafForKey(pages map[PageID]*page, root PageID, key string) PageID {
	for root != 0 {
		p := pages[root]
		if p == nil {
			return 0
		}
		if p.isLeaf() {
			return root
		}
		root = p.searchBranchChild(key)
	}
	return 0
}

func firstLeafKey(p *page) (string, bool) {
	if p == nil || !p.isLeaf() || p.slotCount() == 0 {
		return "", false
	}
	return p.readCellKey(0), true
}

func childIndex(keys []string, key string) int {
	index, found := slices.BinarySearch(keys, key)
	if found {
		return index + 1
	}
	return index
}

func branchChildIndex(p *page, key string) int {
	index, found := p.searchSlot(key)
	if found {
		return index + 1
	}
	return index
}

func branchChildLowerBoundAtOrAfter(p *page, childIndex int, key string) bool {
	if childIndex == 0 {
		return false
	}
	return p.compareCellKey(childIndex-1, key) >= 0
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
