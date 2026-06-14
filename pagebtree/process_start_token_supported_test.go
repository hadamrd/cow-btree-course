//go:build linux || darwin

package pagebtree

import (
	"os"
	"testing"
)

func TestProcessStartTokenReportsCurrentProcess(t *testing.T) {
	pid := os.Getpid()
	first := processStartToken(pid)
	second := processStartToken(pid)
	if first == 0 {
		t.Fatalf("processStartToken(%d) = 0, want stable owner token", pid)
	}
	if second != first {
		t.Fatalf("processStartToken(%d) changed from %d to %d", pid, first, second)
	}
}

func TestBootIDTokenReportsCurrentBoot(t *testing.T) {
	first := bootIDToken()
	second := bootIDToken()
	if first == 0 {
		t.Fatalf("bootIDToken() = 0, want stable boot/session token")
	}
	if second != first {
		t.Fatalf("bootIDToken() changed from %d to %d", first, second)
	}
}
