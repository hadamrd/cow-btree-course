// Package pagebtree contains a page-backed copy-on-write B+tree-style index.
//
// It is a research implementation of the storage-engine shape used by
// mmap-oriented systems such as OpenLDAP LMDB/MDB: pages are addressed by
// stable page ids, page bytes use a slotted layout, branch pages store separator
// keys and child page ids, leaf pages store linked key/value records, and
// overflow pages hold large values that do not fit cleanly inside a leaf cell.
// MDBKernelProfile exposes that OpenLDAP-style kernel contract for a live tree,
// including whether the tree is mmap-backed, uses checked dual metadata pages,
// owns the serialized writer path, persists reclaim records, has a reader
// table, relies on the kernel page cache for raw bytes, and only caches derived
// branch-routing metadata.
// It also exposes byte-balance policy flags: byte-aware split points, byte-aware
// delete redistribution, byte-fit merge checks, and the normalized repair-fill
// threshold used when minimum-key pages are sparsely occupied by bytes.
// The profile also reports that derived cache's capacity and live counters, so
// experiments can distinguish kernel page-cache behavior from Go-side routing
// reuse.
// Put and Delete publish new roots through copy-on-write. PutBytes, GetBytes,
// DeleteBytes, RangeBytes, CursorBytesBetween, and SeekBytes expose the same
// page format for opaque byte keys ordered by a named key order.
// Memory-backed trees can also be created with Options.KeyComparator; that
// comparator is used by search, ranges, cursors, transactions, page validation,
// and the derived branch-routing cache. Mmap trees persist named KeyOrder
// values such as bytewise and reverse bytewise, but reject arbitrary custom
// comparator closures because those functions have no durable identity.
// WriteBatch stages multiple point Put/Delete operations and half-open
// DeleteRange operations, keeps the current root hidden until Commit, then
// publishes one new revision if any staged operation changed the tree.
// BeginReadWrite opens a small read-write transaction facade over the same
// batch machinery. It adds read-your-writes Get, RangeBetween over the staged
// view, transaction-visible DeleteRange expansion, and transaction cursors that
// can stage Delete for their current key before publishing through Commit. A
// transaction reads from the tree revision where it began and CommitDetailed
// returns ErrTxConflict if that live tree revision changed before commit.
// CommitDetailed reports per-operation old values and explicit invalid commit
// errors, and restores the pre-commit tree state if a staged mutation panics
// before publication. CommitSync and CommitSyncDetailed add an explicit
// commit-then-Sync boundary for callers that want one API call to publish and
// request durability. If Sync fails, the returned commit result still describes
// the logical commit visible in the current process, but durable publication is
// not proven; batch and transaction tests cover retrying Sync after injected
// data-sync and metadata-publication faults. Snapshots and cursors keep reading
// their older roots. A cursor opened from Tree owns a snapshot and must be
// closed to release the reader pin; CursorBetween adds a half-open upper bound
// for incremental range reads, Last/Prev traverse the same snapshot in reverse,
// and Delete removes the current key from the live tree for tree-owned cursors
// while the cursor keeps reading its original snapshot. A cursor opened from
// Snapshot borrows that snapshot and does not register another reader. Leaf
// sibling-link repair is deferred while a
// snapshot or cursor is active, because rewriting those headers in place would
// mutate bytes visible to the old root.
// Current-tree Range, RangeFrom, and RangeBetween use those leaf links when no
// active reader can make them stale,
// and range scans compare slot keys before reading child ids or values so
// bounded scans do not decode cells outside the requested key range. Check
// validates the currently open tree's reachable pages, checksums, layout,
// routing invariants, non-root leaf/branch minimum fill, overflow chains,
// length, and freelist safety. It validates leaf links only when no active
// reader is delaying leaf-link repair. Audit runs that same validation path
// and returns a report with Stats plus sorted reachable, free, retired, and
// metadata page IDs, value-free page summaries with role/kind/occupancy/routing
// hints and reclaim revision bounds,
// whether leaf-link validation was checked or skipped, and the exact validation
// error if the tree is not valid.
// Mmap-backed ranges prefetch a configurable bounded window of exact next leaf
// page ranges with MADV_WILLNEED; adjacent page ids are coalesced into one
// hint, and the window can be disabled when the caller wants to avoid even
// exact linked-leaf hints.
// Mmap-backed trees track dirty copied pages so Sync can flush changed data
// pages before publishing metadata. Internal fault-injection tests cover
// before-data-sync, after-metadata-write, before-metadata-sync,
// before-growth-file-size-sync, before-growth-directory-sync, and
// before-growth-remap boundaries, plus the matching file-size/directory/remap
// boundaries for compact-driven shrink; if a metadata publication fault or the
// final metadata flush fails, Sync restores the previous mapped metadata bytes
// before returning the error. MmapOptions.TraceHook can observe sync phases,
// timed dirty data-page sync ranges, sync failures, recovery candidate
// rejection/acceptance, growth and compact remap success/failure geometry,
// sparse-hole punch begin/range/end/failure decisions, freelist/reclaim
// metadata rollback, stale reader cleanup, and obsolete metadata-page reclaim
// decisions as structured events.
// MmapTraceJSONLExporter adapts those events to newline-delimited JSON for
// experiments. MmapTraceAsyncJSONLExporter adds a bounded background queue when
// an experiment must keep trace I/O off the storage hook and can tolerate
// explicit drop accounting. Mmap trees can grow the mapped file when allocation
// reaches the current capacity.
// New database creation and file-size changes from growth and compaction sync
// the data file and parent directory. Compact can trim unused mapped capacity
// and a suffix of already-free page ids when no snapshot is active; if
// compacted metadata publication fails, the temporary in-memory compaction state
// is restored. PunchFreeMmapPages is an experimental sparse-file maintenance
// hook for supported platforms: it asks the filesystem to deallocate already
// reusable mmap pages without changing file length or removing those page IDs
// from the freelist, and it skips free pages that a still-valid fallback
// metadata root could recover; trace events expose attempted ranges, aggregate
// counts, skipped fallback-recoverable pages, and failure reasons.
// MmapHolePunchProfile reports the build's sparse-hole primitive or unsupported
// reason before running an experiment. InspectMmapRecovery reports value-free
// accepted/rejected checked-metadata recovery candidates without claiming a
// reader slot. Close returns
// ErrActiveReaders for mmap-backed trees while snapshots are active, because
// those snapshots still read slices backed by the mapping. If close-time Sync
// fails but the mmap resources are still released, the handle is marked closed
// while the sync error is returned.
// A Snapshot requested after Close is inert and does not register a reader.
// Post-close inspection and maintenance calls such as Stats, Sync, Advise,
// DropMmapCache, MmapCacheStats, MmapSpaceStats, MmapReaderStats, and
// CleanStaleMmapReaders do not touch the released mapping.
// Reopen validation checks metadata format, version, database page size,
// persisted degree, and bounds against the mapped file and declared capacity,
// page checksums, and slotted-page structure before decoding reachable cells.
// The same reachable-page validator backs Check, and it rejects root/branch
// reachability that points at non-tree pages, missing children, duplicate
// children, or separators that no longer match right-child first keys. Reachable
// child subtrees must also keep every key inside the half-open key interval
// assigned by their parent branch. Non-root leaves and branches must contain at
// least degree-1 keys.
// Persisted leaf next pointers must match the branch-order leaf sequence.
// Overflow references must name a first page. Overflow chains must exist, must
// not loop, must contain only overflow pages, and must contain exactly the
// referenced number of payload bytes.
// Reopen also rejects metadata whose stored length does not match the reachable
// leaf-key count, plus persisted freelist IDs that exceed metadata capacity,
// are out of range, duplicated, or still reachable. Small freelists are stored
// inline in metadata; larger freelists spill to checked freelist pages that are
// synced before metadata points at them. When external readers pin retired pages,
// version-3 metadata stores free and pending-retired reclaim records in checked
// reclaim pages, preserving the retirement revision across writer close/reopen.
// Reopen rejects reclaim records whose retired revision is zero or newer than
// the metadata revision that references them, and rejects a reclaim root with
// zero reclaim records.
// A writable handle that accepts an older metadata version rewrites it through
// the normal dual-metadata Sync path; if no logical mutation happened, the
// format upgrade advances one revision so the current writer format lands in
// the alternate metadata slot.
// Old freelist/reclaim-page generations become reusable only after neither
// checked metadata page still names their chain.
// OpenMmap uses a sidecar writer mutex so only one writer can publish at a time.
// OpenMmapReadOnly opens mmap files with a shared read lock, claims a
// provisional revision-0 reader-table slot before metadata recovery, updates the
// slot to the recovered revision, validates existing live slots against that
// recovered revision, and rejects mutations through the returned tree handle.
// Writable OpenMmap also validates live reader slots after recovering metadata
// from an existing file. Writers combine those reader slots with in-process
// snapshots before recycling retired pages, so read-only mmap handles can
// coexist with a writer while pinning old copy-on-write pages. Version-3 reader
// slots also store process-start and boot/session tokens when the platform
// exposes them, reducing PID-reuse and reboot/session ambiguity during stale-slot
// cleanup; version-1 and version-2 sidecars remain readable.
// InspectMmapLockStats checks the writer sidecar without opening a writer and
// reports whether the mutex file exists and whether a non-blocking exclusive
// lock attempt observed active writer contention. MmapReaderStats reports live,
// stale, process-start-tagged, and boot-tagged reader-table slots,
// InspectMmapReaderStats validates slots against the recovered usable root
// revision without claiming an observer slot, and CleanStaleMmapReaders clears
// slots owned by dead or detectably reused owners. Existing malformed
// reader-table sidecars and live
// slots with impossible future revisions or zero claim tokens return
// ErrReaderTable instead of being reset, because resetting them could forget
// active reader watermarks; writer reclaim treats reader-table scan errors as a
// conservative pin on all retired pages. Stats.ReclaimPressure exposes the same
// watermark decision by separating retired pages pinned by readers from retired
// pages immediately eligible for reuse.
// Writable mmap handles can close while external reader-table slots still pin
// retired pages because those pending retired records are published in metadata;
// Close still returns ErrActiveReaders while in-process snapshots are active
// because they hold slices into the mapping. Mmap-backed trees default to
// random-access kernel advice, and expose Advise so callers can pass random,
// sequential, will-need, or normal-policy access-pattern hints to the mmap
// mapping and, on Linux, the backing file's readahead policy without adding a
// second Go heap page cache. WarmMmapTree follows the current root and overflow
// references, then asks the kernel to prefetch only those reachable page ranges.
// DropMmapCache syncs writable mmap trees before asking the kernel to evict
// clean mapped tree pages with MADV_DONTNEED and Linux file-level DONTNEED
// advice. MmapCacheStats uses mincore on Unix to show how many mapped OS pages
// are resident in that kernel cache. MmapSpaceStats uses stat(2) block counts
// on Unix to compare logical file bytes with filesystem-reported allocated
// bytes and statfs(2)-backed filesystem/mount identity where available for
// sparse-file experiments. Current-tree Get also keeps a small
// checksum-keyed cache of decoded branch routing metadata. That derived cache
// is bounded by least-recently-used eviction and can be sized through Options
// or MmapOptions; Stats exposes its capacity, entries, hits, misses,
// invalidations, evictions, range-prefetch window, range-prefetch hint-call
// count, and exact pages covered by those hints, plus mmap warm-up hint-call
// and page counts. Stats also reports logical Revision versus SyncedRevision,
// the last revision for which Sync returned successfully, reclaim-pressure
// counters, plus reachable leaf,
// branch, and overflow page counts, used/free/capacity byte counts, and the
// normalized repair-fill policy. Insertion and delete
// redistribution use encoded cell byte footprints to choose leaf and branch
// split points. Leaf and branch repair can also trigger on configurable low
// byte occupancy at the minimum key count, and merge decisions require the
// combined encoded page bytes to fit in one page.
package pagebtree
