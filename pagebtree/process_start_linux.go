//go:build linux

package pagebtree

import (
	"fmt"
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
