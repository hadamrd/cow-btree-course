# 04. Code Tour

The package is intentionally split by concept.

## Public API

`btree/tree.go` exposes:

- `New[K, V](degree int)`
- `Set(key, value)`
- `Get(key)`
- `Range(visitor)`
- `Snapshot()`
- `Stats()`

The public type is small:

```go
type Tree[K cmp.Ordered, V any] struct {
    root     *node[K, V]
    length   int
    revision uint64
    degree   int
}
```

The root pointer is the version boundary. Publishing a write means assigning a new root pointer after path-copying edits are complete.

## Node Shape

`btree/node.go` contains the private node type:

```go
type node[K cmp.Ordered, V any] struct {
    leaf     bool
    keys     []K
    values   []V
    children []*node[K, V]
}
```

The implementation stores values next to keys even in internal nodes. This is simpler than a B+tree, where all values live in leaves.

## Search

`btree/search.go` has two read-only traversals:

- `searchNode` for point lookup.
- `rangeNode` for sorted in-order scanning.

Neither function allocates or mutates nodes.

## Writes

`btree/insert.go` contains the copy-on-write mutation logic. The key discipline is simple:

```mermaid
flowchart TD
    A["about to change node?"] --> B["node must already be private"]
    B --> C["root cloned by Tree.Set"]
    B --> D["child cloned before descent"]
    B --> E["split edits private parent and private child"]
```

If you add new write operations, keep that discipline. Any helper that mutates a node should document why the node is private.

## Snapshots

`btree/snapshot.go` is deliberately tiny. A snapshot does not clone the tree. It stores the old root pointer and delegates reads to the same search and range helpers as `Tree`.

## Stats

`btree/stats.go` is not needed for the data structure itself. It exists so learners can observe height and node count while experimenting.

## Page-backed Tree

`pagebtree/` is the second implementation. It keeps the learner-friendly search shape, but replaces direct child pointers with page ids and stores page contents in a slotted byte layout.

```mermaid
flowchart LR
    Tree["Tree.root PageID"] --> Page["page"]
    Page --> Child["child PageID"]
    Child --> ChildPage["page"]
```

Read this package after the pointer-based `btree` package. The important files are:

- `pagebtree/page.go` for the slotted page header, slot directory, cells, and direct slot search helpers.
- `pagebtree/tree.go` for `Put`, `Get`, snapshots, and root page publication.
- `pagebtree/insert.go` for copy-before-descend insertion and leaf/branch splits.
