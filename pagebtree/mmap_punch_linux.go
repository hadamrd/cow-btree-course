//go:build linux

package pagebtree

import (
	"os"

	"golang.org/x/sys/unix"
)

var punchFileRange = punchFileRangeLinux

func punchFileRangeLinux(file *os.File, startPage, endPage PageID) error {
	if err := validatePunchRange(startPage, endPage); err != nil {
		return err
	}
	if file == nil || startPage == endPage {
		return nil
	}
	offset := int64(startPage) * PageSize
	length := int64(endPage-startPage) * PageSize
	return unix.Fallocate(
		int(file.Fd()),
		unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE,
		offset,
		length,
	)
}
