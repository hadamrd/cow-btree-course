package pagebtree

// PageSize is the teaching target used in the docs. This package keeps page
// contents as Go slices for readability, while the constant anchors the mental
// model to fixed-size database pages.
const PageSize = 4096

type PageID uint64

type page struct {
	id       PageID
	leaf     bool
	keys     []string
	values   [][]byte
	children []PageID
}

func newLeaf(id PageID, key string, value []byte) *page {
	return &page{
		id:     id,
		leaf:   true,
		keys:   []string{key},
		values: [][]byte{cloneBytes(value)},
	}
}

func (p *page) clone(id PageID) *page {
	out := &page{
		id:       id,
		leaf:     p.leaf,
		keys:     append([]string(nil), p.keys...),
		values:   cloneValues(p.values),
		children: append([]PageID(nil), p.children...),
	}
	return out
}

func (p *page) full(degree int) bool {
	return len(p.keys) == maxKeys(degree)
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	out := make([]byte, len(value))
	copy(out, value)
	return out
}

func cloneValues(values [][]byte) [][]byte {
	out := make([][]byte, len(values))
	for i, value := range values {
		out[i] = cloneBytes(value)
	}
	return out
}

func normalizeDegree(degree int) int {
	if degree < 2 {
		return 2
	}
	return degree
}

func maxKeys(degree int) int {
	return 2*degree - 1
}
