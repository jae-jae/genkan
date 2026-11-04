package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const src = "export const manifest = {}"

func savePlugin(t *testing.T, s *Store, id string) Plugin {
	t.Helper()
	p, _, err := s.SavePlugin(PluginSave{ID: id, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPluginIDs(t *testing.T) {
	for _, id := range []string{"weather", "a1", "hacker-news", "0abc", strings.Repeat("a", 40)} {
		if !ValidPluginID(id) {
			t.Errorf("%q should be valid", id)
		}
	}
	for _, id := range []string{"", "a", "A1", "-ab", "a_b", "../x", "a/b", "a.b", "a b", strings.Repeat("a", 41), "é1"} {
		if ValidPluginID(id) {
			t.Errorf("%q should be invalid", id)
		}
	}
	s, _ := open(t)
	for _, id := range []string{"../evil", "a/b", ""} {
		if _, _, err := s.SavePlugin(PluginSave{ID: id, Source: src}); ErrKind(err) != KindInvalid {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestSavePluginLifecycle(t *testing.T) {
	s, dir := open(t)
	var versions []int64
	s.OnChange(func(v int64) { versions = append(versions, v) })
	p, created, err := s.SavePlugin(PluginSave{ID: "demo", Source: src, SourceURL: "https://x/y.js"})
	if err != nil || !created || !p.Enabled || p.Lang != "js" || p.InstalledAt == "" || p.Rev == 0 || p.SourceURL != "https://x/y.js" {
		t.Fatalf("%+v %v %v", p, created, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "plugins", "demo.js")); err != nil || string(b) != src {
		t.Fatalf("source file: %q %v", b, err)
	}
	if got, lang, err := s.PluginSource("demo"); err != nil || got != src || lang != "js" {
		t.Fatalf("PluginSource: %q %q %v", got, lang, err)
	}
	// the source is not part of genkan.json
	raw, _ := os.ReadFile(filepath.Join(dir, "genkan.json"))
	if strings.Contains(string(raw), "manifest") || !strings.Contains(string(raw), `"schema": 2`) {
		t.Fatalf("genkan.json: %s", raw)
	}
	if _, err := s.SetPluginConfig("demo", map[string]any{"a": "b"}); err != nil {
		t.Fatal(err)
	}
	// update keeps config + enabled, changes rev; switching to ts removes the js file
	f := false
	s.SetPluginEnabled("demo", false)
	p2, created, err := s.SavePlugin(PluginSave{ID: "demo", Source: "// ts\n" + src, Lang: "ts"})
	if err != nil || created || p2.Enabled || p2.Config["a"] != "b" || p2.Rev == p.Rev || p2.InstalledAt != p.InstalledAt || p2.Lang != "ts" {
		t.Fatalf("update: %+v %v", p2, err)
	}
	_ = f
	if _, err := os.Stat(filepath.Join(dir, "plugins", "demo.js")); !os.IsNotExist(err) {
		t.Error("old js source remains after switching to ts")
	}
	if _, lang, _ := s.PluginSource("demo"); lang != "ts" {
		t.Errorf("lang %s", lang)
	}
	if len(versions) != 4 {
		t.Errorf("every change must notify, got %v", versions)
	}
	// persisted and reloaded with fresh revs
	s.Close()
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, ok := s2.GetPlugin("demo")
	if !ok || got.Config["a"] != "b" || got.Lang != "ts" || got.Rev == 0 || got.Enabled {
		t.Fatalf("reload: %+v", got)
	}
	// validation
	for name, in := range map[string]PluginSave{
		"empty":   {ID: "x1", Source: "  "},
		"huge":    {ID: "x1", Source: strings.Repeat("a", MaxPluginSource+1)},
		"badlang": {ID: "x1", Source: src, Lang: "py"},
		"utf8":    {ID: "x1", Source: "\xff\xfe"},
	} {
		if _, _, err := s2.SavePlugin(in); ErrKind(err) != KindInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	// config validation
	if _, err := s2.SetPluginConfig("demo", map[string]any{"big": strings.Repeat("x", maxConfigJSON)}); ErrKind(err) != KindInvalid {
		t.Errorf("large config: %v", err)
	}
	if _, err := s2.SetPluginConfig("nope", nil); ErrKind(err) != KindNotFound {
		t.Errorf("missing: %v", err)
	}
	// plugin limit
	for i := 0; i < maxPlugins; i++ {
		_, _, err = s2.SavePlugin(PluginSave{ID: "p" + strings.Repeat("x", 1) + string(rune('a'+i/26)) + string(rune('a'+i%26)), Source: src})
		if err != nil {
			break
		}
	}
	if ErrKind(err) != KindConflict {
		t.Errorf("plugin limit: %v", err)
	}
}

func TestSchemaV1Migration(t *testing.T) {
	dir := t.TempDir()
	v1 := `{"schema":1,"auth":{"password_hash":"","session_secret":"` + strings.Repeat("a", 64) + `","api_token":"gk_x"},"settings":{},"workspaces":[{"id":"w1","name":"Home","icon":"lucide:house","wallpaper":"preset:dusk","sort":0}],"categories":[],"links":[]}`
	os.WriteFile(filepath.Join(dir, "genkan.json"), []byte(v1), 0o600)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Plugins()) != 0 || len(s.Widgets()) != 0 {
		t.Fatal("v1 file has no plugins")
	}
	s.Close()
	var out struct{ Schema int }
	raw, _ := os.ReadFile(filepath.Join(dir, "genkan.json"))
	json.Unmarshal(raw, &out)
	if out.Schema != SchemaVersion || SchemaVersion != 2 {
		t.Fatalf("schema %d", out.Schema)
	}
	// a file from the future is refused
	os.WriteFile(filepath.Join(dir, "genkan.json"), []byte(`{"schema":99}`), 0o600)
	if _, err := Open(dir); err == nil {
		t.Fatal("newer schema must be refused")
	}
}

func TestWidgetCRUD(t *testing.T) {
	s, _ := open(t)
	ws := firstWS(t, s)
	savePlugin(t, s, "plug")
	if _, err := s.CreateWidget(WidgetInput{Plugin: "ghost", Widget: "w"}); ErrKind(err) != KindNotFound {
		t.Errorf("unknown plugin: %v", err)
	}
	if _, err := s.CreateWidget(WidgetInput{Plugin: "plug", Widget: "W!"}); ErrKind(err) != KindInvalid {
		t.Errorf("bad widget id: %v", err)
	}
	if _, err := s.CreateWidget(WidgetInput{Plugin: "plug", Widget: "w", WorkspaceID: "nope"}); ErrKind(err) != KindNotFound {
		t.Errorf("unknown workspace: %v", err)
	}
	a, err := s.CreateWidget(WidgetInput{Plugin: "plug", Widget: "w", WorkspaceID: ws.ID, Config: map[string]any{"city": "Oslo"}})
	if err != nil || a.ID == "" || a.Sort != 0 || a.Config["city"] != "Oslo" {
		t.Fatalf("%+v %v", a, err)
	}
	b, _ := s.CreateWidget(WidgetInput{Plugin: "plug", Widget: "w"})
	c, _ := s.CreateWidget(WidgetInput{Plugin: "plug", Widget: "w"})
	if b.Sort != 1 || c.Sort != 2 || b.WorkspaceID != "" || b.Config == nil {
		t.Fatalf("sort/defaults: %+v %+v", b, c)
	}
	if err := s.ReorderWidgets([]string{c.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, w := range s.Widgets() {
		order = append(order, w.ID)
	}
	if strings.Join(order, ",") != strings.Join([]string{c.ID, a.ID, b.ID}, ",") {
		t.Fatalf("order %v", order)
	}
	if err := s.ReorderWidgets([]string{"nope"}); ErrKind(err) != KindNotFound {
		t.Errorf("reorder unknown: %v", err)
	}
	cfg := map[string]any{"city": "Rome"}
	n := 9
	empty := ""
	got, err := s.UpdateWidget(a.ID, WidgetPatch{Config: &cfg, Sort: &n, WorkspaceID: &empty})
	if err != nil || got.Config["city"] != "Rome" || got.Sort != 9 || got.WorkspaceID != "" {
		t.Fatalf("%+v %v", got, err)
	}
	bad := "nope"
	if _, err := s.UpdateWidget(a.ID, WidgetPatch{WorkspaceID: &bad}); ErrKind(err) != KindNotFound {
		t.Errorf("update ws: %v", err)
	}
	// returned configs are copies
	got.Config["city"] = "changed"
	if w, _ := s.GetWidget(a.ID); w.Config["city"] != "Rome" {
		t.Error("config aliasing")
	}
	if err := s.DeleteWidget(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWidget(a.ID); ErrKind(err) != KindNotFound {
		t.Errorf("double delete: %v", err)
	}
	// snapshot carries widgets
	if len(s.Snapshot().Widgets) != 2 {
		t.Errorf("snapshot widgets %d", len(s.Snapshot().Widgets))
	}
	// deleting the plugin removes its widgets
	if err := s.DeletePlugin("plug"); err != nil {
		t.Fatal(err)
	}
	if len(s.Widgets()) != 0 {
		t.Error("widgets of a deleted plugin remain")
	}
}

func TestPluginWallpaperValue(t *testing.T) {
	s, _ := open(t)
	savePlugin(t, s, "wp")
	for _, ok := range []string{"plugin:wp/daily", "plugin:wp/a_b-1"} {
		if _, err := s.CreateWorkspace(WorkspaceInput{Name: "x", Wallpaper: ok}); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"plugin:wp", "plugin:wp/", "plugin:/x", "plugin:wp/../x", "plugin:WP/x", "plugin:wp/x/y", "plugin:other/x"} {
		if _, err := s.CreateWorkspace(WorkspaceInput{Name: "x", Wallpaper: bad}); ErrKind(err) != KindInvalid {
			t.Errorf("%s: %v", bad, err)
		}
	}
	// deleting the plugin resets every workspace that used it
	if err := s.DeletePlugin("wp"); err != nil {
		t.Fatal(err)
	}
	for _, w := range s.Snapshot().Workspaces {
		if strings.HasPrefix(w.Wallpaper, "plugin:") {
			t.Errorf("workspace %s still uses %s", w.Name, w.Wallpaper)
		}
	}
}

func TestExportWithoutResolverDropsConfig(t *testing.T) {
	s, _ := open(t)
	savePlugin(t, s, "keepme")
	s.SetPluginConfig("keepme", map[string]any{"token": "SECRET"})
	s.CreateWidget(WidgetInput{Plugin: "keepme", Widget: "w", Config: map[string]any{"pw": "SECRET2"}})
	b, _ := json.Marshal(s.Export())
	if strings.Contains(string(b), "SECRET") {
		t.Fatalf("export without resolver leaked config: %s", b)
	}
	s.SetSecretResolver(func(id string) ([]string, map[string][]string, bool) {
		return []string{"token"}, map[string][]string{"w": {"pw"}}, true
	})
	s.SetPluginConfig("keepme", map[string]any{"token": "SECRET", "city": "Oslo"})
	s.CreateWidget(WidgetInput{Plugin: "keepme", Widget: "w", Config: map[string]any{"pw": "SECRET2", "label": "L"}})
	e := s.Export()
	b, _ = json.Marshal(e)
	if strings.Contains(string(b), "SECRET") || !strings.Contains(string(b), "Oslo") || !strings.Contains(string(b), `"label":"L"`) {
		t.Fatalf("export with resolver: %s", b)
	}
	// export of a v1 store (no plugins) keeps the old key set
	s2, _ := open(t)
	b2, _ := json.Marshal(s2.Export())
	var keys map[string]any
	json.Unmarshal(b2, &keys)
	if _, has := keys["plugins"]; has {
		t.Errorf("empty plugin list should be omitted: %s", b2)
	}
}

func TestImportPluginValidation(t *testing.T) {
	s, _ := open(t)
	good := Export{Schema: SchemaVersion, Workspaces: []Workspace{{ID: "a", Name: "A"}}}
	for name, plugins := range map[string][]ExportPlugin{
		"bad id":    {{Plugin: Plugin{ID: "../x"}, Source: src}},
		"dup":       {{Plugin: Plugin{ID: "aa"}, Source: src}, {Plugin: Plugin{ID: "aa"}, Source: src}},
		"no source": {{Plugin: Plugin{ID: "aa"}}},
		"bad lang":  {{Plugin: Plugin{ID: "aa", Lang: "rb"}, Source: src}},
	} {
		in := good
		in.Plugins = plugins
		if _, err := s.ImportData("replace", in); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if len(s.Plugins()) != 0 {
		t.Fatal("rejected import must not store anything")
	}
	in := good
	in.Plugins = []ExportPlugin{{Plugin: Plugin{ID: "aa", Enabled: true, Config: map[string]any{"k": 1.0}}, Source: src}}
	in.Widgets = []WidgetInstance{{ID: "wid1", Plugin: "aa", Widget: "w", WorkspaceID: "a"}, {Plugin: "zz", Widget: "w"}, {Plugin: "aa", Widget: "w", WorkspaceID: "unknown"}}
	res, err := s.ImportData("replace", in)
	if err != nil || res.Plugins != 1 || res.Widgets != 1 || res.Workspaces != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if w := s.Widgets()[0]; w.ID != "wid1" || w.WorkspaceID != "a" {
		t.Errorf("widget: %+v", w)
	}
	if p, _ := s.GetPlugin("aa"); !p.Enabled || p.Config["k"] != 1.0 || p.InstalledAt == "" {
		t.Errorf("plugin: %+v", p)
	}
}

func TestWidgetLayoutClamp(t *testing.T) {
	for _, tc := range []struct{ in, w, h int }{{-1, 0, 0}, {0, 0, 0}, {6, 6, 80}, {7, 6, 1200}, {3, 3, 5000}} {
		if got := ClampWidgetW(tc.in); got != tc.w {
			t.Errorf("w(%d) = %d", tc.in, got)
		}
	}
	for in, want := range map[int]int{-9: 0, 0: 0, 1: 80, 79: 80, 80: 80, 640: 640, 1200: 1200, 1201: 1200} {
		if got := ClampWidgetH(in); got != want {
			t.Errorf("h(%d) = %d, want %d", in, got, want)
		}
	}
}
