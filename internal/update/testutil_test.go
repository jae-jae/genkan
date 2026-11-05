package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type tarEntry struct {
	name string
	typ  byte
	body string
	link string
}

func makeTarGz(t testing.TB, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o755, Linkname: e.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// fakeRelease is a published release on the fake GitHub.
type fakeRelease struct {
	Tag        string
	Draft      bool
	Prerelease bool
	Body       string
	Files      map[string][]byte // asset name -> content (served under /dl/<tag>/<name>)
}

// fakeGitHub serves /releases and /dl/... .
type fakeGitHub struct {
	t        *testing.T
	ts       *httptest.Server
	mu       sync.Mutex
	releases []fakeRelease
	etag     string
	hits     int
	gotAuth  []string
	gotINM   []string
	status   int // non-zero: answer every /releases with it
	extra    http.HandlerFunc
	pub      ed25519.PublicKey
	priv     ed25519.PrivateKey
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	g := &fakeGitHub{t: t, etag: `"v1"`, pub: pub, priv: priv}
	g.ts = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.ts.Close)
	return g
}

func (g *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.extra != nil && strings.HasPrefix(r.URL.Path, "/x/") {
		g.extra(w, r)
		return
	}
	if r.URL.Path == "/releases" {
		g.hits++
		g.gotAuth = append(g.gotAuth, r.Header.Get("Authorization"))
		g.gotINM = append(g.gotINM, r.Header.Get("If-None-Match"))
		if g.status != 0 {
			if g.status == 403 {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", "1762300000")
			}
			w.WriteHeader(g.status)
			fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
			return
		}
		if r.Header.Get("If-None-Match") == g.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", g.etag)
		var out []map[string]any
		for _, rel := range g.releases {
			var assets []map[string]any
			for name, b := range rel.Files {
				assets = append(assets, map[string]any{"name": name, "size": len(b), "browser_download_url": g.ts.URL + "/dl/" + rel.Tag + "/" + name})
			}
			out = append(out, map[string]any{
				"tag_name": rel.Tag, "name": rel.Tag, "body": rel.Body, "draft": rel.Draft, "prerelease": rel.Prerelease,
				"html_url": "https://github.com/jae-jae/genkan/releases/tag/" + rel.Tag, "published_at": "2025-11-04T10:00:00Z", "assets": assets,
			})
		}
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/dl/") {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/dl/"), "/", 2)
		for _, rel := range g.releases {
			if rel.Tag == parts[0] {
				if b, ok := rel.Files[parts[1]]; ok {
					w.Write(b)
					return
				}
			}
		}
	}
	http.NotFound(w, r)
}

func (g *fakeGitHub) setReleases(rs ...fakeRelease) {
	g.mu.Lock()
	g.releases = rs
	g.etag = fmt.Sprintf(`"v%d"`, time.Now().UnixNano())
	g.mu.Unlock()
}

// signedRelease builds a release with a valid checksums.txt / .sig for arch.
func (g *fakeGitHub) signedRelease(tag, arch string, tarball []byte) fakeRelease {
	name := tarballName(tag, arch)
	sums := []byte(sum(tarball) + "  " + name + "\n" + strings.Repeat("0", 64) + "  other.tar.gz\n")
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(g.priv, sums)) + "\n"
	return fakeRelease{Tag: tag, Files: map[string][]byte{name: tarball, "checksums.txt": sums, "checksums.txt.sig": []byte(sig)}}
}

func goodTarball(t testing.TB, tag string) []byte {
	return makeTarGz(t, tarEntry{name: "genkan", body: "#!/bin/sh\necho genkan " + tag + "\n"})
}

type testSvc struct {
	*Service
	dir     string
	gh      *fakeGitHub
	execs   []execCall
	execMu  sync.Mutex
	enabled atomic.Bool
	dismiss string
}

type execCall struct {
	path       string
	argv, envs []string
}

func newTestSvc(t *testing.T, gh *fakeGitHub, version string, mod func(*Config)) *testSvc {
	t.Helper()
	ts := &testSvc{dir: t.TempDir(), gh: gh}
	ts.enabled.Store(true)
	cfg := Config{
		DataDir: ts.dir, Version: version, API: gh.ts.URL, HTTPClient: gh.ts.Client(),
		CheckEnabled: func() bool { return ts.enabled.Load() },
		Dismissed:    func() string { return ts.dismiss },
		Getenv:       func(string) string { return "" },
		GOOS:         "linux", GOARCH: "amd64",
		NoExec:     func(string) bool { return false },
		PubKey:     gh.pub,
		Executable: func() (string, error) { return "/opt/genkan", nil },
		Args:       []string{"/opt/genkan", "--flag"},
		Environ:    func() []string { return []string{"A=1", "GENKAN_LAUNCHED=1"} },
		Exec: func(path string, argv, env []string) error {
			ts.execMu.Lock()
			ts.execs = append(ts.execs, execCall{path, argv, env})
			ts.execMu.Unlock()
			return nil
		},
		RunVersion: func(_ context.Context, bin string) (string, error) {
			b, err := os.ReadFile(bin)
			if err != nil {
				return "", err
			}
			if i := strings.Index(string(b), "echo "); i >= 0 {
				return strings.TrimSpace(string(b)[i+5:]), nil
			}
			return "", fmt.Errorf("not a script")
		},
		Logf: func(string, ...any) {},
	}
	if mod != nil {
		mod(&cfg)
	}
	ts.Service = New(cfg)
	t.Cleanup(ts.Service.Close)
	return ts
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func writeFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t testing.TB, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func parseURL(s string) (*url.URL, error) { return url.Parse(s) }
