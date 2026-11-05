// Package update implements Genkan's release check and self-update
// (docs/UPDATES.md): semver handling, the GitHub release checker, the
// verified download, the apply job and the start-up launcher.
package update

import (
	"strconv"
	"strings"
)

// Version is a parsed release version vMAJOR.MINOR.PATCH.
type Version struct{ Major, Minor, Patch int }

// ParseVersion parses a plain release version "vX.Y.Z". Anything else
// (pre-releases, "dev", "sha-abc1234", "v1.2.3-4-gabc-dirty", missing "v",
// leading zeros) is rejected.
func ParseVersion(s string) (Version, bool) {
	if !strings.HasPrefix(s, "v") {
		return Version{}, false
	}
	parts := strings.Split(s[1:], ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	var n [3]int
	for i, p := range parts {
		if p == "" || len(p) > 9 || (len(p) > 1 && p[0] == '0') {
			return Version{}, false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return Version{}, false
			}
		}
		n[i], _ = strconv.Atoi(p)
	}
	return Version{n[0], n[1], n[2]}, true
}

func (v Version) String() string {
	return "v" + strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
}

// Compare returns -1, 0 or 1.
func (v Version) Compare(o Version) int {
	for _, p := range [3][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		switch {
		case p[0] < p[1]:
			return -1
		case p[0] > p[1]:
			return 1
		}
	}
	return 0
}

// IsDev reports whether version is a development build (not a plain vX.Y.Z).
func IsDev(version string) bool {
	_, ok := ParseVersion(version)
	return !ok
}

// Newer reports whether a is a valid version newer than b (both must parse).
func Newer(a, b string) bool {
	va, ok1 := ParseVersion(a)
	vb, ok2 := ParseVersion(b)
	return ok1 && ok2 && va.Compare(vb) > 0
}
