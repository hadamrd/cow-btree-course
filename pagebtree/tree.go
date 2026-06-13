package pagebtree

type Tree struct {
	pages    map[PageID]*page
	root     PageID
	nextPage PageID
	length   int
	revision uint64
	degree   int
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
	return searchPage(t.pages, t.root, key)
}

// Put inserts or replaces a key in the current root version.
//
// Pages are never changed through an old page id. Before Put changes a page, it
// first allocates a new page id and copies the old page contents there.
func (t *Tree) Put(key string, value []byte) ([]byte, bool) {
	if t.root == 0 {
		id := t.allocPage()
		t.pages[id] = newLeaf(id, key, value)
		t.root = id
		t.length = 1
		t.revision++
		return nil, false
	}

	rootID := t.copyPage(t.root)
	if t.pages[rootID].full(t.degree) {
		newRootID := t.allocPage()
		t.pages[newRootID] = &page{
			id:       newRootID,
			leaf:     false,
			children: []PageID{rootID},
		}
		t.splitChild(newRootID, 0)
		rootID = newRootID
	}

	old, replaced := t.insertNonFull(rootID, key, value)
	t.root = rootID
	if !replaced {
		t.length++
	}
	t.revision++
	return old, replaced
}

func (t *Tree) Range(visit func(string, []byte) bool) {
	rangePage(t.pages, t.root, visit)
}

func (t *Tree) Snapshot() Snapshot {
	return Snapshot{
		pages:    t.pages,
		root:     t.root,
		length:   t.length,
		revision: t.revision,
		degree:   t.degree,
	}
}

func (t *Tree) Stats() Stats {
	return statsFor(t.pages, t.root, t.length, t.revision, t.degree)
}

func (t *Tree) allocPage() PageID {
	id := t.nextPage
	t.nextPage++
	return id
}

func (t *Tree) copyPage(id PageID) PageID {
	newID := t.allocPage()
	t.pages[newID] = t.pages[id].clone(newID)
	return newID
}
