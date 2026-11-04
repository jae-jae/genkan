package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/MoeForge/moejs"

	"github.com/jae-jae/genkan/internal/store"
)

func (in *instance) checkFetch(r *http.Request) error {
	if err := in.allow.Load().check(r); err != nil {
		in.e.logs.add("warn", "fetch blocked: "+err.Error(), in.redactList())
		return err
	}
	return nil
}

var errNoHost = errors.New("not available while the plugin is being installed")

// installHost defines the global `genkan` object (docs/PLUGINS.md §2.1) and a
// minimal `console` on the runtime.
func installHost(in *instance, m *Manager, probe bool) error {
	rt, e := in.rt, in.e
	logf := func(level string) moejs.NativeFunc {
		return func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			parts := make([]string, len(args))
			for i, a := range args {
				parts[i] = in.logText(a)
			}
			e.logs.add(level, strings.Join(parts, " "), in.redactList())
			return moejs.Undefined(), nil
		}
	}
	// guard wraps a host function that needs the real plugin context.
	guard := func(fn moejs.NativeFunc) moejs.NativeFunc {
		if !probe {
			return fn
		}
		return func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error) {
			return moejs.Undefined(), errNoHost
		}
	}

	storage := in.storageAPI(guard, kvScope{
		get:  e.kv.get,
		set:  e.kv.set,
		del:  e.kv.delete,
		keys: e.kv.keys,
	})

	// need checks the links permission declared in the manifest.
	level := 0
	if e.man != nil {
		switch e.man.Permissions.Links {
		case "read":
			level = 1
		case "write":
			level = 2
		}
	}
	need := func(l int, what string) error {
		if level < l {
			want := "read"
			if l == 2 {
				want = "write"
			}
			return fmt.Errorf("permission denied: %s needs permissions.links = %q", what, want)
		}
		return nil
	}
	toJS := func(v any) (moejs.Value, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return moejs.Undefined(), err
		}
		return rt.ParseJSON(b)
	}
	objArg := func(args []moejs.Value, i int, dst any) error {
		g, err := rt.ToGoWith(moejs.Arg(args, i), moejs.DecodeOptions{MaxBytes: 64 << 10})
		if err != nil {
			return err
		}
		if _, ok := g.(map[string]any); !ok {
			return errors.New("expected an object")
		}
		b, _ := json.Marshal(g)
		return json.Unmarshal(b, dst)
	}
	st := m.st
	links := map[string]any{
		"list": moejs.NativeFunc(guard(func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error) {
			if err := need(1, "genkan.links.list"); err != nil {
				return moejs.Undefined(), err
			}
			return toJS(st.Snapshot().Links)
		})),
		"create": moejs.NativeFunc(guard(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			if err := need(2, "genkan.links.create"); err != nil {
				return moejs.Undefined(), err
			}
			var inp store.LinkInput
			if err := objArg(args, 0, &inp); err != nil {
				return moejs.Undefined(), err
			}
			l, err := st.CreateLink(inp)
			if err != nil {
				return moejs.Undefined(), err
			}
			if m.cfg.OnLinksCreated != nil {
				m.cfg.OnLinksCreated([]store.Link{l})
			}
			return toJS(l)
		})),
		"update": moejs.NativeFunc(guard(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			if err := need(2, "genkan.links.update"); err != nil {
				return moejs.Undefined(), err
			}
			id := moejs.Arg(args, 0)
			if !id.IsString() {
				return moejs.Undefined(), errors.New("link id must be a string")
			}
			var p store.LinkPatch
			if err := objArg(args, 1, &p); err != nil {
				return moejs.Undefined(), err
			}
			l, err := st.UpdateLink(id.String(), p)
			if err != nil {
				return moejs.Undefined(), err
			}
			return toJS(l)
		})),
	}
	workspaces := map[string]any{"list": moejs.NativeFunc(guard(func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error) {
		if err := need(1, "genkan.workspaces.list"); err != nil {
			return moejs.Undefined(), err
		}
		return toJS(st.Snapshot().Workspaces)
	}))}
	categories := map[string]any{"list": moejs.NativeFunc(guard(func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error) {
		if err := need(1, "genkan.categories.list"); err != nil {
			return moejs.Undefined(), err
		}
		return toJS(st.Snapshot().Categories)
	}))}

	// widgets.list: the plugin's OWN widget instances only, with their secrets
	// (the plugin owns them; never other plugins' instances). No permission needed.
	widgetsAPI := map[string]any{"list": moejs.NativeFunc(guard(func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error) {
		return toJS(m.ownWidgets(e))
	}))}

	if err := rt.SetGlobal("genkan", map[string]any{
		"log":        moejs.NativeFunc(logf("info")),
		"storage":    storage,
		"links":      links,
		"workspaces": workspaces,
		"categories": categories,
		"html":       htmlAPI(rt),
		"widgets":    widgetsAPI,
		"version":    m.cfg.Version,
	}); err != nil {
		return err
	}
	// t(key): manifest-time marker for a `messages` entry (expanded by inspect).
	// It is not language aware; inside hooks it fails and points to ctx.t.
	if err := rt.SetGlobal("t", moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if !in.loading.Load() {
			return moejs.Undefined(), errors.New("t() only works while the module is evaluated (in the manifest); inside hooks use ctx.t(key, params)")
		}
		k := moejs.Arg(args, 0)
		if !k.IsString() {
			return moejs.Undefined(), errors.New("t(key): key must be a string")
		}
		return rt.FromGo(map[string]any{markerKey: k.String()})
	})); err != nil {
		return err
	}
	return rt.SetGlobal("console", map[string]any{
		"log":   moejs.NativeFunc(logf("info")),
		"info":  moejs.NativeFunc(logf("info")),
		"debug": moejs.NativeFunc(logf("debug")),
		"warn":  moejs.NativeFunc(logf("warn")),
		"error": moejs.NativeFunc(logf("error")),
	})
}

// logText renders one log argument: strings as is, objects as JSON, errors by their message.
func (in *instance) logText(v moejs.Value) string {
	if v.IsString() {
		return v.String()
	}
	if v.IsObject() {
		if b, err := in.rt.AppendJSON(nil, v); err == nil && string(b) != "{}" {
			return string(b)
		}
		if s, err := in.rt.Realm().ToString(v); err == nil {
			return s.GoString()
		}
		return "[object]"
	}
	return v.String()
}

// translateFunc is ctx.t(key, params?) for one hook call. It never throws: a
// missing key gives the key, unusable params are ignored.
func (in *instance) translateFunc(ms *Messages, lang string) moejs.NativeFunc {
	return func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		kv := moejs.Arg(args, 0)
		key := ""
		switch {
		case kv.IsString():
			key = kv.String()
		case kv.IsUndefined() || kv.IsNull():
		default:
			key = in.logText(kv)
		}
		var params map[string]any
		if pv := moejs.Arg(args, 1); pv.IsObject() {
			if g, err := in.rt.ToGoWith(pv, moejs.DecodeOptions{MaxBytes: maxTParams}); err == nil {
				params, _ = g.(map[string]any)
			}
		}
		return in.rt.FromGo(ms.T(lang, key, params))
	}
}

// kvScope is one storage namespace: the plugin level or one widget instance.
type kvScope struct {
	get  func(key string) (json.RawMessage, bool)
	set  func(key string, val json.RawMessage) error
	del  func(key string)
	keys func() []string
}

// storageAPI builds the get/set/delete/keys object of a namespace
// (genkan.storage and ctx.instance.storage). guard may be nil.
func (in *instance) storageAPI(guard func(moejs.NativeFunc) moejs.NativeFunc, sc kvScope) map[string]any {
	rt := in.rt
	if guard == nil {
		guard = func(fn moejs.NativeFunc) moejs.NativeFunc { return fn }
	}
	keyArg := func(args []moejs.Value) (string, error) {
		v := moejs.Arg(args, 0)
		if !v.IsString() {
			return "", errors.New("storage key must be a string")
		}
		return v.String(), nil
	}
	return map[string]any{
		"get": moejs.NativeFunc(guard(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			key, err := keyArg(args)
			if err != nil {
				return moejs.Undefined(), err
			}
			raw, ok := sc.get(key)
			if !ok {
				return moejs.Undefined(), nil
			}
			return rt.ParseJSON(raw)
		})),
		"set": moejs.NativeFunc(guard(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			key, err := keyArg(args)
			if err != nil {
				return moejs.Undefined(), err
			}
			raw, err := rt.AppendJSONWith(nil, moejs.Arg(args, 1), moejs.JSONOptions{Strict: true})
			if err != nil {
				return moejs.Undefined(), fmt.Errorf("storage value must be JSON-serializable: %v", err)
			}
			return moejs.Undefined(), sc.set(key, raw)
		})),
		"delete": moejs.NativeFunc(guard(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			key, err := keyArg(args)
			if err != nil {
				return moejs.Undefined(), err
			}
			sc.del(key)
			return moejs.Undefined(), nil
		})),
		"keys": moejs.NativeFunc(guard(func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error) {
			return rt.FromGo(sc.keys())
		})),
	}
}

// instanceStorage is ctx.instance.storage of one hook call.
func (in *instance) instanceStorage(kv *kvStore, id string) map[string]any {
	return in.storageAPI(nil, kvScope{
		get:  func(k string) (json.RawMessage, bool) { return kv.iget(id, k) },
		set:  func(k string, v json.RawMessage) error { return kv.iset(id, k, v) },
		del:  func(k string) { kv.idelete(id, k) },
		keys: func() []string { return kv.ikeys(id) },
	})
}

// ownWidgetOut is one entry of genkan.widgets.list().
type ownWidgetOut struct {
	ID          string         `json:"id"`
	Widget      string         `json:"widget"`
	WorkspaceID string         `json:"workspace_id"`
	Config      map[string]any `json:"config"`
}

// ownWidgets lists the widget instances of one plugin; config has the manifest
// defaults applied and secrets in clear text.
func (m *Manager) ownWidgets(e *entry) []ownWidgetOut {
	out := []ownWidgetOut{}
	for _, w := range m.st.Widgets() {
		if w.Plugin != e.id {
			continue
		}
		var schema []ConfigField
		if e.man != nil {
			if d := e.man.widget(w.Widget); d != nil {
				schema = d.Config
			}
		}
		out = append(out, ownWidgetOut{ID: w.ID, Widget: w.Widget, WorkspaceID: w.WorkspaceID, Config: withDefaults(schema, w.Config)})
	}
	return out
}

// widgetSecrets lists the secret config values of all widget instances of a plugin.
func (m *Manager) widgetSecrets(e *entry) []string {
	var out []string
	if e.man == nil {
		return nil
	}
	for _, w := range m.st.Widgets() {
		if w.Plugin != e.id {
			continue
		}
		if d := e.man.widget(w.Widget); d != nil {
			out = append(out, secretValues(d.Config, w.Config)...)
		}
	}
	return out
}
