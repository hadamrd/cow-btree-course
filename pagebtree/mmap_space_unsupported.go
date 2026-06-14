//go:build !unix

package pagebtree

func (t *Tree) MmapSpaceStats() (MmapSpaceStats, error) {
	return MmapSpaceStats{}, nil
}
