package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jae-jae/genkan/internal/store"
)

func newSrv(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, "test"), st, st.Snapshot().Workspaces[0].ID
}

func rpc(t *testing.T, s *Server, body string) (map[string]any, int) {
	t.Helper()
	out, status := s.Handle([]byte(body))
	if out == nil {
		return nil, status
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("bad json %s: %v", out, err)
	}
	return m, status
}

func call(t *testing.T, s *Server, name string, args any) (res map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	m, _ := rpc(t, s, string(b))
	if m["error"] != nil {
		t.Fatalf("rpc error: %v", m["error"])
	}
	return m["result"].(map[string]any)
}

func TestInitialize(t *testing.T) {
	s, _, _ := newSrv(t)
	m, status := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	r := m["result"].(map[string]any)
	if status != 200 || r["protocolVersion"] != "2025-03-26" || r["serverInfo"].(map[string]any)["name"] != "genkan" {
		t.Fatalf("%v", m)
	}
	if _, ok := r["capabilities"].(map[string]any)["tools"]; !ok || r["instructions"] == "" {
		t.Fatalf("capabilities/instructions: %v", r)
	}
	m, _ = rpc(t, s, `{"jsonrpc":"2.0","id":"x","method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if m["result"].(map[string]any)["protocolVersion"] != "2025-06-18" || m["id"] != "x" {
		t.Fatalf("fallback version: %v", m)
	}
}

func TestNotificationsAndErrors(t *testing.T) {
	s, _, _ := newSrv(t)
	if out, status := rpc(t, s, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); out != nil || status != 202 {
		t.Fatalf("notification: %v %d", out, status)
	}
	if m, _ := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); m["result"] == nil {
		t.Fatalf("ping: %v", m)
	}
	m, _ := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"nope"}`)
	if m["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatalf("method not found: %v", m)
	}
	m, status := rpc(t, s, `{bad`)
	if m["error"].(map[string]any)["code"].(float64) != -32700 || status != 400 {
		t.Fatalf("parse error: %v %d", m, status)
	}
	m, _ = rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"zzz"}}`)
	if m["error"].(map[string]any)["code"].(float64) != -32602 {
		t.Fatalf("unknown tool: %v", m)
	}
}

func TestBatch(t *testing.T) {
	s, _, _ := newSrv(t)
	out, status := s.Handle([]byte(`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`))
	var arr []map[string]any
	if err := json.Unmarshal(out, &arr); err != nil || status != 200 || len(arr) != 2 {
		t.Fatalf("batch: %s %d", out, status)
	}
	if out, status := s.Handle([]byte(`[{"jsonrpc":"2.0","method":"notifications/initialized"}]`)); out != nil || status != 202 {
		t.Fatal("notification-only batch")
	}
}

func TestToolsListSchemas(t *testing.T) {
	s, _, _ := newSrv(t)
	m, _ := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := m["result"].(map[string]any)["tools"].([]any)
	want := []string{"get_overview", "search_links", "create_workspace", "update_workspace", "delete_workspace", "create_category", "update_category", "delete_category", "create_links", "update_link", "delete_links", "reorder", "update_settings",
		"list_plugins", "install_plugin", "preview_plugin", "get_plugin_source", "update_plugin_source", "update_plugin", "uninstall_plugin", "test_plugin", "plugin_logs", "plugin_storage", "clear_plugin_storage", "list_widgets", "add_widget", "update_widget", "remove_widget"}
	if len(tools) != len(want) {
		t.Fatalf("got %d tools", len(tools))
	}
	for i, tl := range tools {
		tm := tl.(map[string]any)
		if tm["name"] != want[i] || tm["description"] == "" {
			t.Errorf("tool %d: %v", i, tm["name"])
		}
		sch := tm["inputSchema"].(map[string]any)
		if sch["type"] != "object" || sch["properties"] == nil {
			t.Errorf("%s: schema incomplete", want[i])
		}
		for k, p := range sch["properties"].(map[string]any) {
			pm := p.(map[string]any)
			if pm["type"] == nil || pm["description"] == nil && k != "links" {
				t.Errorf("%s.%s lacks type/description", want[i], k)
			}
		}
	}
	// create_links item schema is fully described
	var cl map[string]any
	for _, tl := range tools {
		if tl.(map[string]any)["name"] == "create_links" {
			cl = tl.(map[string]any)["inputSchema"].(map[string]any)
		}
	}
	items := cl["properties"].(map[string]any)["links"].(map[string]any)["items"].(map[string]any)
	if len(items["properties"].(map[string]any)) < 8 || len(items["required"].([]any)) != 3 {
		t.Fatalf("items schema: %v", items)
	}
}

func TestToolCalls(t *testing.T) {
	s, st, ws := newSrv(t)
	var created []store.Link
	s.OnLinksCreated = func(l []store.Link) { created = l }
	var versions []int64
	st.OnChange(func(v int64) { versions = append(versions, v) })

	res := call(t, s, "create_category", M{"workspace_id": ws, "name": "Media"})
	cat := res["structuredContent"].(map[string]any)["category"].(map[string]any)["id"].(string)
	if res["isError"] != nil {
		t.Fatalf("%v", res)
	}
	content := res["content"].([]any)[0].(map[string]any)
	if content["type"] != "text" || !strings.Contains(content["text"].(string), "Media") {
		t.Fatalf("content: %v", content)
	}

	res = call(t, s, "create_links", M{"links": []M{
		{"workspace_id": ws, "category_id": cat, "title": "Jellyfin", "url": "http://nas.lan:8096", "health_check": true},
		{"workspace_id": ws, "category_id": cat, "title": "Plex", "url": "nas.lan:32400", "pinned": true},
		{"workspace_id": ws, "title": "Bad", "url": "javascript:alert(1)"},
		{"workspace_id": ws, "title": "Dup", "url": "http://nas.lan:8096/"},
	}})
	sc := res["structuredContent"].(map[string]any)
	if sc["created_count"].(float64) != 2 || len(sc["errors"].([]any)) != 2 || res["isError"] != nil {
		t.Fatalf("create_links: %v", sc)
	}
	if len(created) != 2 {
		t.Fatal("OnLinksCreated not called")
	}
	nv := len(versions)
	if nv != 2 { // one for the category, one for the whole batch
		t.Fatalf("expected 2 change notifications, got %d", nv)
	}

	// all failing => isError
	res = call(t, s, "create_links", M{"links": []M{{"workspace_id": "nope", "title": "x", "url": "http://x"}}})
	if res["isError"] != true {
		t.Fatalf("expected isError: %v", res)
	}
	// duplicate on second run
	res = call(t, s, "create_links", M{"links": []M{{"workspace_id": ws, "category_id": cat, "title": "Plex", "url": "http://nas.lan:32400"}}})
	if res["isError"] != true {
		t.Fatalf("duplicate should be reported: %v", res)
	}

	ov := call(t, s, "get_overview", M{})["structuredContent"].(map[string]any)
	w0 := ov["workspaces"].([]any)[0].(map[string]any)
	c0 := w0["categories"].([]any)[0].(map[string]any)
	links := c0["links"].([]any)
	if len(links) != 2 || ov["total_links"].(float64) != 2 || links[0].(map[string]any)["title"] != "Jellyfin" {
		t.Fatalf("overview: %v", ov)
	}
	id := links[1].(map[string]any)["id"].(string)

	sr := call(t, s, "search_links", M{"query": "plex nas"})["structuredContent"].(map[string]any)
	if sr["count"].(float64) != 1 || sr["links"].([]any)[0].(map[string]any)["category_name"] != "Media" {
		t.Fatalf("search: %v", sr)
	}

	res = call(t, s, "update_link", M{"id": id, "title": "Plex Media", "category_id": ""})
	l := res["structuredContent"].(map[string]any)["link"].(map[string]any)
	if l["title"] != "Plex Media" || l["category_id"] != "" {
		t.Fatalf("update_link: %v", l)
	}

	if r := call(t, s, "reorder", M{"kind": "links", "workspace_id": ws, "category_id": cat}); r["isError"] != nil {
		// empty ids is valid and a no-op
		t.Fatalf("%v", r)
	}
	if r := call(t, s, "reorder", M{"kind": "links", "ids": []string{id}}); r["isError"] != true {
		t.Fatal("reorder links without workspace_id should fail")
	}
	if r := call(t, s, "reorder", M{"kind": "workspaces", "ids": []string{ws}}); r["isError"] != nil {
		t.Fatalf("%v", r)
	}

	r := call(t, s, "update_settings", M{"sort_mode": "custom", "show_clock": false})
	if r["structuredContent"].(map[string]any)["settings"].(map[string]any)["sort_mode"] != "custom" {
		t.Fatalf("%v", r)
	}
	if r := call(t, s, "update_settings", M{"sort_mode": "bad"}); r["isError"] != true {
		t.Fatal("bad sort_mode accepted")
	}

	d := call(t, s, "delete_links", M{"ids": []string{id, "missing"}})["structuredContent"].(map[string]any)
	if d["deleted"].(float64) != 1 || len(d["not_found"].([]any)) != 1 {
		t.Fatalf("delete_links: %v", d)
	}
	w2 := call(t, s, "create_workspace", M{"name": "Work", "icon": "lucide:briefcase"})["structuredContent"].(map[string]any)["workspace"].(map[string]any)["id"].(string)
	if r := call(t, s, "update_workspace", M{"id": w2, "name": "Office"}); r["isError"] != nil {
		t.Fatalf("%v", r)
	}
	if r := call(t, s, "delete_category", M{"id": cat, "delete_links": true}); r["isError"] != nil {
		t.Fatalf("%v", r)
	}
	if r := call(t, s, "delete_workspace", M{"id": w2}); r["isError"] != nil {
		t.Fatalf("%v", r)
	}
	if r := call(t, s, "delete_workspace", M{"id": ws}); r["isError"] != true {
		t.Fatal("deleting last workspace should fail")
	}
	if r := call(t, s, "update_category", M{"id": "nope", "name": "x"}); r["isError"] != true {
		t.Fatal("unknown category should fail")
	}
	// null / missing arguments are tolerated
	if r, _ := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_overview"}}`); r["result"] == nil {
		t.Fatalf("%v", r)
	}
}
