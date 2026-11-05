package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxTarball   = 100 << 20 // download size limit
	maxBinary    = 200 << 20 // unpacked size limit
	maxChecksums = 1 << 20
	maxSig       = 4 << 10
	maxRedirects = 5
)

// Download hosts besides the host of an overridden GENKAN_UPDATE_API.
var allowedHosts = map[string]bool{
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// hostAllowed reports whether a download (or a redirect of one) may go to u.
func (s *Service) hostAllowed(u *url.URL) bool {
	if u.Scheme == "https" && allowedHosts[strings.ToLower(u.Hostname())] {
		return true
	}
	return s.apiOverridden() && s.apiURL != nil && s.apiURL.Host != "" && strings.EqualFold(u.Host, s.apiURL.Host) &&
		(u.Scheme == "https" || u.Scheme == "http")
}

// downloadClient is the HTTP client for assets: no overall timeout (the apply
// context bounds it) and redirects only to allowed hosts. The GitHub token is
// never sent.
func (s *Service) downloadClient() *http.Client {
	c := *s.cfg.HTTPClient
	c.Timeout = 0
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("too many redirects")
		}
		if !s.hostAllowed(req.URL) {
			return fmt.Errorf("redirect to a disallowed host (%s)", req.URL.Host)
		}
		return nil
	}
	return &c
}

type countWriter struct {
	n  int64
	on func(int64)
}

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	if c.on != nil {
		c.on(c.n)
	}
	return len(p), nil
}

// fetch downloads rawURL into dst, failing when the body exceeds limit.
// total is the Content-Length (or -1); progress receives the bytes so far.
func (s *Service) fetch(ctx context.Context, rawURL string, limit int64, dst io.Writer, progress func(done, total int64)) error {
	u, err := url.Parse(rawURL)
	if err != nil || !s.hostAllowed(u) {
		return fmt.Errorf("download from a disallowed host")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.downloadClient().Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %s", s.scrub(trimErr(err)))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return fmt.Errorf("download is larger than the %d MiB limit", limit>>20)
	}
	cw := &countWriter{}
	if progress != nil {
		total := resp.ContentLength
		cw.on = func(n int64) { progress(n, total) }
	}
	n, err := io.Copy(io.MultiWriter(dst, cw), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("download failed: %s", s.scrub(trimErr(err)))
	}
	if n > limit {
		return fmt.Errorf("download is larger than the %d MiB limit", limit>>20)
	}
	return nil
}

func (s *Service) fetchBytes(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	var b bytes.Buffer
	if err := s.fetch(ctx, rawURL, limit, &b, nil); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// tarballName is the release asset for version (vX.Y.Z) on linux/arch.
func tarballName(version, arch string) string {
	return "genkan_" + strings.TrimPrefix(version, "v") + "_linux_" + arch + ".tar.gz"
}

// checksumFor finds name in a sha256sum-format file and returns its hex digest.
func checksumFor(checksums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(checksums))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sum, rest, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		file := strings.TrimPrefix(strings.TrimLeft(rest, " "), "*")
		if file == name {
			sum = strings.ToLower(sum)
			if b, err := hex.DecodeString(sum); err != nil || len(b) != sha256.Size {
				return "", fmt.Errorf("checksums.txt has an invalid digest for %s", name)
			}
			return sum, nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

// verifyRelease checks the signature of checksums.txt, then the SHA-256 of the
// tarball (given as its hex digest).
func verifyRelease(pub ed25519.PublicKey, checksums, sig []byte, tarball, gotSum string) error {
	if err := VerifySignature(pub, checksums, sig); err != nil {
		return fmt.Errorf("checksums.txt: %w", err)
	}
	want, err := checksumFor(checksums, tarball)
	if err != nil {
		return err
	}
	if want != gotSum {
		return fmt.Errorf("checksum mismatch for %s", tarball)
	}
	return nil
}

// extractBinary unpacks a .tar.gz that holds exactly one regular file named
// "genkan" into dst (mode 0755). Anything else in the archive (directories,
// links, devices, other names, a second entry) is an error.
func extractBinary(tarGz io.Reader, dst string) error {
	gz, err := gzip.NewReader(tarGz)
	if err != nil {
		return fmt.Errorf("not a gzip file: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("bad tar file: %w", err)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if h.Typeflag != tar.TypeReg || (h.Name != "genkan" && h.Name != "./genkan") {
			return fmt.Errorf("unexpected entry %q in the archive", h.Name)
		}
		if found {
			return fmt.Errorf("the archive holds more than one genkan file")
		}
		found = true
		if h.Size > maxBinary {
			return fmt.Errorf("the binary is larger than the %d MiB limit", maxBinary>>20)
		}
		if err := writeExecutable(tr, dst); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("the archive has no genkan file")
	}
	return nil
}

func writeExecutable(r io.Reader, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, maxBinary+1))
	if err == nil && n > maxBinary {
		err = fmt.Errorf("the binary is larger than the %d MiB limit", maxBinary>>20)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(dst, 0o755) // umask
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}
