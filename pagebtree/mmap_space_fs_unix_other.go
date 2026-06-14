//go:build unix && !linux && !darwin && !freebsd

package pagebtree

func mmapFilesystemIdentity(path string) (mmapFilesystemEvidence, error) {
	return mmapFilesystemEvidence{}, nil
}
