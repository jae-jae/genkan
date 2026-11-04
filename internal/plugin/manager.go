// Package plugin runs Genkan's JavaScript plugins: wallpaper sources, widgets,
// search providers, scheduled jobs and widget actions. Plugins are ES modules
// executed by the pure-Go moejs engine on the server; the browser only ever
// receives sanitized declarative JSON. See docs/PLUGINS.md.
//
// Nothing is created until a plugin hook actually runs: no runtime, no
// goroutine (the scheduler goroutine only exists while an enabled plugin
// declares schedules), no timers.
package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moejs"

	"github.com/jae-jae/genkan/internal/store"
)

// Config holds the manager's dependencies.
type Config struct {
	Store   *store.Store
	DataDir string
	Version string
	// Catalog holds the built-in example plugins as flat *.js files (may be nil).
	Catalog fs.FS
	// OnLinksCreated is called with links a plugin created (e.g. to fetch icons).
	OnLinksCreated func([]store.Link)
	// HTTPClient downloads plugin sources for install-from-URL (default: 15 s timeout).
	HTTPClient *http.Client
	// GitHubToken (GENKAN_GITHUB_TOKEN) raises the GitHub API rate limit of the
	// third-party plugin market (optional).
	GitHubToken string
	// GitHubAPI / GitHubRaw override https://api.github.com and
	// https://raw.githubusercontent.com (tests); GitHubClient the HTTP client.
	GitHubAPI, GitHubRaw string
	GitHubClient         *http.Client
}

// Manager owns all plugin runtime state.
type Manager struct {
	cfg Config
	st  *store.Store
	ctx context.Context
	end context.CancelFunc
	wg  sync.WaitGroup

	mu      sync.Mutex
	entries map[string]*entry

	fl    flight
	cmu   sync.Mutex
	cache map[string]*cacheItem // widget data and wallpapers

	catOnce sync.Once
	catalog []CatalogItem
	catSrc  map[string]string

	sched *scheduler

	mkMu    sync.Mutex
	mkCache map[string]*marketEntry // third-party market (market.go)
	mkGen   uint64                  // bumped by ClearMarketCache so in-flight fetches do not repopulate it
	ghOnce  sync.Once
	ghc     *http.Client
}

// New creates the manager. It does no work until a plugin is used.
func New(cfg Config) *Manager {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: fetchTimeout}
	}
	m := &Manager{cfg: cfg, st: cfg.Store, entries: map[string]*entry{}, cache: map[string]*cacheItem{}}
	m.ctx, m.end = context.WithCancel(context.Background())
	m.sched = newScheduler(m)
	m.st.SetSecretResolver(m.secretResolver)
	m.st.SetWidgetsGone(m.clearInstances)
	m.sched.kick()
	return m
}

// Close stops the scheduler and running hooks and flushes plugin storage.
func (m *Manager) Close() {
	m.end()
	m.sched.stop()
	m.wg.Wait()
	m.mu.Lock()
	for id, e := range m.entries {
		e.close()
		delete(m.entries, id)
	}
	m.mu.Unlock()
}

// Errors ------------------------------------------------------------------------------

func invalidf(format string, a ...any) error {
	return &store.Error{Kind: store.KindInvalid, Msg: fmt.Sprintf(format, a...)}
}
func notFoundf(format string, a ...any) error {
	return &store.Error{Kind: store.KindNotFound, Msg: fmt.Sprintf(format, a...)}
}
func conflictf(format string, a ...any) error {
	return &store.Error{Kind: store.KindConflict, Msg: fmt.Sprintf(format, a...)}
}

// Entries -------------------------------------------------------------------------------

// inspect compiles source and reads its manifest in a throw-away runtime
// without network, storage or links access.
func (m *Manager) inspect(source, lang string) (*Manifest, *moejs.Module, error) {
	name := "plugin.js"
	compile := moejs.Compile
	if lang == "ts" {
		name, compile = "plugin.ts", moejs.CompileTS
	}
	mod, err := compile(name, source)
	if err != nil {
		return nil, nil, fmt.Errorf("compile error: %v", err)
	}
	if reqs := mod.Requests(); len(reqs) > 0 {
		return nil, nil, fmt.Errorf("plugins must be a single file: import %q is not allowed", reqs[0])
	}
	probe := &entry{m: m, id: "probe", logs: newLogRing()}
	in, err := newInstance(m, probe, name, mod, true)
	if err != nil {
		return nil, nil, fmt.Errorf("load error: %v", err)
	}
	defer in.close()
	v, ok := in.rt.Export("manifest")
	if !ok {
		return nil, nil, fmt.Errorf("the module must `export const manifest = {...}`")
	}
	raw, err := in.rt.ToGoWith(v, moejs.DecodeOptions{MaxBytes: 64 << 10})
	if err != nil {
		return nil, nil, fmt.Errorf("manifest is not usable: %v", err)
	}
	var msgs *Messages
	if mv, ok := in.rt.Export("messages"); ok && !mv.IsUndefined() && !mv.IsNull() {
		mraw, err := in.rt.ToGoWith(mv, moejs.DecodeOptions{MaxBytes: maxMessagesJSON})
		if err != nil {
			return nil, nil, fmt.Errorf("messages is not usable: %v", err)
		}
		if msgs, err = parseMessages(mraw); err != nil {
			return nil, nil, err
		}
	}
	if raw, err = expandMarkers(raw, "manifest", msgs); err != nil {
		return nil, nil, err
	}
	man, err := parseManifest(raw)
	if err != nil {
		return nil, nil, err
	}
	man.messages = msgs
	exports := map[string]bool{}
	for _, n := range mod.Exports() {
		exports[n] = true
	}
	for _, h := range man.requiredExports() {
		if !exports[h] {
			return nil, nil, fmt.Errorf("the manifest declares %s but the module does not export `async function %s(ctx)`", capabilityOf(h), h)
		}
	}
	return man, mod, nil
}

func capabilityOf(hook string) string {
	switch hook {
	case hookWallpaper:
		return "wallpapers"
	case hookWidget:
		return "widgets"
	case hookSearch:
		return "search"
	}
	return "schedules"
}

// build creates the entry for a stored plugin.
func (m *Manager) build(rec store.Plugin) *entry {
	e := &entry{m: m, id: rec.ID, rev: rec.Rev, lang: rec.Lang, logs: newLogRing(), hook: map[string]moejs.Hook{}}
	e.kv = newKV(m.cfg.DataDir, rec.ID, func() bool { _, ok := m.st.GetPlugin(rec.ID); return ok })
	e.pool = newPool(e)
	src, lang, err := m.st.PluginSource(rec.ID)
	if err == nil {
		e.lang = lang
		e.man, e.mod, err = m.inspect(src, lang)
	}
	if err == nil && e.man.ID != rec.ID {
		err = fmt.Errorf("manifest.id %q does not match the installed plugin id %q", e.man.ID, rec.ID)
	}
	if err == nil {
		for _, h := range []string{hookWallpaper, hookWidget, hookSearch, hookSchedule, hookAction} {
			if hk, herr := e.mod.Hook(h); herr == nil {
				e.hook[h] = hk
			}
		}
	}
	if err != nil {
		e.err = err
		e.man = nil
		log.Printf("plugin %s: %v", rec.ID, err)
		e.logs.add("error", err.Error(), nil)
	}
	return e
}

// entryFor returns the compiled entry for a stored plugin, building it on first use.
func (m *Manager) entryFor(id string) (*entry, store.Plugin, error) {
	rec, ok := m.st.GetPlugin(id)
	if !ok {
		m.drop(id)
		return nil, rec, notFoundf("plugin not found: %s", id)
	}
	return m.entryRec(rec), rec, nil
}

func (m *Manager) entryRec(rec store.Plugin) *entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[rec.ID]
	if e == nil || e.rev != rec.Rev {
		if e != nil {
			e.close()
		}
		e = m.build(rec)
		m.entries[rec.ID] = e
		m.forgetCache(rec.ID)
	}
	return e
}

func (m *Manager) drop(id string) {
	m.mu.Lock()
	e := m.entries[id]
	delete(m.entries, id)
	m.mu.Unlock()
	if e != nil {
		e.close()
	}
	m.forgetCache(id)
}

// prime stores an entry built from an already inspected source (right after install).
func (m *Manager) prime(rec store.Plugin, man *Manifest, mod *moejs.Module) {
	e := &entry{m: m, id: rec.ID, rev: rec.Rev, lang: rec.Lang, logs: newLogRing(), hook: map[string]moejs.Hook{}, man: man, mod: mod}
	e.kv = newKV(m.cfg.DataDir, rec.ID, func() bool { _, ok := m.st.GetPlugin(rec.ID); return ok })
	e.pool = newPool(e)
	for _, h := range []string{hookWallpaper, hookWidget, hookSearch, hookSchedule, hookAction} {
		if hk, err := mod.Hook(h); err == nil {
			e.hook[h] = hk
		}
	}
	m.mu.Lock()
	old := m.entries[rec.ID]
	if old != nil {
		e.logs, e.kv = old.logs, old.kv // keep the log and pending storage across updates
		old.kv = nil
		old.close()
		e.kv.exists = func() bool { _, ok := m.st.GetPlugin(rec.ID); return ok }
	}
	m.entries[rec.ID] = e
	m.mu.Unlock()
	m.forgetCache(rec.ID)
}

// secretResolver feeds store exports: which config keys are secret.
func (m *Manager) secretResolver(id string) ([]string, map[string][]string, bool) {
	e, _, err := m.entryFor(id)
	if err != nil || e.err != nil {
		return nil, nil, false
	}
	wk := map[string][]string{}
	for _, w := range e.man.Widgets {
		wk[w.ID] = secretKeys(w.Config)
	}
	return secretKeys(e.man.ConfigSchema), wk, true
}

// Info -------------------------------------------------------------------------------------

// Admin holds the fields of PluginInfo that only the admin sees.
type Admin struct {
	Config      map[string]any `json:"config"`
	SecretsSet  []string       `json:"secrets_set"`
	SourceURL   string         `json:"source_url"`
	LastError   string         `json:"last_error"`
	InstalledAt string         `json:"installed_at"`
	UpdatedAt   string         `json:"updated_at"`
}

// Info is PluginInfo (docs/PLUGINS.md §5).
type Info struct {
	Manifest
	Enabled bool `json:"enabled"`
	*Admin
}

func (m *Manager) info(rec store.Plugin, admin bool) Info {
	e := m.entryRec(rec)
	var inf Info
	if e.man != nil {
		inf.Manifest = *e.man
	} else {
		inf.Manifest = Manifest{ID: rec.ID, Name: Plain(rec.ID), Permissions: Permissions{Fetch: []string{}}, ConfigSchema: []ConfigField{}, Wallpapers: []WallpaperDecl{}, Widgets: []WidgetDecl{}, Schedules: []ScheduleDecl{}}
	}
	inf.Enabled = rec.Enabled
	if admin {
		a := &Admin{Config: map[string]any{}, SecretsSet: []string{}, SourceURL: rec.SourceURL, LastError: e.lastError(), InstalledAt: rec.InstalledAt, UpdatedAt: rec.UpdatedAt}
		if e.man != nil {
			a.Config, a.SecretsSet = maskConfig(e.man.ConfigSchema, rec.Config, true)
		}
		inf.Admin = a
	}
	return inf
}

// Infos lists all installed plugins (admin adds config, errors and timestamps).
func (m *Manager) Infos(admin bool) []Info {
	recs := m.st.Plugins()
	out := make([]Info, 0, len(recs))
	live := map[string]bool{}
	for _, r := range recs {
		live[r.ID] = true
		out = append(out, m.info(r, admin))
	}
	m.mu.Lock()
	var stale []string
	for id := range m.entries {
		if !live[id] {
			stale = append(stale, id)
		}
	}
	m.mu.Unlock()
	for _, id := range stale {
		m.drop(id)
	}
	return out
}

// Info returns one plugin.
func (m *Manager) Info(id string, admin bool) (Info, error) {
	rec, ok := m.st.GetPlugin(id)
	if !ok {
		return Info{}, notFoundf("plugin not found: %s", id)
	}
	return m.info(rec, admin), nil
}

// Source returns the plugin's source and language.
func (m *Manager) Source(id string) (string, string, error) { return m.st.PluginSource(id) }

// Logs returns the in-memory log of a plugin (oldest first).
func (m *Manager) Logs(id string) ([]LogEntry, error) {
	e, _, err := m.entryFor(id)
	if err != nil {
		return nil, err
	}
	return e.logs.all(), nil
}

// Install ------------------------------------------------------------------------------------

// InstallRequest installs from exactly one of Source, URL or Catalog.
type InstallRequest struct {
	Source  string `json:"source"`
	Lang    string `json:"lang"`
	URL     string `json:"url"`
	Catalog string `json:"catalog"`
	Enable  *bool  `json:"enable"`
}

const maxDownload = store.MaxPluginSource

// Install installs or updates a plugin. An existing plugin (same manifest id)
// keeps its configuration, widget instances and enabled state.
func (m *Manager) Install(ctx context.Context, req InstallRequest) (Info, error) {
	source, lang, srcURL, err := m.resolve(ctx, req)
	if err != nil {
		return Info{}, err
	}
	man, mod, err := m.inspect(source, lang)
	if err != nil {
		return Info{}, invalidf("invalid plugin: %v", err)
	}
	rec, _, err := m.st.SavePlugin(store.PluginSave{ID: man.ID, Source: source, Lang: lang, SourceURL: srcURL, Enabled: req.Enable})
	if err != nil {
		return Info{}, err
	}
	m.prime(rec, man, mod)
	m.sched.kick()
	return m.info(rec, true), nil
}

// resolve turns an install/preview request into source text, its language
// and the URL it came from (for "check for updates"). Exactly one of source,
// url or catalog must be given; size and language are checked.
func (m *Manager) resolve(ctx context.Context, req InstallRequest) (source, lang, srcURL string, err error) {
	n := 0
	for _, s := range []string{req.Source, req.URL, req.Catalog} {
		if s != "" {
			n++
		}
	}
	if n != 1 {
		return "", "", "", invalidf("provide exactly one of source, url or catalog")
	}
	source, lang = req.Source, req.Lang
	switch {
	case req.URL != "":
		if source, lang, err = m.download(ctx, req.URL, lang); err != nil {
			return "", "", "", err
		}
		srcURL = req.URL
	case req.Catalog != "":
		src, ok := m.catalogSource(req.Catalog)
		if !ok {
			return "", "", "", notFoundf("no such catalog plugin: %s", req.Catalog)
		}
		source, lang = src, "js"
	}
	if lang == "" {
		lang = "js"
	}
	if lang != "js" && lang != "ts" {
		return "", "", "", invalidf(`lang must be "js" or "ts"`)
	}
	if len(source) > store.MaxPluginSource {
		return "", "", "", invalidf("plugin source is too large (max %d KiB)", store.MaxPluginSource>>10)
	}
	return source, lang, srcURL, nil
}

// Validate checks that source is a valid plugin and returns its manifest id
// (used to vet imported plugins).
func (m *Manager) Validate(source, lang string) (string, error) {
	if lang == "" {
		lang = "js"
	}
	man, _, err := m.inspect(source, lang)
	if err != nil {
		return "", err
	}
	return man.ID, nil
}

func (m *Manager) download(ctx context.Context, rawURL, lang string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", "", invalidf("url must be an http(s) URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", "", invalidf("invalid url")
	}
	req.Header.Set("Accept", "text/javascript, application/javascript, text/plain, */*")
	resp, err := m.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", "", &HookError{Msg: "could not download the plugin: " + trimErr(err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", "", &HookError{Msg: fmt.Sprintf("could not download the plugin: HTTP %d", resp.StatusCode)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return "", "", &HookError{Msg: "could not download the plugin: " + trimErr(err)}
	}
	if len(b) > maxDownload {
		return "", "", invalidf("plugin source is too large (max %d KiB)", maxDownload>>10)
	}
	if lang == "" && strings.HasSuffix(strings.ToLower(u.Path), ".ts") {
		lang = "ts"
	}
	return string(b), lang, nil
}

func trimErr(err error) string {
	s := err.Error()
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// Catalog ----------------------------------------------------------------------------------

// CatalogItem describes a built-in example plugin.
type CatalogItem struct {
	ID          string          `json:"id"`
	Name        LocalizedString `json:"name"`
	Description LocalizedString `json:"description"`
	Icon        string          `json:"icon"`
	Version     string          `json:"version"`
	Installed   bool            `json:"installed"`
}

func (m *Manager) loadCatalog() {
	m.catOnce.Do(func() {
		m.catSrc = map[string]string{}
		if m.cfg.Catalog == nil {
			return
		}
		ents, err := fs.ReadDir(m.cfg.Catalog, ".")
		if err != nil {
			return
		}
		for _, de := range ents {
			if de.IsDir() || path.Ext(de.Name()) != ".js" {
				continue
			}
			b, err := fs.ReadFile(m.cfg.Catalog, de.Name())
			if err != nil {
				continue
			}
			man, _, err := m.inspect(string(b), "js")
			if err != nil {
				log.Printf("plugin catalog: skip %s: %v", de.Name(), err)
				continue
			}
			m.catSrc[man.ID] = string(b)
			m.catalog = append(m.catalog, CatalogItem{ID: man.ID, Name: man.Name, Description: man.Description, Icon: man.Icon, Version: man.Version})
		}
		sort.Slice(m.catalog, func(i, j int) bool { return m.catalog[i].ID < m.catalog[j].ID })
	})
}

// Catalog lists the built-in example plugins.
func (m *Manager) Catalog() []CatalogItem {
	m.loadCatalog()
	installed := map[string]bool{}
	for _, p := range m.st.Plugins() {
		installed[p.ID] = true
	}
	out := make([]CatalogItem, len(m.catalog))
	for i, c := range m.catalog {
		c.Installed = installed[c.ID]
		out[i] = c
	}
	return out
}

func (m *Manager) catalogSource(id string) (string, bool) {
	m.loadCatalog()
	s, ok := m.catSrc[id]
	return s, ok
}

// Patch / uninstall ----------------------------------------------------------------------

// PatchRequest changes a plugin. Config is nil when absent. Merge makes Config
// a partial update (null removes a key); otherwise the non-secret config is
// replaced by Config (secrets: "" or omitted keeps, null clears).
type PatchRequest struct {
	Enabled *bool          `json:"enabled"`
	Config  map[string]any `json:"config"`
	Merge   bool           `json:"-"`
	Lang    string         `json:"-"` // language of error messages
}

func (m *Manager) Patch(id string, p PatchRequest) (Info, error) {
	e, rec, err := m.entryFor(id)
	if err != nil {
		return Info{}, err
	}
	if p.Config != nil {
		if e.err != nil {
			return Info{}, conflictf("plugin is broken: %v", e.err)
		}
		in := p.Config
		if p.Merge {
			in = map[string]any{}
			for k, v := range rec.Config {
				in[k] = v
			}
			for k, v := range p.Config {
				if v == nil {
					if isSecretKey(e.man.ConfigSchema, k) {
						in[k] = nil // clears the secret
					} else {
						delete(in, k)
					}
				} else {
					in[k] = v
				}
			}
		}
		clean, err := cleanConfig(e.man.ConfigSchema, in, rec.Config, true, NormLang(p.Lang))
		if err != nil {
			return Info{}, invalidf("%v", err)
		}
		if rec, err = m.st.SetPluginConfig(id, clean); err != nil {
			return Info{}, err
		}
	}
	if p.Enabled != nil {
		if rec, err = m.st.SetPluginEnabled(id, *p.Enabled); err != nil {
			return Info{}, err
		}
		if !*p.Enabled {
			e.pool.purge()
		}
	}
	m.forgetCache(id)
	m.sched.kick()
	return m.info(rec, true), nil
}

func isSecretKey(schema []ConfigField, key string) bool {
	for _, f := range schema {
		if f.Key == key {
			return f.Type == "secret"
		}
	}
	return false
}

// Uninstall removes a plugin with its widgets, storage and downloaded wallpapers.
func (m *Manager) Uninstall(id string) error {
	if err := m.st.DeletePlugin(id); err != nil {
		return err
	}
	m.drop(id)
	m.sched.kick()
	return nil
}

// Helpers ------------------------------------------------------------------------------------

type cacheItem struct {
	stamp   string
	data    any
	updated int64
	next    int64
	err     error
	errTill time.Time
}

// forgetCache removes cached widget data and wallpapers of a plugin.
func (m *Manager) forgetCache(plugin string) {
	m.cmu.Lock()
	for k := range m.cache {
		if strings.HasPrefix(k, "wp|"+plugin+"|") {
			delete(m.cache, k)
		}
	}
	m.cmu.Unlock()
}

func (m *Manager) forgetWidget(id string) {
	m.cmu.Lock()
	for k := range m.cache {
		if strings.HasPrefix(k, "w|"+id+"|") {
			delete(m.cache, k)
		}
	}
	m.cmu.Unlock()
}

func hashJSON(vs ...any) string {
	h := sha256.New()
	for _, v := range vs {
		b, _ := json.Marshal(v)
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func nowMs() int64 { return time.Now().UnixMilli() }
