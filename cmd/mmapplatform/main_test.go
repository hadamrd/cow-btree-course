package main

import (
	"encoding/json"
	"runtime"
	"testing"
)

func TestBuildReportIncludesPlatformProfile(t *testing.T) {
	data, err := json.Marshal(buildReport())
	if err != nil {
		t.Fatalf("Marshal report: %v", err)
	}

	var report platformReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("Unmarshal report: %v", err)
	}
	if report.Profile.GOOS != runtime.GOOS {
		t.Fatalf("GOOS = %q, want %q", report.Profile.GOOS, runtime.GOOS)
	}
	if report.Profile.PageSize == 0 {
		t.Fatalf("PageSize = 0 in report: %+v", report.Profile)
	}
	if report.Profile.HolePunch.Platform == "" {
		t.Fatalf("hole-punch platform = empty: %+v", report.Profile.HolePunch)
	}
}
