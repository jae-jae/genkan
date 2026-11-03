package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Plugin limits.
const (
	MaxPluginSource = 512 << 10 // bytes of source per plugin
	maxPlugins      = 50
	maxWidgets      = 200
	maxConfigJSON   = 16 << 10 // serialized size of one config object
)

// PluginWallpaperDir is the sub directory of uploads/ that holds downloaded
// plugin wallpapers (served through /uploads/plugin-wallpapers/<name>).
const PluginWallpaperDir = "plugin-wallpapers"

var (
	pluginIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)
	sourceIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
)

// ValidPluginID reports whether id is a legal plugin id (also a safe file name).
func ValidPluginID(id string) bool { return pluginIDRe.MatchString(id) }

// ValidSourceID reports whether id is a legal wallpaper/widget/schedule id.
func ValidSourceID(id string) bool { return sourceIDRe.MatchString(id) }

// Plugin is the stored state of an installed plugin. The source lives in
// <data>/plugins/<id>.<lang>, never in genkan.json.
type Plugin struct {
	ID          string         `json:"id"`
	Enabled     bool           `json:"enabled"`
	Lang        string         `json:"lang,omitempty"` // "js" (default) | "ts"
	Config      map[string]any `json:"config"`
	InstalledAt string         `json:"installed_at"`
	UpdatedAt   string         `json:"updated_at"`
	SourceURL   string         `json:"source_url"`

	// Rev changes whenever the source is replaced (in memory only); the
	// plugin manager uses it to invalidate compiled modules.
	Rev uint64 `json:"-"`
}

// WidgetInstance is one placed widget of a plugin.
type WidgetInstance struct {
	ID          string         `json:"id"`
	Plugin      string         `json:"plugin"`
	Widget      string         `json:"widget"`
	WorkspaceID string         `json:"workspace_id"`
	Config      map[string]any `json:"config"`
	Sort        int            `json:"sort"`
}

// ExportPlugin is a plugin in the portable export: state plus source, without
// secret config values.
type ExportPlugin struct {
	Plugin
	Source string `json:"source"`
}

// SecretResolver tells the store which config keys are secret: the plugin
// level keys and, per widget id of the plugin, the instance level keys.
// ok=false (unknown or broken plugin) makes exports drop that plugin's config.
type SecretResolver func(pluginID string) (pluginKeys []string, widgetKeys map[string][]string, ok bool)

// SetSecretResolver installs the resolver used by Export. Without one every
// config is dropped from exports.
func (s *Store) SetSecretResolver(fn SecretResolver) {
	s.mu.Lock()
	s.secrets = fn
	s.mu.Unlock()
}

// PluginWallpaperPrefix is the file name prefix of all wallpapers a plugin downloaded.
func PluginWallpaperPrefix(pluginID string) string {
	sum := sha256.Sum256([]byte(pluginID))
	return hex.EncodeToString(sum[:])[:8] + "-"
}

// PluginSourcePrefix is the file name prefix of the wallpapers of one source.
func PluginSourcePrefix(pluginID, sourceID string) string {
	sum := sha256.Sum256([]byte(sourceID))
	return PluginWallpaperPrefix(pluginID) + hex.EncodeToString(sum[:])[:6] + "-"
}

// Helpers -------------------------------------------------------------------------

func cloneConfig(c map[string]any) map[string]any {
	if c == nil {
		return map[string]any{}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

// CleanConfigJSON validates that c is JSON-serializable and small, and returns a deep copy.
func cleanStoredConfig(c map[string]any) (map[string]any, error) {
	if c == nil {
		return map[string]any{}, nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, invalid("config is not valid JSON")
	}
	if len(b) > maxConfigJSON {
		return nil, invalid("config is too large (max %d KiB)", maxConfigJSON>>10)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil || out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func (s *Store) pluginsDir() string { return filepath.Join(s.dir, "plugins") }

func (s *Store) sourcePath(id, lang string) string {
	return filepath.Join(s.pluginsDir(), id+"."+lang)
}

func (s *Store) plIndex(id string) int {
	for i := range s.d.Plugins {
		if s.d.Plugins[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) wgIndex(id string) int {
	for i := range s.d.Widgets {
		if s.d.Widgets[i].ID == id {
			return i
		}
	}
	return -1
}

func clonePlugin(p Plugin) Plugin {
	p.Config = cloneConfig(p.Config)
	return p
}

func cloneWidget(w WidgetInstance) WidgetInstance {
	w.Config = cloneConfig(w.Config)
	return w
}

// AtomicWriteFile writes data to path via a temp file in the same directory,
// fsync and rename.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// removePluginFiles deletes a plugin's source, KV file and downloaded wallpapers (best effort).
func (s *Store) removePluginFiles(id string) {
	for _, ext := range []string{".js", ".ts", ".kv.json"} {
		_ = os.Remove(filepath.Join(s.pluginsDir(), id+ext))
	}
	dir := filepath.Join(s.dir, "uploads", PluginWallpaperDir)
	pre := PluginWallpaperPrefix(id)
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), pre) {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
}

func cleanSource(src, lang string) (string, error) {
	if lang == "" {
		lang = "js"
	}
	if lang != "js" && lang != "ts" {
		return "", invalid(`lang must be "js" or "ts"`)
	}
	return lang, validateSourceText(src)
}

func validateSourceText(src string) error {
	if strings.TrimSpace(src) == "" {
		return invalid("plugin source is empty")
	}
	if len(src) > MaxPluginSource {
		return invalid("plugin source is too large (max %d KiB)", MaxPluginSource>>10)
	}
	if !utf8.ValidString(src) {
		return invalid("plugin source must be valid UTF-8")
	}
	return nil
}

// Reads ---------------------------------------------------------------------------

// Plugins returns copies of all installed plugins, ordered by id.
func (s *Store) Plugins() []Plugin {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Plugin, len(s.d.Plugins))
	for i, p := range s.d.Plugins {
		out[i] = clonePlugin(p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetPlugin returns one plugin.
func (s *Store) GetPlugin(id string) (Plugin, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if i := s.plIndex(id); i >= 0 {
		return clonePlugin(s.d.Plugins[i]), true
	}
	return Plugin{}, false
}

// PluginSource returns a plugin's source text and language.
func (s *Store) PluginSource(id string) (src, lang string, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := s.plIndex(id)
	if i < 0 {
		return "", "", notFound("plugin", id)
	}
	lang = s.d.Plugins[i].Lang
	b, err := os.ReadFile(s.sourcePath(id, lang))
	if err != nil {
		return "", lang, fmt.Errorf("read plugin source: %w", err)
	}
	return string(b), lang, nil
}

// Widgets returns copies of all widget instances in display order.
func (s *Store) Widgets() []WidgetInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.widgetsLocked()
}

func (s *Store) widgetsLocked() []WidgetInstance {
	out := make([]WidgetInstance, len(s.d.Widgets))
	for i, w := range s.d.Widgets {
		out[i] = cloneWidget(w)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort < out[j].Sort })
	return out
}

// GetWidget returns one widget instance.
func (s *Store) GetWidget(id string) (WidgetInstance, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if i := s.wgIndex(id); i >= 0 {
		return cloneWidget(s.d.Widgets[i]), true
	}
	return WidgetInstance{}, false
}

// Plugin writes -----------------------------------------------------------------------

// PluginSave installs or updates a plugin's source.
type PluginSave struct {
	ID        string
	Source    string
	Lang      string // "js" (default) | "ts"
	SourceURL string
	Enabled   *bool // nil: keep (existing) or enable (new)
}

// SavePlugin writes the source and creates or updates the record. An existing
// plugin keeps its config, widgets and enabled state (unless in.Enabled is set).
func (s *Store) SavePlugin(in PluginSave) (Plugin, bool, error) {
	if !ValidPluginID(in.ID) {
		return Plugin{}, false, invalid("invalid plugin id %q", in.ID)
	}
	lang, err := cleanSource(in.Source, in.Lang)
	if err != nil {
		return Plugin{}, false, err
	}
	if len(in.SourceURL) > maxURL {
		return Plugin{}, false, invalid("source_url is too long")
	}
	var out Plugin
	created := false
	err = s.commit(true, func() (bool, error) {
		t := now()
		i := s.plIndex(in.ID)
		if i < 0 {
			if len(s.d.Plugins) >= maxPlugins {
				return false, conflict("too many plugins (max %d)", maxPlugins)
			}
			enabled := true
			if in.Enabled != nil {
				enabled = *in.Enabled
			}
			s.d.Plugins = append(s.d.Plugins, Plugin{ID: in.ID, Enabled: enabled, Config: map[string]any{}, InstalledAt: t})
			i = len(s.d.Plugins) - 1
			created = true
		}
		if err := AtomicWriteFile(s.sourcePath(in.ID, lang), []byte(in.Source), 0o600); err != nil {
			if created {
				s.d.Plugins = s.d.Plugins[:i]
			}
			return false, fmt.Errorf("write plugin source: %w", err)
		}
		p := &s.d.Plugins[i]
		if p.Lang != "" && p.Lang != lang {
			_ = os.Remove(s.sourcePath(in.ID, p.Lang))
		}
		p.Lang = lang
		if in.Enabled != nil {
			p.Enabled = *in.Enabled
		}
		p.UpdatedAt = t
		p.SourceURL = in.SourceURL
		s.revSeq++
		p.Rev = s.revSeq
		out = clonePlugin(*p)
		return true, nil
	})
	return out, created, err
}

// SetPluginEnabled toggles a plugin.
func (s *Store) SetPluginEnabled(id string, enabled bool) (Plugin, error) {
	var out Plugin
	err := s.commit(true, func() (bool, error) {
		i := s.plIndex(id)
		if i < 0 {
			return false, notFound("plugin", id)
		}
		s.d.Plugins[i].Enabled = enabled
		out = clonePlugin(s.d.Plugins[i])
		return true, nil
	})
	return out, err
}

// SetPluginConfig replaces a plugin's stored config (already validated by the plugin manager).
func (s *Store) SetPluginConfig(id string, cfg map[string]any) (Plugin, error) {
	cfg, err := cleanStoredConfig(cfg)
	if err != nil {
		return Plugin{}, err
	}
	var out Plugin
	err = s.commit(true, func() (bool, error) {
		i := s.plIndex(id)
		if i < 0 {
			return false, notFound("plugin", id)
		}
		s.d.Plugins[i].Config = cfg
		out = clonePlugin(s.d.Plugins[i])
		return true, nil
	})
	return out, err
}

// DeletePlugin uninstalls a plugin: its widgets, source, KV file and
// downloaded wallpapers go too, and workspaces using one of its wallpapers
// fall back to the default wallpaper.
func (s *Store) DeletePlugin(id string) error {
	return s.commit(true, func() (bool, error) {
		i := s.plIndex(id)
		if i < 0 {
			return false, notFound("plugin", id)
		}
		s.d.Plugins = append(s.d.Plugins[:i], s.d.Plugins[i+1:]...)
		s.dropPluginRefsLocked(id)
		s.removePluginFiles(id)
		return true, nil
	})
}

// dropPluginRefsLocked removes widgets of plugin id and resets wallpapers using it.
func (s *Store) dropPluginRefsLocked(id string) {
	ws := s.d.Widgets[:0]
	for _, w := range s.d.Widgets {
		if w.Plugin != id {
			ws = append(ws, w)
		}
	}
	s.d.Widgets = ws
	for i := range s.d.Workspaces {
		if p, _, ok := parsePluginWallpaper(s.d.Workspaces[i].Wallpaper); ok && p == id {
			s.d.Workspaces[i].Wallpaper = DefaultWallpaper
		}
	}
}

// Widget writes --------------------------------------------------------------------------

type WidgetInput struct {
	Plugin      string         `json:"plugin"`
	Widget      string         `json:"widget"`
	WorkspaceID string         `json:"workspace_id"`
	Config      map[string]any `json:"config"`
}

type WidgetPatch struct {
	WorkspaceID *string         `json:"workspace_id"`
	Config      *map[string]any `json:"config"`
	Sort        *int            `json:"sort"`
}

func (s *Store) checkWidgetWorkspace(ws string) error {
	if ws != "" && s.wsIndex(ws) < 0 {
		return notFound("workspace", ws)
	}
	return nil
}

func (s *Store) CreateWidget(in WidgetInput) (WidgetInstance, error) {
	cfg, err := cleanStoredConfig(in.Config)
	if err != nil {
		return WidgetInstance{}, err
	}
	if !ValidSourceID(in.Widget) {
		return WidgetInstance{}, invalid("invalid widget id %q", in.Widget)
	}
	var w WidgetInstance
	err = s.commit(true, func() (bool, error) {
		if s.plIndex(in.Plugin) < 0 {
			return false, notFound("plugin", in.Plugin)
		}
		if err := s.checkWidgetWorkspace(in.WorkspaceID); err != nil {
			return false, err
		}
		if len(s.d.Widgets) >= maxWidgets {
			return false, conflict("too many widgets (max %d)", maxWidgets)
		}
		next := 0
		for _, x := range s.d.Widgets {
			if x.Sort >= next {
				next = x.Sort + 1
			}
		}
		w = WidgetInstance{ID: NewID(), Plugin: in.Plugin, Widget: in.Widget, WorkspaceID: in.WorkspaceID, Config: cfg, Sort: next}
		s.d.Widgets = append(s.d.Widgets, w)
		w = cloneWidget(w)
		return true, nil
	})
	return w, err
}

func (s *Store) UpdateWidget(id string, p WidgetPatch) (WidgetInstance, error) {
	var cfg map[string]any
	if p.Config != nil {
		var err error
		if cfg, err = cleanStoredConfig(*p.Config); err != nil {
			return WidgetInstance{}, err
		}
	}
	var out WidgetInstance
	err := s.commit(true, func() (bool, error) {
		i := s.wgIndex(id)
		if i < 0 {
			return false, notFound("widget", id)
		}
		w := s.d.Widgets[i]
		if p.WorkspaceID != nil {
			if err := s.checkWidgetWorkspace(*p.WorkspaceID); err != nil {
				return false, err
			}
			w.WorkspaceID = *p.WorkspaceID
		}
		if p.Config != nil {
			w.Config = cfg
		}
		if p.Sort != nil {
			w.Sort = *p.Sort
		}
		s.d.Widgets[i] = w
		out = cloneWidget(w)
		return true, nil
	})
	return out, err
}

func (s *Store) DeleteWidget(id string) error {
	return s.commit(true, func() (bool, error) {
		i := s.wgIndex(id)
		if i < 0 {
			return false, notFound("widget", id)
		}
		s.d.Widgets = append(s.d.Widgets[:i], s.d.Widgets[i+1:]...)
		return true, nil
	})
}

func (s *Store) ReorderWidgets(ids []string) error {
	return s.commit(true, func() (bool, error) {
		var listed, rest []*int
		in := map[string]bool{}
		for _, id := range dedupe(ids) {
			i := s.wgIndex(id)
			if i < 0 {
				return false, notFound("widget", id)
			}
			in[id] = true
			listed = append(listed, &s.d.Widgets[i].Sort)
		}
		for i := range s.d.Widgets {
			if !in[s.d.Widgets[i].ID] {
				rest = append(rest, &s.d.Widgets[i].Sort)
			}
		}
		assignOrder(listed, rest)
		return true, nil
	})
}

// Wallpaper values -------------------------------------------------------------------------

// parsePluginWallpaper splits "plugin:<pluginId>/<sourceId>".
func parsePluginWallpaper(v string) (plugin, source string, ok bool) {
	rest, found := strings.CutPrefix(v, "plugin:")
	if !found {
		return "", "", false
	}
	plugin, source, found = strings.Cut(rest, "/")
	if !found || !ValidPluginID(plugin) || !ValidSourceID(source) {
		return "", "", false
	}
	return plugin, source, true
}

// Export / import hooks ---------------------------------------------------------------------

func stripKeys(cfg map[string]any, keys []string) map[string]any {
	cfg = cloneConfig(cfg)
	for _, k := range keys {
		delete(cfg, k)
	}
	return cfg
}

// exportPluginsLocked builds the portable plugin list and widgets. Secret
// values are removed using the resolver; unknown plugins export without config.
func (s *Store) exportPluginsLocked() ([]ExportPlugin, []WidgetInstance) {
	var plugins []ExportPlugin
	secretsOf := map[string]map[string][]string{} // plugin -> widget id -> keys
	known := map[string]bool{}
	for _, p := range s.d.Plugins {
		b, err := os.ReadFile(s.sourcePath(p.ID, p.Lang))
		if err != nil {
			log.Printf("export: skip plugin %s: %v", p.ID, err)
			continue
		}
		ep := ExportPlugin{Plugin: clonePlugin(p), Source: string(b)}
		var pk []string
		ok := false
		if s.secrets != nil {
			var wk map[string][]string
			pk, wk, ok = s.secrets(p.ID)
			secretsOf[p.ID] = wk
		}
		known[p.ID] = ok
		if ok {
			ep.Config = stripKeys(p.Config, pk)
		} else {
			ep.Config = map[string]any{}
		}
		plugins = append(plugins, ep)
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].ID < plugins[j].ID })
	var widgets []WidgetInstance
	for _, w := range s.widgetsLocked() {
		if _, ok := known[w.Plugin]; !ok {
			continue // its plugin was not exported
		}
		if known[w.Plugin] {
			w.Config = stripKeys(w.Config, secretsOf[w.Plugin][w.Widget])
		} else {
			w.Config = map[string]any{}
		}
		widgets = append(widgets, w)
	}
	return plugins, widgets
}

// cleanImportPlugins validates exported plugins.
func cleanImportPlugins(in []ExportPlugin) ([]ExportPlugin, error) {
	if len(in) > maxPlugins {
		return nil, invalid("too many plugins (max %d)", maxPlugins)
	}
	seen := map[string]bool{}
	out := make([]ExportPlugin, 0, len(in))
	for _, p := range in {
		if !ValidPluginID(p.ID) {
			return nil, invalid("invalid plugin id %q", p.ID)
		}
		if seen[p.ID] {
			return nil, invalid("duplicate plugin id %q", p.ID)
		}
		seen[p.ID] = true
		lang, err := cleanSource(p.Source, p.Lang)
		if err != nil {
			return nil, fmt.Errorf("plugin %s: %w", p.ID, err)
		}
		cfg, err := cleanStoredConfig(p.Config)
		if err != nil {
			return nil, fmt.Errorf("plugin %s: %w", p.ID, err)
		}
		if len(p.SourceURL) > maxURL {
			p.SourceURL = ""
		}
		p.Lang, p.Config = lang, cfg
		out = append(out, p)
	}
	return out, nil
}
