package btree

import "cmp"

// Stats is intentionally small and concrete so examples can print it without
// explaining implementation details first.
type Stats struct {
	Len      int
	Revision uint64
	Degree   int
	Height   int
	Nodes    int
	Keys     int
}

func statsFor[K cmp.Ordered, V any](root *node[K, V], length int, revision uint64, degree int) Stats {
	stats := Stats{
		Len:      length,
		Revision: revision,
		Degree:   degree,
	}
	stats.Height = height(root)
	stats.Nodes, stats.Keys = countNodesAndKeys(root)
	return stats
}

func height[K cmp.Ordered, V any](n *node[K, V]) int {
	if n == nil {
		return 0
	}
	if n.leaf {
		return 1
	}
	return 1 + height(n.children[0])
}

func countNodesAndKeys[K cmp.Ordered, V any](n *node[K, V]) (int, int) {
	if n == nil {
		return 0, 0
	}

	nodes := 1
	keys := len(n.keys)
	for _, child := range n.children {
		childNodes, childKeys := countNodesAndKeys(child)
		nodes += childNodes
		keys += childKeys
	}
	return nodes, keys
}
