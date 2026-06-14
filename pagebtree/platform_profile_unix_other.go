//go:build unix && !linux && !darwin

package pagebtree

func processStartTokenSupported() bool {
	return false
}

func bootIDTokenSupported() bool {
	return false
}

func mmapFileAdviceSupported() bool {
	return false
}

func mmapFileAdvicePrimitive() string {
	return ""
}
