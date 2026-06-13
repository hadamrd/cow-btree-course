# CoW B+tree Storage Lab

An experimental Go storage-engine lab for mmap-backed copy-on-write B+trees.

The project is research-oriented: it keeps the code small enough to study, but follows the serious design line used by OpenLDAP LMDB/MDB: slotted pages, copy-on-write roots, mmap-backed page storage, dual metadata pages, reader-pinned recycling, and kernel page-cache cooperation. The docs compare that path with OpenDJ's Berkeley DB Java Edition lineage, where a Java heap cache, append-only logs, cleaning, and B+tree recovery make a very different set of tradeoffs.

## What You Get

- A clean generic B-tree package in [`btree/`](btree/)
- A page-backed copy-on-write package in [`pagebtree/`](pagebtree/) using slotted pages, linked leaves, overflow pages, growable/compactable mmap-backed storage, read-only mmap reader slots, stale-reader cleanup, tunable kernel page-cache advice, bounded branch-routing cache, and cache residency stats
- Copy-on-write writes with stable read-only snapshots
- Runnable demos in [`cmd/cowbtree`](cmd/cowbtree/) and [`cmd/pagebtree-demo`](cmd/pagebtree-demo/)
- Tests that document the behavior and invariants
- Research notes and diagrams in [`docs/`](docs/)

## Quick Start

```bash
go test ./...
go run ./cmd/cowbtree
go run ./cmd/pagebtree-demo
go run ./cmd/mmapbtree-demo
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

## Research Map

Start with [`docs/index.md`](docs/index.md), then read in order:

1. [`docs/01-btree-theory.md`](docs/01-btree-theory.md)
2. [`docs/02-copy-on-write.md`](docs/02-copy-on-write.md)
3. [`docs/03-insertion-algorithm.md`](docs/03-insertion-algorithm.md)
4. [`docs/04-code-tour.md`](docs/04-code-tour.md)
5. [`docs/05-exercises.md`](docs/05-exercises.md)
6. [`docs/06-page-backed-cow.md`](docs/06-page-backed-cow.md)
7. [`docs/07-freelist-and-readers.md`](docs/07-freelist-and-readers.md)
8. [`docs/08-mmap-backed-pages.md`](docs/08-mmap-backed-pages.md)
9. [`docs/09-openldap-opendj-research.md`](docs/09-openldap-opendj-research.md)

## Deliberate Scope

This is a research implementation, not a production database. The logical `btree` package stores values directly in B-tree nodes. The `pagebtree` package uses fixed-size slotted pages, branch separator keys, child page IDs, linked leaf key/value pages, lower-bound and bounded range scans, overflow pages, copy-on-write deletion, reader-pinned retired pages, a reusable freelist with checked spill pages for large persisted lists, an LMDB-inspired read-only mmap reader table with stale-reader inspection and fail-closed format validation, a bounded derived branch-routing cache, and an optional growable mmap-backed page file with dirty-page `Sync`, conservative tail `Compact`, file-size/directory sync after remaps, `madvise` plus Linux file-advice access-pattern hints, tunable exact-leaf prefetch, and `mincore` cache residency stats. Production-grade crash-order proofs, byte-balanced deletion, multi-database catalogs, and full vacuum are still open research tracks.

## License

MIT.
