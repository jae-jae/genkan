// Package favicon discovers and downloads a site's icon.
package favicon

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/jae-jae/genkan/internal/media"
)

const (
	maxPageBytes = 1 << 20
	maxIconBytes = 512 << 10
	fetchTimeout = 5 * time.Second
	userAgent    = "Mozilla/5.0 (compatible; Genkan/1.0; +self-hosted)"
)

// Fetcher downloads favicons into Dir (uploads/icons).
type Fetcher struct {
	Dir    string
	Client *http.Client
}

func NewFetcher(dir string) *Fetcher {
	tr := &http.Transport{
		Proxy:                  http.ProxyFromEnvironment,
		DialContext:            (&net.Dialer{Timeout: fetchTimeout}).DialContext,
		TLSClientConfig:        &tls.Config{InsecureSkipVerify: true}, // homelab self-signed certs
		TLSHandshakeTimeout:    fetchTimeout,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 64 << 10,
	}
	return &Fetcher{Dir: dir, Client: &http.Client{
		Transport: tr,
		Timeout:   fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}}
}

type candidate struct {
	href  string
	score int
}

// Fetch finds and stores the icon for pageURL; returns the stored file name.
func (f *Fetcher) Fetch(ctx context.Context, pageURL string) (string, error) {
	u, err := url.Parse(pageURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("unsupported url")
	}
	var cands []candidate
	body, final, err := f.get(ctx, pageURL, maxPageBytes, true)
	base := u
	if err == nil {
		base = final
		cands = parseIcons(body, final)
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	if len(cands) > 4 {
		cands = cands[:4]
	}
	tried := map[string]bool{}
	list := make([]string, 0, len(cands)+1)
	for _, c := range cands {
		list = append(list, c.href)
	}
	list = append(list, (&url.URL{Scheme: base.Scheme, Host: base.Host, Path: "/favicon.ico"}).String())
	var lastErr error = errors.New("no icon found")
	for _, href := range list {
		if tried[href] {
			continue
		}
		tried[href] = true
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		data, _, err := f.get(ctx, href, maxIconBytes, false)
		if err != nil {
			lastErr = err
			continue
		}
		ext, _, ok := media.Sniff(data)
		if !ok {
			lastErr = errors.New("not an image")
			continue
		}
		name, err := media.Save(f.Dir, data, ext)
		if err != nil {
			return "", err
		}
		return name, nil
	}
	return "", lastErr
}

// get downloads at most limit bytes. When truncate is set an oversized body is
// cut (page parsing); otherwise it is an error (icons).
func (f *Fetcher) get(ctx context.Context, rawURL string, limit int64, truncate bool) ([]byte, *url.URL, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil && len(data) == 0 {
		return nil, nil, err
	}
	if int64(len(data)) > limit {
		if !truncate {
			return nil, nil, errors.New("file too large")
		}
		data = data[:limit]
	}
	return data, resp.Request.URL, nil
}

// parseIcons extracts icon candidates from the HTML head.
func parseIcons(page []byte, pageURL *url.URL) []candidate {
	z := html.NewTokenizer(bytes.NewReader(page))
	base := pageURL
	var out []candidate
loop:
	for {
		switch z.Next() {
		case html.ErrorToken:
			break loop
		case html.EndTagToken:
			if n, _ := z.TagName(); string(n) == "head" {
				break loop
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if tag == "body" {
				break loop
			}
			if (tag != "link" && tag != "base") || !hasAttr {
				continue
			}
			attrs := map[string]string{}
			for {
				k, v, more := z.TagAttr()
				attrs[strings.ToLower(string(k))] = string(v)
				if !more {
					break
				}
			}
			if tag == "base" {
				if b, err := pageURL.Parse(strings.TrimSpace(attrs["href"])); err == nil && attrs["href"] != "" {
					base = b
				}
				continue
			}
			href := strings.TrimSpace(attrs["href"])
			if href == "" {
				continue
			}
			rel := strings.Fields(strings.ToLower(attrs["rel"]))
			kind := ""
			for _, r := range rel {
				switch r {
				case "apple-touch-icon", "apple-touch-icon-precomposed":
					kind = "apple"
				case "icon":
					if kind == "" {
						kind = "icon"
					}
				case "mask-icon":
					if kind == "" {
						kind = "mask"
					}
				}
			}
			if kind == "" {
				continue
			}
			if strings.HasPrefix(strings.ToLower(href), "data:") {
				continue
			}
			abs, err := base.Parse(href)
			if err != nil || (abs.Scheme != "http" && abs.Scheme != "https") {
				continue
			}
			out = append(out, candidate{abs.String(), score(kind, attrs, abs)})
		}
	}
	return out
}

func score(kind string, attrs map[string]string, u *url.URL) int {
	isSVG := strings.Contains(strings.ToLower(attrs["type"]), "svg") || strings.EqualFold(filepath.Ext(u.Path), ".svg")
	size := 0
	for _, s := range strings.Fields(strings.ToLower(attrs["sizes"])) {
		if s == "any" {
			size = max(size, 512)
			continue
		}
		if w, _, ok := strings.Cut(s, "x"); ok {
			if n, err := strconv.Atoi(w); err == nil {
				size = max(size, n)
			}
		}
	}
	switch {
	case kind == "mask":
		return 1
	case isSVG:
		return 5000
	case kind == "apple":
		if size == 0 {
			size = 180
		}
		return 4000 + min(size, 1024)
	}
	if size == 0 {
		size = 32
	}
	return min(size, 1024)
}
