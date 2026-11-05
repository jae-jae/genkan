package plugin

import (
	"context"
	"io/fs"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/jae-jae/genkan/internal/store"
	"github.com/jae-jae/genkan/plugins"
)

// Every built-in example must compile and have a valid manifest.
func TestCatalogCompilesAndValidates(t *testing.T) {
	e := newEnv(t)
	cat := plugins.Catalog()
	ents, err := fs.ReadDir(cat, ".")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, de := range ents {
		if de.IsDir() {
			continue
		}
		if path.Ext(de.Name()) != ".js" {
			t.Errorf("%s: catalog files must be .js", de.Name())
			continue
		}
		b, _ := fs.ReadFile(cat, de.Name())
		id, err := e.m.Validate(string(b), "js")
		if err != nil {
			t.Errorf("%s: %v", de.Name(), err)
			continue
		}
		if id+".js" != de.Name() {
			t.Errorf("%s: manifest id is %q", de.Name(), id)
		}
		n++
	}
	want := []string{"bing-wallpaper", "nasa-apod", "picsum", "weather", "hacker-news", "rss", "world-clock", "countdown", "calculator", "github-search", "pihole", "uptime-summary", "link-importer", "media", "github-stars", "todo", "note"}
	m := New(Config{Store: e.st, DataDir: e.dir, Catalog: cat})
	defer m.Close()
	have := map[string]bool{}
	for _, c := range m.Catalog() {
		have[c.ID] = true
	}
	for _, id := range want {
		if !have[id] {
			t.Errorf("catalog is missing %s", id)
		}
	}
	if n != len(want) {
		t.Errorf("expected the %d documented example plugins, found %d valid files", len(want), n)
	}
	if n != len(m.Catalog()) {
		t.Errorf("%d valid files but %d catalog items", n, len(m.Catalog()))
	}
}

func catalogEnv(t *testing.T) *tenv {
	e := newEnv(t)
	e.m.cfg.Catalog = plugins.Catalog()
	return e
}

func installCatalog(t *testing.T, e *tenv, id string) {
	t.Helper()
	if _, err := e.m.Install(context.Background(), InstallRequest{Catalog: id}); err != nil {
		t.Skipf("catalog plugin %s not available: %v", id, err)
	}
}

func TestCatalogCalculator(t *testing.T) {
	e := catalogEnv(t)
	installCatalog(t, e, "calculator")
	if g := e.m.Search(context.Background(), "1+2*3", "en"); len(g) != 1 || g[0].Results[0].Title != "= 7" {
		t.Errorf("1+2*3: %+v", g)
	}
	for q, want := range map[string]string{"1+2*3": "7", "(1+2)*3": "9", "2^10": "1024", "10/4": "2.5", "50%": "0.5", "0.1+0.2": "0.3", "sqrt(16)+1": "5"} {
		groups := e.m.Search(context.Background(), q, "en")
		if len(groups) != 1 || len(groups[0].Results) != 1 {
			t.Errorf("%q: groups %+v", q, groups)
			continue
		}
		if got := groups[0].Results[0]; got.Copy != want {
			t.Errorf("%q: copy %q, want %q (%+v)", q, got.Copy, want, got)
		}
	}
	for _, q := range []string{"hello", "42", "1/0", "2026-11-03"} {
		if g := e.m.Search(context.Background(), q, "en"); len(g) != 0 {
			t.Errorf("%q should have no results: %+v", q, g)
		}
	}
}

func addWidget(t *testing.T, e *tenv, plugin, widget string, cfg map[string]any) WidgetOut {
	t.Helper()
	w, err := e.m.AddWidget(WidgetAdd{Plugin: plugin, Widget: widget, Config: cfg})
	if err != nil {
		t.Fatalf("add widget: %v", err)
	}
	return w
}

func blockTypes(v WidgetView) string {
	var ts []string
	for _, b := range v.Body {
		ts = append(ts, b["type"].(string))
	}
	return strings.Join(ts, ",")
}

func TestCatalogWorldClock(t *testing.T) {
	e := catalogEnv(t)
	installCatalog(t, e, "world-clock")
	inf, _ := e.m.Info("world-clock", true)
	w := addWidget(t, e, "world-clock", inf.Widgets[0].ID, nil)
	d, err := e.m.Data(context.Background(), w.ID, "en", false)
	if err != nil {
		t.Fatal(err)
	}
	if d.View.Error != "" || len(d.View.Body) == 0 {
		t.Fatalf("view: %+v", d.View)
	}
	// default zones: the Tokyo row shows the current time in Tokyo
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skip("no tz database on this host")
	}
	items, _ := d.View.Body[0]["items"].([]map[string]any)
	found := false
	for _, it := range items {
		if it["title"] == "Tokyo" {
			found = true
			want := time.Now().In(jst)
			ok := it["right"] == want.Format("15:04") || it["right"] == want.Add(-time.Minute).Format("15:04")
			if !ok {
				t.Errorf("Tokyo shows %v, now is %s", it["right"], want.Format("15:04"))
			}
		}
	}
	if !found {
		t.Errorf("default zones lack Tokyo: %+v", items)
	}
	if d.NextAt <= d.UpdatedAt {
		t.Fatalf("times: %+v", d)
	}
	// configured time zones render and differ from each other
	res, err := e.m.Test(context.Background(), "world-clock", TestRequest{Hook: "widget", Config: map[string]any{"zones": "Asia/Tokyo\nAmerica/New_York"}})
	if err != nil || !res.OK {
		t.Fatalf("test: %+v %v", res, err)
	}
	b, _ := jsonString(res.Result)
	if !strings.Contains(b, "Tokyo") || !strings.Contains(b, "New") {
		t.Fatalf("zones missing from %s", b)
	}
}

func TestCatalogCountdown(t *testing.T) {
	e := catalogEnv(t)
	installCatalog(t, e, "countdown")
	inf, _ := e.m.Info("countdown", true)
	wid := inf.Widgets[0].ID
	// default config: counts down to the next New Year
	def, err := e.m.Test(context.Background(), "countdown", TestRequest{Hook: "widget", Target: wid})
	if err != nil || !def.OK || len(def.Result.(WidgetView).Body) == 0 {
		t.Fatalf("default config: %+v %v", def, err)
	}
	var dateKey string
	for _, f := range inf.Widgets[0].Config {
		if f.Type == "string" || f.Type == "text" {
			if strings.Contains(strings.ToLower(f.Key), "date") || strings.Contains(strings.ToLower(f.Key), "target") {
				dateKey = f.Key
			}
		}
	}
	if dateKey == "" {
		t.Skipf("cannot find the date field in %+v", inf.Widgets[0].Config)
	}
	res, err := e.m.Test(context.Background(), "countdown", TestRequest{Hook: "widget", Target: wid, Config: map[string]any{dateKey: "2999-01-01"}})
	if err != nil || !res.OK {
		t.Fatalf("test: %+v %v", res, err)
	}
	// 10 days and 3 hours from now (UTC): the first stat block says 10 days
	target := time.Now().UTC().Add(10*24*time.Hour + 3*time.Hour).Format("2006-01-02T15:04")
	res, err = e.m.Test(context.Background(), "countdown", TestRequest{Hook: "widget", Target: wid, Config: map[string]any{dateKey: target}})
	if err != nil || !res.OK {
		t.Fatalf("test: %+v %v", res, err)
	}
	v := res.Result.(WidgetView)
	if v.Body[0]["type"] != "stat" || v.Body[0]["value"] != 10.0 {
		t.Fatalf("expected 10 days: %+v", v.Body)
	}
}

func TestCatalogAllInstallable(t *testing.T) {
	e := catalogEnv(t)
	for _, c := range e.m.Catalog() {
		if _, err := e.m.Install(context.Background(), InstallRequest{Catalog: c.ID}); err != nil {
			t.Errorf("%s: %v", c.ID, err)
		}
	}
	for _, c := range e.m.Catalog() {
		if !c.Installed {
			t.Errorf("%s should be installed", c.ID)
		}
	}
	_ = store.ValidPluginID
}
