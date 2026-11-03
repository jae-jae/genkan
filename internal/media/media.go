// Package media sniffs image content and stores uploaded files by content hash.
package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Sniff detects an image type from content (never from the client-supplied
// name or Content-Type). It returns the canonical file extension (without
// dot) and the MIME type.
func Sniff(data []byte) (ext, mime string, ok bool) {
	if len(data) < 4 {
		return "", "", false
	}
	// AVIF / HEIF-family: ....ftypavif / avis
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		brand := string(data[8:12])
		if brand == "avif" || brand == "avis" {
			return "avif", "image/avif", true
		}
	}
	if looksLikeSVG(data) {
		return "svg", "image/svg+xml", true
	}
	// ICO: 00 00 01 00 <count>
	if data[0] == 0 && data[1] == 0 && data[2] == 1 && data[3] == 0 && len(data) >= 6 && (data[4] != 0 || data[5] != 0) {
		return "ico", "image/x-icon", true
	}
	switch http.DetectContentType(data) {
	case "image/png":
		return "png", "image/png", true
	case "image/jpeg":
		return "jpg", "image/jpeg", true
	case "image/webp":
		return "webp", "image/webp", true
	case "image/gif":
		return "gif", "image/gif", true
	case "image/avif":
		return "avif", "image/avif", true
	case "image/x-icon", "image/vnd.microsoft.icon":
		return "ico", "image/x-icon", true
	}
	return "", "", false
}

func looksLikeSVG(data []byte) bool {
	head := data
	if len(head) > 2048 {
		head = head[:2048]
	}
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	head = bytes.TrimSpace(head)
	if len(head) == 0 || head[0] != '<' {
		return false
	}
	return bytes.Contains(bytes.ToLower(head), []byte("<svg"))
}

// ContentType maps a stored file extension to a MIME type.
func ContentType(ext string) string {
	switch ext {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	case "avif":
		return "image/avif"
	case "svg":
		return "image/svg+xml"
	case "ico":
		return "image/x-icon"
	}
	return "application/octet-stream"
}

// Save writes data to dir as <sha256[:16]>.<ext> (atomic, idempotent) and
// returns the file name.
func Save(dir string, data []byte, ext string) (string, error) { return SaveNamed(dir, "", data, ext) }

// SaveNamed is Save with a fixed file name prefix: <prefix><sha256[:16]>.<ext>.
func SaveNamed(dir, prefix string, data []byte, ext string) (string, error) {
	sum := sha256.Sum256(data)
	name := prefix + hex.EncodeToString(sum[:])[:16] + "." + ext
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	final := filepath.Join(dir, name)
	if st, err := os.Stat(final); err == nil && st.Size() == int64(len(data)) {
		return name, nil
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		cleanup()
		return "", err
	}
	if err := os.Rename(tmpName, final); err != nil {
		cleanup()
		return "", err
	}
	return name, nil
}

// SafeName reports whether name is a plain file name we could have produced.
func SafeName(name string) bool {
	if name == "" || len(name) > 80 || name[0] == '.' {
		return false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// File describes a stored upload.
type File struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// List returns the regular files in dir with safe names, newest first
// (ties broken by name). A missing dir yields an empty list.
func List(dir string) ([]File, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []File{}, nil
		}
		return nil, err
	}
	out := []File{}
	for _, e := range ents {
		if e.IsDir() || !SafeName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, File{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
