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

// KeyComparator compares two logical keys in the order used by a tree.
//
// The comparator must define a strict total order and must remain stable for
// the lifetime of the tree. Mmap files currently persist only KeyOrderBytewise,
// so custom comparators are accepted for memory-backed trees and rejected for
// mmap-backed trees until a durable comparator identity exists.
type KeyComparator interface {
	CompareKeys(left, right string) int
}

// KeyComparatorFunc adapts a function to KeyComparator.
type KeyComparatorFunc func(left, right string) int

func (f KeyComparatorFunc) CompareKeys(left, right string) int {
	return f(left, right)
}

type KeyComparatorKind uint32

const (
	KeyComparatorBytewise KeyComparatorKind = 1
	KeyComparatorCustom   KeyComparatorKind = 2
)

var bytewiseKeyComparator KeyComparator = KeyComparatorFunc(compareStrings)

func normalizeKeyComparator(comparator KeyComparator) KeyComparator {
	if comparator == nil {
		return bytewiseKeyComparator
	}
	return comparator
}
