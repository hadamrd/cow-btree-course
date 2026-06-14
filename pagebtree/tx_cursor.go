package pagebtree

type txCursorEntry struct {
	key   string
	value []byte
}

// TxCursor is an ordered cursor over a read-write transaction view.
//
// The cursor materializes the visible range when opened. Delete stages removal
// in the transaction, while the cursor keeps walking its original view.
type TxCursor struct {
	tx      *ReadWriteTx
	entries []txCursorEntry
	index   int
	valid   bool
	closed  bool
}

// CursorBetween opens a transaction cursor over keys greater than or equal to
// start and less than end.
func (tx *ReadWriteTx) CursorBetween(start, end string) *TxCursor {
	cursor := &TxCursor{
		tx:     tx,
		index:  -1,
		closed: tx == nil || tx.closed || compareStrings(start, end) >= 0,
	}
	if cursor.closed {
		return cursor
	}
	tx.RangeBetween(start, end, func(key string, value []byte) bool {
		cursor.entries = append(cursor.entries, txCursorEntry{
			key:   key,
			value: cloneBytes(value),
		})
		return true
	})
	cursor.First()
	return cursor
}

// First positions the cursor at the first key.
func (c *TxCursor) First() bool {
	if !c.usable() || len(c.entries) == 0 {
		return c.invalidate()
	}
	c.index = 0
	c.valid = true
	return true
}

// Seek positions the cursor at the first key greater than or equal to key.
func (c *TxCursor) Seek(key string) bool {
	if !c.usable() {
		return false
	}
	for i, entry := range c.entries {
		if compareStrings(entry.key, key) >= 0 {
			c.index = i
			c.valid = true
			return true
		}
	}
	return c.invalidate()
}

// Next advances the cursor to the next key.
func (c *TxCursor) Next() bool {
	if !c.Valid() {
		return false
	}
	c.index++
	if c.index >= len(c.entries) {
		return c.invalidate()
	}
	c.valid = true
	return true
}

// Last positions the cursor at the last key.
func (c *TxCursor) Last() bool {
	if !c.usable() || len(c.entries) == 0 {
		return c.invalidate()
	}
	c.index = len(c.entries) - 1
	c.valid = true
	return true
}

// Prev moves the cursor to the previous key.
func (c *TxCursor) Prev() bool {
	if !c.Valid() {
		return false
	}
	c.index--
	if c.index < 0 {
		return c.invalidate()
	}
	c.valid = true
	return true
}

// Valid reports whether the cursor is positioned at a key.
func (c *TxCursor) Valid() bool {
	return c != nil && c.valid && c.usable()
}

// Key returns the current key, or an empty string if the cursor is invalid.
func (c *TxCursor) Key() string {
	if !c.Valid() {
		return ""
	}
	return c.entries[c.index].key
}

// Value returns a copy of the current value, or nil if the cursor is invalid.
func (c *TxCursor) Value() []byte {
	if !c.Valid() {
		return nil
	}
	return cloneBytes(c.entries[c.index].value)
}

// Delete stages removal of the current key in the transaction.
func (c *TxCursor) Delete() ([]byte, bool) {
	if !c.Valid() || c.tx == nil {
		return nil, false
	}
	entry := c.entries[c.index]
	value, ok := c.tx.Get(entry.key)
	if !ok {
		return nil, false
	}
	c.tx.Delete(entry.key)
	return value, true
}

// Close invalidates the cursor.
func (c *TxCursor) Close() {
	if c == nil || c.closed {
		return
	}
	c.closed = true
	c.invalidate()
	c.tx = nil
	c.entries = nil
}

func (c *TxCursor) usable() bool {
	return c != nil && !c.closed && c.tx != nil && !c.tx.closed
}

func (c *TxCursor) invalidate() bool {
	if c == nil {
		return false
	}
	c.valid = false
	c.index = -1
	return false
}
