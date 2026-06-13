package pagebtree

import "slices"

func searchPage(pages map[PageID]*page, root PageID, key string) ([]byte, bool) {
	for root != 0 {
		p := pages[root]
		index, found := slices.BinarySearch(p.keys, key)
		if found {
			return cloneBytes(p.values[index]), true
		}
		if p.leaf {
			return nil, false
		}
		root = p.children[index]
	}
	return nil, false
}

func rangePage(pages map[PageID]*page, root PageID, visit func(string, []byte) bool) bool {
	if root == 0 {
		return true
	}

	p := pages[root]
	for i, key := range p.keys {
		if !p.leaf && !rangePage(pages, p.children[i], visit) {
			return false
		}
		if !visit(key, cloneBytes(p.values[i])) {
			return false
		}
	}

	if !p.leaf {
		return rangePage(pages, p.children[len(p.keys)], visit)
	}
	return true
}
