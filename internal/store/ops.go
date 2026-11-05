package store

import (
	"crypto/subtle"
	"sort"

	"github.com/jae-jae/genkan/internal/auth"
)

// Locked helpers ---------------------------------------------------------------

func (s *Store) wsIndex(id string) int {
	for i := range s.d.Workspaces {
		if s.d.Workspaces[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) catIndex(id string) int {
	for i := range s.d.Categories {
		if s.d.Categories[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) linkIndex(id string) int {
	for i := range s.d.Links {
		if s.d.Links[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) nextWorkspaceSort() int {
	m := -1
	for _, w := range s.d.Workspaces {
		if w.Sort > m {
			m = w.Sort
		}
	}
	return m + 1
}

func (s *Store) nextCategorySort(ws string) int {
	m := -1
	for _, c := range s.d.Categories {
		if c.WorkspaceID == ws && c.Sort > m {
			m = c.Sort
		}
	}
	return m + 1
}

func (s *Store) nextLinkSort(ws, cat string) int {
	m := -1
	for _, l := range s.d.Links {
		if l.WorkspaceID == ws && l.CategoryID == cat && l.Sort > m {
			m = l.Sort
		}
	}
	return m + 1
}

// nextPinSort returns max(pin_sort of pinned links in ws, ignoring link except)+1 (0 when none).
func (s *Store) nextPinSort(ws, except string) int {
	m := -1
	for _, l := range s.d.Links {
		if l.ID != except && l.WorkspaceID == ws && l.Pinned && l.PinSort > m {
			m = l.PinSort
		}
	}
	return m + 1
}

// checkCategory verifies cat (may be empty) belongs to workspace ws.
func (s *Store) checkPlacement(ws, cat string) error {
	if s.wsIndex(ws) < 0 {
		return notFound("workspace", ws)
	}
	if cat != "" {
		ci := s.catIndex(cat)
		if ci < 0 {
			return notFound("category", cat)
		}
		if s.d.Categories[ci].WorkspaceID != ws {
			return invalid("category %s does not belong to workspace %s", cat, ws)
		}
	}
	return nil
}

// assignOrder gives listed items sort 0..n-1 and the rest follow in their previous order.
func assignOrder(listed, rest []*int) {
	sort.SliceStable(rest, func(i, j int) bool { return *rest[i] < *rest[j] })
	n := 0
	for _, p := range listed {
		*p = n
		n++
	}
	for _, p := range rest {
		*p = n
		n++
	}
}

func dedupe(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := ids[:0:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// Settings ----------------------------------------------------------------------

func (s *Store) UpdateSettings(p SettingsPatch) (Settings, error) {
	var out Settings
	err := s.commit(true, func() (bool, error) {
		ns, err := applySettingsPatch(s.d.Settings, p)
		if err != nil {
			return false, err
		}
		s.d.Settings = ns
		out = ns
		return true, nil
	})
	return out, err
}

// SetUpdateDismissed stores the release version the admin chose to skip
// ("" clears it). version must be a plain vX.Y.Z.
func (s *Store) SetUpdateDismissed(version string) (Settings, error) {
	if version != "" && cleanDismissed(version) == "" {
		return Settings{}, invalid("version must look like v1.2.3")
	}
	var out Settings
	err := s.commit(true, func() (bool, error) {
		out = s.d.Settings
		if out.UpdateDismissed == version {
			return false, nil
		}
		s.d.Settings.UpdateDismissed = version
		out = s.d.Settings
		return true, nil
	})
	return out, err
}

// Workspaces --------------------------------------------------------------------

func (s *Store) CreateWorkspace(in WorkspaceInput) (Workspace, error) {
	name, err := cleanName("name", in.Name, maxWorkspaceName)
	if err != nil {
		return Workspace{}, err
	}
	icon, err := cleanWorkspaceIcon(in.Icon)
	if err != nil {
		return Workspace{}, err
	}
	wp, err := cleanWallpaper(in.Wallpaper)
	if err != nil {
		return Workspace{}, err
	}
	var w Workspace
	err = s.commit(true, func() (bool, error) {
		if err := s.checkWallpaperPlugin(wp); err != nil {
			return false, err
		}
		w = Workspace{ID: NewID(), Name: name, Icon: icon, Wallpaper: wp, Sort: s.nextWorkspaceSort()}
		s.d.Workspaces = append(s.d.Workspaces, w)
		return true, nil
	})
	return w, err
}

func (s *Store) UpdateWorkspace(id string, p WorkspacePatch) (Workspace, error) {
	var out Workspace
	err := s.commit(true, func() (bool, error) {
		i := s.wsIndex(id)
		if i < 0 {
			return false, notFound("workspace", id)
		}
		w := s.d.Workspaces[i]
		var err error
		if p.Name != nil {
			if w.Name, err = cleanName("name", *p.Name, maxWorkspaceName); err != nil {
				return false, err
			}
		}
		if p.Icon != nil {
			if w.Icon, err = cleanWorkspaceIcon(*p.Icon); err != nil {
				return false, err
			}
		}
		if p.Wallpaper != nil {
			if w.Wallpaper, err = cleanWallpaper(*p.Wallpaper); err != nil {
				return false, err
			}
			if err := s.checkWallpaperPlugin(w.Wallpaper); err != nil {
				return false, err
			}
		}
		s.d.Workspaces[i] = w
		out = w
		return true, nil
	})
	return out, err
}

func (s *Store) DeleteWorkspace(id string) error {
	var gone []WidgetRef
	err := s.commit(true, func() (bool, error) {
		i := s.wsIndex(id)
		if i < 0 {
			return false, notFound("workspace", id)
		}
		if len(s.d.Workspaces) <= 1 {
			return false, conflict("cannot delete the last workspace")
		}
		s.d.Workspaces = append(s.d.Workspaces[:i], s.d.Workspaces[i+1:]...)
		cats := s.d.Categories[:0]
		for _, c := range s.d.Categories {
			if c.WorkspaceID != id {
				cats = append(cats, c)
			}
		}
		s.d.Categories = cats
		links := s.d.Links[:0]
		for _, l := range s.d.Links {
			if l.WorkspaceID != id {
				links = append(links, l)
			}
		}
		s.d.Links = links
		ws := s.d.Widgets[:0]
		for _, w := range s.d.Widgets {
			if w.WorkspaceID != id {
				ws = append(ws, w)
			} else {
				gone = append(gone, WidgetRef{ID: w.ID, Plugin: w.Plugin})
			}
		}
		s.d.Widgets = ws
		return true, nil
	})
	if err == nil {
		s.widgetsGone(gone)
	}
	return err
}

// checkWallpaperPlugin rejects plugin:<id>/<source> wallpapers of plugins that are not installed.
func (s *Store) checkWallpaperPlugin(wp string) error {
	if p, _, ok := parsePluginWallpaper(wp); ok && s.plIndex(p) < 0 {
		return invalid("wallpaper plugin %q is not installed", p)
	}
	return nil
}

func (s *Store) ReorderWorkspaces(ids []string) error {
	return s.commit(true, func() (bool, error) {
		var listed, rest []*int
		in := map[string]bool{}
		for _, id := range dedupe(ids) {
			i := s.wsIndex(id)
			if i < 0 {
				return false, notFound("workspace", id)
			}
			in[id] = true
			listed = append(listed, &s.d.Workspaces[i].Sort)
		}
		for i := range s.d.Workspaces {
			if !in[s.d.Workspaces[i].ID] {
				rest = append(rest, &s.d.Workspaces[i].Sort)
			}
		}
		assignOrder(listed, rest)
		return true, nil
	})
}

// Categories --------------------------------------------------------------------

func (s *Store) CreateCategory(workspaceID, name string) (Category, error) {
	name, err := cleanName("name", name, maxCategoryName)
	if err != nil {
		return Category{}, err
	}
	var c Category
	err = s.commit(true, func() (bool, error) {
		if s.wsIndex(workspaceID) < 0 {
			return false, notFound("workspace", workspaceID)
		}
		c = Category{ID: NewID(), WorkspaceID: workspaceID, Name: name, Sort: s.nextCategorySort(workspaceID)}
		s.d.Categories = append(s.d.Categories, c)
		return true, nil
	})
	return c, err
}

func (s *Store) UpdateCategory(id string, name *string) (Category, error) {
	var out Category
	err := s.commit(true, func() (bool, error) {
		i := s.catIndex(id)
		if i < 0 {
			return false, notFound("category", id)
		}
		c := s.d.Categories[i]
		if name != nil {
			n, err := cleanName("name", *name, maxCategoryName)
			if err != nil {
				return false, err
			}
			c.Name = n
		}
		s.d.Categories[i] = c
		out = c
		return true, nil
	})
	return out, err
}

// DeleteCategory removes a category. Its links become uncategorized unless deleteLinks.
func (s *Store) DeleteCategory(id string, deleteLinks bool) error {
	return s.commit(true, func() (bool, error) {
		i := s.catIndex(id)
		if i < 0 {
			return false, notFound("category", id)
		}
		ws := s.d.Categories[i].WorkspaceID
		s.d.Categories = append(s.d.Categories[:i], s.d.Categories[i+1:]...)
		if deleteLinks {
			links := s.d.Links[:0]
			for _, l := range s.d.Links {
				if l.CategoryID != id {
					links = append(links, l)
				}
			}
			s.d.Links = links
			return true, nil
		}
		// move to uncategorized, appended after existing uncategorized links
		var moved []*Link
		for k := range s.d.Links {
			if s.d.Links[k].CategoryID == id {
				moved = append(moved, &s.d.Links[k])
			}
		}
		sort.SliceStable(moved, func(a, b int) bool { return moved[a].Sort < moved[b].Sort })
		next := s.nextLinkSort(ws, "")
		t := now()
		for _, l := range moved {
			l.CategoryID, l.Sort, l.UpdatedAt = "", next, t
			next++
		}
		return true, nil
	})
}

func (s *Store) ReorderCategories(workspaceID string, ids []string) error {
	return s.commit(true, func() (bool, error) {
		if s.wsIndex(workspaceID) < 0 {
			return false, notFound("workspace", workspaceID)
		}
		var listed, rest []*int
		in := map[string]bool{}
		for _, id := range dedupe(ids) {
			i := s.catIndex(id)
			if i < 0 {
				return false, notFound("category", id)
			}
			if s.d.Categories[i].WorkspaceID != workspaceID {
				return false, invalid("category %s does not belong to workspace %s", id, workspaceID)
			}
			in[id] = true
			listed = append(listed, &s.d.Categories[i].Sort)
		}
		for i := range s.d.Categories {
			c := &s.d.Categories[i]
			if c.WorkspaceID == workspaceID && !in[c.ID] {
				rest = append(rest, &c.Sort)
			}
		}
		assignOrder(listed, rest)
		return true, nil
	})
}

// Links -------------------------------------------------------------------------

type cleanedLink struct {
	title, url, desc, icon string
}

func cleanLinkFields(title, rawURL, desc, icon string) (cleanedLink, error) {
	var c cleanedLink
	var err error
	if c.title, err = cleanName("title", title, maxTitle); err != nil {
		return c, err
	}
	if c.url, err = CleanURL(rawURL); err != nil {
		return c, err
	}
	if c.desc, err = cleanText("description", desc, maxDescription); err != nil {
		return c, err
	}
	if c.icon, err = cleanIcon(icon); err != nil {
		return c, err
	}
	return c, nil
}

// CreateLinks creates links in one transaction (single version bump). Each
// item succeeds or fails independently; results are in input order.
func (s *Store) CreateLinks(in []LinkInput) ([]LinkResult, error) {
	res := make([]LinkResult, len(in))
	err := s.commit(true, func() (bool, error) {
		any := false
		t := now()
		for i, li := range in {
			c, err := cleanLinkFields(li.Title, li.URL, li.Description, li.Icon)
			if err != nil {
				res[i].Err = err
				continue
			}
			if err := s.checkPlacement(li.WorkspaceID, li.CategoryID); err != nil {
				res[i].Err = err
				continue
			}
			l := Link{
				ID: NewID(), WorkspaceID: li.WorkspaceID, CategoryID: li.CategoryID,
				Title: c.title, URL: c.url, Description: c.desc, Icon: c.icon,
				Pinned: li.Pinned, Sort: s.nextLinkSort(li.WorkspaceID, li.CategoryID),
				HealthCheck: li.HealthCheck, CreatedAt: t, UpdatedAt: t,
			}
			if l.Pinned {
				l.PinSort = s.nextPinSort(li.WorkspaceID, "")
			}
			s.d.Links = append(s.d.Links, l)
			res[i].Link = l
			any = true
		}
		return any, nil
	})
	return res, err
}

func (s *Store) CreateLink(in LinkInput) (Link, error) {
	res, err := s.CreateLinks([]LinkInput{in})
	if res[0].Err != nil {
		return Link{}, res[0].Err
	}
	return res[0].Link, err
}

func (s *Store) UpdateLink(id string, p LinkPatch) (Link, error) {
	var out Link
	err := s.commit(true, func() (bool, error) {
		i := s.linkIndex(id)
		if i < 0 {
			return false, notFound("link", id)
		}
		l := s.d.Links[i]
		var err error
		if p.Title != nil {
			if l.Title, err = cleanName("title", *p.Title, maxTitle); err != nil {
				return false, err
			}
		}
		if p.URL != nil {
			if l.URL, err = CleanURL(*p.URL); err != nil {
				return false, err
			}
		}
		if p.Description != nil {
			if l.Description, err = cleanText("description", *p.Description, maxDescription); err != nil {
				return false, err
			}
		}
		if p.Icon != nil {
			if l.Icon, err = cleanIcon(*p.Icon); err != nil {
				return false, err
			}
		}
		wasPinned := l.Pinned
		if p.Pinned != nil {
			l.Pinned = *p.Pinned
		}
		if p.HealthCheck != nil {
			l.HealthCheck = *p.HealthCheck
		}
		ws, cat := l.WorkspaceID, l.CategoryID
		if p.WorkspaceID != nil {
			ws = *p.WorkspaceID
		}
		if p.CategoryID != nil {
			cat = *p.CategoryID
		} else if ws != l.WorkspaceID {
			cat = ""
		}
		if ws != l.WorkspaceID || cat != l.CategoryID {
			if err := s.checkPlacement(ws, cat); err != nil {
				return false, err
			}
			if ws != l.WorkspaceID {
				wasPinned = false // pin_sort belongs to the old workspace's row: re-append in the new one
			}
			l.WorkspaceID, l.CategoryID = ws, cat
			l.Sort = s.nextLinkSort(ws, cat)
		}
		if l.Pinned && !wasPinned {
			l.PinSort = s.nextPinSort(l.WorkspaceID, l.ID) // newly pinned: end of the pinned row
		}
		l.UpdatedAt = now()
		s.d.Links[i] = l
		out = l
		out.LastUsedAt = cloneStr(l.LastUsedAt)
		return true, nil
	})
	return out, err
}

// DeleteLinks removes the given links; unknown ids are returned in missing.
func (s *Store) DeleteLinks(ids []string) (deleted int, missing []string, err error) {
	err = s.commit(true, func() (bool, error) {
		del := map[string]bool{}
		for _, id := range dedupe(ids) {
			if s.linkIndex(id) < 0 {
				missing = append(missing, id)
			} else {
				del[id] = true
			}
		}
		if len(del) == 0 {
			return false, nil
		}
		links := s.d.Links[:0]
		for _, l := range s.d.Links {
			if !del[l.ID] {
				links = append(links, l)
			}
		}
		s.d.Links = links
		deleted = len(del)
		return true, nil
	})
	return
}

// PinnedGroup is the pseudo category id used to reorder the pinned row.
const PinnedGroup = "__pinned"

// ReorderLinks rewrites the order of ids. For a real category (or "" for
// uncategorized) it rewrites sort and moves the links into that
// workspace/category; pinned links are ordinary members of their category.
// For PinnedGroup only pin_sort changes (listed links become pinned); sort and
// category_id are never touched.
func (s *Store) ReorderLinks(workspaceID, categoryID string, ids []string) error {
	return s.commit(true, func() (bool, error) {
		pinned := categoryID == PinnedGroup
		if pinned {
			if s.wsIndex(workspaceID) < 0 {
				return false, notFound("workspace", workspaceID)
			}
		} else if err := s.checkPlacement(workspaceID, categoryID); err != nil {
			return false, err
		}
		ids = dedupe(ids)
		idx := make([]int, len(ids))
		in := map[string]bool{}
		for k, id := range ids {
			i := s.linkIndex(id)
			if i < 0 {
				return false, notFound("link", id)
			}
			if pinned && s.d.Links[i].WorkspaceID != workspaceID {
				return false, invalid("link %s is not in workspace %s", id, workspaceID)
			}
			idx[k] = i
			in[id] = true
		}
		t := now()
		var listed, rest []*int
		for _, i := range idx {
			l := &s.d.Links[i]
			if pinned {
				if !l.Pinned {
					l.Pinned = true
					l.UpdatedAt = t
				}
				listed = append(listed, &l.PinSort)
				continue
			}
			if l.WorkspaceID != workspaceID || l.CategoryID != categoryID {
				l.WorkspaceID, l.CategoryID = workspaceID, categoryID
				l.UpdatedAt = t
			}
			listed = append(listed, &l.Sort)
		}
		for i := range s.d.Links {
			l := &s.d.Links[i]
			if in[l.ID] || l.WorkspaceID != workspaceID {
				continue
			}
			if pinned {
				if l.Pinned {
					rest = append(rest, &l.PinSort)
				}
			} else if l.CategoryID == categoryID {
				rest = append(rest, &l.Sort)
			}
		}
		assignOrder(listed, rest)
		return true, nil
	})
}

// Click records a link visit (debounced persistence, no version bump).
func (s *Store) Click(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.linkIndex(id)
	if i < 0 {
		return notFound("link", id)
	}
	l := &s.d.Links[i]
	l.Clicks++
	t := now()
	l.LastUsedAt = &t
	s.markDirtyLocked()
	return nil
}

// SetIcon updates a link's icon (used by the favicon fetcher). With
// onlyIfEmpty the icon is left alone if the user set one meanwhile.
func (s *Store) SetIcon(id, icon string, onlyIfEmpty bool) (Link, bool) {
	var out Link
	ok := false
	_ = s.commit(true, func() (bool, error) {
		i := s.linkIndex(id)
		if i < 0 || (onlyIfEmpty && s.d.Links[i].Icon != "") || s.d.Links[i].Icon == icon {
			return false, nil
		}
		s.d.Links[i].Icon = icon
		s.d.Links[i].UpdatedAt = now()
		out = s.d.Links[i]
		out.LastUsedAt = cloneStr(out.LastUsedAt)
		ok = true
		return true, nil
	})
	return out, ok
}

// Auth & API token -------------------------------------------------------------------

// AuthState returns the password hash and session secret.
func (s *Store) AuthState() (hash, secret string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.d.Auth.PasswordHash, s.d.Auth.SessionSecret
}

func (s *Store) SetupRequired() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.d.Auth.PasswordHash == ""
}

// SetInitialPassword stores hash only if no password exists yet.
func (s *Store) SetInitialPassword(hash string) (bool, error) {
	set := false
	err := s.commit(false, func() (bool, error) {
		if s.d.Auth.PasswordHash != "" {
			return false, nil
		}
		s.d.Auth.PasswordHash = hash
		set = true
		return true, nil
	})
	return set, err
}

// ChangePassword stores a new hash and rotates the session secret (returned).
func (s *Store) ChangePassword(hash string) (string, error) {
	var secret string
	err := s.commit(false, func() (bool, error) {
		s.d.Auth.PasswordHash = hash
		s.d.Auth.SessionSecret = randomHex(32)
		secret = s.d.Auth.SessionSecret
		return true, nil
	})
	return secret, err
}

// APIToken returns the single admin API token (plaintext; admin-only surface).
func (s *Store) APIToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.d.Auth.APIToken
}

// CheckToken reports whether candidate equals the API token (constant time).
func (s *Store) CheckToken(candidate string) bool {
	s.mu.RLock()
	tok := s.d.Auth.APIToken
	s.mu.RUnlock()
	if tok == "" || candidate == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(candidate)) == 1
}

// RegenerateToken replaces the API token with a fresh random one; the old one
// stops working immediately. Returns the new token.
func (s *Store) RegenerateToken() (string, error) {
	var tok string
	err := s.commit(false, func() (bool, error) {
		tok = auth.NewToken()
		s.d.Auth.APIToken = tok
		return true, nil
	})
	return tok, err
}

// ResetWallpaperUsers sets every workspace whose wallpaper is "upload:<file>"
// back to the default preset (used after the file is deleted). Returns how
// many workspaces changed. The version is always bumped so other clients
// refresh their gallery view.
func (s *Store) ResetWallpaperUsers(file string) (int, error) {
	n := 0
	err := s.commit(true, func() (bool, error) {
		for i := range s.d.Workspaces {
			if s.d.Workspaces[i].Wallpaper == "upload:"+file {
				s.d.Workspaces[i].Wallpaper = DefaultWallpaper
				n++
			}
		}
		return true, nil
	})
	return n, err
}
