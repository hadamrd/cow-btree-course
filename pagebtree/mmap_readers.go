package pagebtree

// MmapReaderStats reports live and stale slots in the mmap reader table.
//
// ActiveSlots are slots owned by processes that still appear alive. StaleSlots
// are slots whose process ID is no longer alive and can be cleared with
// CleanStaleMmapReaders.
type MmapReaderStats struct {
	Slots             int
	ActiveSlots       int
	StaleSlots        int
	OldestRevision    uint64
	HasOldestRevision bool
}
