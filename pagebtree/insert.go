package pagebtree

import "slices"

type leafEntry struct {
	key       string
	value     []byte
	encoded   bool
	slotFlags uint16
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
		old := resolveLeafValue(t.pages, entries[index].value, entries[index].slotFlags)
		t.retireOverflowValue(entries[index].value, entries[index].slotFlags)
		entries[index] = leafEntry{key: key, value: cloneBytes(value)}
		t.writeLeafEntries(p, entries)
		return old, true, nil
	}

	insertAt(&entries, index, leafEntry{key: key, value: cloneBytes(value)})
	if len(entries) <= maxKeys(t.degree) {
		t.writeLeafEntries(p, entries)
		return nil, false, nil
	}

	next := p.nextLeaf()
	mid := leafSplitIndex(entries, t.degree)
	rightID := t.allocPage()
	right := t.newPage(rightID, flagLeaf)
	t.pages[rightID] = right
	t.writeLeafEntries(p, entries[:mid])
	t.writeLeafEntries(right, entries[mid:])
	p.setNextLeaf(rightID)
	right.setNextLeaf(next)
	return nil, false, &splitResult{separator: entries[mid].key, right: rightID}
}

func leafSplitIndex(entries []leafEntry, degree int) int {
	if len(entries) < 2 {
		return len(entries)
	}
	minLeft := max(1, degree-1)
	if minLeft > len(entries)-1 {
		minLeft = len(entries) / 2
	}
	maxLeft := len(entries) - minLeft
	if maxLeft > maxKeys(degree) {
		maxLeft = maxKeys(degree)
	}
	if minNeeded := len(entries) - maxKeys(degree); minLeft < minNeeded {
		minLeft = minNeeded
	}
	if minLeft > maxLeft {
		return len(entries) / 2
	}

	total := 0
	for _, entry := range entries {
		total += leafEntryCellBytes(entry)
	}
	best := minLeft
	bestDistance := total
	prefix := 0
	for i, entry := range entries {
		prefix += leafEntryCellBytes(entry)
		split := i + 1
		if split < minLeft || split > maxLeft {
			continue
		}
		distance := absInt(total - 2*prefix)
		if distance < bestDistance {
			best = split
			bestDistance = distance
		}
	}
	return best
}

func leafEntryCellBytes(entry leafEntry) int {
	return slotSize + len(entry.key) + len(entry.value)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
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

	mid := branchSplitIndex(keys, t.degree)
	promoted := keys[mid]

	rightKeys := append([]string(nil), keys[mid+1:]...)
	rightChildren := append([]PageID(nil), children[mid+1:]...)
	leftKeys := append([]string(nil), keys[:mid]...)
	leftChildren := append([]PageID(nil), children[:mid+1]...)

	rightID := t.allocPage()
	right := t.newPage(rightID, flagBranch)
	t.pages[rightID] = right

	mustWriteBranchParts(p, leftKeys, leftChildren)
	mustWriteBranchParts(right, rightKeys, rightChildren)
	return old, replaced, &splitResult{separator: promoted, right: rightID}
}

func branchSplitIndex(keys []string, degree int) int {
	if len(keys) < 3 {
		return len(keys) / 2
	}
	minKeys := max(1, degree-1)
	lo := max(minKeys, len(keys)-1-maxKeys(degree))
	hi := min(maxKeys(degree), len(keys)-1-minKeys)
	if lo > hi {
		return len(keys) / 2
	}

	prefix := make([]int, len(keys)+1)
	for i, key := range keys {
		prefix[i+1] = prefix[i] + branchCellBytes(key)
	}
	best := lo
	bestDistance := absInt(branchPageBytes(prefix, best) - branchPageBytesSuffix(prefix, best+1))
	for i := lo + 1; i <= hi; i++ {
		distance := absInt(branchPageBytes(prefix, i) - branchPageBytesSuffix(prefix, i+1))
		if distance < bestDistance {
			best = i
			bestDistance = distance
		}
	}
	return best
}

func branchPageBytes(prefix []int, keyCount int) int {
	return pageHeaderSize + prefix[keyCount]
}

func branchPageBytesSuffix(prefix []int, start int) int {
	return pageHeaderSize + prefix[len(prefix)-1] - prefix[start]
}

func branchCellBytes(key string) int {
	return slotSize + len(key) + 8
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
	p.updateChecksum()
}

func (t *Tree) writeLeafEntries(p *page, entries []leafEntry) {
	next := p.nextLeaf()
	encoded := make([]leafEntry, len(entries))
	for i, entry := range entries {
		encoded[i] = entry
		if entry.encoded {
			continue
		}
		encoded[i].value, encoded[i].slotFlags = t.leafCellValue(entry.key, entry.value)
		encoded[i].encoded = true
	}

	for {
		if writeEncodedLeafEntries(p, encoded) {
			p.setNextLeaf(next)
			return
		}
		index := largestInlineLeafEntry(encoded)
		if index < 0 {
			panic("leaf page overflow")
		}
		encoded[index].value = t.overflowCellValue(encoded[index].value)
		encoded[index].slotFlags = slotFlagOverflow
	}
}

func writeEncodedLeafEntries(p *page, entries []leafEntry) bool {
	p.reset(flagLeaf)
	for _, entry := range entries {
		if !p.appendCellWithFlags(entry.key, entry.value, entry.slotFlags) {
			return false
		}
	}
	p.updateChecksum()
	return true
}

func largestInlineLeafEntry(entries []leafEntry) int {
	index := -1
	var size int
	for i, entry := range entries {
		if entry.slotFlags&slotFlagOverflow != 0 {
			continue
		}
		entrySize := len(entry.key) + len(entry.value)
		if index < 0 || entrySize > size {
			index = i
			size = entrySize
		}
	}
	return index
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
	p.updateChecksum()
}
