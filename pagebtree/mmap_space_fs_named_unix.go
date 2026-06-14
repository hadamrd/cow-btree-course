//go:build darwin || freebsd

package pagebtree

import "golang.org/x/sys/unix"

func mmapFilesystemIdentity(path string) (string, int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return "", 0, err
	}
	return unixByteString(stat.Fstypename[:]), int64(stat.Type), nil
}

func unixByteString(data []byte) string {
	for i, value := range data {
		if value == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}
