package pagebtree

// MmapSpaceStats describes the logical and physical space reported for an
// mmap-backed tree file.
type MmapSpaceStats struct {
	LogicalFileBytes          int64
	MappedBytes               int64
	AllocatedBytes            int64
	SparseBytes               int64
	AllocatedFilesystemBlocks int64
	FilesystemBlockBytes      int64
	PreferredIOBlockBytes     int64
}

func sparseBytes(logicalBytes, allocatedBytes int64) int64 {
	if allocatedBytes >= logicalBytes {
		return 0
	}
	return logicalBytes - allocatedBytes
}
