package pagebtree

import "slices"

// Delete removes key from the current root version.
//
// Like Put, Delete is copy-on-write: it copies the root and every page on the
// search path before changing page bytes. It deliberately does not implement
// full B+tree sibling redistribution yet, but it does remove empty children,
// rebuild branch separators, and collapse a one-child root.
func (t *Tree) Delete(key string) ([]byte, bool) {
	if t.closed || t.root == 0 {
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
	t.persistMeta()
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
	}
	t.writeBranchChildren(p, children)
	return true
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
