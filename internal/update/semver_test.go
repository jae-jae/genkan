package update

import "testing"

func TestParseVersion(t *testing.T) {
	good := map[string]Version{"v1.0.0": {1, 0, 0}, "v10.20.30": {10, 20, 30}, "v0.0.1": {0, 0, 1}}
	for s, want := range good {
		got, ok := ParseVersion(s)
		if !ok || got != want || got.String() != s {
			t.Errorf("ParseVersion(%q) = %v, %v", s, got, ok)
		}
	}
	for _, s := range []string{"", "dev", "1.2.3", "v1.2", "v1.2.3.4", "v1.2.3-rc1", "v1.2.3+meta", "sha-1a2b3c4", "v1.2.3-4-gabc-dirty",
		"v01.2.3", "v1.02.3", "v1.2.x", "v-1.2.3", "V1.2.3", " v1.2.3", "v1.2.3\n", "v9999999999.0.0", "v1..3"} {
		if _, ok := ParseVersion(s); ok {
			t.Errorf("ParseVersion(%q) should fail", s)
		}
	}
}

func TestCompareAndNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0}, {"v1.0.1", "v1.0.0", 1}, {"v1.0.0", "v1.0.1", -1},
		{"v1.10.0", "v1.9.9", 1}, {"v2.0.0", "v1.99.99", 1}, {"v0.9.0", "v1.0.0", -1},
	}
	for _, c := range cases {
		a, _ := ParseVersion(c.a)
		b, _ := ParseVersion(c.b)
		if got := a.Compare(b); got != c.want {
			t.Errorf("%s vs %s = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if !Newer("v1.2.0", "v1.1.9") || Newer("v1.1.9", "v1.2.0") || Newer("v1.0.0", "v1.0.0") {
		t.Error("Newer")
	}
	if Newer("v2.0.0", "dev") || Newer("dev", "v1.0.0") {
		t.Error("non-semver is never newer")
	}
	for _, v := range []string{"dev", "sha-1a2b3c4", "v1.2.3-4-gabc-dirty", ""} {
		if !IsDev(v) {
			t.Errorf("IsDev(%q)", v)
		}
	}
	if IsDev("v1.0.0") {
		t.Error("v1.0.0 is a release")
	}
}
