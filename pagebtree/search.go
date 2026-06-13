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

func rangePageFrom(pages map[PageID]*page, root PageID, start string, visit func(string, []byte) bool) bool {
	if root == 0 {
		return true
	}

	p := pages[root]
	if p.isLeaf() {
		entries := p.leafEntries()
		index, _ := slices.BinarySearchFunc(entries, start, func(entry leafEntry, key string) int {
			return compareStrings(entry.key, key)
		})
		for _, entry := range entries[index:] {
			if !visit(entry.key, resolveLeafValue(pages, entry.value, entry.slotFlags)) {
				return false
			}
		}
		return true
	}

	keys, children := p.branchParts()
	index := childIndex(keys, start)
	for ; index < len(children); index++ {
		if !rangePageFrom(pages, children[index], start, visit) {
			return false
		}
	}
	return true
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

func rangeLinkedLeavesFrom(pages map[PageID]*page, root PageID, start string, prefetch func(PageID), visit func(string, []byte) bool) bool {
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
		prefetchNextLeaves(pages, p.nextLeaf(), rangePrefetchLeafWindow, prefetch)

		entries := p.leafEntries()
		index := 0
		if firstLeaf {
			index, _ = slices.BinarySearchFunc(entries, start, func(entry leafEntry, key string) int {
				return compareStrings(entry.key, key)
			})
			firstLeaf = false
		}
		for _, entry := range entries[index:] {
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
