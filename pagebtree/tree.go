package pagebtree

import "errors"

type Tree struct {
	pages         map[PageID]*page
	root          PageID
	nextPage      PageID
	length        int
	revision      uint64
	degree        int
	activeReaders map[uint64]int
	retired       []retiredPage
	free          []PageID
	reusedPages   int
	arena         *mmapArena
	closed        bool
	readOnly      bool
	pageCache     pageCache
}

func New(degree int) *Tree {
	return &Tree{
		pages:    map[PageID]*page{},
		nextPage: 1,
		degree:   normalizeDegree(degree),
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
	return searchPageWithCache(t.pages, t.root, key, &t.pageCache)
}

// Put inserts or replaces a key in the current root version.
//
// Pages are never changed through an old page id. Before Put changes a page, it
// first allocates a new page id and copies the old page contents there.
func (t *Tree) Put(key string, value []byte) ([]byte, bool) {
	if t.closed || t.readOnly {
		return nil, false
	}
	if t.root == 0 {
		id := t.allocPage()
		leaf := t.newPage(id, flagLeaf)
		t.writeLeafEntries(leaf, []leafEntry{{key: key, value: cloneBytes(value)}})
		t.pages[id] = leaf
		t.root = id
		t.length = 1
		t.revision++
		t.relinkLeaves()
		return nil, false
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
	t.revision++
	t.reclaimRetiredPages()
	t.relinkLeaves()
	return old, replaced
}

func (t *Tree) Range(visit func(string, []byte) bool) {
	if t.closed {
		return
	}
	if t.activeReaderCount() == 0 && rangeLinkedLeaves(t.pages, t.root, t.rangePrefetcher(), visit) {
		return
	}
	rangePage(t.pages, t.root, visit)
}

// RangeFrom visits keys greater than or equal to start in sorted order.
func (t *Tree) RangeFrom(start string, visit func(string, []byte) bool) {
	if t.closed {
		return
	}
	if t.activeReaderCount() == 0 && rangeLinkedLeavesFrom(t.pages, t.root, start, t.rangePrefetcher(), visit) {
		return
	}
	rangePageFrom(t.pages, t.root, start, visit)
}

// RangeBetween visits keys greater than or equal to start and less than end.
func (t *Tree) RangeBetween(start, end string, visit func(string, []byte) bool) {
	if t.closed {
		return
	}
	if t.activeReaderCount() == 0 && rangeLinkedLeavesBetween(t.pages, t.root, start, end, t.rangePrefetcher(), visit) {
		return
	}
	rangePageBetween(t.pages, t.root, start, end, visit)
}

func (t *Tree) rangePrefetcher() func(PageID) {
	if t.arena == nil {
		return nil
	}
	advised := map[PageID]bool{}
	return func(id PageID) {
		if advised[id] {
			return
		}
		advised[id] = true
		_ = t.arena.advisePageRange(id, id+1, MmapAccessWillNeed)
	}
}

func (t *Tree) Snapshot() *Snapshot {
	t.beginRead(t.revision)
	return &Snapshot{
		tree:     t,
		pages:    t.pages,
		root:     t.root,
		length:   t.length,
		revision: t.revision,
		degree:   t.degree,
	}
}

func (t *Tree) Stats() Stats {
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
	if t.readOnly {
		return nil
	}
	if t.arena == nil {
		t.persistMeta()
		return nil
	}
	return t.syncMmap()
}

func (t *Tree) Close() error {
	if t == nil || t.closed {
		return nil
	}
	t.closed = true
	if !t.readOnly {
		if err := t.Sync(); err != nil {
			if t.arena == nil {
				return err
			}
			return errors.Join(err, t.arena.close())
		}
	}
	if t.arena == nil {
		return nil
	}
	return t.arena.close()
}
