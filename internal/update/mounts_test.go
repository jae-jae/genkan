package update

import (
	"strings"
	"testing"
)

const mountinfo = `22 28 0:21 / /sys rw,nosuid,nodev,noexec,relatime shared:7 - sysfs sysfs rw
28 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw
40 28 0:35 / /data rw,nosuid,nodev,noexec,relatime - ext4 /dev/sdb1 rw
41 28 0:36 / /srv/with\040space rw,noexec - tmpfs tmpfs rw
42 28 0:37 / /srv rw,relatime - ext4 /dev/sdc rw
43 40 0:38 / /data/exec rw,relatime - ext4 /dev/sdd rw
`

func TestNoexecInMountinfo(t *testing.T) {
	cases := map[string]bool{
		"/data":                   true,
		"/data/sub/dir":           true,
		"/data/exec":              false, // nested exec-able mount wins
		"/data/exec/x":            false,
		"/data2":                  false, // not under /data
		"/srv/x":                  false,
		"/srv/with space/x":       true,
		"/home/me":                false, // root, exec
		"/sys/fs":                 true,
		"/":                       false,
		"/data/../srv/with space": true,
	}
	for p, want := range cases {
		if got := noexecInMountinfo(strings.NewReader(mountinfo), p); got != want {
			t.Errorf("%s: %v, want %v", p, got, want)
		}
	}
	if noexecInMountinfo(strings.NewReader(""), "/data") || noexecInMountinfo(strings.NewReader("garbage\n"), "/data") {
		t.Error("empty table")
	}
}
