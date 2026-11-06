package plugin

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jae-jae/genkan/internal/store"
)

// Manifest is the validated `export const manifest` of a plugin. Its JSON
// form is the metadata part of PluginInfo (docs/PLUGINS.md §5).
type Manifest struct {
	ID           string          `json:"id"`
	Name         LocalizedString `json:"name"`
	Version      string          `json:"version"`
	Description  LocalizedString `json:"description"`
	Author       string          `json:"author"`
	Homepage     string          `json:"homepage"`
	Icon         string          `json:"icon"`
	Permissions  Permissions     `json:"permissions"`
	ConfigSchema []ConfigField   `json:"config_schema"`
	Wallpapers   []WallpaperDecl `json:"wallpapers"`
	Widgets      []WidgetDecl    `json:"widgets"`
	Search       *SearchDecl     `json:"search"`
	Schedules    []ScheduleDecl  `json:"schedules"`

	messages *Messages // the module's `messages` table (not part of the wire format)
}

type Permissions struct {
	SSH   bool     `json:"ssh,omitempty"` // fixed Linux resource collection
	Fetch []string `json:"fetch"`
	Links string   `json:"links"` // "" | "read" | "write"
}

type ConfigOption struct {
	Value string          `json:"value"`
	Label LocalizedString `json:"label"`
}

type ConfigField struct {
	Key        string          `json:"key"`
	Type       string          `json:"type"`
	Label      LocalizedString `json:"label"`
	Help       LocalizedString `json:"help,omitzero"`
	Default    any             `json:"default,omitempty"`
	Required   bool            `json:"required,omitempty"`
	Options    []ConfigOption  `json:"options,omitempty"`
	Min        *float64        `json:"min,omitempty"`
	Max        *float64        `json:"max,omitempty"`
	AllowFetch bool            `json:"allowFetch,omitempty"`
	Multiline  bool            `json:"multiline,omitempty"`
	VerifyTLS  bool            `json:"verifyTLS,omitempty"`
}

type WallpaperDecl struct {
	ID      string          `json:"id"`
	Name    LocalizedString `json:"name"`
	Refresh int             `json:"refresh"`
}

type WidgetDecl struct {
	ID      string          `json:"id"`
	Name    LocalizedString `json:"name"`
	Size    string          `json:"size"`
	Refresh int             `json:"refresh"`
	Config  []ConfigField   `json:"config"`
}

type SearchDecl struct {
	Name   LocalizedString `json:"name"`
	Prefix string          `json:"prefix"`
}

type ScheduleDecl struct {
	ID    string          `json:"id"`
	Name  LocalizedString `json:"name,omitzero"`
	Every int             `json:"every"`
}

// Defaults and limits from the contract.
const (
	defaultWallpaperRefresh = 3600
	minWallpaperRefresh     = 300
	defaultWidgetRefresh    = 300
	minWidgetRefresh        = 30
	minScheduleEvery        = 60
	maxConfigFields         = 30
)

var (
	hostRe      = regexp.MustCompile(`^(\*|(\*\.)?[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?)$`)
	configKeyRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,39}$`)
	iconRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
)

// Hooks a capability needs.
const (
	hookWallpaper = "wallpaper"
	hookWidget    = "widget"
	hookSearch    = "search"
	hookSchedule  = "schedule"
	hookAction    = "action"
)

// Capabilities lists the exported functions the manifest needs.
func (m *Manifest) requiredExports() []string {
	var out []string
	if len(m.Wallpapers) > 0 {
		out = append(out, hookWallpaper)
	}
	if len(m.Widgets) > 0 {
		out = append(out, hookWidget)
	}
	if m.Search != nil {
		out = append(out, hookSearch)
	}
	if len(m.Schedules) > 0 {
		out = append(out, hookSchedule)
	}
	return out
}

func (m *Manifest) widget(id string) *WidgetDecl {
	for i := range m.Widgets {
		if m.Widgets[i].ID == id {
			return &m.Widgets[i]
		}
	}
	return nil
}

func (m *Manifest) wallpaper(id string) *WallpaperDecl {
	for i := range m.Wallpapers {
		if m.Wallpapers[i].ID == id {
			return &m.Wallpapers[i]
		}
	}
	return nil
}

func (m *Manifest) schedule(id string) *ScheduleDecl {
	for i := range m.Schedules {
		if m.Schedules[i].ID == id {
			return &m.Schedules[i]
		}
	}
	return nil
}

// Parsing helpers -------------------------------------------------------------------

type obj map[string]any

func asObj(v any, path string) (obj, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", path)
	}
	return m, nil
}

func asList(v any, path string) ([]any, error) {
	l, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", path)
	}
	return l, nil
}

func hasCtl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// str reads an optional or required string field with a rune limit.
func (o obj) str(key, path string, required bool, max int) (string, error) {
	v, ok := o[key]
	if !ok || v == nil {
		if required {
			return "", fmt.Errorf("%s.%s is required", path, key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s.%s must be a string", path, key)
	}
	s = strings.TrimSpace(s)
	if required && s == "" {
		return "", fmt.Errorf("%s.%s is required", path, key)
	}
	if utf8.RuneCountInString(s) > max {
		return "", fmt.Errorf("%s.%s is too long (max %d characters)", path, key, max)
	}
	if hasCtl(s) {
		return "", fmt.Errorf("%s.%s contains control characters", path, key)
	}
	return s, nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	}
	return 0, false
}

// intField reads an optional integer with a default and a minimum.
func (o obj) intField(key, path string, def, min int) (int, error) {
	v, ok := o[key]
	if !ok || v == nil {
		return def, nil
	}
	f, ok := toFloat(v)
	if !ok || f != float64(int(f)) {
		return 0, fmt.Errorf("%s.%s must be an integer", path, key)
	}
	if int(f) < min {
		return 0, fmt.Errorf("%s.%s must be at least %d", path, key, min)
	}
	if f > 31536000 {
		return 0, fmt.Errorf("%s.%s is too large", path, key)
	}
	return int(f), nil
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

// parseManifest validates the manifest object read from the module.
func parseManifest(raw any) (*Manifest, error) {
	o, err := asObj(raw, "manifest")
	if err != nil {
		return nil, err
	}
	const p = "manifest"
	m := &Manifest{Permissions: Permissions{Fetch: []string{}}, ConfigSchema: []ConfigField{}, Wallpapers: []WallpaperDecl{}, Widgets: []WidgetDecl{}, Schedules: []ScheduleDecl{}}
	if m.ID, err = o.str("id", p, true, 40); err != nil {
		return nil, err
	}
	if !store.ValidPluginID(m.ID) {
		return nil, fmt.Errorf("manifest.id %q is invalid: use 2-40 characters of a-z, 0-9 and '-', starting with a letter or digit", m.ID)
	}
	if m.Name, err = o.lstr("name", p, true, 40); err != nil {
		return nil, err
	}
	if m.Version, err = o.str("version", p, true, 20); err != nil {
		return nil, err
	}
	if m.Description, err = o.lstr("description", p, false, 300); err != nil {
		return nil, err
	}
	if m.Author, err = o.str("author", p, false, 60); err != nil {
		return nil, err
	}
	if m.Homepage, err = o.str("homepage", p, false, 2048); err != nil {
		return nil, err
	}
	if m.Homepage != "" && !httpURL(m.Homepage) {
		return nil, fmt.Errorf("manifest.homepage must be an http(s) URL")
	}
	if m.Icon, err = o.str("icon", p, false, 40); err != nil {
		return nil, err
	}
	if m.Icon != "" && !iconRe.MatchString(m.Icon) {
		return nil, fmt.Errorf("manifest.icon must be a Lucide icon name such as \"cloud\"")
	}

	if pv, ok := o["permissions"]; ok && pv != nil {
		po, err := asObj(pv, "manifest.permissions")
		if err != nil {
			return nil, err
		}
		if fv, ok := po["fetch"]; ok && fv != nil {
			l, err := asList(fv, "manifest.permissions.fetch")
			if err != nil {
				return nil, err
			}
			if len(l) > 30 {
				return nil, fmt.Errorf("manifest.permissions.fetch has too many entries (max 30)")
			}
			for i, h := range l {
				s, ok := h.(string)
				s = strings.ToLower(strings.TrimSpace(s))
				if !ok || !hostRe.MatchString(s) || len(s) > 255 {
					return nil, fmt.Errorf("manifest.permissions.fetch[%d] must be a host such as \"api.example.com\", \"*.example.com\" or \"*\"", i)
				}
				m.Permissions.Fetch = append(m.Permissions.Fetch, s)
			}
		}
		if sv, ok := po["ssh"]; ok {
			b, valid := sv.(bool)
			if !valid {
				return nil, fmt.Errorf("manifest.permissions.ssh must be a boolean")
			}
			m.Permissions.SSH = b
		}
		if lv, ok := po["links"]; ok && lv != nil {
			s, _ := lv.(string)
			if s != "read" && s != "write" && s != "" {
				return nil, fmt.Errorf(`manifest.permissions.links must be "read" or "write"`)
			}
			m.Permissions.Links = s
		}
	}

	if cv, ok := o["config"]; ok && cv != nil {
		if m.ConfigSchema, err = parseFields(cv, "manifest.config"); err != nil {
			return nil, err
		}
	}

	if wv, ok := o["wallpapers"]; ok && wv != nil {
		l, err := asList(wv, "manifest.wallpapers")
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for i, it := range l {
			path := fmt.Sprintf("manifest.wallpapers[%d]", i)
			wo, err := asObj(it, path)
			if err != nil {
				return nil, err
			}
			var d WallpaperDecl
			if d.ID, err = srcID(wo, path, seen); err != nil {
				return nil, err
			}
			if d.Name, err = wo.lstr("name", path, true, 40); err != nil {
				return nil, err
			}
			if d.Refresh, err = wo.intField("refresh", path, defaultWallpaperRefresh, minWallpaperRefresh); err != nil {
				return nil, err
			}
			m.Wallpapers = append(m.Wallpapers, d)
		}
	}

	if wv, ok := o["widgets"]; ok && wv != nil {
		l, err := asList(wv, "manifest.widgets")
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for i, it := range l {
			path := fmt.Sprintf("manifest.widgets[%d]", i)
			wo, err := asObj(it, path)
			if err != nil {
				return nil, err
			}
			d := WidgetDecl{Config: []ConfigField{}}
			if d.ID, err = srcID(wo, path, seen); err != nil {
				return nil, err
			}
			if d.Name, err = wo.lstr("name", path, true, 40); err != nil {
				return nil, err
			}
			if d.Size, err = wo.str("size", path, false, 1); err != nil {
				return nil, err
			}
			switch d.Size {
			case "":
				d.Size = "m"
			case "s", "m", "l":
			default:
				return nil, fmt.Errorf(`%s.size must be "s", "m" or "l"`, path)
			}
			if d.Refresh, err = wo.intField("refresh", path, defaultWidgetRefresh, minWidgetRefresh); err != nil {
				return nil, err
			}
			if cv, ok := wo["config"]; ok && cv != nil {
				if d.Config, err = parseFields(cv, path+".config"); err != nil {
					return nil, err
				}
			}
			m.Widgets = append(m.Widgets, d)
		}
	}

	if sv, ok := o["search"]; ok && sv != nil {
		so, err := asObj(sv, "manifest.search")
		if err != nil {
			return nil, err
		}
		d := &SearchDecl{}
		if d.Name, err = so.lstr("name", "manifest.search", true, 40); err != nil {
			return nil, err
		}
		if d.Prefix, err = so.str("prefix", "manifest.search", false, 20); err != nil {
			return nil, err
		}
		if strings.ContainsFunc(d.Prefix, unicode.IsSpace) {
			return nil, fmt.Errorf("manifest.search.prefix must not contain spaces")
		}
		d.Prefix = strings.ToLower(d.Prefix)
		m.Search = d
	}

	if sv, ok := o["schedules"]; ok && sv != nil {
		l, err := asList(sv, "manifest.schedules")
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for i, it := range l {
			path := fmt.Sprintf("manifest.schedules[%d]", i)
			so, err := asObj(it, path)
			if err != nil {
				return nil, err
			}
			var d ScheduleDecl
			if d.ID, err = srcID(so, path, seen); err != nil {
				return nil, err
			}
			if d.Name, err = so.lstr("name", path, false, 40); err != nil {
				return nil, err
			}
			if _, ok := so["every"]; !ok {
				return nil, fmt.Errorf("%s.every is required", path)
			}
			if d.Every, err = so.intField("every", path, 0, minScheduleEvery); err != nil {
				return nil, err
			}
			m.Schedules = append(m.Schedules, d)
		}
	}

	if len(m.requiredExports()) == 0 {
		return nil, fmt.Errorf("manifest declares no capability: add at least one of wallpapers, widgets, search or schedules")
	}
	return m, nil
}

func srcID(o obj, path string, seen map[string]bool) (string, error) {
	id, err := o.str("id", path, true, 40)
	if err != nil {
		return "", err
	}
	if !store.ValidSourceID(id) {
		return "", fmt.Errorf("%s.id %q is invalid: use a-z, 0-9, '-' and '_'", path, id)
	}
	if seen[id] {
		return "", fmt.Errorf("%s.id %q is duplicated", path, id)
	}
	seen[id] = true
	return id, nil
}

func parseFields(v any, path string) ([]ConfigField, error) {
	l, err := asList(v, path)
	if err != nil {
		return nil, err
	}
	if len(l) > maxConfigFields {
		return nil, fmt.Errorf("%s has too many fields (max %d)", path, maxConfigFields)
	}
	out := make([]ConfigField, 0, len(l))
	seen := map[string]bool{}
	for i, it := range l {
		fp := fmt.Sprintf("%s[%d]", path, i)
		fo, err := asObj(it, fp)
		if err != nil {
			return nil, err
		}
		var f ConfigField
		if f.Key, err = fo.str("key", fp, true, 40); err != nil {
			return nil, err
		}
		if !configKeyRe.MatchString(f.Key) {
			return nil, fmt.Errorf("%s.key %q is invalid: use letters, digits and '_', starting with a letter", fp, f.Key)
		}
		if seen[f.Key] {
			return nil, fmt.Errorf("%s.key %q is duplicated", fp, f.Key)
		}
		seen[f.Key] = true
		if f.Type, err = fo.str("type", fp, true, 20); err != nil {
			return nil, err
		}
		switch f.Type {
		case "string", "text", "number", "boolean", "select", "secret", "url":
		default:
			return nil, fmt.Errorf("%s.type %q is not one of string, text, number, boolean, select, secret, url", fp, f.Type)
		}
		if f.Label, err = fo.lstr("label", fp, true, 60); err != nil {
			return nil, err
		}
		if f.Help, err = fo.lstr("help", fp, false, 200); err != nil {
			return nil, err
		}
		if rv, ok := fo["required"]; ok && rv != nil {
			b, ok := rv.(bool)
			if !ok {
				return nil, fmt.Errorf("%s.required must be a boolean", fp)
			}
			f.Required = b
		}
		if av, ok := fo["allowFetch"]; ok && av != nil {
			b, ok := av.(bool)
			if !ok {
				return nil, fmt.Errorf("%s.allowFetch must be a boolean", fp)
			}
			if b && f.Type != "url" {
				return nil, fmt.Errorf(`%s.allowFetch is only valid for type "url"`, fp)
			}
			f.AllowFetch = b
		}
		if mv, ok := fo["multiline"]; ok {
			b, valid := mv.(bool)
			if !valid || (b && f.Type != "secret") {
				return nil, fmt.Errorf("%s.multiline requires a secret field and a boolean", fp)
			}
			f.Multiline = b
		}
		if v, ok := fo["verifyTLS"]; ok {
			b, valid := v.(bool)
			if !valid {
				return nil, fmt.Errorf("%s.verifyTLS must be a boolean", fp)
			}
			if f.Type != "url" || !f.AllowFetch {
				return nil, fmt.Errorf(`%s.verifyTLS requires type "url" and allowFetch: true`, fp)
			}
			f.VerifyTLS = b
		}
		for _, k := range []string{"min", "max"} {
			if nv, ok := fo[k]; ok && nv != nil {
				n, isNum := toFloat(nv)
				if !isNum || f.Type != "number" {
					return nil, fmt.Errorf(`%s.%s is only valid for type "number" and must be a number`, fp, k)
				}
				if k == "min" {
					f.Min = &n
				} else {
					f.Max = &n
				}
			}
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			return nil, fmt.Errorf("%s.min is greater than max", fp)
		}
		if f.Type == "select" {
			ol, err := asList(fo["options"], fp+".options")
			if err != nil || len(ol) == 0 || len(ol) > 50 {
				return nil, fmt.Errorf("%s.options must be a non-empty array (max 50) for type \"select\"", fp)
			}
			for j, ov := range ol {
				op := fmt.Sprintf("%s.options[%d]", fp, j)
				oo, err := asObj(ov, op)
				if err != nil {
					return nil, err
				}
				var opt ConfigOption
				if opt.Value, err = oo.str("value", op, true, 100); err != nil {
					return nil, err
				}
				if opt.Label, err = oo.lstr("label", op, true, 100); err != nil {
					return nil, err
				}
				f.Options = append(f.Options, opt)
			}
		}
		if dv, ok := fo["default"]; ok && dv != nil {
			f.Default, err = checkValue(&f, dv)
			if err != nil {
				return nil, fmt.Errorf("%s.default: %v", fp, err)
			}
		}
		out = append(out, f)
	}
	return out, nil
}

// Config values -----------------------------------------------------------------

const (
	maxConfigString = 2000
	maxConfigText   = 5000
)

// checkValue validates v against the field and returns its normalized form.
func checkValue(f *ConfigField, v any) (any, error) {
	switch f.Type {
	case "string", "secret", "text", "url", "select":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("must be a string")
		}
		if f.Type != "text" {
			s = strings.TrimSpace(s)
		}
		limit := maxConfigString
		if f.Type == "text" || f.Multiline {
			limit = maxConfigText
		}
		if utf8.RuneCountInString(s) > limit {
			return nil, fmt.Errorf("is too long (max %d characters)", limit)
		}
		for _, r := range s {
			if unicode.IsControl(r) && !((f.Type == "text" || f.Multiline) && (r == '\n' || r == '\t' || r == '\r')) {
				return nil, fmt.Errorf("contains control characters")
			}
		}
		switch f.Type {
		case "url":
			if s != "" && (len(s) > 2048 || !httpURL(s)) {
				return nil, fmt.Errorf("must be an http(s) URL")
			}
		case "select":
			for _, o := range f.Options {
				if o.Value == s {
					return s, nil
				}
			}
			return nil, fmt.Errorf("must be one of the listed options")
		}
		return s, nil
	case "number":
		n, ok := toFloat(v)
		if !ok {
			return nil, fmt.Errorf("must be a number")
		}
		if f.Min != nil && n < *f.Min {
			return nil, fmt.Errorf("must be at least %v", *f.Min)
		}
		if f.Max != nil && n > *f.Max {
			return nil, fmt.Errorf("must be at most %v", *f.Max)
		}
		return n, nil
	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("must be true or false")
		}
		return b, nil
	}
	return nil, fmt.Errorf("unknown field type")
}

// cleanConfig validates in against schema and returns the config to store.
// Unknown keys are dropped. Empty values and null remove a key (back to its
// default), except secrets: "" or omitted keeps the stored value (prev) and
// null clears it. With checkRequired, missing required values are an error;
// field labels in errors are resolved with lang.
func cleanConfig(schema []ConfigField, in, prev map[string]any, checkRequired bool, lang string) (map[string]any, error) {
	out := map[string]any{}
	for i := range schema {
		f := &schema[i]
		v, present := in[f.Key]
		if f.Type == "secret" {
			switch {
			case present && v == nil:
			case !present || v == "":
				if pv, ok := prev[f.Key]; ok {
					out[f.Key] = pv
				}
			default:
				nv, err := checkValue(f, v)
				if err != nil {
					return nil, fmt.Errorf("%s %v", f.Label.Resolve(lang), err)
				}
				out[f.Key] = nv
			}
		} else if present && v != nil && v != "" {
			nv, err := checkValue(f, v)
			if err != nil {
				return nil, fmt.Errorf("%s %v", f.Label.Resolve(lang), err)
			}
			if s, isStr := nv.(string); !isStr || s != "" {
				out[f.Key] = nv
			}
		}
		if checkRequired && f.Required {
			_, has := out[f.Key]
			if !has && f.Type != "boolean" && f.Default == nil {
				return nil, fmt.Errorf("%s is required", f.Label.Resolve(lang))
			}
		}
	}
	return out, nil
}

// withDefaults returns cfg plus the defaults of fields that have no value. It never mutates cfg.
func withDefaults(schema []ConfigField, cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg)+len(schema))
	for k, v := range cfg {
		out[k] = v
	}
	for i := range schema {
		f := &schema[i]
		if _, ok := out[f.Key]; !ok && f.Default != nil {
			out[f.Key] = f.Default
		}
	}
	return out
}

func secretKeys(schema []ConfigField) []string {
	var out []string
	for _, f := range schema {
		if f.Type == "secret" {
			out = append(out, f.Key)
		}
	}
	return out
}

// secretValues lists the non-empty stored secret values of cfg.
func secretValues(schema []ConfigField, cfg map[string]any) []string {
	var out []string
	for _, k := range secretKeys(schema) {
		if s, ok := cfg[k].(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// maskConfig returns cfg for a browser: secret values blanked (admin) or removed (guest),
// plus the keys that have a secret value.
func maskConfig(schema []ConfigField, cfg map[string]any, keepKeys bool) (map[string]any, []string) {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	set := []string{}
	for _, f := range schema {
		if f.Type != "secret" {
			continue
		}
		if s, ok := out[f.Key].(string); ok && s != "" {
			set = append(set, f.Key)
		}
		if keepKeys {
			out[f.Key] = ""
		} else {
			delete(out, f.Key)
		}
	}
	// keys that are not in the schema are not shown at all
	known := map[string]bool{}
	for _, f := range schema {
		known[f.Key] = true
	}
	for k := range out {
		if !known[k] {
			delete(out, k)
		}
	}
	return out, set
}

// fetchPolicyFromConfig separates network authorization from certificate policy.
// Both lists use normalized host:port entries, including default ports.
func fetchPolicyFromConfig(schema []ConfigField, cfg map[string]any) (hosts, verified []string) {
	for _, f := range schema {
		if !f.AllowFetch {
			continue
		}
		s, _ := cfg[f.Key].(string)
		u, err := url.Parse(s)
		if err != nil || u.Hostname() == "" {
			continue
		}
		// host:port, with the scheme's default port when the URL names none
		port := u.Port()
		if port == "" {
			port = "80"
			if u.Scheme == "https" {
				port = "443"
			}
		}
		host := net.JoinHostPort(strings.ToLower(u.Hostname()), port)
		hosts = append(hosts, host)
		if f.VerifyTLS {
			verified = append(verified, host)
		}
	}
	return hosts, verified
}

// A strict declaration wins even when another config field grants legacy TLS
// behavior for the same host (including across plugin and instance config).
func lenientHosts(hosts, verified []string) []string {
	strict := make(map[string]bool, len(verified))
	for _, host := range verified {
		strict[host] = true
	}
	var out []string
	for _, host := range hosts {
		if !strict[host] {
			out = append(out, host)
		}
	}
	return out
}
