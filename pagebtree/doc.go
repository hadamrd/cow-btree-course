// Package pagebtree contains a page-backed copy-on-write B+tree-style index.
//
// It is still an educational in-memory implementation, but it models the shape
// used by storage engines more closely than package btree: pages are addressed
// by stable page ids, page bytes use a slotted layout, branch pages store
// separator keys and child page ids, leaf pages store linked key/value records,
// and overflow pages hold large values that do not fit cleanly inside a leaf
// cell. Put and Delete publish new roots through copy-on-write, while snapshots
// keep reading their older roots. Leaf sibling-link repair is deferred while a
// snapshot is active, because rewriting those headers in place would mutate
// bytes visible to the old root. Current-tree Range, RangeFrom, and
// RangeBetween use those leaf links when no active reader can make them stale,
// and range scans compare slot keys before reading child ids or values so
// bounded scans do not decode cells outside the requested key range.
// Mmap-backed ranges prefetch a configurable bounded window of exact next leaf
// page ranges with MADV_WILLNEED; adjacent page ids are coalesced into one
// hint, and the window can be disabled when the caller wants to avoid even
// exact linked-leaf hints.
// Mmap-backed trees track dirty copied pages so Sync can flush changed data
// pages before publishing metadata. If the final metadata flush fails, Sync
// restores the previous mapped metadata bytes before returning the error. Mmap
// trees can grow the mapped file when allocation reaches the current capacity.
// File-size changes from growth and compaction sync the data file and parent
// directory. Compact can trim unused mapped capacity and a suffix of already-free
// page ids when no snapshot is active; if compacted metadata publication fails,
// the temporary in-memory compaction state is restored. Close returns
// ErrActiveReaders for mmap-backed trees while snapshots are active, because
// those snapshots still read slices backed by the mapping. If close-time Sync
// fails but the mmap resources are still released, the handle is marked closed
// while the sync error is returned.
// A Snapshot requested after Close is inert and does not register a reader.
// Post-close inspection and maintenance calls such as Stats, Sync, Advise,
// DropMmapCache, and MmapCacheStats do not touch the released mapping.
// Reopen validation checks metadata format, version, database page size,
// persisted degree, and bounds against the mapped file and declared capacity,
// page checksums, and slotted-page structure before decoding reachable cells,
// and it rejects root/branch reachability that points at non-tree pages,
// missing children, duplicate children, or separators that no longer match
// right-child first keys. Reachable child subtrees must also keep every key
// inside the half-open key interval assigned by their parent branch.
// Persisted leaf next pointers must match the branch-order leaf sequence.
// Overflow references must name a first page. Overflow chains must exist, must
// not loop, must contain only overflow pages, and must contain exactly the
// referenced number of payload bytes.
// Reopen also rejects metadata whose stored length does not match the reachable
// leaf-key count, plus persisted freelist IDs that exceed metadata capacity,
// are out of range, duplicated, or still reachable. Small freelists are stored
// inline in metadata; larger freelists spill to checked freelist pages that are
// synced before metadata points at them. Old freelist-page generations become
// reusable only after neither checked metadata page still names their chain.
// OpenMmapReadOnly opens mmap files with a shared read lock and rejects
// mutations through the returned tree handle. Mmap-backed trees default to
// random-access kernel advice, and expose Advise so callers can pass random,
// sequential, will-need, or normal-policy access-pattern hints to the mmap
// mapping and, on Linux, the backing file's readahead policy without adding a
// second Go heap page cache. WarmMmapTree follows the current root and overflow
// references, then asks the kernel to prefetch only those reachable page ranges.
// DropMmapCache syncs writable mmap trees before asking the kernel to evict
// clean mapped tree pages with MADV_DONTNEED and Linux file-level DONTNEED
// advice. MmapCacheStats uses mincore on Unix to show how many mapped OS pages
// are resident in that kernel cache. Current-tree Get also keeps a small
// checksum-keyed cache of decoded branch routing metadata. That derived cache
// is bounded by least-recently-used eviction and can be sized through Options
// or MmapOptions; Stats exposes its capacity, entries, hits, misses,
// invalidations, evictions, range-prefetch window, range-prefetch hint-call
// count, and exact pages covered by those hints, plus mmap warm-up hint-call
// and page counts.
package pagebtree
