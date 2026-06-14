package pagebtree

import "errors"

type Tree struct {
	pages                    map[PageID]*page
	root                     PageID
	nextPage                 PageID
	length                   int
	revision                 uint64
	syncedRevision           uint64
	degree                   int
	keyOrder                 KeyOrder
	keyComparator            KeyComparator
	customComparator         bool
	activeReaders            map[uint64]int
	retired                  []retiredPage
	free                     []PageID
	metaFreelistRoot         PageID
	metaFreelistPages        []PageID
	reusedPages              int
	arena                    *mmapArena
	closed                   bool
	readOnly                 bool
	pageCache                pageCache
	rangePrefetchLeafWindow  int
	minRepairPageFillPercent int
	rangePrefetchHints       int
	rangePrefetchPages       int
	mmapWarmupHints          int
	mmapWarmupPages          int
	traceHook                MmapTraceHook
}

type Options struct {
	PageCacheCapacity        int
	RangePrefetchLeafWindow  int
	MinRepairPageFillPercent int
	KeyComparator            KeyComparator
}

func New(degree int) *Tree {
	return NewWithOptions(degree, Options{})
}

func NewWithOptions(degree int, options Options) *Tree {
	return &Tree{
		pages:                    map[PageID]*page{},
		nextPage:                 1,
		degree:                   normalizeDegree(degree),
		keyOrder:                 KeyOrderBytewise,
		keyComparator:            normalizeKeyComparator(options.KeyComparator),
		customComparator:         options.KeyComparator != nil,
		pageCache:                newPageCache(options.PageCacheCapacity),
		rangePrefetchLeafWindow:  normalizeRangePrefetchLeafWindow(options.RangePrefetchLeafWindow),
		minRepairPageFillPercent: normalizeMinRepairPageFillPercent(options.MinRepairPageFillPercent),
	}
}

func (t *Tree) Len() int {
	return t.length
}

func (t *Tree) Revision() uint64 {
	return t.revision
}

func (t *Tree) Get(key string) ([]byte, bool) {
	if t.closed {
		return nil, false
	}
	return searchPageWithCache(t.pages, t.root, key, t.compareKeys, &t.pageCache)
}

// Put inserts or replaces a key in the current root version.
//
// Pages are never changed through an old page id. Before Put changes a page, it
// first allocates a new page id and copies the old page contents there.
func (t *Tree) Put(key string, value []byte) ([]byte, bool) {
	old, replaced, changed := t.putStaged(key, value)
	if changed {
		t.publishStagedMutation()
	}
	return old, replaced
}

func (t *Tree) putStaged(key string, value []byte) ([]byte, bool, bool) {
	if t.closed || t.readOnly {
		return nil, false, false
	}
	if t.root == 0 {
		id := t.allocPage()
		leaf := t.newPage(id, flagLeaf)
		t.writeLeafEntries(leaf, []leafEntry{{key: key, value: cloneBytes(value)}})
		t.pages[id] = leaf
		t.root = id
		t.length = 1
		return nil, false, true
	}

	rootID := t.copyPage(t.root)
	old, replaced, split := t.insert(rootID, key, value)
	if split != nil {
		newRootID := t.allocPage()
		newRoot := t.newPage(newRootID, flagBranch)
		t.pages[newRootID] = newRoot
		mustWriteBranchParts(newRoot, []string{split.separator}, []PageID{rootID, split.right})
		rootID = newRootID
	}

	t.root = rootID
	if !replaced {
		t.length++
	}
	return old, replaced, true
}

func (t *Tree) publishStagedMutation() {
	t.revision++
	t.reclaimRetiredPages()
	t.relinkLeaves()
}

func (t *Tree) Range(visit func(string, []byte) bool) {
	if t.closed {
		return
	}
	if t.activeReaderCount() == 0 && rangeLinkedLeaves(t.pages, t.root, t.compareKeys, t.rangePrefetchLeafWindow, t.rangePrefetcher(), visit) {
		return
	}
	rangePage(t.pages, t.root, t.compareKeys, visit)
}

// RangeFrom visits keys greater than or equal to start in sorted order.
func (t *Tree) RangeFrom(start string, visit func(string, []byte) bool) {
	if t.closed {
		return
	}
	if t.activeReaderCount() == 0 && rangeLinkedLeavesFrom(t.pages, t.root, start, t.compareKeys, t.rangePrefetchLeafWindow, t.rangePrefetcher(), visit) {
		return
	}
	rangePageFrom(t.pages, t.root, start, t.compareKeys, visit)
}

// RangeBetween visits keys greater than or equal to start and less than end.
func (t *Tree) RangeBetween(start, end string, visit func(string, []byte) bool) {
	if t.closed {
		return
	}
	if t.activeReaderCount() == 0 && rangeLinkedLeavesBetween(t.pages, t.root, start, end, t.compareKeys, t.rangePrefetchLeafWindow, t.rangePrefetcher(), visit) {
		return
	}
	rangePageBetween(t.pages, t.root, start, end, t.compareKeys, visit)
}

func (t *Tree) rangePrefetcher() func(PageID, PageID) {
	if t.arena == nil || t.rangePrefetchLeafWindow <= 0 {
		return nil
	}
	advised := map[PageID]bool{}
	return func(start, end PageID) {
		if start >= end {
			return
		}
		var runStart PageID
		flush := func(runEnd PageID) {
			if runStart == 0 {
				return
			}
			if err := t.arena.advisePageRange(runStart, runEnd, MmapAccessWillNeed); err == nil {
				t.rangePrefetchHints++
				t.rangePrefetchPages += int(runEnd - runStart)
			}
			runStart = 0
		}
		for id := start; id < end; id++ {
			if advised[id] {
				flush(id)
				continue
			}
			advised[id] = true
			if runStart == 0 {
				runStart = id
			}
		}
		flush(end)
	}
}

func normalizeRangePrefetchLeafWindow(window int) int {
	if window < 0 {
		return 0
	}
	if window == 0 {
		return DefaultRangePrefetchLeafWindow
	}
	return window
}

func normalizeMinRepairPageFillPercent(percent int) int {
	if percent < 0 {
		return 0
	}
	if percent == 0 {
		return DefaultMinRepairPageFillPercent
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func (t *Tree) Snapshot() *Snapshot {
	if t == nil || t.closed {
		return &Snapshot{closed: true}
	}
	t.beginRead(t.revision)
	return &Snapshot{
		tree:          t,
		pages:         t.pages,
		root:          t.root,
		length:        t.length,
		revision:      t.revision,
		degree:        t.degree,
		keyComparator: t.compareKeys,
	}
}

func (t *Tree) compareKeys(left, right string) int {
	if t == nil || t.keyComparator == nil {
		return compareStrings(left, right)
	}
	return t.keyComparator.CompareKeys(left, right)
}

func (t *Tree) Stats() Stats {
	if t == nil || t.closed {
		return Stats{}
	}
	return statsFor(t)
}

func (t *Tree) allocPage() PageID {
	if len(t.free) > 0 {
		last := len(t.free) - 1
		id := t.free[last]
		t.free = t.free[:last]
		t.reusedPages++
		return id
	}

	id := t.nextPage
	if t.arena != nil {
		if err := t.growMmapForPage(id); err != nil {
			panic(err)
		}
	}
	t.nextPage++
	return id
}

func (t *Tree) copyPage(id PageID) PageID {
	newID := t.allocPage()
	dst := t.newPage(newID, t.pages[id].flags())
	copy(dst.data, t.pages[id].data)
	t.pages[newID] = dst
	t.retirePage(id)
	return newID
}

func (t *Tree) newPage(id PageID, flags uint16) *page {
	if t.arena == nil {
		return newPage(id, flags)
	}

	data, err := t.arena.pageBytes(id)
	if err != nil {
		panic(err)
	}
	p := &page{id: id, data: data}
	p.reset(flags)
	t.arena.markDirtyPage(id)
	return p
}

func (t *Tree) Sync() error {
	if t == nil || t.closed {
		return nil
	}
	if t.readOnly {
		return nil
	}
	var err error
	if t.arena == nil {
		err = t.persistMeta()
	} else {
		err = t.syncMmap()
	}
	if err != nil {
		return err
	}
	t.syncedRevision = t.revision
	return nil
}

// Compact trims reusable mmap pages from the physical end of the database file.
//
// It never moves live pages. Interior free-list pages remain reusable for later
// writes; unused mapped capacity can be released, and only a contiguous suffix
// of already-free page ids can shrink nextPage. If a snapshot is active, Compact
// does nothing so pages that may still be visible to that reader cannot be
// reclaimed or unmapped.
func (t *Tree) Compact() error {
	if t == nil || t.closed || t.readOnly {
		return nil
	}
	if _, hasReader := t.oldestReaderRevision(); hasReader {
		return nil
	}
	t.reclaimRetiredPages()
	if t.arena == nil {
		return nil
	}
	return t.compactMmapTail()
}

func (t *Tree) Close() error {
	if t == nil || t.closed {
		return nil
	}
	if t.arena != nil && t.activeReaderCount() > 0 {
		return ErrActiveReaders
	}
	if t.arena != nil && !t.readOnly {
		t.reclaimRetiredPages()
	}
	if !t.readOnly {
		if err := t.Sync(); err != nil {
			if t.arena == nil {
				return err
			}
			closeErr := t.arena.close()
			if closeErr == nil {
				t.closed = true
			}
			return errors.Join(err, closeErr)
		}
	}
	t.closed = true
	if t.arena == nil {
		return nil
	}
	return t.arena.close()
}
