//go:build !unix

package pagebtree

import "runtime"

func mmapHolePunchProfile() MmapHolePunchCapability {
	return MmapHolePunchCapability{
		Supported:                 false,
		Platform:                  runtime.GOOS,
		RequiresPageAlignedRanges: true,
		Experimental:              true,
		UnsupportedReason:         "sparse hole punching is not available in this non-Unix lab build",
	}
}

func (t *Tree) PunchFreeMmapPages() (MmapHolePunchStats, error) {
	return MmapHolePunchStats{}, nil
}
