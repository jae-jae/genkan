package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jae-jae/genkan/internal/store"
)

const storagePlugin = `{
  id:"stor", name:"Stor", version:"1",
  config:[{key:"tok",type:"secret",label:"Tok"}],
  widgets:[{id:"w",name:"W",config:[{key:"label",type:"string",label:"L",default:"d"},{key:"pw",type:"secret",label:"PW"}]}],
  schedules:[{id:"s",every:60}]
}`

const storageBody = `
export async function widget(ctx) {
  const s = ctx.instance.storage;
  const n = (s.get("n") || 0) + 1;
  s.set("n", n);
  s.set("obj", { a: [1, 2] });
  genkan.storage.set("shared", ctx.instance.id);
  return { title: ctx.instance.config.label, body: [{ type: "text", text: "n=" + n + " keys=" + s.keys().join(",") + " plugin=" + genkan.storage.keys().join(",") }] };
}
export async function action(ctx) {
  const s = ctx.instance.storage;
  if (ctx.action === "del") s.delete("obj");
  if (ctx.action === "big") s.set("big", "x".repeat(ctx.params.n));
  return { toast: ctx.action + ":" + s.keys().join(",") };
}
export async function schedule(ctx) {
  const l = genkan.widgets.list();
  genkan.storage.set("seen", l);
  return { n: l.length, noInstance: ctx.instance === undefined };
}
`

func widgetText(t *testing.T, e *tenv, id string) string {
	t.Helper()
	d, err := e.m.Data(context.Background(), id, "en", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.View.Body) == 0 {
		t.Fatalf("empty view %+v", d.View)
	}
	s, _ := d.View.Body[0]["text"].(string)
	return s
}

func TestInstanceStorage(t *testing.T) {
	e := newEnv(t)
	e.install(plug(storagePlugin, storageBody))
	w1 := addWidget(t, e, "stor", "w", map[string]any{"label": "one"})
	w2 := addWidget(t, e, "stor", "w", map[string]any{"label": "two"})

	if got := widgetText(t, e, w1.ID); got != "n=1 keys=n,obj plugin=shared" {
		t.Fatalf("first: %q", got)
	}
	widgetText(t, e, w1.ID)
	if got := widgetText(t, e, w1.ID); !strings.HasPrefix(got, "n=3 ") {
		t.Fatalf("w1 third call: %q", got)
	}
	if got := widgetText(t, e, w2.ID); !strings.HasPrefix(got, "n=1 ") { // instances do not share
		t.Fatalf("w2: %q", got)
	}

	info, err := e.m.Storage("stor")
	if err != nil {
		t.Fatal(err)
	}
	if info.Limit != MaxKVBytes || info.Size <= 0 {
		t.Fatalf("info %+v", info)
	}
	var plain, inst int
	sum := 0
	for _, en := range info.Entries {
		sum += en.Size
		if en.Instance == "" {
			plain++
		} else {
			inst++
			if en.Instance != w1.ID && en.Instance != w2.ID {
				t.Errorf("unknown instance %q", en.Instance)
			}
		}
	}
	if plain != 1 || inst != 4 || sum != info.Size {
		t.Fatalf("entries %+v (plain %d inst %d sum %d)", info, plain, inst, sum)
	}
	if info.Entries[0].Instance != "" || info.Entries[0].Key != "shared" {
		t.Fatalf("plugin keys sort first: %+v", info.Entries[0])
	}

	// the plugin level cannot see or forge instance keys
	ent, _, _ := e.m.entryFor("stor")
	if _, ok := ent.kv.get(instKey(w1.ID, "n")); ok {
		t.Error("plugin-level get reads the instance namespace")
	}
	if err := ent.kv.set(instPrefix+"x/y", []byte("1")); err == nil {
		t.Error("plugin-level set accepted a reserved key")
	}
	if k := ent.kv.keys(); len(k) != 1 || k[0] != "shared" {
		t.Errorf("plugin keys: %v", k)
	}

	// actions use the same instance storage; the 256 KiB cap is shared with the plugin
	res, err := e.m.Action(context.Background(), w1.ID, ActionRequest{Action: "del"}, "en")
	if err != nil || res.Toast != "del:n" {
		t.Fatalf("action: %+v %v", res, err)
	}
	if _, err := e.m.Action(context.Background(), w1.ID, ActionRequest{Action: "big", Params: map[string]any{"n": 300 << 10}}, "en"); err == nil || !strings.Contains(err.Error(), "full") {
		t.Fatalf("cap: %v", err)
	}

	// the test hook of an unsaved instance never persists anything
	before, _ := e.m.Storage("stor")
	tr, err := e.m.Test(context.Background(), "stor", TestRequest{Hook: "widget"})
	if err != nil || !tr.OK {
		t.Fatalf("test: %+v %v", tr, err)
	}
	after, _ := e.m.Storage("stor")
	if len(after.Entries) != len(before.Entries) {
		t.Errorf("unsaved test instance wrote storage: %+v", after.Entries)
	}

	// deleting a widget clears its storage only
	if err := e.m.DeleteWidget(w1.ID); err != nil {
		t.Fatal(err)
	}
	info, _ = e.m.Storage("stor")
	for _, en := range info.Entries {
		if en.Instance == w1.ID {
			t.Errorf("storage of the deleted widget remains: %+v", en)
		}
	}
	if len(info.Entries) != 3 { // shared + w2's n and obj
		t.Errorf("entries after delete: %+v", info.Entries)
	}
}

func TestInstanceStorageCascades(t *testing.T) {
	e := newEnv(t)
	e.install(plug(storagePlugin, storageBody))
	ws, err := e.st.CreateWorkspace(store.WorkspaceInput{Name: "Extra", Icon: "lucide:house", Wallpaper: store.DefaultWallpaper})
	if err != nil {
		t.Fatal(err)
	}
	inWS, err := e.m.AddWidget(WidgetAdd{Plugin: "stor", Widget: "w", WorkspaceID: ws.ID})
	if err != nil {
		t.Fatal(err)
	}
	keep := addWidget(t, e, "stor", "w", nil)
	widgetText(t, e, inWS.ID)
	widgetText(t, e, keep.ID)
	// workspace delete cascades to the widgets and their storage
	if err := e.st.DeleteWorkspace(ws.ID); err != nil {
		t.Fatal(err)
	}
	info, _ := e.m.Storage("stor")
	for _, en := range info.Entries {
		if en.Instance == inWS.ID {
			t.Fatalf("workspace delete left %+v", en)
		}
	}
	found := false
	for _, en := range info.Entries {
		found = found || en.Instance == keep.ID
	}
	if !found {
		t.Fatal("the other instance lost its storage")
	}
	// replace import drops the storage of widgets that are not part of the import
	exp := e.st.Export()
	exp.Widgets = nil
	if _, err := e.st.ImportData("replace", exp); err != nil {
		t.Fatal(err)
	}
	info, _ = e.m.Storage("stor")
	for _, en := range info.Entries {
		if en.Instance != "" {
			t.Fatalf("replace import left %+v", en)
		}
	}
	// uninstall removes the file
	w := addWidget(t, e, "stor", "w", nil)
	widgetText(t, e, w.ID)
	ent, _, _ := e.m.entryFor("stor")
	_ = ent.kv.flush()
	file := filepath.Join(e.dir, "plugins", "stor.kv.json")
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Uninstall("stor"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("kv file survived uninstall: %v", err)
	}
}

func TestClearStorage(t *testing.T) {
	e := newEnv(t)
	e.install(plug(storagePlugin, storageBody))
	w := addWidget(t, e, "stor", "w", nil)
	widgetText(t, e, w.ID)
	if err := e.m.ClearStorage("stor", "obj", w.ID); err != nil {
		t.Fatal(err)
	}
	info, _ := e.m.Storage("stor")
	if len(info.Entries) != 2 { // shared + n
		t.Fatalf("after deleting one instance key: %+v", info.Entries)
	}
	if err := e.m.ClearStorage("stor", "shared", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.m.ClearStorage("stor", "", w.ID); err == nil {
		t.Error("instance without key must be rejected")
	}
	info, _ = e.m.Storage("stor")
	if len(info.Entries) != 1 || info.Entries[0].Key != "n" || info.Entries[0].Instance != w.ID {
		t.Fatalf("after deleting a plugin key: %+v", info.Entries)
	}
	if err := e.m.ClearStorage("stor", "", ""); err != nil {
		t.Fatal(err)
	}
	info, _ = e.m.Storage("stor")
	if len(info.Entries) != 0 || info.Size != 0 {
		t.Fatalf("after clear: %+v", info)
	}
	if err := e.m.ClearStorage("nope", "", ""); store.ErrKind(err) != store.KindNotFound {
		t.Errorf("unknown plugin: %v", err)
	}
	// the cleared widget starts over
	if got := widgetText(t, e, w.ID); !strings.HasPrefix(got, "n=1 ") {
		t.Errorf("widget after clear: %q", got)
	}
}

func TestWidgetsListHost(t *testing.T) {
	e := newEnv(t)
	e.install(plug(storagePlugin, storageBody))
	e.install(plug(`{id:"other", name:"O", version:"1", widgets:[{id:"w",name:"W"}]}`, `export async function widget(){ return {body:[]} }`))
	a := addWidget(t, e, "stor", "w", map[string]any{"label": "A", "pw": "s3cret-pw"})
	addWidget(t, e, "other", "w", nil)
	res, err := e.m.Test(context.Background(), "stor", TestRequest{Hook: "schedule"})
	if err != nil || !res.OK {
		t.Fatalf("schedule: %+v %v", res, err)
	}
	if fmt.Sprint(res.Result.(map[string]any)["n"]) != "1" || res.Result.(map[string]any)["noInstance"] != true {
		t.Fatalf("result %v", res.Result)
	}
	ent, _, _ := e.m.entryFor("stor")
	raw, _ := ent.kv.get("seen")
	var l []map[string]any
	if err := json.Unmarshal(raw, &l); err != nil || len(l) != 1 {
		t.Fatalf("list: %s %v", raw, err)
	}
	cfg := l[0]["config"].(map[string]any)
	if l[0]["id"] != a.ID || l[0]["widget"] != "w" || cfg["label"] != "A" || cfg["pw"] != "s3cret-pw" {
		t.Fatalf("entry %v (own widget, defaults applied, secrets included)", l[0])
	}
	if _, has := l[0]["workspace_id"]; !has {
		t.Fatalf("workspace_id missing: %v", l[0])
	}
	// the secret never reaches the log
	for _, le := range e.m.mustLogs(t, "stor") {
		if strings.Contains(le.Msg, "s3cret-pw") {
			t.Errorf("secret in log: %s", le.Msg)
		}
	}
}

func (m *Manager) mustLogs(t *testing.T, id string) []LogEntry {
	t.Helper()
	l, err := m.Logs(id)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestPluginDataExportImport(t *testing.T) {
	e := newEnv(t)
	e.install(plug(storagePlugin, storageBody))
	w := addWidget(t, e, "stor", "w", nil)
	widgetText(t, e, w.ID)

	data := e.m.ExportData()
	kv := data["stor"]
	if len(kv) != 3 || string(kv["shared"]) != `"`+w.ID+`"` || string(kv[instKey(w.ID, "obj")]) != `{"a":[1,2]}` {
		t.Fatalf("export data: %v", kv)
	}
	exp := e.st.Export()
	if exp.PluginData != nil {
		t.Fatal("default exports carry no plugin data")
	}
	exp.PluginData = data
	if err := ValidatePluginData(data); err != nil {
		t.Fatal(err)
	}

	// merge into an instance that already has the plugin: nothing is restored
	res, err := e.st.ImportData("merge", exp)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.PluginIDs) != 0 {
		t.Fatalf("merge: plugin ids %v", res.PluginIDs)
	}
	if err := e.m.RestoreData(data, res); err != nil {
		t.Fatal(err)
	}
	info, _ := e.m.Storage("stor")
	if len(info.Entries) != 3 {
		t.Fatalf("merge touched storage of an existing plugin: %+v", info.Entries)
	}

	// merge into an empty instance: new plugin, fresh widget ids, instance keys follow
	e2 := newEnv(t)
	res, err = e2.st.ImportData("merge", exp)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.PluginIDs) != 1 || res.WidgetMap[w.ID].ID == "" || res.WidgetMap[w.ID].ID == w.ID {
		t.Fatalf("merge result %+v", res)
	}
	data["stor"][instKey("ghost0", "n")] = json.RawMessage(`1`) // instance that is not in the import
	if err := e2.m.RestoreData(data, res); err != nil {
		t.Fatal(err)
	}
	nid := res.WidgetMap[w.ID].ID
	info, _ = e2.m.Storage("stor")
	if len(info.Entries) != 3 {
		t.Fatalf("restored: %+v", info.Entries)
	}
	for _, en := range info.Entries {
		if en.Instance != "" && en.Instance != nid {
			t.Errorf("instance key not remapped: %+v", en)
		}
	}

	// replace: storage is replaced, a plugin without data is cleared
	e3 := newEnv(t)
	e3.install(plug(storagePlugin, storageBody))
	ent, _, _ := e3.m.entryFor("stor")
	_ = ent.kv.set("old", []byte("1"))
	res, err = e3.st.ImportData("replace", exp)
	if err != nil {
		t.Fatal(err)
	}
	if err := e3.m.RestoreData(data, res); err != nil {
		t.Fatal(err)
	}
	info, _ = e3.m.Storage("stor")
	for _, en := range info.Entries {
		if en.Key == "old" {
			t.Error("replace kept the old storage")
		}
	}
	if len(info.Entries) != 3 {
		t.Fatalf("replace: %+v", info.Entries)
	}
	if err := e3.m.RestoreData(map[string]map[string]json.RawMessage{}, res); err != nil {
		t.Fatal(err)
	}
	if info, _ = e3.m.Storage("stor"); len(info.Entries) != 0 {
		t.Fatalf("plugin missing from plugin_data must be cleared: %+v", info.Entries)
	}

	// validation
	big := map[string]map[string]json.RawMessage{"stor": {"k": json.RawMessage(`"` + strings.Repeat("x", MaxKVBytes) + `"`)}}
	if ValidatePluginData(big) == nil || ValidatePluginData(map[string]map[string]json.RawMessage{"Bad_ID": {"k": json.RawMessage(`1`)}}) == nil {
		t.Error("oversized or badly named plugin data must be rejected")
	}
}

func TestSanitizeLineChart(t *testing.T) {
	pts := []any{
		[]any{3000.0, 3.0}, []any{1000.0, 1.0}, []any{"x", 1.0}, []any{2000.0}, []any{-5.0, 1.0}, []any{2000.4, 2.0}, "junk", []any{nil, 1.0},
	}
	v := SanitizeView(map[string]any{"body": []any{
		map[string]any{"type": "chart", "kind": "line", "points": pts, "format": "compact", "color": "green"},
		map[string]any{"type": "chart", "kind": "line", "points": []any{[]any{"a", "b"}}},
		map[string]any{"type": "chart", "kind": "line", "points": pts, "format": "weird", "color": "pink"},
	}})
	if len(v.Body) != 2 {
		t.Fatalf("blocks: %+v", v.Body)
	}
	b, _ := json.Marshal(v.Body[0])
	if string(b) != `{"color":"green","format":"compact","kind":"line","points":[[1000,1],[2000,2],[3000,3]],"type":"chart"}` {
		t.Errorf("chart: %s", b)
	}
	if _, has := v.Body[1]["format"]; has || v.Body[1]["color"] != nil {
		t.Errorf("unknown format/color must be dropped: %v", v.Body[1])
	}
	// at most 500 points
	many := make([]any, 800)
	for i := range many {
		many[i] = []any{float64(i + 1), 1.0}
	}
	v = SanitizeView(map[string]any{"body": []any{map[string]any{"type": "chart", "kind": "line", "points": many}}})
	if n := len(v.Body[0]["points"].([][2]float64)); n != 500 {
		t.Errorf("points: %d", n)
	}
}
