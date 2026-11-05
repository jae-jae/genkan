//go:build linux

package update

import (
	"os"
	"path/filepath"
)

// dataDirNoexec reports whether dir lives on a noexec mount (per
// /proc/self/mountinfo); an unreadable table counts as exec-able.
func dataDirNoexec(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	defer f.Close()
	return noexecInMountinfo(f, abs)
}
