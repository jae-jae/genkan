package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jae-jae/genkan/internal/store"
)

const actionViewPlugin = `export const manifest = {
  id: "av", name: "AV", version: "1", icon: "puzzle",
  widgets: [{ id: "w", name: "W", size: "s", refresh: 90 }],
};
export async function widget(ctx) { return { title: "n=" + (ctx.instance.storage.get("n") || 0), body: [] }; }
export async function action(ctx) {
  if (ctx.action === "echo") return { toast: String(ctx.params.value.length) };
  const n = (ctx.instance.storage.get("n") || 0) + 1;
  ctx.instance.storage.set("n", n);
  return { toast: "t", view: { title: "n=" + n, tone: "pink", body: [{ type: "markdown", text: "hi", editAction: "save" }, { type: "input", action: "add" }, { type: "bogus" }] } };
}
`

func TestWidgetActionViewREST(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": actionViewPlugin}), nil)
	var w struct{ ID string }
	admin.mustJSON("POST", "/api/widgets", `{"plugin":"av","widget":"w"}`, &w)

	var res struct {
		Toast   string
		Refresh bool
		View    *struct {
			View struct {
				Title string
				Tone  string
				Body  []map[string]any
			}
			UpdatedAt int64 `json:"updated_at"`
			NextAt    int64 `json:"next_at"`
		}
	}
	admin.mustJSON("POST", "/api/widgets/"+w.ID+"/action", `{"action":"inc"}`, &res)
	if res.Toast != "t" || res.View == nil || res.View.View.Title != "n=1" || res.View.View.Tone != "pink" || len(res.View.View.Body) != 2 ||
		res.View.UpdatedAt == 0 || res.View.NextAt-res.View.UpdatedAt != 90_000 {
		t.Fatalf("response: %+v", res)
	}
	// raw shape: view is a WidgetData ({view, updated_at, next_at})
	code, raw := admin.do("POST", "/api/widgets/"+w.ID+"/action", `{"action":"inc"}`)
	var shape map[string]map[string]json.RawMessage
	json.Unmarshal(raw, &shape)
	if code != 200 || shape["view"]["view"] == nil || shape["view"]["updated_at"] == nil || shape["view"]["next_at"] == nil {
		t.Fatalf("shape: %d %s", code, raw)
	}
	// /data serves it (cached, same timestamps) without running widget() again
	var data struct {
		View      struct{ Title string }
		UpdatedAt int64 `json:"updated_at"`
	}
	admin.mustJSON("GET", "/api/widgets/"+w.ID+"/data", "", &data)
	var second struct {
		View struct {
			View      struct{ Title string }
			UpdatedAt int64 `json:"updated_at"`
		}
	}
	json.Unmarshal(raw, &second)
	if data.View.Title != "n=2" || data.UpdatedAt != second.View.UpdatedAt {
		t.Errorf("data: %+v vs action %d", data, second.View.UpdatedAt)
	}
	// guests cannot run actions
	e.st.UpdateSettings(store.SettingsPatch{GuestAccess: ptr(true)})
	if code, _ := e.anon().do("POST", "/api/widgets/"+w.ID+"/action", `{"action":"inc"}`); code != 403 && code != 401 {
		t.Errorf("guest: %d", code)
	}
}

func TestWidgetActionBodyLimits(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": actionViewPlugin}), nil)
	var w struct{ ID string }
	admin.mustJSON("POST", "/api/widgets", `{"plugin":"av","widget":"w"}`, &w)
	post := func(value string, extra string) (int, string) {
		body := fmt.Sprintf(`{"action":"echo","params":{"value":%q%s}}`, value, extra)
		code, b := admin.do("POST", "/api/widgets/"+w.ID+"/action", body)
		return code, string(b)
	}
	// 20000 characters (60 KB of CJK) pass the body limit and the value limit
	if code, b := post(strings.Repeat("好", 20000), ""); code != 200 || !strings.Contains(b, `"toast":"20000"`) {
		t.Errorf("20000 chars: %d %s", code, b)
	}
	// 20001 characters: 400
	if code, b := post(strings.Repeat("a", 20001), ""); code != 400 || !strings.Contains(b, "too long") {
		t.Errorf("20001 chars: %d %s", code, b)
	}
	// the rest of the params stays at 8 KiB
	if code, b := post("v", `,"other":"`+strings.Repeat("y", 9000)+`"`); code != 400 || !strings.Contains(b, "too large") {
		t.Errorf("big rest: %d %s", code, b)
	}
	// the whole body is limited to 64 KiB
	if code, _ := post(strings.Repeat("a", 70<<10), ""); code != 413 {
		t.Errorf("70 KiB body: %d", code)
	}
	// ... but only on this endpoint: other endpoints keep the 1 MiB limit
	big := fmt.Sprintf(`{"hook":"widget","target":"w","query":%q}`, strings.Repeat("a", 100<<10))
	if code, b := admin.do("POST", "/api/plugins/av/test", big); code != 200 {
		t.Errorf("other endpoints keep their limit: %d %s", code, b)
	}
}

func TestPluginTestEndpointActionView(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	admin.mustJSON("POST", "/api/plugins", jsonBody(map[string]any{"source": actionViewPlugin}), nil)
	var res struct {
		OK     bool
		Result struct {
			Toast string
			View  *struct {
				View      struct{ Title, Tone string }
				UpdatedAt int64 `json:"updated_at"`
				NextAt    int64 `json:"next_at"`
			}
		}
	}
	admin.mustJSON("POST", "/api/plugins/av/test", `{"hook":"action","action":"inc"}`, &res)
	if !res.OK || res.Result.View == nil || res.Result.View.View.Title != "n=1" || res.Result.View.View.Tone != "pink" || res.Result.View.NextAt-res.Result.View.UpdatedAt != 90_000 {
		t.Errorf("%+v", res)
	}
}

func TestCatalogListsWidgetsREST(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	var cat []struct {
		ID      string
		Widgets []struct {
			ID   string
			Name any
			Size string
		}
	}
	admin.mustJSON("GET", "/api/plugins/catalog", "", &cat)
	found := map[string]int{}
	for _, c := range cat {
		if c.Widgets == nil {
			t.Errorf("%s: widgets must be an array", c.ID)
		}
		found[c.ID] = len(c.Widgets)
		if c.ID == "todo" {
			name, _ := c.Widgets[0].Name.(map[string]any)
			if c.Widgets[0].ID != "list" || c.Widgets[0].Size != "m" || name["zh"] != "待办" || name["en"] != "To-do" {
				t.Errorf("todo widget: %+v", c.Widgets)
			}
		}
	}
	if found["todo"] != 1 || found["note"] != 1 || found["calculator"] != 0 || found["media"] != 5 {
		t.Errorf("widget counts: %v", found)
	}
}

func TestSettingsPluginsSeededREST(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	var boot struct {
		Settings map[string]any
	}
	admin.mustJSON("GET", "/api/bootstrap", "", &boot)
	if s, ok := boot.Settings["plugins_seeded"].([]any); !ok || len(s) != 0 {
		t.Fatalf("bootstrap settings: %v", boot.Settings)
	}
	e.srv.Plugins().SeedDefaults(t.Context())
	var st map[string]any
	admin.mustJSON("PATCH", "/api/settings", `{"title":"T","plugins_seeded":["evil","todo"]}`, &st)
	if fmt.Sprint(st["plugins_seeded"]) != "[todo note]" || st["title"] != "T" {
		t.Errorf("PATCH must not edit plugins_seeded: %v", st)
	}
	if code, _ := admin.do("PATCH", "/api/settings", `{"plugins_seeded":[]}`); code != 200 {
		t.Errorf("unknown field is ignored: %d", code)
	}
	admin.mustJSON("GET", "/api/bootstrap", "", &boot)
	if fmt.Sprint(boot.Settings["plugins_seeded"]) != "[todo note]" {
		t.Errorf("after: %v", boot.Settings)
	}
	// export carries it; import into a new instance restores it and the plugins stay as they are
	code, exp := admin.do("GET", "/api/export", "")
	var ex struct{ Settings map[string]any }
	json.Unmarshal(exp, &ex)
	if code != 200 || fmt.Sprint(ex.Settings["plugins_seeded"]) != "[todo note]" {
		t.Fatalf("export: %d %v", code, ex.Settings)
	}
	e2 := setup(t)
	a2 := e2.admin()
	a2.mustJSON("POST", "/api/import", `{"mode":"replace","data":`+string(exp)+`}`, nil)
	a2.mustJSON("GET", "/api/bootstrap", "", &boot)
	if fmt.Sprint(boot.Settings["plugins_seeded"]) != "[todo note]" {
		t.Errorf("import: %v", boot.Settings)
	}
	// a seeded plugin removed by the admin does not come back at the next start
	admin.mustJSON("DELETE", "/api/plugins/todo", "", nil)
	e.srv.Plugins().SeedDefaults(t.Context())
	var infos []map[string]any
	admin.mustJSON("GET", "/api/plugins", "", &infos)
	for _, p := range infos {
		if p["id"] == "todo" {
			t.Error("todo came back")
		}
	}
}
