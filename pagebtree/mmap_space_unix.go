//go:build unix

package pagebtree

import (
	"fmt"
	"syscall"
)

const statAllocatedBlockBytes int64 = 512

// MmapSpaceStats reports logical file size and filesystem allocation for an
// mmap-backed tree. Allocation comes from stat(2) block counts and is therefore
// filesystem-reported evidence, not a storage-engine invariant.
func (t *Tree) MmapSpaceStats() (MmapSpaceStats, error) {
	if t == nil || t.closed || t.arena == nil || t.arena.file == nil {
		return MmapSpaceStats{}, nil
	}
	info, err := t.arena.file.Stat()
	if err != nil {
		return MmapSpaceStats{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return MmapSpaceStats{}, fmt.Errorf("mmap space stats unavailable for file info %T", info.Sys())
	}
	fsEvidence, err := mmapFilesystemIdentity(t.arena.file.Name())
	if err != nil {
		return MmapSpaceStats{}, err
	}
	allocatedBytes := stat.Blocks * statAllocatedBlockBytes
	return MmapSpaceStats{
		LogicalFileBytes:          info.Size(),
		MappedBytes:               int64(len(t.arena.data)),
		AllocatedBytes:            allocatedBytes,
		SparseBytes:               sparseBytes(info.Size(), allocatedBytes),
		AllocatedFilesystemBlocks: stat.Blocks,
		FilesystemBlockBytes:      statAllocatedBlockBytes,
		PreferredIOBlockBytes:     int64(stat.Blksize),
		FilesystemType:            fsEvidence.FilesystemType,
		FilesystemTypeID:          fsEvidence.FilesystemTypeID,
		MountPath:                 fsEvidence.MountPath,
		MountSource:               fsEvidence.MountSource,
		MountOptions:              fsEvidence.MountOptions,
	}, nil
}
