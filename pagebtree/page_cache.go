package pagebtree

type pageCache struct {
	branches map[PageID]cachedBranch
	stats    pageCacheStats
}

type cachedBranch struct {
	checksum uint32
	keys     []string
	children []PageID
}

type pageCacheStats struct {
	Entries       int
	Hits          int
	Misses        int
	Invalidations int
}

func (c *pageCache) searchBranchChild(p *page, key string) PageID {
	entry := c.branch(p)
	return entry.children[childIndex(entry.keys, key)]
}

func (c *pageCache) branch(p *page) cachedBranch {
	if c.branches == nil {
		c.branches = map[PageID]cachedBranch{}
	}

	checksum := p.checksum()
	if entry, ok := c.branches[p.id]; ok {
		if entry.checksum == checksum {
			c.stats.Hits++
			return entry
		}
		c.stats.Invalidations++
	}

	keys, children := p.branchParts()
	entry := cachedBranch{
		checksum: checksum,
		keys:     keys,
		children: children,
	}
	c.branches[p.id] = entry
	c.stats.Misses++
	c.stats.Entries = len(c.branches)
	return entry
}

func (c *pageCache) snapshot() pageCacheStats {
	stats := c.stats
	if c.branches != nil {
		stats.Entries = len(c.branches)
	}
	return stats
}
