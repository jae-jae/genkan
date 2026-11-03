package store

import (
	"fmt"
	"os"
	"strings"
)

// ImportResult counts what an import created.
type ImportResult struct {
	Workspaces, Links, Plugins, Widgets int
	// PluginIDs are the plugins this import wrote (merge: only new ones).
	PluginIDs []string
	// WidgetMap maps the widget ids of the import file to the stored widgets
	// (merge assigns fresh ids); only widgets that were kept are listed.
	WidgetMap map[string]ImportedWidget
}

// ImportedWidget is a stored widget of an import.
type ImportedWidget struct{ ID, Plugin string }

// Import loads exported data. mode "replace" swaps all user data (auth and
// tokens are kept); "merge" appends everything under fresh IDs.
func (s *Store) Import(mode string, in Export) (workspaces, links int, err error) {
	r, err := s.ImportData(mode, in)
	return r.Workspaces, r.Links, err
}

// ImportData is Import including plugins and widgets. Replace swaps the
// plugin set; merge adds plugins whose id is not installed yet (existing
// plugins are left alone) and widgets under fresh ids.
func (s *Store) ImportData(mode string, in Export) (res ImportResult, err error) {
	if mode != "replace" && mode != "merge" {
		return res, invalid(`mode must be "replace" or "merge"`)
	}
	if in.Schema > SchemaVersion {
		return res, invalid("export schema %d is newer than supported %d", in.Schema, SchemaVersion)
	}
	merge := mode == "merge"
	if !merge && len(in.Workspaces) == 0 {
		return res, invalid("import has no workspaces")
	}
	plugins, err := cleanImportPlugins(in.Plugins)
	if err != nil {
		return res, err
	}
	t := now()

	// Validate & clean everything outside the lock.
	wsMap := map[string]string{} // old id -> new id
	var wss []Workspace
	for _, w := range in.Workspaces {
		name, err := cleanName("workspace name", w.Name, maxWorkspaceName)
		if err != nil {
			return res, err
		}
		icon := w.Icon
		if ic := strings.TrimSpace(icon); ic != "" && !strings.HasPrefix(ic, WorkspaceIconPrefix) {
			icon = migrateWorkspaceIcon(ic) // exports from before lucide icons carry emoji
		}
		icon, err = cleanWorkspaceIcon(icon)
		if err != nil {
			return res, err
		}
		wp, err := cleanWallpaper(w.Wallpaper)
		if err != nil {
			return res, err
		}
		old := w.ID
		if _, dup := wsMap[old]; dup {
			return res, invalid("duplicate workspace id %q", old)
		}
		id := old
		if merge || !validID(id) {
			id = NewID()
		}
		wsMap[old] = id
		wss = append(wss, Workspace{ID: id, Name: name, Icon: icon, Wallpaper: wp, Sort: w.Sort})
	}
	catMap := map[string]string{}
	catWS := map[string]string{} // new cat id -> new ws id
	var cats []Category
	for _, c := range in.Categories {
		nws, ok := wsMap[c.WorkspaceID]
		if !ok {
			continue // orphan
		}
		name, err := cleanName("category name", c.Name, maxCategoryName)
		if err != nil {
			return res, err
		}
		if _, dup := catMap[c.ID]; dup {
			return res, invalid("duplicate category id %q", c.ID)
		}
		id := c.ID
		if merge || !validID(id) {
			id = NewID()
		}
		catMap[c.ID] = id
		catWS[id] = nws
		cats = append(cats, Category{ID: id, WorkspaceID: nws, Name: name, Sort: c.Sort})
	}
	var lks []Link
	seenLink := map[string]bool{}
	for _, l := range in.Links {
		nws, ok := wsMap[l.WorkspaceID]
		if !ok {
			continue
		}
		c, err := cleanLinkFields(l.Title, l.URL, l.Description, l.Icon)
		if err != nil {
			return res, err
		}
		ncat := ""
		if l.CategoryID != "" {
			if id, ok := catMap[l.CategoryID]; ok && catWS[id] == nws {
				ncat = id
			}
		}
		id := l.ID
		if merge || !validID(id) {
			id = NewID()
		}
		if seenLink[id] {
			return res, invalid("duplicate link id %s", id)
		}
		seenLink[id] = true
		if l.Clicks < 0 {
			l.Clicks = 0
		}
		created, updated := l.CreatedAt, l.UpdatedAt
		if created == "" {
			created = t
		}
		if updated == "" {
			updated = created
		}
		lks = append(lks, Link{
			ID: id, WorkspaceID: nws, CategoryID: ncat, Title: c.title, URL: c.url,
			Description: c.desc, Icon: c.icon, Pinned: l.Pinned, Sort: l.Sort, PinSort: l.PinSort,
			Clicks: l.Clicks, LastUsedAt: normTime(l.LastUsedAt), HealthCheck: l.HealthCheck,
			CreatedAt: created, UpdatedAt: updated,
		})
	}

	// Widgets: keep those whose plugin will exist and whose workspace was imported.
	var wgs []WidgetInstance
	wmap := map[string]ImportedWidget{}
	for _, w := range in.Widgets {
		nws := ""
		if w.WorkspaceID != "" {
			var ok bool
			if nws, ok = wsMap[w.WorkspaceID]; !ok {
				continue
			}
		}
		if !ValidPluginID(w.Plugin) || !ValidSourceID(w.Widget) {
			continue
		}
		cfg, err := cleanStoredConfig(w.Config)
		if err != nil {
			return res, err
		}
		id := w.ID
		if merge || !validID(id) {
			id = NewID()
		}
		wmap[w.ID] = ImportedWidget{ID: id, Plugin: w.Plugin}
		wgs = append(wgs, WidgetInstance{ID: id, Plugin: w.Plugin, Widget: w.Widget, WorkspaceID: nws, Config: cfg, Sort: w.Sort, W: ClampWidgetW(w.W), H: ClampWidgetH(w.H)})
	}

	var gone []WidgetRef
	err = s.commit(true, func() (bool, error) {
		// plugin sources first: a write error leaves the data untouched
		var addPlugins []Plugin
		for _, p := range plugins {
			if merge && s.plIndex(p.ID) >= 0 {
				continue
			}
			if err := AtomicWriteFile(s.sourcePath(p.ID, p.Lang), []byte(p.Source), 0o600); err != nil {
				return false, fmt.Errorf("write plugin source: %w", err)
			}
			if old := s.plIndex(p.ID); old >= 0 && s.d.Plugins[old].Lang != p.Lang {
				_ = os.Remove(s.sourcePath(p.ID, s.d.Plugins[old].Lang))
			}
			np := p.Plugin
			if np.InstalledAt == "" {
				np.InstalledAt = t
			}
			if np.UpdatedAt == "" {
				np.UpdatedAt = t
			}
			s.revSeq++
			np.Rev = s.revSeq
			addPlugins = append(addPlugins, np)
		}
		if merge {
			if len(s.d.Plugins)+len(addPlugins) > maxPlugins {
				return false, conflict("too many plugins (max %d)", maxPlugins)
			}
			base := s.nextWorkspaceSort()
			for i := range wss {
				wss[i].Sort = base + i
			}
			s.d.Workspaces = append(s.d.Workspaces, wss...)
			s.d.Categories = append(s.d.Categories, cats...)
			s.d.Links = append(s.d.Links, lks...)
			s.d.Plugins = append(s.d.Plugins, addPlugins...)
			next := 0
			for _, x := range s.d.Widgets {
				if x.Sort >= next {
					next = x.Sort + 1
				}
			}
			for i := range wgs {
				wgs[i].Sort = next + i
			}
			s.d.Widgets = append(s.d.Widgets, wgs...)
		} else {
			keep := map[string]bool{}
			for _, p := range addPlugins {
				keep[p.ID] = true
			}
			for _, old := range s.d.Plugins {
				if !keep[old.ID] {
					s.removePluginFiles(old.ID)
				}
			}
			s.d.Workspaces, s.d.Categories, s.d.Links = wss, cats, lks
			s.d.Plugins = addPlugins
			for _, w := range s.d.Widgets {
				gone = append(gone, WidgetRef{ID: w.ID, Plugin: w.Plugin})
			}
			s.d.Widgets = nil
			s.d.Settings = normalizeSettings(in.Settings)
		}
		// widgets of plugins that do not exist are dropped; so are wallpapers using them
		kept := wgs[:0]
		for _, w := range wgs {
			if s.plIndex(w.Plugin) >= 0 {
				kept = append(kept, w)
			}
		}
		if !merge {
			s.d.Widgets = kept
		} else {
			// merge appended all of wgs; filter the appended tail
			base := len(s.d.Widgets) - len(wgs)
			tail := s.d.Widgets[:base]
			for _, w := range s.d.Widgets[base:] {
				if s.plIndex(w.Plugin) >= 0 {
					tail = append(tail, w)
				}
			}
			s.d.Widgets = tail
		}
		for i := range s.d.Workspaces {
			if p, _, ok := parsePluginWallpaper(s.d.Workspaces[i].Wallpaper); ok && s.plIndex(p) < 0 {
				s.d.Workspaces[i].Wallpaper = DefaultWallpaper
			}
		}
		res.Plugins = len(addPlugins)
		res.Widgets = len(kept)
		for _, p := range addPlugins {
			res.PluginIDs = append(res.PluginIDs, p.ID)
		}
		keptIDs := map[string]bool{}
		for _, w := range s.d.Widgets {
			keptIDs[w.ID] = true
		}
		for old, w := range wmap {
			if !keptIDs[w.ID] || s.plIndex(w.Plugin) < 0 {
				delete(wmap, old)
			}
		}
		res.WidgetMap = wmap
		return true, nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	// replace: old instances that are not in the import any more lose their storage
	if mode == "replace" {
		stay := map[string]bool{}
		for _, w := range res.WidgetMap {
			stay[w.ID] = true
		}
		var drop []WidgetRef
		for _, g := range gone {
			if !stay[g.ID] {
				drop = append(drop, g)
			}
		}
		s.widgetsGone(drop)
	}
	res.Workspaces, res.Links = len(wss), len(lks)
	return res, nil
}

func normTime(p *string) *string {
	if p == nil || strings.TrimSpace(*p) == "" {
		return nil
	}
	return p
}

func validID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
