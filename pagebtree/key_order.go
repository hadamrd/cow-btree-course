package pagebtree

import "fmt"

// KeyOrder identifies the comparison contract used to sort keys inside leaf and
// branch pages. Mmap files persist this value so reopen can reject unknown or
// incompatible page-order semantics before walking branch separators.
type KeyOrder uint32

const (
	// KeyOrderBytewise sorts keys by unsigned lexicographic byte order. It is
	// the default order used by LMDB-style byte-string keys.
	KeyOrderBytewise KeyOrder = 1
	// KeyOrderReverse sorts keys in the reverse of bytewise order. It is a small
	// built-in persisted comparator used to prove durable comparator identity.
	KeyOrderReverse KeyOrder = 2
)

func normalizeKeyOrder(order KeyOrder) KeyOrder {
	if order == 0 {
		return KeyOrderBytewise
	}
	return order
}

func (o KeyOrder) String() string {
	switch o {
	case KeyOrderBytewise:
		return "bytewise"
	case KeyOrderReverse:
		return "reverse"
	default:
		return fmt.Sprintf("unknown(%d)", o)
	}
}

// KeyComparator compares two logical keys in the order used by a tree.
//
// The comparator must define a strict total order and must remain stable for
// the lifetime of the tree. Mmap files persist named KeyOrder values; arbitrary
// custom comparators are accepted for memory-backed trees and rejected for
// mmap-backed trees because closures do not have a durable identity.
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
	KeyComparatorReverse  KeyComparatorKind = 3
)

func (k KeyComparatorKind) String() string {
	switch k {
	case KeyComparatorBytewise:
		return "bytewise"
	case KeyComparatorCustom:
		return "custom"
	case KeyComparatorReverse:
		return "reverse"
	default:
		return fmt.Sprintf("unknown(%d)", k)
	}
}

var bytewiseKeyComparator KeyComparator = KeyComparatorFunc(compareStrings)
var reverseKeyComparator KeyComparator = KeyComparatorFunc(func(left, right string) int {
	return -compareStrings(left, right)
})

func normalizeKeyComparator(comparator KeyComparator) KeyComparator {
	if comparator == nil {
		return bytewiseKeyComparator
	}
	return comparator
}

func keyComparatorForOrder(order KeyOrder) KeyComparator {
	switch normalizeKeyOrder(order) {
	case KeyOrderReverse:
		return reverseKeyComparator
	default:
		return bytewiseKeyComparator
	}
}

func keyComparatorKindForOrder(order KeyOrder) KeyComparatorKind {
	switch normalizeKeyOrder(order) {
	case KeyOrderReverse:
		return KeyComparatorReverse
	default:
		return KeyComparatorBytewise
	}
}
