package pagebtree

import "fmt"

// Check validates the currently open tree.
//
// It walks pages reachable from the current root, verifies page checksums and
// slotted layouts, checks branch routing invariants, validates overflow chains,
// confirms leaf sibling links, and makes sure reusable page IDs are not still
// reachable. Closed or nil trees are inert and return nil.
func (t *Tree) Check() error {
	if t == nil || t.closed {
		return nil
	}
	if t.root == 0 {
		if t.length != 0 {
			return fmt.Errorf("%w: empty root with length %d", ErrTreeInvariant, t.length)
		}
		return t.validateReusablePages(nil)
	}

	reachable, err := t.validateReachablePages()
	if err != nil {
		return err
	}
	if t.activeReaderCount() == 0 {
		if err := t.validateLeafLinks(); err != nil {
			return err
		}
	}
	keyCount := t.countReachableKeys(t.root, map[PageID]bool{})
	if keyCount != t.length {
		return fmt.Errorf("%w: length %d does not match reachable key count %d", ErrTreeInvariant, t.length, keyCount)
	}
	return t.validateReusablePages(reachable)
}

func (t *Tree) validateReusablePages(reachable map[PageID]bool) error {
	seenFree := map[PageID]bool{}
	for _, id := range t.free {
		if id == 0 || id >= t.nextPage {
			return fmt.Errorf("%w: page %d outside reusable range [1,%d)", ErrFreelist, id, t.nextPage)
		}
		if reachable[id] {
			return fmt.Errorf("%w: page %d is still reachable", ErrFreelist, id)
		}
		if seenFree[id] {
			return fmt.Errorf("%w: page %d appears more than once", ErrFreelist, id)
		}
		seenFree[id] = true
	}
	return nil
}

func (t *Tree) validateReachablePages() (map[PageID]bool, error) {
	seen := map[PageID]bool{}
	if err := t.validatePage(t.root, seen); err != nil {
		return nil, err
	}
	return seen, nil
}

func (t *Tree) validateLeafLinks() error {
	leaves := make([]PageID, 0)
	collectLeavesInOrder(t.pages, t.root, &leaves)
	for i, id := range leaves {
		want := PageID(0)
		if i+1 < len(leaves) {
			want = leaves[i+1]
		}
		got := t.pages[id].nextLeaf()
		if got != want {
			return fmt.Errorf("%w: leaf page %d next leaf %d, want %d", ErrTreeInvariant, id, got, want)
		}
	}
	return nil
}

func (t *Tree) countReachableKeys(id PageID, seen map[PageID]bool) int {
	if id == 0 || seen[id] {
		return 0
	}
	seen[id] = true

	p := t.pages[id]
	if p == nil {
		return 0
	}
	if p.isLeaf() {
		return int(p.slotCount())
	}
	count := 0
	for _, child := range p.childIDs() {
		count += t.countReachableKeys(child, seen)
	}
	return count
}

func (t *Tree) validatePage(id PageID, seen map[PageID]bool) error {
	return t.validatePageBounds(id, seen, "", false, "", false)
}

func (t *Tree) validatePageBounds(id PageID, seen map[PageID]bool, lower string, hasLower bool, upper string, hasUpper bool) error {
	if id == 0 {
		return nil
	}
	if seen[id] {
		return fmt.Errorf("%w: page %d is reachable through multiple tree paths", ErrTreeInvariant, id)
	}
	seen[id] = true

	p := t.pages[id]
	if p == nil {
		return fmt.Errorf("%w: reachable page %d is missing", ErrTreeInvariant, id)
	}
	if !p.validChecksum() {
		return fmt.Errorf("%w: page %d", ErrPageChecksum, id)
	}
	if err := p.validateLayout(); err != nil {
		return err
	}
	if p.isLeaf() {
		for i := 0; i < int(p.slotCount()); i++ {
			key := p.readCellKey(i)
			if hasLower && compareStrings(key, lower) < 0 {
				return fmt.Errorf("%w: leaf page %d key %q outside branch bounds", ErrTreeInvariant, id, key)
			}
			if hasUpper && compareStrings(key, upper) >= 0 {
				return fmt.Errorf("%w: leaf page %d key %q outside branch bounds", ErrTreeInvariant, id, key)
			}
			slot := p.readSlot(i)
			value := p.readCellValue(i)
			if err := t.validateOverflowValue(value, slot.flags, seen); err != nil {
				return err
			}
		}
		return nil
	}
	if !p.isBranch() {
		return fmt.Errorf("%w: reachable page %d is not a tree page", ErrTreeInvariant, id)
	}
	children := p.childIDs()
	for index, child := range children {
		if child == 0 {
			return fmt.Errorf("%w: branch page %d child %d is zero", ErrTreeInvariant, id, index)
		}
		childLower, childHasLower := lower, hasLower
		childUpper, childHasUpper := upper, hasUpper
		if index > 0 {
			childLower, childHasLower = p.readCellKey(index-1), true
		}
		if index < len(children)-1 {
			childUpper, childHasUpper = p.readCellKey(index), true
		}
		if childHasLower && childHasUpper && compareStrings(childLower, childUpper) >= 0 {
			return fmt.Errorf("%w: branch page %d child %d has empty key bounds", ErrTreeInvariant, id, index)
		}
		if err := t.validatePageBounds(child, seen, childLower, childHasLower, childUpper, childHasUpper); err != nil {
			return err
		}
		if index == 0 {
			continue
		}
		separator := p.readCellKey(index - 1)
		first, ok := t.firstKey(child)
		if !ok {
			return fmt.Errorf("%w: branch page %d child %d has no first key", ErrTreeInvariant, id, index)
		}
		if separator != first {
			return fmt.Errorf("%w: branch page %d separator %q does not match child %d first key %q", ErrTreeInvariant, id, separator, index, first)
		}
	}
	return nil
}

func (t *Tree) validateOverflowValue(raw []byte, flags uint16, seen map[PageID]bool) error {
	ref, ok := decodeOverflowRef(raw, flags)
	if !ok {
		return nil
	}
	if ref.first == 0 {
		return fmt.Errorf("%w: overflow reference has no first page", ErrOverflowInvariant)
	}
	var length int
	for id := ref.first; id != 0; {
		if seen[id] {
			return fmt.Errorf("%w: overflow chain loops through page %d", ErrOverflowInvariant, id)
		}
		seen[id] = true
		p := t.pages[id]
		if p == nil {
			return fmt.Errorf("%w: reachable overflow page %d is missing", ErrOverflowInvariant, id)
		}
		if !p.validChecksum() {
			return fmt.Errorf("%w: page %d", ErrPageChecksum, id)
		}
		if err := p.validateLayout(); err != nil {
			return err
		}
		if !p.isOverflow() {
			return fmt.Errorf("%w: page %d in overflow chain is not an overflow page", ErrOverflowInvariant, id)
		}
		length += p.overflowPayloadLen()
		id = p.overflowNext()
	}
	if length != ref.length {
		return fmt.Errorf("%w: chain length %d does not match referenced length %d", ErrOverflowInvariant, length, ref.length)
	}
	return nil
}
