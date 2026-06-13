package btree

import (
	"cmp"
	"slices"
)

func insertNonFull[K cmp.Ordered, V any](n *node[K, V], key K, value V, degree int) (V, bool) {
	index, found := slices.BinarySearch(n.keys, key)
	if found {
		old := n.values[index]
		n.values[index] = value
		return old, true
	}

	if n.leaf {
		insertAt(&n.keys, index, key)
		insertAt(&n.values, index, value)
		var zero V
		return zero, false
	}

	n.children[index] = n.children[index].clone()
	if n.children[index].full(degree) {
		splitChild(n, index, degree)

		switch {
		case key == n.keys[index]:
			old := n.values[index]
			n.values[index] = value
			return old, true
		case key > n.keys[index]:
			index++
		}
	}

	return insertNonFull(n.children[index], key, value, degree)
}

func splitChild[K cmp.Ordered, V any](parent *node[K, V], childIndex int, degree int) {
	left := parent.children[childIndex]
	medianIndex := degree - 1

	medianKey := left.keys[medianIndex]
	medianValue := left.values[medianIndex]

	right := &node[K, V]{
		leaf:   left.leaf,
		keys:   append([]K(nil), left.keys[medianIndex+1:]...),
		values: append([]V(nil), left.values[medianIndex+1:]...),
	}
	left.keys = left.keys[:medianIndex]
	left.values = left.values[:medianIndex]

	if !left.leaf {
		right.children = append([]*node[K, V](nil), left.children[medianIndex+1:]...)
		left.children = left.children[:medianIndex+1]
	}

	insertAt(&parent.keys, childIndex, medianKey)
	insertAt(&parent.values, childIndex, medianValue)
	insertAt(&parent.children, childIndex+1, right)
}

func insertAt[T any](values *[]T, index int, value T) {
	*values = append(*values, value)
	copy((*values)[index+1:], (*values)[index:])
	(*values)[index] = value
}
