package plugin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jae-jae/genkan/internal/store"
)

func shortTimeouts(t *testing.T) {
	t.Helper()
	d, s, sc := timeoutDefault, timeoutSearch, timeoutSched
	timeoutDefault, timeoutSearch, timeoutSched = 700*time.Millisecond, 500*time.Millisecond, 700*time.Millisecond
	t.Cleanup(func() { timeoutDefault, timeoutSearch, timeoutSched = d, s, sc })
}

func TestManifestValidation(t *testing.T) {
	e := newEnv(t)
	body := "export async function widget(ctx){ return {body:[]} }"
	ok := `{id:"demo", name:"Demo", version:"1.0.0", widgets:[{id:"w", name:"W"}]}`
	e.install(plug(ok, body))

	bad := map[string]string{
		`{name:"Demo", version:"1", widgets:[{id:"w",name:"W"}]}`:                                                                                       "manifest.id is required",
		`{id:"Bad_ID", name:"D", version:"1", widgets:[{id:"w",name:"W"}]}`:                                                                             "invalid",
		`{id:"../x", name:"D", version:"1", widgets:[{id:"w",name:"W"}]}`:                                                                               "invalid",
		`{id:"demo", version:"1", widgets:[{id:"w",name:"W"}]}`:                                                                                         "manifest.name is required",
		`{id:"demo", name:"D", widgets:[{id:"w",name:"W"}]}`:                                                                                            "manifest.version is required",
		`{id:"demo", name:"` + strings.Repeat("x", 41) + `", version:"1", widgets:[{id:"w",name:"W"}]}`:                                                 "too long",
		`{id:"demo", name:"D", version:"1"}`:                                                                                                            "no capability",
		`{id:"demo", name:"D", version:"1", homepage:"ftp://x", widgets:[{id:"w",name:"W"}]}`:                                                           "homepage",
		`{id:"demo", name:"D", version:"1", widgets:[{id:"w",name:"W",size:"xl"}]}`:                                                                     "size",
		`{id:"demo", name:"D", version:"1", widgets:[{id:"w",name:"W",refresh:5}]}`:                                                                     "at least 30",
		`{id:"demo", name:"D", version:"1", widgets:[{id:"w",name:"W"},{id:"w",name:"X"}]}`:                                                             "duplicated",
		`{id:"demo", name:"D", version:"1", permissions:{fetch:["http://x.com"]}, widgets:[{id:"w",name:"W"}]}`:                                         "permissions.fetch",
		`{id:"demo", name:"D", version:"1", permissions:{links:"all"}, widgets:[{id:"w",name:"W"}]}`:                                                    "links",
		`{id:"demo", name:"D", version:"1", schedules:[{id:"s",every:30}]}`:                                                                             "at least 60",
		`{id:"demo", name:"D", version:"1", wallpapers:[{id:"s",name:"S",refresh:60}]}`:                                                                 "at least 300",
		`{id:"demo", name:"D", version:"1", config:[{key:"1x",type:"string",label:"L"}], widgets:[{id:"w",name:"W"}]}`:                                  "key",
		`{id:"demo", name:"D", version:"1", config:[{key:"a",type:"weird",label:"L"}], widgets:[{id:"w",name:"W"}]}`:                                    "type",
		`{id:"demo", name:"D", version:"1", config:[{key:"a",type:"select",label:"L"}], widgets:[{id:"w",name:"W"}]}`:                                   "options",
		`{id:"demo", name:"D", version:"1", config:[{key:"a",type:"string",label:"L",allowFetch:true}], widgets:[{id:"w",name:"W"}]}`:                   "allowFetch",
		`{id:"demo", name:"D", version:"1", config:[{key:"a",type:"number",label:"L",default:"x"}], widgets:[{id:"w",name:"W"}]}`:                       "default",
		`{id:"demo", name:"D", version:"1", config:[{key:"a",type:"string",label:"L"},{key:"a",type:"string",label:"M"}], widgets:[{id:"w",name:"W"}]}`: "duplicated",
	}
	for m, want := range bad {
		err := e.installErr(plug(m, body))
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n  error %q does not contain %q", m, err, want)
		}
		if store.ErrKind(err) != store.KindInvalid {
			t.Errorf("%s: want an invalid-kind error, got %v", m, err)
		}
	}

	// declared capability without the exported function
	err := e.installErr(plug(`{id:"nofn", name:"N", version:"1", search:{name:"S"}}`, "export const x = 1"))
	if !strings.Contains(err.Error(), "search") {
		t.Errorf("missing export: %v", err)
	}
	// no manifest, syntax error, imports
	if err := e.installErr("export const x = 1"); !strings.Contains(err.Error(), "manifest") {
		t.Errorf("no manifest: %v", err)
	}
	if err := e.installErr("export const manifest = {"); !strings.Contains(err.Error(), "compile") {
		t.Errorf("syntax: %v", err)
	}
	if err := e.installErr(`import x from "./y.js"; export const manifest = {}`); !strings.Contains(err.Error(), "import") {
		t.Errorf("import: %v", err)
	}
	// both none and several sources
	if _, err := e.m.Install(context.Background(), InstallRequest{}); err == nil {
		t.Error("empty install request")
	}
	if _, err := e.m.Install(context.Background(), InstallRequest{Source: "x", Catalog: "y"}); err == nil {
		t.Error("two sources")
	}
	// defaults
	inf, _ := e.m.Info("demo", false)
	w := inf.Widgets[0]
	if w.Size != "m" || w.Refresh != 300 || inf.Admin != nil {
		t.Errorf("defaults/admin leak: %+v admin=%v", w, inf.Admin)
	}
}

func TestTypeScriptPlugin(t *testing.T) {
	e := newEnv(t)
	src := `interface V { body: any[] }
export const manifest = { id: "ts-demo", name: "TS", version: "1", widgets: [{ id: "w", name: "W" }] };
export async function widget(ctx: { lang: string }): Promise<V> { const n: number = 3; return { body: [{ type: "text", text: "n=" + n + ctx.lang }] }; }`
	if _, err := e.m.Install(context.Background(), InstallRequest{Source: src, Lang: "ts"}); err != nil {
		t.Fatal(err)
	}
	res, err := e.m.Test(context.Background(), "ts-demo", TestRequest{Hook: "widget", Lang: "zh"})
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	if s, _ := jsonString(res.Result); !strings.Contains(s, "n=3zh") {
		t.Fatalf("result %s", s)
	}
	if _, lang, _ := e.m.Source("ts-demo"); lang != "ts" {
		t.Fatalf("lang %q", lang)
	}
	// plain JS source in TS mode is fine, TS in JS mode is a compile error
	if _, err := e.m.Install(context.Background(), InstallRequest{Source: src, Lang: "js"}); err == nil {
		t.Fatal("TS source must not compile as js")
	}
}

func TestHooksAndContext(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{
  id:"hooks", name:"Hooks", version:"2.0.1",
  config:[{key:"name",type:"string",label:"Name",default:"anon"},{key:"tok",type:"secret",label:"Tok"}],
  widgets:[{id:"w",name:"W",config:[{key:"city",type:"string",label:"City",required:true},{key:"n",type:"number",label:"N",min:1,max:5,default:2}]}],
  search:{name:"Hooks", prefix:"hk"},
  schedules:[{id:"sync",every:60}],
  wallpapers:[{id:"pic",name:"Pic"}]
}`, `
export async function widget(ctx) {
  return { title: ctx.config.name, body: [{ type:"text", text: JSON.stringify({p:ctx.plugin, i:ctx.instance, w:ctx.widget, lang:ctx.lang, ver: genkan.version, nowOk: ctx.now > 1e12}) }] };
}
export async function search(ctx) { return [{ title: "q=" + ctx.query }] }
export async function schedule(ctx) { genkan.log("ran", ctx.schedule); return { done: ctx.schedule } }
export async function action(ctx) { return { toast: ctx.action + ":" + JSON.stringify(ctx.params), refresh: true } }
export async function wallpaper(ctx) { return { url: "data:image/png;base64,AAAA", title: ctx.source } }
`))
	bg := context.Background()
	res, err := e.m.Test(bg, "hooks", TestRequest{Hook: "widget", Config: map[string]any{"city": "Tokyo"}, Lang: "zh"})
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	s, _ := jsonString(res.Result)
	for _, want := range []string{`\"id\":\"hooks\"`, `\"version\":\"2.0.1\"`, `\"city\":\"Tokyo\"`, `\"n\":2`, `\"w\":\"w\"`, `\"lang\":\"zh\"`, `\"ver\":\"test-1.2\"`, `\"nowOk\":true`, `"title":"anon"`} {
		if !strings.Contains(s, want) {
			t.Errorf("widget result missing %s in %s", want, s)
		}
	}
	// search: prefix handling
	if g := e.m.Search(bg, "hk hello", "en"); len(g) != 1 || g[0].Results[0].Title != "q=hello" || g[0].Plugin != "hooks" || g[0].Name != "Hooks" {
		t.Errorf("prefixed search: %+v", g)
	}
	for _, q := range []string{"hello", "hk", "hk ", "hkx y"} {
		if g := e.m.Search(bg, q, "en"); len(g) != 0 {
			t.Errorf("query %q should not reach the plugin: %+v", q, g)
		}
	}
	// schedule + logs
	res, _ = e.m.Test(bg, "hooks", TestRequest{Hook: "schedule"})
	if !res.OK || len(res.Logs) != 1 || res.Logs[0].Msg != "ran sync" || res.Logs[0].Level != "info" {
		t.Errorf("schedule: %+v", res)
	}
	// action
	res, _ = e.m.Test(bg, "hooks", TestRequest{Hook: "action", Action: "go", Params: map[string]any{"a": 1.0}, Config: map[string]any{"city": "x"}})
	if !res.OK {
		t.Fatalf("action: %+v", res)
	}
	if r := res.Result.(ActionResult); r.Toast != `go:{"a":1}` || !r.Refresh {
		t.Errorf("action result %+v", r)
	}
	// wallpaper test returns the sanitized object without downloading
	res, _ = e.m.Test(bg, "hooks", TestRequest{Hook: "wallpaper"})
	if !res.OK || res.Result.(WallpaperResult).Title != "pic" {
		t.Errorf("wallpaper: %+v", res)
	}
	// unknown hook / target
	if _, err := e.m.Test(bg, "hooks", TestRequest{Hook: "nope"}); err == nil {
		t.Error("bad hook")
	}
	if _, err := e.m.Test(bg, "hooks", TestRequest{Hook: "widget", Target: "zzz"}); err == nil {
		t.Error("bad target")
	}
	if _, err := e.m.Test(bg, "missing", TestRequest{Hook: "widget"}); store.ErrKind(err) != store.KindNotFound {
		t.Errorf("missing plugin: %v", err)
	}
	// widget instance config validation: required, range, unknown keys dropped
	if _, err := e.m.AddWidget(WidgetAdd{Plugin: "hooks", Widget: "w"}); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("required: %v", err)
	}
	if _, err := e.m.AddWidget(WidgetAdd{Plugin: "hooks", Widget: "w", Config: map[string]any{"city": "x", "n": 9.0}}); err == nil {
		t.Error("range")
	}
	if _, err := e.m.AddWidget(WidgetAdd{Plugin: "hooks", Widget: "nope"}); store.ErrKind(err) != store.KindNotFound {
		t.Errorf("unknown widget: %v", err)
	}
	w := addWidget(t, e, "hooks", "w", map[string]any{"city": " Oslo ", "junk": 1.0})
	if w.Config["city"] != "Oslo" || w.Config["junk"] != nil {
		t.Errorf("config clean: %+v", w.Config)
	}
}

func TestAsyncAndErrors(t *testing.T) {
	shortTimeouts(t)
	e := newEnv(t)
	e.install(plug(`{id:"asy", name:"A", version:"1", widgets:[{id:"w",name:"W"}]}`, `
export async function widget(ctx) {
  const mode = genkan.storage.get("mode") || ctx.instance.config.mode;
  if (mode === "throw") throw new TypeError("boom");
  if (mode === "reject") return Promise.reject(new Error("rejected"));
  if (mode === "later") { await new Promise(r => setTimeout(r, 30)); return { body: [{ type: "text", text: "late" }] }; }
  if (mode === "spin") { for (;;) {} }
  if (mode === "hang") { await new Promise(() => {}); }
  if (mode === "sleep") { await new Promise(r => setTimeout(r, 60000)); }
  if (mode === "leftover") { setInterval(() => {}, 5); return { body: [{ type: "text", text: "ok" }] }; }
  if (mode === "mem") { const a = []; for (;;) a.push(new Array(100000).fill(1)); }
  if (mode === "eval") { return eval("1+1"); }
  return { body: [{ type: "text", text: "plain" }] };
}`))
	// the manifest has no config schema for mode: drive it through storage instead
	run := func(mode string) TestResult {
		t.Helper()
		// storage is shared by all runtimes of the plugin
		ent, _, _ := e.m.entryFor("asy")
		_ = ent.kv.set("mode", []byte(fmt.Sprintf("%q", mode)))
		res, err := e.m.Test(context.Background(), "asy", TestRequest{Hook: "widget"})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if r := run("plain"); !r.OK {
		t.Fatalf("plain: %+v", r)
	}
	if r := run("later"); !r.OK || !strings.Contains(fmt.Sprint(r.Result), "late") {
		t.Fatalf("later: %+v", r)
	}
	if r := run("throw"); r.OK || r.Error != "TypeError: boom" {
		t.Fatalf("throw: %+v", r)
	}
	if r := run("reject"); r.OK || !strings.Contains(r.Error, "rejected") {
		t.Fatalf("reject: %+v", r)
	}
	if r := run("leftover"); !r.OK {
		t.Fatalf("leftover timers must not fail the call: %+v", r)
	}
	t0 := time.Now()
	if r := run("spin"); r.OK || !strings.Contains(r.Error, "timed out") {
		t.Fatalf("spin: %+v", r)
	}
	if d := time.Since(t0); d > 3*time.Second {
		t.Fatalf("interrupt took %s", d)
	}
	if r := run("hang"); r.OK || !strings.Contains(r.Error, "never settled") {
		t.Fatalf("hang: %+v", r)
	}
	if r := run("sleep"); r.OK || !strings.Contains(r.Error, "timed out") {
		t.Fatalf("sleep: %+v", r)
	}
	if r := run("mem"); r.OK || !strings.Contains(r.Error, "memory limit") {
		t.Fatalf("mem: %+v", r)
	}
	if r := run("eval"); r.OK {
		t.Fatalf("eval must be disabled: %+v", r)
	}
	// the plugin still works after all of that, and failures were logged
	if r := run("plain"); !r.OK {
		t.Fatalf("recovery: %+v", r)
	}
	logs, _ := e.m.Logs("asy")
	n := 0
	for _, l := range logs {
		if l.Level == "error" {
			n++
		}
	}
	if n < 5 {
		t.Errorf("expected error log lines, got %d: %+v", n, logs)
	}
	if inf, _ := e.m.Info("asy", true); inf.LastError != "" {
		t.Errorf("last_error should clear after a success: %q", inf.LastError)
	}
}

func TestLastErrorAndPooling(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"pool", name:"P", version:"1", search:{name:"P"}}`, `
let calls = 0;
export async function search(ctx) { calls++; if (ctx.query === "bad") throw new Error("nope"); return [{ title: "calls=" + calls }]; }`))
	bg := context.Background()
	g := e.m.Search(bg, "aa", "en")
	g2 := e.m.Search(bg, "ab", "en")
	if g[0].Results[0].Title != "calls=1" || g2[0].Results[0].Title != "calls=2" {
		t.Fatalf("runtime should be reused: %+v %+v", g, g2)
	}
	ent, _, _ := e.m.entryFor("pool")
	if ent.pool.idleCount() != 1 {
		t.Fatalf("idle %d", ent.pool.idleCount())
	}
	e.m.Search(bg, "bad", "en")
	if inf, _ := e.m.Info("pool", true); !strings.Contains(inf.LastError, "nope") {
		t.Fatalf("last_error: %q", inf.LastError)
	}
	// failed runs are not pooled: the next call still works from the pool/new runtime
	if g := e.m.Search(bg, "ok", "en"); len(g) != 1 {
		t.Fatalf("after failure: %+v", g)
	}
	// update drops the pool and compiled module
	e.install(plug(`{id:"pool", name:"P2", version:"2", search:{name:"P"}}`, `export async function search(ctx) { return [{ title: "v2" }] }`))
	if ent2, _, _ := e.m.entryFor("pool"); ent2 == ent || ent.pool.idleCount() != 0 {
		t.Fatal("update must replace the entry and drop its pool")
	}
	if g := e.m.Search(bg, "zz", "en"); g[0].Results[0].Title != "v2" {
		t.Fatalf("updated source not used: %+v", g)
	}
	// disabling stops search
	if _, err := e.m.Patch("pool", PatchRequest{Enabled: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if g := e.m.Search(bg, "zz", "en"); len(g) != 0 {
		t.Fatalf("disabled plugin searched: %+v", g)
	}
}

func ptr[T any](v T) *T { return &v }

func TestSingleflightWidgetData(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"sf", name:"S", version:"1", widgets:[{id:"w",name:"W",refresh:60}]}`, `
export async function widget(ctx) {
  const n = (genkan.storage.get("n") || 0) + 1; genkan.storage.set("n", n);
  await new Promise(r => setTimeout(r, 150));
  return { body: [{ type: "text", text: "n=" + n }] };
}`))
	w := addWidget(t, e, "sf", "w", nil)
	var wg sync.WaitGroup
	outs := make([]WidgetData, 6)
	for i := range outs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := e.m.Data(context.Background(), w.ID, "en", false)
			if err != nil {
				t.Error(err)
			}
			outs[i] = d
		}()
	}
	wg.Wait()
	for _, o := range outs {
		if o.View.Body[0]["text"] != "n=1" {
			t.Fatalf("concurrent calls must share one execution: %+v", o.View.Body)
		}
	}
	// cached afterwards, forced refresh bypasses the cache
	d, _ := e.m.Data(context.Background(), w.ID, "en", false)
	if d.View.Body[0]["text"] != "n=1" || d.NextAt-d.UpdatedAt != 60000 {
		t.Fatalf("cache: %+v", d)
	}
	d, _ = e.m.Data(context.Background(), w.ID, "en", true)
	if d.View.Body[0]["text"] != "n=2" {
		t.Fatalf("force: %+v", d.View.Body)
	}
	// a config change invalidates the cache (here: plugin config is not in the schema, widget config change)
	if _, err := e.m.UpdateWidget(w.ID, WidgetPatch{Config: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	// errors are cached briefly, not hammered
	e.install(plug(`{id:"sf", name:"S", version:"1", widgets:[{id:"w",name:"W",refresh:60}]}`, `
export async function widget(ctx) { const n = (genkan.storage.get("e") || 0) + 1; genkan.storage.set("e", n); throw new Error("bad" + n) }`))
	_, err1 := e.m.Data(context.Background(), w.ID, "en", false)
	_, err2 := e.m.Data(context.Background(), w.ID, "en", false)
	if err1 == nil || err1.Error() != err2.Error() || !strings.Contains(err1.Error(), "bad1") {
		t.Fatalf("error cache: %v / %v", err1, err2)
	}
	if _, err := e.m.Data(context.Background(), w.ID, "en", true); err == nil || !strings.Contains(err.Error(), "bad2") {
		t.Fatalf("force after error: %v", err)
	}
}

func TestDisabledAndUnknown(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"dis", name:"D", version:"1", widgets:[{id:"w",name:"W"}], wallpapers:[{id:"p",name:"P"}]}`, `
export async function widget() { return {body:[]} }
export async function wallpaper() { return {url:"data:image/png;base64,AA"} }`))
	w := addWidget(t, e, "dis", "w", nil)
	bg := context.Background()
	if _, err := e.m.Patch("dis", PatchRequest{Enabled: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Data(bg, w.ID, "en", false); store.ErrKind(err) != store.KindConflict {
		t.Errorf("disabled data: %v", err)
	}
	if _, err := e.m.Wallpaper(bg, "dis", "p", "en"); store.ErrKind(err) != store.KindConflict {
		t.Errorf("disabled wallpaper: %v", err)
	}
	if _, err := e.m.Data(bg, "nope", "en", false); store.ErrKind(err) != store.KindNotFound {
		t.Errorf("unknown widget: %v", err)
	}
	if _, err := e.m.Wallpaper(bg, "ghost", "p", "en"); store.ErrKind(err) != store.KindNotFound {
		t.Errorf("unknown plugin: %v", err)
	}
	if _, err := e.m.Wallpaper(bg, "dis", "zzz", "en"); err == nil {
		t.Error("unknown source on disabled plugin")
	}
}

// Fetch ----------------------------------------------------------------------------------------

func TestFetchAllowList(t *testing.T) {
	var hits atomic.Int32
	var gotHdr atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotHdr.Store(r.Header.Get("X-Test"))
		fmt.Fprint(w, `{"ok":true,"path":"`+r.URL.Path+`"}`)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://") // 127.0.0.1:port
	e := newEnv(t)
	code := `
export async function widget(ctx) {
  const url = ctx.instance.config.url || ctx.config.url;
  try {
    const r = await fetch(url, { headers: { "X-Test": "yes" } });
    const j = await r.json();
    return { body: [{ type: "text", text: "got " + j.path }] };
  } catch (e) { return { body: [], error: "ERR " + e.message + " " + (e.cause && e.cause.message) } }
}`
	// 1. default deny
	e.install(plug(`{id:"deny", name:"D", version:"1", widgets:[{id:"w",name:"W",config:[{key:"url",type:"url",label:"U"}]}]}`, code))
	res, _ := e.m.Test(context.Background(), "deny", TestRequest{Hook: "widget", Config: map[string]any{"url": srv.URL + "/a"}})
	if !res.OK || !strings.Contains(fmt.Sprint(res.Result), "ERR") || hits.Load() != 0 {
		t.Fatalf("default must deny: %+v hits=%d", res, hits.Load())
	}
	logs, _ := e.m.Logs("deny")
	if !strings.Contains(fmt.Sprint(logs), "fetch blocked") {
		t.Errorf("blocked fetch should be logged: %+v", logs)
	}
	// 2. manifest host without port allows any port; other host denied
	e.install(plug(`{id:"allow", name:"A", version:"1", permissions:{fetch:["127.0.0.1"]}, widgets:[{id:"w",name:"W",config:[{key:"url",type:"url",label:"U"}]}]}`, code))
	res, _ = e.m.Test(context.Background(), "allow", TestRequest{Hook: "widget", Config: map[string]any{"url": srv.URL + "/ok"}})
	if s := fmt.Sprint(res.Result); !res.OK || !strings.Contains(s, "got /ok") {
		t.Fatalf("allowed host: %+v", res)
	}
	if gotHdr.Load() != "yes" {
		t.Error("headers lost")
	}
	hits.Store(0)
	res, _ = e.m.Test(context.Background(), "allow", TestRequest{Hook: "widget", Config: map[string]any{"url": "http://localhost:" + strings.Split(host, ":")[1] + "/no"}})
	if !strings.Contains(fmt.Sprint(res.Result), "ERR") || hits.Load() != 0 {
		t.Fatalf("other host must be denied: %+v", res)
	}
	// 3. a port-restricted manifest entry
	e.install(plug(`{id:"port", name:"P", version:"1", permissions:{fetch:["127.0.0.1:1"]}, widgets:[{id:"w",name:"W",config:[{key:"url",type:"url",label:"U"}]}]}`, code))
	res, _ = e.m.Test(context.Background(), "port", TestRequest{Hook: "widget", Config: map[string]any{"url": srv.URL + "/p"}})
	if !strings.Contains(fmt.Sprint(res.Result), "ERR") {
		t.Fatalf("wrong port must be denied: %+v", res)
	}
	// 4. allowFetch url field (instance level) opens exactly that host:port
	e.install(plug(`{id:"af", name:"AF", version:"1", widgets:[{id:"w",name:"W",config:[{key:"url",type:"url",label:"U",allowFetch:true}]}]}`, code))
	res, _ = e.m.Test(context.Background(), "af", TestRequest{Hook: "widget", Config: map[string]any{"url": srv.URL + "/af"}})
	if !strings.Contains(fmt.Sprint(res.Result), "got /af") {
		t.Fatalf("allowFetch: %+v", res)
	}
	// 5. allowFetch at plugin level
	e.install(plug(`{id:"afp", name:"AFP", version:"1", config:[{key:"url",type:"url",label:"U",allowFetch:true}], widgets:[{id:"w",name:"W"}]}`, code))
	if _, err := e.m.Patch("afp", PatchRequest{Config: map[string]any{"url": srv.URL + "/afp"}}); err != nil {
		t.Fatal(err)
	}
	res, _ = e.m.Test(context.Background(), "afp", TestRequest{Hook: "widget"})
	if !strings.Contains(fmt.Sprint(res.Result), "got /afp") {
		t.Fatalf("plugin allowFetch: %+v", res)
	}
	// 6. wildcard
	e.install(plug(`{id:"star", name:"S", version:"1", permissions:{fetch:["*"]}, widgets:[{id:"w",name:"W",config:[{key:"url",type:"url",label:"U"}]}]}`, code))
	res, _ = e.m.Test(context.Background(), "star", TestRequest{Hook: "widget", Config: map[string]any{"url": srv.URL + "/star"}})
	if !strings.Contains(fmt.Sprint(res.Result), "got /star") {
		t.Fatalf("wildcard: %+v", res)
	}
}

func TestAllowListMatching(t *testing.T) {
	a := newAllowList([]string{"api.example.com", "*.bing.net", "10.0.0.5:8080"})
	cases := []struct {
		host, port string
		want       bool
	}{
		{"api.example.com", "443", true}, {"API.example.com", "80", true}, {"example.com", "443", false},
		{"x.bing.net", "443", true}, {"a.b.bing.net", "443", true}, {"bing.net", "443", false}, {"evilbing.net", "443", false},
		{"10.0.0.5", "8080", true}, {"10.0.0.5", "80", false}, {"api.example.com.evil.org", "443", false},
	}
	for _, c := range cases {
		if got := a.match(c.host, c.port); got != c.want {
			t.Errorf("%s:%s = %v", c.host, c.port, got)
		}
	}
	if !newAllowList([]string{"*"}).match("anything", "1") || !newAllowList([]string{"*"}).hasWildcard() {
		t.Error("wildcard")
	}
	var none *allowList
	if none.match("a", "1") {
		t.Error("nil list must deny")
	}
}

// Storage / host API --------------------------------------------------------------------------

func TestStorage(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"kv", name:"KV", version:"1", search:{name:"KV"}}`, `
export async function search(ctx) {
  const q = ctx.query;
  if (q === "set") { genkan.storage.set("a", {x:[1,2,{y:"z"}]}); genkan.storage.set("b", "str"); return [{title: "ok"}] }
  if (q === "get") return [{title: JSON.stringify(genkan.storage.get("a")) + "|" + genkan.storage.get("b") + "|" + genkan.storage.get("nope") + "|" + genkan.storage.keys().join(",")}]
  if (q === "del") { genkan.storage.delete("a"); return [{title: genkan.storage.keys().join(",")}] }
  if (q === "big") { try { genkan.storage.set("big", "x".repeat(300*1024)); return [{title:"stored"}] } catch (e) { return [{title: "ERR " + e.message}] } }
  if (q === "fill") { genkan.storage.set("f", "y".repeat(200*1024)); try { genkan.storage.set("g", "z".repeat(100*1024)); return [{title:"stored"}] } catch (e) { return [{title: "ERR " + e.message}] } }
  if (q === "fn") { try { genkan.storage.set("fn", () => 1); return [{title:"stored"}] } catch (e) { return [{title: "ERR " + e.message}] } }
  return []
}`))
	title := func(q string) string {
		g := e.m.Search(context.Background(), q, "en")
		if len(g) == 0 {
			t.Fatalf("no result for %s", q)
		}
		return g[0].Results[0].Title
	}
	title("set")
	if got := title("get"); got != `{"x":[1,2,{"y":"z"}]}|str|undefined|a,b` {
		t.Fatalf("get: %s", got)
	}
	if got := title("del"); got != "b" {
		t.Fatalf("del: %s", got)
	}
	if got := title("big"); !strings.Contains(got, "ERR") || !strings.Contains(got, "256") {
		t.Fatalf("cap per value: %s", got)
	}
	if got := title("fill"); !strings.Contains(got, "ERR") {
		t.Fatalf("total cap: %s", got)
	}
	if got := title("fn"); !strings.Contains(got, "ERR") {
		t.Fatalf("function value: %s", got)
	}
	// persisted after close, loaded by a new manager
	ent, _, _ := e.m.entryFor("kv")
	if err := ent.kv.flush(); err != nil {
		t.Fatal(err)
	}
	m2 := New(Config{Store: e.st, DataDir: e.dir})
	defer m2.Close()
	ent2, _, _ := m2.entryFor("kv")
	if v, ok := ent2.kv.get("b"); !ok || string(v) != `"str"` {
		t.Fatalf("not persisted: %s %v", v, ok)
	}
}

func TestLinksAPIPermissions(t *testing.T) {
	e := newEnv(t)
	ws := e.st.Snapshot().Workspaces[0].ID
	hook := `
export async function search(ctx) {
  try {
    if (ctx.query === "list") return [{title: "n=" + genkan.links.list().length + " ws=" + genkan.workspaces.list().length + " cat=" + genkan.categories.list().length}];
    if (ctx.query === "create") { const l = genkan.links.create({workspace_id: ctx.config.ws, title: "From plugin", url: "http://p.lan"}); return [{title: "created " + l.title}] }
    if (ctx.query === "update") { const l = genkan.links.update(genkan.links.list()[0].id, {title: "Renamed"}); return [{title: l.title}] }
  } catch (e) { return [{title: "ERR " + e.message}] }
  return []
}`
	title := func(id, q string) string {
		g := e.m.Search(context.Background(), q, "en")
		for _, x := range g {
			if x.Plugin == id {
				return x.Results[0].Title
			}
		}
		return "<none>"
	}
	for _, perm := range []string{"", "read", "write"} {
		id := "links-" + perm
		if perm == "" {
			id = "links-none"
		}
		pm := ""
		if perm != "" {
			pm = `permissions:{links:"` + perm + `"},`
		}
		e.install(plug(`{id:"`+id+`", name:"L", version:"1", `+pm+` config:[{key:"ws",type:"string",label:"ws"}], search:{name:"`+id+`", prefix:"`+perm+`x"}}`, hook))
		if _, err := e.m.Patch(id, PatchRequest{Config: map[string]any{"ws": ws}}); err != nil {
			t.Fatal(err)
		}
	}
	// prefix trick: each plugin has its own prefix so we address them one by one
	got := func(prefix, id, q string) string { return title(id, prefix+" "+q) }
	if r := got("x", "links-none", "list"); !strings.Contains(r, "ERR") || !strings.Contains(r, "permissions.links") {
		t.Errorf("no permission list: %s", r)
	}
	if r := got("readx", "links-read", "list"); !strings.HasPrefix(r, "n=0 ws=1") {
		t.Errorf("read list: %s", r)
	}
	if r := got("readx", "links-read", "create"); !strings.Contains(r, "ERR") {
		t.Errorf("read must not create: %s", r)
	}
	var created []store.Link
	e.m.cfg.OnLinksCreated = func(l []store.Link) { created = l }
	if r := got("writex", "links-write", "create"); r != "created From plugin" {
		t.Errorf("write create: %s", r)
	}
	if len(created) != 1 || len(e.st.Snapshot().Links) != 1 {
		t.Errorf("link not created: %v", created)
	}
	if r := got("writex", "links-write", "update"); r != "Renamed" {
		t.Errorf("write update: %s", r)
	}
}
