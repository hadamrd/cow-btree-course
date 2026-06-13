// Package pagebtree contains a page-backed copy-on-write B-tree.
//
// It is still an educational in-memory implementation, but it models the shape
// used by storage engines more closely than package btree: nodes are addressed
// by stable page ids, writes allocate copied pages, and snapshots keep reading
// through their captured root page id.
package pagebtree
