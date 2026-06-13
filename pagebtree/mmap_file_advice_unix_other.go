//go:build unix && !linux

package pagebtree

import "os"

func fadviseFileRange(file *os.File, offset, length int64, pattern MmapAccessPattern) error {
	return nil
}
