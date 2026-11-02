package favicon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01")

func TestFetchPrefersAppleTouchAndSVG(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><link rel="icon" href="/small.png" sizes="16x16">
<link rel="apple-touch-icon" href="/apple.png" sizes="180x180"><link rel="mask-icon" href="/mask.svg"></head><body></body></html>`))
	})
	mux.HandleFunc("/apple.png", func(w http.ResponseWriter, r *http.Request) { w.Write(png) })
	mux.HandleFunc("/small.png", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) })
	ts := httptest.NewTLSServer(mux)
	defer ts.Close()
	dir := t.TempDir()
	f := NewFetcher(dir)
	name, err := f.Fetch(context.Background(), ts.URL)
	if err != nil || !strings.HasSuffix(name, ".png") {
		t.Fatalf("%q %v", name, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, name)); len(b) != len(png) {
		t.Fatal("stored content")
	}
}

func TestFetchFallbackToFaviconICO(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("\x00\x00\x01\x00\x01\x00\x10\x10\x00\x00\x01\x00\x20\x00"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("<html><head><title>x</title></head></html>"))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	name, err := NewFetcher(t.TempDir()).Fetch(context.Background(), ts.URL+"/")
	if err != nil || !strings.HasSuffix(name, ".ico") {
		t.Fatalf("%q %v", name, err)
	}
}

func TestFetchRejectsNonImagesAndHuge(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<link rel="icon" href="/i.png">`))
	})
	mux.HandleFunc("/i.png", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>not an image</html>")) })
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Write(append(append([]byte{}, png...), make([]byte, 600<<10)...))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	dir := t.TempDir()
	if name, err := NewFetcher(dir).Fetch(context.Background(), ts.URL); err == nil {
		t.Fatalf("accepted %q", name)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatal("files written for rejected icons")
	}
	if _, err := NewFetcher(dir).Fetch(context.Background(), "ftp://x"); err == nil {
		t.Fatal("bad scheme accepted")
	}
}

func TestFetchHonorsContext(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewFetcher(t.TempDir()).Fetch(ctx, ts.URL); err == nil {
		t.Fatal("expected error")
	}
}
