package pagebtree

import "slices"

const DefaultMinRepairPageFillPercent = 25

// Delete removes key from the current root version.
//
// Like Put, Delete is copy-on-write: it copies the root and every page on the
// search path before changing page bytes. It merges or redistributes underfull
// leaf siblings, borrows before descending into minimum-fill branches when a
// sibling can lend, merges or redistributes underfull branch siblings, then
// rebuilds branch separators and collapses a one-child root. Leaf and branch
// repair can also trigger on low byte fill at the minimum key count, and
// redistribution choose split points with encoded cell byte footprints.
func (t *Tree) Delete(key string) ([]byte, bool) {
	old, deleted, changed := t.deleteStaged(key)
	if changed {
		t.publishStagedMutation()
	}
	return old, deleted
}

func (t *Tree) deleteStaged(key string) ([]byte, bool, bool) {
	if t.closed || t.readOnly || t.root == 0 {
		return nil, false, false
	}
	old, found := t.Get(key)
	if !found {
		return nil, false, false
	}

	rootID := t.copyPage(t.root)
	if !t.deleteFrom(rootID, key) {
		return nil, false, false
	}
	t.root = t.collapseRoot(rootID)
	t.length--
	return old, true, true
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
		return t.compareKeys(entry.key, key)
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
	index := childIndex(keys, key, t.compareKeys)
	children, copiedChildID := t.borrowBranchChildBeforeDescent(children, index)
	if copiedChildID == 0 {
		copiedChildID = t.copyPage(children[index])
		children[index] = copiedChildID
	}

	if !t.deleteFrom(copiedChildID, key) {
		return false
	}
	if t.subtreeEmpty(copiedChildID) {
		t.retirePage(copiedChildID)
		children = append(children[:index], children[index+1:]...)
	} else {
		children = t.mergeUnderfullLeaf(children, index)
		children = t.mergeUnderfullBranch(children, index)
	}
	t.writeBranchChildren(p, children)
	return true
}

func (t *Tree) borrowBranchChildBeforeDescent(children []PageID, index int) ([]PageID, PageID) {
	if index < 0 || index >= len(children) {
		return children, 0
	}
	child := t.pages[children[index]]
	if child == nil || !child.isBranch() || int(child.slotCount()) > minKeys(t.degree) {
		return children, 0
	}

	if index > 0 {
		left := t.pages[children[index-1]]
		if left != nil && left.isBranch() && int(left.slotCount()) > minKeys(t.degree) {
			leftID := t.copyPage(children[index-1])
			childID := t.copyPage(children[index])
			left = t.pages[leftID]
			child = t.pages[childID]
			children[index-1] = leftID
			children[index] = childID

			leftChildren := left.childIDs()
			childChildren := child.childIDs()
			borrowed := leftChildren[len(leftChildren)-1]
			t.writeBranchChildren(left, leftChildren[:len(leftChildren)-1])
			t.writeBranchChildren(child, append([]PageID{borrowed}, childChildren...))
			return children, childID
		}
	}

	if index+1 < len(children) {
		right := t.pages[children[index+1]]
		if right != nil && right.isBranch() && int(right.slotCount()) > minKeys(t.degree) {
			childID := t.copyPage(children[index])
			rightID := t.copyPage(children[index+1])
			child = t.pages[childID]
			right = t.pages[rightID]
			children[index] = childID
			children[index+1] = rightID

			childChildren := child.childIDs()
			rightChildren := right.childIDs()
			borrowed := rightChildren[0]
			t.writeBranchChildren(child, append(childChildren, borrowed))
			t.writeBranchChildren(right, rightChildren[1:])
			return children, childID
		}
	}

	return children, 0
}

func (t *Tree) mergeUnderfullLeaf(children []PageID, index int) []PageID {
	if len(children) <= 1 || index < 0 || index >= len(children) {
		return children
	}
	child := t.pages[children[index]]
	if child == nil || !child.isLeaf() || !t.leafNeedsRepair(child) {
		return children
	}

	if index > 0 {
		left := t.pages[children[index-1]]
		if left != nil && left.isLeaf() {
			merged := append(left.leafEntries(), child.leafEntries()...)
			if len(merged) <= maxKeys(t.degree) && leafEntriesFitPage(merged) {
				leftID := t.copyPage(children[index-1])
				left = t.pages[leftID]
				children[index-1] = leftID
				t.writeLeafEntries(left, merged)
				left.setNextLeaf(child.nextLeaf())
				t.retirePage(children[index])
				children = append(children[:index], children[index+1:]...)
				return children
			}
			leftID := t.copyPage(children[index-1])
			left = t.pages[leftID]
			children[index-1] = leftID
			split := leafSplitIndex(merged, t.degree)
			t.writeLeafEntries(left, merged[:split])
			left.setNextLeaf(children[index])
			t.writeLeafEntries(child, merged[split:])
			return children
		}
	}

	if index+1 < len(children) {
		right := t.pages[children[index+1]]
		if right != nil && right.isLeaf() {
			merged := append(child.leafEntries(), right.leafEntries()...)
			if len(merged) <= maxKeys(t.degree) && leafEntriesFitPage(merged) {
				rightID := t.copyPage(children[index+1])
				right = t.pages[rightID]
				children[index+1] = rightID
				t.writeLeafEntries(child, merged)
				child.setNextLeaf(right.nextLeaf())
				t.retirePage(rightID)
				children = append(children[:index+1], children[index+2:]...)
				return children
			}
			rightID := t.copyPage(children[index+1])
			right = t.pages[rightID]
			children[index+1] = rightID
			split := leafSplitIndex(merged, t.degree)
			t.writeLeafEntries(child, merged[:split])
			child.setNextLeaf(rightID)
			t.writeLeafEntries(right, merged[split:])
			return children
		}
	}
	return children
}

func (t *Tree) leafNeedsRepair(p *page) bool {
	count := int(p.slotCount())
	if count < minKeys(t.degree) {
		return true
	}
	return count <= minKeys(t.degree) && t.isBelowMinRepairFill(p)
}

func leafEntriesFitPage(entries []leafEntry) bool {
	bytes := pageHeaderSize
	for _, entry := range entries {
		bytes += leafEntryCellBytes(entry)
	}
	return bytes <= PageSize
}

func (t *Tree) mergeUnderfullBranch(children []PageID, index int) []PageID {
	if len(children) <= 1 || index < 0 || index >= len(children) {
		return children
	}
	child := t.pages[children[index]]
	if child == nil || !child.isBranch() || !t.branchNeedsRepair(child) {
		return children
	}

	if index > 0 {
		left := t.pages[children[index-1]]
		if left != nil && left.isBranch() {
			mergedChildren := append(left.childIDs(), child.childIDs()...)
			if len(mergedChildren) <= maxKeys(t.degree)+1 && t.branchChildrenFitPage(mergedChildren) {
				leftID := t.copyPage(children[index-1])
				left = t.pages[leftID]
				children[index-1] = leftID
				t.writeBranchChildren(left, mergedChildren)
				t.retirePage(children[index])
				return append(children[:index], children[index+1:]...)
			}
			leftID := t.copyPage(children[index-1])
			left = t.pages[leftID]
			children[index-1] = leftID
			split := t.branchChildSplitIndex(mergedChildren)
			t.writeBranchChildren(left, mergedChildren[:split])
			t.writeBranchChildren(child, mergedChildren[split:])
			return children
		}
	}

	if index+1 < len(children) {
		right := t.pages[children[index+1]]
		if right != nil && right.isBranch() {
			mergedChildren := append(child.childIDs(), right.childIDs()...)
			if len(mergedChildren) <= maxKeys(t.degree)+1 && t.branchChildrenFitPage(mergedChildren) {
				t.writeBranchChildren(child, mergedChildren)
				t.retirePage(children[index+1])
				return append(children[:index+1], children[index+2:]...)
			}
			rightID := t.copyPage(children[index+1])
			right = t.pages[rightID]
			children[index+1] = rightID
			split := t.branchChildSplitIndex(mergedChildren)
			t.writeBranchChildren(child, mergedChildren[:split])
			t.writeBranchChildren(right, mergedChildren[split:])
			return children
		}
	}
	return children
}

func (t *Tree) branchNeedsRepair(p *page) bool {
	count := int(p.slotCount())
	if count < minKeys(t.degree) {
		return true
	}
	return count <= minKeys(t.degree) && t.isBelowMinRepairFill(p)
}

func (t *Tree) isBelowMinRepairFill(p *page) bool {
	threshold := t.minRepairPageFillBytes()
	return threshold > 0 && p.slottedBytesUsed() < threshold
}

func (t *Tree) minRepairPageFillBytes() int {
	return PageSize * t.minRepairPageFillPercent / 100
}

func (t *Tree) branchChildSplitIndex(children []PageID) int {
	if len(children) < 2 {
		return len(children)
	}
	minChildren := max(1, t.degree)
	lo := max(minChildren, len(children)-(maxKeys(t.degree)+1))
	hi := min(maxKeys(t.degree)+1, len(children)-minChildren)
	if lo > hi {
		return len(children) / 2
	}

	best := lo
	bestDistance := absInt(t.branchChildrenBytes(children[:lo]) - t.branchChildrenBytes(children[lo:]))
	for split := lo + 1; split <= hi; split++ {
		distance := absInt(t.branchChildrenBytes(children[:split]) - t.branchChildrenBytes(children[split:]))
		if distance < bestDistance {
			best = split
			bestDistance = distance
		}
	}
	return best
}

func (t *Tree) branchChildrenBytes(children []PageID) int {
	bytes := pageHeaderSize
	for _, child := range children[1:] {
		key, ok := t.firstKey(child)
		if !ok {
			continue
		}
		bytes += branchCellBytes(key)
	}
	return bytes
}

func (t *Tree) branchChildrenFitPage(children []PageID) bool {
	return t.branchChildrenBytes(children) <= PageSize
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
