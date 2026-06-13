package pagebtree

import "slices"

func (t *Tree) insertNonFull(pageID PageID, key string, value []byte) ([]byte, bool) {
	p := t.pages[pageID]
	index, found := slices.BinarySearch(p.keys, key)
	if found {
		old := cloneBytes(p.values[index])
		p.values[index] = cloneBytes(value)
		return old, true
	}

	if p.leaf {
		insertAt(&p.keys, index, key)
		insertAt(&p.values, index, cloneBytes(value))
		return nil, false
	}

	copiedChildID := t.copyPage(p.children[index])
	p.children[index] = copiedChildID

	if t.pages[copiedChildID].full(t.degree) {
		t.splitChild(pageID, index)

		switch {
		case key == p.keys[index]:
			old := cloneBytes(p.values[index])
			p.values[index] = cloneBytes(value)
			return old, true
		case key > p.keys[index]:
			index++
		}
	}

	return t.insertNonFull(p.children[index], key, value)
}

func (t *Tree) splitChild(parentID PageID, childIndex int) {
	parent := t.pages[parentID]
	left := t.pages[parent.children[childIndex]]
	medianIndex := t.degree - 1

	medianKey := left.keys[medianIndex]
	medianValue := cloneBytes(left.values[medianIndex])

	rightID := t.allocPage()
	right := &page{
		id:     rightID,
		leaf:   left.leaf,
		keys:   append([]string(nil), left.keys[medianIndex+1:]...),
		values: cloneValues(left.values[medianIndex+1:]),
	}
	t.pages[rightID] = right

	left.keys = left.keys[:medianIndex]
	left.values = left.values[:medianIndex]

	if !left.leaf {
		right.children = append([]PageID(nil), left.children[medianIndex+1:]...)
		left.children = left.children[:medianIndex+1]
	}

	insertAt(&parent.keys, childIndex, medianKey)
	insertAt(&parent.values, childIndex, medianValue)
	insertAt(&parent.children, childIndex+1, rightID)
}

func insertAt[T any](values *[]T, index int, value T) {
	*values = append(*values, value)
	copy((*values)[index+1:], (*values)[index:])
	(*values)[index] = value
}
