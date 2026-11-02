package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

type asset struct {
	data  []byte
	gz    []byte // nil when not compressed
	ctype string
	etag  string
}

type static struct {
	files map[string]*asset
	index *asset
}

var ctypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json; charset=utf-8",
	".webmanifest": "application/manifest+json",
	".svg":         "image/svg+xml",
	".txt":         "text/plain; charset=utf-8",
	".map":         "application/json",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".webp":        "image/webp",
	".avif":        "image/avif",
	".gif":         "image/gif",
	".ico":         "image/x-icon",
	".woff2":       "font/woff2",
	".woff":        "font/woff",
}

func compressible(ctype string) bool {
	return strings.HasPrefix(ctype, "text/") || strings.HasPrefix(ctype, "application/json") ||
		strings.HasPrefix(ctype, "application/manifest+json") || strings.HasPrefix(ctype, "image/svg+xml")
}

// newStatic loads the embedded frontend into memory, pre-gzipping text assets.
func newStatic(fsys fs.FS) *static {
	s := &static{files: map[string]*asset{}}
	if fsys == nil {
		return s
	}
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		f, err := fsys.Open(p)
		if err != nil {
			return nil
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return nil
		}
		ext := strings.ToLower(path.Ext(p))
		ct := ctypes[ext]
		if ct == "" {
			ct = mime.TypeByExtension(ext)
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		sum := sha256.Sum256(data)
		a := &asset{data: data, ctype: ct, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
		if compressible(ct) && len(data) > 256 {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			_, _ = zw.Write(data)
			_ = zw.Close()
			if buf.Len() < len(data) {
				a.gz = buf.Bytes()
			}
		}
		s.files["/"+p] = a
		return nil
	})
	s.index = s.files["/index.html"]
	return s
}

func (s *static) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p := path.Clean(r.URL.Path)
	if p == "/" || p == "." {
		p = "/index.html"
	}
	a := s.files[p]
	if a == nil {
		// Missing files (anything with an extension, or under /assets) are real 404s;
		// everything else is a client-side route and gets the SPA shell.
		if strings.HasPrefix(p, "/assets/") || strings.Contains(path.Base(p), ".") || s.index == nil {
			http.NotFound(w, r)
			return
		}
		a = s.index
	}
	gz := a.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
	etag := a.etag
	if gz {
		etag = etag[:len(etag)-1] + `-gz"`
	}
	h := w.Header()
	h.Set("Content-Type", a.ctype)
	h.Set("ETag", etag)
	h.Add("Vary", "Accept-Encoding")
	if strings.HasPrefix(p, "/assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := a.data
	if gz {
		body = a.gz
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("Content-Length", itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
