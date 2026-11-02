package mcp

import (
	"strings"
	"testing"

	"github.com/jae-jae/genkan/internal/store"
)

func toolSchema(t *testing.T, s *Server, name string) (desc string, props map[string]any) {
	t.Helper()
	m, _ := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	for _, tl := range m["result"].(map[string]any)["tools"].([]any) {
		tm := tl.(map[string]any)
		if tm["name"] == name {
			return tm["description"].(string), tm["inputSchema"].(map[string]any)["properties"].(map[string]any)
		}
	}
	t.Fatalf("tool %s not found", name)
	return
}

func TestWorkspaceIconEnumInSchema(t *testing.T) {
	s, _, _ := newSrv(t)
	want := map[string]bool{}
	for _, n := range store.BuiltinIcons {
		want["lucide:"+n] = true
	}
	for _, tool := range []string{"create_workspace", "update_workspace"} {
		_, props := toolSchema(t, s, tool)
		icon := props["icon"].(map[string]any)
		if icon["type"] != "string" {
			t.Errorf("%s icon type %v", tool, icon["type"])
		}
		enum, ok := icon["enum"].([]any)
		if !ok || len(enum) != 52 {
			t.Fatalf("%s: icon enum = %v", tool, icon["enum"])
		}
		for _, v := range enum {
			if !want[v.(string)] {
				t.Errorf("%s: unexpected enum value %v", tool, v)
			}
			delete(want, v.(string))
		}
		if len(want) != 0 {
			t.Errorf("%s: enum is missing %v", tool, want)
		}
		if d, _ := icon["description"].(string); !strings.Contains(d, "lucide") {
			t.Errorf("%s: icon description %q", tool, d)
		}
		// refill for the next tool
		for _, n := range store.BuiltinIcons {
			want["lucide:"+n] = true
		}
	}
}

func TestWorkspaceIconValidationViaTool(t *testing.T) {
	s, _, _ := newSrv(t)
	res := call(t, s, "create_workspace", M{"name": "Lab", "icon": "lucide:flask-conical"})
	w := res["structuredContent"].(map[string]any)["workspace"].(map[string]any)
	if res["isError"] != nil || w["icon"] != "lucide:flask-conical" {
		t.Fatalf("%v", res)
	}
	if res := call(t, s, "create_workspace", M{"name": "Bad", "icon": "🧪"}); res["isError"] != true {
		t.Fatalf("emoji icon accepted: %v", res)
	}
	if res := call(t, s, "create_workspace", M{"name": "Bad", "icon": "lucide:nope"}); res["isError"] != true {
		t.Fatalf("unknown icon accepted: %v", res)
	}
	if res := call(t, s, "update_workspace", M{"id": w["id"], "icon": "lucide:nope"}); res["isError"] != true {
		t.Fatalf("unknown icon accepted on update: %v", res)
	}
	res = call(t, s, "update_workspace", M{"id": w["id"], "icon": "lucide:rocket"})
	if res["isError"] != nil || res["structuredContent"].(map[string]any)["workspace"].(map[string]any)["icon"] != "lucide:rocket" {
		t.Fatalf("%v", res)
	}
	res = call(t, s, "create_workspace", M{"name": "NoIcon"})
	if res["structuredContent"].(map[string]any)["workspace"].(map[string]any)["icon"] != "lucide:layout-grid" {
		t.Fatalf("default icon: %v", res)
	}
}

func TestPinnedSemanticsInToolsAndOverview(t *testing.T) {
	s, _, ws := newSrv(t)
	for _, tool := range []string{"create_links", "update_link"} {
		_, props := toolSchema(t, s, tool)
		var pinned map[string]any
		if tool == "create_links" {
			pinned = props["links"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["pinned"].(map[string]any)
		} else {
			pinned = props["pinned"].(map[string]any)
		}
		d := pinned["description"].(string)
		if !strings.Contains(d, "AND stays in its category") || strings.Contains(d, "favorites") {
			t.Errorf("%s: pinned description %q", tool, d)
		}
	}
	rd, rprops := toolSchema(t, s, "reorder")
	if !strings.Contains(rd, "pin_sort") || !strings.Contains(rd, "__pinned") || !strings.Contains(rprops["category_id"].(map[string]any)["description"].(string), "pinned row") {
		t.Errorf("reorder description lacks pinned semantics: %q", rd)
	}
	od, _ := toolSchema(t, s, "get_overview")
	if !strings.Contains(od, "pin_sort") {
		t.Errorf("get_overview description: %q", od)
	}

	cat := call(t, s, "create_category", M{"workspace_id": ws, "name": "Media"})["structuredContent"].(map[string]any)["category"].(map[string]any)["id"].(string)
	call(t, s, "create_links", M{"links": []M{
		{"workspace_id": ws, "category_id": cat, "title": "A", "url": "http://a.lan", "pinned": true},
		{"workspace_id": ws, "category_id": cat, "title": "B", "url": "http://b.lan"},
		{"workspace_id": ws, "category_id": cat, "title": "C", "url": "http://c.lan", "pinned": true},
	}})
	ov := call(t, s, "get_overview", M{})["structuredContent"].(map[string]any)
	links := ov["workspaces"].([]any)[0].(map[string]any)["categories"].([]any)[0].(map[string]any)["links"].([]any)
	if len(links) != 3 {
		t.Fatalf("pinned links must stay in the category: %v", links)
	}
	pin := map[string]float64{}
	for _, l := range links {
		lm := l.(map[string]any)
		v, ok := lm["pin_sort"]
		if !ok {
			t.Fatalf("overview link lacks pin_sort: %v", lm)
		}
		pin[lm["title"].(string)] = v.(float64)
	}
	if pin["A"] != 0 || pin["C"] != 1 {
		t.Fatalf("pin_sort values %v", pin)
	}
	// update_link pinned false -> true appends to the end of the pinned row
	var bID string
	for _, l := range links {
		if l.(map[string]any)["title"] == "B" {
			bID = l.(map[string]any)["id"].(string)
		}
	}
	res := call(t, s, "update_link", M{"id": bID, "pinned": true})
	if l := res["structuredContent"].(map[string]any)["link"].(map[string]any); l["pin_sort"].(float64) != 2 || l["category_id"] != cat {
		t.Fatalf("update_link pin: %v", l)
	}
	// reorder __pinned reorders pin_sort only
	res = call(t, s, "reorder", M{"kind": "links", "workspace_id": ws, "category_id": "__pinned", "ids": []string{bID}})
	if res["isError"] != nil {
		t.Fatalf("%v", res)
	}
	ov = call(t, s, "get_overview", M{})["structuredContent"].(map[string]any)
	for _, l := range ov["workspaces"].([]any)[0].(map[string]any)["categories"].([]any)[0].(map[string]any)["links"].([]any) {
		lm := l.(map[string]any)
		if lm["title"] == "B" && lm["pin_sort"].(float64) != 0 {
			t.Errorf("B should lead the pinned row: %v", lm)
		}
	}
	// search results include pin_sort as well
	sr := call(t, s, "search_links", M{"query": "b.lan"})["structuredContent"].(map[string]any)["links"].([]any)
	if _, ok := sr[0].(map[string]any)["pin_sort"]; !ok {
		t.Errorf("search result lacks pin_sort")
	}
}
