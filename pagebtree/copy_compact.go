package pagebtree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrCopyCompactDestinationExists = errors.New("copy compact destination already exists")

const compactInitialMaxPages = 2
const copyCompactMetaPages = 2

type CopyCompactResult struct {
	Keys                      int
	SourceAllocatedPages      int
	DestinationAllocatedPages int
	SourceFileBytes           int64
	DestinationFileBytes      int64
}

// CopyCompactMmap copies the live contents of one mmap tree into a new mmap
// file and compacts the destination tail before returning.
//
// The source is opened read-only, so the copy sees one recovered root. The
// destination path and its sidecar lock/table files must not already exist; this
// API is intentionally conservative and never overwrites an existing file.
func CopyCompactMmap(srcPath string, dstPath string, options MmapOptions) (CopyCompactResult, error) {
	var result CopyCompactResult
	if err := validateCopyCompactPaths(srcPath, dstPath); err != nil {
		return result, err
	}

	sourceInfo, err := os.Stat(srcPath)
	if err != nil {
		return result, err
	}
	result.SourceFileBytes = sourceInfo.Size()
	result.SourceAllocatedPages = dataPagesForFileSize(sourceInfo.Size())

	src, err := OpenMmapReadOnly(srcPath)
	if err != nil {
		return result, err
	}
	defer src.Close()
	if err := src.Check(); err != nil {
		return result, err
	}

	if options.MaxPages == 0 {
		options.MaxPages = compactInitialMaxPages
	}
	if options.KeyOrder == 0 {
		options.KeyOrder = KeyOrderBytewise
	}

	cleanupDestination := true
	defer func() {
		if cleanupDestination {
			removeCopyCompactArtifacts(dstPath)
		}
	}()

	dst, err := OpenMmap(dstPath, options)
	if err != nil {
		return result, err
	}
	closeDestination := func() error {
		err := dst.Close()
		dst = nil
		return err
	}
	defer func() {
		if dst != nil {
			_ = closeDestination()
		}
	}()

	src.RangeBytes(func(key []byte, value []byte) bool {
		dst.PutBytes(key, value)
		result.Keys++
		return true
	})
	if err := dst.Check(); err != nil {
		return result, err
	}
	if err := dst.Compact(); err != nil {
		return result, err
	}
	result.DestinationAllocatedPages = int(dst.arena.maxPages)
	if err := closeDestination(); err != nil {
		return result, err
	}

	destinationInfo, err := os.Stat(dstPath)
	if err != nil {
		return result, err
	}
	result.DestinationFileBytes = destinationInfo.Size()
	result.DestinationAllocatedPages = dataPagesForFileSize(destinationInfo.Size())
	cleanupDestination = false
	return result, nil
}

func validateCopyCompactPaths(srcPath string, dstPath string) error {
	if srcPath == "" {
		return fmt.Errorf("copy compact source path is empty")
	}
	if dstPath == "" {
		return fmt.Errorf("copy compact destination path is empty")
	}
	absSrc, err := filepath.Abs(srcPath)
	if err != nil {
		return err
	}
	absDst, err := filepath.Abs(dstPath)
	if err != nil {
		return err
	}
	if absSrc == absDst {
		return fmt.Errorf("%w: destination is the source path", ErrCopyCompactDestinationExists)
	}
	for _, path := range copyCompactArtifacts(dstPath) {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%w: %s", ErrCopyCompactDestinationExists, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func copyCompactArtifacts(path string) []string {
	return []string{path, path + ".writer", path + ".readers"}
}

func removeCopyCompactArtifacts(path string) {
	for _, artifact := range copyCompactArtifacts(path) {
		_ = os.Remove(artifact)
	}
}

func dataPagesForFileSize(size int64) int {
	pages := int(size / PageSize)
	if pages <= 0 {
		return 0
	}
	if pages <= copyCompactMetaPages {
		return 0
	}
	return pages - copyCompactMetaPages
}
