//go:build darwin || freebsd

package pagebtree

import "golang.org/x/sys/unix"

func mmapFilesystemIdentity(path string) (mmapFilesystemEvidence, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return mmapFilesystemEvidence{}, err
	}
	return mmapFilesystemEvidence{
		FilesystemType:   unixByteString(stat.Fstypename[:]),
		FilesystemTypeID: int64(stat.Type),
		MountPath:        unixByteString(stat.Mntonname[:]),
		MountSource:      unixByteString(stat.Mntfromname[:]),
	}, nil
}

func unixByteString(data []byte) string {
	for i, value := range data {
		if value == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}
