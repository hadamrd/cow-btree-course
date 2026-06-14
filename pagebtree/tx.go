package pagebtree

import "sort"

type txValue struct {
	value   []byte
	deleted bool
}

// ReadWriteTx stages mutations while exposing a read-your-writes view.
//
// It is intentionally a small transaction facade over WriteBatch: reads consult
// a staged overlay, range deletes expand against that overlay plus the current
// tree, and Commit publishes through the same copy-on-write batch path.
type ReadWriteTx struct {
	tree         *Tree
	base         *Snapshot
	baseRevision uint64
	batch        *WriteBatch
	staged       map[string]txValue
	closed       bool
}

// BeginReadWrite opens a single-use read-write transaction.
func (t *Tree) BeginReadWrite() *ReadWriteTx {
	tx := &ReadWriteTx{
		tree:   t,
		staged: map[string]txValue{},
	}
	if t != nil {
		tx.base = t.Snapshot()
		tx.baseRevision = t.Revision()
		tx.batch = t.Batch()
	}
	return tx
}

// Get returns the transaction-visible value for key.
func (tx *ReadWriteTx) Get(key string) ([]byte, bool) {
	if tx == nil || tx.closed {
		return nil, false
	}
	if value, ok := tx.staged[key]; ok {
		if value.deleted {
			return nil, false
		}
		return cloneBytes(value.value), true
	}
	if tx.base == nil {
		return nil, false
	}
	return tx.base.Get(key)
}

// Put stages an insert or replacement visible to subsequent transaction reads.
func (tx *ReadWriteTx) Put(key string, value []byte) {
	if tx == nil || tx.closed || tx.batch == nil {
		return
	}
	copied := cloneBytes(value)
	tx.batch.Put(key, copied)
	tx.staged[key] = txValue{value: copied}
}

// Delete stages a key removal visible to subsequent transaction reads.
func (tx *ReadWriteTx) Delete(key string) {
	if tx == nil || tx.closed || tx.batch == nil {
		return
	}
	tx.batch.Delete(key)
	tx.staged[key] = txValue{deleted: true}
}

// DeleteRange stages removal of keys greater than or equal to start and less
// than end in the transaction-visible view.
func (tx *ReadWriteTx) DeleteRange(start, end string) {
	if tx == nil || tx.closed || tx.batch == nil || tx.compareKeys(start, end) >= 0 {
		return
	}
	for _, key := range tx.visibleKeysBetween(start, end) {
		tx.Delete(key)
	}
}

// RangeBetween visits transaction-visible keys greater than or equal to start
// and less than end.
func (tx *ReadWriteTx) RangeBetween(start, end string, visit func(string, []byte) bool) {
	if tx == nil || tx.closed || visit == nil || tx.compareKeys(start, end) >= 0 {
		return
	}
	for _, key := range tx.visibleKeysBetween(start, end) {
		value, ok := tx.Get(key)
		if !ok {
			continue
		}
		if !visit(key, value) {
			return
		}
	}
}

func (tx *ReadWriteTx) visibleKeysBetween(start, end string) []string {
	keys := []string{}
	seen := map[string]bool{}
	if tx.base != nil {
		tx.base.RangeBetween(start, end, func(key string, value []byte) bool {
			seen[key] = true
			if staged, ok := tx.staged[key]; ok && staged.deleted {
				return true
			}
			keys = append(keys, key)
			return true
		})
	}
	for key, staged := range tx.staged {
		if staged.deleted || seen[key] {
			continue
		}
		if tx.compareKeys(key, start) >= 0 && tx.compareKeys(key, end) < 0 {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return tx.compareKeys(keys[i], keys[j]) < 0 })
	return keys
}

func (tx *ReadWriteTx) compareKeys(left, right string) int {
	if tx != nil && tx.tree != nil {
		return tx.tree.compareKeys(left, right)
	}
	if tx != nil && tx.base != nil {
		return tx.base.compareKeys(left, right)
	}
	return compareStrings(left, right)
}

// Rollback discards all staged operations.
func (tx *ReadWriteTx) Rollback() {
	if tx == nil || tx.closed {
		return
	}
	if tx.batch != nil {
		tx.batch.Rollback()
	}
	tx.close()
}

// Commit applies staged operations and returns true when the tree changed.
func (tx *ReadWriteTx) Commit() bool {
	result, err := tx.CommitDetailed()
	return err == nil && result.Changed
}

// CommitSync commits staged operations and syncs the tree when they changed it.
func (tx *ReadWriteTx) CommitSync() (bool, error) {
	result, err := tx.CommitSyncDetailed()
	return err == nil && result.Changed, err
}

// CommitDetailed applies staged operations and reports per-operation old values.
func (tx *ReadWriteTx) CommitDetailed() (BatchCommitResult, error) {
	if tx == nil || tx.closed || tx.batch == nil {
		return BatchCommitResult{}, ErrBatchClosed
	}
	defer tx.close()
	if tx.tree != nil && tx.tree.Revision() != tx.baseRevision {
		tx.batch.Rollback()
		return BatchCommitResult{}, ErrTxConflict
	}
	return tx.batch.CommitDetailed()
}

// CommitSyncDetailed applies staged operations, reports their effects, and
// syncs the tree when the commit changed it.
//
// If Sync fails, the returned BatchCommitResult still describes the logical
// commit that is visible in this process. Callers must treat the returned error
// as "durability not proven" and decide whether to close, retry Sync, or recover
// from disk.
func (tx *ReadWriteTx) CommitSyncDetailed() (BatchCommitResult, error) {
	if tx == nil {
		return BatchCommitResult{}, ErrBatchClosed
	}
	tree := tx.tree
	result, err := tx.CommitDetailed()
	if err != nil || !result.Changed {
		return result, err
	}
	if tree == nil {
		return result, ErrTreeClosed
	}
	if err := tree.Sync(); err != nil {
		return result, err
	}
	return result, nil
}

func (tx *ReadWriteTx) close() {
	if tx == nil {
		return
	}
	if tx.base != nil {
		tx.base.Close()
	}
	tx.staged = nil
	tx.closed = true
	tx.base = nil
}
