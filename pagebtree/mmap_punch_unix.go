//go:build unix

package pagebtree

import "fmt"

// PunchFreeMmapPages asks the filesystem to deallocate page-size-aligned ranges
// for mmap pages that are already reusable. It never removes pages from the
// free list and never changes file size.
func (t *Tree) PunchFreeMmapPages() (MmapHolePunchStats, error) {
	var stats MmapHolePunchStats
	if t == nil || t.closed || t.readOnly || t.arena == nil {
		return stats, nil
	}
	t.reclaimRetiredPages()

	recoverable, err := t.recoverableMmapPages()
	if err != nil {
		return stats, err
	}
	free := make([]PageID, 0, len(t.free))
	seen := map[PageID]bool{}
	for _, id := range t.free {
		if id < firstTreePageID || id >= t.nextPage || seen[id] {
			continue
		}
		seen[id] = true
		stats.FreePages++
		if recoverable[id] {
			stats.SkippedRecoverablePages++
			continue
		}
		free = append(free, id)
	}
	for _, pageRange := range coalescedPageRanges(free) {
		if err := punchFileRange(t.arena.file, pageRange.start, pageRange.end); err != nil {
			return stats, err
		}
		pages := int(pageRange.end - pageRange.start)
		stats.Ranges++
		stats.PunchedPages += pages
		stats.PunchedBytes += int64(pages * PageSize)
	}
	return stats, nil
}

func (t *Tree) recoverableMmapPages() (map[PageID]bool, error) {
	recoverable := map[PageID]bool{}
	if t == nil || t.arena == nil || len(t.arena.data) < metaPageCount*PageSize {
		return recoverable, nil
	}
	for index := 0; index < metaPageCount; index++ {
		metaPage := t.arena.data[index*PageSize : (index+1)*PageSize]
		record, ok, err := readMetaPageChecked(metaPage)
		if err != nil || !ok {
			continue
		}
		if err := validateMetaSlot(record, index); err != nil {
			continue
		}
		if err := t.validateMetaKeyOrder(record); err != nil {
			continue
		}
		if err := t.validateMetaBounds(record); err != nil {
			continue
		}
		candidate := *t
		candidate.applyMetaRecord(record)
		if _, err := candidate.resolveMetaFreelist(&record); err != nil {
			continue
		}
		reachable, err := candidate.validateReachablePages()
		if err != nil {
			continue
		}
		for id := range reachable {
			recoverable[id] = true
		}
	}
	return recoverable, nil
}

func validatePunchRange(startPage, endPage PageID) error {
	if startPage < firstTreePageID || endPage < startPage {
		return fmt.Errorf("invalid mmap punch page range [%d,%d)", startPage, endPage)
	}
	if startPage == endPage {
		return nil
	}
	return nil
}
