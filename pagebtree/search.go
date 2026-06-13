package pagebtree

import "slices"

const rangePrefetchLeafWindow = 2

func searchPage(pages map[PageID]*page, root PageID, key string) ([]byte, bool) {
	for root != 0 {
		p := pages[root]
		if p.isLeaf() {
			raw, flags, found := p.searchLeafCell(key)
			if !found {
				return nil, false
			}
			return resolveLeafValue(pages, raw, flags), true
		}

		root = p.searchBranchChild(key)
	}
	return nil, false
}

func rangePage(pages map[PageID]*page, root PageID, visit func(string, []byte) bool) bool {
	if root == 0 {
		return true
	}

	p := pages[root]
	if p.isLeaf() {
		for _, entry := range p.leafEntries() {
			if !visit(entry.key, resolveLeafValue(pages, entry.value, entry.slotFlags)) {
				return false
			}
		}
		return true
	}

	keys, children := p.branchParts()
	for i, key := range keys {
		if !rangePage(pages, children[i], visit) {
			return false
		}
		_ = key
	}

	return rangePage(pages, children[len(children)-1], visit)
}

func rangeLinkedLeaves(pages map[PageID]*page, root PageID, prefetch func(PageID), visit func(string, []byte) bool) bool {
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
		prefetchNextLeaves(pages, p.nextLeaf(), rangePrefetchLeafWindow, prefetch)
		for _, entry := range p.leafEntries() {
			if !visit(entry.key, resolveLeafValue(pages, entry.value, entry.slotFlags)) {
				return true
			}
		}
		leaf = p.nextLeaf()
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

func childIndex(keys []string, key string) int {
	index, found := slices.BinarySearch(keys, key)
	if found {
		return index + 1
	}
	return index
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
