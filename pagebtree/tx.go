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
	tree   *Tree
	batch  *WriteBatch
	staged map[string]txValue
	closed bool
}

// BeginReadWrite opens a single-use read-write transaction.
func (t *Tree) BeginReadWrite() *ReadWriteTx {
	tx := &ReadWriteTx{
		tree:   t,
		staged: map[string]txValue{},
	}
	if t != nil {
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
	if tx.tree == nil {
		return nil, false
	}
	return tx.tree.Get(key)
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
	if tx == nil || tx.closed || tx.batch == nil || compareStrings(start, end) >= 0 {
		return
	}
	for _, key := range tx.visibleKeysBetween(start, end) {
		tx.Delete(key)
	}
}

// RangeBetween visits transaction-visible keys greater than or equal to start
// and less than end.
func (tx *ReadWriteTx) RangeBetween(start, end string, visit func(string, []byte) bool) {
	if tx == nil || tx.closed || visit == nil || compareStrings(start, end) >= 0 {
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
	if tx.tree != nil {
		tx.tree.RangeBetween(start, end, func(key string, value []byte) bool {
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
		if compareStrings(key, start) >= 0 && compareStrings(key, end) < 0 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// Rollback discards all staged operations.
func (tx *ReadWriteTx) Rollback() {
	if tx == nil || tx.closed {
		return
	}
	if tx.batch != nil {
		tx.batch.Rollback()
	}
	tx.staged = nil
	tx.closed = true
}

// Commit applies staged operations and returns true when the tree changed.
func (tx *ReadWriteTx) Commit() bool {
	result, err := tx.CommitDetailed()
	return err == nil && result.Changed
}

// CommitDetailed applies staged operations and reports per-operation old values.
func (tx *ReadWriteTx) CommitDetailed() (BatchCommitResult, error) {
	if tx == nil || tx.closed || tx.batch == nil {
		return BatchCommitResult{}, ErrBatchClosed
	}
	defer func() {
		tx.staged = nil
		tx.closed = true
	}()
	return tx.batch.CommitDetailed()
}
