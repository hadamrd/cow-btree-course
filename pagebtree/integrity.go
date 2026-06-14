package pagebtree

import (
	"fmt"
	"sort"
)

// AuditReport describes the validation result and traversal evidence produced
// by Tree.Audit.
type AuditReport struct {
	Stats            Stats
	ReachablePageIDs []PageID
	FreePageIDs      []PageID
	RetiredPageIDs   []PageID
	PageSummaries    []PageSummary
	LeafLinksChecked bool
	LeafLinksSkipped bool
	Error            error
}

// PageSummary is a compact, value-free description of one page known to Audit.
//
// Role distinguishes pages reachable from the current root from reusable or
// reader-pinned pages. Kind describes the page header kind when the page bytes
// are present. Children is populated for branch pages; NextPage is populated for
// linked leaves and overflow pages when their next pointer is nonzero.
type PageSummary struct {
	ID              PageID   `json:"id"`
	Role            string   `json:"role"`
	Kind            string   `json:"kind"`
	Keys            int      `json:"keys,omitempty"`
	Separators      int      `json:"separators,omitempty"`
	Children        []PageID `json:"children,omitempty"`
	NextPage        PageID   `json:"next_page,omitempty"`
	BytesUsed       int      `json:"bytes_used,omitempty"`
	BytesFree       int      `json:"bytes_free,omitempty"`
	BytesCapacity   int      `json:"bytes_capacity,omitempty"`
	RetiredRevision uint64   `json:"retired_revision,omitempty"`
}

// Valid reports whether Audit found no validation error.
func (r AuditReport) Valid() bool {
	return r.Error == nil
}

// Check validates the currently open tree.
//
// It walks pages reachable from the current root, verifies page checksums and
// slotted layouts, checks branch routing invariants, validates overflow chains,
// confirms leaf sibling links, and makes sure reusable page IDs are not still
// reachable. Closed or nil trees are inert and return nil.
func (t *Tree) Check() error {
	return t.Audit().Error
}

// Audit validates the currently open tree and returns traversal evidence.
//
// It is intentionally read-only. A valid report contains the same rich counters
// as Stats plus sorted reachable, free, and retired page IDs. If validation
// fails, Error is the same kind of error Check would return and any page IDs
// reached before the failure are still reported for diagnosis.
func (t *Tree) Audit() AuditReport {
	report := AuditReport{}
	if t == nil || t.closed {
		return report
	}
	report.Stats = auditBaseStats(t)
	report.FreePageIDs = sortedPageIDsFromSlice(t.free)
	report.RetiredPageIDs = sortedRetiredPageIDs(t.retired)

	if t.root == 0 {
		report.PageSummaries = t.pageSummaries(nil)
		if t.length != 0 {
			report.Error = fmt.Errorf("%w: empty root with length %d", ErrTreeInvariant, t.length)
			return report
		}
		report.Error = t.validateReusablePages(nil)
		if report.Error == nil {
			report.Stats = statsFor(t)
		}
		return report
	}

	reachable, err := t.validateReachablePages()
	report.ReachablePageIDs = sortedPageIDs(reachable)
	report.PageSummaries = t.pageSummaries(reachable)
	if err != nil {
		report.Error = err
		return report
	}

	if t.activeReaderCount() == 0 {
		report.LeafLinksChecked = true
		if err := t.validateLeafLinks(); err != nil {
			report.Error = err
			return report
		}
	} else {
		report.LeafLinksSkipped = true
	}
	keyCount := t.countReachableKeys(t.root, map[PageID]bool{})
	if keyCount != t.length {
		report.Error = fmt.Errorf("%w: length %d does not match reachable key count %d", ErrTreeInvariant, t.length, keyCount)
		return report
	}
	report.Error = t.validateReusablePages(reachable)
	if report.Error == nil {
		report.Stats = statsFor(t)
	}
	return report
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
	seenRetired := map[PageID]bool{}
	for _, retired := range t.retired {
		id := retired.id
		if retired.revision == 0 {
			return fmt.Errorf("%w: retired page %d has zero revision", ErrFreelist, id)
		}
		if id == 0 || id >= t.nextPage {
			return fmt.Errorf("%w: retired page %d outside reusable range [1,%d)", ErrFreelist, id, t.nextPage)
		}
		if reachable[id] {
			return fmt.Errorf("%w: retired page %d is still reachable", ErrFreelist, id)
		}
		if seenFree[id] {
			return fmt.Errorf("%w: retired page %d also appears free", ErrFreelist, id)
		}
		if seenRetired[id] {
			return fmt.Errorf("%w: retired page %d appears more than once", ErrFreelist, id)
		}
		seenRetired[id] = true
	}
	return nil
}

func (t *Tree) validateReachablePages() (map[PageID]bool, error) {
	seen := map[PageID]bool{}
	if err := t.validatePage(t.root, seen); err != nil {
		return seen, err
	}
	return seen, nil
}

func auditBaseStats(t *Tree) Stats {
	stats := Stats{
		Root:              t.root,
		Len:               t.length,
		Revision:          t.revision,
		SyncedRevision:    t.syncedRevision,
		Degree:            t.degree,
		AllocatedPages:    len(t.pages),
		RetiredPages:      len(t.retired),
		FreePages:         len(t.free),
		ActiveReaders:     t.activeReaderCount(),
		ReusedPages:       t.reusedPages,
		PageCacheCapacity: t.pageCache.snapshot().Capacity,
		Storage:           "memory",
	}
	if t.arena != nil {
		stats.Storage = "mmap"
	}
	return stats
}

func sortedPageIDs(ids map[PageID]bool) []PageID {
	out := make([]PageID, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedPageIDsFromSlice(ids []PageID) []PageID {
	out := append([]PageID(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedRetiredPageIDs(retired []retiredPage) []PageID {
	out := make([]PageID, 0, len(retired))
	for _, page := range retired {
		out = append(out, page.id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (t *Tree) pageSummaries(reachable map[PageID]bool) []PageSummary {
	summaries := make([]PageSummary, 0, len(reachable)+len(t.free)+len(t.retired))
	for _, id := range sortedPageIDs(reachable) {
		summaries = append(summaries, summarizePage(id, t.pages[id], "reachable", 0))
	}
	for _, id := range sortedPageIDsFromSlice(t.free) {
		summaries = append(summaries, summarizePage(id, t.pages[id], "free", 0))
	}
	retired := append([]retiredPage(nil), t.retired...)
	sort.Slice(retired, func(i, j int) bool { return retired[i].id < retired[j].id })
	for _, page := range retired {
		summaries = append(summaries, summarizePage(page.id, t.pages[page.id], "retired", page.revision))
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].ID != summaries[j].ID {
			return summaries[i].ID < summaries[j].ID
		}
		return summaries[i].Role < summaries[j].Role
	})
	return summaries
}

func summarizePage(id PageID, p *page, role string, retiredRevision uint64) PageSummary {
	summary := PageSummary{
		ID:              id,
		Role:            role,
		Kind:            "missing",
		RetiredRevision: retiredRevision,
	}
	if p == nil {
		return summary
	}
	summary.Kind = pageKind(p)
	summary.BytesCapacity = PageSize
	switch {
	case p.isLeaf():
		summary.Keys = int(p.slotCount())
		summary.NextPage = p.nextLeaf()
		summary.BytesUsed = p.slottedBytesUsed()
	case p.isBranch():
		summary.Separators = int(p.slotCount())
		summary.Children = p.childIDs()
		summary.BytesUsed = p.slottedBytesUsed()
	case p.isOverflow():
		summary.NextPage = p.overflowNext()
		summary.BytesUsed = p.overflowBytesUsed()
	default:
		summary.BytesUsed = PageSize
	}
	summary.BytesFree = summary.BytesCapacity - summary.BytesUsed
	return summary
}

func pageKind(p *page) string {
	switch {
	case p == nil:
		return "missing"
	case p.isLeaf():
		return "leaf"
	case p.isBranch():
		return "branch"
	case p.isOverflow():
		return "overflow"
	case p.flags()&flagFreelist != 0:
		return "freelist"
	case p.flags()&flagReclaim != 0:
		return "reclaim"
	default:
		return "unknown"
	}
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
	if err := p.validateLayoutWithComparator(t.compareKeys); err != nil {
		return err
	}
	if p.isLeaf() {
		if id != t.root && int(p.slotCount()) < minKeys(t.degree) {
			return fmt.Errorf("%w: leaf page %d has %d keys, want at least %d", ErrTreeInvariant, id, p.slotCount(), minKeys(t.degree))
		}
		for i := 0; i < int(p.slotCount()); i++ {
			key := p.readCellKey(i)
			if hasLower && t.compareKeys(key, lower) < 0 {
				return fmt.Errorf("%w: leaf page %d key %q outside branch bounds", ErrTreeInvariant, id, key)
			}
			if hasUpper && t.compareKeys(key, upper) >= 0 {
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
	if id != t.root && int(p.slotCount()) < minKeys(t.degree) {
		return fmt.Errorf("%w: branch page %d has %d keys, want at least %d", ErrTreeInvariant, id, p.slotCount(), minKeys(t.degree))
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
		if childHasLower && childHasUpper && t.compareKeys(childLower, childUpper) >= 0 {
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
