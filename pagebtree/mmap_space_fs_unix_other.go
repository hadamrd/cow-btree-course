//go:build unix && !linux && !darwin && !freebsd

package pagebtree

func mmapFilesystemIdentity(path string) (string, int64, error) {
	return "", 0, nil
}
