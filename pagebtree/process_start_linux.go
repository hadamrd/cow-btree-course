//go:build linux

package pagebtree

import (
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
)

func processStartTokenUnix(pid int) uint64 {
	if pid <= 0 {
		return 0
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	text := string(data)
	endCommand := strings.LastIndex(text, ") ")
	if endCommand < 0 {
		return 0
	}
	fields := strings.Fields(text[endCommand+2:])
	if len(fields) <= 19 {
		return 0
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0
	}
	return start
}

func bootIDTokenUnix() uint64 {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return 0
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return 0
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(text))
	token := hash.Sum64()
	if token == 0 {
		return 1
	}
	return token
}
