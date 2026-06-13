package pagebtree

func (t *Tree) relinkLeaves() {
	if t.root == 0 || t.activeReaderCount() > 0 {
		return
	}
	leaves := make([]PageID, 0)
	collectLeavesInOrder(t.pages, t.root, &leaves)
	for i, id := range leaves {
		next := PageID(0)
		if i+1 < len(leaves) {
			next = leaves[i+1]
		}
		p := t.pages[id]
		if p.nextLeaf() == next {
			continue
		}
		p.setNextLeaf(next)
		if t.arena != nil {
			t.arena.markDirtyPage(id)
		}
	}
}

func collectLeavesInOrder(pages map[PageID]*page, root PageID, out *[]PageID) {
	if root == 0 {
		return
	}
	p := pages[root]
	if p.isLeaf() {
		*out = append(*out, root)
		return
	}
	for _, child := range p.childIDs() {
		collectLeavesInOrder(pages, child, out)
	}
}
