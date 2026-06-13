package btree

import (
	"cmp"
	"slices"
)

func searchNode[K cmp.Ordered, V any](n *node[K, V], key K) (V, bool) {
	for n != nil {
		index, found := slices.BinarySearch(n.keys, key)
		if found {
			return n.values[index], true
		}
		if n.leaf {
			var zero V
			return zero, false
		}
		n = n.children[index]
	}

	var zero V
	return zero, false
}

func rangeNode[K cmp.Ordered, V any](n *node[K, V], visit func(K, V) bool) bool {
	if n == nil {
		return true
	}

	for i, key := range n.keys {
		if !n.leaf && !rangeNode(n.children[i], visit) {
			return false
		}
		if !visit(key, n.values[i]) {
			return false
		}
	}

	if !n.leaf {
		return rangeNode(n.children[len(n.keys)], visit)
	}
	return true
}
