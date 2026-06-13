package pagebtree

import "slices"

// WarmMmapTree asks the kernel to prefetch pages reachable from the current root.
//
// It is intentionally narrower than sequential advice: it follows the B+tree and
// overflow references, coalesces adjacent page IDs, and leaves reusable/free pages
// alone. Memory-backed trees and closed trees treat this as a no-op.
func (t *Tree) WarmMmapTree() error {
	if t == nil || t.closed || t.arena == nil || t.root == 0 {
		return nil
	}
	hints, pages, err := t.adviseMmapPageIDs(collectReachableMmapPageIDs(t.pages, t.root), MmapAccessWillNeed)
	t.mmapWarmupHints += hints
	t.mmapWarmupPages += pages
	return err
}

func (t *Tree) adviseMmapPageIDs(ids []PageID, pattern MmapAccessPattern) (int, int, error) {
	if len(ids) == 0 {
		return 0, 0, nil
	}

	hints := 0
	pages := 0
	runStart := ids[0]
	runEnd := ids[0] + 1
	flush := func() error {
		if err := t.arena.advisePageRange(runStart, runEnd, pattern); err != nil {
			return err
		}
		hints++
		pages += int(runEnd - runStart)
		return nil
	}

	for _, id := range ids[1:] {
		if id == runEnd {
			runEnd++
			continue
		}
		if err := flush(); err != nil {
			return hints, pages, err
		}
		runStart = id
		runEnd = id + 1
	}
	if err := flush(); err != nil {
		return hints, pages, err
	}
	return hints, pages, nil
}

func collectReachableMmapPageIDs(pages map[PageID]*page, root PageID) []PageID {
	seen := map[PageID]bool{}
	collectReachableMmapPageID(pages, root, seen)

	ids := make([]PageID, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func collectReachableMmapPageID(pages map[PageID]*page, id PageID, seen map[PageID]bool) {
	if id == 0 || seen[id] {
		return
	}
	p := pages[id]
	if p == nil {
		return
	}
	seen[id] = true

	if p.isOverflow() {
		collectReachableMmapPageID(pages, p.overflowNext(), seen)
		return
	}
	if p.isBranch() {
		for _, child := range p.childIDs() {
			collectReachableMmapPageID(pages, child, seen)
		}
		return
	}
	if !p.isLeaf() {
		return
	}
	for i := 0; i < int(p.slotCount()); i++ {
		slot := p.readSlot(i)
		ref, ok := decodeOverflowRef(p.readCellValue(i), slot.flags)
		if ok {
			collectReachableMmapPageID(pages, ref.first, seen)
		}
	}
}
