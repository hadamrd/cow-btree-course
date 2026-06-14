//go:build unix && !linux

package pagebtree

import (
	"os"
	"runtime"
)

var punchFileRange = punchFileRangeUnsupported

func mmapHolePunchProfile() MmapHolePunchCapability {
	return MmapHolePunchCapability{
		Supported:                 false,
		Platform:                  runtime.GOOS,
		RequiresPageAlignedRanges: true,
		Experimental:              true,
		UnsupportedReason:         "no platform-specific sparse hole-punch primitive is wired in this lab build",
	}
}

func punchFileRangeUnsupported(file *os.File, startPage, endPage PageID) error {
	if err := validatePunchRange(startPage, endPage); err != nil {
		return err
	}
	if startPage == endPage {
		return nil
	}
	return ErrMmapHolePunchUnsupported
}
