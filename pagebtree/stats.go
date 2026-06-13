package pagebtree

type Stats struct {
	Root           PageID
	Len            int
	Revision       uint64
	Degree         int
	Height         int
	Pages          int
	Keys           int
	AllocatedPages int
}

func statsFor(pages map[PageID]*page, root PageID, length int, revision uint64, degree int) Stats {
	pagesCount, keys := countPagesAndKeys(pages, root, map[PageID]bool{})
	return Stats{
		Root:           root,
		Len:            length,
		Revision:       revision,
		Degree:         degree,
		Height:         height(pages, root),
		Pages:          pagesCount,
		Keys:           keys,
		AllocatedPages: len(pages),
	}
}

func height(pages map[PageID]*page, root PageID) int {
	if root == 0 {
		return 0
	}
	p := pages[root]
	if p.leaf {
		return 1
	}
	return 1 + height(pages, p.children[0])
}

func countPagesAndKeys(pages map[PageID]*page, root PageID, seen map[PageID]bool) (int, int) {
	if root == 0 || seen[root] {
		return 0, 0
	}
	seen[root] = true

	p := pages[root]
	pageCount := 1
	keyCount := len(p.keys)
	for _, child := range p.children {
		childPages, childKeys := countPagesAndKeys(pages, child, seen)
		pageCount += childPages
		keyCount += childKeys
	}
	return pageCount, keyCount
}
