//go:build !linux

package update

func dataDirNoexec(string) bool { return false }
