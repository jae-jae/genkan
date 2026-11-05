package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func relFiles() map[string][]byte { return map[string][]byte{"checksums.txt": []byte("x")} }

func TestCheckFiltersAndSorts(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.setReleases(
		fakeRelease{Tag: "v1.1.0", Body: "one one", Files: relFiles()},
		fakeRelease{Tag: "v1.3.0", Body: "three"},
		fakeRelease{Tag: "v1.4.0", Draft: true},
		fakeRelease{Tag: "v1.5.0", Prerelease: true},
		fakeRelease{Tag: "v1.6.0-rc1"},
		fakeRelease{Tag: "nightly"},
		fakeRelease{Tag: "v1.2.0"},
		fakeRelease{Tag: "v1.0.0"}, // running
		fakeRelease{Tag: "v0.9.0"}, // older
		fakeRelease{Tag: "v1.2.0"}, // duplicate
	)
	s := newTestSvc(t, gh, "v1.0.0", nil)
	if err := s.Check(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	var got []string
	for _, r := range st.Releases {
		got = append(got, r.Version)
	}
	if strings.Join(got, ",") != "v1.3.0,v1.2.0,v1.1.0" {
		t.Fatalf("releases = %v", got)
	}
	if st.Latest != "v1.3.0" || !st.Available || st.Error != "" || st.CheckedAt == 0 || st.Current != "v1.0.0" || st.Dev {
		t.Fatalf("status = %+v", st)
	}
	if st.Releases[2].Notes != "one one" || st.Releases[2].PublishedAt != 1762250400000 || !strings.HasSuffix(st.Releases[2].URL, "/releases/tag/v1.1.0") {
		t.Fatalf("release = %+v", st.Releases[2])
	}
	if st.Repo != RepoURL || st.Apply.Supported != true {
		t.Fatalf("repo/apply: %+v", st)
	}
}

func TestStatusReleasesNeverNull(t *testing.T) {
	gh := newFakeGitHub(t)
	s := newTestSvc(t, gh, "v1.0.0", nil)
	st := s.Status()
	if st.Releases == nil || st.Latest != "" || st.Available || st.CheckedAt != 0 {
		t.Fatalf("%+v", st)
	}
	b, _ := json.Marshal(st)
	if !strings.Contains(string(b), `"releases":[]`) || !strings.Contains(string(b), `"job":{"phase":"idle","version":"","progress":0,"error":""}`) {
		t.Fatalf("%s", b)
	}
	d := newTestSvc(t, gh, "dev", nil).Status()
	if d.Releases == nil || !d.Dev {
		t.Fatalf("%+v", d)
	}
}

func TestCheckETag(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.setReleases(fakeRelease{Tag: "v1.1.0"})
	s := newTestSvc(t, gh, "v1.0.0", nil)
	for i := 0; i < 3; i++ {
		if err := s.Check(context.Background(), false); err != nil {
			t.Fatal(err)
		}
	}
	if gh.hits != 3 || gh.gotINM[0] != "" || gh.gotINM[1] != gh.etag || gh.gotINM[2] != gh.etag {
		t.Fatalf("hits=%d inm=%v", gh.hits, gh.gotINM)
	}
	if st := s.Status(); st.Latest != "v1.1.0" || st.Error != "" {
		t.Fatalf("after 304: %+v", st)
	}
	gh.setReleases(fakeRelease{Tag: "v1.2.0"}, fakeRelease{Tag: "v1.1.0"})
	if err := s.Check(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Latest != "v1.2.0" || len(st.Releases) != 2 {
		t.Fatalf("after change: %+v", st)
	}
}

func TestCheckPersistedCache(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.setReleases(fakeRelease{Tag: "v1.1.0", Files: relFiles()})
	s := newTestSvc(t, gh, "v1.0.0", nil)
	if err := s.Check(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, ".update", "check.json")); err != nil {
		t.Fatal(err)
	}
	// a new process on the same data dir knows the result and the ETag without a fetch
	s2 := newTestSvc(t, gh, "v1.0.0", func(c *Config) { c.DataDir = s.dir })
	st := s2.Status()
	if st.Latest != "v1.1.0" || st.CheckedAt == 0 {
		t.Fatalf("not restored: %+v", st)
	}
	if err := s2.Check(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if gh.gotINM[len(gh.gotINM)-1] != gh.etag {
		t.Fatalf("ETag not reused: %v", gh.gotINM)
	}
	// once the binary is the new version nothing is "available" any more
	s3 := newTestSvc(t, gh, "v1.1.0", func(c *Config) { c.DataDir = s.dir })
	if st := s3.Status(); st.Available || st.Latest != "" || len(st.Releases) != 0 {
		t.Fatalf("up to date: %+v", st)
	}
	// a cache of another API is ignored
	s4 := newTestSvc(t, gh, "v1.0.0", func(c *Config) { c.DataDir = s.dir; c.API = gh.ts.URL + "/other" })
	if st := s4.Status(); st.Available {
		t.Fatalf("foreign cache used: %+v", st)
	}
}

func TestRunSkipsFetchWhenCacheFresh(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.setReleases(fakeRelease{Tag: "v1.1.0"})
	s := newTestSvc(t, gh, "v1.0.0", nil)
	s.Check(context.Background(), false)
	hits := gh.hits
	s2 := newTestSvc(t, gh, "v1.0.0", func(c *Config) { c.DataDir = s.dir; c.FirstCheckDelay = time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	go s2.Run(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()
	if gh.hits != hits {
		t.Fatal("fresh cache should delay the first check")
	}
}

func TestRunChecksPeriodically(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.setReleases(fakeRelease{Tag: "v1.1.0"})
	s := newTestSvc(t, gh, "v1.0.0", func(c *Config) { c.FirstCheckDelay = 5 * time.Millisecond; c.Interval = 20 * time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	waitFor(t, "3 checks", func() bool { gh.mu.Lock(); defer gh.mu.Unlock(); return gh.hits >= 3 })
	// turning the setting off stops the fetching
	s.enabled.Store(false)
	time.Sleep(60 * time.Millisecond)
	gh.mu.Lock()
	h := gh.hits
	gh.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if gh.hits != h {
		t.Fatalf("checked while update_check is off: %d -> %d", h, gh.hits)
	}
}

func TestCheckDisabledCases(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.setReleases(fakeRelease{Tag: "v1.1.0"})

	// setting off: periodic check refused, manual works
	s := newTestSvc(t, gh, "v1.0.0", nil)
	s.enabled.Store(false)
	if err := s.Check(context.Background(), false); err != ErrCheckDisabled {
		t.Fatalf("auto with setting off: %v", err)
	}
	if st := s.Status(); st.CheckEnabled || gh.hits != 0 {
		t.Fatalf("%+v hits=%d", st, gh.hits)
	}
	if err := s.Check(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if gh.hits != 1 {
		t.Fatal("manual check should fetch")
	}

	// env off: both refused
	e := newTestSvc(t, gh, "v1.0.0", func(c *Config) {
		c.Getenv = func(k string) string {
			if k == EnvCheck {
				return "off"
			}
			return ""
		}
	})
	if e.Check(context.Background(), true) != ErrCheckDisabled || e.Check(context.Background(), false) != ErrCheckDisabled {
		t.Fatal("env off should refuse")
	}
	if !e.Status().EnvDisabled {
		t.Fatal("env_disabled")
	}

	// dev: both refused
	d := newTestSvc(t, gh, "v1.2.3-4-gabc-dirty", nil)
	if d.Check(context.Background(), true) != ErrCheckDisabled {
		t.Fatal("dev should refuse")
	}
	if gh.hits != 1 {
		t.Fatal("no fetch expected")
	}
	// Run returns at once for dev builds
	done := make(chan struct{})
	go func() { d.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run should return for dev")
	}
}

func TestCheckSendsTokenOnlyToAPI(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.setReleases(fakeRelease{Tag: "v1.1.0"})
	s := newTestSvc(t, gh, "v1.0.0", func(c *Config) { c.Token = "ghp_secret" })
	s.Check(context.Background(), false)
	if gh.gotAuth[0] != "Bearer ghp_secret" {
		t.Fatalf("auth = %q", gh.gotAuth[0])
	}
	s2 := newTestSvc(t, gh, "v1.0.0", nil)
	s2.Check(context.Background(), false)
	if gh.gotAuth[1] != "" {
		t.Fatalf("auth without token = %q", gh.gotAuth[1])
	}
}

func TestCheckErrors(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.setReleases(fakeRelease{Tag: "v1.1.0"})
	s := newTestSvc(t, gh, "v1.0.0", nil)
	s.Check(context.Background(), false)

	check := func(status int, token, want string) {
		t.Helper()
		gh.mu.Lock()
		gh.status = status
		gh.mu.Unlock()
		s.cfg.Token = token
		if err := s.Check(context.Background(), true); err != nil {
			t.Fatalf("a GitHub failure is not an API error: %v", err)
		}
		st := s.Status()
		if !strings.Contains(st.Error, want) {
			t.Fatalf("HTTP %d: error %q, want %q", status, st.Error, want)
		}
		// the last good list stays
		if st.Latest != "v1.1.0" {
			t.Fatalf("list dropped on error: %+v", st)
		}
		if token != "" && strings.Contains(st.Error, token) {
			t.Fatal("token leaked")
		}
	}
	check(403, "", "rate limit exceeded (resets 23:46 UTC); set GENKAN_GITHUB_TOKEN")
	check(403, "ghp_xyz123", "rate limit exceeded")
	check(401, "ghp_xyz123", "rejected the token")
	check(404, "", "not found")
	check(502, "", "unavailable (HTTP 502)")

	gh.mu.Lock()
	gh.status = 0
	gh.mu.Unlock()
	s.Check(context.Background(), true)
	if st := s.Status(); st.Error != "" {
		t.Fatalf("error not cleared: %q", st.Error)
	}

	// unreachable
	gh.ts.Close()
	s.Check(context.Background(), true)
	if st := s.Status(); !strings.Contains(st.Error, "could not reach GitHub") || st.Latest != "v1.1.0" {
		t.Fatalf("%+v", st)
	}
}

func TestCheckBadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) }))
	defer ts.Close()
	gh := newFakeGitHub(t)
	s := newTestSvc(t, gh, "v1.0.0", func(c *Config) { c.API = ts.URL })
	s.Check(context.Background(), true)
	if st := s.Status(); st.Error != "unexpected GitHub response" || st.Available {
		t.Fatalf("%+v", st)
	}
}
