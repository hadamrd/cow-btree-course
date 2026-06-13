// Package pagebtree contains a page-backed copy-on-write B+tree-style index.
//
// It is still an educational in-memory implementation, but it models the shape
// used by storage engines more closely than package btree: pages are addressed
// by stable page ids, page bytes use a slotted layout, branch pages store
// separator keys and child page ids, leaf pages store linked key/value records,
// and overflow pages hold large values that do not fit cleanly inside a leaf
// cell. Put and Delete publish new roots through copy-on-write, while snapshots
// keep reading their older roots. Mmap-backed trees track dirty copied pages so
// Sync can flush changed data pages before publishing metadata, and they can
// grow the mapped file when allocation reaches the current capacity.
// OpenMmapReadOnly opens mmap files with a shared read lock and rejects
// mutations through the returned tree handle. Mmap-backed trees expose Advise so
// callers can pass random, sequential, or will-need access-pattern hints to the
// kernel page cache without adding a second Go heap page cache. MmapCacheStats
// uses mincore on Unix to show how many mapped OS pages are resident in that
// kernel cache.
package pagebtree
