package plugin

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// drive helpers: the built-in todo and note plugins, loaded through the real Manager.

type todoRow struct {
	ID, Title string
	Checked   bool
}

func todoRows(t *testing.T, v WidgetView) (rows []todoRow, list map[string]any) {
	t.Helper()
	for _, b := range v.Body {
		if b["type"] == "list" {
			list = b
		}
	}
	if list == nil {
		t.Fatalf("no list block: %+v", v.Body)
	}
	for _, it := range list["items"].([]map[string]any) {
		rows = append(rows, todoRow{ID: it["id"].(string), Title: it["title"].(string), Checked: it["check"].(map[string]any)["checked"].(bool)})
	}
	return rows, list
}

func titles(rows []todoRow) string {
	var s []string
	for _, r := range rows {
		m := ""
		if r.Checked {
			m = "*"
		}
		s = append(s, r.Title+m)
	}
	return strings.Join(s, ",")
}

type todoDriver struct {
	t    *testing.T
	e    *tenv
	id   string
	lang string
}

func newTodo(t *testing.T) *todoDriver {
	e := catalogEnv(t)
	installCatalog(t, e, "todo")
	w := addWidget(t, e, "todo", "list", nil)
	return &todoDriver{t: t, e: e, id: w.ID, lang: "en"}
}

func (d *todoDriver) act(action string, params map[string]any) (ActionResult, error) {
	return d.e.m.Action(context.Background(), d.id, ActionRequest{Action: action, Params: params}, d.lang)
}

// do runs an action that must succeed and returns the rows of the view it returned.
func (d *todoDriver) do(action string, params map[string]any) ([]todoRow, WidgetView) {
	d.t.Helper()
	res, err := d.act(action, params)
	if err != nil {
		d.t.Fatalf("%s: %v", action, err)
	}
	if res.View == nil {
		d.t.Fatalf("%s: no view returned", action)
	}
	rows, _ := todoRows(d.t, res.View.View)
	return rows, res.View.View
}

func (d *todoDriver) add(text string) {
	d.t.Helper()
	d.do("add", map[string]any{"value": text})
}

func (d *todoDriver) idOf(text string) string {
	d.t.Helper()
	d.e.m.forgetWidget(d.id)
	data, err := d.e.m.Data(context.Background(), d.id, d.lang, true)
	if err != nil {
		d.t.Fatal(err)
	}
	rows, _ := todoRows(d.t, data.View)
	for _, r := range rows {
		if r.Title == text {
			return r.ID
		}
	}
	d.t.Fatalf("no item %q in %s", text, titles(rows))
	return ""
}

func TestTodoEmptyView(t *testing.T) {
	d := newTodo(t)
	data, err := d.e.m.Data(context.Background(), d.id, "en", false)
	if err != nil {
		t.Fatal(err)
	}
	v := data.View
	if v.Title != "To-do" || v.Icon != "list-todo" || v.Error != "" || len(v.Body) != 2 {
		t.Fatalf("view: %+v", v)
	}
	in := v.Body[0]
	if in["type"] != "input" || in["action"] != "add" || in["icon"] != "plus" || in["maxLength"] != 500 || in["placeholder"] != "Add a task" || in["submit"] != "Add" {
		t.Errorf("input: %#v", in)
	}
	_, list := todoRows(t, v)
	if list["sortAction"] != "reorder" || list["empty"] != "Nothing to do. Enjoy!" || len(list["items"].([]map[string]any)) != 0 {
		t.Errorf("list: %#v", list)
	}
	// Chinese
	data, _ = d.e.m.Data(context.Background(), d.id, "zh", false)
	if data.View.Title != "待办" || data.View.Body[0]["placeholder"] != "添加待办" {
		t.Errorf("zh: %+v", data.View)
	}
}

func TestTodoAddToggleRenameRemove(t *testing.T) {
	d := newTodo(t)
	d.add("  Buy   milk ")
	d.add("Walk the dog")
	rows, v := d.do("add", map[string]any{"value": "Write docs"})
	if titles(rows) != "Buy milk,Walk the dog,Write docs" || v.Title != "To-do · 3" {
		t.Fatalf("after add: %s / %q", titles(rows), v.Title)
	}
	// every row has the full interactive shape after sanitizing
	_, list := todoRows(t, v)
	it := list["items"].([]map[string]any)[0]
	id := it["id"].(string)
	if it["check"].(map[string]any)["action"] != "toggle" || it["check"].(map[string]any)["params"].(map[string]any)["id"] != id ||
		it["edit"].(map[string]any)["action"] != "rename" || it["edit"].(map[string]any)["params"].(map[string]any)["id"] != id {
		t.Errorf("row: %#v", it)
	}
	a := it["actions"].([]map[string]any)
	if len(a) != 1 || a[0]["icon"] != "trash-2" || a[0]["action"] != "remove" || a[0]["label"] != "Delete" || a[0]["params"].(map[string]any)["id"] != id {
		t.Errorf("actions: %#v", a)
	}

	// toggle with explicit checked: the done item moves behind the open ones, title counts open items
	rows, v = d.do("toggle", map[string]any{"id": d.idOf("Buy milk"), "checked": true})
	if titles(rows) != "Walk the dog,Write docs,Buy milk*" || v.Title != "To-do · 2" {
		t.Fatalf("after toggle: %s / %q", titles(rows), v.Title)
	}
	// the clear button appears with a count
	last := v.Body[len(v.Body)-1]
	if last["type"] != "button" || last["action"] != "clear" || last["icon"] != "eraser" || last["label"] != "Clear finished (1)" {
		t.Errorf("clear button: %#v", last)
	}
	// toggle without `checked` flips
	rows, _ = d.do("toggle", map[string]any{"id": d.idOf("Buy milk")})
	if titles(rows) != "Buy milk,Walk the dog,Write docs" {
		t.Fatalf("flip back: %s", titles(rows))
	}
	// a stale checked=false for an open item is a no-op
	rows, _ = d.do("toggle", map[string]any{"id": d.idOf("Walk the dog"), "checked": false})
	if titles(rows) != "Buy milk,Walk the dog,Write docs" {
		t.Fatalf("no-op: %s", titles(rows))
	}

	rows, _ = d.do("rename", map[string]any{"id": d.idOf("Write docs"), "value": "  Write   the docs  "})
	if titles(rows) != "Buy milk,Walk the dog,Write the docs" {
		t.Fatalf("rename: %s", titles(rows))
	}
	rows, _ = d.do("remove", map[string]any{"id": d.idOf("Walk the dog")})
	if titles(rows) != "Buy milk,Write the docs" {
		t.Fatalf("remove: %s", titles(rows))
	}
	// ids are unique and valid for the sanitizer (sortAction survived)
	_, list = todoRows(t, v)
	if list["sortAction"] != "reorder" {
		t.Error("sortAction lost")
	}
	// the view returned by an action is what /data serves next (cache written back)
	data, _ := d.e.m.Data(context.Background(), d.id, "en", false)
	if rows, _ := todoRows(t, data.View); titles(rows) != "Buy milk,Write the docs" {
		t.Errorf("data: %s", titles(rows))
	}
}

func TestTodoReorderAndClear(t *testing.T) {
	d := newTodo(t)
	for _, s := range []string{"a", "b", "c", "d"} {
		d.add(s)
	}
	ids := map[string]string{}
	for _, s := range []string{"a", "b", "c", "d"} {
		ids[s] = d.idOf(s)
	}
	d.do("toggle", map[string]any{"id": ids["b"], "checked": true})
	// the UI shows a,c,d,b* ; the user drags d to the top
	rows, _ := d.do("reorder", map[string]any{"ids": []any{ids["d"], ids["a"], ids["c"], ids["b"]}})
	if titles(rows) != "d,a,c,b*" {
		t.Fatalf("reorder: %s", titles(rows))
	}
	// ids that are unknown are ignored, items that are not listed keep their order at the end
	rows, _ = d.do("reorder", map[string]any{"ids": []any{ids["c"], "nope", 5.0, ids["c"]}})
	if titles(rows) != "c,d,a,b*" {
		t.Fatalf("partial reorder: %s", titles(rows))
	}
	// a non-array is rejected
	if _, err := d.act("reorder", map[string]any{"ids": "x"}); err == nil || !strings.Contains(err.Error(), "Invalid request") {
		t.Errorf("bad ids: %v", err)
	}
	d.do("toggle", map[string]any{"id": ids["a"], "checked": true})
	rows, v := d.do("clear", nil)
	if titles(rows) != "c,d" || v.Title != "To-do · 2" {
		t.Fatalf("clear: %s / %q", titles(rows), v.Title)
	}
	for _, b := range v.Body {
		if b["type"] == "button" {
			t.Error("no clear button without finished items")
		}
	}
}

func TestTodoRejectsBadInput(t *testing.T) {
	d := newTodo(t)
	d.add("keep")
	id := d.idOf("keep")
	cases := []struct {
		name, action string
		params       map[string]any
		want         string
	}{
		{"empty add", "add", map[string]any{"value": ""}, "Please enter some text"},
		{"blank add", "add", map[string]any{"value": " \t\n "}, "Please enter some text"},
		{"control-only add", "add", map[string]any{"value": "\x00\x01"}, "Please enter some text"},
		{"non-string add", "add", map[string]any{"value": 5.0}, "Please enter some text"},
		{"no params add", "add", nil, "Please enter some text"},
		{"empty rename", "rename", map[string]any{"id": id, "value": "  "}, "Please enter some text"},
		{"rename unknown", "rename", map[string]any{"id": "nope", "value": "x"}, "no longer exists"},
		{"toggle unknown", "toggle", map[string]any{"id": "nope"}, "no longer exists"},
		{"toggle no id", "toggle", nil, "no longer exists"},
		{"remove unknown", "remove", map[string]any{"id": "nope"}, "no longer exists"},
		{"unknown action", "frobnicate", nil, "Unknown action: frobnicate"},
	}
	for _, c := range cases {
		_, err := d.act(c.action, c.params)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	// nothing changed
	data, _ := d.e.m.Data(context.Background(), d.id, "en", true)
	if rows, _ := todoRows(t, data.View); titles(rows) != "keep" {
		t.Errorf("state changed by rejected actions: %s", titles(rows))
	}
	// errors are localized
	d.lang = "zh"
	if _, err := d.act("add", map[string]any{"value": ""}); err == nil || !strings.Contains(err.Error(), "请输入内容") {
		t.Errorf("zh error: %v", err)
	}
	if _, err := d.act("remove", map[string]any{"id": "nope"}); err == nil || !strings.Contains(err.Error(), "该条目已不存在") {
		t.Errorf("zh error: %v", err)
	}
}

func TestTodoTextLimitsAndCapacity(t *testing.T) {
	d := newTodo(t)
	rows, _ := d.do("add", map[string]any{"value": strings.Repeat("好", 700)})
	if n := len([]rune(rows[0].Title)); n != 500 {
		t.Errorf("text is cut at 500 characters, got %d", n)
	}
	rows, _ = d.do("add", map[string]any{"value": "line1\nline2\x07\ttab"})
	if rows[1].Title != "line1 line2 tab" {
		t.Errorf("whitespace: %q", rows[1].Title)
	}
	for i := 2; i < 200; i++ {
		d.do("add", map[string]any{"value": fmt.Sprintf("task %d", i)})
	}
	data, _ := d.e.m.Data(context.Background(), d.id, "en", true)
	rows, list := todoRows(t, data.View)
	if len(rows) != 200 || list["sortAction"] != "reorder" {
		t.Fatalf("200 items must all be shown with sorting: %d %v", len(rows), list["sortAction"])
	}
	// the 201st is refused with a localized error
	if _, err := d.act("add", map[string]any{"value": "one too many"}); err == nil || !strings.Contains(err.Error(), "The list is full (max 200 items)") {
		t.Errorf("201st: %v", err)
	}
	d.lang = "zh"
	if _, err := d.act("add", map[string]any{"value": "x"}); err == nil || !strings.Contains(err.Error(), "清单已满（最多 200 条）") {
		t.Errorf("201st zh: %v", err)
	}
	// still within the 256 KiB storage limit with 200 long items
	d.lang = "en"
	d.do("clear", nil)
	for _, r := range rows[:100] {
		d.do("rename", map[string]any{"id": r.ID, "value": strings.Repeat("好", 500)})
	}
}

func TestTodoInstancesAreIndependent(t *testing.T) {
	d := newTodo(t)
	w2 := addWidget(t, d.e, "todo", "list", nil)
	d.add("only in the first")
	data, _ := d.e.m.Data(context.Background(), w2.ID, "en", true)
	if rows, _ := todoRows(t, data.View); len(rows) != 0 {
		t.Errorf("second list: %s", titles(rows))
	}
}

func TestTodoParallelAdds(t *testing.T) {
	d := newTodo(t)
	done := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			_, err := d.act("add", map[string]any{"value": fmt.Sprintf("p%d", i)})
			done <- err
		}()
	}
	for i := 0; i < 20; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	data, _ := d.e.m.Data(context.Background(), d.id, "en", true)
	rows, _ := todoRows(t, data.View)
	if len(rows) != 20 {
		t.Errorf("lost adds: %d of 20", len(rows))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.ID] = true
	}
	if len(seen) != 20 {
		t.Errorf("duplicate ids: %d", len(seen))
	}
}

// Note ----------------------------------------------------------------------------------------

func newNote(t *testing.T, cfg map[string]any) (*tenv, WidgetOut) {
	e := catalogEnv(t)
	installCatalog(t, e, "note")
	return e, addWidget(t, e, "note", "note", cfg)
}

func noteBlock(t *testing.T, v WidgetView) map[string]any {
	t.Helper()
	if v.Title != "" || v.Icon != "" || v.Error != "" || len(v.Body) != 1 || v.Body[0]["type"] != "markdown" {
		t.Fatalf("view: %+v", v)
	}
	return v.Body[0]
}

func TestNoteSaveAndView(t *testing.T) {
	e, w := newNote(t, nil)
	ctx := context.Background()
	data, err := e.m.Data(ctx, w.ID, "en", false)
	if err != nil {
		t.Fatal(err)
	}
	b := noteBlock(t, data.View)
	if b["text"] != "" || b["editAction"] != "save" || !strings.Contains(b["placeholder"].(string), "Double-click") {
		t.Errorf("empty note: %#v", b)
	}
	if data.View.Tone != "yellow" { // the default of the `tone` option
		t.Errorf("default tone: %q", data.View.Tone)
	}

	text := "# Shopping\r\n\r\n- [ ] milk\n- [x] eggs\n\n\tcode\x07 end\n"
	res, err := e.m.Action(ctx, w.ID, ActionRequest{Action: "save", Params: map[string]any{"value": text}}, "en")
	if err != nil || res.View == nil {
		t.Fatalf("save: %v %+v", err, res)
	}
	b = noteBlock(t, res.View.View)
	if b["text"] != "# Shopping\n\n- [ ] milk\n- [x] eggs\n\n\tcode end\n" || b["editAction"] != "save" {
		t.Errorf("saved view: %#v", b)
	}
	// persisted per instance and served by /data from the cache and from the hook
	for _, force := range []bool{false, true} {
		d, _ := e.m.Data(ctx, w.ID, "zh", force)
		if b := noteBlock(t, d.View); b["text"] != "# Shopping\n\n- [ ] milk\n- [x] eggs\n\n\tcode end\n" || !strings.Contains(b["placeholder"].(string), "双击") {
			t.Errorf("data (force=%v): %#v", force, b)
		}
	}
	other := addWidget(t, e, "note", "note", nil)
	if d, _ := e.m.Data(ctx, other.ID, "en", true); noteBlock(t, d.View)["text"] != "" {
		t.Error("notes must be per instance")
	}
	// an empty value clears the note
	res, err = e.m.Action(ctx, w.ID, ActionRequest{Action: "save", Params: map[string]any{"value": ""}}, "en")
	if err != nil || noteBlock(t, res.View.View)["text"] != "" {
		t.Errorf("clear: %v %+v", err, res)
	}
	// 20000 characters are the maximum; the plugin cuts anything longer
	res, err = e.m.Action(ctx, w.ID, ActionRequest{Action: "save", Params: map[string]any{"value": strings.Repeat("好", 20000)}}, "en")
	if err != nil || len([]rune(noteBlock(t, res.View.View)["text"].(string))) != 20000 {
		t.Errorf("20000: %v", err)
	}
}

func TestNoteTone(t *testing.T) {
	ctx := context.Background()
	e, _ := newNote(t, nil)
	for tone, want := range map[string]string{"yellow": "yellow", "pink": "pink", "blue": "blue", "green": "green", "purple": "purple", "default": ""} {
		w := addWidget(t, e, "note", "note", map[string]any{"tone": tone})
		d, err := e.m.Data(ctx, w.ID, "en", true)
		if err != nil || d.View.Tone != want {
			t.Errorf("tone %s: %q %v", tone, d.View.Tone, err)
		}
		// the view returned by save carries the tone too
		res, err := e.m.Action(ctx, w.ID, ActionRequest{Action: "save", Params: map[string]any{"value": "x"}}, "en")
		if err != nil || res.View.View.Tone != want {
			t.Errorf("save with tone %s: %q %v", tone, res.View.View.Tone, err)
		}
	}
	// invalid tones are rejected by the widget config validation
	if _, err := e.m.AddWidget(WidgetAdd{Plugin: "note", Widget: "note", Config: map[string]any{"tone": "red"}}); err == nil {
		t.Error("tone red must be rejected")
	}
}

func TestNoteRejectsBadInput(t *testing.T) {
	e, w := newNote(t, nil)
	ctx := context.Background()
	for name, p := range map[string]any{"no params": nil, "number": map[string]any{"value": 5.0}, "list": map[string]any{"value": []any{"x"}}, "missing": map[string]any{}} {
		if _, err := e.m.Action(ctx, w.ID, ActionRequest{Action: "save", Params: p}, "en"); err == nil || !strings.Contains(err.Error(), "Invalid request") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.m.Action(ctx, w.ID, ActionRequest{Action: "nope"}, "zh"); err == nil || !strings.Contains(err.Error(), "未知操作：nope") {
		t.Errorf("unknown action: %v", err)
	}
}

func TestTodoAndNoteManifests(t *testing.T) {
	e := catalogEnv(t)
	installCatalog(t, e, "todo")
	installCatalog(t, e, "note")
	for _, inf := range e.m.Infos(true) {
		if len(inf.Permissions.Fetch) != 0 || inf.Permissions.Links != "" {
			t.Errorf("%s must not need permissions: %+v", inf.ID, inf.Permissions)
		}
		if inf.LastError != "" || len(inf.Widgets) != 1 || inf.Widgets[0].Size != "m" {
			t.Errorf("%s: %+v", inf.ID, inf)
		}
		for _, l := range []string{"en", "zh"} {
			if inf.Name.Resolve(l) == "" || inf.Description.Resolve(l) == "" || inf.Widgets[0].Name.Resolve(l) == "" {
				t.Errorf("%s lacks a %s translation", inf.ID, l)
			}
		}
		if inf.Name.Resolve("en") == inf.Name.Resolve("zh") {
			t.Errorf("%s: names are not translated", inf.ID)
		}
		switch inf.ID {
		case "todo":
			if inf.Icon != "list-todo" || len(inf.Widgets[0].Config) != 0 {
				t.Errorf("todo: %+v", inf)
			}
		case "note":
			if inf.Icon != "sticky-note" || len(inf.Widgets[0].Config) != 1 || inf.Widgets[0].Config[0].Key != "tone" || inf.Widgets[0].Config[0].Default != "yellow" || len(inf.Widgets[0].Config[0].Options) != 6 {
				t.Errorf("note: %+v", inf.Widgets[0].Config)
			}
		}
	}
}
