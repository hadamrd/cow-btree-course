# OpenLDAP-Style mmap B+tree Research Lab

An experimental Go storage-engine research lab for mmap-backed copy-on-write B+trees.

The reference design line is OpenLDAP MDB/LMDB: slotted pages in one mapped file, copy-on-write updates, alternating checked metadata pages, one serialized writer, lock-free readers, reader-table watermarks for recycling, and direct cooperation with the kernel page cache. The contrast design is OpenDJ's Berkeley DB Java Edition backend: Java heap caching, append-only `.jdb` logs, cleaner work, and B+tree recovery from logged state.

## What You Get

- A clean generic B-tree package in [`btree/`](btree/)
- A page-backed copy-on-write package in [`pagebtree/`](pagebtree/) using slotted pages, linked leaves, overflow pages, growable/compactable mmap-backed storage, offline mmap copy compaction, read-only mmap reader slots, stale-reader cleanup, tunable kernel page-cache advice, bounded branch-routing cache, validation audit reports, cache residency stats, and optional mmap trace events with JSONL export
- A small `MDBKernelProfile` API that reports which OpenLDAP-style kernel mechanics and byte-balance policies are active on a live tree
- Copy-on-write writes with stable read-only snapshots
- Runnable demos and tools in [`cmd/cowbtree`](cmd/cowbtree/), [`cmd/pagebtree-demo`](cmd/pagebtree-demo/), [`cmd/mmapbtree-demo`](cmd/mmapbtree-demo/), [`cmd/mdbkernel-demo`](cmd/mdbkernel-demo/), [`cmd/mmaptrace-demo`](cmd/mmaptrace-demo/), and [`cmd/mmapinspect`](cmd/mmapinspect/)
- Tests that document the behavior and invariants
- Research notes and diagrams in [`docs/`](docs/)

## Quick Start

```bash
go test ./...
go run ./cmd/cowbtree
go run ./cmd/pagebtree-demo
go run ./cmd/mmapbtree-demo
go run ./cmd/mdbkernel-demo
go run ./cmd/mmaptrace-demo > mmap-trace.jsonl
go run ./cmd/mmapinspect --readers --cache /path/to/source.db
go run ./cmd/mmapcopycompact /path/to/source.db /path/to/compact.db
go run ./cmd/mmapcompact /path/to/source.db
go test ./pagebtree -run '^$' -bench 'Benchmark(PageTree|MmapTree)' -benchtime=100x
```

```go
tree := btree.New[int, string](2)
tree.Set(10, "ten")
snapshot := tree.Snapshot()

tree.Set(10, "TEN")

oldValue, _ := snapshot.Get(10) // "ten"
newValue, _ := tree.Get(10)    // "TEN"
```

Page-backed usage:

```go
tree := pagebtree.New(2)
tree.Put("k01", []byte("value-01"))

value, ok := tree.Get("k01")
old, deleted := tree.Delete("k01")
tree.RangeFrom("k10", func(key string, value []byte) bool {
	// visit keys >= k10 in sorted order
	return true
})
tree.RangeBetween("k10", "k20", func(key string, value []byte) bool {
	// visit k10 <= keys < k20
	return true
})
```

OpenLDAP-style mmap kernel profile:

```go
tree, _ := pagebtree.OpenMmap("research.db", pagebtree.MmapOptions{
	Degree:   2,
	MaxPages: 1024,
})
defer tree.Close()

tree.Put("uid=alice", []byte("entry bytes"))
profile := tree.MDBKernelProfile()

fmt.Println(profile.Storage)          // mmap
fmt.Println(profile.SlottedPages)     // true
fmt.Println(profile.ReaderTable)      // true
fmt.Println(profile.KernelPageCache)  // true
fmt.Println(profile.RawHeapPageCache) // false
fmt.Println(profile.DerivedBranchRoutingCacheCapacity)
fmt.Println(profile.DerivedBranchRoutingCacheHits)
```

## Research Map

Start with [`docs/index.md`](docs/index.md). For the serious guided course with diagrams and direct code citations, start here:

0. [`docs/00-storage-engine-course.md`](docs/00-storage-engine-course.md)

Then read the focused chapters in order:

1. [`docs/01-btree-theory.md`](docs/01-btree-theory.md)
2. [`docs/02-copy-on-write.md`](docs/02-copy-on-write.md)
3. [`docs/03-insertion-algorithm.md`](docs/03-insertion-algorithm.md)
4. [`docs/04-code-tour.md`](docs/04-code-tour.md)
5. [`docs/05-exercises.md`](docs/05-exercises.md)
6. [`docs/06-page-backed-cow.md`](docs/06-page-backed-cow.md)
7. [`docs/07-freelist-and-readers.md`](docs/07-freelist-and-readers.md)
8. [`docs/08-mmap-backed-pages.md`](docs/08-mmap-backed-pages.md)
9. [`docs/09-openldap-opendj-research.md`](docs/09-openldap-opendj-research.md)
10. [`docs/10-respectability-gap-audit.md`](docs/10-respectability-gap-audit.md)

## Deliberate Scope

This is a research implementation, not a production database. The logical `btree` package stores values directly in B-tree nodes. The `pagebtree` package uses fixed-size slotted pages, byte-aware leaf and branch split points, byte-aware delete redistribution, conservative byte-fit merge decisions, configurable leaf and branch byte-occupancy repair triggers, branch separator keys, child page IDs, linked leaf key/value pages, string and opaque byte-key APIs with metadata-persisted named key ordering, memory-backed custom comparator support, lower-bound and bounded range scans, snapshot-backed seek/next/prev cursors with half-open bounds and point delete, write batches with half-open range delete and explicit `CommitSync` helpers, read-write transactions with stable base reads, optimistic revision-conflict detection, read-your-writes point/range/cursor reads, cursor delete, and explicit commit-then-sync helpers, overflow pages, copy-on-write deletion, reader-pinned retired pages, a reusable freelist with checked spill pages for large persisted lists, versioned reclaim metadata for externally pinned retired pages, an LMDB-inspired read-only mmap reader table with stale-reader inspection and fail-closed format validation, a bounded derived branch-routing cache, and an optional growable mmap-backed page file with dirty-page `Sync`, logical-vs-synced revision stats, conservative tail `Compact`, offline `CopyCompactMmap`, guarded offline `CompactMmapFile` replacement, file-size/directory sync after remaps, `madvise` plus Linux file-advice access-pattern hints, tunable exact-leaf prefetch, `mincore` cache residency stats, opt-in trace events for sync, recovery fallback, growth/compact remap success and failure, freelist/reclaim metadata rollback, reader cleanup, and obsolete metadata-page reclaim decisions, plus a small JSONL trace exporter for experiments. Production-grade crash-order proofs, arbitrary mmap custom-comparator plugin identities beyond the built-in persisted orders, mature byte-occupancy target heuristics, multi-database catalogs, sparse-file reclamation, and online vacuum are still open research tracks.

## License

MIT.
