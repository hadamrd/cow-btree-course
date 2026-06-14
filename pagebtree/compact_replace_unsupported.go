//go:build !unix

package pagebtree

import "errors"

// CompactMmapFile is available only on Unix-like platforms where mmap and
// advisory file locks are implemented by this package.
func CompactMmapFile(path string, options MmapOptions) (CopyCompactResult, error) {
	return CopyCompactResult{}, errors.New("mmap page storage is only available on Unix-like platforms")
}
