//go:build !unix

package pagebtree

func (t *Tree) PunchFreeMmapPages() (MmapHolePunchStats, error) {
	return MmapHolePunchStats{}, nil
}
