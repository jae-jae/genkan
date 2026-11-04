package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jae-jae/genkan/plugins"
)

// ghStub is a minimal GitHub API: repository counts and a stargazers list where
// star k was given at base + (k-1)*step.
type ghStub struct {
	ts *httptest.Server

	mu         sync.Mutex
	stars      map[string]int // lower-case owner/name -> stargazers_count
	repoReqs   map[string]int
	pages      map[string][]int // pages requested per repo, in order
	limitAfter int              // stargazer requests answered before 403 (-1 = never)
	failRepo   int              // status for GET /repos/x/y (0 = normal)
	noAuth     int              // stargazer requests refused for a missing token
	accepts    []string
	auths      []string
	base, step int64
}

func newGHStub(t *testing.T) *ghStub {
	g := &ghStub{stars: map[string]int{}, repoReqs: map[string]int{}, pages: map[string][]int{}, limitAfter: -1}
	g.ts = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.ts.Close)
	return g
}

func (g *ghStub) starTime(k int) int64 { return g.base + int64(k-1)*g.step }

func (g *ghStub) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	w.Header().Set("X-RateLimit-Remaining", "50")
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "repos" {
		http.NotFound(w, r)
		return
	}
	repo := strings.ToLower(parts[1] + "/" + parts[2])
	g.auths = append(g.auths, r.Header.Get("Authorization"))
	n, ok := g.stars[repo]
	if len(parts) == 3 {
		g.repoReqs[repo]++
		if g.failRepo != 0 {
			w.WriteHeader(g.failRepo)
			return
		}
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, 404)
			return
		}
		fmt.Fprintf(w, `{"stargazers_count":%d}`, n)
		return
	}
	if r.Header.Get("Authorization") == "" { // like the real API: stargazers need a signed-in request
		g.noAuth++
		http.Error(w, `{"message":"Requires authentication"}`, 401)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	g.accepts = append(g.accepts, r.Header.Get("Accept")+"|"+r.URL.Query().Get("per_page"))
	if g.limitAfter >= 0 && len(g.accepts) > g.limitAfter {
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, `{"message":"API rate limit exceeded"}`, 403)
		return
	}
	g.pages[repo] = append(g.pages[repo], page)
	first := (page-1)*100 + 1
	var items []string
	for k := first; k < first+2 && k <= n; k++ {
		items = append(items, fmt.Sprintf(`{"starred_at":%q,"user":{"login":"u%d"}}`, time.Unix(g.starTime(k), 0).UTC().Format(time.RFC3339), k))
	}
	fmt.Fprintf(w, "[%s]", strings.Join(items, ","))
}

func (g *ghStub) set(repo string, n int) {
	g.mu.Lock()
	g.stars[strings.ToLower(repo)] = n
	// the whole history spans ~600 days and ends a day ago
	g.base = time.Now().Unix() - 600*86400
	if n > 0 {
		g.step = 599 * 86400 / int64(n)
	}
	g.mu.Unlock()
}

func (g *ghStub) pagesOf(repo string) []int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]int(nil), g.pages[strings.ToLower(repo)]...)
}

// installStars installs github-stars from the catalog, pointed at the stub.
func installStars(t *testing.T, e *tenv, g *ghStub) {
	t.Helper()
	installStarsAt(t, e, g, time.Time{})
}

// installStarsAt freezes the plugin's clock when at is nonzero.
func installStarsAt(t *testing.T, e *tenv, g *ghStub, at time.Time) {
	t.Helper()
	b, err := fs.ReadFile(plugins.Catalog(), "github-stars.js")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.ReplaceAll(string(b), "https://api.github.com", g.ts.URL)
	src = strings.Replace(src, `fetch: ["api.github.com"]`, `fetch: ["127.0.0.1"]`, 1)
	if !strings.Contains(src, g.ts.URL) || !strings.Contains(src, "127.0.0.1") {
		t.Fatal("could not point the plugin at the stub")
	}
	if !at.IsZero() {
		// The JS engine shares immutable built-ins, so replace calls in this test's source.
		if !strings.Contains(src, "Date.now()") {
			t.Fatal("could not freeze the plugin clock")
		}
		src = strings.ReplaceAll(src, "Date.now()", strconv.FormatInt(at.UnixMilli(), 10))
	}
	e.install(src)
	if _, err := e.m.Patch("github-stars", PatchRequest{Config: map[string]any{"token": "tok-1"}}); err != nil {
		t.Fatal(err)
	}
}

func starsWidget(t *testing.T, e *tenv, repo, rng string) WidgetOut {
	t.Helper()
	return addWidget(t, e, "github-stars", "trend", map[string]any{"repo": repo, "range": rng})
}

func starsView(t *testing.T, e *tenv, id, lang string) WidgetView {
	t.Helper()
	d, err := e.m.Data(context.Background(), id, lang, true)
	if err != nil {
		t.Fatal(err)
	}
	return d.View
}

func block(v WidgetView, typ string) map[string]any {
	for _, b := range v.Body {
		if b["type"] == typ {
			return b
		}
	}
	return nil
}

func chartPts(t *testing.T, v WidgetView) [][2]float64 {
	t.Helper()
	c := block(v, "chart")
	if c == nil {
		t.Fatalf("no chart in %+v", v)
	}
	if c["kind"] != "line" || c["format"] != "int" {
		t.Fatalf("chart %+v", c)
	}
	return c["points"].([][2]float64)
}

// decSeries decodes the storage format of series:<repo>.
func decSeries(t *testing.T, raw json.RawMessage) [][2]int64 {
	t.Helper()
	var s struct {
		T int64     `json:"t"`
		C int64     `json:"c"`
		D [][]int64 `json:"d"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("series %s: %v", raw, err)
	}
	out := [][2]int64{{s.T * 60, s.C}}
	for _, d := range s.D {
		s.T += d[0]
		s.C += d[1]
		out = append(out, [2]int64{s.T * 60, s.C})
	}
	return out
}

func encSeries(pts [][2]int64) json.RawMessage {
	d := make([][2]int64, 0, len(pts))
	for i := 1; i < len(pts); i++ {
		d = append(d, [2]int64{pts[i][0]/60 - pts[i-1][0]/60, pts[i][1] - pts[i-1][1]})
	}
	b, _ := json.Marshal(map[string]any{"t": pts[0][0] / 60, "c": pts[0][1], "d": d})
	return b
}

func storedSeries(t *testing.T, e *tenv, repo string) [][2]int64 {
	t.Helper()
	ent, _, _ := e.m.entryFor("github-stars")
	raw, ok := ent.kv.get("series:" + strings.ToLower(repo))
	if !ok {
		t.Fatalf("no series for %s", repo)
	}
	return decSeries(t, raw)
}

func storedMeta(t *testing.T, e *tenv, repo string) map[string]any {
	t.Helper()
	ent, _, _ := e.m.entryFor("github-stars")
	raw, ok := ent.kv.get("meta:" + strings.ToLower(repo))
	if !ok {
		t.Fatalf("no meta for %s", repo)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

func runSchedule(t *testing.T, e *tenv) {
	t.Helper()
	res, err := e.m.Test(context.Background(), "github-stars", TestRequest{Hook: "schedule"})
	if err != nil || !res.OK {
		t.Fatalf("schedule: %+v %v", res, err)
	}
}

func TestStarsBackfillSampling(t *testing.T) {
	e := newEnv(t)
	g := newGHStub(t)
	g.set("preactjs/preact", 2345)
	installStars(t, e, g)
	if _, err := e.m.Patch("github-stars", PatchRequest{Config: map[string]any{"token": "tok-1"}}); err != nil {
		t.Fatal(err)
	}
	w := starsWidget(t, e, "preactjs/Preact", "all")

	v := starsView(t, e, w.ID, "en")
	if v.Error != "" || v.Title != "preactjs/Preact" || v.Link != "https://github.com/preactjs/Preact" {
		t.Fatalf("view %+v", v)
	}
	// ~10 pages spread over ceil(2345/100) = 24 pages: first and last included, each once
	pages := g.pagesOf("preactjs/preact")
	if len(pages) != 10 || pages[0] != 1 || pages[len(pages)-1] != 24 {
		t.Fatalf("pages %v", pages)
	}
	seen := map[int]bool{}
	for _, p := range pages {
		if seen[p] || p < 1 || p > 24 {
			t.Fatalf("bad pages %v", pages)
		}
		seen[p] = true
	}
	for _, a := range g.accepts {
		if a != "application/vnd.github.star+json|100" {
			t.Fatalf("stargazers request: %q", a)
		}
	}
	if g.repoReqs["preactjs/preact"] != 1 {
		t.Errorf("a fresh widget needs exactly one count request, got %d", g.repoReqs["preactjs/preact"])
	}
	for _, a := range g.auths {
		if a != "Bearer tok-1" {
			t.Errorf("authorization %q", a)
		}
	}
	// the points: (starred_at of the page's first item, (page-1)*100+1) and the current total last
	pts := chartPts(t, v)
	if len(pts) != 11 {
		t.Fatalf("%d points: %v", len(pts), pts)
	}
	sorted := append([]int(nil), pages...)
	sort.Ints(sorted)
	for i, p := range sorted {
		wantT, wantV := float64(g.starTime((p-1)*100+1)*1000), float64((p-1)*100+1)
		if pts[i][1] != wantV || pts[i][0] < wantT-60000 || pts[i][0] > wantT+60000 { // stored with minute precision
			t.Errorf("point %d (page %d): %v, want [%v %v]", i, p, pts[i], wantT, wantV)
		}
	}
	if pts[10][1] != 2345 || time.Since(time.UnixMilli(int64(pts[10][0]))) > time.Minute {
		t.Errorf("last point %v", pts[10])
	}
	meta := storedMeta(t, e, "preactjs/preact")
	if meta["bf"] != 2.0 || len(meta["todo"].([]any)) != 0 || meta["big"] == true {
		t.Errorf("meta %v", meta)
	}
	// the widget text
	st, stats := block(v, "stat"), block(v, "stats")
	if st["value"] != "2,345" || st["label"] != "Stars" || stats == nil {
		t.Fatalf("stat %v stats %v", st, stats)
	}
	if block(v, "text") != nil {
		t.Errorf("unexpected note: %v", block(v, "text"))
	}
	// no further backfill and no new requests while the data is fresh
	before := len(g.accepts)
	starsView(t, e, w.ID, "en")
	if len(g.accepts) != before || g.repoReqs["preactjs/preact"] != 1 {
		t.Errorf("fresh data triggered requests")
	}
}

func TestStarsBigRepoNote(t *testing.T) {
	e := newEnv(t)
	g := newGHStub(t)
	g.set("facebook/react", 230000)
	installStars(t, e, g)
	w := starsWidget(t, e, "https://github.com/facebook/react.git", "1y")
	v := starsView(t, e, w.ID, "en")
	pages := g.pagesOf("facebook/react")
	if len(pages) != 10 || pages[len(pages)-1] != 400 {
		t.Fatalf("pages %v (capped at 400)", pages)
	}
	if v.Title != "facebook/react" {
		t.Errorf("title %q", v.Title)
	}
	var note string
	for _, b := range v.Body {
		if b["type"] == "text" {
			note, _ = b["text"].(string)
		}
	}
	if !strings.Contains(note, "40,000") {
		t.Errorf("partial-history note missing: %+v", v.Body)
	}
	if st := block(v, "stat"); st["value"] != "230,000" {
		t.Errorf("stat %v", st)
	}
	vz := starsView(t, e, w.ID, "zh")
	if block(vz, "stat")["label"] != "星标" {
		t.Errorf("zh: %+v", vz.Body)
	}
}

func TestStarsRateLimitStopsAndResumes(t *testing.T) {
	e := newEnv(t)
	g := newGHStub(t)
	g.set("a/b", 5000)
	g.limitAfter = 3 // the 4th stargazers request is answered with 403
	installStars(t, e, g)
	starsWidget(t, e, "a/b", "all")
	runSchedule(t, e)
	if got := len(g.accepts); got != 4 { // 3 pages + the rejected one: no more attempts
		t.Fatalf("stargazer requests: %d", got)
	}
	meta := storedMeta(t, e, "a/b")
	if meta["bf"] != 1.0 || len(meta["todo"].([]any)) != 7 || meta["at"] != 0.0 {
		t.Fatalf("meta after the stop: %v", meta)
	}
	if n := len(storedSeries(t, e, "a/b")); n != 4 { // 3 backfill points + now
		t.Fatalf("series has %d points", n)
	}
	// a later run resumes with the remaining pages only
	g.mu.Lock()
	g.limitAfter = -1
	g.mu.Unlock()
	runSchedule(t, e)
	pages := g.pagesOf("a/b")
	if len(pages) != 10 {
		t.Fatalf("pages %v", pages)
	}
	seen := map[int]bool{}
	for _, p := range pages {
		if seen[p] {
			t.Fatalf("page %d fetched twice: %v", p, pages)
		}
		seen[p] = true
	}
	meta = storedMeta(t, e, "a/b")
	if meta["bf"] != 2.0 || len(meta["todo"].([]any)) != 0 {
		t.Fatalf("meta after resume: %v", meta)
	}
	if n := len(storedSeries(t, e, "a/b")); n != 11 {
		t.Fatalf("series has %d points", n)
	}
	// the widget shows the rate-limit reason when nothing is known yet
	e2 := newEnv(t)
	g2 := newGHStub(t)
	g2.set("x/y", 10)
	g2.failRepo = 403
	installStars(t, e2, g2)
	w := starsWidget(t, e2, "x/y", "30d")
	if v := starsView(t, e2, w.ID, "en"); !strings.Contains(v.Error, "rate limit") {
		t.Errorf("error %q", v.Error)
	}
}

func TestStarsSharedSeriesAndPrune(t *testing.T) {
	e := newEnv(t)
	g := newGHStub(t)
	g.set("a/one", 150)
	g.set("a/two", 40)
	installStars(t, e, g)
	w1 := starsWidget(t, e, "a/one", "30d")
	w2 := starsWidget(t, e, "A/One", "all") // same repository, other spelling and range
	w3 := starsWidget(t, e, "a/two", "90d")
	runSchedule(t, e)
	if n := g.repoReqs["a/one"]; n != 1 {
		t.Errorf("two instances of one repository sampled it %d times in the first run", n)
	}
	if n := g.repoReqs["a/two"]; n != 1 {
		t.Errorf("a/two: %d", n)
	}
	if p := g.pagesOf("a/one"); len(p) != 2 { // 150 stars = 2 pages, backfilled once for both instances
		t.Errorf("pages of a/one: %v", p)
	}
	v1, v2 := starsView(t, e, w1.ID, "en"), starsView(t, e, w2.ID, "en")
	if len(chartPts(t, v1)) == 0 || len(chartPts(t, v2)) == 0 || block(v1, "stat")["value"] != block(v2, "stat")["value"] {
		t.Errorf("views differ: %+v / %+v", v1, v2)
	}
	if n := g.repoReqs["a/one"]; n != 1 {
		t.Errorf("fresh data must not be refetched: %d", n)
	}
	// instance removed: the series stays for 30 days, then it is deleted
	if err := e.m.DeleteWidget(w3.ID); err != nil {
		t.Fatal(err)
	}
	runSchedule(t, e)
	ent, _, _ := e.m.entryFor("github-stars")
	if _, ok := ent.kv.get("series:a/two"); !ok {
		t.Fatal("series of an unused repository was deleted at once")
	}
	old := time.Now().Unix() - 31*86400
	_ = ent.kv.set("meta:a/two", json.RawMessage(fmt.Sprintf(`{"bf":2,"todo":[],"at":0,"seen":%d,"big":false}`, old)))
	runSchedule(t, e)
	if _, ok := ent.kv.get("series:a/two"); ok {
		t.Error("series not pruned after 30 days")
	}
	if _, ok := ent.kv.get("meta:a/two"); ok {
		t.Error("meta not pruned after 30 days")
	}
	if _, ok := ent.kv.get("series:a/one"); !ok {
		t.Error("series in use was pruned")
	}
}

func TestStarsCompactionAndWidgetNumbers(t *testing.T) {
	// Shifting the samples back two hours spans an extra UTC date from 00:01
	// through 02:00. Daily compaction therefore keeps 55 old buckets then, not 54.
	for _, tc := range []struct {
		clock   string
		oldDays int
	}{
		{"00:00", 54},
		{"00:01", 55},
		{"01:11", 55}, // time of the original CI failure
		{"02:00", 55},
		{"02:01", 54},
		{"12:00", 54},
		{"23:59", 54},
	} {
		t.Run(tc.clock, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, "2026-11-04T"+tc.clock+":00Z")
			if err != nil {
				t.Fatal(err)
			}
			testStarsCompactionAndWidgetNumbers(t, at, tc.oldDays)
		})
	}
}

func testStarsCompactionAndWidgetNumbers(t *testing.T, at time.Time, wantDays int) {
	t.Helper()
	e := newEnv(t)
	g := newGHStub(t)
	g.set("a/b", 100000)
	installStarsAt(t, e, g, at)
	w := starsWidget(t, e, "a/b", "30d")
	ent, _, _ := e.m.entryFor("github-stars")

	// 60 days of hourly samples growing by 1 star per 2 hours; the last one is a minute old
	now := at.Unix()
	var seed [][2]int64
	for h := 60 * 24; h >= 0; h-- {
		ts := now - 60 - int64(h)*3600
		seed = append(seed, [2]int64{ts - ts%60, 5000 - int64(h)/2})
	}
	if err := ent.kv.set("series:a/b", encSeries(seed)); err != nil {
		t.Fatal(err)
	}
	_ = ent.kv.set("meta:a/b", json.RawMessage(fmt.Sprintf(`{"bf":2,"todo":[],"at":0,"seen":%d,"big":false}`, now)))

	// the widget reads without touching GitHub: the data is fresh
	v := starsView(t, e, w.ID, "en")
	if g.repoReqs["a/b"] != 0 {
		t.Fatalf("fresh data was refetched")
	}
	if st := block(v, "stat"); st["value"] != "5,000" || st["trend"] != "up" {
		t.Errorf("stat %v", st)
	}
	items := block(v, "stats")["items"].([]map[string]any)
	if items[0]["value"] != "+84" || items[1]["value"] != "+360" || items[0]["label"] != "Last 7 days" || items[1]["trend"] != "up" {
		t.Errorf("deltas %v", items)
	}
	pts := chartPts(t, v)
	if first := time.UnixMilli(int64(pts[0][0])); at.Sub(first) < 29*24*time.Hour || at.Sub(first) > 31*24*time.Hour {
		t.Errorf("30d chart starts at %v", first)
	}
	if len(pts) > 500 {
		t.Errorf("%d chart points", len(pts))
	}

	// the schedule samples (the newest point is > 30 min old now) and compacts: hourly for 7 days, daily before
	_ = ent.kv.set("series:a/b", encSeries(func() [][2]int64 {
		out := append([][2]int64(nil), seed...)
		for i := range out { // shift everything 2 hours into the past
			out[i][0] -= 7200
		}
		return out
	}()))
	runSchedule(t, e)
	got := storedSeries(t, e, "a/b")
	if got[len(got)-1][1] != 100000 {
		t.Fatalf("last point %v", got[len(got)-1])
	}
	recent, days := 0, map[int64]int{}
	for _, p := range got {
		if now-p[0] <= 7*86400 {
			recent++
		} else {
			days[p[0]/86400]++
		}
	}
	for d, n := range days {
		if n != 1 {
			t.Errorf("day %d has %d points", d, n)
		}
	}
	if recent != 167 || len(days) != wantDays {
		t.Errorf("recent %d (want 167), old days %d (want %d), total %d", recent, len(days), wantDays, len(got))
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i][0] < got[j][0] }) {
		t.Error("series not sorted")
	}
}

func TestStarsErrorsAndCollecting(t *testing.T) {
	e := newEnv(t)
	g := newGHStub(t)
	g.set("a/one", 3)
	installStars(t, e, g)
	wBad := starsWidget(t, e, "not a repo", "90d")
	wMissing := starsWidget(t, e, "nobody/nothing", "90d")
	wEmpty := starsWidget(t, e, "ab", "90d")
	if v := starsView(t, e, wBad.ID, "en"); v.Error != "Invalid repository “not a repo”: use owner/name." || len(v.Body) != 0 {
		t.Errorf("bad repo: %+v", v)
	}
	if v := starsView(t, e, wBad.ID, "zh"); v.Error != "仓库“not a repo”无效：请使用 owner/name 格式。" {
		t.Errorf("bad repo zh: %q", v.Error)
	}
	if v := starsView(t, e, wMissing.ID, "en"); v.Error != "Repository nobody/nothing was not found." {
		t.Errorf("missing: %q", v.Error)
	}
	if v := starsView(t, e, wMissing.ID, "zh"); v.Error != "找不到仓库 nobody/nothing。" {
		t.Errorf("missing zh: %q", v.Error)
	}
	if v := starsView(t, e, wEmpty.ID, "en"); !strings.Contains(v.Error, "owner/name") {
		t.Errorf("short: %q", v.Error)
	}
	// the schedule ignores invalid repositories and never requests them
	runSchedule(t, e)
	for repo := range g.repoReqs {
		if repo != "a/one" && repo != "nobody/nothing" {
			t.Errorf("unexpected request for %q", repo)
		}
	}

	// fewer than two points: "Collecting data…" (no chart)
	w := starsWidget(t, e, "a/one", "90d")
	ent, _, _ := e.m.entryFor("github-stars")
	now := time.Now().Unix()
	_ = ent.kv.set("series:a/one", encSeries([][2]int64{{now - 30, 3}}))
	_ = ent.kv.set("meta:a/one", json.RawMessage(fmt.Sprintf(`{"bf":2,"todo":[],"at":0,"seen":%d,"big":false}`, now)))
	v := starsView(t, e, w.ID, "en")
	if block(v, "chart") != nil || block(v, "text")["text"] != "Collecting data…" {
		t.Errorf("collecting: %+v", v.Body)
	}
	if vz := starsView(t, e, w.ID, "zh"); block(vz, "text")["text"] != "正在收集数据…" {
		t.Errorf("collecting zh: %+v", vz.Body)
	}
	// a failed refresh keeps showing the stored data and says why
	_ = ent.kv.set("series:a/one", encSeries([][2]int64{{now - 5*86400, 1}, {now - 7200, 3}}))
	g.mu.Lock()
	g.failRepo = 429
	g.mu.Unlock()
	v = starsView(t, e, w.ID, "en")
	if v.Error != "" || block(v, "chart") == nil || !strings.Contains(fmt.Sprint(block(v, "text")["text"]), "Could not refresh") {
		t.Errorf("stale: %+v", v)
	}
}

// 20 repositories at their steady state (a year of daily points on top of a week of hourly ones, and more
// than a year of history) must use well under half of the 256 KiB plugin storage.
func TestStarsTwentyReposFitInStorage(t *testing.T) {
	e := newEnv(t)
	g := newGHStub(t)
	installStars(t, e, g)
	ent, _, _ := e.m.entryFor("github-stars")

	// 2.5 years of hourly samples for one repository (as if compaction never ran) ...
	now := time.Now().Unix()
	var seed [][2]int64
	for h := 24 * 365 * 5 / 2; h >= 0; h-- {
		ts := now - 60 - int64(h)*3600
		seed = append(seed, [2]int64{ts - ts%60, 1000 + int64(24*365*5/2-h)/9})
	}
	raw := encSeries(seed)
	if len(raw) > MaxKVBytes-2000 {
		t.Skipf("seed is %d bytes", len(raw))
	}
	g.set("o/r00", 9000)
	_ = ent.kv.set("series:o/r00", raw)
	_ = ent.kv.set("meta:o/r00", json.RawMessage(fmt.Sprintf(`{"bf":2,"todo":[],"at":0,"seen":%d,"big":false}`, now)))
	starsWidget(t, e, "o/r00", "all")
	// move the newest point out of the 30 minute window so the schedule samples
	seed2 := append([][2]int64(nil), seed...)
	for i := range seed2 {
		seed2[i][0] -= 3600
	}
	_ = ent.kv.set("series:o/r00", encSeries(seed2))
	runSchedule(t, e)
	one, _ := ent.kv.get("series:o/r00")
	pts := decSeries(t, one)
	t.Logf("one repository: %d points, %d bytes", len(pts), len(one))
	if len(pts) > 800 {
		t.Fatalf("compaction kept %d points", len(pts))
	}

	// ... and 19 more of the same size
	for i := 1; i < 20; i++ {
		repo := fmt.Sprintf("o/r%02d", i)
		g.set(repo, 9000+i)
		older := append([][2]int64(nil), pts...)
		for j := range older {
			older[j][0] -= 3600
		}
		if err := ent.kv.set("series:"+repo, encSeries(older)); err != nil {
			t.Fatalf("%s: %v", repo, err)
		}
		_ = ent.kv.set("meta:"+repo, json.RawMessage(fmt.Sprintf(`{"bf":2,"todo":[],"at":0,"seen":%d,"big":false}`, now)))
		starsWidget(t, e, repo, "all")
	}
	// the schedule appends a point to every repository and compacts again, without hitting the cap
	runSchedule(t, e)
	info, err := e.m.Storage("github-stars")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("20 repositories: %d of %d bytes", info.Size, info.Limit)
	if info.Size > info.Limit/2 {
		t.Errorf("20 repositories use %d of %d bytes", info.Size, info.Limit)
	}
	for i := 0; i < 20; i++ {
		repo := fmt.Sprintf("o/r%02d", i)
		s := storedSeries(t, e, repo)
		if s[len(s)-1][1] != int64(9000+i) {
			t.Errorf("%s: last point %v", repo, s[len(s)-1])
		}
	}
}

// GitHub lists stargazers only to signed-in requests: without a token there is no backfill (and no wasted
// request), the plugin samples on its own and says how to get the older history.
func TestStarsWithoutTokenSkipsBackfill(t *testing.T) {
	e := newEnv(t)
	g := newGHStub(t)
	g.set("a/b", 500)
	installStars(t, e, g)
	if _, err := e.m.Patch("github-stars", PatchRequest{Config: map[string]any{"token": nil}}); err != nil {
		t.Fatal(err)
	}
	w := starsWidget(t, e, "a/b", "all")
	v := starsView(t, e, w.ID, "en")
	if v.Error != "" || block(v, "stat")["value"] != "500" || block(v, "chart") != nil {
		t.Fatalf("view %+v", v)
	}
	var notes []string
	for _, b := range v.Body {
		if b["type"] == "text" {
			notes = append(notes, b["text"].(string))
		}
	}
	if len(notes) != 2 || notes[0] != "Collecting data…" || !strings.Contains(notes[1], "GitHub token") {
		t.Fatalf("notes %q", notes)
	}
	if vz := starsView(t, e, w.ID, "zh"); !strings.Contains(fmt.Sprint(vz.Body), "令牌") {
		t.Errorf("zh note: %+v", vz.Body)
	}
	runSchedule(t, e)
	if len(g.accepts) != 0 || g.noAuth != 0 {
		t.Fatalf("stargazers were requested without a token: %d %d", len(g.accepts), g.noAuth)
	}
	for _, a := range g.auths {
		if a != "" {
			t.Fatalf("authorization %q without a token", a)
		}
	}
	// a second sample (the first one is made older) gives a chart
	ent, _, _ := e.m.entryFor("github-stars")
	pts := storedSeries(t, e, "a/b")
	pts[0][0] -= 7200
	_ = ent.kv.set("series:a/b", encSeries(pts))
	runSchedule(t, e)
	v = starsView(t, e, w.ID, "en")
	if got := chartPts(t, v); len(got) != 2 || got[1][1] != 500 {
		t.Fatalf("points %v", got)
	}
	if meta := storedMeta(t, e, "a/b"); meta["bf"] != 0.0 {
		t.Errorf("meta %v", meta)
	}
	// adding the token later starts the backfill
	if _, err := e.m.Patch("github-stars", PatchRequest{Config: map[string]any{"token": "tok-2"}}); err != nil {
		t.Fatal(err)
	}
	runSchedule(t, e)
	if pages := g.pagesOf("a/b"); len(pages) != 5 || storedMeta(t, e, "a/b")["bf"] != 2.0 {
		t.Fatalf("pages %v meta %v", pages, storedMeta(t, e, "a/b"))
	}
}
