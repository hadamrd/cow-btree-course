package pagebtree

// MmapCacheStats describes how much of an mmap-backed tree is resident in the
// operating system page cache.
type MmapCacheStats struct {
	MappedBytes         int
	MappedDatabasePages int
	OSPageSize          int
	OSPages             int
	ResidentOSPages     int
}
