//go:build darwin

package pagebtree

import "golang.org/x/sys/unix"

func processStartTokenUnix(pid int) uint64 {
	if pid <= 0 {
		return 0
	}
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || proc == nil {
		return 0
	}
	sec := uint64(proc.Proc.P_starttime.Sec)
	usec := uint64(proc.Proc.P_starttime.Usec)
	if sec == 0 && usec == 0 {
		return 0
	}
	return sec*1_000_000 + usec
}
