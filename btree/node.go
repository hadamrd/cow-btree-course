package btree

import "cmp"

// node is the private building block of the tree.
//
// A node is never mutated while it is reachable from an older Snapshot. Before a
// write changes a node, Set clones every node on the search path from the root
// down to the target leaf. Untouched children remain shared.
type node[K cmp.Ordered, V any] struct {
	leaf     bool
	keys     []K
	values   []V
	children []*node[K, V]
}

func newLeaf[K cmp.Ordered, V any](key K, value V) *node[K, V] {
	return &node[K, V]{
		leaf:   true,
		keys:   []K{key},
		values: []V{value},
	}
}

func (n *node[K, V]) clone() *node[K, V] {
	if n == nil {
		return nil
	}
	out := &node[K, V]{
		leaf:     n.leaf,
		keys:     append([]K(nil), n.keys...),
		values:   append([]V(nil), n.values...),
		children: append([]*node[K, V](nil), n.children...),
	}
	return out
}

func (n *node[K, V]) full(degree int) bool {
	return len(n.keys) == maxKeys(degree)
}

func maxKeys(degree int) int {
	return 2*degree - 1
}

func normalizeDegree(degree int) int {
	if degree < 2 {
		return 2
	}
	return degree
}
