// Package pagebtree contains a page-backed copy-on-write B+tree-style index.
//
// It is still an educational in-memory implementation, but it models the shape
// used by storage engines more closely than package btree: pages are addressed
// by stable page ids, page bytes use a slotted layout, branch pages store
// separator keys and child page ids, leaf pages store key/value records, and
// overflow pages hold large values that do not fit cleanly inside a leaf cell.
// Put and Delete publish new roots through copy-on-write, while snapshots keep
// reading their older roots. OpenMmapReadOnly opens mmap files with a shared
// read lock and rejects mutations through the returned tree handle.
package pagebtree
