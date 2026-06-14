package pagebtree

import "errors"

// ErrReaderTable identifies malformed mmap reader-table sidecar files.
var ErrReaderTable = errors.New("mmap reader table invalid")

// MmapReaderStats reports live and stale slots in the mmap reader table.
//
// ActiveSlots are slots owned by processes that still appear alive. StaleSlots
// are slots whose process ID is no longer alive or whose process-start token no
// longer matches that PID. ProcessStartSlots counts slots carrying that stronger
// owner tag. Stale slots can be cleared with CleanStaleMmapReaders.
type MmapReaderStats struct {
	Slots             int
	ActiveSlots       int
	StaleSlots        int
	ProcessStartSlots int
	OldestRevision    uint64
	HasOldestRevision bool
}
