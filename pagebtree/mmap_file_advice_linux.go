//go:build linux

package pagebtree

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func fadviseFileRange(file *os.File, offset, length int64, pattern MmapAccessPattern) error {
	advice, err := fileAdvice(pattern)
	if err != nil {
		return err
	}
	return unix.Fadvise(int(file.Fd()), offset, length, advice)
}

func fileAdvice(pattern MmapAccessPattern) (int, error) {
	switch pattern {
	case MmapAccessDefault, MmapAccessRandom:
		return unix.FADV_RANDOM, nil
	case MmapAccessNormal:
		return unix.FADV_NORMAL, nil
	case MmapAccessSequential:
		return unix.FADV_SEQUENTIAL, nil
	case MmapAccessWillNeed:
		return unix.FADV_WILLNEED, nil
	case mmapAccessDontNeed:
		return unix.FADV_DONTNEED, nil
	default:
		return 0, fmt.Errorf("unknown file access pattern %d", pattern)
	}
}
