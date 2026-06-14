//go:build linux

package pagebtree

func processStartTokenSupported() bool {
	return true
}

func bootIDTokenSupported() bool {
	return true
}

func mmapFileAdviceSupported() bool {
	return true
}

func mmapFileAdvicePrimitive() string {
	return "posix_fadvise"
}
