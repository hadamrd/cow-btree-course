package pagebtree

// KeyOrder identifies the comparison contract used to sort keys inside leaf and
// branch pages. Mmap files persist this value so reopen can reject unknown or
// incompatible page-order semantics before walking branch separators.
type KeyOrder uint32

const (
	// KeyOrderBytewise sorts keys by unsigned lexicographic byte order. It is
	// the only order currently implemented by the page format.
	KeyOrderBytewise KeyOrder = 1
)

func normalizeKeyOrder(order KeyOrder) KeyOrder {
	if order == 0 {
		return KeyOrderBytewise
	}
	return order
}
