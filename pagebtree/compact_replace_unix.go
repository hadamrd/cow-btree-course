//go:build unix

package pagebtree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CompactMmapFile copies a closed mmap database into a compact temporary file
// and atomically replaces the database file with that compact copy.
//
// The call is offline. It takes the source writer mutex before copying, so no
// writer can publish new roots during the copy. Before the final rename, it also
// takes an exclusive lock on the database file; active mmap readers hold shared
// locks, so the replacement is rejected while readers are open.
func CompactMmapFile(path string, options MmapOptions) (CopyCompactResult, error) {
	var result CopyCompactResult
	if path == "" {
		return result, fmt.Errorf("compact mmap file path is empty")
	}

	writerLock, err := openWriterLock(path)
	if err != nil {
		return result, err
	}
	defer closeWriterLock(writerLock)

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tempPath, err := reserveCompactTempPath(dir, base)
	if err != nil {
		return result, err
	}
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			removeCopyCompactArtifacts(tempPath)
		}
	}()

	result, err = CopyCompactMmap(path, tempPath, options)
	if err != nil {
		return result, err
	}

	sourceLock, err := lockSourceForReplacement(path)
	if err != nil {
		return result, err
	}
	defer sourceLock.close()

	if err := os.Rename(tempPath+".readers", path+".readers"); err != nil {
		return result, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return result, err
	}
	if err := removeCompactArtifactIfExists(tempPath + ".writer"); err != nil {
		return result, err
	}
	if err := syncDirectoryPath(dir); err != nil {
		return result, err
	}

	cleanupTemp = false
	return result, nil
}

func reserveCompactTempPath(dir string, base string) (string, error) {
	file, err := os.CreateTemp(dir, "."+base+".compact-*.db")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

type sourceReplacementLock struct {
	file *os.File
}

func lockSourceForReplacement(path string) (*sourceReplacementLock, error) {
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file, true); err != nil {
		file.Close()
		if errors.Is(err, ErrDatabaseLocked) {
			return nil, ErrActiveReaders
		}
		return nil, err
	}
	return &sourceReplacementLock{file: file}, nil
}

func (l *sourceReplacementLock) close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := errors.Join(unlockFile(l.file), l.file.Close())
	l.file = nil
	return err
}

func removeCompactArtifactIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
