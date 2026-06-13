# Copy-on-Write B-tree Course

This folder is a guided course for the code in this repository. Read it as a small book: each chapter introduces one idea, then points back to the exact implementation files.

## Learning Goals

By the end, you should be able to explain:

- Why B-trees keep data shallow and cache-friendly.
- How node splits preserve sorted search.
- Why copy-on-write updates can keep old snapshots readable.
- What path copying shares, what it copies, and why.
- Where this teaching implementation differs from a production database index.

## Map

```mermaid
flowchart TD
    A["01. B-tree theory"] --> B["02. Copy-on-write"]
    B --> C["03. Insertion algorithm"]
    C --> D["04. Code tour"]
    D --> E["05. Exercises"]
    D --> F["06. Page-backed CoW"]
```

## Repository Layout

```text
btree/
  doc.go        Package overview
  node.go       Private node shape and cloning
  search.go     Lookup and ordered traversal
  insert.go     Path-copying insertion and splitting
  snapshot.go   Read-only historical roots
  stats.go      Small learning-oriented structure counters
  tree.go       Public Tree API

pagebtree/
  page.go       Page ids and page copies
  tree.go       Root page publication
  insert.go     Page-copying insertion and splitting
  snapshot.go   Read-only historical root page ids

cmd/cowbtree/        Logical B-tree demonstration
cmd/pagebtree-demo/  Page-backed CoW demonstration
docs/           Course chapters
```

## Suggested Reading Path

1. Read the theory once without opening the code.
2. Run `go test ./...` to see the behavior contract.
3. Read `btree/tree_test.go`; it is the executable specification.
4. Step through `Tree.Set` in `btree/tree.go`.
5. Run `go run ./cmd/pagebtree-demo` to see page root ids change across writes.
6. Change the degree in the demos and observe how `Stats` changes.
