//go:build darwin

package pagebtree

func processStartTokenSupported() bool {
	return true
}

func bootIDTokenSupported() bool {
	return true
}

func mmapFileAdviceSupported() bool {
	return false
}

func mmapFileAdvicePrimitive() string {
	return ""
}
