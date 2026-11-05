package update

import (
	"bufio"
	"io"
	"path/filepath"
	"strings"
)

// noexecInMountinfo reports whether the mount that holds path (the longest
// mount point that is a prefix of path) carries the noexec option. r is the
// content of /proc/self/mountinfo.
func noexecInMountinfo(r io.Reader, path string) bool {
	path = filepath.Clean(path)
	best, bestNoexec := -1, false
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		// 36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
		f := strings.Fields(sc.Text())
		if len(f) < 6 {
			continue
		}
		mp := unescapeMount(f[4])
		if !hasPathPrefix(path, mp) || len(mp) <= best {
			continue
		}
		best = len(mp)
		bestNoexec = false
		for _, o := range strings.Split(f[5], ",") {
			if o == "noexec" {
				bestNoexec = true
			}
		}
	}
	return bestNoexec
}

func hasPathPrefix(path, mp string) bool {
	if mp == "/" {
		return true
	}
	return path == mp || strings.HasPrefix(path, mp+"/")
}

// unescapeMount decodes the octal escapes (\040 for a space) of mountinfo.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			n := 0
			ok := true
			for _, c := range []byte(s[i+1 : i+4]) {
				if c < '0' || c > '7' {
					ok = false
					break
				}
				n = n*8 + int(c-'0')
			}
			if ok {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
