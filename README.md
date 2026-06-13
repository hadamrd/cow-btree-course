# Copy-on-Write B-tree Course

An educational Go implementation of a copy-on-write B-tree.

The project is intentionally small, heavily commented, and organized as a course. It is meant for readers who want to understand the mechanics behind B-trees, structural sharing, and snapshot-friendly writes without starting inside a production database engine.

## What You Get

- A clean generic B-tree package in [`btree/`](btree/)
- A page-backed copy-on-write package in [`pagebtree/`](pagebtree/) using slotted pages
- Copy-on-write writes with stable read-only snapshots
- Runnable demos in [`cmd/cowbtree`](cmd/cowbtree/) and [`cmd/pagebtree-demo`](cmd/pagebtree-demo/)
- Tests that document the behavior and invariants
- A course-style documentation folder with Mermaid diagrams in [`docs/`](docs/)

## Quick Start

```bash
go test ./...
go run ./cmd/cowbtree
go run ./cmd/pagebtree-demo
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
```

## Course Map

Start with [`docs/index.md`](docs/index.md), then read in order:

1. [`docs/01-btree-theory.md`](docs/01-btree-theory.md)
2. [`docs/02-copy-on-write.md`](docs/02-copy-on-write.md)
3. [`docs/03-insertion-algorithm.md`](docs/03-insertion-algorithm.md)
4. [`docs/04-code-tour.md`](docs/04-code-tour.md)
5. [`docs/05-exercises.md`](docs/05-exercises.md)
6. [`docs/06-page-backed-cow.md`](docs/06-page-backed-cow.md)
7. [`docs/07-freelist-and-readers.md`](docs/07-freelist-and-readers.md)

## Deliberate Scope

This is a teaching implementation, not a storage engine. The logical `btree` package stores values directly in B-tree nodes. The `pagebtree` package uses fixed-size slotted pages, branch separator keys, child page IDs, leaf key/value records, reader-pinned retired pages, and a reusable freelist. Deletion, write-ahead logging, and disk persistence are left as guided exercises.

## License

MIT.
