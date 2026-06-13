package pagebtree

import "container/list"

const DefaultPageCacheCapacity = 128

type pageCache struct {
	capacity    int
	initialized bool
	branches    map[PageID]*cachedBranchEntry
	lru         *list.List
	stats       pageCacheStats
}

type cachedBranch struct {
	checksum uint32
	keys     []string
	children []PageID
}

type cachedBranchEntry struct {
	branch  cachedBranch
	element *list.Element
}

type pageCacheStats struct {
	Entries       int
	Capacity      int
	Hits          int
	Misses        int
	Invalidations int
	Evictions     int
}

func newPageCache(capacity int) pageCache {
	return pageCache{
		capacity:    normalizePageCacheCapacity(capacity),
		initialized: true,
	}
}

func normalizePageCacheCapacity(capacity int) int {
	if capacity == 0 {
		return DefaultPageCacheCapacity
	}
	if capacity < 0 {
		return 0
	}
	return capacity
}

func (c *pageCache) searchBranchChild(p *page, key string) PageID {
	entry := c.branch(p)
	return entry.children[childIndex(entry.keys, key)]
}

func (c *pageCache) branch(p *page) cachedBranch {
	c.init()

	checksum := p.checksum()
	if entry, ok := c.branches[p.id]; ok {
		if entry.branch.checksum == checksum {
			c.stats.Hits++
			c.touch(entry)
			return entry.branch
		}
		c.stats.Invalidations++
		c.remove(p.id, entry)
	}

	keys, children := p.branchParts()
	entry := cachedBranch{
		checksum: checksum,
		keys:     keys,
		children: children,
	}
	c.stats.Misses++
	c.insert(p.id, entry)
	return entry
}

func (c *pageCache) snapshot() pageCacheStats {
	c.init()
	stats := c.stats
	stats.Capacity = c.capacity
	if c.capacity > 0 {
		stats.Entries = len(c.branches)
	}
	return stats
}

func (c *pageCache) init() {
	if !c.initialized {
		c.capacity = DefaultPageCacheCapacity
		c.initialized = true
	}
	c.stats.Capacity = c.capacity
	if c.capacity == 0 {
		return
	}
	if c.branches == nil {
		c.branches = map[PageID]*cachedBranchEntry{}
	}
	if c.lru == nil {
		c.lru = list.New()
	}
}

func (c *pageCache) insert(id PageID, branch cachedBranch) {
	if c.capacity == 0 {
		c.stats.Entries = 0
		return
	}
	for len(c.branches) >= c.capacity {
		c.evictOldest()
	}
	element := c.lru.PushFront(id)
	c.branches[id] = &cachedBranchEntry{branch: branch, element: element}
	c.stats.Entries = len(c.branches)
}

func (c *pageCache) touch(entry *cachedBranchEntry) {
	if c.lru != nil && entry.element != nil {
		c.lru.MoveToFront(entry.element)
	}
}

func (c *pageCache) remove(id PageID, entry *cachedBranchEntry) {
	delete(c.branches, id)
	if c.lru != nil && entry.element != nil {
		c.lru.Remove(entry.element)
	}
	c.stats.Entries = len(c.branches)
}

func (c *pageCache) evictOldest() {
	if c.lru == nil {
		return
	}
	element := c.lru.Back()
	if element == nil {
		return
	}
	id := element.Value.(PageID)
	delete(c.branches, id)
	c.lru.Remove(element)
	c.stats.Evictions++
	c.stats.Entries = len(c.branches)
}
