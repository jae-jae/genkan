package update

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyRelease(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	tarball := goodTarball(t, "v1.1.0")
	name := tarballName("v1.1.0", "amd64")
	sums := []byte(sum(tarball) + "  " + name + "\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)) + "\n")

	if err := verifyRelease(pub, sums, sig, name, sum(tarball)); err != nil {
		t.Fatal(err)
	}
	// trailing newline of checksums.txt does not matter to the signature as long as the bytes are the same
	noNL := bytes.TrimRight(sums, "\n")
	sigNoNL := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, noNL)))
	if err := verifyRelease(pub, noNL, sigNoNL, name, sum(tarball)); err != nil {
		t.Fatal(err)
	}
	// signature over other bytes
	if err := verifyRelease(pub, append(sums, '\n'), sig, name, sum(tarball)); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("modified checksums.txt accepted: %v", err)
	}
	// other key
	pub2, _, _ := ed25519.GenerateKey(nil)
	if err := verifyRelease(pub2, sums, sig, name, sum(tarball)); err == nil {
		t.Fatal("wrong key accepted")
	}
	// malformed signatures
	for _, bad := range []string{"", "!!!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if err := verifyRelease(pub, sums, []byte(bad), name, sum(tarball)); err == nil {
			t.Fatalf("signature %q accepted", bad)
		}
	}
	// bad digest
	if err := verifyRelease(pub, sums, sig, name, sum([]byte("other"))); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("bad sha accepted: %v", err)
	}
	// no entry for this arch
	if err := verifyRelease(pub, sums, sig, tarballName("v1.1.0", "arm64"), sum(tarball)); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Fatalf("missing entry: %v", err)
	}
}

func TestEmbeddedKey(t *testing.T) {
	k, err := ParsePublicKey(EmbeddedPublicKey)
	if err != nil || len(k) != ed25519.PublicKeySize {
		t.Fatalf("embedded key: %v", err)
	}
	if _, err := ParsePublicKey("AAAA"); err == nil {
		t.Fatal("short key accepted")
	}
	s := New(Config{DataDir: t.TempDir(), Version: "v1.0.0"})
	if !bytes.Equal(s.key, k) {
		t.Fatal("default is the embedded key")
	}
	other, _, _ := ed25519.GenerateKey(nil)
	o := New(Config{DataDir: t.TempDir(), Version: "v1.0.0", Getenv: func(k string) string {
		if k == EnvPubKey {
			return base64.StdEncoding.EncodeToString(other)
		}
		return ""
	}})
	if !bytes.Equal(o.key, other) {
		t.Fatal("GENKAN_UPDATE_PUBKEY overrides")
	}
	b := New(Config{DataDir: t.TempDir(), Version: "v1.0.0", Getenv: func(k string) string {
		if k == EnvPubKey {
			return "garbage"
		}
		return ""
	}})
	if b.keyErr == nil {
		t.Fatal("bad GENKAN_UPDATE_PUBKEY must not fall back silently")
	}
}

func TestExtractBinary(t *testing.T) {
	dst := func() string { return filepath.Join(t.TempDir(), "v", "genkan") }
	ok := dst()
	if err := extractBinary(bytes.NewReader(goodTarball(t, "v1.0.0")), ok); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(ok)
	if err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("%v %v", fi, err)
	}
	if err := extractBinary(bytes.NewReader(makeTarGz(t, tarEntry{name: "./genkan", body: "x"})), dst()); err != nil {
		t.Fatalf("./genkan: %v", err)
	}

	bad := map[string][]tarEntry{
		"empty":         {},
		"other name":    {{name: "evil", body: "x"}},
		"subdir":        {{name: "bin/genkan", body: "x"}},
		"traversal":     {{name: "../genkan", body: "x"}},
		"absolute":      {{name: "/genkan", body: "x"}},
		"dir first":     {{name: "dir/", typ: tar.TypeDir}, {name: "genkan", body: "x"}},
		"extra file":    {{name: "genkan", body: "x"}, {name: "README", body: "y"}},
		"two genkan":    {{name: "genkan", body: "x"}, {name: "genkan", body: "y"}},
		"symlink":       {{name: "genkan", typ: tar.TypeSymlink, link: "/bin/sh"}},
		"hardlink":      {{name: "genkan", typ: tar.TypeLink, link: "other"}},
		"symlink extra": {{name: "genkan", body: "x"}, {name: "link", typ: tar.TypeSymlink, link: "genkan"}},
		"fifo":          {{name: "genkan", typ: tar.TypeFifo}},
	}
	for name, entries := range bad {
		d := dst()
		if err := extractBinary(bytes.NewReader(makeTarGz(t, entries...)), d); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := extractBinary(strings.NewReader("not gzip"), dst()); err == nil {
		t.Error("garbage accepted")
	}
	// a symlink planted at the destination is not followed
	d := dst()
	os.MkdirAll(filepath.Dir(d), 0o755)
	target := filepath.Join(t.TempDir(), "victim")
	os.WriteFile(target, []byte("keep"), 0o644)
	os.Symlink(target, d)
	if err := extractBinary(bytes.NewReader(goodTarball(t, "v1.0.0")), d); err == nil {
		t.Error("existing destination must fail (O_EXCL)")
	}
	if readFile(t, target) != "keep" {
		t.Error("symlink target overwritten")
	}
}

func TestFetchLimitsAndHosts(t *testing.T) {
	gh := newFakeGitHub(t)
	s := newTestSvc(t, gh, "v1.0.0", nil)
	big := bytes.Repeat([]byte("a"), 2048)
	gh.setReleases(fakeRelease{Tag: "v1.1.0", Files: map[string][]byte{"big": big}})

	var b bytes.Buffer
	if err := s.fetch(context.Background(), gh.ts.URL+"/dl/v1.1.0/big", 1024, &b, nil); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized accepted: %v", err)
	}
	b.Reset()
	if err := s.fetch(context.Background(), gh.ts.URL+"/dl/v1.1.0/big", 4096, &b, nil); err != nil || b.Len() != 2048 {
		t.Fatalf("%v %d", err, b.Len())
	}
	if err := s.fetch(context.Background(), gh.ts.URL+"/dl/v1.1.0/none", 4096, &b, nil); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404: %v", err)
	}

	// chunked body without Content-Length that exceeds the limit
	chunked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		for i := 0; i < 10; i++ {
			w.Write(bytes.Repeat([]byte("z"), 512))
			fl.Flush()
		}
	}))
	defer chunked.Close()
	sc := newTestSvc(t, gh, "v1.0.0", func(c *Config) { c.API = chunked.URL; c.HTTPClient = chunked.Client() })
	b.Reset()
	if err := sc.fetch(context.Background(), chunked.URL+"/f", 1024, &b, nil); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("chunked oversized accepted: %v", err)
	}
	if b.Len() > 1025 {
		t.Fatalf("wrote %d bytes past the limit", b.Len())
	}

	// the first URL must be an allowed host too
	if err := s.fetch(context.Background(), "https://evil.example.com/x", 1024, &b, nil); err == nil || !strings.Contains(err.Error(), "disallowed host") {
		t.Fatalf("%v", err)
	}
	if err := s.fetch(context.Background(), "http://github.com/x", 1024, &b, nil); err == nil {
		t.Fatal("plain http to github.com accepted")
	}
}

func TestRedirectAllowlist(t *testing.T) {
	evilHit := false
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { evilHit = true; w.Write([]byte("evil")) }))
	defer evil.Close()
	var self *httptest.Server
	self = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-evil":
			http.Redirect(w, r, evil.URL+"/x", http.StatusFound)
		case "/to-self":
			http.Redirect(w, r, self.URL+"/ok", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, self.URL+"/loop", http.StatusFound)
		case "/ok":
			w.Write([]byte("fine"))
		}
	}))
	defer self.Close()
	gh := newFakeGitHub(t)
	s := newTestSvc(t, gh, "v1.0.0", func(c *Config) { c.API = self.URL; c.HTTPClient = self.Client() })

	var b bytes.Buffer
	err := s.fetch(context.Background(), self.URL+"/to-evil", 1024, &b, nil)
	if err == nil || !strings.Contains(err.Error(), "disallowed host") || evilHit {
		t.Fatalf("redirect to another host: err=%v hit=%v", err, evilHit)
	}
	b.Reset()
	if err := s.fetch(context.Background(), self.URL+"/to-self", 1024, &b, nil); err != nil || b.String() != "fine" {
		t.Fatalf("redirect to the API host: %v %q", err, b.String())
	}
	if err := s.fetch(context.Background(), self.URL+"/loop", 1024, &b, nil); err == nil {
		t.Fatal("redirect loop")
	}

	// without an override the API host is not special: only the GitHub hosts are allowed
	d := New(Config{DataDir: t.TempDir(), Version: "v1.0.0", Getenv: func(string) string { return "" }})
	for host, want := range map[string]bool{
		"https://github.com/a": true, "https://objects.githubusercontent.com/a": true, "https://release-assets.githubusercontent.com/a": true,
		"https://api.github.com/a": false, "https://github.com.evil.io/a": false, "https://evilgithub.com/a": false,
		"http://github.com/a": false, "https://127.0.0.1/a": false, "https://GitHub.com/a": true,
	} {
		u, _ := parseURL(host)
		if got := d.hostAllowed(u); got != want {
			t.Errorf("hostAllowed(%s) = %v, want %v", host, got, want)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	h := strings.Repeat("ab", 32)
	txt := []byte("# comment\n\n" + h + "  a.tar.gz\n" + h + " *b.tar.gz\nnothex  c.tar.gz\n")
	for _, n := range []string{"a.tar.gz", "b.tar.gz"} {
		if got, err := checksumFor(txt, n); err != nil || got != h {
			t.Errorf("%s: %q %v", n, got, err)
		}
	}
	if _, err := checksumFor(txt, "c.tar.gz"); err == nil {
		t.Error("invalid digest accepted")
	}
	if _, err := checksumFor(txt, "d.tar.gz"); err == nil {
		t.Error("missing entry accepted")
	}
}
