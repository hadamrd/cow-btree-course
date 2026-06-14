package pagebtree

type Stats struct {
	Root                    PageID
	Len                     int
	Revision                uint64
	Degree                  int
	Height                  int
	Pages                   int
	LeafPages               int
	BranchPages             int
	OverflowPages           int
	Keys                    int
	Separators              int
	PageBytesUsed           int
	PageBytesFree           int
	PageBytesCapacity       int
	LeafBytesUsed           int
	BranchBytesUsed         int
	OverflowBytesUsed       int
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
	reachable := collectReachableStats(pages, root, map[PageID]bool{})
	return Stats{
		Root:              root,
		Len:               length,
		Revision:          revision,
		Degree:            degree,
		Height:            height(pages, root),
		Pages:             reachable.pages(),
		LeafPages:         reachable.leafPages,
		BranchPages:       reachable.branchPages,
		OverflowPages:     reachable.overflowPages,
		Keys:              reachable.keys,
		Separators:        reachable.separators,
		PageBytesUsed:     reachable.bytesUsed(),
		PageBytesFree:     reachable.bytesCapacity() - reachable.bytesUsed(),
		PageBytesCapacity: reachable.bytesCapacity(),
		LeafBytesUsed:     reachable.leafBytesUsed,
		BranchBytesUsed:   reachable.branchBytesUsed,
		OverflowBytesUsed: reachable.overflowBytesUsed,
		AllocatedPages:    len(pages),
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

type reachableStats struct {
	leafPages         int
	branchPages       int
	overflowPages     int
	keys              int
	separators        int
	leafBytesUsed     int
	branchBytesUsed   int
	overflowBytesUsed int
}

func (s reachableStats) pages() int {
	return s.leafPages + s.branchPages + s.overflowPages
}

func (s reachableStats) bytesUsed() int {
	return s.leafBytesUsed + s.branchBytesUsed + s.overflowBytesUsed
}

func (s reachableStats) bytesCapacity() int {
	return s.pages() * PageSize
}

func (s *reachableStats) add(other reachableStats) {
	s.leafPages += other.leafPages
	s.branchPages += other.branchPages
	s.overflowPages += other.overflowPages
	s.keys += other.keys
	s.separators += other.separators
	s.leafBytesUsed += other.leafBytesUsed
	s.branchBytesUsed += other.branchBytesUsed
	s.overflowBytesUsed += other.overflowBytesUsed
}

func collectReachableStats(pages map[PageID]*page, root PageID, seen map[PageID]bool) reachableStats {
	if root == 0 || seen[root] {
		return reachableStats{}
	}
	seen[root] = true

	p := pages[root]
	var stats reachableStats
	if p.isLeaf() {
		stats.leafPages = 1
		stats.keys = int(p.slotCount())
		stats.leafBytesUsed = p.slottedBytesUsed()
		for _, entry := range p.leafEntries() {
			stats.add(collectOverflowStats(pages, entry.value, entry.slotFlags, seen))
		}
	} else {
		stats.branchPages = 1
		stats.separators = int(p.slotCount())
		stats.branchBytesUsed = p.slottedBytesUsed()
	}
	for _, child := range p.childIDs() {
		stats.add(collectReachableStats(pages, child, seen))
	}
	return stats
}

func collectOverflowStats(pages map[PageID]*page, raw []byte, flags uint16, seen map[PageID]bool) reachableStats {
	ref, ok := decodeOverflowRef(raw, flags)
	if !ok {
		return reachableStats{}
	}
	var stats reachableStats
	for id := ref.first; id != 0 && !seen[id]; {
		seen[id] = true
		p := pages[id]
		stats.overflowPages++
		stats.overflowBytesUsed += p.overflowBytesUsed()
		id = p.overflowNext()
	}
	return stats
}
