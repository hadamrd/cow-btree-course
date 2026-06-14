# 08. Mmap-backed Pages

The `pagebtree` package can now store pages in an mmap-backed file.

This is the step from an in-memory model toward the OpenLDAP LMDB/MDB design family. The B+tree still uses the same slotted page layout, copy-on-write page allocation, snapshots, and reader-safe freelist mechanics. The difference is that page bytes can live inside a file mapping instead of Go heap arrays, and read-only mmap handles now register their root revision in a small reader table so a concurrent writer knows which retired pages are still unsafe to recycle.

## Run It

```bash
go run ./cmd/mmapbtree-demo
```

The demo creates a temporary database file, inserts keys, deletes one key, closes the tree, reopens the file, and reads back through the B+tree search path.

## File Layout

The mmap file is page based:

```mermaid
flowchart LR
    P0["page 0<br/>metadata A"] --> P2["page 2<br/>tree page"]
    P1["page 1<br/>metadata B"] --> P2
    P2 --> P3["page 3<br/>tree page"]
    P3 --> PN["..."]
```

Pages `0` and `1` are alternating metadata pages:

- magic bytes
- format version
- database page size
- root page id
- next page id
- length
- revision
- degree
- max page capacity
- reusable page IDs or a freelist-page root
- CRC32 checksum

`Sync` is the explicit durability boundary. Writable mmap pages are marked dirty when copy-on-write allocates or reuses their page IDs. `Sync` flushes those dirty tree and overflow page bytes first, then writes the metadata page selected by `revision % 2`, then flushes that metadata page. If the final metadata flush fails, the in-memory mapped metadata bytes are restored to their previous contents before the error is returned, so the handle does not keep advertising a metadata candidate that failed publication. `Close` calls `Sync` for writable trees. Because snapshots read slices backed by the mapping, mmap-backed `Close` returns `ErrActiveReaders` if in-process snapshots are still open; this keeps the mapping and file lock alive until readers release their handles. External read-only mmap handles are different: if they still pin retired pages, writable `Close` publishes those pending retired `(pageID, revision)` records in checked reclaim pages, then closes normally. Once close succeeds, the arena clears the released byte slice, lock state, dirty-page set, and file handle so later code cannot accidentally keep using stale mmap resources. If close-time `Sync` fails but the arena still releases the mapping and lock, the tree handle is marked closed while the sync error is returned; later post-close calls stay inert instead of touching released mmap bytes. A `Snapshot` requested after `Close` is inert and does not register a reader, so it cannot touch unmapped page bytes. Post-close inspection and maintenance calls such as `Stats`, `Sync`, `Advise`, `DropMmapCache`, `MmapCacheStats`, `MmapSpaceStats`, `MmapReaderStats`, and `CleanStaleMmapReaders` are no-ops or zero-stat reads. On reopen, the tree validates metadata magic, version, database page size, degree, checksum, key-order identity, revision-slot parity, freelist/reclaim encoding, and pending-retired safety before trying candidate records from newest to oldest. A candidate is usable only if its root and `nextPage` are inside both the mapped file and the metadata's declared capacity, the persisted ordering names a supported comparator, the root and every reachable tree or overflow page pass validation, non-root leaves and branches respect the persisted degree's minimum key count, the metadata length matches the reachable leaf-key count, and the persisted freelist plus retired-page set is safe to reuse later. If the newest metadata page is torn, corrupted, uses an unsupported format, page size, degree, or key order, appears in the wrong alternating metadata slot for its revision, points at a torn root page, points outside the mapped file, contradicts its own capacity, stores the wrong logical length, contains an underfull non-root leaf or branch, or names an unsafe reusable page, the older valid page can still point to a previous root. If no metadata page is usable, the error returned is from the newest trusted metadata revision rather than from whichever older slot was scanned last.

The key-order field is intentionally modest: it records the named comparator used for branch separators and leaf slots. The zero value is accepted as bytewise for older images, but new metadata writes an explicit `KeyOrder` identifier. Today the persisted registry contains bytewise order and a reverse-bytewise order used to prove durable comparator identity across close/reopen. An unknown requested order or an unknown persisted order is rejected before the tree walks branch child pointers. Memory-backed trees can install an arbitrary custom `KeyComparator`, but mmap-backed trees reject custom comparator closures because the file format can only safely reopen named, built-in orders. This compatibility promise is backed by `pagebtree/testdata/mmap-v2-legacy-zero-key-order.db`, a real mmap image with zero key-order metadata. The current mmap boundary is therefore explicit: named orders are durable; arbitrary custom comparators need a future plugin identity before mmap can safely reopen them.

Small reusable-page lists are stored directly in the metadata page. Once the list no longer fits there, version-2 metadata stores a freelist-page root instead. Freelist pages are normal checksummed database pages with a compact sequence of page IDs and a next-page pointer. When retired pages are still pinned by external readers, version-3 metadata stores a reclaim-page root; reclaim records with revision `0` are immediately free, and records with a nonzero revision stay retired until reader watermarks pass that revision. `Sync` writes and flushes those freelist or reclaim pages before publishing metadata that points at them, so reopen never follows an unsynced reclaim root. Reopen checks that each persisted reusable or retired page ID fits inside the metadata-declared capacity, is inside the allocated page range, appears only once, is not reachable from the accepted root or overflow chains, and does not collide with the metadata-owned page chain. Freelist and reclaim chain pages must each contribute at least one record; an empty chain page is rejected because it can hide lost metadata records while still keeping the checksum valid. Retired records must also name a nonzero revision no newer than the metadata revision that references them, and version-3 metadata may not name a reclaim root when the reclaim count is zero. Because either of the two metadata pages may be the recovery point, old freelist/reclaim-page generations become reusable only after neither valid metadata page still names their chain.

Tree pages start at page id `2`. The page id maps directly to a byte range:

```text
offset = pageID * PageSize
size   = PageSize
```

Tree pages and overflow pages also carry CRC32 checksums in their page headers. On reopen, `OpenMmap` walks the pages reachable from each metadata candidate root, including overflow chains referenced by leaf values, and rejects corruption before serving reads. Validation is deliberately layered: first the page checksum must match, then the page bytes must still form a legal layout, then branch routing must still describe a proper tree. Pages reached through the root and branch-child graph must be leaf or branch pages; a valid overflow page is still invalid in that graph. Leaf and branch pages validate their slotted-page structure before any key/value cells are decoded; overflow pages validate their payload length before the chain is followed, and referenced overflow chains must name a first page, must exist, must not loop, must contain only overflow pages, and must contain exactly the number of payload bytes recorded in the leaf's `OVF1` reference. Branch pages also reject missing child pages, duplicate child references, separators that do not match the first key of their right child, and child subtrees containing keys outside the half-open key interval assigned by the parent branch. Non-root leaf pages must also contain at least `degree-1` keys. Leaf pages reject next-leaf links that do not match the branch-order leaf sequence. If an older metadata candidate is still reachable and valid, it can be used as the recovery point.

The database page size is fixed at 4096 bytes for the lab and stored in the metadata page. Existing files must be page-aligned and large enough to hold the two metadata pages plus the minimum tree-page capacity before they are mapped. Reopen rejects a valid-checksum metadata page that advertises a different page size instead of interpreting page IDs with the wrong geometry. The persisted degree must also be inside the range that a fixed slotted page can represent: it must be at least 2, and its maximum-key count must fit inside the page's slot directory capacity. The operating system's VM page size may be larger. The mmap sync helpers therefore align requested `msync` byte ranges to the OS page size before asking the kernel to flush them. Dirty logical pages are coalesced into contiguous ranges before `msync`, so a small write does not force the whole mapped file to flush.

The mapped file can grow and explicitly compact. `MmapOptions.MaxPages` sets the initial tree-page capacity for a new database. Creating a new mmap database writes initial metadata, syncs the file, and syncs the parent directory so the directory entry is part of the durability story. Existing databases reopen at their current file size, even if a larger `MaxPages` hint is passed; capacity grows later through page allocation. When copy-on-write allocation reaches the mapped capacity, the tree flushes dirty data pages without publishing new metadata, extends the file, syncs the file-size change and parent directory, creates a larger replacement mapping, and only then releases the old mapping. That order matters: if the replacement `mmap` fails, existing pages remain readable through the old mapping. If the replacement succeeds but releasing the old mapping fails, the growth path releases the replacement mapping and restores the previous file size before returning the error. After the swap succeeds, the tree rebinds in-memory page objects to their new byte ranges. The next `Sync` still controls when metadata publishes the new root and `nextPage`.

`Compact` is intentionally conservative. It first refuses to run while any in-process snapshot or registered mmap reader-table slot is active. Then it reclaims safe retired pages, lowers `nextPage` only across a contiguous suffix of free page IDs, persists the compacted metadata, truncates the file, syncs the file-size change and parent directory, and remaps the file down to the remaining capacity. If metadata publication fails, the temporary in-memory compaction state is restored before the error is returned. Like growth, shrink remapping acquires the replacement mapping before releasing the old mapping, so a failed replacement `mmap` does not leave the handle pointing at unmapped bytes; it also restores the previous file size before returning the failure. It can also release unused mapped capacity beyond `nextPage`, such as an oversized initial `MaxPages`. It never moves live pages, so interior reusable pages remain in the freelist for future writes.

`PunchFreeMmapPages` is the active-file sparse experiment. It first reclaims retired pages that are no longer pinned by in-process snapshots or mmap reader-table watermarks. Then it scans the freelist, removes duplicates and invalid IDs, and excludes any free page that is still reachable from either valid checked metadata page. That last rule matters because the older metadata slot may still be the crash-recovery fallback; punching a page reachable from that older root would turn fallback recovery into zero-filled data. The remaining free page IDs are sorted and coalesced into page-aligned ranges. `MmapHolePunchProfile()` reports the build's capability contract before an experiment runs: Linux reports the `fallocate(FALLOC_FL_PUNCH_HOLE | FALLOC_FL_KEEP_SIZE)` primitive, page-aligned ranges, and preserved file size; other Unix builds currently report an unsupported reason for non-empty ranges until a platform-specific primitive is added; non-Unix builds report unsupported/inert behavior. The same path emits trace events so experiments can see the begin event, each half-open punched range, aggregate end stats, or the exact attempted range and error reason on failure.

Offline copy compaction is the safer next step before online vacuum. `CopyCompactMmap(src, dst, options)` opens the source as a read-only mmap snapshot, validates it with `Check`, creates a brand-new destination file, copies only live key/value records through `RangeBytes` in the source's persisted key order, validates the destination, runs destination `Compact`, and returns file-size/page-count statistics. Unless the caller explicitly supplies a destination `KeyOrder`, copy compaction preserves the source's recovered order. The destination path and its `.writer` and `.readers` sidecars must not already exist; this API refuses to overwrite instead of acting like an in-place repair tool. The command wrapper writes a separate replacement candidate:

```bash
go run ./cmd/mmapcopycompact source.db compact.db
```

`CompactMmapFile(path, options)` layers a guarded offline swap on top. It takes the source `.writer` mutex before copying so no writer can publish new roots during the copy. After the compact temporary file is ready, it takes an exclusive lock on the database file; active mmap readers hold shared locks, so the swap is rejected while readers are still open. It then resets the reader sidecar, renames the compact file over the source file, removes the temporary writer sidecar, and syncs the directory. The command wrapper is:

```bash
go run ./cmd/mmapcompact source.db
```

This reclaims interior free pages because it builds a fresh tree whose page IDs are assigned only for live records. It is still offline: the swap refuses active readers and writers instead of relocating pages under them. It is different from `PunchFreeMmapPages`: copy compaction rewrites live records into a smaller replacement file, while hole punching leaves the active file size and page IDs unchanged and only asks the filesystem to release physical blocks behind safe free extents.

Code to read:

- Offline copy-compaction API: [`../pagebtree/copy_compact.go#L23-L104`](../pagebtree/copy_compact.go#L23-L104)
- Guarded replacement API: [`../pagebtree/compact_replace_unix.go#L12-L70`](../pagebtree/compact_replace_unix.go#L12-L70)
- Sparse-hole API, capability profile, and safety filter: [`../pagebtree/mmap_punch.go`](../pagebtree/mmap_punch.go), [`../pagebtree/mmap_punch_unix.go#L7-L45`](../pagebtree/mmap_punch_unix.go#L7-L45), [`../pagebtree/mmap_punch_unix.go#L47-L80`](../pagebtree/mmap_punch_unix.go#L47-L80)
- Linux hole-punch syscall wrapper: [`../pagebtree/mmap_punch_linux.go#L10-L27`](../pagebtree/mmap_punch_linux.go#L10-L27)
- Sparse-hole trace emission helpers: [`../pagebtree/mmap_trace_unix.go`](../pagebtree/mmap_trace_unix.go)
- Refuse-overwrite path checks: [`../pagebtree/copy_compact.go#L106-L132`](../pagebtree/copy_compact.go#L106-L132)
- Command wrapper: [`../cmd/mmapcopycompact/main.go#L10-L25`](../cmd/mmapcopycompact/main.go#L10-L25)
- Replacement command wrapper: [`../cmd/mmapcompact/main.go#L10-L25`](../cmd/mmapcompact/main.go#L10-L25)
- Copy-compaction tests: [`../pagebtree/mmap_test.go#L1993-L2086`](../pagebtree/mmap_test.go#L1993-L2086)
- Replacement tests: [`../pagebtree/mmap_test.go#L2088-L2196`](../pagebtree/mmap_test.go#L2088-L2196)
- Sparse-hole tests: [`../pagebtree/mmap_test.go#L3836-L3962`](../pagebtree/mmap_test.go#L3836-L3962)
- Sparse-hole trace tests: [`../pagebtree/mmap_test.go`](../pagebtree/mmap_test.go)

## Why Mmap Helps

With mmap, the operating system maps file pages into the process address space. Code can read and write page bytes through memory loads and stores, while the OS page cache handles bringing file pages in and flushing dirty pages out.

That is one of the reasons B-trees pair well with page-oriented storage:

- tree nodes align with file pages
- branch nodes reduce random I/O by keeping the tree shallow
- hot pages stay in the OS page cache
- range scans can walk mostly sequential page memory

The important caching point is that mmap already uses the kernel page cache. Adding a second, broad Go heap page cache would often duplicate memory and fight the kernel. This project keeps page bytes in the mapping and adds access-pattern advice instead. On Linux, each public access hint is applied twice: `madvise` tells the virtual-memory mapping how the process expects to touch mapped bytes, and `posix_fadvise` tells the backing file's readahead policy what file offsets are likely or unlikely to be useful. Other Unix platforms keep the same API and research hooks, but file-level advice is a no-op when the portable syscall is not available.

- `MmapAccessDefault` uses the engine default, which is currently `MADV_RANDOM` plus Linux `FADV_RANDOM`. The zero-value option is intentionally point-lookup friendly.
- `MmapAccessRandom` also uses random advice. This tells Linux not to spend much effort on broad sequential readahead for root-to-branch-to-leaf point lookups.
- `MmapAccessSequential` uses sequential advice. Use it before range scans or bulk verification passes where nearby pages are likely to be read soon.
- `MmapAccessWillNeed` uses will-need advice. Use it as a prefetch hint before a known upcoming scan.
- `MmapAccessNormal` returns the mapping and, on Linux, the file to the kernel's normal policy for experiments or workloads where Linux's own heuristics are a better fit.

These are hints, not contracts. The kernel can ignore them or combine them with its own readahead heuristics. Correctness comes from the page checksums, copy-on-write roots, and metadata validation, not from prefetch behavior.

The package also exposes `WarmMmapTree()` and `DropMmapCache()`, which are intentionally separate from `Advise`. `WarmMmapTree` is not a broad access pattern. It follows the current B+tree root, branch children, leaf overflow references, and overflow chains, then issues `WILLNEED` only for the reachable page IDs it found. Adjacent page IDs are coalesced before calling the kernel, and reusable/free pages are skipped because they are not reachable from the current root. `Stats.MmapWarmupHints` and `Stats.MmapWarmupPages` show the number of hint calls and exact database pages covered.

`DropMmapCache()` is the opposite cache-pressure operation. Writable mmap trees call `Sync` first, then issue `DONTNEED` advice over the allocated tree-page range: `MADV_DONTNEED` for the mapping and, on Linux, `FADV_DONTNEED` for the backing file. Read-only mmap handles skip the sync and issue the advice directly. Use it after a batch job, verification pass, or demo experiment when you want to tell the kernel that these clean mapped pages do not need to stay resident.

## Coordinating With Linux Readahead

Linux already tries to detect sequential access on file-backed mappings. That is helpful for a table scan, but a B+tree point lookup is not a table scan: after the root, the next useful page is chosen by key comparison, not by physical page number. If we let the kernel assume too much sequential locality, it can pull unrelated pages into memory and evict pages we actually need.

The implementation therefore uses four rules:

- keep normal point lookups random-friendly with default `MADV_RANDOM` advice
- prefetch only exact page ranges when the tree knows the next pages, such as linked leaves during a range scan
- warm the current tree with `WarmMmapTree` when the caller explicitly wants reachable pages resident before a read-heavy phase
- avoid prefetch when the tree cannot prove the hint is still correct, such as while active readers have deferred leaf-link repair
- drop clean mapped pages only through the explicit `DropMmapCache` path, after writable state has been synced

This is why `OpenMmap` and `OpenMmapReadOnly` both apply random-access advice by default. `RangeFrom` and `RangeBetween` then first descend the tree to the correct lower-bound leaf and issue small will-need hints for a bounded window of linked next leaves. On Linux, those hints cover both the mapped address range and the matching file offsets. Adjacent leaf page IDs are coalesced into one exact half-open page range, so the tree avoids both broad whole-file guessing and avoidable one-page syscall chatter. The default window is `DefaultRangePrefetchLeafWindow` pages. `MmapOptions.RangePrefetchLeafWindow` can set a smaller or larger exact-page window, and a negative value disables linked-leaf prefetch entirely. These scans do not switch the whole mapping to sequential mode for ordinary bounded scans. Broad sequential advice remains available through `Tree.Advise` for explicit bulk passes, such as a full verification walk, where physical readahead is likely to be useful. `MmapAccessNormal` is available when you want to compare this policy against Linux's default readahead heuristics.

The project also has a small user-space page cache, but it deliberately does not cache raw page bytes. Raw bytes stay in the mmap region and the kernel page cache. The Go cache stores derived branch-routing metadata: decoded separator keys and child page IDs for branch pages reached by current-tree `Get`. Each entry is keyed by page ID plus the page checksum. If a page ID is later reused with different bytes, the checksum changes and the cache refreshes the decoded routing entry.

That derived cache is bounded with least-recently-used eviction. `DefaultPageCacheCapacity` is used unless a memory tree is created with `NewWithOptions` or an mmap tree is opened with `MmapOptions.PageCacheCapacity`. A negative capacity disables this derived cache, while still leaving raw bytes in the mmap and kernel page cache. `Stats` exposes `PageCacheCapacity`, `PageCacheEntries`, `PageCacheHits`, `PageCacheMisses`, `PageCacheInvalidations`, and `PageCacheEvictions` so this behavior is visible. It also exposes reachable leaf, branch, and overflow page counts plus used/free/capacity bytes for the current root. Those byte-fill counters are a prerequisite for studying byte-balanced split and delete behavior instead of relying only on key counts. `MDBKernelProfile` exposes the same derived-cache capacity and counters beside the kernel-cache booleans, plus byte-policy flags for byte-aware split points, byte-aware delete redistribution, byte-fit merges, and the normalized repair-fill threshold. That keeps the OpenLDAP-style profile honest about what the Go process caches, what it leaves to the kernel, and which page-balance policy is actually active.

`Range`, `RangeFrom(start)`, and `RangeBetween(start, end)` now use the B+tree's leaf links to make smaller hints than a whole-file sequential policy. `RangeFrom` first walks the branch pages to the leaf that can contain `start`, so it avoids scanning the left side of the tree before the lower bound. `RangeBetween` also stops before the exclusive `end` key and does not prefetch a next leaf if that leaf's first key is already outside the requested interval. When no active reader has deferred leaf-link repair, the scan walks leaf-to-leaf and asks the kernel to prefetch the configured window of exact next leaf pages with `MADV_WILLNEED`; adjacent page IDs inside that window are coalesced into one `madvise` range. It does not ask Linux to guess far ahead across the whole mapping. If readers are active and current leaf links may be stale, the scan falls back to the recursive branch walk and skips leaf prefetch. `Stats.RangePrefetchLeafWindow`, `Stats.RangePrefetchHints`, and `Stats.RangePrefetchPages` make the configured window, successful hint-call count, and exact pages covered visible while experimenting.

`WarmMmapTree` is useful after opening a database or before a known read-heavy phase. It is more expensive than doing nothing because the tree must read page headers and child pointers to discover the exact reachable set, but that cost is deliberate: the hint is based on B+tree structure rather than Linux guessing from physical file offsets. It also includes reachable overflow pages, so large values are warmed with their leaf references, and it skips deleted/reusable pages even if they are still allocated inside the file.

```mermaid
flowchart LR
    L1["current leaf"] --> L2["next leaf"]
    L2 --> L3["next next leaf"]
    L3 --> L4["later leaf"]

    S["RangeBetween(start,end)"] --> D["descend to lower-bound leaf"]
    D --> A["read current leaf"]
    S --> P["MADV_WILLNEED next leaves before end"]
    P -. page id .-> L2
    P -. page id .-> L3
```

The package also exposes `MmapCacheStats`, backed by `mincore` on Unix. This is an observability tool, not an application cache. It reports:

- mapped file bytes
- mapped database pages
- OS VM page size
- mapped OS page count
- resident OS page count

That makes the distinction visible between the project's 4096-byte database pages and the kernel's VM pages. On some systems those sizes match; on others one OS page covers several database pages.

`MmapSpaceStats` is the matching physical-space observer for sparse-file work. On Unix it reads `stat(2)` data from the mapped database file and reports logical file bytes, mapped bytes, allocated bytes, sparse bytes, allocated filesystem blocks, the 512-byte block unit used by the allocation count, and the filesystem's preferred I/O block size. `MmapHolePunchProfile()` complements those counters with the platform-level sparse-punch capability used by this build. Both are intentionally evidence surfaces rather than correctness invariants: allocation can be rounded up, compressed, cloned, delayed, or unsupported depending on the filesystem. The APIs are still useful because they let experiments compare `Compact`, `CopyCompactMmap`, `CompactMmapFile`, and `PunchFreeMmapPages` with stable measurement and capability fields.

## Observability Hooks

Counters answer "how much"; trace events answer "why did this path happen?" The project now exposes `MmapOptions.TraceHook`, an optional synchronous callback that receives `MmapTraceEvent` structs. The hook is intentionally narrow and storage-engine-shaped, not a general logging framework. It reports:

- `mmap-sync-begin`, `mmap-sync-data-range`, `mmap-sync-data-synced`, `mmap-sync-meta-published`, `mmap-sync-end`, and `mmap-sync-failed` around the dirty-data-before-metadata publish path
- `mmap-recovery-candidate-rejected` when a metadata candidate fails checksum, slot, bounds, reachable-page, freelist, reclaim, leaf-link, or length validation
- `mmap-recovery-candidate-accepted` when recovery chooses the root that will serve reads
- `mmap-reclaim-obsolete-metadata-pages` when old freelist/reclaim metadata pages become reusable after neither checked metadata slot references them
- `mmap-freelist-metadata-rollback` and `mmap-reclaim-metadata-rollback` when freshly prepared checked metadata pages are removed after a later sync or metadata-publish failure
- `mmap-growth-begin`, `mmap-growth-end`, and `mmap-growth-failed` around growth remapping, with old/new mapped capacity and file size
- `mmap-compact-begin`, `mmap-compact-end`, and `mmap-compact-failed` around tail compaction, with old/new `nextPage`, capacity, and file size
- `mmap-reader-table-cleanup` when stale dead-PID reader-table slots are explicitly cleared
- `mmap-punch-begin`, `mmap-punch-range`, `mmap-punch-end`, and `mmap-punch-failed` around sparse-hole punching, including skipped fallback-recoverable pages, punched ranges, punched pages, punched bytes, and failure reasons

Each event carries stable revision/page geometry: root page ID, `nextPage`, mapped capacity, old/new geometry for growth and compaction, logical length, dirty/free/retired counts, reclaimed-page count for reclaim events, metadata-page count for metadata rollback events, cleared-slot count for reader cleanup, punch range/page/byte counts for sparse punching, metadata slot, file-size bytes when a remap is involved, and a rejection or failure reason when one exists. Dirty data-range, metadata-rollback, and punch-range events also carry half-open `StartPage`/`EndPage` boundaries matching the affected logical page IDs. Dirty range events add `DurationNanos` for that range flush. A hook should return quickly and should not call back into the same tree; use it to append to a test buffer, increment a probe, or hand off to an external logger.

For simple experiments, `NewMmapTraceJSONLExporter` adapts the hook to newline-delimited JSON. Trace hooks cannot return errors, so the exporter keeps the first write or encode error and exposes it through `Err()` after `Sync`, `Close`, or the experiment block. The JSON schema uses lower-snake field names and omits the internal negative `MetadataSlot` sentinel when an event is not tied to a checked metadata page. The built-in events are value-free: they report page IDs, revisions, counts, ranges, durations, slots, and failure reasons, but not application keys or values. If an embedding application adds its own trace fields around this hook, redact at that application boundary before export. `NewMmapTraceAsyncJSONLExporter` adds a bounded background queue for experiments where trace writes should not run inside the storage hook; `Close` flushes queued events and `Dropped` reports events skipped because the queue was full or already closed. The `cmd/mmaptrace-demo` command writes pure JSONL to stdout and runs a small write/delete/compact workload so the output contains sync, growth, and compaction events:

```bash
go run ./cmd/mmaptrace-demo > mmap-trace.jsonl
```

For a one-shot read-only integrity snapshot, `cmd/mmapinspect` opens the
database with `OpenMmapReadOnly`, runs `Audit`, and prints indented JSON with
the validity bit, validation error if any, stats, persisted `key_order`,
`key_order_name`, `key_comparator`, `key_comparator_name`, reachable page IDs,
free page IDs, retired page IDs, and leaf-link validation state. `--readers`
adds reader-table slot statistics, including active/stale slots and oldest
pinned revision. `--cache` adds `mincore`-backed mapped/resident page counts,
and `--space` adds `stat(2)` logical-vs-allocated file-space counts plus the
hole-punch capability profile for sparse experiments. `--pages` adds value-free page summaries with role, kind, byte
occupancy, branch children, and next-page hints. `--keys N` adds a bounded first/last key
sample in the recovered comparator order without dumping values. `--trace
TRACE.jsonl` reads value-free trace output, counts event kinds, summarizes dirty
data-page ranges, summarizes sparse-hole punched ranges/pages/bytes and skipped
fallback-recoverable pages, lists failure reasons, and checks whether the last
traced revision/root/nextPage geometry matches the inspected file:

```bash
go run ./cmd/mmapinspect --readers --cache --space --pages --keys=4 --trace mmap-trace.jsonl source.db
```

This is useful when studying recovery fallback. If the newest metadata page points at a torn root page, a trace hook can show the newest candidate rejected with a checksum or invariant reason and the older candidate accepted. That is more precise than a counter saying "one open succeeded."

Code to read:

- Audit inspect command: [`../cmd/mmapinspect/main.go`](../cmd/mmapinspect/main.go)
- Audit inspect command tests: [`../cmd/mmapinspect/main_test.go`](../cmd/mmapinspect/main_test.go)
- Mmap space stats API: [`../pagebtree/mmap_space.go`](../pagebtree/mmap_space.go), [`../pagebtree/mmap_space_unix.go`](../pagebtree/mmap_space_unix.go)
- Trace event API and JSON field schema: [`../pagebtree/mmap_trace.go#L3-L109`](../pagebtree/mmap_trace.go#L3-L109)
- JSONL exporters: [`../pagebtree/mmap_trace_export.go`](../pagebtree/mmap_trace_export.go)
- JSONL exporter example: [`../pagebtree/example_test.go#L137-L157`](../pagebtree/example_test.go#L137-L157)
- JSONL trace demo command: [`../cmd/mmaptrace-demo/main.go`](../cmd/mmaptrace-demo/main.go)
- Hook option: [`../pagebtree/mmap.go#L56-L64`](../pagebtree/mmap.go#L56-L64)
- Dirty range coalescing and range callbacks: [`../pagebtree/mmap.go#L540-L588`](../pagebtree/mmap.go#L540-L588)
- Sync phase and range emissions: [`../pagebtree/mmap.go#L1287-L1309`](../pagebtree/mmap.go#L1287-L1309)
- Recovery candidate emissions: [`../pagebtree/mmap.go#L937-L1051`](../pagebtree/mmap.go#L937-L1051)
- Obsolete metadata-page reclaim event: [`../pagebtree/mmap.go#L1408-L1438`](../pagebtree/mmap.go#L1408-L1438)
- Growth trace emissions: [`../pagebtree/mmap.go#L346-L394`](../pagebtree/mmap.go#L346-L394)
- Compact trace emissions: [`../pagebtree/mmap.go#L408-L482`](../pagebtree/mmap.go#L408-L482)
- Reader cleanup trace emission: [`../pagebtree/reader_table_unix.go#L262-L272`](../pagebtree/reader_table_unix.go#L262-L272)
- Sparse-hole trace emissions: [`../pagebtree/mmap_punch_unix.go`](../pagebtree/mmap_punch_unix.go), [`../pagebtree/mmap_trace_unix.go`](../pagebtree/mmap_trace_unix.go)
- Trace hook behavior tests: [`../pagebtree/mmap_test.go#L3712-L3866`](../pagebtree/mmap_test.go#L3712-L3866), [`../pagebtree/mmap_test.go#L3868-L3911`](../pagebtree/mmap_test.go#L3868-L3911), [`../pagebtree/mmap_test.go#L5132-L5183`](../pagebtree/mmap_test.go#L5132-L5183), and [`../pagebtree/mmap_trace_export_test.go#L12-L87`](../pagebtree/mmap_trace_export_test.go#L12-L87)

## Live Integrity Checks

`Tree.Check()` runs the reachable-page validator against the currently open tree. It verifies page checksums, slotted-page layout, branch child reachability, separator keys, subtree key bounds, non-root leaf and branch minimum fill, overflow references and chains, length-vs-key-count consistency, and reusable-page safety. When no in-process reader is active, it also validates persisted leaf sibling links.

The active-reader exception is intentional. Current leaf-link repair is deferred while snapshots are open, because rewriting a copied page's sibling pointer in place could mutate bytes still visible to an older root. During that window, `Check()` validates the durable tree shape but skips the leaf-link invariant until the last reader closes and repair can run.

The same reachable-page checks are used during mmap metadata recovery. That means `Check()` is the live, explicit version of the safety gate that `OpenMmap` applies before accepting a recovered root.

```mermaid
flowchart TD
    G["Get(key)"] --> A["Advise random access"]
    A --> R["root page"]
    R --> D["branch route cache<br/>decoded keys + child ids"]
    D --> B["branch page"]
    B --> L["leaf page"]
    L --> V["value or overflow chain"]

    S["Range / bounded scan"] --> Q["Will-need exact next leaves"]
    Q --> P1["current leaf page"]
    P1 --> P2["next linked leaf"]
    P2 --> P3["next linked leaf"]

    C["MmapCacheStats"] --> M["mincore"]
    M --> K["resident OS pages"]

    W["WarmMmapTree"] --> E["walk current root reachability"]
    E --> N["MADV_WILLNEED exact tree + overflow pages"]

    I["Check"] --> V["reachable-page validation"]
    V --> O["checksums + layout + routing + overflow"]

    X["DropMmapCache"] --> Y["Sync dirty pages + metadata"]
    Y --> Z["MADV_DONTNEED clean tree-page range"]
```

## Writer Mutex and Reader Table

`OpenMmap` takes a non-blocking exclusive advisory lock on a sidecar writer-mutex file. A second writer attempting to open the same path receives `ErrDatabaseLocked` until the first writer closes. The database file itself is mapped with a shared advisory lock so read-only handles can coexist with the writer.

`OpenMmapReadOnly` opens the database file with a shared advisory lock and a read-only mapping, then claims a sidecar reader-table slot with revision `0` before metadata recovery reads a root. Revision `0` is a conservative provisional pin: writers that scan the table during that short window must keep all retired pages unavailable for reuse. After metadata recovery chooses a root, the reader updates its slot to the recovered revision and validates the table against that recovered revision before returning the handle. Writable `OpenMmap` performs the same validation after recovering metadata from an existing file. The slot records the process ID and revision. When a writer reclaims retired pages, it combines in-process snapshots with the oldest live revision found in that reader table. If any active reader can still see a retired page, the writer allocates new pages instead of recycling that page ID. Version-3 metadata can persist those still-pinned retired records in checked reclaim pages, so a writer can close and a later writer can recover the same pending retired list. When the read-only handle closes, the next writer reclaim pass can move those records into the reusable free list.

`MmapReaderStats()` exposes the table shape: total slots, live active slots, stale slots whose process no longer exists, and the oldest live reader revision. `CleanStaleMmapReaders()` clears stale slots explicitly. Writers also clean dead-PID slots when scanning the oldest reader, but the public maintenance call makes reader-table hygiene visible in tests, demos, and operational experiments. A live slot whose revision is newer than the tree's current revision, or whose claim token is zero, is treated as malformed reader-table state. If a writer sees that while reclaiming retired pages, it fails closed by pinning all retired pages instead of recycling from an untrusted watermark.

Reader-table initialization is fail-closed. A missing or brand-new zero-length `.readers` file is initialized with the expected magic, version, and slot count. An existing sidecar with the wrong size, magic, version, or slot count returns `ErrReaderTable` instead of being reset. That matters because silently recreating a malformed table while read-only handles exist would erase their page-recycling watermarks.

This is the same kernel idea described in the MDB paper: a single writer mutex is separate from the main database mapping, and the lock region contains reader slots that let writers decide when old copy-on-write pages are safe to reuse. This project keeps the table deliberately small and file-backed for study; LMDB's production table is shared-memory-oriented and cache-line-aware.

```mermaid
sequenceDiagram
    participant A as Writer A
    participant R as Reader
    participant B as Writer B
    participant F as DB file
    participant L as writer sidecar
    participant T as reader table
    A->>L: OpenMmap + exclusive writer mutex
    R->>F: OpenMmapReadOnly + read-only mapping
    R->>T: claim provisional slot at revision 0
    R->>F: recover metadata root at revision N
    R->>T: update slot to revision N
    A->>T: scan oldest reader before recycling
    A->>F: delete key, retire copied pages
    A->>F: allocate fresh pages while N pins retired pages
    A->>F: Sync publishes reclaim page with P,N
    A->>L: Close releases writer mutex
    B->>L: OpenMmap
    B->>F: recover pending retired P,N
    R->>T: close clears slot
    B->>T: scan finds no old reader
    B->>F: retired pages become reusable
```

This keeps the storage lab honest: the mmap implementation has one writer at a time, read-only handles can inspect a stable root without taking write permission, and a writer can keep progressing while old readers pin pages through revision watermarks.

## What Is Still Not Production-grade

This chapter makes the project more serious, but it is still not a production database:

- freelist pages are reclaimed conservatively after both metadata slots advance, guarded offline compaction can create and swap in a smaller replacement file, and `PunchFreeMmapPages` can ask supported filesystems to sparse-punch safe free extents, but there is still no online vacuum that moves live pages in place
- `Sync` flushes dirty data pages before metadata, and reopen can fall back from a torn newest root to an older valid root, but there is no complete crash-safe write-order protocol or WAL
- file creation, mapped file growth, and compaction sync file-size or directory-entry changes, but the project still does not model every filesystem or storage-device ordering edge case
- metadata pages, reachable tree pages, and reachable overflow pages are checksummed and validated for format, page size, degree, bounds, layout, routing, freelist safety, and key-count consistency, but there is no page-level repair
- the reader table now lets read-only mmap handles coexist with a writer, pin recycling, clean stale slots, reject malformed existing sidecars, and preserve externally pinned retired pages across writer close/reopen, but it is intentionally simpler than LMDB's production lock-table implementation
- overflow pages are linear chains, not a compact extent/tree structure
- insertion and delete redistribution choose byte-aware leaf and branch split points, byte-full leaf rewrites can spill cells to overflow pages, leaf/branch repair can trigger on configurable low byte occupancy at minimum key count, and merge decisions require combined bytes to fit in one page; `Stats` reports reachable page byte fill and the normalized repair-fill percent, but repair still lacks mature occupancy-target heuristics
- `Get`, branch range traversal, and bounded leaf scans search slot directories directly, but insertion and deletion still rewrite copied pages from decoded entries
- `Delete` removes records, borrows before descending into minimum-fill branches, merges or redistributes underfull leaves and branches, and collapses simple roots
- `Compact` can truncate unused capacity and trailing free pages, `CopyCompactMmap` can copy live records into a fresh compact file, `CompactMmapFile` can swap that file in while rejecting active readers/writers, and `PunchFreeMmapPages` can sparse-punch safe free extents without changing file length on supported systems, but there is no online vacuum that moves live pages inside an active database

The goal is to make mmap concrete without burying the reader under every database-engine concern at once.
