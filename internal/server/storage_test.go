package server

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const storageDemo = `export const manifest = {
  id: "kvdemo", name: "KV", version: "1.0.0",
  widgets: [{ id: "w", name: "W", size: "s", refresh: 60 }],
};
export async function widget(ctx) {
  ctx.instance.storage.set("count", 7);
  genkan.storage.set("plain", { hello: "world" });
  return { body: [{ type: "text", text: "ok" }] };
}
`

type storageResp struct {
	Size    int `json:"size"`
	Limit   int `json:"limit"`
	Entries []struct {
		Key      string          `json:"key"`
		Size     int             `json:"size"`
		Value    json.RawMessage `json:"value"`
		Instance string          `json:"instance"`
	} `json:"entries"`
}

func TestPluginStorageREST(t *testing.T) {
	e := setup(t)
	a := e.admin()
	a.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": storageDemo}), nil)
	var w map[string]any
	a.mustJSON("POST", "/api/widgets", `{"plugin":"kvdemo","widget":"w"}`, &w)
	wid := w["id"].(string)
	if code, b := a.do("GET", "/api/widgets/"+wid+"/data", ""); code != 200 {
		t.Fatalf("data: %d %s", code, b)
	}

	var st storageResp
	a.mustJSON("GET", "/api/plugins/kvdemo/storage", "", &st)
	if st.Limit != 256<<10 || len(st.Entries) != 2 || st.Size <= 0 {
		t.Fatalf("storage: %+v", st)
	}
	if st.Entries[0].Key != "plain" || st.Entries[0].Instance != "" || string(st.Entries[0].Value) != `{"hello":"world"}` {
		t.Fatalf("plugin key: %+v", st.Entries[0])
	}
	if st.Entries[1].Key != "count" || st.Entries[1].Instance != wid || string(st.Entries[1].Value) != "7" {
		t.Fatalf("instance key: %+v", st.Entries[1])
	}
	if code, _ := a.do("GET", "/api/plugins/nope/storage", ""); code != 404 {
		t.Errorf("unknown plugin: %d", code)
	}

	// delete one key (instance scoped needs &instance=), then everything
	if code, _ := a.do("DELETE", "/api/plugins/kvdemo/storage?key=count", ""); code != 200 {
		t.Fatalf("delete missing instance: %d", code)
	}
	a.mustJSON("GET", "/api/plugins/kvdemo/storage", "", &st)
	if len(st.Entries) != 2 { // the plugin-level key "count" does not exist: nothing deleted
		t.Fatalf("after wrong-scope delete: %+v", st)
	}
	a.mustJSON("DELETE", "/api/plugins/kvdemo/storage?key=count&instance="+wid, "", nil)
	a.mustJSON("GET", "/api/plugins/kvdemo/storage", "", &st)
	if len(st.Entries) != 1 || st.Entries[0].Key != "plain" {
		t.Fatalf("after instance key delete: %+v", st)
	}
	if code, _ := a.do("DELETE", "/api/plugins/kvdemo/storage?instance="+wid, ""); code != 400 {
		t.Errorf("instance without key: %d", code)
	}
	a.mustJSON("DELETE", "/api/plugins/kvdemo/storage", "", nil)
	a.mustJSON("GET", "/api/plugins/kvdemo/storage", "", &st)
	if len(st.Entries) != 0 || st.Size != 0 {
		t.Fatalf("after clear: %+v", st)
	}

	// deleting the widget (REST) clears its instance storage
	a.mustJSON("GET", "/api/widgets/"+wid+"/data?force=1", "", nil)
	a.mustJSON("DELETE", "/api/widgets/"+wid, "", nil)
	a.mustJSON("GET", "/api/plugins/kvdemo/storage", "", &st)
	if len(st.Entries) != 1 || st.Entries[0].Instance != "" {
		t.Fatalf("after widget delete: %+v", st)
	}
}

func TestPluginDataExportImportREST(t *testing.T) {
	e := setup(t)
	a := e.admin()
	a.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": storageDemo}), nil)
	var w map[string]any
	a.mustJSON("POST", "/api/widgets", `{"plugin":"kvdemo","widget":"w"}`, &w)
	wid := w["id"].(string)
	a.mustJSON("GET", "/api/widgets/"+wid+"/data", "", nil)

	// default export: no plugin_data key at all
	code, plain := a.do("GET", "/api/export", "")
	if code != 200 || strings.Contains(string(plain), "plugin_data") {
		t.Fatalf("default export: %d %s", code, plain)
	}
	code, full := a.do("GET", "/api/export?plugin_data=1", "")
	if code != 200 {
		t.Fatalf("export: %d %s", code, full)
	}
	var x struct {
		PluginData map[string]map[string]json.RawMessage `json:"plugin_data"`
	}
	json.Unmarshal(full, &x)
	kv := x.PluginData["kvdemo"]
	if raw, ok := kv["plain"]; ok { // the export is indented
		var c bytes.Buffer
		_ = json.Compact(&c, raw)
		kv["plain"] = c.Bytes()
	}
	if len(kv) != 2 || string(kv["plain"]) != `{"hello":"world"}` || string(kv["@i/"+wid+"/count"]) != "7" {
		t.Fatalf("plugin_data: %s", full)
	}

	// replace import into another instance restores it (widget ids are kept)
	e2 := setup(t)
	a2 := e2.admin()
	a2.mustJSON("POST", "/api/import", `{"mode":"replace","data":`+string(full)+`}`, nil)
	var st storageResp
	a2.mustJSON("GET", "/api/plugins/kvdemo/storage", "", &st)
	if len(st.Entries) != 2 {
		t.Fatalf("restored: %+v", st)
	}
	// the same file without plugin_data leaves existing storage alone
	a2.mustJSON("POST", "/api/import", `{"mode":"replace","data":`+string(plain)+`}`, nil)
	a2.mustJSON("GET", "/api/plugins/kvdemo/storage", "", &st)
	if len(st.Entries) != 2 {
		t.Fatalf("import without plugin_data touched storage: %+v", st)
	}
	// merge: only new plugins; the widget gets a fresh id and its storage follows
	e3 := setup(t)
	a3 := e3.admin()
	a3.mustJSON("POST", "/api/import", `{"mode":"merge","data":`+string(full)+`}`, nil)
	a3.mustJSON("GET", "/api/plugins/kvdemo/storage", "", &st)
	var ws []map[string]any
	a3.mustJSON("GET", "/api/bootstrap", "", &struct {
		Widgets *[]map[string]any `json:"widgets"`
	}{&ws})
	if len(st.Entries) != 2 || len(ws) != 1 || ws[0]["id"] == wid || st.Entries[1].Instance != ws[0]["id"] {
		t.Fatalf("merge: %+v widgets %v", st, ws)
	}
	// invalid plugin_data is rejected before anything is imported
	bad := `{"mode":"merge","data":{"schema":2,"workspaces":[],"categories":[],"links":[],"plugin_data":{"Bad Id":{"k":1}}}}`
	if code, b := a3.do("POST", "/api/import", bad); code != 400 {
		t.Errorf("bad plugin_data: %d %s", code, b)
	}
}
