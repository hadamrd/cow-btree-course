package pagebtree

import (
	"runtime"
	"testing"
)

func TestMmapPlatformProfileReportsBuildContract(t *testing.T) {
	profile := MmapPlatformProfile()
	if profile.GOOS != runtime.GOOS {
		t.Fatalf("GOOS = %q, want %q", profile.GOOS, runtime.GOOS)
	}
	if profile.PageSize != PageSize {
		t.Fatalf("PageSize = %d, want %d", profile.PageSize, PageSize)
	}
	if profile.HolePunch.Platform == "" {
		t.Fatalf("HolePunch.Platform = empty: %+v", profile.HolePunch)
	}
	if profile.HolePunch != MmapHolePunchProfile() {
		t.Fatalf("HolePunch = %+v, want MmapHolePunchProfile", profile.HolePunch)
	}
	if profile.MmapSupported {
		if !profile.ReaderTableSupported {
			t.Fatalf("ReaderTableSupported = false on mmap-supported profile: %+v", profile)
		}
		if !profile.WriterLockSupported {
			t.Fatalf("WriterLockSupported = false on mmap-supported profile: %+v", profile)
		}
		if !profile.ReadOnlySharedLockSupported {
			t.Fatalf("ReadOnlySharedLockSupported = false on mmap-supported profile: %+v", profile)
		}
		if profile.MmapPrimitive == "" || profile.LockPrimitive == "" {
			t.Fatalf("missing mmap or lock primitive on mmap-supported profile: %+v", profile)
		}
		return
	}
	if profile.UnsupportedReason == "" {
		t.Fatalf("UnsupportedReason = empty on mmap-unsupported profile: %+v", profile)
	}
}
