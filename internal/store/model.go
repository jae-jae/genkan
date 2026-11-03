// Package store holds Genkan's in-memory data model with atomic JSON persistence.
package store

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jae-jae/genkan/internal/media"
)

// SchemaVersion 2 added plugins and widgets (additive: schema 1 files load unchanged).
const SchemaVersion = 2

// Length caps.
const (
	maxWorkspaceName = 40
	maxCategoryName  = 60
	maxTitle         = 120
	maxDescription   = 500
	maxURL           = 2048
	maxIcon          = 2048
	maxSiteTitle     = 60
	maxEmoji         = 16
)

var WallpaperPresets = []string{"aurora", "dusk", "ocean", "forest", "sakura", "graphite"}

// DefaultWallpaper is used for the first workspace, for new workspaces created
// without a wallpaper, for the empty string and as the reset target when an
// uploaded wallpaper is deleted.
const DefaultWallpaper = "preset:dusk"

type Settings struct {
	Title          string `json:"title"`
	SortMode       string `json:"sort_mode"`
	GuestAccess    bool   `json:"guest_access"`
	SearchEngine   string `json:"search_engine"`
	OpenInNewTab   bool   `json:"open_in_new_tab"`
	HealthInterval int    `json:"health_interval"`
	ShowClock      bool   `json:"show_clock"`
}

// SettingsPatch is a partial Settings update; nil fields are untouched.
type SettingsPatch struct {
	Title          *string `json:"title"`
	SortMode       *string `json:"sort_mode"`
	GuestAccess    *bool   `json:"guest_access"`
	SearchEngine   *string `json:"search_engine"`
	OpenInNewTab   *bool   `json:"open_in_new_tab"`
	HealthInterval *int    `json:"health_interval"`
	ShowClock      *bool   `json:"show_clock"`
}

func DefaultSettings() Settings {
	return Settings{
		Title:          "Genkan",
		SortMode:       "frequency",
		GuestAccess:    false,
		SearchEngine:   "https://www.google.com/search?q=%s",
		OpenInNewTab:   true,
		HealthInterval: 60,
		ShowClock:      true,
	}
}

type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	Wallpaper string `json:"wallpaper"`
	Sort      int    `json:"sort"`
}

type Category struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Sort        int    `json:"sort"`
}

type Link struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	CategoryID  string  `json:"category_id"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Description string  `json:"description"`
	Icon        string  `json:"icon"`
	Pinned      bool    `json:"pinned"`
	Sort        int     `json:"sort"`     // order within the category
	PinSort     int     `json:"pin_sort"` // order within the pinned row (only meaningful while pinned)
	Clicks      int     `json:"clicks"`
	LastUsedAt  *string `json:"last_used_at"`
	HealthCheck bool    `json:"health_check"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// UnmarshalJSON decodes a Link; data written before pin_sort existed gets
// pin_sort = sort so the pinned row keeps its previous order.
func (l *Link) UnmarshalJSON(b []byte) error {
	type plain Link
	aux := struct {
		*plain
		PinSort *int `json:"pin_sort"`
	}{plain: (*plain)(l)}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	if aux.PinSort != nil {
		l.PinSort = *aux.PinSort
	} else {
		l.PinSort = l.Sort
	}
	return nil
}

// Export is the portable (auth-free) data format.
type Export struct {
	Schema     int         `json:"schema"`
	Settings   Settings    `json:"settings"`
	Workspaces []Workspace `json:"workspaces"`
	Categories []Category  `json:"categories"`
	Links      []Link      `json:"links"`
	// Plugins carry their source but never secret config values.
	Plugins []ExportPlugin   `json:"plugins,omitempty"`
	Widgets []WidgetInstance `json:"widgets,omitempty"`
}

// Input / patch types ------------------------------------------------------

type WorkspaceInput struct {
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	Wallpaper string `json:"wallpaper"`
}

type WorkspacePatch struct {
	Name      *string `json:"name"`
	Icon      *string `json:"icon"`
	Wallpaper *string `json:"wallpaper"`
}

type LinkInput struct {
	WorkspaceID string `json:"workspace_id"`
	CategoryID  string `json:"category_id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Pinned      bool   `json:"pinned"`
	HealthCheck bool   `json:"health_check"`
}

type LinkPatch struct {
	WorkspaceID *string `json:"workspace_id"`
	CategoryID  *string `json:"category_id"`
	Title       *string `json:"title"`
	URL         *string `json:"url"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Pinned      *bool   `json:"pinned"`
	HealthCheck *bool   `json:"health_check"`
}

// LinkResult is the per-item outcome of CreateLinks.
type LinkResult struct {
	Link Link
	Err  error
}

// Errors --------------------------------------------------------------------

type Kind int

const (
	KindInvalid Kind = iota + 1
	KindNotFound
	KindConflict
)

type Error struct {
	Kind Kind
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func invalid(format string, a ...any) error {
	return &Error{KindInvalid, fmt.Sprintf(format, a...)}
}
func notFound(what, id string) error {
	return &Error{KindNotFound, fmt.Sprintf("%s not found: %s", what, id)}
}
func conflict(format string, a ...any) error {
	return &Error{KindConflict, fmt.Sprintf(format, a...)}
}

// ErrKind returns the error kind, or 0 for non-store errors.
func ErrKind(err error) Kind {
	if e, ok := err.(*Error); ok {
		return e.Kind
	}
	return 0
}

// IDs -------------------------------------------------------------------------

var idEnc = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewID returns a 12-char random lowercase base32 id.
func NewID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return idEnc.EncodeToString(b[:])[:12]
}

// Validation ---------------------------------------------------------------------

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func cleanName(field, s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", invalid("%s is required", field)
	}
	if utf8.RuneCountInString(s) > max {
		return "", invalid("%s is too long (max %d characters)", field, max)
	}
	if hasControl(s) {
		return "", invalid("%s contains control characters", field)
	}
	return s, nil
}

func cleanText(field, s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		return "", invalid("%s is too long (max %d characters)", field, max)
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return "", invalid("%s contains control characters", field)
		}
	}
	return s, nil
}

var blockedSchemes = map[string]bool{"javascript": true, "data": true, "vbscript": true, "blob": true, "file": true, "about": true}

// CleanURL validates and normalizes a link URL. A bare "host:port/path" gets
// an http:// prefix. Any scheme with a host is accepted except script-like ones.
func CleanURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", invalid("url is required")
	}
	if len(s) > maxURL {
		return "", invalid("url is too long")
	}
	low := strings.ToLower(s)
	for sch := range blockedSchemes {
		if strings.HasPrefix(low, sch+":") {
			return "", invalid("url scheme %q is not allowed", sch)
		}
	}
	if !strings.Contains(s, "://") {
		s = "http://" + strings.TrimPrefix(s, "//")
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", invalid("invalid url")
	}
	if blockedSchemes[strings.ToLower(u.Scheme)] {
		return "", invalid("url scheme %q is not allowed", u.Scheme)
	}
	if u.Scheme == "" || u.Hostname() == "" {
		return "", invalid("url must include a scheme and host")
	}
	return s, nil
}

func cleanIcon(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if len(s) > maxIcon {
		return "", invalid("icon is too long")
	}
	switch {
	case strings.HasPrefix(s, "favicon:"), strings.HasPrefix(s, "upload:"):
		if !media.SafeName(s[strings.IndexByte(s, ':')+1:]) {
			return "", invalid("invalid icon file name")
		}
		return s, nil
	case strings.HasPrefix(s, "emoji:"):
		e := strings.TrimSpace(s[len("emoji:"):])
		if e == "" || utf8.RuneCountInString(e) > maxEmoji || hasControl(e) {
			return "", invalid("invalid emoji icon")
		}
		return "emoji:" + e, nil
	case strings.HasPrefix(s, WorkspaceIconPrefix):
		if !builtinIconSet[s[len(WorkspaceIconPrefix):]] {
			return "", invalid("unknown lucide icon; use lucide:<name> with one of the built-in icon names (e.g. lucide:server)")
		}
		return s, nil
	case strings.HasPrefix(s, "url:"):
		u, err := url.Parse(s[len("url:"):])
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return "", invalid("icon url must be http(s)")
		}
		return s, nil
	}
	return "", invalid("icon must be empty or start with favicon:, upload:, emoji:, lucide: or url:")
}

func cleanWallpaper(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultWallpaper, nil
	}
	switch {
	case strings.HasPrefix(s, "preset:"):
		id := s[len("preset:"):]
		for _, p := range WallpaperPresets {
			if p == id {
				return s, nil
			}
		}
		return "", invalid("unknown wallpaper preset %q", id)
	case strings.HasPrefix(s, "upload:"):
		if media.SafeName(s[len("upload:"):]) {
			return s, nil
		}
		return "", invalid("invalid wallpaper file name")
	case strings.HasPrefix(s, "plugin:"):
		if _, _, ok := parsePluginWallpaper(s); ok {
			return s, nil
		}
		return "", invalid("plugin wallpaper must be plugin:<pluginId>/<sourceId>")
	}
	return "", invalid("wallpaper must be preset:<id>, upload:<file> or plugin:<pluginId>/<sourceId>")
}

// ApplySettingsPatch validates and applies p to s.
func applySettingsPatch(s Settings, p SettingsPatch) (Settings, error) {
	if p.Title != nil {
		t, err := cleanName("title", *p.Title, maxSiteTitle)
		if err != nil {
			return s, err
		}
		s.Title = t
	}
	if p.SortMode != nil {
		switch *p.SortMode {
		case "frequency", "recent", "custom":
			s.SortMode = *p.SortMode
		default:
			return s, invalid("sort_mode must be frequency, recent or custom")
		}
	}
	if p.GuestAccess != nil {
		s.GuestAccess = *p.GuestAccess
	}
	if p.SearchEngine != nil {
		e := strings.TrimSpace(*p.SearchEngine)
		u, err := url.Parse(strings.ReplaceAll(e, "%s", "x"))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || !strings.Contains(e, "%s") || len(e) > maxURL {
			return s, invalid("search_engine must be an http(s) URL containing %%s")
		}
		s.SearchEngine = e
	}
	if p.OpenInNewTab != nil {
		s.OpenInNewTab = *p.OpenInNewTab
	}
	if p.HealthInterval != nil {
		v := *p.HealthInterval
		if v < 15 {
			v = 15
		}
		if v > 86400 {
			v = 86400
		}
		s.HealthInterval = v
	}
	if p.ShowClock != nil {
		s.ShowClock = *p.ShowClock
	}
	return s, nil
}

func normalizeSettings(s Settings) Settings {
	out, err := applySettingsPatch(DefaultSettings(), SettingsPatch{
		Title: &s.Title, SortMode: &s.SortMode, GuestAccess: &s.GuestAccess,
		SearchEngine: &s.SearchEngine, OpenInNewTab: &s.OpenInNewTab,
		HealthInterval: &s.HealthInterval, ShowClock: &s.ShowClock,
	})
	if err != nil {
		// fall back field by field: keep what is valid
		d := DefaultSettings()
		d.GuestAccess, d.OpenInNewTab, d.ShowClock = s.GuestAccess, s.OpenInNewTab, s.ShowClock
		if s.HealthInterval >= 15 {
			d.HealthInterval = s.HealthInterval
		}
		return d
	}
	return out
}
