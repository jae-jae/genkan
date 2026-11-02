package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSniff(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		ext  string
	}{
		{"png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), "png"},
		{"jpg", []byte("\xff\xd8\xff\xe0\x00\x10JFIF"), "jpg"},
		{"gif", []byte("GIF89a\x01\x00"), "gif"},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "webp"},
		{"avif", []byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00"), "avif"},
		{"ico", []byte("\x00\x00\x01\x00\x01\x00\x10\x10"), "ico"},
		{"svg", []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"), "svg"},
		{"svg-prolog", []byte("\xef\xbb\xbf<?xml version=\"1.0\"?>\n<!-- c --><svg></svg>"), "svg"},
	}
	for _, c := range cases {
		ext, _, ok := Sniff(c.data)
		if !ok || ext != c.ext {
			t.Errorf("%s: got %q ok=%v", c.name, ext, ok)
		}
	}
	for _, bad := range [][]byte{[]byte("hello world"), []byte("<html><body>x</body></html>"), []byte("<?xml version=\"1.0\"?><x/>"), []byte("MZ\x90\x00"), nil} {
		if _, _, ok := Sniff(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestSaveAndSafeName(t *testing.T) {
	dir := t.TempDir()
	a, err := Save(dir, []byte("abc"), "png")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Save(dir, []byte("abc"), "png")
	if a != b || len(a) != 16+len(".png") {
		t.Fatalf("names %q %q", a, b)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, a)); string(got) != "abc" {
		t.Fatal("content")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("temp files left: %d", len(ents))
	}
	for _, n := range []string{"", ".hidden", "../x", "a/b", "a b", "a\\b"} {
		if SafeName(n) {
			t.Errorf("SafeName(%q)", n)
		}
	}
	if !SafeName("f7fb9db9ecbbbe96.png") {
		t.Fatal("good name rejected")
	}
}
