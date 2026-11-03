package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jae-jae/genkan/internal/auth"
)

const (
	fileName      = "genkan.json"
	flushDebounce = 2 * time.Second
)

type authData struct {
	PasswordHash  string `json:"password_hash"`
	SessionSecret string `json:"session_secret"`
	APIToken      string `json:"api_token"` // plaintext on purpose: admin-only, shown in the UI and embedded in the Skill download
}

type fileData struct {
	Schema     int              `json:"schema"`
	Auth       authData         `json:"auth"`
	Settings   Settings         `json:"settings"`
	Workspaces []Workspace      `json:"workspaces"`
	Categories []Category       `json:"categories"`
	Links      []Link           `json:"links"`
	Plugins    []Plugin         `json:"plugins,omitempty"`
	Widgets    []WidgetInstance `json:"widgets,omitempty"`
}

// Store is the in-memory database. All exported methods are safe for concurrent use.
type Store struct {
	dir  string
	path string

	mu      sync.RWMutex
	d       fileData
	version int64
	dirty   bool // unflushed debounced changes (clicks)
	timer   *time.Timer
	closed  bool
	seq     uint64
	notify  func(version int64)
	secrets SecretResolver
	revSeq  uint64
	gone    func([]WidgetRef) // called after widget instances were removed

	fileMu  sync.Mutex
	written uint64
}

// Open loads (or initializes) the store in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, path: filepath.Join(dir, fileName), version: 1}
	s.d.Settings = DefaultSettings()
	rewrite := false
	raw, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &s.d); err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.path, err)
		}
		// Pre-single-token files carry a hashed "tokens" array; drop it (rewrite below).
		var legacy struct {
			Tokens json.RawMessage `json:"tokens"`
		}
		if json.Unmarshal(raw, &legacy) == nil && legacy.Tokens != nil {
			rewrite = true
		}
		// Links written before pin_sort existed got pin_sort = sort from Link.UnmarshalJSON; persist that.
		var legacyLinks struct {
			Links []struct {
				PinSort *int `json:"pin_sort"`
			} `json:"links"`
		}
		if json.Unmarshal(raw, &legacyLinks) == nil {
			for _, l := range legacyLinks.Links {
				if l.PinSort == nil {
					rewrite = true
					break
				}
			}
		}
		if s.d.Schema > SchemaVersion {
			return nil, fmt.Errorf("%s has schema %d, newer than supported %d", s.path, s.d.Schema, SchemaVersion)
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return nil, err
	}
	changed := s.normalize() || rewrite
	if changed || errors.Is(err, os.ErrNotExist) {
		s.mu.Lock()
		data, seq := s.marshalLocked()
		s.mu.Unlock()
		if err := s.write(data, seq); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// normalize fills in required defaults; reports whether anything changed.
func (s *Store) normalize() bool {
	changed := false
	d := &s.d
	// Schema 2 added plugins and widgets; v1 files need no migration.
	for i := range d.Plugins {
		p := &d.Plugins[i]
		s.revSeq++
		p.Rev = s.revSeq
		if p.Lang == "" {
			p.Lang = "js"
		}
		if p.Config == nil {
			p.Config = map[string]any{}
		}
	}
	for i := range d.Widgets {
		if d.Widgets[i].Config == nil {
			d.Widgets[i].Config = map[string]any{}
		}
	}
	if d.Schema != SchemaVersion {
		d.Schema = SchemaVersion
		changed = true
	}
	if d.Auth.SessionSecret == "" {
		d.Auth.SessionSecret = randomHex(32)
		changed = true
	}
	if d.Auth.APIToken == "" {
		d.Auth.APIToken = auth.NewToken()
		changed = true
	}
	if ns := normalizeSettings(d.Settings); ns != d.Settings {
		d.Settings = ns
		changed = true
	}
	if len(d.Workspaces) == 0 {
		d.Workspaces = []Workspace{{ID: NewID(), Name: "Home", Icon: firstWorkspaceIcon, Wallpaper: DefaultWallpaper, Sort: 0}}
		changed = true
	}
	for i := range d.Workspaces {
		if d.Workspaces[i].Wallpaper == "" {
			d.Workspaces[i].Wallpaper = DefaultWallpaper
			changed = true
		}
		if ic := migrateWorkspaceIcon(d.Workspaces[i].Icon); ic != d.Workspaces[i].Icon {
			d.Workspaces[i].Icon = ic // emoji -> lucide migration, persisted by Open
			changed = true
		}
	}
	return changed
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// OnChange registers a callback invoked (outside the lock) after every
// data mutation with the new version. Call before serving traffic.
func (s *Store) OnChange(fn func(version int64)) {
	s.mu.Lock()
	s.notify = fn
	s.mu.Unlock()
}

func (s *Store) Version() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

func (s *Store) Dir() string { return s.dir }

// Persistence -----------------------------------------------------------------

// marshalLocked serializes state; the caller holds mu (read or write).
func (s *Store) marshalLocked() ([]byte, uint64) {
	d := s.d
	if d.Categories == nil {
		d.Categories = []Category{}
	}
	if d.Links == nil {
		d.Links = []Link{}
	}
	data, err := json.MarshalIndent(&d, "", "  ")
	if err != nil {
		panic(err) // plain structs; cannot happen
	}
	s.seq++
	return data, s.seq
}

// write atomically replaces the data file: tmp + fsync + rename.
func (s *Store) write(data []byte, seq uint64) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if seq <= s.written {
		return nil // a newer snapshot was already written
	}
	tmp, err := os.CreateTemp(s.dir, ".genkan-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return err
	}
	if d, err := os.Open(s.dir); err == nil { // make the rename durable (best effort)
		_ = d.Sync()
		d.Close()
	}
	s.written = seq
	return nil
}

// commit runs fn under the write lock. If fn reports a change, the version
// is bumped, the file persisted and (when bump) listeners notified.
func (s *Store) commit(bump bool, fn func() (changed bool, err error)) error {
	s.mu.Lock()
	changed, err := fn()
	if err != nil || !changed {
		s.mu.Unlock()
		return err
	}
	if bump {
		s.version++
	}
	v, notify := s.version, s.notify
	s.dirty = false
	data, seq := s.marshalLocked()
	s.mu.Unlock()
	werr := s.write(data, seq)
	if werr != nil {
		log.Printf("store: persist failed: %v", werr)
	}
	if bump && notify != nil {
		notify(v)
	}
	if werr != nil {
		return fmt.Errorf("persist: %w", werr)
	}
	return nil
}

// markDirtyLocked schedules a debounced flush; caller holds the write lock.
func (s *Store) markDirtyLocked() {
	s.dirty = true
	if s.timer == nil && !s.closed {
		s.timer = time.AfterFunc(flushDebounce, func() {
			if err := s.Flush(); err != nil {
				log.Printf("store: flush failed: %v", err)
			}
		})
	}
}

// Flush persists pending debounced changes immediately.
func (s *Store) Flush() error {
	s.mu.Lock()
	s.timer = nil
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	s.dirty = false
	data, seq := s.marshalLocked()
	s.mu.Unlock()
	return s.write(data, seq)
}

// Close flushes pending changes and stops the debounce timer.
func (s *Store) Close() error {
	s.mu.Lock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
	return s.Flush()
}

// Reads -------------------------------------------------------------------------

type Snapshot struct {
	Settings   Settings
	Workspaces []Workspace
	Categories []Category
	Links      []Link
	Widgets    []WidgetInstance
	Version    int64
}

// Snapshot returns sorted deep copies of all user data.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{
		Settings:   s.d.Settings,
		Workspaces: append([]Workspace{}, s.d.Workspaces...),
		Categories: append([]Category{}, s.d.Categories...),
		Links:      make([]Link, len(s.d.Links)),
		Version:    s.version,
	}
	snap.Widgets = s.widgetsLocked()
	copy(snap.Links, s.d.Links)
	for i := range snap.Links { // detach *string pointers
		snap.Links[i].LastUsedAt = cloneStr(snap.Links[i].LastUsedAt)
	}
	sort.SliceStable(snap.Workspaces, func(i, j int) bool { return snap.Workspaces[i].Sort < snap.Workspaces[j].Sort })
	sort.SliceStable(snap.Categories, func(i, j int) bool { return snap.Categories[i].Sort < snap.Categories[j].Sort })
	sort.SliceStable(snap.Links, func(i, j int) bool { return snap.Links[i].Sort < snap.Links[j].Sort })
	return snap
}

func cloneStr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.d.Settings
}

func (s *Store) GetLink(id string) (Link, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, l := range s.d.Links {
		if l.ID == id {
			l.LastUsedAt = cloneStr(l.LastUsedAt)
			return l, true
		}
	}
	return Link{}, false
}

// HealthTarget is a link eligible for health checking.
type HealthTarget struct{ ID, URL string }

// HealthConfig returns the links with health_check enabled and the interval (seconds).
func (s *Store) HealthConfig() ([]HealthTarget, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []HealthTarget
	for _, l := range s.d.Links {
		if l.HealthCheck {
			out = append(out, HealthTarget{l.ID, l.URL})
		}
	}
	return out, s.d.Settings.HealthInterval
}

// Export returns the portable data set (no auth, no API token).
func (s *Store) Export() Export {
	snap := s.Snapshot()
	exp := Export{Schema: SchemaVersion, Settings: snap.Settings, Workspaces: snap.Workspaces, Categories: snap.Categories, Links: snap.Links}
	s.mu.RLock()
	exp.Plugins, exp.Widgets = s.exportPluginsLocked()
	s.mu.RUnlock()
	return exp
}
