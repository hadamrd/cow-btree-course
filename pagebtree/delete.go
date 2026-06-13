package pagebtree

import "slices"

// Delete removes key from the current root version.
//
// Like Put, Delete is copy-on-write: it copies the root and every page on the
// search path before changing page bytes. It merges underfull leaf siblings
// when one sibling can absorb the other, then rebuilds branch separators and
// collapses a one-child root. It deliberately stops short of full branch-level
// sibling redistribution.
func (t *Tree) Delete(key string) ([]byte, bool) {
	if t.closed || t.readOnly || t.root == 0 {
		return nil, false
	}

	old, found := t.Get(key)
	if !found {
		return nil, false
	}

	rootID := t.copyPage(t.root)
	if !t.deleteFrom(rootID, key) {
		return nil, false
	}
	t.root = t.collapseRoot(rootID)
	t.length--
	t.revision++
	t.reclaimRetiredPages()
	t.relinkLeaves()
	return old, true
}

func (t *Tree) deleteFrom(pageID PageID, key string) bool {
	p := t.pages[pageID]
	if p.isLeaf() {
		return t.deleteFromLeaf(p, key)
	}
	return t.deleteFromBranch(p, key)
}

func (t *Tree) deleteFromLeaf(p *page, key string) bool {
	entries := p.leafEntries()
	index, found := slices.BinarySearchFunc(entries, key, func(entry leafEntry, key string) int {
		return compareStrings(entry.key, key)
	})
	if !found {
		return false
	}

	t.retireOverflowValue(entries[index].value, entries[index].slotFlags)
	entries = append(entries[:index], entries[index+1:]...)
	t.writeLeafEntries(p, entries)
	return true
}

func (t *Tree) deleteFromBranch(p *page, key string) bool {
	keys, children := p.branchParts()
	index := childIndex(keys, key)
	copiedChildID := t.copyPage(children[index])
	children[index] = copiedChildID

	if !t.deleteFrom(copiedChildID, key) {
		return false
	}
	if t.subtreeEmpty(copiedChildID) {
		t.retirePage(copiedChildID)
		children = append(children[:index], children[index+1:]...)
	} else {
		children = t.mergeUnderfullLeaf(children, index)
	}
	t.writeBranchChildren(p, children)
	return true
}

func (t *Tree) mergeUnderfullLeaf(children []PageID, index int) []PageID {
	if len(children) <= 1 || index < 0 || index >= len(children) {
		return children
	}
	child := t.pages[children[index]]
	if child == nil || !child.isLeaf() || int(child.slotCount()) >= minKeys(t.degree) {
		return children
	}

	if index > 0 {
		left := t.pages[children[index-1]]
		if left != nil && left.isLeaf() {
			merged := append(left.leafEntries(), child.leafEntries()...)
			if len(merged) <= maxKeys(t.degree) {
				leftID := t.copyPage(children[index-1])
				left = t.pages[leftID]
				children[index-1] = leftID
				t.writeLeafEntries(left, merged)
				left.setNextLeaf(child.nextLeaf())
				t.retirePage(children[index])
				children = append(children[:index], children[index+1:]...)
				return children
			}
		}
	}

	if index+1 < len(children) {
		right := t.pages[children[index+1]]
		if right != nil && right.isLeaf() {
			merged := append(child.leafEntries(), right.leafEntries()...)
			if len(merged) <= maxKeys(t.degree) {
				rightID := t.copyPage(children[index+1])
				right = t.pages[rightID]
				children[index+1] = rightID
				t.writeLeafEntries(child, merged)
				child.setNextLeaf(right.nextLeaf())
				t.retirePage(rightID)
				children = append(children[:index+1], children[index+2:]...)
				return children
			}
		}
	}
	return children
}

func (t *Tree) writeBranchChildren(p *page, children []PageID) {
	if len(children) == 0 {
		p.reset(flagLeaf)
		return
	}
	keys := make([]string, 0, len(children)-1)
	for _, child := range children[1:] {
		key, ok := t.firstKey(child)
		if !ok {
			continue
		}
		keys = append(keys, key)
	}
	mustWriteBranchParts(p, keys, children)
}

func (t *Tree) collapseRoot(rootID PageID) PageID {
	for rootID != 0 {
		p := t.pages[rootID]
		if p.isLeaf() {
			if p.slotCount() == 0 {
				t.retirePage(rootID)
				return 0
			}
			return rootID
		}
		if !p.isBranch() {
			return rootID
		}
		children := p.childIDs()
		switch len(children) {
		case 0:
			t.retirePage(rootID)
			return 0
		case 1:
			t.retirePage(rootID)
			rootID = children[0]
		default:
			return rootID
		}
	}
	return 0
}

func (t *Tree) subtreeEmpty(id PageID) bool {
	p := t.pages[id]
	if p == nil {
		return true
	}
	if p.isLeaf() {
		return p.slotCount() == 0
	}
	for _, child := range p.childIDs() {
		if !t.subtreeEmpty(child) {
			return false
		}
	}
	return true
}

func (t *Tree) firstKey(id PageID) (string, bool) {
	for id != 0 {
		p := t.pages[id]
		if p == nil {
			return "", false
		}
		if p.isLeaf() {
			if p.slotCount() == 0 {
				return "", false
			}
			key, _ := p.readCell(0)
			return key, true
		}
		id = p.leftmostChild()
	}
	return "", false
}

func minKeys(degree int) int {
	return degree - 1
}
