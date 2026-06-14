package pagebtree

import "errors"

var (
	ErrFreelist      = errors.New("freelist invalid")
	ErrActiveReaders = errors.New("tree has active readers")
)

type retiredPage struct {
	id       PageID
	revision uint64
}

func (t *Tree) beginRead(revision uint64) {
	if t.activeReaders == nil {
		t.activeReaders = map[uint64]int{}
	}
	t.activeReaders[revision]++
}

func (t *Tree) endRead(revision uint64) {
	if t.activeReaders == nil {
		return
	}

	count := t.activeReaders[revision]
	switch {
	case count <= 1:
		delete(t.activeReaders, revision)
	default:
		t.activeReaders[revision] = count - 1
	}
	t.reclaimRetiredPages()
}

func (t *Tree) retirePage(id PageID) {
	t.retired = append(t.retired, retiredPage{
		id:       id,
		revision: t.revision,
	})
}

func (t *Tree) reclaimRetiredPages() {
	if len(t.retired) == 0 {
		return
	}

	oldest, hasReader := t.oldestReaderRevision()
	kept := t.retired[:0]
	for _, retired := range t.retired {
		if hasReader && oldest <= retired.revision {
			kept = append(kept, retired)
			continue
		}
		t.free = append(t.free, retired.id)
	}
	t.retired = kept
}

func (t *Tree) oldestReaderRevision() (uint64, bool) {
	oldest, found, err := t.oldestReaderRevisionDetailed()
	if err != nil {
		return 0, true
	}
	return oldest, found
}

func (t *Tree) oldestReaderRevisionDetailed() (uint64, bool, error) {
	var oldest uint64
	found := false
	for revision := range t.activeReaders {
		if !found || revision < oldest {
			oldest = revision
			found = true
		}
	}
	if t.arena != nil && t.arena.readerTable != nil {
		revision, hasReader, err := t.arena.readerTable.oldest(t.revision)
		if err != nil {
			return 0, true, err
		}
		if hasReader && (!found || revision < oldest) {
			oldest = revision
			found = true
		}
	}
	return oldest, found, nil
}

func (t *Tree) activeReaderCount() int {
	total := 0
	for _, count := range t.activeReaders {
		total += count
	}
	return total
}
