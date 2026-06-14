package pagebtree

// Cursor is a snapshot-backed ordered read cursor.
//
// A cursor opened from Tree owns its snapshot and releases the reader pin on
// Close. A cursor opened from Snapshot borrows that snapshot; closing the cursor
// does not close the snapshot, and the cursor becomes invalid if the snapshot is
// closed.
type Cursor struct {
	snapshot *Snapshot
	owned    bool
	pages    map[PageID]*page
	root     PageID
	stack    []cursorFrame
	key      string
	value    []byte
	lower    string
	end      string
	hasLower bool
	hasEnd   bool
	valid    bool
	closed   bool
}

type cursorFrame struct {
	id    PageID
	index int
}

// Cursor opens a read cursor over the tree's current root.
func (t *Tree) Cursor() *Cursor {
	if t == nil {
		return &Cursor{closed: true}
	}
	return newCursor(t.Snapshot(), true)
}

// CursorBetween opens a read cursor over keys greater than or equal to start
// and less than end. The returned cursor is positioned at the first key in the
// half-open interval, or invalid if the interval is empty.
func (t *Tree) CursorBetween(start, end string) *Cursor {
	if t == nil {
		return &Cursor{closed: true}
	}
	return newCursorBetween(t.Snapshot(), true, start, end)
}

// Cursor opens a read cursor over this snapshot without registering another
// reader.
func (s *Snapshot) Cursor() *Cursor {
	return newCursor(s, false)
}

// CursorBetween opens a read cursor over this snapshot's keys greater than or
// equal to start and less than end without registering another reader.
func (s *Snapshot) CursorBetween(start, end string) *Cursor {
	return newCursorBetween(s, false, start, end)
}

func newCursor(snapshot *Snapshot, owned bool) *Cursor {
	cursor := &Cursor{
		snapshot: snapshot,
		owned:    owned,
		closed:   snapshot == nil || snapshot.closed,
	}
	if snapshot != nil {
		cursor.pages = snapshot.pages
		cursor.root = snapshot.root
	}
	return cursor
}

func newCursorBetween(snapshot *Snapshot, owned bool, start, end string) *Cursor {
	cursor := newCursor(snapshot, owned)
	cursor.lower = start
	cursor.end = end
	cursor.hasLower = true
	cursor.hasEnd = true
	if start >= end {
		return cursor
	}
	cursor.First()
	return cursor
}

// First positions the cursor at the first key.
func (c *Cursor) First() bool {
	if !c.usable() {
		return false
	}
	if c.hasLower {
		return c.Seek(c.lower)
	}
	c.stack = c.stack[:0]
	return c.descendLeft(c.root)
}

// Seek positions the cursor at the first key greater than or equal to key.
func (c *Cursor) Seek(key string) bool {
	if !c.usable() {
		return false
	}
	if c.hasLower && key < c.lower {
		key = c.lower
	}
	if c.hasEnd && key >= c.end {
		return c.invalidate()
	}
	c.stack = c.stack[:0]
	for id := c.root; id != 0; {
		p := c.pages[id]
		if p == nil {
			return c.invalidate()
		}
		if p.isLeaf() {
			index, _ := p.searchSlot(key)
			c.stack = append(c.stack, cursorFrame{id: id, index: index})
			return c.loadForward()
		}
		index := branchChildIndex(p, key)
		c.stack = append(c.stack, cursorFrame{id: id, index: index})
		id = p.branchChild(index)
	}
	return c.invalidate()
}

// Next advances the cursor to the next key.
func (c *Cursor) Next() bool {
	if !c.Valid() {
		return false
	}
	last := len(c.stack) - 1
	c.stack[last].index++
	return c.loadForward()
}

// Last positions the cursor at the last key.
func (c *Cursor) Last() bool {
	if !c.usable() {
		return false
	}
	c.stack = c.stack[:0]
	if c.hasEnd {
		return c.seekBefore(c.end)
	}
	return c.descendRight(c.root)
}

// Prev moves the cursor to the previous key.
func (c *Cursor) Prev() bool {
	if !c.Valid() {
		return false
	}
	last := len(c.stack) - 1
	c.stack[last].index--
	return c.loadBackward()
}

// Valid reports whether the cursor is positioned at a key.
func (c *Cursor) Valid() bool {
	return c != nil && c.valid && c.usable()
}

// Key returns the current key, or an empty string if the cursor is invalid.
func (c *Cursor) Key() string {
	if !c.Valid() {
		return ""
	}
	return c.key
}

// Value returns a copy of the current value, or nil if the cursor is invalid.
func (c *Cursor) Value() []byte {
	if !c.Valid() {
		return nil
	}
	return cloneBytes(c.value)
}

// Close releases resources held by the cursor.
func (c *Cursor) Close() {
	if c == nil || c.closed {
		return
	}
	if c.owned && c.snapshot != nil {
		c.snapshot.Close()
	}
	c.closed = true
	c.invalidate()
	c.snapshot = nil
	c.pages = nil
	c.stack = nil
}

func (c *Cursor) usable() bool {
	return c != nil && !c.closed && c.snapshot != nil && !c.snapshot.closed
}

func (c *Cursor) descendLeft(id PageID) bool {
	for id != 0 {
		p := c.pages[id]
		if p == nil {
			return c.invalidate()
		}
		if p.isLeaf() {
			c.stack = append(c.stack, cursorFrame{id: id})
			return c.loadForward()
		}
		c.stack = append(c.stack, cursorFrame{id: id})
		id = p.leftmostChild()
	}
	return c.invalidate()
}

func (c *Cursor) descendRight(id PageID) bool {
	for id != 0 {
		p := c.pages[id]
		if p == nil {
			return c.invalidate()
		}
		if p.isLeaf() {
			c.stack = append(c.stack, cursorFrame{id: id, index: int(p.slotCount()) - 1})
			return c.loadBackward()
		}
		index := int(p.slotCount())
		c.stack = append(c.stack, cursorFrame{id: id, index: index})
		id = p.branchChild(index)
	}
	return c.invalidate()
}

func (c *Cursor) seekBefore(key string) bool {
	if c.hasLower && key <= c.lower {
		return c.invalidate()
	}
	c.stack = c.stack[:0]
	for id := c.root; id != 0; {
		p := c.pages[id]
		if p == nil {
			return c.invalidate()
		}
		index, equal := p.searchSlot(key)
		if p.isLeaf() {
			index--
			c.stack = append(c.stack, cursorFrame{id: id, index: index})
			return c.loadBackward()
		}
		childIndex := index
		if equal {
			childIndex = index
		}
		c.stack = append(c.stack, cursorFrame{id: id, index: childIndex})
		id = p.branchChild(childIndex)
	}
	return c.invalidate()
}

func (c *Cursor) loadForward() bool {
	for len(c.stack) > 0 {
		last := len(c.stack) - 1
		frame := &c.stack[last]
		p := c.pages[frame.id]
		if p == nil {
			return c.invalidate()
		}
		if p.isLeaf() {
			if frame.index < int(p.slotCount()) {
				c.loadLeafSlot(p, frame.index)
				if c.hasEnd && c.key >= c.end {
					return c.invalidate()
				}
				return true
			}
			c.stack = c.stack[:last]
			continue
		}
		frame.index++
		if frame.index <= int(p.slotCount()) {
			return c.descendLeft(p.branchChild(frame.index))
		}
		c.stack = c.stack[:last]
	}
	return c.invalidate()
}

func (c *Cursor) loadBackward() bool {
	for len(c.stack) > 0 {
		last := len(c.stack) - 1
		frame := &c.stack[last]
		p := c.pages[frame.id]
		if p == nil {
			return c.invalidate()
		}
		if p.isLeaf() {
			if frame.index >= 0 {
				c.loadLeafSlot(p, frame.index)
				if c.hasLower && c.key < c.lower {
					return c.invalidate()
				}
				return true
			}
			c.stack = c.stack[:last]
			continue
		}
		frame.index--
		if frame.index >= 0 {
			return c.descendRight(p.branchChild(frame.index))
		}
		c.stack = c.stack[:last]
	}
	return c.invalidate()
}

func (c *Cursor) loadLeafSlot(p *page, index int) {
	slot := p.readSlot(index)
	c.key = p.readCellKey(index)
	c.value = resolveLeafValue(c.pages, p.readCellValue(index), slot.flags)
	c.valid = true
}

func (c *Cursor) invalidate() bool {
	if c == nil {
		return false
	}
	c.key = ""
	c.value = nil
	c.valid = false
	return false
}
