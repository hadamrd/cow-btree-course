// Package btree contains a compact copy-on-write B-tree for learning.
//
// The implementation favors readability over maximum throughput. It uses a
// classic B-tree shape, path-copying writes, and cheap snapshots that keep an
// old root pointer alive.
package btree
