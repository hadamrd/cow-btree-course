package pagebtree

import "slices"

type leafEntry struct {
	key   string
	value []byte
}

type splitResult struct {
	separator string
	right     PageID
}

func (t *Tree) insert(pageID PageID, key string, value []byte) ([]byte, bool, *splitResult) {
	p := t.pages[pageID]
	if p.isLeaf() {
		return t.insertLeaf(pageID, key, value)
	}

	return t.insertBranch(pageID, key, value)
}

func (t *Tree) insertLeaf(pageID PageID, key string, value []byte) ([]byte, bool, *splitResult) {
	p := t.pages[pageID]
	entries := p.leafEntries()
	index, found := slices.BinarySearchFunc(entries, key, func(entry leafEntry, key string) int {
		return compareStrings(entry.key, key)
	})
	if found {
		old := cloneBytes(entries[index].value)
		entries[index].value = cloneBytes(value)
		mustWriteLeafEntries(p, entries)
		return old, true, nil
	}

	insertAt(&entries, index, leafEntry{key: key, value: cloneBytes(value)})
	if len(entries) <= maxKeys(t.degree) {
		mustWriteLeafEntries(p, entries)
		return nil, false, nil
	}

	mid := len(entries) / 2
	rightID := t.allocPage()
	right := newPage(rightID, flagLeaf)
	t.pages[rightID] = right
	mustWriteLeafEntries(p, entries[:mid])
	mustWriteLeafEntries(right, entries[mid:])
	return nil, false, &splitResult{separator: entries[mid].key, right: rightID}
}

func (t *Tree) insertBranch(pageID PageID, key string, value []byte) ([]byte, bool, *splitResult) {
	p := t.pages[pageID]
	keys, children := p.branchParts()

	index := childIndex(keys, key)
	copiedChildID := t.copyPage(children[index])
	children[index] = copiedChildID
	mustWriteBranchParts(p, keys, children)

	old, replaced, split := t.insert(copiedChildID, key, value)
	if split == nil {
		return old, replaced, nil
	}

	insertAt(&keys, index, split.separator)
	insertAt(&children, index+1, split.right)
	if len(keys) <= maxKeys(t.degree) {
		mustWriteBranchParts(p, keys, children)
		return old, replaced, nil
	}

	mid := len(keys) / 2
	promoted := keys[mid]

	rightKeys := append([]string(nil), keys[mid+1:]...)
	rightChildren := append([]PageID(nil), children[mid+1:]...)
	leftKeys := append([]string(nil), keys[:mid]...)
	leftChildren := append([]PageID(nil), children[:mid+1]...)

	rightID := t.allocPage()
	right := newPage(rightID, flagBranch)
	t.pages[rightID] = right

	mustWriteBranchParts(p, leftKeys, leftChildren)
	mustWriteBranchParts(right, rightKeys, rightChildren)
	return old, replaced, &splitResult{separator: promoted, right: rightID}
}

func insertAt[T any](values *[]T, index int, value T) {
	*values = append(*values, value)
	copy((*values)[index+1:], (*values)[index:])
	(*values)[index] = value
}

func mustWriteLeafEntries(p *page, entries []leafEntry) {
	p.reset(flagLeaf)
	for _, entry := range entries {
		if !p.appendCell(entry.key, entry.value) {
			panic("leaf page overflow")
		}
	}
}

func mustWriteBranchParts(p *page, keys []string, children []PageID) {
	if len(children) != len(keys)+1 {
		panic("branch page must have one more child than key")
	}

	p.reset(flagBranch)
	p.setLeftmostChild(children[0])
	for i, key := range keys {
		if !p.appendCell(key, encodePageIDValue(children[i+1])) {
			panic("branch page overflow")
		}
	}
}
