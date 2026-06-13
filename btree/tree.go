package btree

import "cmp"

// Tree is a small copy-on-write B-tree.
//
// It stores ordered keys and arbitrary values. Writes update the current tree
// root, while existing snapshots keep seeing the old root.
type Tree[K cmp.Ordered, V any] struct {
	root     *node[K, V]
	length   int
	revision uint64
	degree   int
}

// New creates an empty B-tree.
//
// degree is the classic B-tree minimum degree. Every node can hold at most
// 2*degree-1 keys. Values below 2 are normalized to 2, the smallest useful
// degree and the easiest one to draw in examples.
func New[K cmp.Ordered, V any](degree int) *Tree[K, V] {
	return &Tree[K, V]{degree: normalizeDegree(degree)}
}

func (t *Tree[K, V]) Len() int {
	return t.length
}

func (t *Tree[K, V]) Revision() uint64 {
	return t.revision
}

func (t *Tree[K, V]) Get(key K) (V, bool) {
	return searchNode(t.root, key)
}

// Set inserts or replaces a key.
//
// The returned bool reports whether the key already existed. When it is true,
// the first return value is the previous value.
func (t *Tree[K, V]) Set(key K, value V) (V, bool) {
	if t.root == nil {
		t.root = newLeaf(key, value)
		t.length = 1
		t.revision++
		var zero V
		return zero, false
	}

	root := t.root.clone()
	if root.full(t.degree) {
		root = &node[K, V]{
			leaf:     false,
			children: []*node[K, V]{root},
		}
		splitChild(root, 0, t.degree)
	}

	old, replaced := insertNonFull(root, key, value, t.degree)
	t.root = root
	if !replaced {
		t.length++
	}
	t.revision++
	return old, replaced
}

func (t *Tree[K, V]) Range(visit func(K, V) bool) {
	rangeNode(t.root, visit)
}

func (t *Tree[K, V]) Snapshot() Snapshot[K, V] {
	return Snapshot[K, V]{
		root:     t.root,
		length:   t.length,
		revision: t.revision,
		degree:   t.degree,
	}
}

func (t *Tree[K, V]) Stats() Stats {
	return statsFor(t.root, t.length, t.revision, t.degree)
}
