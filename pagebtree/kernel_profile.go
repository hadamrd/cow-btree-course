package pagebtree

// MDBKernelProfile describes how a Tree maps onto the small OpenLDAP MDB/LMDB
// kernel model studied by this repository.
//
// It is intentionally observational: calling MDBKernelProfile never changes
// tree bytes, reader-table slots, kernel advice, or durability state.
type MDBKernelProfile struct {
	Name string

	Storage        string
	PageSize       int
	MaxMappedPages int
	AccessPattern  MmapAccessPattern
	KeyOrder       KeyOrder
	KeyComparator  KeyComparatorKind
	ReadOnly       bool
	Closed         bool

	Root     PageID
	Revision uint64
	Keys     int
	Pages    int

	ReusablePages int
	RetiredPages  int
	ActiveReaders int
	DirtyPages    int

	SlottedPages                  bool
	BPlusTreePages                bool
	CopyOnWrite                   bool
	ByteAwareSplitPoints          bool
	ByteAwareDeleteRedistribution bool
	ByteFitDeleteMerges           bool
	ConfigurableRepairFill        bool
	MinRepairPageFillPercent      int
	DualCheckedMetaPages          bool
	SerializedWriter              bool
	ReaderTable                   bool
	ReaderPinnedRecycling         bool
	PersistedReclaimRecords       bool
	KernelPageCache               bool
	RawHeapPageCache              bool
	DerivedBranchRoutingCache     bool

	DerivedBranchRoutingCacheCapacity      int
	DerivedBranchRoutingCacheEntries       int
	DerivedBranchRoutingCacheHits          int
	DerivedBranchRoutingCacheMisses        int
	DerivedBranchRoutingCacheInvalidations int
	DerivedBranchRoutingCacheEvictions     int
}

// MDBKernelProfile returns a compact research profile for the current tree.
//
// Memory-backed trees expose the same page and copy-on-write mechanics, while
// mmap-backed trees additionally report the mapped-file, metadata, reader-table,
// and kernel page-cache pieces that make the OpenLDAP-style kernel interesting.
func (t *Tree) MDBKernelProfile() MDBKernelProfile {
	profile := MDBKernelProfile{
		Name:                          "openldap-mdb-inspired",
		Storage:                       "memory",
		PageSize:                      PageSize,
		KeyOrder:                      KeyOrderBytewise,
		KeyComparator:                 KeyComparatorBytewise,
		SlottedPages:                  true,
		BPlusTreePages:                true,
		CopyOnWrite:                   true,
		ByteAwareSplitPoints:          true,
		ByteAwareDeleteRedistribution: true,
		ByteFitDeleteMerges:           true,
		ConfigurableRepairFill:        true,
		MinRepairPageFillPercent:      DefaultMinRepairPageFillPercent,
		ReaderPinnedRecycling:         true,
		RawHeapPageCache:              false,
	}
	if t == nil {
		return profile
	}

	profile.ReadOnly = t.readOnly
	profile.Closed = t.closed
	profile.KeyOrder = normalizeKeyOrder(t.keyOrder)
	profile.KeyComparator = keyComparatorKindForOrder(t.keyOrder)
	if t.customComparator {
		profile.KeyComparator = KeyComparatorCustom
	}
	profile.Root = t.root
	profile.Revision = t.revision
	profile.Keys = t.length
	profile.Pages = countProfilePages(t)
	profile.MinRepairPageFillPercent = t.minRepairPageFillPercent
	profile.ReusablePages = len(t.free)
	profile.RetiredPages = len(t.retired)
	profile.ActiveReaders = t.activeReaderCount()
	cacheStats := t.pageCache.snapshot()
	profile.DerivedBranchRoutingCache = cacheStats.Capacity > 0
	profile.DerivedBranchRoutingCacheCapacity = cacheStats.Capacity
	profile.DerivedBranchRoutingCacheEntries = cacheStats.Entries
	profile.DerivedBranchRoutingCacheHits = cacheStats.Hits
	profile.DerivedBranchRoutingCacheMisses = cacheStats.Misses
	profile.DerivedBranchRoutingCacheInvalidations = cacheStats.Invalidations
	profile.DerivedBranchRoutingCacheEvictions = cacheStats.Evictions

	if t.arena == nil {
		return profile
	}

	profile.Storage = "mmap"
	profile.MaxMappedPages = t.arena.maxPages
	profile.AccessPattern = normalizeMmapAccessPattern(t.arena.accessPattern)
	profile.DirtyPages = len(t.arena.dirtyPages)
	profile.DualCheckedMetaPages = true
	profile.SerializedWriter = !t.readOnly
	profile.ReaderTable = t.arena.readerTable != nil
	profile.PersistedReclaimRecords = true
	profile.KernelPageCache = true
	return profile
}

func countProfilePages(t *Tree) int {
	if t.root == 0 {
		return 0
	}
	return collectReachableStats(t.pages, t.root, map[PageID]bool{}).pages()
}
