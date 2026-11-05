package plugin

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// sv sanitizes a view with the given body blocks.
func sv(blocks ...any) WidgetView { return SanitizeView(map[string]any{"body": blocks}) }

func block0(t *testing.T, v WidgetView) map[string]any {
	t.Helper()
	if len(v.Body) != 1 {
		t.Fatalf("expected 1 block, got %d: %+v", len(v.Body), v.Body)
	}
	return v.Body[0]
}

func items(b map[string]any) []map[string]any { return b["items"].([]map[string]any) }

func TestSanitizeInputBlock(t *testing.T) {
	b := block0(t, sv(map[string]any{
		"type": "input", "action": "add", "params": map[string]any{"list": "a"},
		"placeholder": "  Add a   task ", "submit": "Add", "icon": "plus", "maxLength": 100.0, "value": "hello",
		"junk": 1,
	}))
	want := map[string]any{
		"type": "input", "action": "add", "params": map[string]any{"list": "a"},
		"placeholder": "Add a task", "submit": "Add", "icon": "plus", "maxLength": 100, "value": "hello",
	}
	if !reflect.DeepEqual(b, want) {
		t.Errorf("got %#v", b)
	}

	// minimal: defaults
	b = block0(t, sv(map[string]any{"type": "input", "action": "x"}))
	if !reflect.DeepEqual(b, map[string]any{"type": "input", "action": "x", "maxLength": 500}) {
		t.Errorf("minimal: %#v", b)
	}

	// rejections: missing / invalid action names drop the block
	for _, a := range []any{nil, "", "has space", "a/b", strings.Repeat("a", 41), 5.0, []any{"x"}} {
		if v := sv(map[string]any{"type": "input", "action": a}); len(v.Body) != 0 {
			t.Errorf("action %v should drop the block: %+v", a, v.Body)
		}
	}

	// maxLength is clamped to 1..20000; garbage falls back to the default
	for in, want := range map[any]int{0.0: 1, -5.0: 1, 1.0: 1, 20000.0: 20000, 99999.0: 20000, "x": 500, nil: 500, 12.7: 12} {
		b := block0(t, sv(map[string]any{"type": "input", "action": "a", "maxLength": in}))
		if b["maxLength"] != want {
			t.Errorf("maxLength %v -> %v, want %d", in, b["maxLength"], want)
		}
	}

	// value is cut at maxLength; params are dropped (block kept) when too big or not an object, like button
	b = block0(t, sv(map[string]any{"type": "input", "action": "a", "maxLength": 5.0, "value": "abcdefgh", "params": map[string]any{"x": strings.Repeat("y", 3000)}}))
	if b["value"] != "abcde" {
		t.Errorf("value: %v", b["value"])
	}
	if _, has := b["params"]; has {
		t.Error("oversized params must be dropped")
	}
	if b := block0(t, sv(map[string]any{"type": "input", "action": "a", "params": "str"})); b["params"] != nil {
		t.Error("non-object params must be dropped")
	}

	// bad icon dropped, placeholder / submit truncated at 200
	b = block0(t, sv(map[string]any{"type": "input", "action": "a", "icon": "Not Valid!", "placeholder": strings.Repeat("p", 300), "submit": strings.Repeat("s", 300)}))
	if _, has := b["icon"]; has || len([]rune(b["placeholder"].(string))) != 200 || len([]rune(b["submit"].(string))) != 200 {
		t.Errorf("fields: %#v", b)
	}
}

func listItem(extra map[string]any) map[string]any {
	m := map[string]any{"title": "t"}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestSanitizeListItemInteractiveFields(t *testing.T) {
	b := block0(t, sv(map[string]any{"type": "list", "sortAction": "reorder", "empty": " nothing here ", "items": []any{
		map[string]any{
			"title": "One", "id": "a-1",
			"check": map[string]any{"checked": true, "action": "toggle", "params": map[string]any{"id": "a-1"}},
			"edit":  map[string]any{"action": "rename", "params": map[string]any{"id": "a-1"}},
			"actions": []any{
				map[string]any{"icon": "trash-2", "label": "Delete", "action": "remove", "params": map[string]any{"id": "a-1"}},
			},
		},
		map[string]any{"title": "Two", "id": "b.2:x_y"},
	}}))
	got := items(b)
	if len(got) != 2 {
		t.Fatalf("items: %#v", got)
	}
	want0 := map[string]any{
		"title": "One", "id": "a-1",
		"check":   map[string]any{"checked": true, "action": "toggle", "params": map[string]any{"id": "a-1"}},
		"edit":    map[string]any{"action": "rename", "params": map[string]any{"id": "a-1"}},
		"actions": []map[string]any{{"icon": "trash-2", "label": "Delete", "action": "remove", "params": map[string]any{"id": "a-1"}}},
	}
	if !reflect.DeepEqual(got[0], want0) {
		t.Errorf("item 0:\n got %#v\nwant %#v", got[0], want0)
	}
	if b["sortAction"] != "reorder" || b["empty"] != "nothing here" {
		t.Errorf("list fields: %#v", b)
	}
	// the result must be JSON serializable as the API sends it
	if _, err := json.Marshal(sv(b)); err != nil {
		t.Error(err)
	}
}

func TestSanitizeListItemRejections(t *testing.T) {
	big := map[string]any{"x": strings.Repeat("y", 3000)}
	cases := map[string]map[string]any{
		"bad id chars":        {"id": "has space"},
		"id with slash":       {"id": "a/b"},
		"empty id":            {"id": ""},
		"id too long":         {"id": strings.Repeat("a", 65)},
		"numeric id":          {"id": 5.0},
		"check bad action":    {"check": map[string]any{"checked": true, "action": "no good"}},
		"check no action":     {"check": map[string]any{"checked": true}},
		"check big params":    {"check": map[string]any{"action": "t", "params": big}},
		"check not object":    {"check": "yes"},
		"edit bad action":     {"edit": map[string]any{"action": ""}},
		"edit big params":     {"edit": map[string]any{"action": "r", "params": big}},
		"edit array params":   {"edit": map[string]any{"action": "r", "params": []any{1}}},
		"actions not a list":  {"actions": map[string]any{"icon": "x", "label": "y", "action": "z"}},
		"action no icon":      {"actions": []any{map[string]any{"label": "L", "action": "a"}}},
		"action bad icon":     {"actions": []any{map[string]any{"icon": "Bad Icon", "label": "L", "action": "a"}}},
		"action no label":     {"actions": []any{map[string]any{"icon": "x", "action": "a"}}},
		"action blank label":  {"actions": []any{map[string]any{"icon": "x", "label": "   ", "action": "a"}}},
		"action bad name":     {"actions": []any{map[string]any{"icon": "x", "label": "L", "action": "a b"}}},
		"action big params":   {"actions": []any{map[string]any{"icon": "x", "label": "L", "action": "a", "params": big}}},
		"action not an obj":   {"actions": []any{"nope", 3.0}},
		"all actions invalid": {"actions": []any{map[string]any{"icon": "x"}, map[string]any{"label": "L"}}},
	}
	for name, extra := range cases {
		b := block0(t, sv(map[string]any{"type": "list", "items": []any{listItem(extra)}}))
		got := items(b)
		if len(got) != 1 {
			t.Errorf("%s: the item itself must be kept: %#v", name, got)
			continue
		}
		for _, k := range []string{"check", "edit", "actions"} {
			if _, has := got[0][k]; has {
				t.Errorf("%s: %s must be dropped: %#v", name, k, got[0])
			}
		}
		if _, has := got[0]["id"]; has && name != "check bad action" && strings.HasPrefix(name, "bad id") {
			t.Errorf("%s: id must be dropped: %#v", name, got[0])
		}
	}
	// invalid ids are dropped, valid siblings kept
	for _, bad := range []any{"has space", "a/b", "", strings.Repeat("a", 65), 5.0, nil} {
		got := items(block0(t, sv(map[string]any{"type": "list", "items": []any{listItem(map[string]any{"id": bad})}})))
		if _, has := got[0]["id"]; has {
			t.Errorf("id %v must be dropped", bad)
		}
	}
	if got := items(block0(t, sv(map[string]any{"type": "list", "items": []any{listItem(map[string]any{"id": strings.Repeat("a", 64)})}}))); got[0]["id"] == nil {
		t.Error("a 64 character id is valid")
	}
}

func TestSanitizeListItemActionsLimitAndCheckDefaults(t *testing.T) {
	var acts []any
	for i := 0; i < 5; i++ {
		acts = append(acts, map[string]any{"icon": "x", "label": fmt.Sprintf("L%d", i), "action": fmt.Sprintf("a%d", i)})
	}
	// an invalid button in front does not count against the limit
	acts = append([]any{map[string]any{"icon": "x"}}, acts...)
	got := items(block0(t, sv(map[string]any{"type": "list", "items": []any{listItem(map[string]any{"actions": acts, "check": map[string]any{"action": "t"}})}})))
	as := got[0]["actions"].([]map[string]any)
	if len(as) != 3 || as[0]["label"] != "L0" || as[2]["label"] != "L2" {
		t.Errorf("actions: %#v", as)
	}
	// checked defaults to false and is always a bool
	if c := got[0]["check"].(map[string]any); c["checked"] != false || c["action"] != "t" {
		t.Errorf("check: %#v", c)
	}
	// non-bool checked is false
	got = items(block0(t, sv(map[string]any{"type": "list", "items": []any{listItem(map[string]any{"check": map[string]any{"action": "t", "checked": "yes"}})}})))
	if got[0]["check"].(map[string]any)["checked"] != false {
		t.Error("checked must be a real bool")
	}
}

func TestSanitizeListSortAction(t *testing.T) {
	mk := func(sort any, its ...any) WidgetView {
		return sv(map[string]any{"type": "list", "sortAction": sort, "items": its})
	}
	withID := func(id string) any { return map[string]any{"title": "t" + id, "id": id} }
	noID := map[string]any{"title": "no id"}

	if b := block0(t, mk("reorder", withID("a"), withID("b"))); b["sortAction"] != "reorder" {
		t.Errorf("sortAction should be kept: %#v", b)
	}
	// an item without an id drops it
	if b := block0(t, mk("reorder", withID("a"), noID)); b["sortAction"] != nil {
		t.Errorf("an item lacks an id: %#v", b)
	}
	// an item whose id is invalid counts as having none
	if b := block0(t, mk("reorder", withID("a"), map[string]any{"title": "x", "id": "bad id"})); b["sortAction"] != nil {
		t.Errorf("invalid id: %#v", b)
	}
	// duplicate ids make the order ambiguous
	if b := block0(t, mk("reorder", withID("a"), withID("a"))); b["sortAction"] != nil {
		t.Errorf("duplicate ids: %#v", b)
	}
	// items that are dropped anyway (no title) do not count
	if b := block0(t, mk("reorder", withID("a"), map[string]any{"title": "  "})); b["sortAction"] != "reorder" {
		t.Errorf("dropped item must not count: %#v", b)
	}
	// bad action names
	for _, bad := range []any{"", "a b", strings.Repeat("a", 41), 7.0, nil} {
		if b := block0(t, mk(bad, withID("a"))); b["sortAction"] != nil {
			t.Errorf("sortAction %v must be dropped", bad)
		}
	}
	// an empty list keeps it (all zero items have ids): the browser sorts nothing
	if b := block0(t, mk("reorder")); b["sortAction"] != "reorder" {
		t.Errorf("empty list: %#v", b)
	}
}

func TestSanitizeListLimitsAndEmpty(t *testing.T) {
	many := func(n int, extra func(i int) map[string]any) []any {
		var out []any
		for i := 0; i < n; i++ {
			m := map[string]any{"title": fmt.Sprintf("item %d", i)}
			for k, v := range extra(i) {
				m[k] = v
			}
			out = append(out, m)
		}
		return out
	}
	plain := func(int) map[string]any { return nil }
	withID := func(i int) map[string]any { return map[string]any{"id": fmt.Sprintf("i%d", i)} }
	withCheck := func(i int) map[string]any {
		return map[string]any{"id": fmt.Sprintf("i%d", i), "check": map[string]any{"action": "t"}}
	}

	// plain lists stay at 20
	if n := len(items(block0(t, sv(map[string]any{"type": "list", "items": many(300, plain)})))); n != 20 {
		t.Errorf("plain list: %d", n)
	}
	// ids alone are not interactive
	if n := len(items(block0(t, sv(map[string]any{"type": "list", "items": many(300, withID)})))); n != 20 {
		t.Errorf("ids only: %d", n)
	}
	// check / edit / actions allow 200
	if n := len(items(block0(t, sv(map[string]any{"type": "list", "items": many(300, withCheck)})))); n != 200 {
		t.Errorf("check list: %d", n)
	}
	edit := func(int) map[string]any { return map[string]any{"edit": map[string]any{"action": "r"}} }
	if n := len(items(block0(t, sv(map[string]any{"type": "list", "items": many(300, edit)})))); n != 200 {
		t.Errorf("edit list: %d", n)
	}
	acts := func(int) map[string]any {
		return map[string]any{"actions": []any{map[string]any{"icon": "x", "label": "l", "action": "a"}}}
	}
	if n := len(items(block0(t, sv(map[string]any{"type": "list", "items": many(300, acts)})))); n != 200 {
		t.Errorf("actions list: %d", n)
	}
	// sortAction alone (all items have ids) allows 200
	if n := len(items(block0(t, sv(map[string]any{"type": "list", "sortAction": "s", "items": many(300, withID)})))); n != 200 {
		t.Errorf("sortAction list: %d", n)
	}
	// ... but a sortAction that gets dropped (ids missing) leaves a plain list: 20
	if n := len(items(block0(t, sv(map[string]any{"type": "list", "sortAction": "s", "items": many(300, plain)})))); n != 20 {
		t.Errorf("dropped sortAction: %d", n)
	}
	// invalid interactive objects do not unlock the larger limit
	badCheck := func(int) map[string]any { return map[string]any{"check": map[string]any{"action": "bad action"}} }
	if n := len(items(block0(t, sv(map[string]any{"type": "list", "items": many(300, badCheck)})))); n != 20 {
		t.Errorf("invalid check: %d", n)
	}

	// an empty list is kept; its `empty` text is cleaned and capped
	b := block0(t, sv(map[string]any{"type": "list", "items": []any{}, "empty": "  No\n tasks  "}))
	if b["empty"] != "No tasks" || len(items(b)) != 0 {
		t.Errorf("empty list: %#v", b)
	}
	b = block0(t, sv(map[string]any{"type": "list", "empty": strings.Repeat("e", 300)}))
	if len([]rune(b["empty"].(string))) != 200 {
		t.Errorf("empty cap: %d", len(b["empty"].(string)))
	}
	if _, has := block0(t, sv(map[string]any{"type": "list", "items": []any{}}))["empty"]; has {
		t.Error("no empty text: field must be absent")
	}
	// the block budget still limits a view (a 200-item list is one block)
	if v := sv(map[string]any{"type": "list", "items": many(200, withCheck)}); len(v.Body) != 1 {
		t.Error("budget")
	}
}

func TestSanitizeMarkdownBlock(t *testing.T) {
	text := "# Hi\r\n\r\n- [ ] task\n\twith tab\n  two  spaces \n"
	b := block0(t, sv(map[string]any{"type": "markdown", "text": text, "editAction": "save", "params": map[string]any{"k": "v"}, "placeholder": " Write… ", "extra": 1}))
	want := map[string]any{"type": "markdown", "text": "# Hi\n\n- [ ] task\n\twith tab\n  two  spaces \n", "editAction": "save", "params": map[string]any{"k": "v"}, "placeholder": "Write…"}
	if !reflect.DeepEqual(b, want) {
		t.Errorf("got %#v", b)
	}

	// control characters are stripped, newline and tab kept, whitespace not collapsed or trimmed
	b = block0(t, sv(map[string]any{"type": "markdown", "text": "a\x00b\x07c\x1bd\x7fe\u0085f\rg\n\th "}))
	if b["text"] != "abcdef"+"g\n\th " {
		t.Errorf("control chars: %q", b["text"])
	}

	// empty text is a valid, empty note; a non-string text drops the block
	if b := block0(t, sv(map[string]any{"type": "markdown", "text": ""})); b["text"] != "" {
		t.Errorf("empty text: %#v", b)
	}
	for _, bad := range []any{nil, 5.0, []any{"x"}, map[string]any{}} {
		if v := sv(map[string]any{"type": "markdown", "text": bad}); len(v.Body) != 0 {
			t.Errorf("text %v should drop the block", bad)
		}
	}
	if v := sv(map[string]any{"type": "markdown"}); len(v.Body) != 0 {
		t.Error("missing text should drop the block")
	}

	// 20000 character cap (characters, not bytes)
	b = block0(t, sv(map[string]any{"type": "markdown", "text": strings.Repeat("好", 25000)}))
	if n := len([]rune(b["text"].(string))); n != 20000 {
		t.Errorf("cap: %d", n)
	}

	// editAction rules: invalid -> read-only block, params dropped with it
	for _, bad := range []any{"", "bad action", strings.Repeat("a", 41), 3.0} {
		b := block0(t, sv(map[string]any{"type": "markdown", "text": "x", "editAction": bad, "params": map[string]any{"k": 1}}))
		if _, has := b["editAction"]; has {
			t.Errorf("editAction %v must be dropped", bad)
		}
		if _, has := b["params"]; has {
			t.Errorf("params without editAction must be dropped")
		}
	}
	// oversized params are dropped but the block stays editable
	b = block0(t, sv(map[string]any{"type": "markdown", "text": "x", "editAction": "save", "params": map[string]any{"k": strings.Repeat("y", 3000)}}))
	if b["editAction"] != "save" || b["params"] != nil {
		t.Errorf("big params: %#v", b)
	}
	if b := block0(t, sv(map[string]any{"type": "markdown", "text": "x", "placeholder": strings.Repeat("p", 300)})); len([]rune(b["placeholder"].(string))) != 200 {
		t.Error("placeholder cap")
	}
}

func TestSanitizeViewTone(t *testing.T) {
	for _, tone := range []string{"yellow", "pink", "blue", "green", "purple"} {
		if v := SanitizeView(map[string]any{"tone": tone}); v.Tone != tone {
			t.Errorf("%s: %q", tone, v.Tone)
		}
	}
	for _, bad := range []any{"default", "red", "Yellow", " yellow", "", 3.0, nil, []any{"blue"}} {
		if v := SanitizeView(map[string]any{"tone": bad}); v.Tone != "" {
			t.Errorf("tone %v must be dropped, got %q", bad, v.Tone)
		}
	}
	// JSON: the field is only present when set
	b, _ := json.Marshal(SanitizeView(map[string]any{"tone": "pink", "body": []any{}}))
	if !strings.Contains(string(b), `"tone":"pink"`) {
		t.Errorf("json: %s", b)
	}
	b, _ = json.Marshal(SanitizeView(map[string]any{"body": []any{}}))
	if strings.Contains(string(b), "tone") {
		t.Errorf("json: %s", b)
	}
}

func TestSanitizeActionView(t *testing.T) {
	r := SanitizeAction(map[string]any{"toast": "ok", "view": map[string]any{"title": "T", "tone": "blue", "body": []any{map[string]any{"type": "markdown", "text": "hi"}, map[string]any{"type": "script"}}}})
	if r.Toast != "ok" || r.View == nil || r.View.View.Title != "T" || r.View.View.Tone != "blue" || len(r.View.View.Body) != 1 {
		t.Errorf("%+v", r)
	}
	// not an object: ignored
	for _, v := range []any{"x", 1.0, []any{}, nil, true} {
		if r := SanitizeAction(map[string]any{"view": v}); r.View != nil {
			t.Errorf("view %v must be ignored", v)
		}
	}
	// existing behavior is untouched
	if r := SanitizeAction(map[string]any{"refresh": true}); !r.Refresh || r.View != nil {
		t.Errorf("%+v", r)
	}
}
