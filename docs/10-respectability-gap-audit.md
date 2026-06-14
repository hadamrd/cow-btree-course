# Respectability Gap Audit

This is the blunt gap list against a more respectable storage-engine bar. The
project is already useful as a research kernel, but a serious reader should not
mistake it for a production database.

## Current Position

What is credible today:

- Fixed-size slotted pages with checksums and layout validation.
- B+tree page search using branch separator keys and child page IDs.
- Copy-on-write page updates with stable snapshots.
- mmap-backed storage with dirty data-before-metadata sync.
- Dual checked metadata pages and newest-first recovery fallback.
- Reader-pinned page recycling with in-process snapshots and mmap reader-table
  watermarks.
- Persisted freelist/reclaim metadata.
- Overflow page chains with validation.
- Kernel page-cache cooperation through mmap advice, file advice on Linux,
  exact warm-up, exact range prefetch, and cache-residency stats.
- Offline mmap copy compaction that copies live records into a fresh smaller
  file without overwriting existing destination artifacts, plus a guarded
  offline replacement path that rejects active readers and writers before
  renaming the compact file into place.
- A bounded derived branch-routing cache that does not duplicate raw page bytes.
- Opt-in mmap trace events for sync phases and failures, recovery candidate
  accept/reject decisions, growth and compact remap success/failure geometry,
  freelist/reclaim metadata rollback, stale reader cleanup, and obsolete
  metadata-page reclaim decisions.
- Snapshot-backed cursors for incremental ordered reads.
- A persisted mmap key-order identifier for the current bytewise page ordering.
- Reproducible Go microbenchmarks for page and mmap get, seek/next, range,
  insert, delete, reopen, and sync paths.

## P0 Gaps

These are the gaps that most separate the project from a genuinely serious
storage-engine artifact.

| Gap | Why it matters | Current state | Next useful slice |
| --- | --- | --- | --- |
| Crash fault injection | Recovery code is only respectable when tested at every publish boundary. | Started: the internal rollback matrix covers sync, growth, and compact-shrink boundaries. The copied-image crash harness now classifies sync-publication, growth, compact-shrink, large-freelist spill, large-reclaim spill, and obsolete metadata-generation reclaim images. `TestMmapSyncProcessCrashMatrixClassifiesRecoveryRoot` kills a child writer at sync-publication fault points and reopens the same database from a fresh process. `TestMmapGrowthProcessCrashMatrixClassifiesOldRoot` does the same for growth file-size, directory-sync, and pre-remap fault points, all recovering the old root. `TestMmapCompactShrinkProcessCrashMatrixClassifiesCompactedRoot` does the same for compact-shrink file-size, directory-sync, and pre-remap fault points, reopening live keys from the compacted root. `TestMmapLargeFreelistProcessCrashMatrixClassifiesRecoveryRoot` does the same for large-freelist spill publication, preserving old metadata before data sync and persisted reusable-page records after metadata write. `TestMmapLargeReclaimProcessCrashMatrixClassifiesReaderPinnedRetiredPages` keeps a live read-only parent handle while killing the child writer at large-reclaim publication points, proving persisted retired records remain pinned by a surviving reader watermark. | True power-fail testing is still outside the local harness. |
| Transaction batching | Real engines commit a unit of work, not one implicit root publish per call. | Started: `WriteBatch` stages point `Put`/`Delete` operations, hides them until `Commit`, and publishes one tree revision across memory and mmap trees. `CommitDetailed` reports per-operation old values, returns explicit invalid-commit errors, and restores the pre-commit tree state if staged replay panics before publication. `Sync` remains the mmap durability boundary. | Add cursor/range-aware write experiments and a fuller ACID transaction boundary. |
| Cursor API | Real B+tree users need `seek`/`next` control, not only callback scans. | Closed in this pass with snapshot-backed forward cursors. | Extend cursors with bounded end keys, reverse traversal, and delete-through-cursor experiments. |
| Comparator and key model | Production B+trees need an explicit key-ordering contract before prefix compression, duplicate keys, or locale-aware indexes. | Started: page cells store byte strings and compare bytewise; public `PutBytes`/`GetBytes`/`DeleteBytes`/`RangeBytes`/cursor byte-key APIs expose opaque byte keys; mmap metadata persists the bytewise key-order identifier and rejects unknown requested or persisted orders; `pagebtree/testdata/mmap-v2-legacy-zero-key-order.db` proves reopen compatibility with the pre-key-order metadata word. | Add a real pluggable comparator boundary, broader old-format fixture coverage, and a page-format path for prefix compression. |
| Fuzz and model checking | Handwritten tests miss malformed-page combinations and delete/split corner cases. | Started: `FuzzPageTreeMatchesSortedMapModel` compares `pagebtree` with a sorted-map oracle across put, delete, batch, get, range, cursor, and `Check` operations. `FuzzMmapTreeMatchesSortedMapModelAcrossReopen` adds mmap sync/close/reopen cycles and overflow-heavy values. `FuzzMmapMalformedPageGeneratorRejectsOrChecks` mutates mmap file bytes, metadata, page headers, checksums, truncation, and tree/overflow-bearing pages, then requires any accepted image to pass `Check`. | Extend model checking to longer process-crash/reopen probes, minimized malformed-page corpora, and semantic corruption oracles. |

## P1 Gaps

These are not all needed for the next commit, but they define the serious
research frontier.

| Gap | Why it matters | Current state | Next useful slice |
| --- | --- | --- | --- |
| Byte-balanced pages | Variable-size records make key-count balancing weak. | Started: `Stats` now reports reachable leaf/branch/overflow page counts plus total and per-kind used bytes, free bytes, and capacity; insertion and delete redistribution choose legal leaf/branch split points by encoded cell byte footprint, and large inline values can spill to overflow pages. Underfull detection and merge decisions are still degree/key-count based. | Use byte occupancy thresholds to trigger repair and choose merge/redistribution. |
| Prefix compression | Interior separators and leaf keys waste space without compression. | Keys are stored plainly in every cell. | Add an optional page-local prefix-compressed leaf format version. |
| Online vacuum / page relocation | Tail compaction cannot reclaim interior holes to the filesystem. | Started: `Compact` trims only a contiguous free suffix and never moves live pages; `CopyCompactMmap` copies live records into a fresh compact mmap tree; `CompactMmapFile` takes the writer mutex, rejects active mmap readers via exclusive source-file lock, resets the reader sidecar, and renames the compact file into place. | Attempt online page relocation or sparse-hole experiments. |
| Sparse-file punching | Reusable interior pages remain allocated by the filesystem. | Interior free pages stay inside the file. | Experiment with hole punching for page-size-aligned free extents while preserving mmap semantics. |
| Multi-process robustness | Reader tables need stronger owner identity than PID alone. | Slots use PID, revision, and token, with stale PID cleanup and fail-closed validation. | Include boot/session identity or start time to reduce PID reuse ambiguity. |
| Observability | A serious engine should explain stalls, reclaim pressure, and recovery decisions. | Started: `MmapOptions.TraceHook` emits structured `MmapTraceEvent` records for sync phases, timed per-range dirty data-page flushes, sync failures, recovery candidate accept/reject decisions, growth and compact remap success/failure geometry, freelist/reclaim metadata rollback page spans, stale reader cleanup counts, and obsolete metadata-page reclaim decisions. `MmapTraceJSONLExporter` and `cmd/mmaptrace-demo` provide a small JSONL export path for experiments. Stats still expose counters, and `MmapCacheStats` exposes kernel residency. | Add workload trace examples, trace redaction guidance, and optional asynchronous export experiments. |
| Benchmarks | Without benchmarks, optimization claims are weak. | Started: `pagebtree/bench_test.go` covers page and mmap point `Get`, cursor seek/next, bounded range scans, sequential insert/delete, mmap sync-after-put, mmap delete+sync, and mmap reopen validation. | Add benchstat comparison scripts, larger workload profiles, and CI/manual baseline guidance. |

## P2 Gaps

These make the project more complete, but they should follow the P0/P1 work.

| Gap | Why it matters | Current state | Next useful slice |
| --- | --- | --- | --- |
| Multi-database catalog | LMDB-style environments commonly hold named databases. | One tree per file. | Add a catalog page after the single-tree kernel is more proven. |
| Duplicate keys and cursors | Many B+tree APIs need duplicate-key support. | Keys are unique. | Model duplicates as sorted duplicate records or subpages. |
| Durable metadata evolution | Format upgrades need explicit compatibility tests. | Metadata versions exist, but upgrade testing is narrow. | Add old-format fixture files and upgrade/reopen tests. |
| Platform matrix | mmap behavior differs across Unix variants and non-Unix platforms. | Unix path is primary; non-Unix has stubs. | Add CI matrix notes and compile-only checks for key platforms. |

## Gap Closed In This Pass: Cursor API

The new cursor is intentionally forward-only and snapshot-backed:

- `Tree.Cursor()` opens a cursor over the current root and owns the snapshot
  reader pin.
- `Snapshot.Cursor()` borrows an existing snapshot and does not register another
  reader.
- `First()` positions at the first key.
- `Seek(key)` positions at the first key greater than or equal to `key`.
- `Next()` advances one key at a time.
- `Key()` and `Value()` expose the current record, with `Value()` returning a
  copy.
- `Close()` releases the owned snapshot for tree cursors.

The implementation keeps a branch/leaf path stack. It does not materialize the
whole range before iteration.

Code to read:

- Cursor implementation: [`../pagebtree/cursor.go`](../pagebtree/cursor.go)
- Cursor behavior tests: [`../pagebtree/cursor_test.go`](../pagebtree/cursor_test.go)
- mmap cursor reader-pinning test: [`../pagebtree/mmap_test.go`](../pagebtree/mmap_test.go)

## Gap Advanced In This Pass: Offline Copy Compaction

The new copy-compaction path is deliberately offline:

- `CopyCompactMmap(src, dst, options)` opens `src` through `OpenMmapReadOnly`.
- It validates the recovered source root with `Check`.
- It refuses to run if the destination file, destination `.writer` sidecar, or
  destination `.readers` sidecar already exists.
- It copies live records through `RangeBytes` into a new mmap tree.
- It validates and tail-compacts the destination before returning file-size and
  mapped-page statistics.
- `cmd/mmapcopycompact` exposes the same path for experiments.
- `CompactMmapFile(path, options)` adds the guarded replacement workflow:
  source writer mutex, compact temp file, exclusive source-file lock to reject
  active readers, reader-sidecar reset, database-file rename, and directory sync.
- `cmd/mmapcompact` exposes the replacement path.

This closes the first useful step toward vacuuming: interior free pages can be
removed from a replacement file and that file can now be swapped in while
refusing active readers/writers. It does not yet solve online relocation or
sparse-hole punching inside the active database.

Code to read:

- API and result contract: [`../pagebtree/copy_compact.go#L15-L104`](../pagebtree/copy_compact.go#L15-L104)
- Destination safety checks: [`../pagebtree/copy_compact.go#L106-L142`](../pagebtree/copy_compact.go#L106-L142)
- Guarded replacement path: [`../pagebtree/compact_replace_unix.go#L12-L70`](../pagebtree/compact_replace_unix.go#L12-L70)
- CLI wrapper: [`../cmd/mmapcopycompact/main.go#L10-L25`](../cmd/mmapcopycompact/main.go#L10-L25)
- Replacement CLI wrapper: [`../cmd/mmapcompact/main.go#L10-L25`](../cmd/mmapcompact/main.go#L10-L25)
- Copy tests: [`../pagebtree/mmap_test.go#L1993-L2086`](../pagebtree/mmap_test.go#L1993-L2086)
- Replacement tests: [`../pagebtree/mmap_test.go#L2088-L2196`](../pagebtree/mmap_test.go#L2088-L2196)

## Recommended Next Grind

The next most valuable slice is continuing crash fault injection. The project
now has internal fault points before dirty data sync, after metadata write,
before metadata sync, before growth file-size sync, before growth directory
sync, before growth remap, and the same file-size/directory/remap boundaries for
compact-driven shrink. The sync-publish matrix proves failed publishes reopen
on the old durable root and, when metadata bytes had already been encoded, roll
back mapped metadata bytes before returning. The growth matrix proves failed
file-size/directory/remap boundaries preserve the old mapping, old capacity, old
file size, and old durable root. The compact-shrink matrix proves failed
file-size/directory/remap boundaries preserve the readable old mapping and
restore the physical file size before returning. The copied-image sync matrix
now snapshots the database file at injected sync-publication boundaries, reopens
the copied file through a fresh mmap handle, and classifies the recovery point:
before-data-sync recovers the old root, while after-metadata-write and
before-metadata-sync recover the new root. The copied-image growth matrix proves
that file-size, directory-sync, and pre-remap growth images still recover the old
durable root because growth never publishes metadata. The copied-image
compact-shrink matrix proves that file-size, directory-sync, and pre-remap
shrink images reopen the compacted root and preserve all live keys, because
compaction publishes metadata before the physical shrink. The copied-image
large-freelist matrix proves that a pre-data-sync image keeps the old metadata
with no reusable pages, while after-metadata-write and before-metadata-sync
images reopen the spilled freelist and can reuse those pages. The copied-image
large-reclaim matrix preserves a copied reader-table sidecar, proving that a
pre-data-sync image keeps the old metadata with no retired pages, while
after-metadata-write and before-metadata-sync images reopen the checked reclaim
chain and keep retired pages pinned behind the copied reader watermark. The
obsolete-generation matrix proves that an image taken while an older metadata
slot still references a metadata freelist chain does not recycle that chain, and
an image taken after both metadata slots advance recomputes the obsolete chain
as reusable during recovery. The process-exit sync matrix kills a child writer
at the same sync-publication fault points, then reopens the same database from a
fresh process and classifies old-root versus new-root recovery. The process-exit
growth matrix applies the same child-process pattern to file-size,
directory-sync, and pre-remap growth boundaries, all of which recover the old
root because growth does not publish metadata. The process-exit compact-shrink
matrix kills a child writer at file-size, directory-sync, and pre-remap shrink
boundaries, then reopens all live keys from the compacted root and checks the
compacted file geometry. The large-freelist process-exit matrix covers
old-metadata recovery before data sync and persisted freelist reuse after
metadata write or before metadata sync. The large-reclaim process-exit matrix
keeps a live read-only parent handle, kills the child writer at large-reclaim
publication points, and verifies that persisted retired records remain pinned
by that surviving reader after writable recovery and a follow-up write. The
remaining crash-work is true power-fail probing outside the local harness.
