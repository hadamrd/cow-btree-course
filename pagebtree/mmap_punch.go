package pagebtree

import (
	"errors"
	"slices"
)

// ErrMmapHolePunchUnsupported reports that the current platform does not expose
// a safe page-size-aligned hole-punch primitive through this lab.
var ErrMmapHolePunchUnsupported = errors.New("mmap sparse hole punching is unsupported")

// MmapHolePunchStats reports the result of punching sparse holes for reusable
// mmap pages. File size is preserved; punched pages remain in the free list.
type MmapHolePunchStats struct {
	FreePages               int
	SkippedRecoverablePages int
	Ranges                  int
	PunchedPages            int
	PunchedBytes            int64
}

// MmapHolePunchCapability reports the platform contract behind
// PunchFreeMmapPages. It describes what this build can request from the
// filesystem; actual block reclamation is still filesystem dependent.
type MmapHolePunchCapability struct {
	Supported                 bool
	Platform                  string
	Primitive                 string
	PreservesFileSize         bool
	RequiresPageAlignedRanges bool
	Experimental              bool
	UnsupportedReason         string
}

// MmapHolePunchProfile reports the sparse-hole punching capability for this
// build. The result is process-independent and does not inspect a specific
// filesystem.
func MmapHolePunchProfile() MmapHolePunchCapability {
	return mmapHolePunchProfile()
}

func coalescedPageRanges(ids []PageID) []pageRange {
	if len(ids) == 0 {
		return nil
	}
	sorted := append([]PageID(nil), ids...)
	slices.Sort(sorted)

	ranges := make([]pageRange, 0, len(sorted))
	start := sorted[0]
	end := start + 1
	for _, id := range sorted[1:] {
		if id == end {
			end++
			continue
		}
		if id > end {
			ranges = append(ranges, pageRange{start: start, end: end})
			start = id
			end = id + 1
		}
	}
	ranges = append(ranges, pageRange{start: start, end: end})
	return ranges
}
