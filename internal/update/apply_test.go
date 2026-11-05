package update

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// releaseFixture: running v1.0.0, release v1.1.0 properly signed.
func applyFixture(t *testing.T, mod func(*Config)) *testSvc {
	t.Helper()
	gh := newFakeGitHub(t)
	gh.setReleases(gh.signedRelease("v1.1.0", "amd64", goodTarball(t, "v1.1.0")))
	s := newTestSvc(t, gh, "v1.0.0", mod)
	writeFile(t, filepath.Join(s.dir, "genkan.json"), `{"data":1}`)
	writeFile(t, filepath.Join(s.dir, "plugins", "p.js"), "plugin")
	if err := s.Check(context.Background(), true); err != nil && !errors.Is(err, ErrCheckDisabled) {
		t.Fatal(err)
	}
	return s
}

func waitPhase(t *testing.T, s *testSvc, phase string) Job {
	t.Helper()
	var j Job
	waitFor(t, "phase "+phase, func() bool { j = s.Status().Job; return j.Phase == phase })
	return j
}

func TestApplyHappyPath(t *testing.T) {
	s := applyFixture(t, nil)
	if err := s.Apply("v1.1.0"); err != nil {
		t.Fatal(err)
	}
	j := waitPhase(t, s, PhaseRestarting)
	if j.Version != "v1.1.0" || j.Progress != 1 || j.Error != "" {
		t.Fatalf("job %+v", j)
	}
	select {
	case <-s.RestartRequested():
	case <-time.After(time.Second):
		t.Fatal("no restart request")
	}
	if got := readFile(t, filepath.Join(s.dir, ".update", "v1.1.0", "genkan")); !strings.Contains(got, "genkan v1.1.0") {
		t.Fatal("binary not installed")
	}
	fi, _ := os.Stat(filepath.Join(s.dir, ".update", "v1.1.0", "genkan"))
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode())
	}
	st, err := readState(s.dir)
	if err != nil || st.Version != "v1.1.0" || st.Status != "pending" || st.Attempts != 0 || st.From != "v1.0.0" || st.Backup != "backup-v1.1.0" || st.At == 0 {
		t.Fatalf("state %+v %v", st, err)
	}
	if readFile(t, filepath.Join(s.dir, ".update", "backup-v1.1.0", "genkan.json")) != `{"data":1}` ||
		readFile(t, filepath.Join(s.dir, ".update", "backup-v1.1.0", "plugins", "p.js")) != "plugin" {
		t.Fatal("backup")
	}
	if _, err := os.Stat(filepath.Join(s.dir, ".update", "download-v1.1.0.tar.gz")); err == nil {
		t.Fatal("download left behind")
	}

	// the final step: the store changed after the first backup; the refresh picks it up, then the exec
	writeFile(t, filepath.Join(s.dir, "genkan.json"), `{"data":2}`)
	if err := s.Restart(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(s.dir, ".update", "backup-v1.1.0", "genkan.json")) != `{"data":2}` {
		t.Fatal("backup not refreshed")
	}
	if len(s.execs) != 1 {
		t.Fatalf("execs %v", s.execs)
	}
	c := s.execs[0]
	if c.path != "/opt/genkan" || strings.Join(c.argv, " ") != "/opt/genkan --flag" {
		t.Fatalf("exec %+v", c)
	}
	// GENKAN_LAUNCHED must be dropped so that the launcher runs again
	if strings.Join(c.envs, ",") != "A=1" {
		t.Fatalf("env %v", c.envs)
	}
}

func TestApplyEndToEndWithLauncherAndRollback(t *testing.T) {
	s := applyFixture(t, nil)
	if err := s.Apply("v1.1.0"); err != nil {
		t.Fatal(err)
	}
	<-s.RestartRequested()
	// the new version migrates the data
	writeFile(t, filepath.Join(s.dir, "genkan.json"), `{"data":"migrated"}`)

	l := Launcher{DataDir: s.dir, Version: "v1.0.0", Environ: []string{"A=1"}, Args: []string{"/genkan"}, Logf: func(string, ...any) {},
		Exec: func(string, []string, []string) error { return nil }}
	if l.Run() != OutcomeExec { // first start of the new binary
		t.Fatal("first launch")
	}
	// ... which crashes before confirming; the container restarts the old image
	if l.Run() != OutcomeRolledBack {
		t.Fatal("second launch should roll back")
	}
	if readFile(t, filepath.Join(s.dir, "genkan.json")) != `{"data":1}` {
		t.Fatal("data not restored")
	}
	st := s.Status()
	if st.RolledBack != "v1.1.0" {
		t.Fatalf("rolled_back %q", st.RolledBack)
	}
}

func TestApplyValidation(t *testing.T) {
	s := applyFixture(t, nil)
	for _, v := range []string{"", "v9.9.9", "v1.0.0", "v0.9.0", "latest", "../x"} {
		if err := s.Apply(v); !errors.Is(err, ErrUnknownVersion) {
			t.Errorf("Apply(%q) = %v", v, err)
		}
	}
	if s.Status().Job.Phase != PhaseIdle {
		t.Fatal("job started")
	}
}

func TestApplyUnsupported(t *testing.T) {
	cases := []struct {
		reason string
		mod    func(*Config)
	}{
		{"dev", func(c *Config) { c.Version = "dev" }},
		{"disabled", func(c *Config) {
			c.Getenv = func(k string) string {
				if k == EnvSelfUpdate {
					return "off"
				}
				return ""
			}
		}},
		{"platform", func(c *Config) { c.GOOS = "darwin" }},
		{"platform", func(c *Config) { c.GOARCH = "riscv64" }},
		{"noexec", func(c *Config) { c.NoExec = func(string) bool { return true } }},
	}
	for _, c := range cases {
		s := applyFixture(t, c.mod)
		st := s.Status()
		if st.Apply.Supported || st.Apply.Reason != c.reason {
			t.Errorf("%s: apply = %+v", c.reason, st.Apply)
		}
		err := s.Apply("v1.1.0")
		var u *UnsupportedError
		if !errors.As(err, &u) || u.Reason != c.reason {
			t.Errorf("%s: err = %v", c.reason, err)
		}
		if st.Job.Phase != PhaseIdle {
			t.Errorf("%s: job started", c.reason)
		}
	}
}

func TestApplyBusy(t *testing.T) {
	release := make(chan struct{})
	s := applyFixture(t, nil)
	s.cfg.RunVersion = func(ctx context.Context, bin string) (string, error) {
		<-release
		return "genkan v1.1.0", nil
	}
	if err := s.Apply("v1.1.0"); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, s, PhaseInstalling)
	if err := s.Apply("v1.1.0"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second apply: %v", err)
	}
	close(release)
	waitPhase(t, s, PhaseRestarting)
	if err := s.Apply("v1.1.0"); !errors.Is(err, ErrBusy) {
		t.Fatalf("apply while restarting: %v", err)
	}
}

func TestApplyFailures(t *testing.T) {
	type tc struct {
		name  string
		build func(g *fakeGitHub) fakeRelease
		mod   func(*Config)
		want  string
	}
	tar := func(tag string) []byte { return goodTarball(t, tag) }
	tests := []tc{
		{name: "bad signature", want: "signature", build: func(g *fakeGitHub) fakeRelease {
			r := g.signedRelease("v1.1.0", "amd64", tar("v1.1.0"))
			other := newFakeGitHub(t)
			r.Files["checksums.txt.sig"] = other.signedRelease("v1.1.0", "amd64", tar("v1.1.0")).Files["checksums.txt.sig"]
			return r
		}},
		{name: "bad sha", want: "checksum mismatch", build: func(g *fakeGitHub) fakeRelease {
			r := g.signedRelease("v1.1.0", "amd64", tar("v1.1.0"))
			r.Files[tarballName("v1.1.0", "amd64")] = tar("v1.1.1")
			return r
		}},
		{name: "missing tarball", want: "no genkan_1.1.0_linux_amd64.tar.gz asset", build: func(g *fakeGitHub) fakeRelease {
			r := g.signedRelease("v1.1.0", "arm64", tar("v1.1.0"))
			return r
		}},
		{name: "missing sig", want: "no checksums.txt.sig asset", build: func(g *fakeGitHub) fakeRelease {
			r := g.signedRelease("v1.1.0", "amd64", tar("v1.1.0"))
			delete(r.Files, "checksums.txt.sig")
			return r
		}},
		{name: "extra entry", want: "unexpected entry", build: func(g *fakeGitHub) fakeRelease {
			return g.signedRelease("v1.1.0", "amd64", makeTarGz(t, tarEntry{name: "genkan", body: "echo genkan v1.1.0"}, tarEntry{name: "evil", body: "x"}))
		}},
		{name: "wrong version", want: `reports "genkan v1.0.5"`, build: func(g *fakeGitHub) fakeRelease {
			return g.signedRelease("v1.1.0", "amd64", tar("v1.0.5"))
		}},
		{name: "does not run", want: "does not run", build: func(g *fakeGitHub) fakeRelease {
			return g.signedRelease("v1.1.0", "amd64", makeTarGz(t, tarEntry{name: "genkan", body: "garbage"}))
		}},
		{name: "bad pubkey env", want: EnvPubKey, mod: func(c *Config) {
			c.PubKey = nil
			c.Getenv = func(k string) string {
				if k == EnvPubKey {
					return "nope"
				}
				return ""
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := newFakeGitHub(t)
			if tt.build != nil {
				gh.setReleases(tt.build(gh))
			} else {
				gh.setReleases(gh.signedRelease("v1.1.0", "amd64", tar("v1.1.0")))
			}
			s := newTestSvc(t, gh, "v1.0.0", tt.mod)
			writeFile(t, filepath.Join(s.dir, "genkan.json"), "{}")
			s.Check(context.Background(), true)
			err := s.Apply("v1.1.0")
			if tt.name == "bad pubkey env" {
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("err %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			j := waitPhase(t, s, PhaseFailed)
			if !strings.Contains(j.Error, tt.want) {
				t.Fatalf("error %q, want %q", j.Error, tt.want)
			}
			// nothing stays behind and nothing restarts
			ents, _ := os.ReadDir(filepath.Join(s.dir, ".update"))
			for _, e := range ents {
				if e.Name() != "check.json" {
					t.Errorf("left behind: %s", e.Name())
				}
			}
			select {
			case <-s.RestartRequested():
				t.Fatal("restart requested after a failure")
			default:
			}
			// a failed job can be retried
			gh.setReleases(gh.signedRelease("v1.1.0", "amd64", tar("v1.1.0")))
			s.Check(context.Background(), true)
			if err := s.Apply("v1.1.0"); err != nil {
				t.Fatalf("retry: %v", err)
			}
			waitPhase(t, s, PhaseRestarting)
		})
	}
}

func TestApplyOversizedTarball(t *testing.T) {
	gh := newFakeGitHub(t)
	r := gh.signedRelease("v1.1.0", "amd64", goodTarball(t, "v1.1.0"))
	gh.setReleases(r)
	s := newTestSvc(t, gh, "v1.0.0", nil)
	s.Check(context.Background(), true)
	// serve the tarball through the extra handler as an endless body
	gh.mu.Lock()
	gh.extra = func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1<<20)
		for i := 0; i < 101; i++ {
			if _, err := w.Write(buf); err != nil {
				return
			}
		}
	}
	gh.mu.Unlock()
	s.Service.mu.Lock()
	for i := range s.cache.Releases[0].Assets {
		if strings.HasSuffix(s.cache.Releases[0].Assets[i].Name, ".tar.gz") {
			s.cache.Releases[0].Assets[i].URL = gh.ts.URL + "/x/big"
		}
	}
	s.Service.mu.Unlock()
	if err := s.Apply("v1.1.0"); err != nil {
		t.Fatal(err)
	}
	j := waitPhase(t, s, PhaseFailed)
	if !strings.Contains(j.Error, "100 MiB limit") {
		t.Fatalf("error %q", j.Error)
	}
}

func TestApplyProgress(t *testing.T) {
	gh := newFakeGitHub(t)
	bigBin := strings.Repeat("#", 3<<20)
	r := gh.signedRelease("v1.1.0", "amd64", makeTarGz(t, tarEntry{name: "genkan", body: bigBin + "\necho genkan v1.1.0\n"}))
	gh.setReleases(r)
	var seen []float64
	s := newTestSvc(t, gh, "v1.0.0", nil)
	writeFile(t, filepath.Join(s.dir, "genkan.json"), "{}")
	s.Check(context.Background(), true)
	s.Apply("v1.1.0")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j := s.Status().Job
		seen = append(seen, j.Progress)
		if j.Phase == PhaseRestarting || j.Phase == PhaseFailed {
			break
		}
		time.Sleep(time.Millisecond)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] < seen[i-1] {
			t.Fatalf("progress went backwards: %v", seen)
		}
	}
	if s.Status().Job.Phase != PhaseRestarting {
		t.Fatalf("job %+v", s.Status().Job)
	}
}

func TestConfirm(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, ".update")
	writeFile(t, filepath.Join(dir, "v1.1.0", "genkan"), "bin")
	writeFile(t, filepath.Join(dir, "v1.0.5", "genkan"), "old")
	writeFile(t, filepath.Join(dir, "backup-v1.1.0", "genkan.json"), "{}")
	writeFile(t, filepath.Join(dir, "check.json"), "{}")
	writeState(data, State{Version: "v1.1.0", Status: "pending", Attempts: 1, From: "v1.0.0", Backup: "backup-v1.1.0"})

	var probes int
	mk := func(env string, probe func(context.Context) error) *Service {
		return New(Config{DataDir: data, Version: "v1.1.0", ConfirmAfter: 30 * time.Millisecond, Probe: probe, Logf: func(string, ...any) {},
			Getenv: func(k string) string {
				if k == LaunchedEnv {
					return env
				}
				return ""
			}, FirstCheckDelay: time.Hour})
	}
	// not started by the launcher: nothing happens
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mk("", nil).confirmLoop(ctx)
	time.Sleep(120 * time.Millisecond)
	if st, _ := readState(data); st.Status != "pending" {
		t.Fatal("confirmed without GENKAN_LAUNCHED")
	}

	// a failing probe delays the confirmation
	s := mk("1", func(context.Context) error {
		probes++
		if probes < 3 {
			return errors.New("not yet")
		}
		return nil
	})
	done := make(chan struct{})
	go func() { s.confirmLoop(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("not confirmed")
	}
	if probes < 3 {
		t.Fatalf("probes %d", probes)
	}
	st, _ := readState(data)
	if st.Status != "confirmed" {
		t.Fatalf("state %+v", st)
	}
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "check.json,state.json,v1.1.0" {
		t.Fatalf("after confirm: %v", names)
	}
}

func TestConfirmNotForOtherVersion(t *testing.T) {
	data := t.TempDir()
	writeState(data, State{Version: "v1.2.0", Status: "pending", Attempts: 1})
	s := New(Config{DataDir: data, Version: "v1.1.0", ConfirmAfter: time.Millisecond, Logf: func(string, ...any) {},
		Getenv: func(k string) string {
			if k == LaunchedEnv {
				return "1"
			}
			return ""
		}})
	s.confirmLoop(context.Background())
	if st, _ := readState(data); st.Status != "pending" {
		t.Fatal("confirmed a different version")
	}
}

func TestStatusAfterDismiss(t *testing.T) {
	s := applyFixture(t, nil)
	s.dismiss = "v1.1.0"
	if st := s.Status(); st.Dismissed != "v1.1.0" || !st.Available || st.Latest != "v1.1.0" {
		t.Fatalf("%+v", st)
	}
}

// The binary we run from (state.from of the next update) must survive applying
// and failing: it is the rollback target until the new version confirms.
func TestApplyKeepsRunningVersionDir(t *testing.T) {
	s := applyFixture(t, func(c *Config) { c.Version = "v1.0.5" })
	own := filepath.Join(s.dir, ".update", "v1.0.5", "genkan")
	writeFile(t, own, "running binary")
	if err := s.Apply("v1.1.0"); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, s, PhaseRestarting)
	if readFile(t, own) != "running binary" {
		t.Fatal("running version dir removed by apply")
	}
	if st, _ := readState(s.dir); st.From != "v1.0.5" {
		t.Fatalf("state %+v", st)
	}
	// a failing job removes only its own dirs
	s2 := applyFixture(t, func(c *Config) { c.Version = "v1.0.5" })
	writeFile(t, filepath.Join(s2.dir, ".update", "v1.0.5", "genkan"), "running binary")
	s2.cfg.RunVersion = func(context.Context, string) (string, error) { return "genkan v0.0.1", nil }
	s2.Apply("v1.1.0")
	waitPhase(t, s2, PhaseFailed)
	if readFile(t, filepath.Join(s2.dir, ".update", "v1.0.5", "genkan")) != "running binary" {
		t.Fatal("running version dir removed by a failed job")
	}
}
