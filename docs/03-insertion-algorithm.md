# 03. Insertion Algorithm

Insertion uses top-down splitting. Before descending into a full child, the algorithm splits that child. This guarantees the recursive call always receives a non-full node.

## High-level Flow

```mermaid
flowchart TD
    A["Tree.Set(key, value)"] --> B{"empty tree?"}
    B -- yes --> C["create first leaf"]
    B -- no --> D["clone root"]
    D --> E{"root full?"}
    E -- yes --> F["create new root<br/>split old root"]
    E -- no --> G["insertNonFull"]
    F --> G
    G --> H["publish copied root"]
```

## Descending

```mermaid
flowchart TD
    A["insertNonFull(node, key)"] --> B{"key in node?"}
    B -- yes --> C["replace value"]
    B -- no --> D{"leaf?"}
    D -- yes --> E["insert into sorted slices"]
    D -- no --> F["clone chosen child"]
    F --> G{"child full?"}
    G -- yes --> H["split child"]
    G -- no --> I["descend"]
    H --> J["choose left, median, or right"]
    J --> I
```

## Split Example

For degree `2`, maximum keys is `3`.

```mermaid
flowchart TD
    P0["parent<br/>50"] --> C0["full child<br/>10 | 20 | 30"]
    C0 --> S["split at median 20"]
    S --> P1["parent<br/>20 | 50"]
    P1 --> L["left child<br/>10"]
    P1 --> R["right child<br/>30"]
```

The code is in `btree/insert.go`:

- `insertNonFull` decides where to go.
- `splitChild` moves the median into the parent.
- `insertAt` keeps keys, values, and children aligned.

## Why Root Splits Are Special

When the root is full, there is no parent to receive the median. The tree creates a new empty root, makes the old root its first child, and splits that child.

```mermaid
flowchart LR
    Old["old full root"] --> New["new root"]
    New --> Left["left child"]
    New --> Right["right child"]
```

This is the only operation that increases tree height.
