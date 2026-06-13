package pagebtree

type Stats struct {
	Root           PageID
	Len            int
	Revision       uint64
	Degree         int
	Height         int
	Pages          int
	Keys           int
	Separators     int
	AllocatedPages int
	RetiredPages   int
	FreePages      int
	ActiveReaders  int
	ReusedPages    int
	Storage        string
}

func statsFor(t *Tree) Stats {
	stats := statsForSnapshot(t.pages, t.root, t.length, t.revision, t.degree)
	stats.AllocatedPages = len(t.pages)
	stats.RetiredPages = len(t.retired)
	stats.FreePages = len(t.free)
	stats.ActiveReaders = t.activeReaderCount()
	stats.ReusedPages = t.reusedPages
	stats.Storage = "memory"
	if t.arena != nil {
		stats.Storage = "mmap"
	}
	return stats
}

func statsForSnapshot(pages map[PageID]*page, root PageID, length int, revision uint64, degree int) Stats {
	pagesCount, keys, separators := countPagesAndKeys(pages, root, map[PageID]bool{})
	return Stats{
		Root:           root,
		Len:            length,
		Revision:       revision,
		Degree:         degree,
		Height:         height(pages, root),
		Pages:          pagesCount,
		Keys:           keys,
		Separators:     separators,
		AllocatedPages: len(pages),
	}
}

func height(pages map[PageID]*page, root PageID) int {
	if root == 0 {
		return 0
	}
	p := pages[root]
	if p.isLeaf() {
		return 1
	}
	return 1 + height(pages, p.leftmostChild())
}

func countPagesAndKeys(pages map[PageID]*page, root PageID, seen map[PageID]bool) (int, int, int) {
	if root == 0 || seen[root] {
		return 0, 0, 0
	}
	seen[root] = true

	p := pages[root]
	pageCount := 1
	keyCount := 0
	separatorCount := 0
	if p.isLeaf() {
		keyCount = int(p.slotCount())
	} else {
		separatorCount = int(p.slotCount())
	}
	for _, child := range p.childIDs() {
		childPages, childKeys, childSeparators := countPagesAndKeys(pages, child, seen)
		pageCount += childPages
		keyCount += childKeys
		separatorCount += childSeparators
	}
	return pageCount, keyCount, separatorCount
}
