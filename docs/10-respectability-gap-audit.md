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
- Snapshot-backed cursors for incremental ordered reads, including forward and
  reverse half-open bounded cursors plus tree-owned point delete.
- Process-exit crash probes for direct sync publication and transaction commit
  followed by mmap sync publication.
- Persisted mmap key-order identifiers for named bytewise and reverse-bytewise
  comparators.
- Runtime profile flags for the active byte-balance policy: byte-aware split
  points, byte-aware delete redistribution, byte-fit merge checks, and the
  normalized low-fill repair threshold.
- `Stats.SyncedRevision` to distinguish the current logical revision from the
  last revision that completed `Sync`.
- `AuditReport` to expose validation evidence: current stats, reachable page
  IDs, reusable page IDs, retired page IDs, value-free page summaries,
  linked-leaf check state, and the same validation error that `Check` returns.
- Reproducible Go microbenchmarks for page and mmap get, seek/next, forward and
  reverse bounded cursor, range, insert, delete, reopen, and sync paths.

## Respectable-Engine Bar

The project should be judged against concrete storage-engine families, not
against vague database vocabulary:

- OpenLDAP MDB/LMDB-style mmap engines need checked metadata pages, strict
  data-before-meta publication, one serialized writer, lock-free readers,
  reader watermarks, and careful page recycling. This repo now implements and
  tests a visible subset of that shape, including mmap, dual metadata pages,
  reader tables, reclaim metadata, and child-process crash probes.
- Berkeley DB JE/OpenDJ-style engines pay for a different design: append-only
  logs, replay, cleaner work, Java heap cache policy, and log-file lifecycle.
  This repo deliberately does not implement that log-cleaner architecture; it
  uses in-place mmap pages plus copy-on-write root publication.
- SQLite/WiredTiger/RocksDB-class maturity includes long-lived compatibility
  fixtures, deep corruption corpora, documented fsync assumptions, concurrency
  stress, benchmark history, operational tooling, and release discipline. This
  repo has tests and traces, but it is still far from that operational bar.

The useful grind is therefore not "add buzzwords." It is proving each mechanism
at a boundary: page layout, search, COW mutation, reader pinning, recycling,
metadata publication, crash recovery, and workload behavior.

## Buyer-Style Audit

A skeptical buyer would probably classify the repo like this:

| Area | Buyer verdict | Evidence | Main rejection reason |
| --- | --- | --- | --- |
| Page layout | Credible research kernel | Fixed 4096-byte pages, checksum field, slot directory, cell area, and layout validation are concrete code, not only diagrams. | No versioned alternate page formats such as prefix-compressed pages. |
| B+tree search | Credible for point/range mechanics | Search descends branch pages through separator keys and child page IDs, then binary-searches leaf slots before resolving inline or overflow values. | Not yet backed by years of randomized production workload history. |
| COW and snapshots | Credible single-writer design | Page copies allocate new IDs, retire old page IDs, and publish a new root; snapshots pin older revisions. | Concurrency stress is limited compared with production MVCC engines. |
| mmap durability | Respectable local harness, not a product guarantee | Dirty data pages are synced before metadata publication, dual metadata pages are checked, and process-exit crash probes classify old-root/new-root recovery. | Still lacks real power-fail rigs, filesystem-specific evidence, and torn-sector simulation. |
| Recycling | Serious but narrow | In-process readers and mmap reader-table watermarks keep retired pages out of the free list until old readers are gone; reclaim metadata survives reopen. | PID reuse and long-running multi-process operational cases need harder owner identity and soak testing. |
| Caching | Honest and intentionally small | The Go cache stores derived branch routing only; raw page bytes stay in mmap and the kernel page cache. | No adaptive buffer manager, no workload-tuned eviction policy beyond bounded LRU branch metadata. |
| Key ordering | More respectable, still narrow | Memory trees accept a custom comparator across search/range/cursor/tx/check/cache paths. mmap now persists named `KeyOrder` identities for bytewise and reverse-bytewise order and reopens with the matching comparator. | Arbitrary custom comparator plugins, locale/collation identities, and prefix-compressed order-aware page formats are still unsupported. |
| Operations | Useful lab tools | `mmapinspect`, trace hooks, cache residency stats, compact-copy commands, and benchmarks exist. | No release process, no compatibility matrix, no corruption corpus management, and no production support envelope. |

The honest product label is therefore: **research-grade mmap/COW B+tree kernel
with unusually visible mechanics**, not a database one should embed in a
business-critical service.

## Code Evidence Map

These are the files a serious reader should inspect before trusting any claim:

| Claim | Code to read |
| --- | --- |
| Slotted pages really use a header, slot directory, and cells growing toward each other | [`pagebtree/page.go#L10-L13`](../pagebtree/page.go#L10-L13), [`pagebtree/page.go#L23-L40`](../pagebtree/page.go#L23-L40), [`pagebtree/page.go#L187-L236`](../pagebtree/page.go#L187-L236) |
| Point lookup walks branch pages to a leaf instead of scanning all keys | [`pagebtree/search.go#L12-L34`](../pagebtree/search.go#L12-L34) |
| Lower-bound and bounded range scans use comparator-aware slot search and branch pruning | [`pagebtree/search.go#L54-L95`](../pagebtree/search.go#L54-L95), [`pagebtree/search.go#L148-L181`](../pagebtree/search.go#L148-L181) |
| Copy-on-write allocates a new page and retires the old page ID | [`pagebtree/tree.go#L243-L269`](../pagebtree/tree.go#L243-L269) |
| Readers pin retired pages until their revision is no longer visible | [`pagebtree/freelist.go#L15-L35`](../pagebtree/freelist.go#L15-L35), [`pagebtree/freelist.go#L44-L80`](../pagebtree/freelist.go#L44-L80) |
| mmap persists named comparator identity and rejects non-durable custom closures | [`pagebtree/key_order.go#L3-L70`](../pagebtree/key_order.go#L3-L70), [`pagebtree/mmap.go#L110-L120`](../pagebtree/mmap.go#L110-L120) |
| mmap writer setup uses sidecar locks and a shared mapped arena | [`pagebtree/mmap.go#L118-L205`](../pagebtree/mmap.go#L118-L205) |
| mmap sync publishes dirty data before checked metadata | [`pagebtree/mmap.go#L1317-L1344`](../pagebtree/mmap.go#L1317-L1344) |
| mmap readers are tracked through a sidecar table | [`pagebtree/reader_table_unix.go#L42-L64`](../pagebtree/reader_table_unix.go#L42-L64), [`pagebtree/reader_table_unix.go#L74-L181`](../pagebtree/reader_table_unix.go#L74-L181) |
| Branch-routing cache is derived metadata, not a raw page cache | [`pagebtree/page_cache.go#L7-L20`](../pagebtree/page_cache.go#L7-L20), [`pagebtree/page_cache.go#L52-L80`](../pagebtree/page_cache.go#L52-L80) |
| Process-exit crash probes exist, but are still not power-fail proof | [`pagebtree/mmap_process_crash_test.go#L20-L92`](../pagebtree/mmap_process_crash_test.go#L20-L92) |

## P0 Gaps

These are the gaps that most separate the project from a genuinely serious
storage-engine artifact.

| Gap | Why it matters | Current state | Next useful slice |
| --- | --- | --- | --- |
| Crash fault injection | Recovery code is only respectable when tested at every publish boundary. | Started: the internal rollback matrix covers sync, growth, and compact-shrink boundaries. The copied-image crash harness now classifies sync-publication, growth, compact-shrink, large-freelist spill, large-reclaim spill, and obsolete metadata-generation reclaim images. `TestMmapSyncProcessCrashMatrixClassifiesRecoveryRoot` kills a child writer at sync-publication fault points and reopens the same database from a fresh process. `TestMmapTxSyncProcessCrashMatrixClassifiesRecoveryRoot` does the same after a committed read-write transaction, proving transaction publication still uses the same old-root/new-root mmap sync boundary. `TestMmapGrowthProcessCrashMatrixClassifiesOldRoot` does the same for growth file-size, directory-sync, and pre-remap fault points, all recovering the old root. `TestMmapCompactShrinkProcessCrashMatrixClassifiesCompactedRoot` does the same for compact-shrink file-size, directory-sync, and pre-remap fault points, reopening live keys from the compacted root. `TestMmapLargeFreelistProcessCrashMatrixClassifiesRecoveryRoot` does the same for large-freelist spill publication, preserving old metadata before data sync and persisted reusable-page records after metadata write. `TestMmapLargeReclaimProcessCrashMatrixClassifiesReaderPinnedRetiredPages` keeps a live read-only parent handle while killing the child writer at large-reclaim publication points, proving persisted retired records remain pinned by a surviving reader watermark. | True power-fail testing, torn-write simulation, and filesystem-specific fsync matrices are still outside the local harness. |
| Transaction batching | Real engines commit a unit of work, not one implicit root publish per call. | Advanced: `WriteBatch` stages point `Put`/`Delete` operations and half-open `DeleteRange`, hides them until `Commit`, and publishes one tree revision across memory and mmap trees. `BeginReadWrite` adds a small transaction facade with stable begin-revision reads, read-your-writes `Get`, transaction-visible `RangeBetween`, transaction cursors with staged `Delete`, range delete expansion over the staged view, rollback, byte-key wrappers, optimistic revision-conflict detection through `ErrTxConflict`, and commit through the same batch machinery. `CommitDetailed` reports per-operation old values, returns explicit invalid-commit errors, and restores the pre-commit tree state if staged replay panics before publication. `CommitSync` and `CommitSyncDetailed` now add explicit commit-then-sync helpers for batches and read-write transactions; if sync fails, the detailed result still reports the logical commit while the error says durable publication is not proven. Batch and transaction fault tests prove that retrying `Sync` after before-data-sync, after-metadata-write, and before-metadata-sync failures can publish that already-visible commit and survive close/reopen. `Sync` remains the mmap durability boundary, and the tx process-crash test verifies committed transaction changes recover as old-root before dirty data sync and new-root after metadata bytes are written. Tree-owned cursor delete outside a transaction is still point-wise and publishes immediately. | Add a fuller ACID contract: concurrent workload stress, abort/crash matrices around multi-operation conflicts, and documented fsync guarantees per filesystem. |
| Cursor API | Real B+tree users need `seek`/`next` control, not only callback scans. | Advanced: snapshot-backed cursors support `First`, `Seek`, `Next`, `Last`, `Prev`, half-open `CursorBetween(start,end)` bounds, and tree-owned point `Delete` while keeping cursor iteration on the old snapshot. | Explore richer cursor-write semantics inside explicit transactions. |
| Comparator and key model | Production B+trees need an explicit key-ordering contract before prefix compression, duplicate keys, or locale-aware indexes. | Advanced: page cells store byte strings; public `PutBytes`/`GetBytes`/`DeleteBytes`/`RangeBytes`/cursor byte-key APIs expose opaque byte keys; memory-backed trees can install a `KeyComparator` used by search, range scans, cursors, transactions, validation, and derived branch-routing cache lookups; mmap metadata persists named `KeyOrder` identities for bytewise and reverse-bytewise order, reopens with the matching comparator, and rejects unknown requested/persisted orders or arbitrary custom comparator closures; `pagebtree/testdata/mmap-v2-legacy-zero-key-order.db` proves reopen compatibility with the pre-key-order metadata word. | Add broader old-format fixture coverage, arbitrary comparator plugin identities or locale/collation IDs, and a page-format path for prefix compression. |
| Fuzz and model checking | Handwritten tests miss malformed-page combinations and delete/split corner cases. | Started: `FuzzPageTreeMatchesSortedMapModel` compares `pagebtree` with a sorted-map oracle across put, delete, cursor delete, batch, batch range delete, read-write transaction, transaction cursor delete, get, range, cursor, bounded cursor, reverse bounded cursor, and `Check` operations. `FuzzMmapTreeMatchesSortedMapModelAcrossReopen` adds mmap sync/close/reopen cycles, batch range delete, read-write transaction, transaction cursor delete, and overflow-heavy values. `FuzzMmapMalformedPageGeneratorRejectsOrChecks` mutates mmap file bytes, metadata, page headers, checksums, truncation, and tree/overflow-bearing pages, then requires any accepted image to pass `Check`. | Extend model checking to longer process-crash/reopen probes, minimized malformed-page corpora, and semantic corruption oracles. |

## P1 Gaps

These are not all needed for the next commit, but they define the serious
research frontier.

| Gap | Why it matters | Current state | Next useful slice |
| --- | --- | --- | --- |
| Byte-balanced pages | Variable-size records make key-count balancing weak. | Started: `Stats` now reports reachable leaf/branch/overflow page counts plus total and per-kind used bytes, free bytes, capacity, and normalized repair-fill policy; `MDBKernelProfile` reports the active byte-policy flags and threshold; insertion and delete redistribution choose legal leaf/branch split points by encoded cell byte footprint, large inline values can spill to overflow pages, leaf/branch repair have configurable low-byte triggers at the minimum key count, and merge decisions require combined bytes to fit in one page. | Replace conservative thresholds with production-style occupancy targets and workload-tested heuristics. |
| Prefix compression | Interior separators and leaf keys waste space without compression. | Keys are stored plainly in every cell. | Add an optional page-local prefix-compressed leaf format version. |
| Online vacuum / page relocation | Tail compaction cannot reclaim interior holes to the filesystem. | Started: `Compact` trims only a contiguous free suffix and never moves live pages; `CopyCompactMmap` copies live records into a fresh compact mmap tree; `CompactMmapFile` takes the writer mutex, rejects active mmap readers via exclusive source-file lock, resets the reader sidecar, and renames the compact file into place. | Attempt online page relocation or sparse-hole experiments. |
| Sparse-file punching | Reusable interior pages remain allocated by the filesystem. | Interior free pages stay inside the file. | Experiment with hole punching for page-size-aligned free extents while preserving mmap semantics. |
| Multi-process robustness | Reader tables need stronger owner identity than PID alone. | Slots use PID, revision, and token, with stale PID cleanup and fail-closed validation. | Include boot/session identity or start time to reduce PID reuse ambiguity. |
| Observability | A serious engine should explain stalls, reclaim pressure, and recovery decisions. | Started: `MmapOptions.TraceHook` emits structured `MmapTraceEvent` records for sync phases, timed per-range dirty data-page flushes, sync failures, recovery candidate accept/reject decisions, growth and compact remap success/failure geometry, freelist/reclaim metadata rollback page spans, stale reader cleanup counts, and obsolete metadata-page reclaim decisions. `MmapTraceJSONLExporter` and `cmd/mmaptrace-demo` provide a small JSONL export path for experiments. `Stats` exposes counters, `Stats.SyncedRevision` distinguishes logical versus successfully synced revision, `AuditReport` exposes validation traversal evidence and value-free page summaries, `cmd/mmapinspect` prints read-only audit JSON including numeric and named key-order/comparator identity plus optional reader-table, cache-residency, page-summary, and bounded key-sample sections, and `MmapCacheStats` exposes kernel residency. | Add workload trace examples, trace redaction guidance, optional asynchronous export experiments, and richer inspect modes such as JSONL trace correlation. |
| Benchmarks | Without benchmarks, optimization claims are weak. | Started: `pagebtree/bench_test.go` covers page and mmap point `Get`, cursor seek/next, forward and reverse bounded cursor scans, bounded range scans, sequential insert/delete, mmap sync-after-put, mmap delete+sync, and mmap reopen validation. | Add benchstat comparison scripts, larger workload profiles, and CI/manual baseline guidance. |

## P2 Gaps

These make the project more complete, but they should follow the P0/P1 work.

| Gap | Why it matters | Current state | Next useful slice |
| --- | --- | --- | --- |
| Multi-database catalog | LMDB-style environments commonly hold named databases. | One tree per file. | Add a catalog page after the single-tree kernel is more proven. |
| Duplicate keys and cursors | Many B+tree APIs need duplicate-key support. | Keys are unique. | Model duplicates as sorted duplicate records or subpages. |
| Durable metadata evolution | Format upgrades need explicit compatibility tests. | Metadata versions exist, but upgrade testing is narrow. | Add old-format fixture files and upgrade/reopen tests. |
| Platform matrix | mmap behavior differs across Unix variants and non-Unix platforms. | Unix path is primary; non-Unix has stubs. | Add CI matrix notes and compile-only checks for key platforms. |

## Gap Closed In This Pass: Cursor API

The cursor is intentionally snapshot-backed and incremental:

- `Tree.Cursor()` opens a cursor over the current root and owns the snapshot
  reader pin.
- `Snapshot.Cursor()` borrows an existing snapshot and does not register another
  reader.
- `CursorBetween(start,end)` opens a half-open bounded cursor and positions it
  at the first key inside the interval.
- `First()` positions at the first key, or the lower bound for bounded cursors.
- `Seek(key)` positions at the first key greater than or equal to `key`.
- `Next()` advances one key at a time.
- `Last()` positions at the last key, or the last key inside a bounded cursor.
- `Prev()` walks backward and stops before crossing a bounded cursor's lower
  bound.
- `Delete()` removes the current key from the live tree for tree-owned cursors,
  while the cursor itself keeps reading its original snapshot.
- `Key()` and `Value()` expose the current record, with `Value()` returning a
  copy.
- `Close()` releases the owned snapshot for tree cursors.

The implementation keeps a branch/leaf path stack. It does not materialize the
whole range before iteration.

Code to read:

- Cursor implementation: [`../pagebtree/cursor.go`](../pagebtree/cursor.go)
- Cursor behavior tests: [`../pagebtree/cursor_test.go`](../pagebtree/cursor_test.go)
- mmap cursor reader-pinning test: [`../pagebtree/mmap_test.go`](../pagebtree/mmap_test.go)

## Gap Advanced In This Pass: Comparator Boundary

The comparator gap is not closed, but it moved from "missing abstraction" to an
explicit persisted-identity boundary:

- Memory-backed trees can install `Options.KeyComparator`.
- The comparator is used by point lookup, lower-bound lookup, range scans,
  linked-leaf range scans, cursors, transaction-visible ordering, validation,
  and the derived branch-routing cache.
- `MDBKernelProfile.KeyComparator` reports whether the active tree is bytewise,
  reverse, or custom.
- mmap metadata persists named `KeyOrder` identities. `KeyOrderBytewise` is the
  default and legacy-zero compatibility order; `KeyOrderReverse` is a small
  built-in order that proves durable comparator identity.
- `OpenMmap` rejects arbitrary custom comparator closures because a function
  pointer or closure cannot be recovered safely from disk.

This is the right failure mode for a research engine: custom in-memory order is
available for experiments, named on-disk orders are durable, and arbitrary
on-disk comparator plugins are rejected until the format can persist a stable
plugin identity.

Code to read:

- Comparator API and persisted named-order registry: [`../pagebtree/key_order.go#L3-L70`](../pagebtree/key_order.go#L3-L70)
- Memory tree option wiring: [`../pagebtree/tree.go#L39-L54`](../pagebtree/tree.go#L39-L54)
- Comparator-aware search: [`../pagebtree/search.go#L12-L34`](../pagebtree/search.go#L12-L34)
- Comparator-aware page validation: [`../pagebtree/page.go#L159-L187`](../pagebtree/page.go#L159-L187)
- mmap order validation and custom-closure rejection: [`../pagebtree/mmap.go#L110-L120`](../pagebtree/mmap.go#L110-L120), [`../pagebtree/mmap.go#L1154-L1170`](../pagebtree/mmap.go#L1154-L1170)
- Behavior tests: [`../pagebtree/tree_test.go#L86-L132`](../pagebtree/tree_test.go#L86-L132), [`../pagebtree/mmap_test.go#L5974-L6043`](../pagebtree/mmap_test.go#L5974-L6043)

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
transaction sync matrix commits a read-write transaction, kills the child writer
during the following mmap `Sync`, and classifies recovery with the same rule:
before-data-sync reopens the old root and after metadata bytes are written
reopens the new root. The process-exit growth matrix applies the same
child-process pattern to file-size, directory-sync, and pre-remap growth
boundaries, all of which recover the old root because growth does not publish
metadata. The process-exit compact-shrink matrix kills a child writer at
file-size, directory-sync, and pre-remap shrink boundaries, then reopens all
live keys from the compacted root and checks the compacted file geometry. The
large-freelist process-exit matrix covers old-metadata recovery before data sync
and persisted freelist reuse after metadata write or before metadata sync. The
large-reclaim process-exit matrix keeps a live read-only parent handle, kills
the child writer at large-reclaim publication points, and verifies that
persisted retired records remain pinned by that surviving reader after writable
recovery and a follow-up write. The remaining crash-work is true power-fail
probing outside the local harness, torn-sector simulation, and
filesystem-specific sync-order evidence.
