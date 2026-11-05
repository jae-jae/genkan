package update

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A tiny stand-in for a release binary: `version` prints "genkan <version>",
// anything else reports how it was started.
const tinySrc = `package main

import (
	"fmt"
	"os"
	"strings"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("genkan", version)
		return
	}
	fmt.Printf("tiny %s launched=%s args=%s\n", version, os.Getenv("GENKAN_LAUNCHED"), strings.Join(os.Args[1:], ","))
}
`

func buildTiny(t *testing.T, version string) []byte {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain")
	}
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "main.go"), []byte(tinySrc), 0o644)
	os.WriteFile(filepath.Join(src, "go.mod"), []byte("module tiny\n\ngo 1.21\n"), 0o644)
	out := filepath.Join(t.TempDir(), "genkan")
	cmd := exec.Command(goBin, "build", "-o", out, "-ldflags", "-X main.version="+version, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tarGzOf(t *testing.T, bin []byte) []byte {
	return makeTarGz(t, tarEntry{name: "genkan", body: string(bin)})
}

// TestRealBinaryEndToEnd extracts a real executable, runs its `version`
// check, and lets the real launcher (syscall.Exec in a child process) start it.
func TestRealBinaryEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix only")
	}
	bin := buildTiny(t, "v1.1.0")
	gh := newFakeGitHub(t)
	gh.setReleases(gh.signedRelease("v1.1.0", "amd64", tarGzOf(t, bin)))
	s := newTestSvc(t, gh, "v1.0.0", func(c *Config) { c.RunVersion = nil })
	writeFile(t, filepath.Join(s.dir, "genkan.json"), "{}")
	s.Check(context.Background(), true)
	if err := s.Apply("v1.1.0"); err != nil {
		t.Fatal(err)
	}
	if j := waitPhase(t, s, PhaseRestarting); j.Error != "" {
		t.Fatalf("%+v", j)
	}
	got, err := os.ReadFile(filepath.Join(s.dir, ".update", "v1.1.0", "genkan"))
	if err != nil || !bytes.Equal(got, bin) {
		t.Fatal("extracted binary differs")
	}

	// a binary that reports another version is refused by the real check
	bad := buildTiny(t, "v9.9.9")
	gh.setReleases(gh.signedRelease("v1.2.0", "amd64", tarGzOf(t, bad)))
	s.mu.Lock()
	s.job = Job{Phase: PhaseIdle}
	s.mu.Unlock()
	s.Check(context.Background(), true)
	if err := s.Apply("v1.2.0"); err != nil {
		t.Fatal(err)
	}
	if j := waitPhase(t, s, PhaseFailed); !strings.Contains(j.Error, `reports "genkan v9.9.9"`) {
		t.Fatalf("%+v", j)
	}

	// the real launcher, in a child process so that syscall.Exec replaces that process
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperLaunch$")
	cmd.Env = append(os.Environ(), "GENKAN_TEST_LAUNCH="+s.dir, "GENKAN_LAUNCHED=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "tiny v1.1.0 launched=1 args=-test.run=^TestHelperLaunch$") {
		t.Fatalf("child output:\n%s", out)
	}
	if st, _ := readState(s.dir); st.Attempts != 1 {
		t.Fatalf("state %+v", st)
	}
}

func TestHelperLaunch(t *testing.T) {
	dir := os.Getenv("GENKAN_TEST_LAUNCH")
	if dir == "" {
		t.Skip("helper process")
	}
	os.Unsetenv("GENKAN_LAUNCHED")
	Launch(dir, "v1.0.0")
	os.Stdout.WriteString("launcher returned\n")
}
