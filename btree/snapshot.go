package btree

import "cmp"

// Snapshot is a read-only view of a tree revision.
//
// Snapshots are cheap: they hold only a root pointer plus metadata. Copy-on-write
// updates make that pointer stable because later writes clone the path they
// change instead of mutating nodes reachable from the snapshot.
type Snapshot[K cmp.Ordered, V any] struct {
	root     *node[K, V]
	length   int
	revision uint64
	degree   int
}

func (s Snapshot[K, V]) Len() int {
	return s.length
}

func (s Snapshot[K, V]) Revision() uint64 {
	return s.revision
}

func (s Snapshot[K, V]) Get(key K) (V, bool) {
	return searchNode(s.root, key)
}

func (s Snapshot[K, V]) Range(visit func(K, V) bool) {
	rangeNode(s.root, visit)
}

func (s Snapshot[K, V]) Stats() Stats {
	return statsFor(s.root, s.length, s.revision, s.degree)
}
