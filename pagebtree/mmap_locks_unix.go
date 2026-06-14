//go:build unix

package pagebtree

import (
	"errors"
	"os"
)

// InspectMmapLockStats checks writer-sidecar lock state without opening the
// tree as a writer and without creating missing sidecar files.
func InspectMmapLockStats(path string) (MmapLockStats, error) {
	var stats MmapLockStats
	file, err := os.OpenFile(path+".writer", os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	defer file.Close()

	stats.WriterSidecarExists = true
	if err := lockFile(file, true); err != nil {
		if errors.Is(err, ErrDatabaseLocked) {
			stats.WriterLocked = true
			return stats, nil
		}
		return stats, err
	}
	return stats, unlockFile(file)
}
