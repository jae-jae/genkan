package plugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jae-jae/genkan/internal/store"
)

func TestSanitizeView(t *testing.T) {
	long := strings.Repeat("é", 800)
	var many []any
	for i := 0; i < 70; i++ {
		many = append(many, map[string]any{"type": "text", "text": fmt.Sprint(i)})
	}
	raw := map[string]any{
		"title": long, "icon": "cloud", "link": "javascript:alert(1)", "evil": "x",
		"body": append([]any{
			map[string]any{"type": "script", "src": "x"},
			map[string]any{"type": "text", "text": "hi\x00\x07 there", "size": "huge", "align": "center", "muted": true, "onclick": "x"},
			map[string]any{"type": "stat", "label": "L", "value": 3.5, "unit": "ms", "trend": "sideways", "icon": "Bad Icon!"},
			map[string]any{"type": "stat", "value": 1},
			map[string]any{"type": "stats", "items": []any{map[string]any{"label": "a", "value": "1"}, "junk", map[string]any{"label": "b", "value": 2}, map[string]any{"label": "c", "value": 3}, map[string]any{"label": "d", "value": 4}, map[string]any{"label": "e", "value": 5}, map[string]any{"label": "f", "value": 6}, map[string]any{"label": "g", "value": 7}}},
			map[string]any{"type": "list", "items": []any{
				map[string]any{"title": "ok", "url": "https://a.b/c", "image": "data:image/png;base64,AAAA", "icon": "star", "badge": "new", "right": "r", "subtitle": "s"},
				map[string]any{"title": "bad urls", "url": "javascript:x", "image": "data:text/html;base64,AAAA"},
				map[string]any{"subtitle": "no title"},
			}},
			map[string]any{"type": "kv", "items": []any{map[string]any{"k": "a", "v": 5.0}, map[string]any{"k": "", "v": "x"}}},
			map[string]any{"type": "progress", "value": 7.0, "label": "p"},
			map[string]any{"type": "progress", "value": "x"},
			map[string]any{"type": "image", "src": "javascript:1"},
			map[string]any{"type": "image", "src": "http://x/i.png", "href": "ftp://x", "ratio": 99.0},
			map[string]any{"type": "chart", "kind": "spark", "values": []any{1.0, "x", 2.0, nil, 3.0}, "labels": []any{"a", "b"}},
			map[string]any{"type": "chart", "kind": "pie", "values": []any{1.0}},
			map[string]any{"type": "row", "children": []any{map[string]any{"type": "text", "text": "in row"}, map[string]any{"type": "button", "label": "Go", "action": "do it!"}, map[string]any{"type": "button", "label": "Go", "action": "do", "params": map[string]any{"a": 1.0}, "icon": "play"}}},
			map[string]any{"type": "divider"},
		}, many...),
	}
	v := SanitizeView(raw)
	if utf8Len(v.Title) != 500 || v.Icon != "cloud" || v.Link != "" {
		t.Errorf("header: len=%d icon=%q link=%q", utf8Len(v.Title), v.Icon, v.Link)
	}
	total := 0
	for _, blk := range v.Body {
		total++
		if ch, ok := blk["children"].([]map[string]any); ok {
			total += len(ch)
		}
	}
	if total != 50 {
		t.Errorf("body limited to 50 blocks including nested ones, got %d", total)
	}
	b := func(i int) map[string]any { return v.Body[i] }
	// body[0] = text (script dropped)
	if b(0)["text"] != "hi there" || b(0)["size"] != nil || b(0)["align"] != "center" || b(0)["muted"] != true || b(0)["onclick"] != nil {
		t.Errorf("text: %+v", b(0))
	}
	if b(1)["value"] != 3.5 || b(1)["trend"] != nil || b(1)["icon"] != nil || b(1)["unit"] != "ms" {
		t.Errorf("stat: %+v", b(1))
	}
	if items := b(2)["items"].([]map[string]any); len(items) != 6 {
		t.Errorf("stats max 6, got %d", len(items))
	}
	li := b(3)["items"].([]map[string]any)
	if len(li) != 2 || li[0]["url"] != "https://a.b/c" || li[0]["image"] != "data:image/png;base64,AAAA" || li[1]["url"] != nil || li[1]["image"] != nil {
		t.Errorf("list: %+v", li)
	}
	if kv := b(4)["items"].([]map[string]any); len(kv) != 1 || kv[0]["v"] != "5" {
		t.Errorf("kv: %+v", kv)
	}
	if b(5)["value"] != 1.0 {
		t.Errorf("progress clamp: %+v", b(5))
	}
	img := b(6)
	if img["src"] != "http://x/i.png" || img["href"] != nil || img["ratio"] != 10.0 {
		t.Errorf("image: %+v", img)
	}
	ch := b(7)
	if vals := ch["values"].([]float64); len(vals) != 3 || ch["kind"] != "spark" {
		t.Errorf("chart: %+v", ch)
	}
	row := b(8)["children"].([]map[string]any)
	if len(row) != 2 || row[1]["action"] != "do" || row[1]["icon"] != "play" || row[1]["params"] == nil {
		t.Errorf("row: %+v", row)
	}
	if b(9)["type"] != "divider" {
		t.Errorf("divider: %+v", b(9))
	}
	// every URL-looking value in the output is http(s) or data:image
	out, _ := json.Marshal(v)
	if strings.Contains(string(out), "javascript") || strings.Contains(string(out), "text/html") {
		t.Errorf("unsafe value survived: %s", out)
	}
	if e := SanitizeView("nope"); e.Error == "" || e.Body == nil {
		t.Errorf("non-object: %+v", e)
	}
	if e := SanitizeView(map[string]any{"error": "plugin says no"}); e.Error != "plugin says no" {
		t.Errorf("plugin error: %+v", e)
	}
	// deeply nested rows are cut off
	deep := map[string]any{"type": "text", "text": "deep"}
	for i := 0; i < 6; i++ {
		deep = map[string]any{"type": "row", "children": []any{deep}}
	}
	if dv := SanitizeView(map[string]any{"body": []any{deep}}); len(dv.Body) != 0 {
		t.Errorf("deep nesting should vanish: %+v", dv.Body)
	}
	// big params are dropped
	big := map[string]any{"type": "button", "label": "B", "action": "a", "params": strings.Repeat("x", 5000)}
	if bv := SanitizeView(map[string]any{"body": []any{big}}); bv.Body[0]["params"] != nil {
		t.Error("oversized params kept")
	}
}

func utf8Len(s string) int { return len([]rune(s)) }

func TestSanitizeSearchAndWallpaper(t *testing.T) {
	var in []any
	for i := 0; i < 12; i++ {
		in = append(in, map[string]any{"title": fmt.Sprint("t", i), "url": "javascript:x", "copy": "c", "icon": "calculator"})
	}
	in = append(in, "junk", map[string]any{"subtitle": "no title"})
	r := SanitizeResults(in)
	if len(r) != 8 || r[0].URL != "" || r[0].Copy != "c" || r[0].Icon != "calculator" {
		t.Errorf("results: %+v", r)
	}
	if got := SanitizeResults("x"); got == nil || len(got) != 0 {
		t.Errorf("non-array: %#v", got)
	}
	pick := func(n int) int { return n - 1 }
	w, ok := SanitizeWallpaper([]any{map[string]any{"url": "ftp://x"}, map[string]any{"url": "https://a/b.jpg", "title": "T", "link": "https://l"}, map[string]any{"url": "http://c/d.png"}}, pick)
	if !ok || w.URL != "http://c/d.png" {
		t.Errorf("array pick: %+v", w)
	}
	if _, ok := SanitizeWallpaper(map[string]any{"url": "file:///etc/passwd"}, pick); ok {
		t.Error("file url accepted")
	}
	if w, ok := SanitizeWallpaper(map[string]any{"url": "data:image/png;base64,AAAA", "credit": "C", "link": "javascript:1"}, pick); !ok || w.Credit != "C" || w.Link != "" {
		t.Errorf("data url: %+v", w)
	}
	if a := SanitizeAction(map[string]any{"toast": "hi", "refresh": true}); a.Toast != "hi" || !a.Refresh {
		t.Errorf("action: %+v", a)
	}
}

func TestConfigSecretsAndMasking(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{
  id:"sec", name:"Sec", version:"1",
  config:[{key:"token",type:"secret",label:"Token"},{key:"mode",type:"select",label:"Mode",options:[{value:"a",label:"A"},{value:"b",label:"B"}],default:"a"},
          {key:"n",type:"number",label:"N",min:0,max:10},{key:"on",type:"boolean",label:"On"},{key:"home",type:"url",label:"Home",required:true}],
  widgets:[{id:"w",name:"W",config:[{key:"key",type:"secret",label:"Key"},{key:"label",type:"string",label:"Label"}]}]
}`, `
export async function widget(ctx) {
  genkan.log("token is " + ctx.config.token + " and key " + ctx.instance.config.key);
  console.log({t: ctx.config.token});
  throw new Error("failed with " + ctx.config.token);
}`))
	// required, validation
	if _, err := e.m.Patch("sec", PatchRequest{Config: map[string]any{"token": "sekret-token"}}); err == nil || !strings.Contains(err.Error(), "Home is required") {
		t.Fatalf("required: %v", err)
	}
	for _, bad := range []map[string]any{
		{"home": "ftp://x"}, {"home": "http://ok", "mode": "zzz"}, {"home": "http://ok", "n": 11.0}, {"home": "http://ok", "n": "x"}, {"home": "http://ok", "on": "yes"},
	} {
		if _, err := e.m.Patch("sec", PatchRequest{Config: bad}); store.ErrKind(err) != store.KindInvalid {
			t.Errorf("%v: %v", bad, err)
		}
	}
	inf, err := e.m.Patch("sec", PatchRequest{Config: map[string]any{"token": "sekret-token", "home": "http://h.lan", "n": 3.0, "unknown": 1.0}})
	if err != nil {
		t.Fatal(err)
	}
	if inf.Config["token"] != "" || inf.Config["home"] != "http://h.lan" || inf.Config["unknown"] != nil || len(inf.SecretsSet) != 1 || inf.SecretsSet[0] != "token" {
		t.Fatalf("admin view: %+v", inf.Admin)
	}
	if s, _ := jsonString(inf); strings.Contains(s, "sekret-token") {
		t.Fatalf("secret leaked in info: %s", s)
	}
	pub, _ := e.m.Info("sec", false)
	var pubMap map[string]any
	pb, _ := json.Marshal(pub)
	json.Unmarshal(pb, &pubMap)
	for _, k := range []string{"config", "secrets_set", "source_url", "last_error"} {
		if _, has := pubMap[k]; has {
			t.Fatalf("guest view has admin field %s: %s", k, pb)
		}
	}
	if pub.Admin != nil || strings.Contains(string(pb), "sekret") || strings.Contains(string(pb), "h.lan") {
		t.Fatalf("guest view leaks: %s", pb)
	}
	// "" keeps, other keys replaced (n omitted => cleared), null clears
	inf, _ = e.m.Patch("sec", PatchRequest{Config: map[string]any{"token": "", "home": "http://h2.lan"}})
	rec, _ := e.st.GetPlugin("sec")
	if rec.Config["token"] != "sekret-token" || rec.Config["n"] != nil || rec.Config["home"] != "http://h2.lan" || inf.SecretsSet[0] != "token" {
		t.Fatalf("keep: %+v", rec.Config)
	}
	// merge mode (MCP): untouched keys stay
	e.m.Patch("sec", PatchRequest{Merge: true, Config: map[string]any{"n": 4.0}})
	rec, _ = e.st.GetPlugin("sec")
	if rec.Config["n"] != 4.0 || rec.Config["home"] != "http://h2.lan" || rec.Config["token"] != "sekret-token" {
		t.Fatalf("merge: %+v", rec.Config)
	}
	w := addWidget(t, e, "sec", "w", map[string]any{"key": "inst-key-123", "label": "L"})
	if w.Config["key"] != "" || len(w.SecretsSet) != 1 {
		t.Fatalf("widget admin view: %+v", w)
	}
	if g := e.m.Present([]store.WidgetInstance{{ID: w.ID, Plugin: "sec", Widget: "w", Config: map[string]any{"key": "inst-key-123", "label": "L"}}}, false)[0]; g.Config["key"] != nil || g.Config["label"] != "L" || g.SecretsSet != nil {
		t.Fatalf("guest widget view: %+v", g)
	}
	// the plugin sees the plaintext, but logs / errors / test output do not
	res, _ := e.m.Test(context.Background(), "sec", TestRequest{Hook: "widget", Instance: w.ID})
	all, _ := jsonString(res)
	for _, s := range []string{"sekret-token", "inst-key-123"} {
		if strings.Contains(all, s) {
			t.Errorf("%q leaked in test response: %s", s, all)
		}
	}
	if !strings.Contains(all, "***") || res.OK {
		t.Errorf("expected redacted failure: %s", all)
	}
	logs, _ := e.m.Logs("sec")
	if s, _ := jsonString(logs); strings.Contains(s, "sekret-token") || strings.Contains(s, "inst-key-123") {
		t.Errorf("secret in logs: %s", s)
	}
	if inf, _ := e.m.Info("sec", true); strings.Contains(inf.LastError, "sekret") {
		t.Errorf("secret in last_error: %s", inf.LastError)
	}
	// but ctx.config does carry the real value
	e.install(plug(`{id:"sec2", name:"S2", version:"1", config:[{key:"token",type:"secret",label:"T",default:"DEMO"}], search:{name:"S"}}`, `export async function search(ctx){ return [{title: ctx.config.token}] }`))
	if g := e.m.Search(context.Background(), "aa", "en"); len(g) != 1 || g[0].Results[0].Title != "DEMO" {
		t.Fatalf("default: %+v", g)
	}
	e.m.Patch("sec2", PatchRequest{Config: map[string]any{"token": "real-one"}})
	if g := e.m.Search(context.Background(), "aa", "en"); g[0].Results[0].Title != "real-one" {
		t.Fatalf("plaintext in ctx: %+v", g)
	}
	e.m.Patch("sec2", PatchRequest{Config: map[string]any{"token": nil}})
	if g := e.m.Search(context.Background(), "aa", "en"); g[0].Results[0].Title != "DEMO" {
		t.Fatalf("cleared secret falls back to default: %+v", g)
	}
}

func TestExportImportSecrets(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"exp", name:"E", version:"1", config:[{key:"token",type:"secret",label:"T"},{key:"city",type:"string",label:"C"}],
      widgets:[{id:"w",name:"W",config:[{key:"pw",type:"secret",label:"P"},{key:"label",type:"string",label:"L"}]}]}`, `export async function widget(){ return {body:[]} }`))
	e.m.Patch("exp", PatchRequest{Config: map[string]any{"token": "TOPSECRET", "city": "Oslo"}})
	w := addWidget(t, e, "exp", "w", map[string]any{"pw": "WIDGETSECRET", "label": "Lbl"})
	exp := e.st.Export()
	b, _ := json.Marshal(exp)
	if strings.Contains(string(b), "TOPSECRET") || strings.Contains(string(b), "WIDGETSECRET") {
		t.Fatalf("secret in export: %s", b)
	}
	if len(exp.Plugins) != 1 || exp.Plugins[0].Source == "" || exp.Plugins[0].Config["city"] != "Oslo" || len(exp.Widgets) != 1 || exp.Widgets[0].Config["label"] != "Lbl" || exp.Widgets[0].ID != w.ID {
		t.Fatalf("export content: %+v", exp.Plugins)
	}
	// a plugin whose manifest cannot be read exports without any config
	rec, _ := e.st.GetPlugin("exp")
	_ = rec
	// import into a fresh instance
	e2 := newEnv(t)
	for _, p := range exp.Plugins {
		if id, err := e2.m.Validate(p.Source, p.Lang); err != nil || id != p.ID {
			t.Fatalf("validate: %v %q", err, id)
		}
	}
	res, err := e2.st.ImportData("replace", exp)
	if err != nil || res.Plugins != 1 || res.Widgets != 1 {
		t.Fatalf("import: %+v %v", res, err)
	}
	inf, err := e2.m.Info("exp", true)
	if err != nil || inf.Config["city"] != "Oslo" || len(inf.SecretsSet) != 0 {
		t.Fatalf("imported: %+v %v", inf, err)
	}
	if got := e2.m.Present(e2.st.Widgets(), true); len(got) != 1 || got[0].Config["label"] != "Lbl" {
		t.Fatalf("imported widgets: %+v", got)
	}
	// merge keeps the existing plugin untouched and assigns fresh widget ids
	res, err = e2.st.ImportData("merge", exp)
	if err != nil || res.Plugins != 0 || res.Widgets != 1 || len(e2.st.Widgets()) != 2 {
		t.Fatalf("merge: %+v %v", res, err)
	}
	// workspaces/wallpapers: a wallpaper of a plugin that is not part of the import is reset
	bad := store.Export{Schema: store.SchemaVersion, Workspaces: []store.Workspace{{ID: "a", Name: "A", Wallpaper: "plugin:ghost/x"}}}
	if _, err := e2.st.ImportData("replace", bad); err != nil {
		t.Fatal(err)
	}
	if got := e2.st.Snapshot().Workspaces[0].Wallpaper; got != store.DefaultWallpaper {
		t.Fatalf("dangling plugin wallpaper kept: %s", got)
	}
	if len(e2.st.Plugins()) != 0 {
		t.Fatal("replace import must remove plugins that are not in the data")
	}
	if _, err := os.Stat(filepath.Join(e2.dir, "plugins", "exp.js")); !os.IsNotExist(err) {
		t.Fatal("source file of a removed plugin remains")
	}
}

func TestUninstallCascade(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"gone", name:"G", version:"1", widgets:[{id:"w",name:"W"}], wallpapers:[{id:"wp",name:"WP"}]}`, `
export async function widget(){ genkan.storage.set("k", 1); return {body:[]} }
export async function wallpaper(){ return {url: "data:image/png;base64,`+pngB64(1)+`"} }`))
	w := addWidget(t, e, "gone", "w", nil)
	if _, err := e.m.Data(context.Background(), w.ID, "en", true); err != nil {
		t.Fatal(err)
	}
	ent, _, _ := e.m.entryFor("gone")
	if err := ent.kv.flush(); err != nil {
		t.Fatal(err)
	}
	ws := e.st.Snapshot().Workspaces[0]
	if _, err := e.st.UpdateWorkspace(ws.ID, store.WorkspacePatch{Wallpaper: ptr("plugin:gone/wp")}); err != nil {
		t.Fatal(err)
	}
	other, _ := e.st.CreateWorkspace(store.WorkspaceInput{Name: "Other", Wallpaper: "preset:aurora"})
	info, err := e.m.Wallpaper(context.Background(), "gone", "wp", "en")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(e.dir, "uploads", "plugin-wallpapers", strings.TrimPrefix(info.URL, "/uploads/plugin-wallpapers/"))
	for _, p := range []string{filepath.Join(e.dir, "plugins", "gone.js"), filepath.Join(e.dir, "plugins", "gone.kv.json"), file} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s: %v", p, err)
		}
	}
	// wallpapers referencing a missing plugin are rejected
	if _, err := e.st.UpdateWorkspace(other.ID, store.WorkspacePatch{Wallpaper: ptr("plugin:nope/x")}); store.ErrKind(err) != store.KindInvalid {
		t.Errorf("unknown plugin wallpaper: %v", err)
	}
	if _, err := e.st.UpdateWorkspace(other.ID, store.WorkspacePatch{Wallpaper: ptr("plugin:../x/y")}); store.ErrKind(err) != store.KindInvalid {
		t.Errorf("malformed plugin wallpaper: %v", err)
	}
	if err := e.m.Uninstall("gone"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(e.dir, "plugins", "gone.js"), filepath.Join(e.dir, "plugins", "gone.kv.json"), file} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be gone", p)
		}
	}
	if len(e.st.Widgets()) != 0 {
		t.Error("widgets remain")
	}
	snap := e.st.Snapshot()
	for _, x := range snap.Workspaces {
		if x.ID == ws.ID && x.Wallpaper != store.DefaultWallpaper {
			t.Errorf("wallpaper not reset: %s", x.Wallpaper)
		}
		if x.ID == other.ID && x.Wallpaper != "preset:aurora" {
			t.Errorf("unrelated workspace changed: %s", x.Wallpaper)
		}
	}
	if err := e.m.Uninstall("gone"); store.ErrKind(err) != store.KindNotFound {
		t.Errorf("double uninstall: %v", err)
	}
	// deleting a workspace deletes the widgets pinned to it
	e.install(plug(`{id:"again", name:"A", version:"1", widgets:[{id:"w",name:"W"}]}`, `export async function widget(){ return {body:[]} }`))
	ww, err := e.m.AddWidget(WidgetAdd{Plugin: "again", Widget: "w", WorkspaceID: other.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.DeleteWorkspace(other.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.st.GetWidget(ww.ID); ok {
		t.Error("widget of a deleted workspace remains")
	}
}

// pngB64 returns a small valid PNG (seed makes different images) as base64.
func pngB64(seed int) string {
	return base64.StdEncoding.EncodeToString(pngBytes(seed))
}

func pngBytes(seed int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		img.Set(x, x, color.RGBA{uint8(seed), uint8(seed >> 8), 7, 255})
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestWallpaperDownloadCachePrune(t *testing.T) {
	var imgHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/img/"):
			imgHits.Add(1)
			var seed int
			fmt.Sscan(strings.TrimPrefix(r.URL.Path, "/img/"), &seed)
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngBytes(seed))
		case r.URL.Path == "/html":
			w.Write([]byte("<html>not an image</html>"))
		case r.URL.Path == "/huge":
			w.Write(bytes.Repeat([]byte{0x89}, 16<<20))
		}
	}))
	defer srv.Close()
	e := newEnv(t)
	e.install(plug(`{id:"wp", name:"WP", version:"1", permissions:{fetch:["127.0.0.1"]},
      config:[{key:"base",type:"string",label:"B"},{key:"i",type:"number",label:"I"},{key:"path",type:"string",label:"P"}],
      wallpapers:[{id:"daily",name:"Daily",refresh:300},{id:"other",name:"Other"}]}`, `
export async function wallpaper(ctx) {
  const i = ctx.config.i || 0;
  const path = ctx.source === "other" ? "/img/9999" : (ctx.config.path || "/img/" + i);
  return { url: ctx.config.base + path, title: "T" + i, credit: "Credit", link: "https://example.com" };
}`))
	cfg := func(extra map[string]any) {
		m := map[string]any{"base": srv.URL}
		for k, v := range extra {
			m[k] = v
		}
		if _, err := e.m.Patch("wp", PatchRequest{Config: m}); err != nil {
			t.Fatal(err)
		}
	}
	cfg(map[string]any{"i": 1.0})
	bg := context.Background()
	info, err := e.m.Wallpaper(bg, "wp", "daily", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.URL, "/uploads/plugin-wallpapers/") || !strings.HasSuffix(info.URL, ".png") || info.Title != "T1" || info.Credit != "Credit" || info.Link != "https://example.com" || info.UpdatedAt == 0 {
		t.Fatalf("info %+v", info)
	}
	dir := filepath.Join(e.dir, "uploads", "plugin-wallpapers")
	if _, err := os.Stat(filepath.Join(dir, strings.TrimPrefix(info.URL, "/uploads/plugin-wallpapers/"))); err != nil {
		t.Fatal(err)
	}
	// cached: no second hook run / download
	if _, err := e.m.Wallpaper(bg, "wp", "daily", "en"); err != nil || imgHits.Load() != 1 {
		t.Fatalf("cache: hits=%d err=%v", imgHits.Load(), err)
	}
	// another source of the same plugin is stored separately
	if _, err := e.m.Wallpaper(bg, "wp", "other", "en"); err != nil {
		t.Fatal(err)
	}
	// each config change gives a new image; only the 5 most recent per source stay
	for i := 2; i <= 9; i++ {
		cfg(map[string]any{"i": float64(i)})
		if _, err := e.m.Wallpaper(bg, "wp", "daily", "en"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Millisecond) // distinct mtimes
	}
	pre := store.PluginSourcePrefix("wp", "daily")
	otherPre := store.PluginSourcePrefix("wp", "other")
	var daily, other int
	ents, _ := os.ReadDir(dir)
	for _, de := range ents {
		switch {
		case strings.HasPrefix(de.Name(), pre):
			daily++
		case strings.HasPrefix(de.Name(), otherPre):
			other++
		}
	}
	if daily != 5 || other != 1 {
		t.Fatalf("daily=%d other=%d files=%v", daily, other, ents)
	}
	// the current image is among the kept ones
	cur, _ := e.m.Wallpaper(bg, "wp", "daily", "en")
	if _, err := os.Stat(filepath.Join(dir, strings.TrimPrefix(cur.URL, "/uploads/plugin-wallpapers/"))); err != nil {
		t.Fatalf("current image pruned: %v", err)
	}
	// failures: not an image, too large, host not allowed, bad scheme
	e.m.Patch("wp", PatchRequest{Config: map[string]any{"base": srv.URL, "path": "/html"}})
	if _, err := e.m.Wallpaper(bg, "wp", "daily", "en"); err == nil || !strings.Contains(err.Error(), "not a jpg") {
		t.Errorf("html: %v", err)
	}
	e.m.Patch("wp", PatchRequest{Config: map[string]any{"base": srv.URL, "path": "/huge"}})
	if _, err := e.m.Wallpaper(bg, "wp", "daily", "en"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("huge: %v", err)
	}
	e.m.Patch("wp", PatchRequest{Config: map[string]any{"base": strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)}})
	if _, err := e.m.Wallpaper(bg, "wp", "daily", "en"); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("host allow list: %v", err)
	}
	// failed attempts are cached briefly
	hits := imgHits.Load()
	e.m.Wallpaper(bg, "wp", "daily", "en")
	if imgHits.Load() != hits {
		t.Error("error was not cached")
	}
	// unknown source
	if _, err := e.m.Wallpaper(bg, "wp", "zzz", "en"); store.ErrKind(err) != store.KindNotFound {
		t.Errorf("unknown source: %v", err)
	}
}

func TestInstallFromURL(t *testing.T) {
	src := plug(`{id:"remote", name:"R", version:"1", search:{name:"R"}}`, `export async function search(){ return [] }`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/p.js":
			fmt.Fprint(w, src)
		case "/big.js":
			fmt.Fprint(w, strings.Repeat("//x\n", 200<<10))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	e := newEnv(t)
	bg := context.Background()
	inf, err := e.m.Install(bg, InstallRequest{URL: srv.URL + "/p.js"})
	if err != nil || inf.SourceURL != srv.URL+"/p.js" || !inf.Enabled || inf.Admin == nil {
		t.Fatalf("%+v %v", inf, err)
	}
	for _, u := range []string{srv.URL + "/big.js", srv.URL + "/missing.js", "file:///etc/passwd", "ftp://x/y.js", "javascript:alert(1)", "not a url"} {
		if _, err := e.m.Install(bg, InstallRequest{URL: u}); err == nil {
			t.Errorf("%s should fail", u)
		}
	}
	if _, err := e.m.Install(bg, InstallRequest{Source: strings.Repeat("a", 513<<10)}); store.ErrKind(err) != store.KindInvalid {
		t.Errorf("oversized source: %v", err)
	}
	// update keeps config + enabled state; enable:false disables
	e.m.Patch("remote", PatchRequest{Enabled: ptr(true)})
	inf, err = e.m.Install(bg, InstallRequest{Source: src, Enable: ptr(false)})
	if err != nil || inf.Enabled || inf.SourceURL != "" {
		t.Fatalf("update with enable=false: %+v %v", inf, err)
	}
	if inf, _ = e.m.Install(bg, InstallRequest{Source: src}); inf.Enabled {
		t.Fatal("update without enable must keep the state")
	}
	if _, err := e.m.Install(bg, InstallRequest{Catalog: "zzz"}); store.ErrKind(err) != store.KindNotFound {
		t.Errorf("unknown catalog: %v", err)
	}
	// manifest id with path traversal characters never reaches the file system
	if _, err := e.m.Install(bg, InstallRequest{Source: plug(`{id:"../../evil", name:"x", version:"1", search:{name:"x"}}`, `export async function search(){}`)}); err == nil {
		t.Fatal("traversal id accepted")
	}
	if _, err := e.st.GetPlugin("../../evil"); err {
		t.Fatal("stored")
	}
}

func TestScheduler(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"cron", name:"C", version:"1", schedules:[{id:"tick",every:60}]}`, `
export async function schedule(ctx) { const n = (genkan.storage.get("n") || 0) + 1; genkan.storage.set("n", n); genkan.log("tick", n, ctx.lang, ctx.schedule); }`))
	// install started the real goroutine; stop it and drive the scheduler directly instead of waiting for real time
	s := e.m.sched
	s.stop()
	jobs := s.plan()
	if len(jobs) != 1 || jobs["cron|tick"].every != time.Minute {
		t.Fatalf("plan: %+v", jobs)
	}
	if d := time.Until(s.next["cron|tick"]); d <= 0 || d > firstRunAfterStart {
		t.Fatalf("first run in %s", d)
	}
	e.m.wg.Add(1)
	s.runOne(jobs["cron|tick"])
	logs, _ := e.m.Logs("cron")
	if len(logs) != 1 || logs[0].Msg != "tick 1 en tick" {
		t.Fatalf("logs: %+v", logs)
	}
	// disabling removes the schedule; no enabled plugin => the goroutine ends
	e.m.Patch("cron", PatchRequest{Enabled: ptr(false)})
	if jobs := s.plan(); len(jobs) != 0 {
		t.Fatalf("disabled plugin still scheduled: %+v", jobs)
	}
}

func TestSchedulerStartsAndStops(t *testing.T) {
	e := newEnv(t)
	running := func() bool {
		e.m.sched.mu.Lock()
		defer e.m.sched.mu.Unlock()
		return e.m.sched.running
	}
	if running() {
		t.Fatal("no goroutine before any plugin exists")
	}
	e.install(plug(`{id:"cron", name:"C", version:"1", schedules:[{id:"tick",every:60}]}`, `export async function schedule() {}`))
	if !running() {
		t.Fatal("scheduler should run with an enabled plugin")
	}
	e.m.Patch("cron", PatchRequest{Enabled: ptr(false)})
	deadline := time.Now().Add(3 * time.Second)
	for running() {
		if time.Now().After(deadline) {
			t.Fatal("scheduler goroutine did not exit without schedules")
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.m.Patch("cron", PatchRequest{Enabled: ptr(true)})
	if !running() {
		t.Fatal("scheduler did not restart")
	}
}

func TestNoRuntimeUntilUsed(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"lazy", name:"L", version:"1", search:{name:"L"}}`, `export async function search(){ return [] }`))
	ent, _, _ := e.m.entryFor("lazy")
	if ent.pool.idleCount() != 0 {
		t.Fatal("install must not leave a pooled runtime")
	}
	e.m.Search(context.Background(), "ab", "en")
	if ent.pool.idleCount() != 1 {
		t.Fatal("first use should pool one runtime")
	}
	ent.pool.purge()
	if ent.pool.idleCount() != 0 {
		t.Fatal("purge")
	}
}
