package pagebtree

import "slices"

func searchPage(pages map[PageID]*page, root PageID, key string) ([]byte, bool) {
	for root != 0 {
		p := pages[root]
		if p.isLeaf() {
			entries := p.leafEntries()
			index, found := slices.BinarySearchFunc(entries, key, func(entry leafEntry, key string) int {
				return compareStrings(entry.key, key)
			})
			if found {
				return cloneBytes(entries[index].value), true
			}
			return nil, false
		}

		keys, children := p.branchParts()
		root = children[childIndex(keys, key)]
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
			if !visit(entry.key, cloneBytes(entry.value)) {
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
