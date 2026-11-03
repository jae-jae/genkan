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
