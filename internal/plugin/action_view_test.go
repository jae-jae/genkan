package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jae-jae/genkan/internal/store"
)

const counterManifest = `{
  id:"ctr", name:"Counter", version:"1", icon:"hash",
  widgets:[{id:"c",name:"C",size:"s",refresh:120}]
}`

const counterBody = `
const view = (ctx) => ({
  title: "n=" + (ctx.instance.storage.get("n") || 0),
  tone: "blue",
  body: [{ type: "markdown", text: "lang=" + ctx.lang, editAction: "save" }, { type: "script" }],
});
export async function widget(ctx) { return view(ctx); }
export async function action(ctx) {
  const s = ctx.instance.storage;
  switch (ctx.action) {
    case "inc": {
      const n = s.get("n") || 0;
      await new Promise((r) => setTimeout(r, 5)); // widen the read-modify-write window
      s.set("n", n + 1);
      return { view: view(ctx) };
    }
    case "inc-toast": s.set("n", (s.get("n") || 0) + 1); return { toast: "ok", view: view(ctx) };
    case "echo": return { toast: ctx.params.value.length + ":" + JSON.stringify(Object.keys(ctx.params).sort()) };
    case "plain": return { toast: "plain" };
    case "bad-view": return { view: "nope", toast: "bv" };
    case "boom": s.set("n", 41); throw new Error("boom");
    case "slow": await new Promise((r) => setTimeout(r, 300)); return { toast: "slow" };
  }
  return {};
}
`

func ctrEnv(t *testing.T) (*tenv, WidgetOut) {
	e := newEnv(t)
	e.install(plug(counterManifest, counterBody))
	return e, addWidget(t, e, "ctr", "c", nil)
}

func TestActionReturnsViewAndUpdatesCache(t *testing.T) {
	e, w := ctrEnv(t)
	ctx := context.Background()

	d0, err := e.m.Data(ctx, w.ID, "en", false)
	if err != nil || d0.View.Title != "n=0" {
		t.Fatalf("data: %v %+v", err, d0)
	}
	before := time.Now().UnixMilli()
	res, err := e.m.Action(ctx, w.ID, ActionRequest{Action: "inc"}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if res.View == nil {
		t.Fatal("no view in the result")
	}
	v := res.View
	// sanitized: tone kept, unknown block dropped, valid block kept
	if v.View.Title != "n=1" || v.View.Tone != "blue" || len(v.View.Body) != 1 || v.View.Body[0]["type"] != "markdown" {
		t.Errorf("view: %+v", v.View)
	}
	// the same meta as /data: updated_at = now, next_at = now + refresh
	if v.UpdatedAt < before || v.UpdatedAt > time.Now().UnixMilli() || v.NextAt-v.UpdatedAt != 120_000 {
		t.Errorf("meta: updated=%d next=%d", v.UpdatedAt, v.NextAt)
	}
	// the next /data (no force) is served from the cache and equals the action's view
	d1, err := e.m.Data(ctx, w.ID, "en", false)
	if err != nil {
		t.Fatal(err)
	}
	if d1.UpdatedAt != v.UpdatedAt || d1.NextAt != v.NextAt || d1.View.Title != "n=1" {
		t.Errorf("cache not updated: %+v vs %+v", d1, v)
	}
	// ... which the widget() hook did not produce: its text would still say n=1, so check the hook was not re-run
	// by comparing against a forced call that must differ in UpdatedAt
	time.Sleep(2 * time.Millisecond)
	d2, _ := e.m.Data(ctx, w.ID, "en", true)
	if d2.UpdatedAt == v.UpdatedAt || d2.View.Title != "n=1" {
		t.Errorf("forced: %+v", d2)
	}

	// JSON shape of the response
	b, _ := json.Marshal(res)
	var shape map[string]any
	json.Unmarshal(b, &shape)
	vv, _ := shape["view"].(map[string]any)
	if vv == nil || vv["view"] == nil || vv["updated_at"] == nil || vv["next_at"] == nil {
		t.Errorf("response shape: %s", b)
	}
}

func TestActionViewLanguageAndOtherCaches(t *testing.T) {
	e, w := ctrEnv(t)
	ctx := context.Background()
	e.m.Data(ctx, w.ID, "en", false)
	e.m.Data(ctx, w.ID, "zh", false)
	if _, err := e.m.Action(ctx, w.ID, ActionRequest{Action: "inc"}, "zh"); err != nil {
		t.Fatal(err)
	}
	// the view is cached for the language of the caller; the other language is dropped and recomputed
	dz, _ := e.m.Data(ctx, w.ID, "zh", false)
	if dz.View.Body[0]["text"] != "lang=zh" || dz.View.Title != "n=1" {
		t.Errorf("zh: %+v", dz.View)
	}
	de, _ := e.m.Data(ctx, w.ID, "en", false)
	if de.View.Body[0]["text"] != "lang=en" || de.View.Title != "n=1" {
		t.Errorf("en must be fresh, not stale: %+v", de.View)
	}
}

func TestActionWithoutViewDropsCache(t *testing.T) {
	e, w := ctrEnv(t)
	ctx := context.Background()
	d0, _ := e.m.Data(ctx, w.ID, "en", false)
	for _, name := range []string{"plain", "bad-view"} {
		res, err := e.m.Action(ctx, w.ID, ActionRequest{Action: name}, "en")
		if err != nil || res.View != nil {
			t.Fatalf("%s: %v %+v", name, err, res)
		}
	}
	time.Sleep(2 * time.Millisecond)
	d1, _ := e.m.Data(ctx, w.ID, "en", false)
	if d1.UpdatedAt == d0.UpdatedAt {
		t.Error("the cached view must be dropped when an action returns no view")
	}
	// toast + view together
	res, _ := e.m.Action(ctx, w.ID, ActionRequest{Action: "inc-toast"}, "en")
	if res.Toast != "ok" || res.View == nil || res.View.View.Title != "n=1" {
		t.Errorf("%+v", res)
	}
	// a failing action still drops the cache and returns the error
	if _, err := e.m.Action(ctx, w.ID, ActionRequest{Action: "boom"}, "en"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("boom: %v", err)
	}
	if d, _ := e.m.Data(ctx, w.ID, "en", false); d.View.Title != "n=41" {
		t.Errorf("stale cache after a failing action: %+v", d.View)
	}
}

func TestActionParamsLimits(t *testing.T) {
	e, w := ctrEnv(t)
	ctx := context.Background()
	run := func(p any) (ActionResult, error) {
		return e.m.Action(ctx, w.ID, ActionRequest{Action: "echo", Params: p}, "en")
	}
	// params.value up to 20000 characters (not bytes) with small other params
	ok := strings.Repeat("好", 20000)
	res, err := run(map[string]any{"value": ok, "id": "x"})
	if err != nil || res.Toast != `20000:["id","value"]` {
		t.Fatalf("20000 chars: %v %+v", err, res)
	}
	// 20001 characters is rejected
	if _, err := run(map[string]any{"value": ok + "x"}); store.ErrKind(err) != store.KindInvalid {
		t.Errorf("20001 chars: %v", err)
	}
	// the rest is limited to 8 KiB, value excluded
	if _, err := run(map[string]any{"value": "v", "other": strings.Repeat("y", 9000)}); store.ErrKind(err) != store.KindInvalid {
		t.Errorf("big rest: %v", err)
	}
	if _, err := run(map[string]any{"value": "v", "other": strings.Repeat("y", 7000)}); err != nil {
		t.Errorf("7000 byte rest: %v", err)
	}
	// a non-string value counts as part of the rest
	if _, err := run(map[string]any{"value": []any{strings.Repeat("y", 9000)}}); store.ErrKind(err) != store.KindInvalid {
		t.Errorf("big non-string value: %v", err)
	}
	// non-object params keep the old 8 KiB limit
	if _, err := e.m.Action(ctx, w.ID, ActionRequest{Action: "plain", Params: strings.Repeat("z", 9000)}, "en"); store.ErrKind(err) != store.KindInvalid {
		t.Errorf("big string params: %v", err)
	}
	// bad action names
	for _, a := range []string{"", "a b", strings.Repeat("a", 41)} {
		if _, err := e.m.Action(ctx, w.ID, ActionRequest{Action: a}, "en"); store.ErrKind(err) != store.KindInvalid {
			t.Errorf("action %q: %v", a, err)
		}
	}
	if _, err := e.m.Action(ctx, "nope", ActionRequest{Action: "plain"}, "en"); store.ErrKind(err) != store.KindNotFound {
		t.Errorf("unknown widget: %v", err)
	}
}

// Actions of one instance run one at a time: 20 parallel read-modify-write
// increments must not lose an update. Run with -race.
func TestActionsOfOneInstanceAreSerialized(t *testing.T) {
	e, w := ctrEnv(t)
	other := addWidget(t, e, "ctr", "c", nil)
	const n = 20
	var wg sync.WaitGroup
	views := make([]*WidgetData, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := e.m.Action(context.Background(), w.ID, ActionRequest{Action: "inc"}, "en")
			errs[i], views[i] = err, res.View
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for i := range views {
		if errs[i] != nil || views[i] == nil {
			t.Fatalf("call %d: %v", i, errs[i])
		}
		seen[views[i].View.Title] = true
	}
	// every call saw its own distinct count: n=1 .. n=20
	for i := 1; i <= n; i++ {
		if !seen[fmt.Sprintf("n=%d", i)] {
			t.Errorf("no action produced n=%d (lost update): %v", i, seen)
		}
	}
	if d, _ := e.m.Data(context.Background(), w.ID, "en", true); d.View.Title != fmt.Sprintf("n=%d", n) {
		t.Errorf("final: %s", d.View.Title)
	}
	if d, _ := e.m.Data(context.Background(), other.ID, "en", true); d.View.Title != "n=0" {
		t.Errorf("another instance changed: %s", d.View.Title)
	}
}

func TestActionsOfDifferentInstancesRunConcurrently(t *testing.T) {
	e, w1 := ctrEnv(t)
	w2 := addWidget(t, e, "ctr", "c", nil)
	start := time.Now()
	var wg sync.WaitGroup
	for _, id := range []string{w1.ID, w2.ID} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.m.Action(context.Background(), id, ActionRequest{Action: "slow"}, "en"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if d := time.Since(start); d > 550*time.Millisecond {
		t.Errorf("two instances took %v: they must not block each other (2 x 300ms)", d)
	}
}

func TestActionWaitingForTheLockHonorsContext(t *testing.T) {
	e, w := ctrEnv(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.m.Action(context.Background(), w.ID, ActionRequest{Action: "slow"}, "en")
	}()
	time.Sleep(60 * time.Millisecond) // the slow action holds the lock
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := e.m.Action(ctx, w.ID, ActionRequest{Action: "inc"}, "en"); err != context.DeadlineExceeded {
		t.Errorf("waiting action: %v", err)
	}
	<-done
	// the abandoned action did not run
	if d, _ := e.m.Data(context.Background(), w.ID, "en", true); d.View.Title != "n=0" {
		t.Errorf("abandoned action ran: %s", d.View.Title)
	}
}

// A widget() call that was already running when an action finished must not
// overwrite the action's fresher cached view.
func TestStaleDataDoesNotOverwriteActionView(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"slowv", name:"S", version:"1", widgets:[{id:"w",name:"W",refresh:300}]}`, `
export async function widget(ctx) {
  const n = ctx.instance.storage.get("n") || 0;
  await new Promise((r) => setTimeout(r, 200)); // the view is built from n, but delivered late
  return { title: "n=" + n, body: [] };
}
export async function action(ctx) {
  ctx.instance.storage.set("n", 5);
  return { view: { title: "n=5", body: [] } };
}`))
	w := addWidget(t, e, "slowv", "w", nil)
	ctx := context.Background()
	got := make(chan WidgetData, 1)
	go func() { d, _ := e.m.Data(ctx, w.ID, "en", false); got <- d }()
	time.Sleep(60 * time.Millisecond) // widget() is now running with n=0
	if _, err := e.m.Action(ctx, w.ID, ActionRequest{Action: "set"}, "en"); err != nil {
		t.Fatal(err)
	}
	if d := <-got; d.View.Title != "n=0" {
		t.Fatalf("the slow call returns what it computed: %s", d.View.Title)
	}
	if d, _ := e.m.Data(ctx, w.ID, "en", false); d.View.Title != "n=5" {
		t.Errorf("cache was overwritten by a stale view: %s", d.View.Title)
	}
}

func TestTestEndpointSupportsActionView(t *testing.T) {
	e, w := ctrEnv(t)
	res, err := e.m.Test(context.Background(), "ctr", TestRequest{Hook: "action", Action: "inc", Instance: w.ID})
	if err != nil || !res.OK {
		t.Fatalf("%v %+v", err, res)
	}
	ar, ok := res.Result.(ActionResult)
	if !ok || ar.View == nil || ar.View.View.Title != "n=1" || ar.View.View.Tone != "blue" || ar.View.NextAt-ar.View.UpdatedAt != 120_000 || len(ar.View.View.Body) != 1 {
		t.Errorf("result: %#v", res.Result)
	}
	// unsaved instance: scratch storage, nothing persisted
	res, _ = e.m.Test(context.Background(), "ctr", TestRequest{Hook: "action", Action: "inc"})
	if ar := res.Result.(ActionResult); ar.View == nil || ar.View.View.Title != "n=1" {
		t.Errorf("scratch: %#v", res.Result)
	}
	if d, _ := e.m.Data(context.Background(), w.ID, "en", true); d.View.Title != "n=1" {
		t.Errorf("the scratch run must not touch the saved instance: %s", d.View.Title)
	}
	// params limits apply to the test endpoint too
	if _, err := e.m.Test(context.Background(), "ctr", TestRequest{Hook: "action", Action: "echo", Params: map[string]any{"value": strings.Repeat("a", 20001)}}); store.ErrKind(err) != store.KindInvalid {
		t.Errorf("limits: %v", err)
	}
}

// Seeding ------------------------------------------------------------------------------------

func seedEnv(t *testing.T) *tenv { return catalogEnv(t) }

func installedIDs(e *tenv) map[string]bool {
	out := map[string]bool{}
	for _, p := range e.st.Plugins() {
		out[p.ID] = true
	}
	return out
}

func TestSeedFreshInstall(t *testing.T) {
	e := seedEnv(t)
	e.m.SeedDefaults(context.Background())
	got := installedIDs(e)
	if !got["todo"] || !got["note"] || len(got) != 2 {
		t.Fatalf("installed: %v", got)
	}
	for _, id := range []string{"todo", "note"} {
		rec, _ := e.st.GetPlugin(id)
		if !rec.Enabled {
			t.Errorf("%s must be enabled", id)
		}
	}
	if s := e.st.Settings().PluginsSeeded; strings.Join(s, ",") != "todo,note" {
		t.Errorf("plugins_seeded: %v", s)
	}
	if len(e.st.Snapshot().Widgets) != 0 {
		t.Error("seeding must not add widgets")
	}
	// idempotent
	e.m.SeedDefaults(context.Background())
	if s := e.st.Settings().PluginsSeeded; len(s) != 2 {
		t.Errorf("second run: %v", s)
	}
}

func TestSeedAlreadyInstalledIsKeptAndRecorded(t *testing.T) {
	e := seedEnv(t)
	// the admin already has a todo (here: disabled, with a widget): untouched, but recorded
	installCatalog(t, e, "todo")
	f := false
	if _, err := e.m.Patch("todo", PatchRequest{Enabled: &f}); err != nil {
		t.Fatal(err)
	}
	e.m.SeedDefaults(context.Background())
	rec, ok := e.st.GetPlugin("todo")
	if !ok || rec.Enabled {
		t.Errorf("an installed plugin must not be changed: %+v", rec)
	}
	if !installedIDs(e)["note"] {
		t.Error("note must be installed")
	}
	if s := e.st.Settings().PluginsSeeded; strings.Join(s, ",") != "todo,note" {
		t.Errorf("plugins_seeded: %v", s)
	}
}

func TestSeedUninstalledStaysUninstalled(t *testing.T) {
	e := seedEnv(t)
	ctx := context.Background()
	e.m.SeedDefaults(ctx)
	if err := e.m.Uninstall("todo"); err != nil {
		t.Fatal(err)
	}
	e.m.SeedDefaults(ctx) // "next start"
	got := installedIDs(e)
	if got["todo"] || !got["note"] {
		t.Errorf("installed: %v", got)
	}
}

func TestSeedFailureIsNotRecorded(t *testing.T) {
	e := seedEnv(t)
	ctx := context.Background()
	// "note" is missing from the catalog this time: it fails, "todo" still works
	e.m.cfg.Catalog = mapFS(map[string]string{"todo.js": mustCatalogSource(t, "todo")})
	e.m.SeedDefaults(ctx)
	got := installedIDs(e)
	if !got["todo"] || got["note"] {
		t.Fatalf("installed: %v", got)
	}
	if s := e.st.Settings().PluginsSeeded; strings.Join(s, ",") != "todo" {
		t.Fatalf("a failure must not be recorded: %v", s)
	}
	// next start with a healthy catalog: retried
	e.m.cfg.Catalog = catalogEnv(t).m.cfg.Catalog
	e.m.catOnce = sync.Once{}
	e.m.catalog, e.m.catSrc = nil, nil
	e.m.SeedDefaults(ctx)
	if got := installedIDs(e); !got["note"] {
		t.Errorf("retry: %v", got)
	}
	if s := e.st.Settings().PluginsSeeded; strings.Join(s, ",") != "todo,note" {
		t.Errorf("after retry: %v", s)
	}
}

func mustCatalogSource(t *testing.T, id string) string {
	t.Helper()
	src, ok := catalogEnv(t).m.catalogSource(id)
	if !ok {
		t.Fatalf("no catalog plugin %s", id)
	}
	return src
}

func TestCatalogItemsListWidgets(t *testing.T) {
	e := catalogEnv(t)
	var todo, note, calc *CatalogItem
	cat := e.m.Catalog()
	for i := range cat {
		switch cat[i].ID {
		case "todo":
			todo = &cat[i]
		case "note":
			note = &cat[i]
		case "calculator":
			calc = &cat[i]
		}
	}
	if todo == nil || note == nil || calc == nil {
		t.Fatal("catalog items missing")
	}
	if len(todo.Widgets) != 1 || todo.Widgets[0].ID != "list" || todo.Widgets[0].Size != "m" ||
		todo.Widgets[0].Name.Resolve("zh") != "待办" || todo.Widgets[0].Name.Resolve("en") != "To-do" {
		t.Errorf("todo widgets: %+v", todo.Widgets)
	}
	if len(note.Widgets) != 1 || note.Widgets[0].ID != "note" || note.Widgets[0].Name.Resolve("zh") != "便签" {
		t.Errorf("note widgets: %+v", note.Widgets)
	}
	// a plugin without widgets has an empty array, never null
	b, _ := json.Marshal(calc)
	if !strings.Contains(string(b), `"widgets":[]`) {
		t.Errorf("calculator: %s", b)
	}
	// localized objects like Info.widgets
	b, _ = json.Marshal(todo.Widgets)
	if !strings.Contains(string(b), `"zh":"待办"`) || !strings.Contains(string(b), `"size":"m"`) {
		t.Errorf("json: %s", b)
	}
	// installed plugins show the same widget names in Infos
	installCatalog(t, e, "todo")
	for _, inf := range e.m.Infos(true) {
		if inf.ID == "todo" && (len(inf.Widgets) != 1 || inf.Widgets[0].Name.Resolve("en") != todo.Widgets[0].Name.Resolve("en")) {
			t.Errorf("info widgets: %+v", inf.Widgets)
		}
	}
}
