package pagebtree

import "strings"

func parseLinuxMountInfo(path, mountInfo string) mmapFilesystemEvidence {
	var best mmapFilesystemEvidence
	for _, line := range strings.Split(mountInfo, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if separator < 6 || len(fields) <= separator+3 {
			continue
		}
		mountPath := linuxMountInfoUnescape(fields[4])
		if !pathWithinMount(path, mountPath) || len(mountPath) <= len(best.MountPath) {
			continue
		}
		best = mmapFilesystemEvidence{
			FilesystemType: fields[separator+1],
			MountPath:      mountPath,
			MountSource:    linuxMountInfoUnescape(fields[separator+2]),
			MountOptions:   fields[5],
		}
	}
	return best
}

func pathWithinMount(path, mountPath string) bool {
	if mountPath == "/" {
		return strings.HasPrefix(path, "/")
	}
	return path == mountPath || strings.HasPrefix(path, mountPath+"/")
}

func linuxMountInfoUnescape(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+3 < len(value) && isOctalDigit(value[i+1]) && isOctalDigit(value[i+2]) && isOctalDigit(value[i+3]) {
			out.WriteByte((value[i+1]-'0')*64 + (value[i+2]-'0')*8 + value[i+3] - '0')
			i += 3
			continue
		}
		out.WriteByte(value[i])
	}
	return out.String()
}

func isOctalDigit(value byte) bool {
	return value >= '0' && value <= '7'
}
