package plugin

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jae-jae/genkan/internal/store"
)

const (
	errorCacheTTL = 15 * time.Second // failed widget/wallpaper calls are not retried sooner
	maxActionJSON = 8 << 10
	searchBudget  = 2 * time.Second
)

// hookReq describes one hook call.
type hookReq struct {
	hook    string
	lang    string
	timeout time.Duration
	fields  map[string]any // source, widget, query, action, params, schedule
	// widget instance, when the call belongs to one
	instID     string
	instSchema []ConfigField
	instCfg    map[string]any // stored instance config (secrets in clear text)
}

// call builds ctx for a hook and runs it.
func (m *Manager) call(parent context.Context, e *entry, rec store.Plugin, r hookReq) (any, error) {
	if e.err != nil {
		return nil, hookErr("plugin is broken: %v", e.err)
	}
	cfg := withDefaults(e.man.ConfigSchema, rec.Config)
	jsctx := map[string]any{
		"plugin": map[string]any{"id": rec.ID, "version": e.man.Version},
		"config": cfg,
		"lang":   r.lang,
		"now":    float64(nowMs()),
	}
	for k, v := range r.fields {
		jsctx[k] = v
	}
	o := callOpts{hook: r.hook, timeout: r.timeout, ctx: jsctx}
	o.pluginHost = fetchHostsFromConfig(e.man.ConfigSchema, cfg)
	o.secrets = secretValues(e.man.ConfigSchema, rec.Config)
	if r.instSchema != nil || r.instID != "" {
		icfg := withDefaults(r.instSchema, r.instCfg)
		jsctx["instance"] = map[string]any{"id": r.instID, "config": icfg}
		o.extraHosts = fetchHostsFromConfig(r.instSchema, icfg)
		o.secrets = append(o.secrets, secretValues(r.instSchema, r.instCfg)...)
	}
	return e.invoke(parent, o)
}

// ready returns the entry of an enabled plugin.
func (m *Manager) ready(id string) (*entry, store.Plugin, error) {
	e, rec, err := m.entryFor(id)
	if err != nil {
		return nil, rec, err
	}
	if !rec.Enabled {
		return nil, rec, conflictf("plugin %s is disabled", id)
	}
	if e.err != nil {
		return nil, rec, &HookError{Msg: "plugin is broken: " + e.err.Error()}
	}
	return e, rec, nil
}

// Widgets ---------------------------------------------------------------------------------------

// WidgetOut is WidgetInstance as sent to browsers.
type WidgetOut struct {
	ID          string         `json:"id"`
	Plugin      string         `json:"plugin"`
	Widget      string         `json:"widget"`
	WorkspaceID string         `json:"workspace_id"`
	Config      map[string]any `json:"config"`
	Sort        int            `json:"sort"`
	SecretsSet  []string       `json:"secrets_set,omitempty"`
}

// Present converts widget instances for a browser: secret values are blanked
// for the admin (with secrets_set) and removed for guests.
func (m *Manager) Present(ws []store.WidgetInstance, admin bool) []WidgetOut {
	out := make([]WidgetOut, 0, len(ws))
	schemas := map[string]*entry{}
	for _, w := range ws {
		e, ok := schemas[w.Plugin]
		if !ok {
			if rec, found := m.st.GetPlugin(w.Plugin); found {
				e = m.entryRec(rec)
			}
			schemas[w.Plugin] = e
		}
		o := WidgetOut{ID: w.ID, Plugin: w.Plugin, Widget: w.Widget, WorkspaceID: w.WorkspaceID, Sort: w.Sort, Config: map[string]any{}}
		if e != nil && e.man != nil {
			if d := e.man.widget(w.Widget); d != nil {
				var set []string
				o.Config, set = maskConfig(d.Config, w.Config, admin)
				if admin {
					o.SecretsSet = set
				}
			}
		}
		out = append(out, o)
	}
	return out
}

// WidgetAdd is the body of POST /api/widgets.
type WidgetAdd struct {
	Plugin      string         `json:"plugin"`
	Widget      string         `json:"widget"`
	WorkspaceID string         `json:"workspace_id"`
	Config      map[string]any `json:"config"`
}

func (m *Manager) widgetDecl(plugin, widget string) (*entry, *WidgetDecl, error) {
	e, _, err := m.entryFor(plugin)
	if err != nil {
		return nil, nil, err
	}
	if e.err != nil {
		return nil, nil, conflictf("plugin %s is broken: %v", plugin, e.err)
	}
	d := e.man.widget(widget)
	if d == nil {
		return nil, nil, notFoundf("plugin %s has no widget %q", plugin, widget)
	}
	return e, d, nil
}

// AddWidget places a new widget instance.
func (m *Manager) AddWidget(in WidgetAdd) (WidgetOut, error) {
	_, d, err := m.widgetDecl(in.Plugin, in.Widget)
	if err != nil {
		return WidgetOut{}, err
	}
	cfg, err := cleanConfig(d.Config, in.Config, nil, true)
	if err != nil {
		return WidgetOut{}, invalidf("%v", err)
	}
	w, err := m.st.CreateWidget(store.WidgetInput{Plugin: in.Plugin, Widget: in.Widget, WorkspaceID: in.WorkspaceID, Config: cfg})
	if err != nil {
		return WidgetOut{}, err
	}
	return m.Present([]store.WidgetInstance{w}, true)[0], nil
}

// WidgetPatch is the body of PATCH /api/widgets/{id}. Merge makes Config a partial update.
type WidgetPatch struct {
	WorkspaceID *string        `json:"workspace_id"`
	Config      map[string]any `json:"config"`
	Sort        *int           `json:"sort"`
	Merge       bool           `json:"-"`
}

func (m *Manager) UpdateWidget(id string, p WidgetPatch) (WidgetOut, error) {
	w, ok := m.st.GetWidget(id)
	if !ok {
		return WidgetOut{}, notFoundf("widget not found: %s", id)
	}
	sp := store.WidgetPatch{WorkspaceID: p.WorkspaceID, Sort: p.Sort}
	if p.Config != nil {
		_, d, err := m.widgetDecl(w.Plugin, w.Widget)
		if err != nil {
			return WidgetOut{}, err
		}
		in := p.Config
		if p.Merge {
			in = map[string]any{}
			for k, v := range w.Config {
				in[k] = v
			}
			for k, v := range p.Config {
				if v == nil && !isSecretKey(d.Config, k) {
					delete(in, k)
				} else {
					in[k] = v
				}
			}
		}
		clean, err := cleanConfig(d.Config, in, w.Config, true)
		if err != nil {
			return WidgetOut{}, invalidf("%v", err)
		}
		sp.Config = &clean
	}
	nw, err := m.st.UpdateWidget(id, sp)
	if err != nil {
		return WidgetOut{}, err
	}
	m.forgetWidget(id)
	return m.Present([]store.WidgetInstance{nw}, true)[0], nil
}

func (m *Manager) DeleteWidget(id string) error {
	if err := m.st.DeleteWidget(id); err != nil {
		return err
	}
	m.forgetWidget(id)
	return nil
}

func (m *Manager) ReorderWidgets(ids []string) error { return m.st.ReorderWidgets(ids) }

// WidgetData is the response of GET /api/widgets/{id}/data. Times are Unix milliseconds.
type WidgetData struct {
	View      WidgetView `json:"view"`
	UpdatedAt int64      `json:"updated_at"`
	NextAt    int64      `json:"next_at"`
}

// Data returns the (cached) view of a widget instance. force bypasses the cache.
func (m *Manager) Data(ctx context.Context, id, lang string, force bool) (WidgetData, error) {
	w, ok := m.st.GetWidget(id)
	if !ok {
		return WidgetData{}, notFoundf("widget not found: %s", id)
	}
	e, rec, err := m.ready(w.Plugin)
	if err != nil {
		return WidgetData{}, err
	}
	d := e.man.widget(w.Widget)
	if d == nil {
		return WidgetData{}, &HookError{Msg: "plugin has no widget " + w.Widget}
	}
	lang = NormLang(lang)
	key := "w|" + id + "|" + lang
	stamp := hashJSON(rec.Rev, rec.Config, w.Config)
	if !force {
		if it := m.cached(key, stamp); it != nil {
			if it.err != nil {
				return WidgetData{}, it.err
			}
			return it.data.(WidgetData), nil
		}
	}
	v, err, _ := m.fl.do(key+"|"+stamp, func() (any, error) {
		raw, err := m.call(m.ctx, e, rec, hookReq{
			hook: hookWidget, lang: lang, timeout: timeoutDefault,
			fields: map[string]any{"widget": w.Widget},
			instID: w.ID, instSchema: nonNil(d.Config), instCfg: w.Config,
		})
		if err != nil {
			m.putCache(key, &cacheItem{stamp: stamp, err: err, errTill: time.Now().Add(errorCacheTTL)})
			return nil, err
		}
		view := SanitizeView(raw)
		now := nowMs()
		out := WidgetData{View: view, UpdatedAt: now, NextAt: now + int64(d.Refresh)*1000}
		m.putCache(key, &cacheItem{stamp: stamp, data: out, updated: now, next: out.NextAt})
		return out, nil
	})
	if err != nil {
		return WidgetData{}, waitErr(ctx, err)
	}
	return v.(WidgetData), nil
}

func nonNil(f []ConfigField) []ConfigField {
	if f == nil {
		return []ConfigField{}
	}
	return f
}

func waitErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (m *Manager) cached(key, stamp string) *cacheItem {
	m.cmu.Lock()
	defer m.cmu.Unlock()
	it := m.cache[key]
	if it == nil || it.stamp != stamp {
		return nil
	}
	now := time.Now()
	if it.err != nil {
		if now.After(it.errTill) {
			return nil
		}
		return it
	}
	if nowMs() >= it.next {
		return nil
	}
	return it
}

func (m *Manager) putCache(key string, it *cacheItem) {
	m.cmu.Lock()
	m.cache[key] = it
	m.cmu.Unlock()
}

// ActionRequest is the body of POST /api/widgets/{id}/action.
type ActionRequest struct {
	Action string `json:"action"`
	Params any    `json:"params"`
}

// Action runs the plugin's action hook for a widget instance. The cached view is dropped.
func (m *Manager) Action(ctx context.Context, id string, req ActionRequest, lang string) (ActionResult, error) {
	w, ok := m.st.GetWidget(id)
	if !ok {
		return ActionResult{}, notFoundf("widget not found: %s", id)
	}
	if !actionRe.MatchString(req.Action) {
		return ActionResult{}, invalidf("invalid action name")
	}
	if jsonSize(req.Params) > maxActionJSON {
		return ActionResult{}, invalidf("params are too large")
	}
	e, rec, err := m.ready(w.Plugin)
	if err != nil {
		return ActionResult{}, err
	}
	d := e.man.widget(w.Widget)
	if d == nil {
		return ActionResult{}, notFoundf("plugin has no widget %q", w.Widget)
	}
	if _, has := e.hook[hookAction]; !has {
		return ActionResult{}, invalidf("plugin %s does not export action()", w.Plugin)
	}
	raw, err := m.call(ctx, e, rec, hookReq{
		hook: hookAction, lang: NormLang(lang), timeout: timeoutDefault,
		fields: map[string]any{"widget": w.Widget, "action": req.Action, "params": req.Params},
		instID: w.ID, instSchema: nonNil(d.Config), instCfg: w.Config,
	})
	m.forgetWidget(id)
	if err != nil {
		return ActionResult{}, err
	}
	return SanitizeAction(raw), nil
}

// Search ---------------------------------------------------------------------------------------

// SearchGroup is one plugin's results.
type SearchGroup struct {
	Plugin  string         `json:"plugin"`
	Name    string         `json:"name"`
	Icon    string         `json:"icon"`
	Results []SearchResult `json:"results"`
}

// HasSearch reports whether any enabled plugin provides search.
func (m *Manager) HasSearch() bool {
	for _, r := range m.st.Plugins() {
		if r.Enabled && m.entryRec(r).man != nil && m.entryRec(r).man.Search != nil {
			return true
		}
	}
	return false
}

// Search queries every enabled search plugin concurrently (2 s in total).
// Plugins without a prefix only see queries of at least 2 characters.
func (m *Manager) Search(ctx context.Context, q, lang string) []SearchGroup {
	q = strings.TrimSpace(q)
	groups := []SearchGroup{}
	if q == "" {
		return groups
	}
	lang = NormLang(lang)
	type job struct {
		e     *entry
		rec   store.Plugin
		query string
	}
	var jobs []job
	for _, rec := range m.st.Plugins() {
		if !rec.Enabled {
			continue
		}
		e := m.entryRec(rec)
		if e.man == nil || e.man.Search == nil {
			continue
		}
		query := q
		if pre := e.man.Search.Prefix; pre != "" {
			low := strings.ToLower(q)
			if !strings.HasPrefix(low, pre+" ") {
				continue
			}
			query = strings.TrimSpace(q[len(pre)+1:])
			if query == "" {
				continue
			}
		} else if len([]rune(q)) < 2 {
			continue
		}
		jobs = append(jobs, job{e, rec, query})
	}
	if len(jobs) == 0 {
		return groups
	}
	cctx, cancel := context.WithTimeout(ctx, searchBudget)
	defer cancel()
	res := make([]*SearchGroup, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := "s|" + j.rec.ID + "|" + lang + "|" + j.query
			v, err, _ := m.fl.do(key, func() (any, error) {
				raw, err := m.call(m.ctx, j.e, j.rec, hookReq{
					hook: hookSearch, lang: lang, timeout: timeoutSearch,
					fields: map[string]any{"query": j.query},
				})
				if err != nil {
					return nil, err
				}
				return SanitizeResults(raw), nil
			})
			if err != nil || cctx.Err() != nil {
				return
			}
			if rs := v.([]SearchResult); len(rs) > 0 {
				res[i] = &SearchGroup{Plugin: j.rec.ID, Name: j.e.man.Search.Name, Icon: j.e.man.Icon, Results: rs}
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-cctx.Done():
	}
	for _, g := range res { // slow plugins that are still running are ignored
		if g != nil {
			groups = append(groups, *g)
		}
	}
	return groups
}

// Test ------------------------------------------------------------------------------------------

// TestRequest is the body of POST /api/plugins/{id}/test.
type TestRequest struct {
	Hook     string         `json:"hook"`
	Target   string         `json:"target"`
	Query    string         `json:"query"`
	Config   map[string]any `json:"config"`
	Action   string         `json:"action"`
	Params   any            `json:"params"`
	Instance string         `json:"instance"` // optional: saved widget instance whose config is the base
	Lang     string         `json:"lang"`
}

// TestResult is the response of the test endpoint.
type TestResult struct {
	OK     bool       `json:"ok"`
	Result any        `json:"result"`
	Error  string     `json:"error,omitempty"`
	Logs   []LogEntry `json:"logs"`
	MS     int64      `json:"ms"`
}

// Test runs a hook once for debugging. Nothing is cached or downloaded; the
// result is the sanitized value the real endpoint would use.
func (m *Manager) Test(ctx context.Context, id string, req TestRequest) (TestResult, error) {
	e, rec, err := m.entryFor(id)
	if err != nil {
		return TestResult{}, err
	}
	if e.err != nil {
		return TestResult{Logs: e.logs.all(), Error: "plugin is broken: " + e.err.Error()}, nil
	}
	hr := hookReq{hook: req.Hook, lang: NormLang(req.Lang), timeout: timeoutDefault, fields: map[string]any{}}
	if req.Lang == "" {
		hr.lang = "en"
	}
	first := func(ids ...string) string {
		if req.Target != "" {
			return req.Target
		}
		if len(ids) > 0 {
			return ids[0]
		}
		return ""
	}
	switch req.Hook {
	case hookWallpaper:
		if len(e.man.Wallpapers) == 0 {
			return TestResult{}, invalidf("plugin has no wallpaper sources")
		}
		src := first(e.man.Wallpapers[0].ID)
		if e.man.wallpaper(src) == nil {
			return TestResult{}, notFoundf("no wallpaper source %q", src)
		}
		hr.fields["source"] = src
	case hookWidget, hookAction:
		if len(e.man.Widgets) == 0 {
			return TestResult{}, invalidf("plugin has no widgets")
		}
		wid := first(e.man.Widgets[0].ID)
		d := e.man.widget(wid)
		if d == nil {
			return TestResult{}, notFoundf("no widget %q", wid)
		}
		var base map[string]any
		instID := "test"
		if req.Instance != "" {
			w, ok := m.st.GetWidget(req.Instance)
			if !ok || w.Plugin != id {
				return TestResult{}, notFoundf("widget instance not found: %s", req.Instance)
			}
			base, instID = w.Config, w.ID
		}
		cfg, err := cleanConfig(d.Config, req.Config, base, false)
		if err != nil {
			return TestResult{}, invalidf("%v", err)
		}
		if req.Config == nil {
			cfg = base
		}
		hr.instID, hr.instSchema, hr.instCfg = instID, nonNil(d.Config), cfg
		hr.fields["widget"] = wid
		if req.Hook == hookAction {
			if _, has := e.hook[hookAction]; !has {
				return TestResult{}, invalidf("plugin does not export action()")
			}
			if !actionRe.MatchString(req.Action) {
				return TestResult{}, invalidf("action is required")
			}
			hr.fields["action"], hr.fields["params"] = req.Action, req.Params
		}
	case hookSearch:
		if e.man.Search == nil {
			return TestResult{}, invalidf("plugin has no search provider")
		}
		hr.timeout = timeoutSearch
		hr.fields["query"] = req.Query
	case hookSchedule:
		if len(e.man.Schedules) == 0 {
			return TestResult{}, invalidf("plugin has no schedules")
		}
		sid := first(e.man.Schedules[0].ID)
		if e.man.schedule(sid) == nil {
			return TestResult{}, notFoundf("no schedule %q", sid)
		}
		hr.timeout = timeoutSched
		hr.lang = "en"
		hr.fields["schedule"] = sid
	default:
		return TestResult{}, invalidf(`hook must be one of "wallpaper", "widget", "search", "schedule", "action"`)
	}
	if _, has := e.hook[hr.hook]; !has {
		return TestResult{}, invalidf("plugin does not export %s()", hr.hook)
	}

	mark := e.logs.mark()
	start := time.Now()
	raw, err := m.call(ctx, e, rec, hr)
	res := TestResult{MS: time.Since(start).Milliseconds()}
	secrets := secretValues(e.man.ConfigSchema, rec.Config)
	if err != nil {
		res.Error = redactSecrets(err.Error(), secrets)
	} else {
		res.OK = true
		switch req.Hook {
		case hookWidget:
			res.Result = SanitizeView(raw)
		case hookSearch:
			res.Result = SanitizeResults(raw)
		case hookAction:
			res.Result = SanitizeAction(raw)
		case hookWallpaper:
			wr, ok := SanitizeWallpaper(raw, rand.IntN)
			if !ok {
				res.OK, res.Error = false, "wallpaper() must return { url } (http(s) or data:image) or an array of those"
			} else {
				res.Result = wr
			}
		default:
			res.Result = raw
		}
		if len(secrets) > 0 { // never echo a secret, even if the plugin returned one
			if b, merr := json.Marshal(res.Result); merr == nil {
				if red := redactSecrets(string(b), secrets); red != string(b) {
					var back any
					if json.Unmarshal([]byte(red), &back) == nil {
						res.Result = back
					}
				}
			}
		}
	}
	res.Logs = e.logs.since(mark)
	sort.SliceStable(res.Logs, func(i, j int) bool { return res.Logs[i].Ts < res.Logs[j].Ts })
	return res, nil
}
