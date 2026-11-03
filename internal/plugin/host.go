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
	keyArg := func(args []moejs.Value) (string, error) {
		v := moejs.Arg(args, 0)
		if !v.IsString() {
			return "", errors.New("storage key must be a string")
		}
		return v.String(), nil
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

	storage := map[string]any{
		"get": moejs.NativeFunc(guard(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			key, err := keyArg(args)
			if err != nil {
				return moejs.Undefined(), err
			}
			raw, ok := e.kv.get(key)
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
			return moejs.Undefined(), e.kv.set(key, raw)
		})),
		"delete": moejs.NativeFunc(guard(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			key, err := keyArg(args)
			if err != nil {
				return moejs.Undefined(), err
			}
			e.kv.delete(key)
			return moejs.Undefined(), nil
		})),
		"keys": moejs.NativeFunc(guard(func(*moejs.Realm, moejs.Value, []moejs.Value) (moejs.Value, error) {
			return rt.FromGo(e.kv.keys())
		})),
	}

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

	if err := rt.SetGlobal("genkan", map[string]any{
		"log":        moejs.NativeFunc(logf("info")),
		"storage":    storage,
		"links":      links,
		"workspaces": workspaces,
		"categories": categories,
		"version":    m.cfg.Version,
	}); err != nil {
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
