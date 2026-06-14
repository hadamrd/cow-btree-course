//go:build unix && !linux && !darwin

package pagebtree

func processStartTokenUnix(pid int) uint64 {
	return 0
}
