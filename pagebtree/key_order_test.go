package pagebtree

import "testing"

func TestKeyOrderStringNames(t *testing.T) {
	tests := []struct {
		order KeyOrder
		want  string
	}{
		{KeyOrderBytewise, "bytewise"},
		{KeyOrderReverse, "reverse"},
		{KeyOrder(99), "unknown(99)"},
	}

	for _, tt := range tests {
		if got := tt.order.String(); got != tt.want {
			t.Fatalf("KeyOrder(%d).String() = %q, want %q", tt.order, got, tt.want)
		}
	}
}

func TestKeyComparatorKindStringNames(t *testing.T) {
	tests := []struct {
		kind KeyComparatorKind
		want string
	}{
		{KeyComparatorBytewise, "bytewise"},
		{KeyComparatorReverse, "reverse"},
		{KeyComparatorCustom, "custom"},
		{KeyComparatorKind(99), "unknown(99)"},
	}

	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Fatalf("KeyComparatorKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
