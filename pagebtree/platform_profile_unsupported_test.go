//go:build !unix

package pagebtree

import "testing"

func TestMmapPlatformProfileReportsNonUnixStubs(t *testing.T) {
	profile := MmapPlatformProfile()
	if profile.MmapSupported {
		t.Fatalf("MmapSupported = true on non-Unix profile: %+v", profile)
	}
	if profile.ReaderTableSupported || profile.WriterLockSupported {
		t.Fatalf("non-Unix profile reports mmap mechanics: %+v", profile)
	}
	if profile.UnsupportedReason == "" {
		t.Fatalf("UnsupportedReason = empty on non-Unix profile: %+v", profile)
	}
}
