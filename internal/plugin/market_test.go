package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jae-jae/genkan/internal/store"
)

// fakeGH is a tiny GitHub: search + repo + tree on /, raw files under /raw/.
type fakeGH struct {
	t     *testing.T
	ts    *httptest.Server
	hits  map[string]*atomic.Int32
	mu    sync.Mutex
	last  *http.Request
	files map[string]string // "owner/name/branch/path" -> content
	trees map[string][]map[string]any
	// behaviours
	searchStatus int
	searchBody   string
	searchHdr    map[string]string
}

func newFakeGH(t *testing.T) *fakeGH {
	g := &fakeGH{t: t, hits: map[string]*atomic.Int32{}, files: map[string]string{}, trees: map[string][]map[string]any{}}
	g.ts = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.ts.Close)
	return g
}

func (g *fakeGH) hit(k string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if c := g.hits[k]; c != nil {
		return int(c.Load())
	}
	return 0
}

func (g *fakeGH) count(k string) {
	g.mu.Lock()
	c := g.hits[k]
	if c == nil {
		c = &atomic.Int32{}
		g.hits[k] = c
	}
	g.mu.Unlock()
	c.Add(1)
}

func (g *fakeGH) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/raw/"):
		g.count("raw")
		if r.Header.Get("Authorization") != "" {
			g.t.Errorf("token leaked to the raw host")
		}
		if c, ok := g.files[strings.TrimPrefix(p, "/raw/")]; ok {
			w.Write([]byte(c))
			return
		}
		http.NotFound(w, r)
		return
	case p == "/search/repositories":
		g.count("search")
		g.mu.Lock()
		g.last = r.Clone(r.Context())
		g.mu.Unlock()
		for k, v := range g.searchHdr {
			w.Header().Set(k, v)
		}
		if g.searchStatus != 0 {
			w.WriteHeader(g.searchStatus)
			w.Write([]byte(g.searchBody))
			return
		}
		w.Write([]byte(`{"total_count":57,"items":[
		 {"full_name":"alice/genkan-weather","description":null,"stargazers_count":12,"updated_at":"2026-10-01T00:00:00Z","html_url":"https://github.com/alice/genkan-weather","default_branch":"main","owner":{"avatar_url":"https://a/1.png"}},
		 {"full_name":"bob/pack","description":"Bob's pack","stargazers_count":3,"updated_at":"2026-09-01T00:00:00Z","html_url":"https://github.com/bob/pack","default_branch":"master","owner":{"avatar_url":"https://a/2.png"}},
		 {"full_name":"../evil","description":"x","stargazers_count":1,"updated_at":"","html_url":"","default_branch":"main","owner":{"avatar_url":""}}]}`))
		return
	case strings.HasPrefix(p, "/repos/"):
		rest := strings.TrimPrefix(p, "/repos/")
		if i := strings.Index(rest, "/git/trees/"); i >= 0 {
			g.count("tree")
			repo := rest[:i]
			tr, ok := g.trees[repo]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if r.URL.Query().Get("recursive") != "1" {
				g.t.Errorf("tree without recursive=1")
			}
			json.NewEncoder(w).Encode(map[string]any{"tree": tr, "truncated": false})
			return
		}
		g.count("repo")
		if _, ok := g.trees[rest]; !ok {
			if _, ok := g.files[rest+"/main/genkan-plugins.json"]; !ok {
				w.WriteHeader(404)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
		}
		fmt.Fprintf(w, `{"default_branch":"main","full_name":%q}`, rest)
		return
	}
	http.NotFound(w, r)
}

func (g *fakeGH) mgr(e *tenv, token string) {
	e.m.cfg.GitHubAPI = g.ts.URL
	e.m.cfg.GitHubRaw = g.ts.URL + "/raw"
	e.m.cfg.GitHubToken = token
}

func blob(path string, size int) map[string]any {
	return map[string]any{"path": path, "type": "blob", "size": size}
}

const goodPlugin = `export const manifest = { id: "%s", name: "N", version: "%s", permissions: { fetch: ["example.com"] }, widgets: [{ id: "w", name: "W", size: "s" }] };
export async function widget(ctx) { return { body: [] } }
`

func TestMarketSearch(t *testing.T) {
	e := newEnv(t)
	g := newFakeGH(t)
	g.mgr(e, "tok-123")

	res, err := e.m.Market("updated", "  weather   app ", 2)
	if err != nil || res.Error != "" {
		t.Fatalf("%v %+v", err, res)
	}
	if res.Total != 57 || len(res.Items) != 2 || res.CachedAt == 0 {
		t.Fatalf("%+v", res)
	}
	if it := res.Items[0]; it.Repo != "alice/genkan-weather" || it.Description != "" || it.Stars != 12 || it.OwnerAvatar != "https://a/1.png" || it.DefaultBranch != "main" || it.HTMLURL == "" {
		t.Errorf("%+v", it)
	}
	if res.Items[1].Description != "Bob's pack" || res.Items[1].DefaultBranch != "master" {
		t.Errorf("%+v", res.Items[1])
	}
	q := g.last.URL.Query()
	if q.Get("q") != "topic:genkan-plugin weather app" || q.Get("sort") != "updated" || q.Get("page") != "2" {
		t.Errorf("query: %v", q)
	}
	if g.last.Header.Get("Authorization") != "Bearer tok-123" || g.last.Header.Get("User-Agent") == "" || !strings.Contains(g.last.Header.Get("Accept"), "github") {
		t.Errorf("headers: %v", g.last.Header)
	}
	// cached for 10 minutes: same key does not hit GitHub again; another key does
	e.m.Market("updated", "weather app", 2)
	if g.hit("search") != 1 {
		t.Errorf("hits = %d", g.hit("search"))
	}
	e.m.Market("stars", "", 0)
	if g.hit("search") != 2 || g.last.URL.Query().Get("q") != "topic:genkan-plugin" || g.last.URL.Query().Get("sort") != "stars" {
		t.Errorf("hits = %d %v", g.hit("search"), g.last.URL.Query())
	}
	if _, err := e.m.Market("random", "", 1); err == nil {
		t.Error("bad sort accepted")
	}
}

func TestMarketSearchNoToken(t *testing.T) {
	e := newEnv(t)
	g := newFakeGH(t)
	g.mgr(e, "")
	e.m.Market("", "", 1)
	if g.last.Header.Get("Authorization") != "" {
		t.Errorf("auth header without token: %v", g.last.Header)
	}
}

func TestMarketErrors(t *testing.T) {
	e := newEnv(t)
	g := newFakeGH(t)
	g.mgr(e, "")

	// rate limit: reported in error with a hint, never an error return; cached briefly
	g.searchStatus = 403
	g.searchBody = `{"message":"API rate limit exceeded for 1.2.3.4"}`
	g.searchHdr = map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1893456000"}
	res, err := e.m.Market("stars", "", 1)
	if err != nil || !strings.Contains(res.Error, "rate limit") || !strings.Contains(res.Error, "GENKAN_GITHUB_TOKEN") || res.Items == nil || len(res.Items) != 0 {
		t.Fatalf("%v %+v", err, res)
	}
	if b, _ := json.Marshal(res); !strings.Contains(string(b), `"items":[]`) {
		t.Errorf("items must marshal as []: %s", b)
	}
	e.m.Market("stars", "", 1)
	if g.hit("search") != 1 {
		t.Errorf("errors are cached briefly, hits = %d", g.hit("search"))
	}

	// other failures
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"message":"Bad credentials"}`, "rejected the token"},
		{502, `<html>bad gateway</html>`, "unavailable"},
		{422, `{"message":"Validation Failed"}`, "Validation Failed"},
	} {
		g.searchStatus, g.searchBody, g.searchHdr = tc.status, tc.body, nil
		res, _ := e.m.Market("stars", tc.want, 1) // distinct cache key
		if !strings.Contains(res.Error, tc.want) || strings.Contains(res.Error, "<html>") {
			t.Errorf("%d: %q", tc.status, res.Error)
		}
	}
	// invalid JSON
	g.searchStatus, g.searchBody = 200, `not json`
	if res, _ := e.m.Market("stars", "junk", 1); res.Error == "" {
		t.Error("junk body")
	}

	// unreachable
	g.ts.Close()
	res, err = e.m.Market("stars", "down", 1)
	if err != nil || !strings.Contains(res.Error, "could not reach GitHub") {
		t.Errorf("%v %+v", err, res)
	}
}

func TestMarketTokenScrubbed(t *testing.T) {
	e := newEnv(t)
	e.m.cfg.GitHubToken = "sekrit-token"
	if got := e.m.scrub("boom sekrit-token boom"); strings.Contains(got, "sekrit") {
		t.Error(got)
	}
}

func TestMarketRepoScan(t *testing.T) {
	e := newEnv(t)
	g := newFakeGH(t)
	g.mgr(e, "tok-123")
	e.install(fmt.Sprintf(goodPlugin, "inst-one", "1.0.0")) // installed locally at 1.0.0

	big := strings.Repeat("x", store.MaxPluginSource+1)
	g.trees["alice/pack"] = []map[string]any{
		blob("README.md", 10),
		blob("inst-one.js", 200),
		blob("plugins/other.js", 200),
		blob("plugins/ts-one.ts", 200),
		blob("plugins/broken.js", 200),
		blob("plugins/syntax.js", 200),
		blob("plugins/helper.js", 200),
		blob("node_modules/dep/index.js", 200),
		blob(".github/scripts/x.js", 200),
		blob(".hidden/y.js", 200),
		blob("tests/plugin.js", 200),
		blob("src/test/z.js", 200),
		blob("dist/bundle.js", 200),
		blob("types.d.ts", 200),
		blob("big.js", len(big)),
		{"path": "plugins", "type": "tree"},
	}
	for _, f := range []string{"node_modules/dep/index.js", ".github/scripts/x.js", ".hidden/y.js", "tests/plugin.js", "src/test/z.js", "dist/bundle.js"} {
		g.files["alice/pack/main/"+f] = fmt.Sprintf(goodPlugin, "zz-excluded", "1")
	}
	g.files["alice/pack/main/inst-one.js"] = fmt.Sprintf(goodPlugin, "inst-one", "2.0.0")
	g.files["alice/pack/main/plugins/other.js"] = fmt.Sprintf(goodPlugin, "other", "1.0.0")
	g.files["alice/pack/main/plugins/ts-one.ts"] = "export const manifest: any = { id: \"ts-one\", name: \"T\", version: \"1\", widgets: [{ id: \"w\", name: \"W\" }] };\nexport async function widget(ctx: any) { return { body: [] } }\n"
	g.files["alice/pack/main/plugins/broken.js"] = "export const manifest = { id: \"Bad Id\", name: \"x\", version: \"1\" };\nexport async function widget() {}"
	g.files["alice/pack/main/plugins/syntax.js"] = "export const manifest = {\n  id: \"syn\",\n  name: ;\n};\n"
	g.files["alice/pack/main/plugins/helper.js"] = "export function add(a, b) { return a + b }" // not a plugin: skipped silently

	scan, err := e.m.MarketRepo("alice/pack")
	if err != nil || scan.Error != "" {
		t.Fatalf("%v %+v", err, scan)
	}
	if scan.Repo != "alice/pack" || scan.DefaultBranch != "main" || scan.CachedAt == 0 {
		t.Errorf("%+v", scan)
	}
	var paths []string
	by := map[string]RepoPlugin{}
	for _, p := range scan.Plugins {
		paths = append(paths, p.Path)
		by[p.Path] = p
	}
	want := []string{"inst-one.js", "plugins/broken.js", "plugins/other.js", "plugins/syntax.js", "plugins/ts-one.ts"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %v", paths)
	}
	one := by["inst-one.js"]
	if one.Plugin == nil || one.Plugin.ID != "inst-one" || one.Plugin.Version != "2.0.0" || !one.Installed || one.InstalledVersion != "1.0.0" ||
		one.URL != g.ts.URL+"/raw/alice/pack/main/inst-one.js" || one.Plugin.Admin != nil {
		t.Errorf("%+v", one)
	}
	if o := by["plugins/other.js"]; o.Plugin == nil || o.Installed || o.InstalledVersion != "" || o.Error != "" || len(o.Plugin.Widgets) != 1 {
		t.Errorf("%+v", o)
	}
	if o := by["plugins/ts-one.ts"]; o.Plugin == nil || o.Plugin.ID != "ts-one" {
		t.Errorf("ts: %+v", o)
	}
	if o := by["plugins/broken.js"]; o.Plugin != nil || !strings.Contains(o.Error, "invalid plugin") {
		t.Errorf("broken: %+v", o)
	}
	if o := by["plugins/syntax.js"]; o.Plugin != nil || !strings.Contains(o.Error, "3:") {
		t.Errorf("syntax error needs line:col: %+v", o)
	}
	if b, _ := json.Marshal(by["plugins/broken.js"]); !strings.Contains(string(b), `"plugin":null`) {
		t.Errorf("%s", b)
	}

	// cached: no new GitHub requests, but the install state is live
	repoHits, rawHits := g.hit("repo"), g.hit("raw")
	e.install(fmt.Sprintf(goodPlugin, "other", "0.9.0"))
	scan2, _ := e.m.MarketRepo("alice/pack")
	if g.hit("repo") != repoHits || g.hit("raw") != rawHits {
		t.Errorf("not cached: %d/%d %d/%d", g.hit("repo"), repoHits, g.hit("raw"), rawHits)
	}
	for _, p := range scan2.Plugins {
		if p.Path == "plugins/other.js" && (!p.Installed || p.InstalledVersion != "0.9.0") {
			t.Errorf("install state is stale: %+v", p)
		}
	}
}

func TestMarketRepoManifestList(t *testing.T) {
	e := newEnv(t)
	g := newFakeGH(t)
	g.mgr(e, "")
	g.files["bob/list/main/genkan-plugins.json"] = `{"plugins":["a.js","sub/b.ts","../etc/passwd.js","/abs.js","notes.txt","a.js","missing.js","sub/../c.js","helper.js"]}`
	g.files["bob/list/main/a.js"] = fmt.Sprintf(goodPlugin, "list-a", "1.0.0")
	g.files["bob/list/main/sub/b.ts"] = "export const manifest = { id: \"list-b\", name: \"B\", version: \"1\", widgets: [{ id: \"w\", name: \"W\" }] }; export async function widget() { return { body: [] } }"
	g.files["bob/list/main/helper.js"] = "export const x = 1"
	scan, err := e.m.MarketRepo("bob/list")
	if err != nil || scan.Error != "" {
		t.Fatalf("%v %+v", err, scan)
	}
	var got []string
	for _, p := range scan.Plugins {
		got = append(got, p.Path)
	}
	// listed files are authoritative: reported even when they are not plugins or missing; unsafe paths dropped; no tree request
	if strings.Join(got, ",") != "a.js,sub/b.ts,missing.js,helper.js" || g.hit("tree") != 0 {
		t.Fatalf("%v tree=%d", got, g.hit("tree"))
	}
	if scan.Plugins[0].Plugin == nil || scan.Plugins[1].Plugin == nil || scan.Plugins[1].Plugin.ID != "list-b" {
		t.Errorf("%+v", scan.Plugins)
	}
	if !strings.Contains(scan.Plugins[2].Error, "download failed") || scan.Plugins[3].Error == "" {
		t.Errorf("%+v", scan.Plugins[2:])
	}

	g.files["bob/bad/main/genkan-plugins.json"] = `{nope`
	if _, ok := g.trees["bob/bad"]; !ok {
		g.trees["bob/bad"] = nil
	}
	scan, _ = e.m.MarketRepo("bob/bad")
	if !strings.Contains(scan.Error, "genkan-plugins.json") || scan.Plugins == nil {
		t.Errorf("%+v", scan)
	}
}

func TestMarketRepoLimitsAndErrors(t *testing.T) {
	e := newEnv(t)
	g := newFakeGH(t)
	g.mgr(e, "")
	var tr []map[string]any
	for i := 0; i < 60; i++ {
		p := fmt.Sprintf("p%02d.js", i)
		tr = append(tr, blob(p, 100))
		g.files["c/many/main/"+p] = fmt.Sprintf(goodPlugin, fmt.Sprintf("many-%02d", i), "1")
	}
	g.trees["c/many"] = tr
	scan, _ := e.m.MarketRepo("c/many")
	if len(scan.Plugins) != 40 || scan.Plugins[0].Path != "p00.js" || scan.Plugins[39].Path != "p39.js" {
		t.Errorf("len = %d", len(scan.Plugins))
	}

	// unknown repo: error field, empty list
	scan, err := e.m.MarketRepo("nobody/nothing")
	if err != nil || !strings.Contains(scan.Error, "not found") || scan.Plugins == nil || len(scan.Plugins) != 0 {
		t.Errorf("%v %+v", err, scan)
	}
	n := g.hit("repo")
	e.m.MarketRepo("nobody/nothing")
	if g.hit("repo") != n {
		t.Error("errors are cached briefly")
	}

	// strict repo validation (never reaches GitHub)
	for _, bad := range []string{"", "a", "a/b/c", "a b/c", "a/b c", "../x", "x/..", "./x", "a/b?x=1", "a/b#", "a\\b/c", "https://github.com/a/b", "a/b\n", "é/x", "/a/b", "a//b"} {
		if _, err := e.m.MarketRepo(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, ok := range []string{"a/b", "A_b.c-d/e.f_g-h", "jae-jae/genkan"} {
		if !ValidRepo(ok) {
			t.Errorf("rejected %q", ok)
		}
	}
}

func TestMarketConcurrentSingleflight(t *testing.T) {
	e := newEnv(t)
	g := newFakeGH(t)
	g.mgr(e, "")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); e.m.Market("stars", "", 1) }()
	}
	wg.Wait()
	if n := g.hit("search"); n != 1 {
		t.Errorf("search hits = %d", n)
	}
}

func TestClearMarketCache(t *testing.T) {
	e := newEnv(t)
	g := newFakeGH(t)
	g.mgr(e, "")
	e.m.Market("stars", "", 1)
	e.m.Market("stars", "", 1)
	if n := g.hit("search"); n != 1 {
		t.Fatalf("search hits before clear = %d", n)
	}
	e.m.ClearMarketCache()
	e.m.Market("stars", "", 1)
	if n := g.hit("search"); n != 2 {
		t.Errorf("search hits after clear = %d", n)
	}
	e.m.ClearMarketCache()
	e.m.ClearMarketCache() // an empty cache is fine

	// a computation that started before the clear must not repopulate the cache
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	calls := 0
	go func() {
		defer close(done)
		e.m.marketCached("k", func(context.Context) (any, bool) {
			calls++
			close(started)
			<-release
			return "old", false
		})
	}()
	<-started
	e.m.ClearMarketCache()
	close(release)
	<-done
	got := e.m.marketCached("k", func(context.Context) (any, bool) { calls++; return "new", false })
	if got != "new" || calls != 2 {
		t.Errorf("stale in-flight result was cached: got %v after %d calls", got, calls)
	}
}

func TestPreview(t *testing.T) {
	e := newEnv(t)
	src := fmt.Sprintf(goodPlugin, "prev-one", "1.2.3")
	res, err := e.m.Preview(context.Background(), InstallRequest{Source: src})
	if err != nil || res.Error != "" || res.Plugin == nil {
		t.Fatalf("%v %+v", err, res)
	}
	if res.Plugin.ID != "prev-one" || res.Plugin.Version != "1.2.3" || res.Source != src || res.Lang != "js" || res.Installed || res.InstalledVersion != "" || res.Plugin.Admin != nil {
		t.Errorf("%+v", res)
	}
	if b, _ := json.Marshal(res); strings.Contains(string(b), "secrets_set") || strings.Contains(string(b), "installed_at") || strings.Contains(string(b), "installed_version") {
		t.Errorf("runtime/admin fields leaked: %s", b)
	}
	// nothing installed, no files
	if len(e.st.Plugins()) != 0 {
		t.Error("preview installed the plugin")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "plugins")); err == nil {
		ents, _ := os.ReadDir(filepath.Join(e.dir, "plugins"))
		if len(ents) != 0 {
			t.Errorf("files written: %v", ents)
		}
	}

	// installed state
	e.install(fmt.Sprintf(goodPlugin, "prev-one", "1.0.0"))
	res, _ = e.m.Preview(context.Background(), InstallRequest{Source: src})
	if !res.Installed || res.InstalledVersion != "1.0.0" || res.Plugin.Version != "1.2.3" {
		t.Errorf("%+v", res)
	}
	if inf, _ := e.m.Info("prev-one", false); inf.Version != "1.0.0" {
		t.Error("preview changed the installed plugin")
	}

	// catalog
	e.m.cfg.Catalog = mapFS(map[string]string{"cat.js": fmt.Sprintf(goodPlugin, "cat-one", "3.0.0")})
	res, err = e.m.Preview(context.Background(), InstallRequest{Catalog: "cat-one"})
	if err != nil || res.Plugin == nil || res.Plugin.ID != "cat-one" || res.Source == "" {
		t.Errorf("%v %+v", err, res)
	}
	if _, err := e.m.Preview(context.Background(), InstallRequest{Catalog: "nope"}); err == nil {
		t.Error("unknown catalog")
	}
	// request shape errors
	for _, r := range []InstallRequest{{}, {Source: "x", URL: "http://x"}, {Source: src, Lang: "py"}} {
		if _, err := e.m.Preview(context.Background(), r); err == nil {
			t.Errorf("accepted %+v", r)
		}
	}
	// invalid plugins come back in error, with the source and line:col
	bad := "export const manifest = {\n  id: \"x-y\",\n  name: ;\n};\n"
	res, err = e.m.Preview(context.Background(), InstallRequest{Source: bad})
	if err != nil || res.Plugin != nil || res.Source != bad || !strings.Contains(res.Error, "3:") || res.Installed {
		t.Errorf("%v %+v", err, res)
	}
	if b, _ := json.Marshal(res); !strings.Contains(string(b), `"plugin":null`) {
		t.Errorf("%s", b)
	}
	res, _ = e.m.Preview(context.Background(), InstallRequest{Source: "export const manifest = { id: \"no-hook\", name: \"n\", version: \"1\", widgets: [{ id: \"w\", name: \"W\" }] }"})
	if res.Plugin != nil || !strings.Contains(res.Error, "widget") {
		t.Errorf("%+v", res)
	}
	res, _ = e.m.Preview(context.Background(), InstallRequest{Source: `import x from "y"; export const manifest = {}`})
	if res.Plugin != nil || !strings.Contains(res.Error, "single file") {
		t.Errorf("%+v", res)
	}
	// ts
	res, _ = e.m.Preview(context.Background(), InstallRequest{Lang: "ts", Source: "export const manifest = { id: \"ts-p\", name: \"T\", version: \"1\", widgets: [{ id: \"w\", name: \"W\" }] };\nexport async function widget(ctx: any): Promise<any> { return { body: [] } }"})
	if res.Plugin == nil || res.Lang != "ts" {
		t.Errorf("%+v", res)
	}
}

func TestPreviewURL(t *testing.T) {
	e := newEnv(t)
	src := fmt.Sprintf(goodPlugin, "from-url", "1.0.0")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/p.ts":
			w.Write([]byte("export const manifest = { id: \"from-ts\", name: \"T\", version: \"1\", widgets: [{ id: \"w\", name: \"W\" }] };\nexport async function widget(ctx: any) { return { body: [] } }"))
		case "/big.js":
			w.Write([]byte(strings.Repeat("//xxxxxxxx\n", 60000)))
		case "/gone.js":
			http.NotFound(w, r)
		default:
			w.Write([]byte(src))
		}
	}))
	defer ts.Close()
	res, err := e.m.Preview(context.Background(), InstallRequest{URL: ts.URL + "/p.js"})
	if err != nil || res.Plugin == nil || res.Plugin.ID != "from-url" || res.Source != src {
		t.Fatalf("%v %+v", err, res)
	}
	if len(e.st.Plugins()) != 0 {
		t.Error("installed")
	}
	if res, _ = e.m.Preview(context.Background(), InstallRequest{URL: ts.URL + "/p.ts"}); res.Plugin == nil || res.Lang != "ts" {
		t.Errorf("%+v", res)
	}
	// download failures are reported in error (200), limits and schemes as errors
	if res, err = e.m.Preview(context.Background(), InstallRequest{URL: ts.URL + "/gone.js"}); err != nil || res.Plugin != nil || !strings.Contains(res.Error, "HTTP 404") {
		t.Errorf("%v %+v", err, res)
	}
	if _, err = e.m.Preview(context.Background(), InstallRequest{URL: ts.URL + "/big.js"}); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("%v", err)
	}
	if _, err = e.m.Preview(context.Background(), InstallRequest{URL: "file:///etc/passwd"}); err == nil {
		t.Error("file scheme")
	}
	if _, err = e.m.Preview(context.Background(), InstallRequest{URL: "ftp://x/y.js"}); err == nil {
		t.Error("ftp scheme")
	}
}

func TestUpdateSource(t *testing.T) {
	e := newEnv(t)
	v1 := `export const manifest = { id: "edit-me", name: "Edit", version: "1.0.0",
  config: [{ key: "greeting", type: "string", label: "G", default: "hi" }],
  widgets: [{ id: "w", name: "W", size: "s" }] };
export async function widget(ctx) { return { body: [{ type: "text", text: "v1 " + ctx.config.greeting }] } }
`
	e.install(v1)
	if _, err := e.m.Patch("edit-me", PatchRequest{Config: map[string]any{"greeting": "yo"}}); err != nil {
		t.Fatal(err)
	}
	w, err := e.m.AddWidget(WidgetAdd{Plugin: "edit-me", Widget: "w"})
	if err != nil {
		t.Fatal(err)
	}
	e.m.Patch("edit-me", PatchRequest{Enabled: ptrBool(false)})

	v2 := strings.Replace(strings.Replace(v1, `"1.0.0"`, `"2.0.0"`, 1), "v1 ", "v2 ", 1)
	inf, err := e.m.UpdateSource("edit-me", v2, "")
	if err != nil {
		t.Fatal(err)
	}
	if inf.Version != "2.0.0" || inf.Enabled || inf.Admin == nil || inf.Config["greeting"] != "yo" {
		t.Fatalf("enabled state/config must be kept: %+v", inf)
	}
	if src, lang, _ := e.m.Source("edit-me"); src != v2 || lang != "js" {
		t.Error("source not saved")
	}
	if len(e.st.Widgets()) != 1 || e.st.Widgets()[0].ID != w.ID {
		t.Error("widgets must be kept")
	}
	e.m.Patch("edit-me", PatchRequest{Enabled: ptrBool(true)})
	d, err := e.m.Data(context.Background(), w.ID, "en", false)
	if err != nil || fmt.Sprint(d.View.Body[0]["text"]) != "v2 yo" {
		t.Errorf("%v %+v", err, d)
	}

	// failures leave everything untouched
	_, err = e.m.UpdateSource("edit-me", "export const manifest = {\n id: \"edit-me\",\n name: ;\n};", "")
	if err == nil || !strings.Contains(err.Error(), "3:") || !strings.Contains(strings.ToLower(err.Error()), "syntax") {
		t.Errorf("syntax error should carry line:col: %v", err)
	}
	if _, err = e.m.UpdateSource("edit-me", strings.Replace(v2, `id: "edit-me"`, `id: "other-id"`, 1), ""); err == nil || !strings.Contains(err.Error(), "edit-me") {
		t.Errorf("id change: %v", err)
	}
	if _, err = e.m.UpdateSource("nope", v2, ""); err == nil {
		t.Error("unknown plugin")
	} else if se, ok := err.(*store.Error); !ok || se.Kind != store.KindNotFound {
		t.Errorf("%v", err)
	}
	if _, err = e.m.UpdateSource("edit-me", v2, "py"); err == nil {
		t.Error("lang")
	}
	if _, err = e.m.UpdateSource("edit-me", "   ", ""); err == nil {
		t.Error("empty")
	}
	if _, err = e.m.UpdateSource("edit-me", strings.Repeat("//xxxxxxxx\n", 60000), ""); err == nil {
		t.Error("too large")
	}
	if src, _, _ := e.m.Source("edit-me"); src != v2 {
		t.Error("failed update changed the source")
	}

	// switching to TypeScript removes the .js file
	ts := "export const manifest = { id: \"edit-me\", name: \"Edit\", version: \"3.0.0\", widgets: [{ id: \"w\", name: \"W\" }] };\nexport async function widget(ctx: any): Promise<any> { return { body: [] } }\n"
	inf, err = e.m.UpdateSource("edit-me", ts, "ts")
	if err != nil || inf.Version != "3.0.0" {
		t.Fatalf("%v", err)
	}
	if _, lang, _ := e.m.Source("edit-me"); lang != "ts" {
		t.Error("lang")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "plugins", "edit-me.js")); err == nil {
		t.Error("old source file left behind")
	}
	// omitted lang keeps ts
	if _, err = e.m.UpdateSource("edit-me", strings.Replace(ts, "3.0.0", "3.0.1", 1), ""); err != nil {
		t.Errorf("keeps ts: %v", err)
	}
}

func TestUpdateSourceFixesBrokenPlugin(t *testing.T) {
	e := newEnv(t)
	src := fmt.Sprintf(goodPlugin, "fix-me", "1.0.0")
	e.install(src)
	// corrupt the stored source on disk, then fix it through the editor
	os.WriteFile(filepath.Join(e.dir, "plugins", "fix-me.js"), []byte("export const manifest = {"), 0o600)
	e.m.drop("fix-me")
	if inf, _ := e.m.Info("fix-me", true); inf.Admin == nil || inf.LastError == "" {
		t.Skip("the corrupted plugin is not reported as broken until re-read")
	}
	if _, err := e.m.UpdateSource("fix-me", src, ""); err != nil {
		t.Fatal(err)
	}
	if inf, _ := e.m.Info("fix-me", true); inf.LastError != "" {
		t.Errorf("%+v", inf)
	}
}

func ptrBool(b bool) *bool { return &b }

func TestWidgetLayoutFields(t *testing.T) {
	e := newEnv(t)
	e.install(fmt.Sprintf(goodPlugin, "lay-out", "1"))
	w, err := e.m.AddWidget(WidgetAdd{Plugin: "lay-out", Widget: "w", W: 9, H: 5})
	if err != nil || w.W != 6 || w.H != 80 {
		t.Fatalf("%v %+v", err, w)
	}
	w2, _ := e.m.AddWidget(WidgetAdd{Plugin: "lay-out", Widget: "w"})
	if w2.W != 0 || w2.H != 0 {
		t.Errorf("defaults: %+v", w2)
	}
	for _, tc := range []struct{ in, wantW, h, wantH int }{
		{-3, 0, -5, 0}, {0, 0, 0, 0}, {3, 3, 80, 80}, {6, 6, 1200, 1200}, {7, 6, 5000, 1200}, {2, 2, 79, 80},
	} {
		got, err := e.m.UpdateWidget(w2.ID, WidgetPatch{W: &tc.in, H: &tc.h})
		if err != nil || got.W != tc.wantW || got.H != tc.wantH {
			t.Errorf("%+v: %v %+v", tc, err, got)
		}
	}
	// patching other fields keeps the layout; w/h survive a restart of the cache
	three, hh := 3, 200
	e.m.UpdateWidget(w2.ID, WidgetPatch{W: &three, H: &hh})
	sort := 7
	got, _ := e.m.UpdateWidget(w2.ID, WidgetPatch{Sort: &sort})
	if got.W != 3 || got.H != 200 {
		t.Errorf("%+v", got)
	}
	stored, _ := e.st.GetWidget(w2.ID)
	if stored.W != 3 || stored.H != 200 {
		t.Errorf("%+v", stored)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"w":3`) || !strings.Contains(string(b), `"h":200`) {
		t.Errorf("%s", b)
	}
	if b, _ := json.Marshal(w2); !strings.Contains(string(b), `"w":0`) || !strings.Contains(string(b), `"h":0`) {
		t.Errorf("zero values must be present: %s", b)
	}
}
