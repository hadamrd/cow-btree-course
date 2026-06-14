//go:build unix

package pagebtree

import "testing"

func TestMmapPlatformProfileReportsUnixMmapMechanics(t *testing.T) {
	profile := MmapPlatformProfile()
	if !profile.MmapSupported {
		t.Fatalf("MmapSupported = false on Unix profile: %+v", profile)
	}
	if !profile.MadviseSupported {
		t.Fatalf("MadviseSupported = false on Unix profile: %+v", profile)
	}
	if !profile.CacheResidencySupported || profile.CacheResidencyPrimitive != "mincore" {
		t.Fatalf("cache residency profile = %+v, want mincore support", profile)
	}
	if !profile.FileSpaceStatsSupported || profile.FileSpaceStatsPrimitive != "stat" {
		t.Fatalf("file-space profile = %+v, want stat support", profile)
	}
}
