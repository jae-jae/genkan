package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jae-jae/genkan/internal/plugin"
	"github.com/jae-jae/genkan/internal/store"
	"github.com/jae-jae/genkan/plugins"
)

func newPluginSrv(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s, st, _ := newSrv(t)
	m := plugin.New(plugin.Config{Store: st, DataDir: st.Dir(), Version: "test", Catalog: plugins.Catalog()})
	t.Cleanup(m.Close)
	s.Plugins = m
	return s, st
}

func structured(t *testing.T, res map[string]any) map[string]any {
	t.Helper()
	sc, _ := res["structuredContent"].(map[string]any)
	if sc == nil {
		t.Fatalf("no structuredContent: %v", res)
	}
	return sc
}

const mcpPlugin = `export const manifest = {
  id: "mcp-demo", name: "Demo", version: "1",
  config: [{ key: "token", type: "secret", label: "Token" }, { key: "city", type: "string", label: "City" }, { key: "unit", type: "string", label: "Unit" }],
  widgets: [{ id: "w", name: "W", config: [{ key: "label", type: "string", label: "Label", required: true }, { key: "pw", type: "secret", label: "PW" }] }],
};
export async function widget(ctx) { return { body: [{ type: "text", text: ctx.instance.config.label + "/" + ctx.config.city }] } }
`

func TestPluginTools(t *testing.T) {
	s, st := newPluginSrv(t)
	// catalog + install
	res := call(t, s, "list_plugins", nil)
	cat := structured(t, res)["catalog"].([]any)
	if len(cat) < 10 {
		t.Fatalf("catalog: %v", cat)
	}
	if r := call(t, s, "install_plugin", map[string]any{"catalog": "calculator"}); r["isError"] != nil {
		t.Fatalf("install catalog: %v", r)
	}
	r := call(t, s, "install_plugin", map[string]any{"source": mcpPlugin})
	if r["isError"] != nil || structured(t, r)["plugin"].(map[string]any)["id"] != "mcp-demo" {
		t.Fatalf("install: %v", r)
	}
	// errors are tool errors with a useful message
	r = call(t, s, "install_plugin", map[string]any{"source": "export const manifest = {id: 'zz'}"})
	if r["isError"] != true || !strings.Contains(r["content"].([]any)[0].(map[string]any)["text"].(string), "manifest") {
		t.Fatalf("invalid install: %v", r)
	}
	if r := call(t, s, "install_plugin", map[string]any{}); r["isError"] != true {
		t.Fatalf("empty install: %v", r)
	}
	// update_plugin merges config; secrets are never echoed
	call(t, s, "update_plugin", map[string]any{"id": "mcp-demo", "config": map[string]any{"city": "Oslo", "token": "TOPSECRET"}})
	r = call(t, s, "update_plugin", map[string]any{"id": "mcp-demo", "config": map[string]any{"unit": "c"}})
	p := structured(t, r)["plugin"].(map[string]any)
	cfg := p["config"].(map[string]any)
	if cfg["city"] != "Oslo" || cfg["unit"] != "c" || cfg["token"] != "" {
		t.Fatalf("merge: %v", cfg)
	}
	r = call(t, s, "update_plugin", map[string]any{"id": "mcp-demo", "config": map[string]any{"unit": nil}})
	if cfg := structured(t, r)["plugin"].(map[string]any)["config"].(map[string]any); cfg["unit"] != nil || cfg["city"] != "Oslo" {
		t.Fatalf("null removes: %v", cfg)
	}
	// widgets
	if r := call(t, s, "add_widget", map[string]any{"plugin": "mcp-demo", "widget": "w"}); r["isError"] != true {
		t.Fatalf("required label: %v", r)
	}
	r = call(t, s, "add_widget", map[string]any{"plugin": "mcp-demo", "widget": "w", "config": map[string]any{"label": "L", "pw": "WSECRET"}})
	w := structured(t, r)["widget"].(map[string]any)
	wid := w["id"].(string)
	call(t, s, "update_widget", map[string]any{"id": wid, "config": map[string]any{"label": "L2"}})
	lw := structured(t, call(t, s, "list_widgets", nil))["widgets"].([]any)
	if len(lw) != 1 || lw[0].(map[string]any)["config"].(map[string]any)["label"] != "L2" {
		t.Fatalf("list_widgets: %v", lw)
	}
	if got, _ := st.GetWidget(wid); got.Config["pw"] != "WSECRET" {
		t.Fatalf("merge must keep the secret: %v", got.Config)
	}
	// test_plugin
	r = call(t, s, "test_plugin", map[string]any{"id": "mcp-demo", "hook": "widget", "instance": wid})
	sc := structured(t, r)
	if r["isError"] != nil || sc["ok"] != true || !strings.Contains(jsonOf(sc["result"]), "L2/Oslo") {
		t.Fatalf("test_plugin: %v", r)
	}
	r = call(t, s, "test_plugin", map[string]any{"id": "calculator", "hook": "search", "query": "6*7"})
	if !strings.Contains(jsonOf(structured(t, r)["result"]), `"copy":"42"`) {
		t.Fatalf("calculator: %v", r)
	}
	r = call(t, s, "test_plugin", map[string]any{"id": "mcp-demo", "hook": "widget", "config": map[string]any{"label": "x"}, "target": "nope"})
	if r["isError"] != true {
		t.Fatalf("bad target: %v", r)
	}
	if r := call(t, s, "plugin_logs", map[string]any{"id": "mcp-demo"}); r["isError"] != nil || structured(t, r)["logs"] == nil {
		t.Fatalf("logs: %v", r)
	}
	// nothing the tools returned may contain a secret
	all := jsonOf(call(t, s, "list_plugins", nil)) + jsonOf(call(t, s, "list_widgets", nil)) + jsonOf(call(t, s, "plugin_logs", map[string]any{"id": "mcp-demo"}))
	if strings.Contains(all, "TOPSECRET") || strings.Contains(all, "WSECRET") {
		t.Fatalf("secret leaked: %s", all)
	}
	// remove + uninstall
	if r := call(t, s, "remove_widget", map[string]any{"id": wid}); r["isError"] != nil {
		t.Fatalf("remove: %v", r)
	}
	if r := call(t, s, "remove_widget", map[string]any{"id": wid}); r["isError"] != true {
		t.Fatalf("remove twice: %v", r)
	}
	if r := call(t, s, "uninstall_plugin", map[string]any{"id": "mcp-demo"}); r["isError"] != nil {
		t.Fatalf("uninstall: %v", r)
	}
	if _, ok := st.GetPlugin("mcp-demo"); ok {
		t.Fatal("still installed")
	}
	// workspace wallpapers accept the plugin form through update_workspace (existing tool)
	if r := call(t, s, "update_workspace", map[string]any{"id": st.Snapshot().Workspaces[0].ID, "wallpaper": "plugin:calculator/x"}); r["isError"] != nil {
		t.Fatalf("plugin wallpaper: %v", r)
	}
}

func jsonOf(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestPluginToolsWithoutManager(t *testing.T) {
	s, _, _ := newSrv(t)
	if r := call(t, s, "list_plugins", nil); r["isError"] != true {
		t.Fatalf("%v", r)
	}
}

func TestPluginSourceTools(t *testing.T) {
	s, st := newPluginSrv(t)
	// preview: nothing installed, errors are tool errors carrying the details
	r := call(t, s, "preview_plugin", map[string]any{"source": mcpPlugin})
	sc := structured(t, r)
	if r["isError"] != nil || sc["plugin"].(map[string]any)["id"] != "mcp-demo" || sc["installed"] != false || sc["source"] != mcpPlugin {
		t.Fatalf("preview: %v", r)
	}
	if len(st.Plugins()) != 0 {
		t.Fatal("preview installed")
	}
	r = call(t, s, "preview_plugin", map[string]any{"source": "export const manifest = {\n id: \"abc\",\n name: ;\n};"})
	if r["isError"] != true || !strings.Contains(jsonOf(structured(t, r)), "3:") {
		t.Fatalf("preview error: %v", r)
	}
	if r := call(t, s, "preview_plugin", map[string]any{}); r["isError"] != true {
		t.Fatalf("empty: %v", r)
	}
	if r := call(t, s, "preview_plugin", map[string]any{"catalog": "calculator"}); r["isError"] != nil {
		t.Fatalf("catalog: %v", r)
	}

	call(t, s, "install_plugin", map[string]any{"source": mcpPlugin, "enable": false})
	r = call(t, s, "get_plugin_source", map[string]any{"id": "mcp-demo"})
	if sc := structured(t, r); sc["source"] != mcpPlugin || sc["lang"] != "js" {
		t.Fatalf("get: %v", r)
	}
	if r := call(t, s, "get_plugin_source", map[string]any{"id": "nope"}); r["isError"] != true {
		t.Fatalf("get missing: %v", r)
	}
	v2 := strings.Replace(mcpPlugin, `version: "1"`, `version: "9"`, 1)
	r = call(t, s, "update_plugin_source", map[string]any{"id": "mcp-demo", "source": v2})
	p := structured(t, r)["plugin"].(map[string]any)
	if r["isError"] != nil || p["version"] != "9" || p["enabled"] != false {
		t.Fatalf("update: %v", r)
	}
	if r := call(t, s, "update_plugin_source", map[string]any{"id": "mcp-demo", "source": strings.Replace(v2, `id: "mcp-demo"`, `id: "other"`, 1)}); r["isError"] != true {
		t.Fatalf("id change: %v", r)
	}
	if r := call(t, s, "update_plugin_source", map[string]any{"id": "mcp-demo"}); r["isError"] != true {
		t.Fatalf("missing source: %v", r)
	}
}

func TestWidgetLayoutTools(t *testing.T) {
	s, st := newPluginSrv(t)
	call(t, s, "install_plugin", map[string]any{"source": mcpPlugin})
	r := call(t, s, "add_widget", map[string]any{"plugin": "mcp-demo", "widget": "w", "config": map[string]any{"label": "L"}, "w": 4, "h": 250})
	w := structured(t, r)["widget"].(map[string]any)
	if w["w"] != 4.0 || w["h"] != 250.0 {
		t.Fatalf("add: %v", w)
	}
	id := w["id"].(string)
	r = call(t, s, "update_widget", map[string]any{"id": id, "w": 9, "h": 1})
	w = structured(t, r)["widget"].(map[string]any)
	if w["w"] != 6.0 || w["h"] != 80.0 {
		t.Fatalf("clamp: %v", w)
	}
	call(t, s, "update_widget", map[string]any{"id": id, "config": map[string]any{"label": "L2"}})
	lw := structured(t, call(t, s, "list_widgets", nil))["widgets"].([]any)[0].(map[string]any)
	if lw["w"] != 6.0 || lw["h"] != 80.0 {
		t.Fatalf("list: %v", lw)
	}
	if got, _ := st.GetWidget(id); got.W != 6 || got.H != 80 {
		t.Fatalf("%+v", got)
	}
}

// Localized manifest strings reach MCP clients as they are (plain string or { lang: text }).
func TestPluginToolsLocalizedStrings(t *testing.T) {
	s, _ := newPluginSrv(t)
	src := `export const messages = { en: { n: "Loc", c: "City" }, zh: { n: "本地化", c: "城市" } };
export const manifest = { id: "mcp-loc", name: t("n"), version: "1", description: "plain",
  config: [{ key: "city", type: "string", label: t("c") }], widgets: [{ id: "w", name: { en: "W", zh: "组件" } }] };
export async function widget(ctx) { return { body: [{ type: "text", text: ctx.t("n") }] } }`
	r := call(t, s, "install_plugin", map[string]any{"source": src})
	if r["isError"] != nil {
		t.Fatalf("install: %v", r)
	}
	p := structured(t, r)["plugin"].(map[string]any)
	name, _ := p["name"].(map[string]any)
	if name["en"] != "Loc" || name["zh"] != "本地化" || p["description"] != "plain" {
		t.Fatalf("name/description: %v %v", p["name"], p["description"])
	}
	field := p["config_schema"].([]any)[0].(map[string]any)["label"].(map[string]any)
	wname := p["widgets"].([]any)[0].(map[string]any)["name"].(map[string]any)
	if field["zh"] != "城市" || wname["zh"] != "组件" {
		t.Fatalf("label/widget: %v %v", field, wname)
	}
	// the catalog entries are objects too
	found := false
	for _, c := range structured(t, call(t, s, "list_plugins", nil))["catalog"].([]any) {
		if c := c.(map[string]any); c["id"] == "weather" {
			n, ok := c["name"].(map[string]any)
			found = ok && n["zh"] == "天气"
		}
	}
	if !found {
		t.Error("catalog weather name is not a localized object")
	}
	// test_plugin runs with ctx.t (default language en)
	tr := call(t, s, "test_plugin", map[string]any{"id": "mcp-loc", "hook": "widget"})
	if b, _ := json.Marshal(tr); !strings.Contains(string(b), "Loc") {
		t.Fatalf("test: %s", b)
	}
}

const mcpStoragePlugin = `export const manifest = { id: "mcp-kv", name: "KV", version: "1", widgets: [{ id: "w", name: "W" }] };
export async function widget(ctx) {
  ctx.instance.storage.set("c", 3);
  genkan.storage.set("plain", "p");
  return { body: [{ type: "text", text: "ok" }] };
}
`

func TestPluginStorageTools(t *testing.T) {
	s, _ := newPluginSrv(t)
	if r := call(t, s, "install_plugin", map[string]any{"source": mcpStoragePlugin}); r["isError"] != nil {
		t.Fatalf("install: %v", r)
	}
	w := structured(t, call(t, s, "add_widget", map[string]any{"plugin": "mcp-kv", "widget": "w"}))["widget"].(map[string]any)
	wid := w["id"].(string)
	if r := call(t, s, "test_plugin", map[string]any{"id": "mcp-kv", "hook": "widget", "instance": wid}); r["isError"] != nil {
		t.Fatalf("run: %v", r)
	}
	st := structured(t, call(t, s, "plugin_storage", map[string]any{"id": "mcp-kv"}))
	entries := st["entries"].([]any)
	if st["limit"] != float64(256<<10) || len(entries) != 2 || entries[1].(map[string]any)["instance"] != wid {
		t.Fatalf("storage: %v", st)
	}
	if r := call(t, s, "clear_plugin_storage", map[string]any{"id": "mcp-kv", "key": "c", "instance": wid}); r["isError"] != nil {
		t.Fatalf("clear key: %v", r)
	}
	if n := len(structured(t, call(t, s, "plugin_storage", map[string]any{"id": "mcp-kv"}))["entries"].([]any)); n != 1 {
		t.Fatalf("after clearing one key: %d entries", n)
	}
	if r := call(t, s, "clear_plugin_storage", map[string]any{"id": "mcp-kv"}); r["isError"] != nil {
		t.Fatalf("clear all: %v", r)
	}
	if n := len(structured(t, call(t, s, "plugin_storage", map[string]any{"id": "mcp-kv"}))["entries"].([]any)); n != 0 {
		t.Fatalf("after clear: %d entries", n)
	}
	if r := call(t, s, "plugin_storage", map[string]any{"id": "nope"}); r["isError"] != true {
		t.Fatalf("unknown plugin: %v", r)
	}
	// removing the widget (MCP) clears its instance storage
	call(t, s, "test_plugin", map[string]any{"id": "mcp-kv", "hook": "widget", "instance": wid})
	call(t, s, "remove_widget", map[string]any{"id": wid})
	for _, e := range structured(t, call(t, s, "plugin_storage", map[string]any{"id": "mcp-kv"}))["entries"].([]any) {
		if e.(map[string]any)["instance"] != nil {
			t.Fatalf("instance storage survived remove_widget: %v", e)
		}
	}
}
