package pagebtree

import "testing"

func TestParseLinuxMountInfoFindsLongestMount(t *testing.T) {
	const mountInfo = `20 1 8:1 / / rw,relatime - ext4 /dev/root rw
21 20 8:2 / /mnt/db\040dir rw,noatime shared:1 - xfs /dev/sdb1 rw,attr2
22 21 8:3 / /mnt/db\040dir/nested ro,relatime - tmpfs tmpfs rw,size=1024k
`

	evidence := parseLinuxMountInfo("/mnt/db dir/nested/tree.db", mountInfo)
	if evidence.MountPath != "/mnt/db dir/nested" {
		t.Fatalf("MountPath = %q, want nested mount", evidence.MountPath)
	}
	if evidence.FilesystemType != "tmpfs" {
		t.Fatalf("FilesystemType = %q, want tmpfs", evidence.FilesystemType)
	}
	if evidence.MountSource != "tmpfs" {
		t.Fatalf("MountSource = %q, want tmpfs", evidence.MountSource)
	}
	if evidence.MountOptions != "ro,relatime" {
		t.Fatalf("MountOptions = %q, want ro,relatime", evidence.MountOptions)
	}
}
