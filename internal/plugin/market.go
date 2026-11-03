package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jae-jae/genkan/internal/store"
)

// Third-party plugins on GitHub (docs/PLUGINS.md §11.4). This is host code:
// it talks to GitHub with a plain http.Client, never through a plugin sandbox,
// and only when the admin opens the market.

const (
	defaultGitHubAPI = "https://api.github.com"
	defaultGitHubRaw = "https://raw.githubusercontent.com"
	marketTopic      = "genkan-plugin"
	marketTTL        = 10 * time.Minute
	marketErrTTL     = 30 * time.Second
	marketCacheMax   = 128
	marketPerPage    = 20
	marketMaxPage    = 30 // GitHub only serves the first 1000 search results
	maxScanFiles     = 40
	scanWorkers      = 4
	scanTimeout      = 60 * time.Second
	maxManifestList  = 64 << 10
	maxAPIBody       = 4 << 20
	marketUserAgent  = "genkan-plugin-market"
)

var repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// MarketItem is one repository in the market search.
type MarketItem struct {
	Repo          string `json:"repo"`
	Description   string `json:"description"`
	Stars         int    `json:"stars"`
	UpdatedAt     string `json:"updated_at"`
	HTMLURL       string `json:"html_url"`
	OwnerAvatar   string `json:"owner_avatar"`
	DefaultBranch string `json:"default_branch"`
}

// MarketResult is the response of GET /api/plugins/market. CachedAt is Unix milliseconds.
type MarketResult struct {
	Items    []MarketItem `json:"items"`
	Total    int          `json:"total"`
	CachedAt int64        `json:"cached_at"`
	Error    string       `json:"error,omitempty"`
}

// RepoPlugin is one scanned plugin file of a repository.
type RepoPlugin struct {
	Path             string `json:"path"`
	URL              string `json:"url"`
	Plugin           *Info  `json:"plugin"`
	Error            string `json:"error,omitempty"`
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

// RepoScan is the response of GET /api/plugins/market/repo. CachedAt is Unix milliseconds.
type RepoScan struct {
	Repo          string       `json:"repo"`
	DefaultBranch string       `json:"default_branch"`
	Plugins       []RepoPlugin `json:"plugins"`
	CachedAt      int64        `json:"cached_at"`
	Error         string       `json:"error,omitempty"`
}

type marketEntry struct {
	val any
	exp time.Time
}

func (m *Manager) ghAPI() string {
	return strings.TrimRight(orStr(m.cfg.GitHubAPI, defaultGitHubAPI), "/")
}
func (m *Manager) ghRaw() string {
	return strings.TrimRight(orStr(m.cfg.GitHubRaw, defaultGitHubRaw), "/")
}

func orStr(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

func (m *Manager) ghClient() *http.Client {
	m.ghOnce.Do(func() {
		if m.cfg.GitHubClient != nil {
			m.ghc = m.cfg.GitHubClient
			return
		}
		m.ghc = &http.Client{Timeout: 20 * time.Second}
	})
	return m.ghc
}

// marketCached returns a cached value or computes it once (concurrent callers
// share one computation). fn reports the value and whether it is an error
// result, which is cached for a much shorter time.
func (m *Manager) marketCached(key string, fn func(ctx context.Context) (val any, failed bool)) any {
	now := time.Now()
	m.mkMu.Lock()
	if e, ok := m.mkCache[key]; ok && now.Before(e.exp) {
		m.mkMu.Unlock()
		return e.val
	}
	m.mkMu.Unlock()
	v, _, _ := m.fl.do("mk|"+key, func() (any, error) {
		ctx, cancel := context.WithTimeout(m.ctx, scanTimeout)
		defer cancel()
		val, failed := fn(ctx)
		if m.ctx.Err() != nil {
			return val, nil // shutting down: do not cache
		}
		ttl := marketTTL
		if failed {
			ttl = marketErrTTL
		}
		m.mkMu.Lock()
		if m.mkCache == nil {
			m.mkCache = map[string]*marketEntry{}
		}
		if len(m.mkCache) >= marketCacheMax {
			t := time.Now()
			var oldest string
			for k, e := range m.mkCache {
				if t.After(e.exp) {
					delete(m.mkCache, k)
				} else if oldest == "" || e.exp.Before(m.mkCache[oldest].exp) {
					oldest = k
				}
			}
			if len(m.mkCache) >= marketCacheMax && oldest != "" {
				delete(m.mkCache, oldest)
			}
		}
		m.mkCache[key] = &marketEntry{val: val, exp: time.Now().Add(ttl)}
		m.mkMu.Unlock()
		return val, nil
	})
	return v
}

// ghGet performs a GET against GitHub and returns the body (at most limit
// bytes; more is an error) and the status. api=true adds the API headers and
// the token (never sent to the raw host).
func (m *Manager) ghGet(ctx context.Context, rawURL string, api bool, limit int64) ([]byte, int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("User-Agent", marketUserAgent)
	if api {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if m.cfg.GitHubToken != "" {
			req.Header.Set("Authorization", "Bearer "+m.cfg.GitHubToken)
		}
	}
	resp, err := m.ghClient().Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("could not reach GitHub: %s", m.scrub(trimErr(err)))
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, resp.Header, fmt.Errorf("could not read the GitHub response: %s", m.scrub(trimErr(err)))
	}
	if int64(len(b)) > limit {
		return nil, resp.StatusCode, resp.Header, fmt.Errorf("the GitHub response is too large")
	}
	return b, resp.StatusCode, resp.Header, nil
}

func (m *Manager) scrub(s string) string {
	if t := m.cfg.GitHubToken; t != "" {
		s = strings.ReplaceAll(s, t, "***")
	}
	return s
}

// ghStatusError turns an unsuccessful GitHub response into a readable message.
func (m *Manager) ghStatusError(status int, h http.Header, body []byte) string {
	var j struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &j)
	msg := strings.TrimSpace(j.Message)
	limited := h.Get("X-RateLimit-Remaining") == "0" || h.Get("Retry-After") != "" || strings.Contains(strings.ToLower(msg), "rate limit")
	switch {
	case (status == http.StatusForbidden || status == http.StatusTooManyRequests) && limited:
		s := "GitHub API rate limit exceeded"
		if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil && reset > 0 {
			s += " (resets " + time.Unix(reset, 0).UTC().Format("15:04 UTC") + ")"
		}
		if m.cfg.GitHubToken == "" {
			s += "; set GENKAN_GITHUB_TOKEN to raise the limit"
		}
		return s + ", try again later"
	case status == http.StatusUnauthorized:
		return "GitHub rejected the token (check GENKAN_GITHUB_TOKEN)"
	case status == http.StatusNotFound:
		return "not found on GitHub"
	case status >= 500:
		return fmt.Sprintf("GitHub is unavailable (HTTP %d)", status)
	}
	if msg != "" {
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Sprintf("GitHub error (HTTP %d): %s", status, m.scrub(msg))
	}
	return fmt.Sprintf("GitHub error (HTTP %d)", status)
}

// ghJSON GETs an API URL and decodes the JSON body into dst.
func (m *Manager) ghJSON(ctx context.Context, rawURL string, dst any) error {
	b, status, h, err := m.ghGet(ctx, rawURL, true, maxAPIBody)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("%s", m.ghStatusError(status, h, b))
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("unexpected GitHub response")
	}
	return nil
}

// Market searches GitHub repositories with the genkan-plugin topic. sort is
// "stars" (default) or "updated". Failures are reported in Error.
func (m *Manager) Market(sortBy, q string, page int) (MarketResult, error) {
	switch sortBy {
	case "":
		sortBy = "stars"
	case "stars", "updated":
	default:
		return MarketResult{}, invalidf(`sort must be "stars" or "updated"`)
	}
	q = cleanQuery(q)
	if page < 1 {
		page = 1
	}
	if page > marketMaxPage {
		page = marketMaxPage
	}
	key := "s|" + sortBy + "|" + q + "|" + strconv.Itoa(page)
	v := m.marketCached(key, func(ctx context.Context) (any, bool) {
		res := MarketResult{Items: []MarketItem{}, CachedAt: nowMs()}
		qs := url.Values{}
		query := "topic:" + marketTopic
		if q != "" {
			query += " " + q
		}
		qs.Set("q", query)
		qs.Set("sort", sortBy)
		qs.Set("order", "desc")
		qs.Set("per_page", strconv.Itoa(marketPerPage))
		qs.Set("page", strconv.Itoa(page))
		var out struct {
			Total int `json:"total_count"`
			Items []struct {
				FullName      string  `json:"full_name"`
				Description   *string `json:"description"`
				Stars         int     `json:"stargazers_count"`
				UpdatedAt     string  `json:"updated_at"`
				HTMLURL       string  `json:"html_url"`
				DefaultBranch string  `json:"default_branch"`
				Owner         struct {
					AvatarURL string `json:"avatar_url"`
				} `json:"owner"`
			} `json:"items"`
		}
		if err := m.ghJSON(ctx, m.ghAPI()+"/search/repositories?"+qs.Encode(), &out); err != nil {
			res.Error = err.Error()
			return res, true
		}
		res.Total = out.Total
		for _, it := range out.Items {
			if !ValidRepo(it.FullName) {
				continue
			}
			mi := MarketItem{Repo: it.FullName, Stars: it.Stars, UpdatedAt: it.UpdatedAt, HTMLURL: it.HTMLURL,
				OwnerAvatar: it.Owner.AvatarURL, DefaultBranch: it.DefaultBranch}
			if it.Description != nil {
				mi.Description = *it.Description
			}
			res.Items = append(res.Items, mi)
		}
		return res, false
	})
	res, ok := v.(MarketResult)
	if !ok {
		res = MarketResult{CachedAt: nowMs(), Error: "internal error while querying GitHub"}
	}
	res.Items = append([]MarketItem{}, res.Items...)
	return res, nil
}

func cleanQuery(q string) string {
	q = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, q)
	q = strings.Join(strings.Fields(q), " ")
	if len(q) > 100 {
		q = strings.ToValidUTF8(q[:100], "")
	}
	return q
}

// Repo scanning --------------------------------------------------------------------------------

// ValidRepo reports whether s is a safe "owner/name" GitHub repository path.
func ValidRepo(s string) bool {
	if !repoRe.MatchString(s) {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if p == "." || p == ".." {
			return false
		}
	}
	return true
}

// scanned is the cached, install-state independent part of a RepoPlugin.
type scanned struct {
	path, url, err string
	info           *Info
}

type scanResult struct {
	branch   string
	files    []scanned
	err      string
	cachedAt int64
}

// MarketRepo scans a repository for plugin files, compiling and validating
// each one. Install state is added on every call, the scan itself is cached.
func (m *Manager) MarketRepo(repo string) (RepoScan, error) {
	if !ValidRepo(repo) {
		return RepoScan{}, invalidf("repo must look like owner/name")
	}
	v := m.marketCached("r|"+strings.ToLower(repo), func(ctx context.Context) (any, bool) {
		r := m.scanRepo(ctx, repo)
		r.cachedAt = nowMs()
		return r, r.err != ""
	})
	r, ok := v.(scanResult)
	if !ok {
		r = scanResult{err: "internal error while scanning the repository", cachedAt: nowMs()}
	}
	out := RepoScan{Repo: repo, DefaultBranch: r.branch, Plugins: make([]RepoPlugin, 0, len(r.files)), CachedAt: r.cachedAt, Error: r.err}
	inst := m.installedVersions()
	for _, f := range r.files {
		rp := RepoPlugin{Path: f.path, URL: f.url, Error: f.err}
		if f.info != nil {
			cp := *f.info
			rp.Plugin = &cp
			if ver, ok := inst[cp.ID]; ok {
				rp.Installed, rp.InstalledVersion = true, ver
			}
		}
		out.Plugins = append(out.Plugins, rp)
	}
	return out, nil
}

// excludedDirs are never scanned (any other hidden directory is skipped too).
var excludedDirs = map[string]bool{"node_modules": true, "test": true, "tests": true, "dist": true, "__tests__": true}

type treeFile struct {
	path string
	size int64
}

// candidate reports whether a tree path may hold a plugin.
func candidate(p string, size int64) bool {
	lp := strings.ToLower(p)
	if !(strings.HasSuffix(lp, ".js") || strings.HasSuffix(lp, ".ts")) ||
		strings.HasSuffix(lp, ".d.ts") || strings.HasSuffix(lp, ".min.js") {
		return false
	}
	if size > store.MaxPluginSource {
		return false
	}
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		if strings.HasPrefix(seg, ".") || (i < len(parts)-1 && excludedDirs[strings.ToLower(seg)]) {
			return false
		}
	}
	return true
}

func rawFileURL(base, repo, branch, file string) string {
	segs := strings.Split(branch, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	fs := strings.Split(file, "/")
	for i, s := range fs {
		fs[i] = url.PathEscape(s)
	}
	return base + "/" + repo + "/" + strings.Join(segs, "/") + "/" + strings.Join(fs, "/")
}

func (m *Manager) scanRepo(ctx context.Context, repo string) scanResult {
	res := scanResult{files: []scanned{}}
	var info struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := m.ghJSON(ctx, m.ghAPI()+"/repos/"+repo, &info); err != nil {
		res.err = repoErr(err)
		return res
	}
	branch := info.DefaultBranch
	if branch == "" {
		res.err = "the repository has no default branch"
		return res
	}
	res.branch = branch
	base := m.ghRaw()

	// explicit list: genkan-plugins.json at the repository root
	var paths []string
	explicit := false
	b, status, _, err := m.ghGet(ctx, rawFileURL(base, repo, branch, "genkan-plugins.json"), false, maxManifestList)
	switch {
	case err != nil:
		res.err = err.Error()
		return res
	case status == http.StatusOK:
		var lst struct {
			Plugins []string `json:"plugins"`
		}
		if err := json.Unmarshal(b, &lst); err != nil {
			res.err = "genkan-plugins.json is not valid JSON: " + trimErr(err)
			return res
		}
		explicit = true
		seen := map[string]bool{}
		for _, p := range lst.Plugins {
			p = strings.TrimPrefix(strings.TrimSpace(p), "./")
			if p == "" || seen[p] || path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "../") || p == ".." {
				continue
			}
			if e := strings.ToLower(path.Ext(p)); e != ".js" && e != ".ts" {
				continue
			}
			seen[p] = true
			paths = append(paths, p)
		}
	case status != http.StatusNotFound:
		res.err = fmt.Sprintf("could not read genkan-plugins.json (HTTP %d)", status)
		return res
	}

	if !explicit {
		var tree struct {
			Tree []struct {
				Path string `json:"path"`
				Type string `json:"type"`
				Size int64  `json:"size"`
			} `json:"tree"`
		}
		if err := m.ghJSON(ctx, m.ghAPI()+"/repos/"+repo+"/git/trees/"+url.PathEscape(branch)+"?recursive=1", &tree); err != nil {
			res.err = repoErr(err)
			return res
		}
		var files []treeFile
		for _, t := range tree.Tree {
			if t.Type == "blob" && candidate(t.Path, t.Size) {
				files = append(files, treeFile{t.Path, t.Size})
			}
		}
		sort.Slice(files, func(i, j int) bool {
			di, dj := strings.Count(files[i].path, "/"), strings.Count(files[j].path, "/")
			if di != dj {
				return di < dj
			}
			return files[i].path < files[j].path
		})
		for _, f := range files {
			paths = append(paths, f.path)
		}
	}
	if len(paths) > maxScanFiles {
		paths = paths[:maxScanFiles]
	}

	out := make([]*scanned, len(paths))
	var wg sync.WaitGroup
	sem := make(chan struct{}, scanWorkers)
	for i, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			defer func() {
				if r := recover(); r != nil {
					out[i] = &scanned{path: p, url: rawFileURL(base, repo, branch, p), err: "internal error while checking this file"}
				}
			}()
			out[i] = m.scanFile(ctx, base, repo, branch, p, explicit)
		}()
	}
	wg.Wait()
	for _, f := range out {
		if f != nil {
			res.files = append(res.files, *f)
		}
	}
	return res
}

func repoErr(err error) string {
	if err.Error() == "not found on GitHub" {
		return "repository not found (or it is private)"
	}
	return err.Error()
}

// scanFile downloads and validates one candidate. In automatic mode files
// that do not look like plugins are skipped silently (nil).
func (m *Manager) scanFile(ctx context.Context, base, repo, branch, p string, explicit bool) *scanned {
	f := &scanned{path: p, url: rawFileURL(base, repo, branch, p)}
	b, status, _, err := m.ghGet(ctx, f.url, false, store.MaxPluginSource)
	switch {
	case err != nil:
		f.err = "download failed: " + err.Error()
		return f
	case status != http.StatusOK:
		f.err = fmt.Sprintf("download failed: HTTP %d", status)
		return f
	}
	src := string(b)
	if !explicit && !(strings.Contains(src, "manifest") && strings.Contains(src, "export")) {
		return nil
	}
	if err := store.ValidateSourceText(src); err != nil {
		f.err = err.Error()
		return f
	}
	lang := "js"
	if strings.HasSuffix(strings.ToLower(p), ".ts") {
		lang = "ts"
	}
	man, _, err := m.inspect(src, lang)
	if err != nil {
		f.err = "invalid plugin: " + err.Error()
		return f
	}
	f.info = &Info{Manifest: *man}
	return f
}
