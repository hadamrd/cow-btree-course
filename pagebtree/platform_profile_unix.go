//go:build unix

package pagebtree

import "runtime"

func mmapPlatformProfile() MmapPlatformCapability {
	return MmapPlatformCapability{
		GOOS:                        runtime.GOOS,
		MmapSupported:               true,
		MmapPrimitive:               "mmap",
		WriterLockSupported:         true,
		ReadOnlySharedLockSupported: true,
		LockPrimitive:               "flock",
		ReaderTableSupported:        true,
		ProcessStartTokenSupported:  processStartTokenSupported(),
		BootIDTokenSupported:        bootIDTokenSupported(),
		MadviseSupported:            true,
		FileAdviceSupported:         mmapFileAdviceSupported(),
		FileAdvicePrimitive:         mmapFileAdvicePrimitive(),
		CacheResidencySupported:     true,
		CacheResidencyPrimitive:     "mincore",
		FileSpaceStatsSupported:     true,
		FileSpaceStatsPrimitive:     "stat",
		DirectorySyncSupported:      true,
	}
}
