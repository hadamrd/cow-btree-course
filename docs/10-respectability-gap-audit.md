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
- A bounded derived branch-routing cache that does not duplicate raw page bytes.
- Snapshot-backed cursors for incremental ordered reads.

## P0 Gaps

These are the gaps that most separate the project from a genuinely serious
storage-engine artifact.

| Gap | Why it matters | Current state | Next useful slice |
| --- | --- | --- | --- |
| Crash fault injection | Recovery code is only respectable when tested at every publish boundary. | Started: the internal rollback matrix covers sync, growth, and compact-shrink boundaries. The copied-image crash harness now classifies sync-publication, growth, compact-shrink, large-freelist spill, large-reclaim spill, and obsolete metadata-generation reclaim images. `TestMmapSyncProcessCrashMatrixClassifiesRecoveryRoot` kills a child writer at sync-publication fault points and reopens the same database from a fresh process. `TestMmapGrowthProcessCrashMatrixClassifiesOldRoot` does the same for growth file-size, directory-sync, and pre-remap fault points, all recovering the old root. `TestMmapCompactShrinkProcessCrashMatrixClassifiesCompactedRoot` does the same for compact-shrink file-size, directory-sync, and pre-remap fault points, reopening live keys from the compacted root. | Extend process-level crash/reopen probes to freelist and reclaim paths; true power-fail testing is still outside the local harness. |
| Transaction batching | Real engines commit a unit of work, not one implicit root publish per call. | Started: `WriteBatch` stages point `Put`/`Delete` operations, hides them until `Commit`, and publishes one tree revision across memory and mmap trees. `Sync` remains the mmap durability boundary. | Add richer transaction ergonomics: old-value reporting, explicit errors, panic-safe rollback, and cursor/range-aware write experiments. |
| Cursor API | Real B+tree users need `seek`/`next` control, not only callback scans. | Closed in this pass with snapshot-backed forward cursors. | Extend cursors with bounded end keys, reverse traversal, and delete-through-cursor experiments. |
| Comparator and key model | Production B+trees cannot be hardwired to Go string ordering. | Page cells store strings and compare byte-by-byte through string order. | Introduce byte-key APIs and an explicit comparator boundary before adding prefix compression. |
| Fuzz and model checking | Handwritten tests miss malformed-page combinations and delete/split corner cases. | Started: `FuzzPageTreeMatchesSortedMapModel` compares `pagebtree` with a sorted-map oracle across put, delete, batch, get, range, cursor, and `Check` operations. `FuzzMmapTreeMatchesSortedMapModelAcrossReopen` adds mmap sync/close/reopen cycles and overflow-heavy values. `FuzzMmapMalformedPageGeneratorRejectsOrChecks` mutates mmap file bytes, metadata, page headers, checksums, truncation, and tree/overflow-bearing pages, then requires any accepted image to pass `Check`. | Extend model checking to longer process-crash/reopen probes, minimized malformed-page corpora, and semantic corruption oracles. |

## P1 Gaps

These are not all needed for the next commit, but they define the serious
research frontier.

| Gap | Why it matters | Current state | Next useful slice |
| --- | --- | --- | --- |
| Byte-balanced pages | Variable-size records make key-count balancing weak. | Split/delete decisions are mostly degree/key-count based, with overflow fallback for byte pressure. | Track page byte fill and split/redistribute by byte occupancy. |
| Prefix compression | Interior separators and leaf keys waste space without compression. | Keys are stored plainly in every cell. | Add an optional page-local prefix-compressed leaf format version. |
| Online vacuum / page relocation | Tail compaction cannot reclaim interior holes to the filesystem. | `Compact` only trims a contiguous free suffix and never moves live pages. | Add an offline copy/compact tool before attempting online relocation. |
| Sparse-file punching | Reusable interior pages remain allocated by the filesystem. | Interior free pages stay inside the file. | Experiment with hole punching for page-size-aligned free extents while preserving mmap semantics. |
| Multi-process robustness | Reader tables need stronger owner identity than PID alone. | Slots use PID, revision, and token, with stale PID cleanup and fail-closed validation. | Include boot/session identity or start time to reduce PID reuse ambiguity. |
| Observability | A serious engine should explain stalls, reclaim pressure, and recovery decisions. | Stats expose many counters, but no structured trace/event stream. | Add optional event hooks for sync phases, recovery candidate rejection, and reclaim decisions. |
| Benchmarks | Without benchmarks, optimization claims are weak. | Tests validate behavior but do not track performance. | Add reproducible microbenchmarks for get, seek/next, range, insert, delete, reopen, and sync. |

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
compacted file geometry. The remaining crash-work is extending that subprocess
pattern to freelist/reclaim paths and eventually running true power-fail probes
outside the local harness.
