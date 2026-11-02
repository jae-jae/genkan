package store

import (
	"strings"
)

// Import loads exported data. mode "replace" swaps all user data (auth and
// tokens are kept); "merge" appends everything under fresh IDs.
func (s *Store) Import(mode string, in Export) (workspaces, links int, err error) {
	if mode != "replace" && mode != "merge" {
		return 0, 0, invalid(`mode must be "replace" or "merge"`)
	}
	if in.Schema > SchemaVersion {
		return 0, 0, invalid("export schema %d is newer than supported %d", in.Schema, SchemaVersion)
	}
	merge := mode == "merge"
	if !merge && len(in.Workspaces) == 0 {
		return 0, 0, invalid("import has no workspaces")
	}
	t := now()

	// Validate & clean everything outside the lock.
	wsMap := map[string]string{} // old id -> new id
	var wss []Workspace
	for _, w := range in.Workspaces {
		name, err := cleanName("workspace name", w.Name, maxWorkspaceName)
		if err != nil {
			return 0, 0, err
		}
		icon := w.Icon
		if ic := strings.TrimSpace(icon); ic != "" && !strings.HasPrefix(ic, WorkspaceIconPrefix) {
			icon = migrateWorkspaceIcon(ic) // exports from before lucide icons carry emoji
		}
		icon, err = cleanWorkspaceIcon(icon)
		if err != nil {
			return 0, 0, err
		}
		wp, err := cleanWallpaper(w.Wallpaper)
		if err != nil {
			return 0, 0, err
		}
		old := w.ID
		if _, dup := wsMap[old]; dup {
			return 0, 0, invalid("duplicate workspace id %q", old)
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
			return 0, 0, err
		}
		if _, dup := catMap[c.ID]; dup {
			return 0, 0, invalid("duplicate category id %q", c.ID)
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
			return 0, 0, err
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
			return 0, 0, invalid("duplicate link id %s", id)
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

	err = s.commit(true, func() (bool, error) {
		if merge {
			base := s.nextWorkspaceSort()
			for i := range wss {
				wss[i].Sort = base + i
			}
			s.d.Workspaces = append(s.d.Workspaces, wss...)
			s.d.Categories = append(s.d.Categories, cats...)
			s.d.Links = append(s.d.Links, lks...)
		} else {
			s.d.Workspaces, s.d.Categories, s.d.Links = wss, cats, lks
			s.d.Settings = normalizeSettings(in.Settings)
		}
		return true, nil
	})
	if err != nil {
		return 0, 0, err
	}
	return len(wss), len(lks), nil
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
