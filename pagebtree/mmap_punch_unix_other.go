//go:build unix && !linux

package pagebtree

import "os"

var punchFileRange = punchFileRangeUnsupported

func punchFileRangeUnsupported(file *os.File, startPage, endPage PageID) error {
	if err := validatePunchRange(startPage, endPage); err != nil {
		return err
	}
	if startPage == endPage {
		return nil
	}
	return ErrMmapHolePunchUnsupported
}
