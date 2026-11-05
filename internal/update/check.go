package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	perPage     = 30
	maxAPIBody  = 4 << 20
	userAgent   = "genkan-update"
	checkAPIVer = "2022-11-28"
)

// asset is a downloadable file of a release.
type asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// storedRelease is a release as kept in check.json: the public Release plus
// what applying needs.
type storedRelease struct {
	Release
	Assets []asset `json:"assets"`
}

// checkCache is <data>/.update/check.json.
type checkCache struct {
	API       string          `json:"api"`
	ETag      string          `json:"etag"`
	CheckedAt int64           `json:"checked_at"` // last successful check, Unix ms
	Releases  []storedRelease `json:"releases"`   // every final semver release, newest first
}

type ghRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (s *Service) cachePath() string {
	return filepath.Join(updateDir(s.cfg.DataDir), checkFile)
}

// loadLocked reads check.json once; the caller holds mu.
func (s *Service) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	var c checkCache
	if readJSON(s.cachePath(), &c) == nil && c.API == s.api {
		s.cache = c
		s.checkedAt = c.CheckedAt
	}
}

// newerLocked returns the cached releases newer than the running version.
func (s *Service) newerLocked() []Release {
	out := []Release{}
	for _, r := range s.cache.Releases {
		if Newer(r.Version, s.cfg.Version) {
			out = append(out, r.Release)
		}
	}
	return out
}

func (s *Service) findRelease(version string) *storedRelease {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	if IsDev(s.cfg.Version) || !Newer(version, s.cfg.Version) {
		return nil
	}
	for i := range s.cache.Releases {
		if s.cache.Releases[i].Version == version {
			r := s.cache.Releases[i]
			return &r
		}
	}
	return nil
}

// Check fetches the release list now. manual=true is the "check now" button:
// it ignores the update_check setting. Development builds and
// GENKAN_UPDATE_CHECK=off are refused with ErrCheckDisabled. Failures to reach
// GitHub are not returned: they are recorded in Status().Error.
func (s *Service) Check(ctx context.Context, manual bool) error {
	if s.Dev() || s.envDisabled() || (!manual && !s.settingOn()) {
		return ErrCheckDisabled
	}
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	s.mu.Lock()
	s.loadLocked()
	s.checking = true
	etag := s.cache.ETag
	s.mu.Unlock()

	rels, newETag, notModified, err := s.fetchReleases(ctx, etag)

	s.mu.Lock()
	s.checking = false
	s.checkedAt = s.cfg.Now().UnixMilli()
	if err != nil {
		s.lastErr = err.Error()
		s.mu.Unlock()
		return nil
	}
	s.lastErr = ""
	s.cache.API = s.api
	s.cache.CheckedAt = s.checkedAt
	if !notModified {
		s.cache.ETag = newETag
		s.cache.Releases = rels
	}
	snapshot := s.cache
	s.mu.Unlock()
	if err := writeJSONFile(s.cachePath(), snapshot); err != nil {
		s.cfg.Logf("update: could not save %s: %v", checkFile, err)
	}
	return nil
}

func (s *Service) fetchReleases(ctx context.Context, etag string) (rels []storedRelease, newETag string, notModified bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.api+"/releases?per_page="+strconv.Itoa(perPage), nil)
	if err != nil {
		return nil, "", false, fmt.Errorf("invalid %s: %s", EnvAPI, trimErr(err))
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", checkAPIVer)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if s.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	}
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, "", false, fmt.Errorf("could not reach GitHub: %s", s.scrub(trimErr(err)))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBody+1))
	if err != nil {
		return nil, "", false, fmt.Errorf("could not read the GitHub response: %s", s.scrub(trimErr(err)))
	}
	if len(body) > maxAPIBody {
		return nil, "", false, fmt.Errorf("the GitHub response is too large")
	}
	switch resp.StatusCode {
	case http.StatusNotModified:
		return nil, etag, true, nil
	case http.StatusOK:
	default:
		return nil, "", false, fmt.Errorf("%s", s.statusError(resp.StatusCode, resp.Header, body))
	}
	var list []ghRelease
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, "", false, fmt.Errorf("unexpected GitHub response")
	}
	return filterReleases(list, RepoURL), resp.Header.Get("ETag"), false, nil
}

// filterReleases keeps final (non-draft, non-prerelease) releases with a
// plain vX.Y.Z tag, newest first, without duplicates.
func filterReleases(list []ghRelease, repo string) []storedRelease {
	out := []storedRelease{}
	seen := map[string]bool{}
	for _, g := range list {
		if _, ok := ParseVersion(g.TagName); !ok || g.Draft || g.Prerelease || seen[g.TagName] {
			continue
		}
		seen[g.TagName] = true
		r := storedRelease{Release: Release{Version: g.TagName, Name: strings.TrimSpace(g.Name), Notes: g.Body, URL: g.HTMLURL}}
		if r.Name == "" {
			r.Name = g.TagName
		}
		if r.URL == "" {
			r.URL = repo + "/releases/tag/" + g.TagName
		}
		if t, err := time.Parse(time.RFC3339, g.PublishedAt); err == nil {
			r.PublishedAt = t.UnixMilli()
		}
		for _, a := range g.Assets {
			r.Assets = append(r.Assets, asset{Name: a.Name, URL: a.URL, Size: a.Size})
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := ParseVersion(out[i].Version)
		b, _ := ParseVersion(out[j].Version)
		return a.Compare(b) > 0
	})
	return out
}

func (s *Service) scrub(m string) string {
	if t := s.cfg.Token; t != "" {
		m = strings.ReplaceAll(m, t, "***")
	}
	return m
}

func trimErr(err error) string {
	m := err.Error()
	if len(m) > 200 {
		m = m[:200]
	}
	return m
}

// statusError turns an unsuccessful GitHub response into a readable message.
func (s *Service) statusError(status int, h http.Header, body []byte) string {
	var j struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &j)
	msg := strings.TrimSpace(j.Message)
	limited := h.Get("X-RateLimit-Remaining") == "0" || h.Get("Retry-After") != "" || strings.Contains(strings.ToLower(msg), "rate limit")
	switch {
	case (status == http.StatusForbidden || status == http.StatusTooManyRequests) && limited:
		m := "GitHub API rate limit exceeded"
		if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil && reset > 0 {
			m += " (resets " + time.Unix(reset, 0).UTC().Format("15:04 UTC") + ")"
		}
		if s.cfg.Token == "" {
			m += "; set GENKAN_GITHUB_TOKEN to raise the limit"
		}
		return m + ", try again later"
	case status == http.StatusUnauthorized:
		return "GitHub rejected the token (check GENKAN_GITHUB_TOKEN)"
	case status == http.StatusNotFound:
		return "release list not found on GitHub"
	case status >= 500:
		return fmt.Sprintf("GitHub is unavailable (HTTP %d)", status)
	}
	if msg != "" {
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Sprintf("GitHub error (HTTP %d): %s", status, s.scrub(msg))
	}
	return fmt.Sprintf("GitHub error (HTTP %d)", status)
}

// Run is the background loop: the first check about a minute after start
// (sooner never, and later when check.json is still fresh), then every
// Interval, as long as ctx lives. It also watches for the confirmation of a
// freshly launched version. It returns at once for development builds.
func (s *Service) Run(ctx context.Context) {
	go s.confirmLoop(ctx)
	if s.Dev() {
		return
	}
	delay := s.cfg.FirstCheckDelay
	if delay <= 0 {
		delay = time.Minute + time.Duration(rand.Int63n(int64(30*time.Second)))
	}
	s.mu.Lock()
	s.loadLocked()
	cached := s.cache.CheckedAt
	s.mu.Unlock()
	if cached > 0 {
		if wait := time.UnixMilli(cached).Add(s.cfg.Interval).Sub(s.cfg.Now()); wait > delay {
			delay = wait
		}
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := s.Check(ctx, false); err == nil {
			s.mu.Lock()
			if s.lastErr != "" {
				s.cfg.Logf("update: check failed: %s", s.lastErr)
			}
			s.mu.Unlock()
		}
		t.Reset(s.cfg.Interval)
	}
}
