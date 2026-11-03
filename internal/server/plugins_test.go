package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jae-jae/genkan/internal/store"
)

const demoPlugin = `export const manifest = {
  id: "demo", name: "Demo", version: "1.0.0", icon: "puzzle",
  permissions: { fetch: ["127.0.0.1"] },
  config: [{ key: "token", type: "secret", label: "Token" }, { key: "note", type: "string", label: "Note" }],
  widgets: [{ id: "w", name: "W", size: "s", refresh: 60, config: [{ key: "pw", type: "secret", label: "PW" }, { key: "label", type: "string", label: "Label", default: "L" }] }],
  search: { name: "Demo search", prefix: "" },
  wallpapers: [{ id: "pic", name: "Pic" }],
};
export async function widget(ctx) {
  const n = (genkan.storage.get("n") || 0) + 1; genkan.storage.set("n", n);
  return { title: ctx.instance.config.label, body: [{ type: "text", text: "lang=" + ctx.lang + " n=" + n }, { type: "button", label: "Go", action: "go" }] };
}
export async function search(ctx) { return [{ title: "found " + ctx.query, copy: ctx.query }] }
export async function action(ctx) { return { toast: "did " + ctx.action, refresh: true } }
export async function wallpaper(ctx) { return { url: ctx.config.note, credit: "me" } }
`

func jsonBody(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestPluginRESTPermissions(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	var inf map[string]any
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": demoPlugin}), &inf)
	if inf["id"] != "demo" || inf["enabled"] != true {
		t.Fatalf("install: %v", inf)
	}
	var w map[string]any
	admin.mustJSON("POST", "/api/widgets", `{"plugin":"demo","widget":"w","config":{"pw":"swordfish"}}`, &w)
	wid := w["id"].(string)

	// everyone: anonymous (guest access off), guest, admin
	anon := e.anon()
	guestEnv := func() *client { e.st.UpdateSettings(store.SettingsPatch{GuestAccess: ptr(true)}); return e.anon() }

	type rq struct{ method, path, body string }
	adminOnly := []rq{
		{"GET", "/api/plugins", ""}, {"POST", "/api/plugins", `{"catalog":"x"}`}, {"GET", "/api/plugins/catalog", ""},
		{"GET", "/api/plugins/demo/source", ""}, {"GET", "/api/plugins/demo/logs", ""}, {"POST", "/api/plugins/demo/test", `{"hook":"search","query":"ab"}`},
		{"PATCH", "/api/plugins/demo", `{"enabled":true}`}, {"DELETE", "/api/plugins/demo", ""},
		{"POST", "/api/widgets", `{"plugin":"demo","widget":"w"}`}, {"PATCH", "/api/widgets/" + wid, `{"sort":3}`}, {"DELETE", "/api/widgets/" + wid, ""},
		{"POST", "/api/widgets/reorder", `{"ids":[]}`}, {"POST", "/api/widgets/" + wid + "/action", `{"action":"go"}`},
	}
	readable := []rq{
		{"GET", "/api/widgets/" + wid + "/data", ""}, {"GET", "/api/plugins/search?q=hello", ""}, {"GET", "/api/plugins/wallpaper/demo/pic", ""}, {"GET", "/api/bootstrap", ""},
	}
	// not logged in, guest access off: 401 everywhere except bootstrap (public)
	for _, r := range append(append([]rq{}, adminOnly...), readable...) {
		code, _ := anon.do(r.method, r.path, r.body)
		want := 401
		if r.path == "/api/bootstrap" {
			want = 200
		}
		if code != want {
			t.Errorf("anon %s %s = %d, want %d", r.method, r.path, code, want)
		}
	}
	// guest: read endpoints ok (data may 502 for the wallpaper, but never 401/403), admin endpoints 403
	g := guestEnv()
	for _, r := range adminOnly {
		if code, _ := g.do(r.method, r.path, r.body); code != 403 {
			t.Errorf("guest %s %s = %d, want 403", r.method, r.path, code)
		}
	}
	for _, r := range readable {
		if code, b := g.do(r.method, r.path, r.body); code == 401 || code == 403 {
			t.Errorf("guest %s %s = %d %s", r.method, r.path, code, b)
		}
	}
	// the cookie client needs the CSRF header on writes
	noCSRF := e.admin()
	noCSRF.csrf = false
	if code, _ := noCSRF.do("POST", "/api/widgets", `{"plugin":"demo","widget":"w"}`); code != 403 {
		t.Errorf("csrf: %d", code)
	}
	// token auth works for the admin API
	tok := e.token()
	if code, _ := tok.do("GET", "/api/plugins", ""); code != 200 {
		t.Errorf("token: %d", code)
	}
	// guests: ?force=1 is ignored (cached), admins can force
	var d1, d2, d3 struct {
		View      struct{ Body []map[string]any }
		UpdatedAt int64 `json:"updated_at"`
		NextAt    int64 `json:"next_at"`
	}
	g.mustJSON("GET", "/api/widgets/"+wid+"/data?lang=zh", "", &d1)
	g.mustJSON("GET", "/api/widgets/"+wid+"/data?force=1&lang=zh", "", &d2)
	if d1.UpdatedAt != d2.UpdatedAt || d1.UpdatedAt == 0 || d1.NextAt-d1.UpdatedAt != 60000 || !strings.HasPrefix(d1.View.Body[0]["text"].(string), "lang=zh n=") {
		t.Fatalf("guest force: %+v %+v", d1, d2)
	}
	admin.mustJSON("GET", "/api/widgets/"+wid+"/data?force=1&lang=zh", "", &d3)
	if d3.UpdatedAt < d1.UpdatedAt || d3.View.Body[0]["text"] == d1.View.Body[0]["text"] {
		t.Fatalf("admin force: %+v", d3)
	}
	// Accept-Language picks the language
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/widgets/"+wid+"/data?force=1", nil)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Authorization", "Bearer "+e.st.APIToken())
	resp, _ := http.DefaultClient.Do(req)
	var d4 struct {
		View struct{ Body []map[string]any }
	}
	json.NewDecoder(resp.Body).Decode(&d4)
	resp.Body.Close()
	if !strings.HasPrefix(d4.View.Body[0]["text"].(string), "lang=zh n=") {
		t.Fatalf("Accept-Language: %+v", d4)
	}
	// action
	var ar map[string]any
	admin.mustJSON("POST", "/api/widgets/"+wid+"/action", `{"action":"go","params":{"a":1}}`, &ar)
	if ar["toast"] != "did go" || ar["refresh"] != true {
		t.Fatalf("action: %v", ar)
	}
	if code, _ := admin.do("POST", "/api/widgets/"+wid+"/action", `{"action":"bad action!"}`); code != 400 {
		t.Errorf("bad action name: %d", code)
	}
	// search
	var sr struct {
		Groups []struct {
			Plugin, Name string
			Results      []map[string]any
		}
	}
	g.mustJSON("GET", "/api/plugins/search?q=hello", "", &sr)
	if len(sr.Groups) != 1 || sr.Groups[0].Plugin != "demo" || sr.Groups[0].Results[0]["title"] != "found hello" || sr.Groups[0].Name != "Demo search" {
		t.Fatalf("search: %+v", sr)
	}
	g.mustJSON("GET", "/api/plugins/search?q=h", "", &sr)
	if len(sr.Groups) != 0 {
		t.Fatalf("one-character queries are not sent to unprefixed plugins: %+v", sr)
	}
}

func ptr[T any](v T) *T { return &v }

func TestPluginRESTSecretsNeverLeak(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": demoPlugin}), nil)
	admin.mustJSON("PATCH", "/api/plugins/demo", `{"config":{"token":"tok-SECRET-1","note":"n"}}`, nil)
	var w map[string]any
	admin.mustJSON("POST", "/api/widgets", `{"plugin":"demo","widget":"w","config":{"pw":"pw-SECRET-2","label":"Lab"}}`, &w)
	wid := w["id"].(string)
	if w["config"].(map[string]any)["pw"] != "" || fmt.Sprint(w["secrets_set"]) != "[pw]" {
		t.Fatalf("widget create response: %v", w)
	}
	e.st.UpdateSettings(store.SettingsPatch{GuestAccess: ptr(true)})
	guest := e.anon()

	paths := []string{"/api/bootstrap", "/api/plugins", "/api/plugins/demo/logs", "/api/plugins/demo/source", "/api/export", "/api/plugins/search?q=zz", "/api/widgets/" + wid + "/data"}
	for _, p := range paths {
		for name, c := range map[string]*client{"admin": admin, "guest": guest} {
			code, b := c.do("GET", p, "")
			if strings.Contains(string(b), "SECRET") {
				t.Errorf("%s GET %s leaked a secret (status %d): %s", name, p, code, b)
			}
		}
	}
	// the admin sees that secrets are set, in plugin info and bootstrap widget instances
	var infos []map[string]any
	admin.mustJSON("GET", "/api/plugins", "", &infos)
	if fmt.Sprint(infos[0]["secrets_set"]) != "[token]" || infos[0]["config"].(map[string]any)["token"] != "" || infos[0]["config"].(map[string]any)["note"] != "n" {
		t.Fatalf("plugin info: %v", infos[0])
	}
	var bs struct {
		Plugins []map[string]any
		Widgets []map[string]any
	}
	admin.mustJSON("GET", "/api/bootstrap", "", &bs)
	if len(bs.Plugins) != 1 || bs.Plugins[0]["config"] != nil || bs.Plugins[0]["config_schema"] == nil || bs.Plugins[0]["permissions"] == nil || bs.Plugins[0]["search"] == nil || bs.Plugins[0]["wallpapers"] == nil {
		t.Fatalf("bootstrap plugins: %v", bs.Plugins)
	}
	if len(bs.Widgets) != 1 || bs.Widgets[0]["config"].(map[string]any)["pw"] != "" || bs.Widgets[0]["config"].(map[string]any)["label"] != "Lab" || fmt.Sprint(bs.Widgets[0]["secrets_set"]) != "[pw]" {
		t.Fatalf("bootstrap widgets (admin): %v", bs.Widgets)
	}
	bs = struct {
		Plugins []map[string]any
		Widgets []map[string]any
	}{}
	guest.mustJSON("GET", "/api/bootstrap", "", &bs)
	if _, has := bs.Widgets[0]["config"].(map[string]any)["pw"]; has || bs.Widgets[0]["secrets_set"] != nil || bs.Widgets[0]["config"].(map[string]any)["label"] != "Lab" {
		t.Fatalf("bootstrap widgets (guest): %v", bs.Widgets)
	}
	// blank keeps, null clears (plugin level and instance level)
	admin.mustJSON("PATCH", "/api/plugins/demo", `{"config":{"token":"","note":"n2"}}`, nil)
	admin.mustJSON("PATCH", "/api/widgets/"+wid, `{"config":{"pw":"","label":"Lab2"}}`, &w)
	if fmt.Sprint(w["secrets_set"]) != "[pw]" {
		t.Fatalf("blank must keep: %v", w)
	}
	rec, _ := e.st.GetPlugin("demo")
	if rec.Config["token"] != "tok-SECRET-1" {
		t.Fatalf("secret lost: %v", rec.Config)
	}
	w = nil
	admin.mustJSON("PATCH", "/api/widgets/"+wid, `{"config":{"pw":null,"label":"Lab2"}}`, &w)
	if w["secrets_set"] != nil {
		t.Fatalf("null must clear: %v", w)
	}
	// the secret is never exposed by anything after a failing plugin run either
	code, b := admin.do("POST", "/api/plugins/demo/test", `{"hook":"wallpaper"}`)
	if code != 200 || strings.Contains(string(b), "SECRET") {
		t.Fatalf("test endpoint: %d %s", code, b)
	}
}

func TestPluginInstallEndpoints(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	var cat []map[string]any
	admin.mustJSON("GET", "/api/plugins/catalog", "", &cat)
	if len(cat) < 10 {
		t.Fatalf("catalog: %v", cat)
	}
	ids := map[string]bool{}
	for _, c := range cat {
		ids[c["id"].(string)] = c["installed"] == true
		for _, k := range []string{"name", "description", "icon", "version"} {
			if c[k] == nil {
				t.Errorf("catalog item lacks %s: %v", k, c)
			}
		}
	}
	if installed, ok := ids["calculator"]; !ok || installed {
		t.Fatalf("calculator: %v %v", ok, installed)
	}
	var inf map[string]any
	admin.mustJSON("POST", "/api/plugins", `{"catalog":"calculator","enable":false}`, &inf)
	if inf["enabled"] != false || inf["permissions"] == nil || inf["config_schema"] == nil || inf["search"] == nil {
		t.Fatalf("install disabled: %v", inf)
	}
	if code, _ := admin.do("GET", "/api/plugins/search?q=1%2B1", ""); code != 200 {
		t.Fatal(code)
	}
	var sr struct{ Groups []any }
	admin.mustJSON("GET", "/api/plugins/search?q=1%2B1", "", &sr)
	if len(sr.Groups) != 0 {
		t.Fatalf("disabled plugin searched: %v", sr)
	}
	admin.mustJSON("PATCH", "/api/plugins/calculator", `{"enabled":true}`, &inf)
	admin.mustJSON("GET", "/api/plugins/search?q=1%2B1", "", &sr)
	if len(sr.Groups) != 1 {
		t.Fatalf("enabled plugin not searched: %v", sr)
	}
	var src map[string]string
	admin.mustJSON("GET", "/api/plugins/calculator/source", "", &src)
	if !strings.Contains(src["source"], "manifest") || src["lang"] != "js" {
		t.Fatalf("source: %v", src)
	}
	var lg struct{ Logs []any }
	admin.mustJSON("GET", "/api/plugins/calculator/logs", "", &lg)
	if lg.Logs == nil {
		t.Fatal("logs must be an array")
	}
	var tr map[string]any
	admin.mustJSON("POST", "/api/plugins/calculator/test", `{"hook":"search","query":"2*21"}`, &tr)
	res := tr["result"].([]any)
	if tr["ok"] != true || res[0].(map[string]any)["copy"] != "42" || tr["logs"] == nil || tr["ms"] == nil {
		t.Fatalf("test: %v", tr)
	}
	// errors
	for name, c := range map[string]struct {
		method, path, body string
		want               int
	}{
		"empty body":       {"POST", "/api/plugins", ``, 400},
		"no source":        {"POST", "/api/plugins", `{}`, 400},
		"two sources":      {"POST", "/api/plugins", `{"source":"x","catalog":"calculator"}`, 400},
		"broken source":    {"POST", "/api/plugins", `{"source":"export const manifest = {id:'zz'}"}`, 400},
		"unknown catalog":  {"POST", "/api/plugins", `{"catalog":"nope"}`, 404},
		"file url":         {"POST", "/api/plugins", `{"url":"file:///etc/passwd"}`, 400},
		"unknown plugin":   {"PATCH", "/api/plugins/nope", `{"enabled":true}`, 404},
		"delete unknown":   {"DELETE", "/api/plugins/nope", ``, 404},
		"traversal source": {"GET", "/api/plugins/..%2Fx/source", ``, 404},
		"widget unknown":   {"POST", "/api/widgets", `{"plugin":"calculator","widget":"w"}`, 404},
		"widget gone":      {"GET", "/api/widgets/nope/data", ``, 404},
		"test bad hook":    {"POST", "/api/plugins/calculator/test", `{"hook":"x"}`, 400},
		"wallpaper none":   {"GET", "/api/plugins/wallpaper/nope/x", ``, 404},
		"wallpaper source": {"GET", "/api/plugins/wallpaper/calculator/x", ``, 404},
	} {
		if code, b := admin.do(c.method, c.path, c.body); code != c.want || !strings.Contains(string(b), `"error"`) {
			t.Errorf("%s: %d %s, want %d", name, code, b, c.want)
		}
	}
	// failing hooks: 502 with a message; disabled plugin: 409
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": strings.Replace(demoPlugin, `return { title: ctx.instance.config.label,`, `throw new Error("kaput"); return { title: 1,`, 1)}), nil)
	var w map[string]any
	admin.mustJSON("POST", "/api/widgets", `{"plugin":"demo","widget":"w"}`, &w)
	code, b := admin.do("GET", "/api/widgets/"+w["id"].(string)+"/data", "")
	if code != 502 || !strings.Contains(string(b), "kaput") {
		t.Errorf("hook failure: %d %s", code, b)
	}
	admin.mustJSON("PATCH", "/api/plugins/demo", `{"enabled":false}`, nil)
	if code, _ := admin.do("GET", "/api/widgets/"+w["id"].(string)+"/data", ""); code != 409 {
		t.Errorf("disabled widget data: %d", code)
	}
	if code, _ := admin.do("GET", "/api/plugins/wallpaper/demo/pic", ""); code != 409 {
		t.Errorf("disabled wallpaper: %d", code)
	}
	admin.mustJSON("DELETE", "/api/plugins/demo", "", nil)
	if code, _ := admin.do("GET", "/api/widgets/"+w["id"].(string)+"/data", ""); code != 404 {
		t.Errorf("deleted widget data: %d", code)
	}
}

func TestPluginInstallFromURL(t *testing.T) {
	src := strings.Replace(demoPlugin, `id: "demo"`, `id: "remote"`, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big.js" {
			w.Write(bytes.Repeat([]byte("//xxxxxxxx\n"), 60000))
			return
		}
		w.Write([]byte(src))
	}))
	defer ts.Close()
	e := setup(t)
	admin := e.admin()
	var inf map[string]any
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"url": ts.URL + "/p.js"}), &inf)
	if inf["id"] != "remote" || inf["source_url"] != ts.URL+"/p.js" {
		t.Fatalf("%v", inf)
	}
	if code, b := admin.do("POST", "/api/plugins", jsonBody(map[string]any{"url": ts.URL + "/big.js"})); code != 400 || !strings.Contains(string(b), "too large") {
		t.Errorf("big: %d %s", code, b)
	}
}

func TestPluginWallpaperEndpoint(t *testing.T) {
	img := pngFile()
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(img) }))
	defer imgSrv.Close()
	e := setup(t)
	admin := e.admin()
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": demoPlugin}), nil)
	admin.mustJSON("PATCH", "/api/plugins/demo", jsonBody(map[string]any{"config": map[string]any{"note": imgSrv.URL + "/a.png"}}), nil)
	var wp map[string]any
	admin.mustJSON("GET", "/api/plugins/wallpaper/demo/pic", "", &wp)
	url, _ := wp["url"].(string)
	if !strings.HasPrefix(url, "/uploads/plugin-wallpapers/") || wp["credit"] != "me" || wp["updated_at"] == nil {
		t.Fatalf("wallpaper: %v", wp)
	}
	// served by the existing uploads handler with read permission
	if code, b := admin.do("GET", url, ""); code != 200 || !bytes.Equal(b, img) {
		t.Fatalf("serve: %d (%d bytes)", code, len(b))
	}
	if code, _ := e.anon().do("GET", url, ""); code != 401 {
		t.Errorf("anonymous upload read: %d", code)
	}
	e.st.UpdateSettings(store.SettingsPatch{GuestAccess: ptr(true)})
	if code, _ := e.anon().do("GET", url, ""); code != 200 {
		t.Errorf("guest upload read: %d", code)
	}
	// the workspace can use the plugin wallpaper, and uninstalling resets it
	ws := e.st.Snapshot().Workspaces[0]
	admin.mustJSON("PATCH", "/api/workspaces/"+ws.ID, `{"wallpaper":"plugin:demo/pic"}`, nil)
	if code, _ := admin.do("PATCH", "/api/workspaces/"+ws.ID, `{"wallpaper":"plugin:ghost/pic"}`); code != 400 {
		t.Errorf("unknown plugin wallpaper: %d", code)
	}
	admin.mustJSON("DELETE", "/api/plugins/demo", "", nil)
	if got := e.st.Snapshot().Workspaces[0].Wallpaper; got != store.DefaultWallpaper {
		t.Errorf("wallpaper after uninstall: %s", got)
	}
	if code, _ := admin.do("GET", url, ""); code != 404 {
		t.Errorf("downloaded file should be gone: %d", code)
	}
	// path traversal through the uploads kind
	if code, _ := admin.do("GET", "/uploads/plugin-wallpapers/..%2F..%2Fgenkan.json", ""); code != 404 {
		t.Errorf("traversal: %d", code)
	}
}

func pngFile() []byte {
	return []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x9a\xa0\xa0\x00\x00\x00\x00IEND\xaeB`\x82")
}

func TestPluginExportImportREST(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": demoPlugin}), nil)
	admin.mustJSON("PATCH", "/api/plugins/demo", `{"config":{"token":"tok-SECRET-1","note":"keep"}}`, nil)
	admin.mustJSON("POST", "/api/widgets", `{"plugin":"demo","widget":"w","config":{"pw":"pw-SECRET-2","label":"Lab"}}`, nil)
	code, exp := admin.do("GET", "/api/export", "")
	if code != 200 || strings.Contains(string(exp), "SECRET") {
		t.Fatalf("export: %d %s", code, exp)
	}
	var x struct {
		Plugins []struct {
			ID     string
			Source string
			Config map[string]any
		}
		Widgets []map[string]any
	}
	json.Unmarshal(exp, &x)
	if len(x.Plugins) != 1 || !strings.Contains(x.Plugins[0].Source, "manifest") || x.Plugins[0].Config["note"] != "keep" || len(x.Widgets) != 1 {
		t.Fatalf("export content: %s", exp)
	}
	// import into another instance
	e2 := setup(t)
	a2 := e2.admin()
	var res map[string]any
	a2.mustJSON("POST", "/api/import", `{"mode":"replace","data":`+string(exp)+`}`, &res)
	if res["plugins"] != 1.0 || res["widgets"] != 1.0 {
		t.Fatalf("import: %v", res)
	}
	var infos []map[string]any
	a2.mustJSON("GET", "/api/plugins", "", &infos)
	if len(infos) != 1 || infos[0]["config"].(map[string]any)["note"] != "keep" || infos[0]["last_error"] != "" {
		t.Fatalf("imported plugin: %v", infos)
	}
	if _, err := os.Stat(filepath.Join(e2.srv.cfg.DataDir, "plugins", "demo.js")); err != nil {
		t.Fatal(err)
	}
	// importing a plugin with broken code or a mismatching id is rejected as a whole
	for name, src := range map[string]string{
		"syntax":   "export const manifest = {",
		"mismatch": strings.Replace(demoPlugin, `id: "demo"`, `id: "other"`, 1),
	} {
		bad := fmt.Sprintf(`{"mode":"merge","data":{"schema":2,"workspaces":[],"categories":[],"links":[],"plugins":[{"id":"demo","enabled":true,"source":%q}]}}`, src)
		if code, b := a2.do("POST", "/api/import", bad); code != 400 {
			t.Errorf("%s: %d %s", name, code, b)
		}
	}
	// plain v1 exports (no plugin keys) still import
	a2.mustJSON("POST", "/api/import", `{"mode":"merge","data":{"schema":1,"workspaces":[],"categories":[],"links":[]}}`, nil)
}

func TestPluginBootstrapEmptyForVisitors(t *testing.T) {
	e := setup(t)
	var bs struct {
		Plugins, Widgets []any
	}
	e.anon().mustJSON("GET", "/api/bootstrap", "", &bs)
	if bs.Plugins == nil || bs.Widgets == nil || len(bs.Plugins) != 0 {
		t.Fatalf("%+v", bs)
	}
}

func TestPluginSourceEditAndPreviewREST(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": demoPlugin, "enable": false}), nil)
	admin.mustJSON("PATCH", "/api/plugins/demo", `{"config":{"note":"keep"}}`, nil)
	admin.mustJSON("POST", "/api/widgets", `{"plugin":"demo","widget":"w"}`, nil)

	v2 := strings.Replace(demoPlugin, `version: "1.0.0"`, `version: "2.0.0"`, 1)
	var inf map[string]any
	admin.mustJSON("PUT", "/api/plugins/demo/source", jsonBody(map[string]any{"source": v2}), &inf)
	if inf["version"] != "2.0.0" || inf["enabled"] != false || inf["config"].(map[string]any)["note"] != "keep" {
		t.Fatalf("%v", inf)
	}
	var src map[string]string
	admin.mustJSON("GET", "/api/plugins/demo/source", "", &src)
	if src["source"] != v2 || src["lang"] != "js" {
		t.Fatalf("%v", src)
	}
	if ws := e.st.Widgets(); len(ws) != 1 {
		t.Error("widgets must be kept")
	}
	// errors
	code, b := admin.do("PUT", "/api/plugins/demo/source", jsonBody(map[string]any{"source": "export const manifest = {\n  id: \"demo\",\n  name: ;\n};"}))
	if code != 400 || !strings.Contains(string(b), "3:") {
		t.Errorf("syntax: %d %s", code, b)
	}
	if code, b := admin.do("PUT", "/api/plugins/demo/source", jsonBody(map[string]any{"source": strings.Replace(v2, `id: "demo"`, `id: "zzz"`, 1)})); code != 400 || !strings.Contains(string(b), "demo") {
		t.Errorf("id: %d %s", code, b)
	}
	if code, _ := admin.do("PUT", "/api/plugins/nope/source", jsonBody(map[string]any{"source": v2})); code != 404 {
		t.Errorf("404: %d", code)
	}
	if code, _ := admin.do("PUT", "/api/plugins/demo/source", `{"source":`); code != 400 {
		t.Errorf("bad json: %d", code)
	}

	// preview: nothing installed
	var pv map[string]any
	admin.mustJSON("POST", "/api/plugins/preview", jsonBody(map[string]any{"source": strings.Replace(demoPlugin, `id: "demo"`, `id: "fresh"`, 1)}), &pv)
	pl := pv["plugin"].(map[string]any)
	if pl["id"] != "fresh" || pv["installed"] != false || pv["lang"] != "js" || !strings.Contains(pv["source"].(string), "fresh") || pl["permissions"] == nil {
		t.Fatalf("%v", pv)
	}
	if _, has := pl["config"]; has {
		t.Errorf("preview exposes admin fields: %v", pl)
	}
	admin.mustJSON("POST", "/api/plugins/preview", jsonBody(map[string]any{"source": v2}), &pv)
	if pv["installed"] != true || pv["installed_version"] != "2.0.0" {
		t.Errorf("%v", pv)
	}
	admin.mustJSON("POST", "/api/plugins/preview", jsonBody(map[string]any{"source": "export const manifest = {"}), &pv)
	if pv["plugin"] != nil || pv["error"] == "" || pv["source"] == "" {
		t.Errorf("%v", pv)
	}
	admin.mustJSON("POST", "/api/plugins/preview", `{"catalog":"calculator"}`, &pv)
	if pv["plugin"].(map[string]any)["id"] != "calculator" {
		t.Errorf("%v", pv)
	}
	if code, _ := admin.do("POST", "/api/plugins/preview", `{}`); code != 400 {
		t.Errorf("empty: %d", code)
	}
	if got := len(e.st.Plugins()); got != 1 {
		t.Errorf("preview installed something: %d plugins", got)
	}
}

func TestPluginMarketREST(t *testing.T) {
	var searches atomic.Int32
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search/repositories":
			searches.Add(1)
			if r.Header.Get("Authorization") != "Bearer ghp_test" {
				t.Errorf("token not sent: %q", r.Header.Get("Authorization"))
			}
			w.Write([]byte(`{"total_count":1,"items":[{"full_name":"o/r","description":"d","stargazers_count":5,"updated_at":"2026-01-01T00:00:00Z","html_url":"https://github.com/o/r","default_branch":"main","owner":{"avatar_url":"https://a/x.png"}}]}`))
		case r.URL.Path == "/repos/o/r":
			w.Write([]byte(`{"default_branch":"main"}`))
		case r.URL.Path == "/repos/o/r/git/trees/main":
			w.Write([]byte(`{"tree":[{"path":"p.js","type":"blob","size":100}]}`))
		case r.URL.Path == "/raw/o/r/main/p.js":
			w.Write([]byte(strings.Replace(demoPlugin, `id: "demo"`, `id: "market-one"`, 1)))
		case strings.HasPrefix(r.URL.Path, "/raw/"):
			http.NotFound(w, r)
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	defer gh.Close()
	e := setupCfg(t, func(c *Config) { c.GitHubAPI, c.GitHubRaw, c.GitHubToken = gh.URL, gh.URL+"/raw", "ghp_test" })
	admin := e.admin()

	var mk struct {
		Items []map[string]any `json:"items"`
		Total int
		Error string
	}
	admin.mustJSON("GET", "/api/plugins/market?sort=updated&q=foo&page=1", "", &mk)
	if mk.Total != 1 || len(mk.Items) != 1 || mk.Items[0]["repo"] != "o/r" || mk.Items[0]["owner_avatar"] != "https://a/x.png" || mk.Items[0]["stars"] != 5.0 || mk.Error != "" {
		t.Fatalf("%+v", mk)
	}
	if code, _ := admin.do("GET", "/api/plugins/market?sort=bogus", ""); code != 400 {
		t.Errorf("sort: %d", code)
	}
	if code, _ := admin.do("GET", "/api/plugins/market?page=x", ""); code != 400 {
		t.Errorf("page: %d", code)
	}
	var sc struct {
		Repo          string
		DefaultBranch string `json:"default_branch"`
		Plugins       []struct {
			Path, URL string
			Plugin    map[string]any
			Installed bool
		}
	}
	admin.mustJSON("GET", "/api/plugins/market/repo?repo=o/r", "", &sc)
	if len(sc.Plugins) != 1 || sc.Plugins[0].Plugin["id"] != "market-one" || sc.Plugins[0].URL != gh.URL+"/raw/o/r/main/p.js" || sc.Plugins[0].Installed {
		t.Fatalf("%+v", sc)
	}
	// installing from the scanned URL records source_url and flips "installed"
	var inf map[string]any
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"url": sc.Plugins[0].URL}), &inf)
	if inf["source_url"] != sc.Plugins[0].URL {
		t.Errorf("%v", inf)
	}
	var sc2 struct{ Plugins []struct{ Installed bool } }
	admin.mustJSON("GET", "/api/plugins/market/repo?repo=o/r", "", &sc2)
	if !sc2.Plugins[0].Installed {
		t.Error("installed flag")
	}
	for _, bad := range []string{"", "x", "../../etc", "a/b/c", "a/b%3Fx"} {
		if code, _ := admin.do("GET", "/api/plugins/market/repo?repo="+bad, ""); code != 400 {
			t.Errorf("repo=%q: %d", bad, code)
		}
	}
	// GitHub failures are 200 with an error, not a 5xx
	var sr map[string]any
	admin.mustJSON("GET", "/api/plugins/market/repo?repo=o/unknown", "", &sr)
	if sr["error"] == nil || sr["error"] == "" {
		t.Errorf("%v", sr)
	}
	gh.Close()
	admin.mustJSON("GET", "/api/plugins/market?q=down", "", &mk)
	if mk.Error == "" || mk.Items == nil {
		t.Errorf("%+v", mk)
	}

	// admin only
	for _, p := range []string{"/api/plugins/market", "/api/plugins/market/repo?repo=o/r"} {
		if code, _ := e.anon().do("GET", p, ""); code != 401 {
			t.Errorf("anon %s: %d", p, code)
		}
	}
	if code, _ := e.anon().do("PUT", "/api/plugins/demo/source", `{"source":"x"}`); code != 401 {
		t.Errorf("anon put: %d", code)
	}
	e.st.UpdateSettings(store.SettingsPatch{GuestAccess: ptr(true)})
	g := e.anon()
	for _, rq := range [][3]string{{"GET", "/api/plugins/market", ""}, {"GET", "/api/plugins/market/repo?repo=o/r", ""}, {"POST", "/api/plugins/preview", `{"catalog":"calculator"}`}, {"PUT", "/api/plugins/demo/source", `{"source":"x"}`}} {
		if code, _ := g.do(rq[0], rq[1], rq[2]); code != 403 {
			t.Errorf("guest %s %s: %d", rq[0], rq[1], code)
		}
	}
}

func TestPluginMarketClearCache(t *testing.T) {
	var searches, trees atomic.Int32
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/repositories":
			searches.Add(1)
			w.Write([]byte(`{"total_count":1,"items":[{"full_name":"o/r","default_branch":"main","owner":{}}]}`))
		case "/repos/o/r":
			w.Write([]byte(`{"default_branch":"main"}`))
		case "/repos/o/r/git/trees/main":
			trees.Add(1)
			w.Write([]byte(`{"tree":[]}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	defer gh.Close()
	e := setupCfg(t, func(c *Config) { c.GitHubAPI, c.GitHubRaw = gh.URL, gh.URL+"/raw" })
	admin := e.admin()
	fetch := func() {
		admin.mustJSON("GET", "/api/plugins/market?q=x", "", nil)
		admin.mustJSON("GET", "/api/plugins/market/repo?repo=o/r", "", nil)
	}
	fetch()
	fetch() // served from the cache
	if searches.Load() != 1 || trees.Load() != 1 {
		t.Fatalf("expected one GitHub search and one scan before clearing, got %d / %d", searches.Load(), trees.Load())
	}
	var ok map[string]any
	admin.mustJSON("POST", "/api/plugins/market/cache/clear", "", &ok)
	if ok["ok"] != true {
		t.Fatalf("clear response: %v", ok)
	}
	fetch()
	if searches.Load() != 2 || trees.Load() != 2 {
		t.Errorf("clearing did not empty both caches: %d / %d", searches.Load(), trees.Load())
	}
	admin.mustJSON("POST", "/api/plugins/market/cache/clear", "", nil) // idempotent on an empty cache
	admin.mustJSON("POST", "/api/plugins/market/cache/clear", "", nil)

	// admin only, CSRF header required for cookie sessions
	noCSRF := &client{e: e, c: admin.c, csrf: false}
	if code, _ := noCSRF.do("POST", "/api/plugins/market/cache/clear", ""); code != 403 {
		t.Errorf("csrf: %d", code)
	}
	if code, _ := e.anon().do("POST", "/api/plugins/market/cache/clear", ""); code != 401 {
		t.Errorf("anon: %d", code)
	}
	e.st.UpdateSettings(store.SettingsPatch{GuestAccess: ptr(true)})
	if code, _ := e.anon().do("POST", "/api/plugins/market/cache/clear", ""); code != 403 {
		t.Errorf("guest: %d", code)
	}
}

func TestWidgetLayoutREST(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": demoPlugin}), nil)
	var w map[string]any
	admin.mustJSON("POST", "/api/widgets", `{"plugin":"demo","widget":"w","w":4,"h":300}`, &w)
	if w["w"] != 4.0 || w["h"] != 300.0 {
		t.Fatalf("%v", w)
	}
	id := w["id"].(string)
	admin.mustJSON("PATCH", "/api/widgets/"+id, `{"w":99,"h":5}`, &w)
	if w["w"] != 6.0 || w["h"] != 80.0 {
		t.Fatalf("clamp: %v", w)
	}
	admin.mustJSON("PATCH", "/api/widgets/"+id, `{"h":0}`, &w)
	if w["w"] != 6.0 || w["h"] != 0.0 {
		t.Fatalf("auto height keeps w: %v", w)
	}
	admin.mustJSON("PATCH", "/api/widgets/"+id, `{"w":3,"h":400}`, nil)
	// bootstrap (guests too) carries w/h
	e.st.UpdateSettings(store.SettingsPatch{GuestAccess: ptr(true)})
	var bs struct{ Widgets []map[string]any }
	e.anon().mustJSON("GET", "/api/bootstrap", "", &bs)
	if len(bs.Widgets) != 1 || bs.Widgets[0]["w"] != 3.0 || bs.Widgets[0]["h"] != 400.0 {
		t.Fatalf("%v", bs.Widgets)
	}
	// export/import keep the layout (and clamp hand-edited values)
	_, exp := admin.do("GET", "/api/export", "")
	e2 := setup(t)
	a2 := e2.admin()
	a2.mustJSON("POST", "/api/import", `{"mode":"replace","data":`+string(exp)+`}`, nil)
	ws := e2.st.Widgets()
	if len(ws) != 1 || ws[0].W != 3 || ws[0].H != 400 {
		t.Fatalf("%+v", ws)
	}
	var x map[string]any
	json.Unmarshal(exp, &x)
	x["widgets"].([]any)[0].(map[string]any)["w"] = 50
	x["widgets"].([]any)[0].(map[string]any)["h"] = 3
	mod, _ := json.Marshal(x)
	a2.mustJSON("POST", "/api/import", `{"mode":"replace","data":`+string(mod)+`}`, nil)
	if ws := e2.st.Widgets(); ws[0].W != 6 || ws[0].H != 80 {
		t.Fatalf("%+v", ws)
	}
	// old data without w/h reads as 0/0
	delete(x["widgets"].([]any)[0].(map[string]any), "w")
	delete(x["widgets"].([]any)[0].(map[string]any), "h")
	oldData, _ := json.Marshal(x)
	old := `{"mode":"replace","data":` + string(oldData) + `}`
	a2.mustJSON("POST", "/api/import", old, nil)
	if ws := e2.st.Widgets(); len(ws) != 1 || ws[0].W != 0 || ws[0].H != 0 {
		t.Fatalf("%+v", ws)
	}
}

const locPlugin = `export const messages = { en: { n: "Locale demo", g: "Group", hi: "Hello {q}" }, zh: { n: "本地化演示", g: "分组", hi: "你好 {q}" } };
export const manifest = {
  id: "loc-demo", name: t("n"), version: "1", description: { en: "English text", zh: "中文说明" },
  config: [{ key: "city", type: "string", label: t("n"), required: true }],
  widgets: [{ id: "w", name: t("n"), config: [{ key: "x", type: "string", label: t("g"), required: true }] }],
  search: { name: t("g"), prefix: "" },
};
export async function widget(ctx) { return { title: ctx.t("n"), body: [{ type: "text", text: ctx.t("hi", { q: ctx.locale }) }] } }
export async function search(ctx) { return [{ title: ctx.t("hi", { q: ctx.query }) }] }
`

func TestPluginLocalization(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	var inf map[string]any
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": locPlugin}), &inf)
	wantName := map[string]any{"en": "Locale demo", "zh": "本地化演示"}
	if fmt.Sprint(inf["name"]) != fmt.Sprint(wantName) {
		t.Fatalf("install response: %v", inf["name"])
	}

	// bootstrap, plugin list, preview and the catalog all carry the objects
	var boot struct {
		Plugins []map[string]any `json:"plugins"`
	}
	admin.mustJSON("GET", "/api/bootstrap", "", &boot)
	var found map[string]any
	for _, p := range boot.Plugins {
		if p["id"] == "loc-demo" {
			found = p
		}
	}
	if found == nil || fmt.Sprint(found["name"]) != fmt.Sprint(wantName) || fmt.Sprint(found["description"]) != fmt.Sprint(map[string]any{"en": "English text", "zh": "中文说明"}) {
		t.Fatalf("bootstrap: %v", found)
	}
	var list []map[string]any
	admin.mustJSON("GET", "/api/plugins", "", &list)
	if fmt.Sprint(list[0]["name"]) != fmt.Sprint(wantName) {
		t.Fatalf("list: %v", list[0]["name"])
	}
	var pv struct {
		Plugin map[string]any `json:"plugin"`
	}
	admin.mustJSON("POST", "/api/plugins/preview", jsonBody(map[string]any{"source": locPlugin}), &pv)
	if fmt.Sprint(pv.Plugin["name"]) != fmt.Sprint(wantName) {
		t.Fatalf("preview: %v", pv.Plugin["name"])
	}
	var cat []map[string]any
	admin.mustJSON("GET", "/api/plugins/catalog", "", &cat)
	for _, c := range cat {
		if c["id"] == "weather" {
			if n, ok := c["name"].(map[string]any); !ok || n["en"] != "Weather" || n["zh"] != "天气" {
				t.Fatalf("catalog weather: %v", c["name"])
			}
		}
	}

	// search: the group name is resolved for the request language
	var sr struct {
		Groups []struct{ Name string } `json:"groups"`
	}
	for q, want := range map[string]string{"?lang=zh": "分组", "?lang=en": "Group", "?lang=fr": "Group"} {
		admin.mustJSON("GET", "/api/plugins/search"+q+"&q=hey", "", &sr)
		if len(sr.Groups) != 1 || sr.Groups[0].Name != want {
			t.Errorf("search %s: %+v", q, sr)
		}
	}
	if code, b := admin.do("GET", "/api/plugins/search?q=hey", ""); code != 200 || !strings.Contains(string(b), `"name":"Group"`) {
		t.Errorf("default language: %d %s", code, b)
	}

	// widget data and config errors follow the language
	var w map[string]any
	admin.mustJSON("POST", "/api/widgets", `{"plugin":"loc-demo","widget":"w","config":{"x":"a"}}`, &w)
	var d struct {
		View struct {
			Title string
			Body  []map[string]any
		}
	}
	admin.mustJSON("GET", "/api/widgets/"+w["id"].(string)+"/data?lang=zh", "", &d)
	if d.View.Title != "本地化演示" || d.View.Body[0]["text"] != "你好 zh-CN" {
		t.Errorf("zh: %+v", d.View)
	}
	admin.mustJSON("GET", "/api/widgets/"+w["id"].(string)+"/data?lang=en", "", &d)
	if d.View.Title != "Locale demo" || d.View.Body[0]["text"] != "Hello en-US" {
		t.Errorf("en: %+v", d.View)
	}
	code, b := admin.do("POST", "/api/widgets?lang=zh", `{"plugin":"loc-demo","widget":"w","config":{}}`)
	if code != 400 || !strings.Contains(string(b), "分组 is required") {
		t.Errorf("zh error: %d %s", code, b)
	}
	code, b = admin.do("POST", "/api/widgets?lang=en", `{"plugin":"loc-demo","widget":"w","config":{}}`)
	if code != 400 || !strings.Contains(string(b), "Group is required") {
		t.Errorf("en error: %d %s", code, b)
	}
	code, b = admin.do("PATCH", "/api/plugins/loc-demo?lang=zh", `{"config":{}}`)
	if code != 400 || !strings.Contains(string(b), "本地化演示 is required") {
		t.Errorf("patch zh error: %d %s", code, b)
	}
}
