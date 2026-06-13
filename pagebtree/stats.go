package pagebtree

type Stats struct {
	Root                    PageID
	Len                     int
	Revision                uint64
	Degree                  int
	Height                  int
	Pages                   int
	Keys                    int
	Separators              int
	AllocatedPages          int
	RetiredPages            int
	FreePages               int
	ActiveReaders           int
	ReusedPages             int
	Storage                 string
	PageCacheEntries        int
	PageCacheCapacity       int
	PageCacheHits           int
	PageCacheMisses         int
	PageCacheInvalidations  int
	PageCacheEvictions      int
	RangePrefetchLeafWindow int
	RangePrefetchHints      int
	RangePrefetchPages      int
	MmapWarmupHints         int
	MmapWarmupPages         int
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
	cacheStats := t.pageCache.snapshot()
	stats.PageCacheEntries = cacheStats.Entries
	stats.PageCacheCapacity = cacheStats.Capacity
	stats.PageCacheHits = cacheStats.Hits
	stats.PageCacheMisses = cacheStats.Misses
	stats.PageCacheInvalidations = cacheStats.Invalidations
	stats.PageCacheEvictions = cacheStats.Evictions
	stats.RangePrefetchLeafWindow = t.rangePrefetchLeafWindow
	stats.RangePrefetchHints = t.rangePrefetchHints
	stats.RangePrefetchPages = t.rangePrefetchPages
	stats.MmapWarmupHints = t.mmapWarmupHints
	stats.MmapWarmupPages = t.mmapWarmupPages
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
		for _, entry := range p.leafEntries() {
			overflowPages := countOverflowPages(pages, entry.value, entry.slotFlags, seen)
			pageCount += overflowPages
		}
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

func countOverflowPages(pages map[PageID]*page, raw []byte, flags uint16, seen map[PageID]bool) int {
	ref, ok := decodeOverflowRef(raw, flags)
	if !ok {
		return 0
	}
	count := 0
	for id := ref.first; id != 0 && !seen[id]; {
		seen[id] = true
		count++
		id = pages[id].overflowNext()
	}
	return count
}
