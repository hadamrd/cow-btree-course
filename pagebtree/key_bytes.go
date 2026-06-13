package pagebtree

func keyFromBytes(key []byte) string {
	return string(key)
}

func bytesFromKey(key string) []byte {
	return []byte(key)
}

// PutBytes inserts or replaces an opaque byte key.
//
// The current page format orders keys by bytewise lexicographic order. The key
// bytes are copied into an immutable internal string, so callers may reuse or
// mutate the input slice after this call returns.
func (t *Tree) PutBytes(key []byte, value []byte) ([]byte, bool) {
	return t.Put(keyFromBytes(key), value)
}

// GetBytes returns a copy of the value stored for an opaque byte key.
func (t *Tree) GetBytes(key []byte) ([]byte, bool) {
	return t.Get(keyFromBytes(key))
}

// DeleteBytes removes an opaque byte key.
func (t *Tree) DeleteBytes(key []byte) ([]byte, bool) {
	return t.Delete(keyFromBytes(key))
}

// RangeBytes visits opaque byte keys in bytewise lexicographic order.
func (t *Tree) RangeBytes(visit func([]byte, []byte) bool) {
	if visit == nil {
		return
	}
	t.Range(func(key string, value []byte) bool {
		return visit(bytesFromKey(key), value)
	})
}

// RangeBytesFrom visits opaque byte keys greater than or equal to start.
func (t *Tree) RangeBytesFrom(start []byte, visit func([]byte, []byte) bool) {
	if visit == nil {
		return
	}
	t.RangeFrom(keyFromBytes(start), func(key string, value []byte) bool {
		return visit(bytesFromKey(key), value)
	})
}

// RangeBytesBetween visits opaque byte keys greater than or equal to start and
// less than end.
func (t *Tree) RangeBytesBetween(start []byte, end []byte, visit func([]byte, []byte) bool) {
	if visit == nil {
		return
	}
	t.RangeBetween(keyFromBytes(start), keyFromBytes(end), func(key string, value []byte) bool {
		return visit(bytesFromKey(key), value)
	})
}

// PutBytes stages an insert or replacement for an opaque byte key.
func (b *WriteBatch) PutBytes(key []byte, value []byte) {
	b.Put(keyFromBytes(key), value)
}

// DeleteBytes stages removal of an opaque byte key.
func (b *WriteBatch) DeleteBytes(key []byte) {
	b.Delete(keyFromBytes(key))
}

// GetBytes returns a copy of the snapshot value for an opaque byte key.
func (s *Snapshot) GetBytes(key []byte) ([]byte, bool) {
	return s.Get(keyFromBytes(key))
}

// RangeBytes visits snapshot byte keys in bytewise lexicographic order.
func (s *Snapshot) RangeBytes(visit func([]byte, []byte) bool) {
	if visit == nil {
		return
	}
	s.Range(func(key string, value []byte) bool {
		return visit(bytesFromKey(key), value)
	})
}

// RangeBytesFrom visits snapshot byte keys greater than or equal to start.
func (s *Snapshot) RangeBytesFrom(start []byte, visit func([]byte, []byte) bool) {
	if visit == nil {
		return
	}
	s.RangeFrom(keyFromBytes(start), func(key string, value []byte) bool {
		return visit(bytesFromKey(key), value)
	})
}

// RangeBytesBetween visits snapshot byte keys greater than or equal to start
// and less than end.
func (s *Snapshot) RangeBytesBetween(start []byte, end []byte, visit func([]byte, []byte) bool) {
	if visit == nil {
		return
	}
	s.RangeBetween(keyFromBytes(start), keyFromBytes(end), func(key string, value []byte) bool {
		return visit(bytesFromKey(key), value)
	})
}

// SeekBytes positions the cursor at the first opaque byte key greater than or
// equal to key.
func (c *Cursor) SeekBytes(key []byte) bool {
	return c.Seek(keyFromBytes(key))
}

// KeyBytes returns a copy of the current opaque byte key.
func (c *Cursor) KeyBytes() []byte {
	if !c.Valid() {
		return nil
	}
	return bytesFromKey(c.key)
}
