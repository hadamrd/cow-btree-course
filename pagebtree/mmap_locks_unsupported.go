//go:build !unix

package pagebtree

// InspectMmapLockStats reports zero lock evidence on unsupported mmap builds.
func InspectMmapLockStats(path string) (MmapLockStats, error) {
	return MmapLockStats{}, nil
}
