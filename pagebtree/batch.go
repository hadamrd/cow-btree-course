package pagebtree

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
// closed, read-only, or already-closed batch is a no-op.
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
	if b == nil || b.closed {
		return false
	}
	defer func() {
		b.ops = nil
		b.closed = true
	}()
	if b.tree == nil || b.tree.closed || b.tree.readOnly || len(b.ops) == 0 {
		return false
	}

	changed := false
	for _, op := range b.ops {
		switch op.kind {
		case batchPut:
			_, _, opChanged := b.tree.putStaged(op.key, op.value)
			changed = changed || opChanged
		case batchDelete:
			_, _, opChanged := b.tree.deleteStaged(op.key)
			changed = changed || opChanged
		}
	}
	if changed {
		b.tree.publishStagedMutation()
	}
	return changed
}
