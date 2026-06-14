//go:build unix && !linux && !darwin

package pagebtree

func processStartTokenUnix(pid int) uint64 {
	return 0
}

func bootIDTokenUnix() uint64 {
	return 0
}
