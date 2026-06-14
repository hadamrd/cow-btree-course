//go:build !unix

package pagebtree

import "runtime"

func mmapPlatformProfile() MmapPlatformCapability {
	return MmapPlatformCapability{
		GOOS:              runtime.GOOS,
		UnsupportedReason: "mmap page storage is only available on Unix-like platforms in this lab build",
	}
}
