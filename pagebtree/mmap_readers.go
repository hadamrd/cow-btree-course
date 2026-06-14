package pagebtree

import "errors"

// ErrReaderTable identifies malformed mmap reader-table sidecar files.
var ErrReaderTable = errors.New("mmap reader table invalid")

// MmapReaderStats reports live and stale slots in the mmap reader table.
//
// ActiveSlots are slots owned by processes that still appear alive. StaleSlots
// are slots whose process ID is no longer alive, whose process-start token no
// longer matches that PID, or whose boot/session token no longer matches this
// host boot. ProcessStartSlots and BootIDSlots count slots carrying those
// stronger owner tags. Stale slots can be cleared with CleanStaleMmapReaders.
type MmapReaderStats struct {
	FormatVersion              uint64
	ProcessStartTokenSupported bool
	BootIDTokenSupported       bool
	Slots                      int
	ActiveSlots                int
	StaleSlots                 int
	ProcessStartSlots          int
	BootIDSlots                int
	OldestRevision             uint64
	HasOldestRevision          bool
}
