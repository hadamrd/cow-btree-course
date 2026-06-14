//go:build linux

package pagebtree

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func mmapFilesystemIdentity(path string) (mmapFilesystemEvidence, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return mmapFilesystemEvidence{}, err
	}
	evidence := mmapFilesystemEvidence{FilesystemTypeID: stat.Type}
	mountInfo, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return evidence, nil
	}
	if abs, err := filepath.Abs(path); err == nil {
		parsed := parseLinuxMountInfo(abs, string(mountInfo))
		if parsed.MountPath != "" {
			parsed.FilesystemTypeID = evidence.FilesystemTypeID
			return parsed, nil
		}
	}
	return evidence, nil
}
