package pagebtree

import (
	"errors"
	"fmt"
)

var (
	ErrBatchClosed = errors.New("write batch is closed")
	ErrTreeClosed  = errors.New("tree is closed")
	ErrReadOnly    = errors.New("tree is read-only")
	ErrBatchPanic  = errors.New("write batch commit panicked")
)

type batchOpKind uint8

const (
	batchPut batchOpKind = iota + 1
	batchDelete
)

type batchOp struct {
	kind  batchOpKind
	key   string
	value []byte
}

// BatchOperation identifies the staged mutation kind reported by
// BatchCommitResult.
type BatchOperation string

const (
	BatchPutOperation    BatchOperation = "put"
	BatchDeleteOperation BatchOperation = "delete"
)

// BatchOperationResult reports what one staged operation observed while Commit
// replayed the batch against the tree.
type BatchOperationResult struct {
	Kind     BatchOperation
	Key      string
	OldValue []byte
	Existed  bool
	Changed  bool
}

// BatchCommitResult reports the effect of a detailed batch commit.
type BatchCommitResult struct {
	Changed    bool
	Operations []BatchOperationResult
}

// WriteBatch stages point mutations and publishes them as one tree revision.
//
// A batch records operations until Commit. Reads on the tree keep seeing the
// old root while the batch is being built. Commit replays operations in order
// through the same copy-on-write page machinery as Put and Delete, then advances
// the tree revision once if at least one operation changed the tree.
type WriteBatch struct {
	tree   *Tree
	ops    []batchOp
	closed bool
}

// Batch opens an explicit write batch for this tree.
//
// Batches are single-use. Rollback discards staged operations. Commit on a
// closed, read-only, or already-closed batch is a no-op for Commit and an
// explicit error for CommitDetailed.
func (t *Tree) Batch() *WriteBatch {
	return &WriteBatch{tree: t}
}

// Put stages an insert or replacement.
func (b *WriteBatch) Put(key string, value []byte) {
	if b == nil || b.closed {
		return
	}
	b.ops = append(b.ops, batchOp{
		kind:  batchPut,
		key:   key,
		value: cloneBytes(value),
	})
}

// Delete stages a key removal.
func (b *WriteBatch) Delete(key string) {
	if b == nil || b.closed {
		return
	}
	b.ops = append(b.ops, batchOp{
		kind: batchDelete,
		key:  key,
	})
}

// DeleteRange stages removal of keys greater than or equal to start and less
// than end. The range is expanded against the tree visible when this method is
// called; later writes are not added to this batch automatically.
func (b *WriteBatch) DeleteRange(start, end string) {
	if b == nil || b.closed || b.tree == nil || start >= end {
		return
	}
	var keys []string
	b.tree.RangeBetween(start, end, func(key string, value []byte) bool {
		keys = append(keys, key)
		return true
	})
	for _, key := range keys {
		b.Delete(key)
	}
}

// Rollback discards all staged operations.
func (b *WriteBatch) Rollback() {
	if b == nil {
		return
	}
	b.ops = nil
	b.closed = true
}

// Commit applies staged operations and returns true when the tree changed.
func (b *WriteBatch) Commit() bool {
	result, err := b.CommitDetailed()
	return err == nil && result.Changed
}

// CommitDetailed applies staged operations and reports per-operation old values.
//
// The detailed form keeps Commit's one-revision publication semantics but also
// returns explicit errors for invalid commit attempts. If a staged mutation
// panics before the batch publishes, the tree's reachable state and dirty-page
// tracking are restored before returning ErrBatchPanic.
func (b *WriteBatch) CommitDetailed() (result BatchCommitResult, err error) {
	if b == nil || b.closed {
		return BatchCommitResult{}, ErrBatchClosed
	}
	defer func() {
		b.ops = nil
		b.closed = true
	}()
	if b.tree == nil {
		return BatchCommitResult{}, ErrTreeClosed
	}
	if b.tree.closed {
		return BatchCommitResult{}, ErrTreeClosed
	}
	if b.tree.readOnly {
		return BatchCommitResult{}, ErrReadOnly
	}
	if len(b.ops) == 0 {
		return BatchCommitResult{}, nil
	}

	state := saveBatchTreeState(b.tree)
	defer func() {
		if recovered := recover(); recovered != nil {
			state.restore(b.tree)
			result = BatchCommitResult{}
			err = fmt.Errorf("%w: %v", ErrBatchPanic, recovered)
		}
	}()

	changed := false
	for _, op := range b.ops {
		switch op.kind {
		case batchPut:
			old, existed, opChanged := b.tree.putStaged(op.key, op.value)
			result.Operations = append(result.Operations, BatchOperationResult{
				Kind:     BatchPutOperation,
				Key:      op.key,
				OldValue: cloneBytes(old),
				Existed:  existed,
				Changed:  opChanged,
			})
			changed = changed || opChanged
		case batchDelete:
			old, existed, opChanged := b.tree.deleteStaged(op.key)
			result.Operations = append(result.Operations, BatchOperationResult{
				Kind:     BatchDeleteOperation,
				Key:      op.key,
				OldValue: cloneBytes(old),
				Existed:  existed,
				Changed:  opChanged,
			})
			changed = changed || opChanged
		}
	}
	if changed {
		b.tree.publishStagedMutation()
	}
	result.Changed = changed
	return result, nil
}

type batchTreeState struct {
	pages             map[PageID]*page
	root              PageID
	nextPage          PageID
	length            int
	revision          uint64
	retired           []retiredPage
	free              []PageID
	metaFreelistRoot  PageID
	metaFreelistPages []PageID
	reusedPages       int
	dirtyPages        map[PageID]bool
}

func saveBatchTreeState(t *Tree) batchTreeState {
	state := batchTreeState{
		pages:             cloneBatchPages(t.pages),
		root:              t.root,
		nextPage:          t.nextPage,
		length:            t.length,
		revision:          t.revision,
		retired:           append([]retiredPage(nil), t.retired...),
		free:              append([]PageID(nil), t.free...),
		metaFreelistRoot:  t.metaFreelistRoot,
		metaFreelistPages: append([]PageID(nil), t.metaFreelistPages...),
		reusedPages:       t.reusedPages,
	}
	if t.arena != nil {
		state.dirtyPages = cloneBatchDirtyPages(t.arena.dirtyPages)
	}
	return state
}

func (s batchTreeState) restore(t *Tree) {
	t.pages = cloneBatchPages(s.pages)
	t.root = s.root
	t.nextPage = s.nextPage
	t.length = s.length
	t.revision = s.revision
	t.retired = append([]retiredPage(nil), s.retired...)
	t.free = append([]PageID(nil), s.free...)
	t.metaFreelistRoot = s.metaFreelistRoot
	t.metaFreelistPages = append([]PageID(nil), s.metaFreelistPages...)
	t.reusedPages = s.reusedPages
	t.pageCache = newPageCache(t.pageCache.capacity)
	if t.arena != nil {
		t.arena.dirtyPages = cloneBatchDirtyPages(s.dirtyPages)
	}
}

func cloneBatchPages(pages map[PageID]*page) map[PageID]*page {
	out := make(map[PageID]*page, len(pages))
	for id, p := range pages {
		out[id] = p
	}
	return out
}

func cloneBatchDirtyPages(dirty map[PageID]bool) map[PageID]bool {
	if dirty == nil {
		return nil
	}
	out := make(map[PageID]bool, len(dirty))
	for id, value := range dirty {
		out[id] = value
	}
	return out
}
