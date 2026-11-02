package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Workspace icons ---------------------------------------------------------------

func TestBuiltinIconsList(t *testing.T) {
	if len(BuiltinIcons) != 52 {
		t.Fatalf("want 52 built-in icons, got %d", len(BuiltinIcons))
	}
	seen := map[string]bool{}
	for _, n := range BuiltinIcons {
		if seen[n] || n == "" || n != strings.ToLower(n) || strings.ContainsAny(n, " :_") {
			t.Fatalf("bad icon name %q", n)
		}
		seen[n] = true
	}
	for _, n := range []string{"house", "layout-grid", "gamepad-2", "rocket", "chart-line"} {
		if !seen[n] {
			t.Errorf("missing %s", n)
		}
	}
}

func TestWorkspaceIconValidationAndDefault(t *testing.T) {
	s, _ := open(t)
	if w := firstWS(t, s); w.Icon != "lucide:house" {
		t.Fatalf("default workspace icon %q", w.Icon)
	}
	w, err := s.CreateWorkspace(WorkspaceInput{Name: "Empty"})
	if err != nil || w.Icon != "lucide:layout-grid" {
		t.Fatalf("empty icon -> %+v %v", w, err)
	}
	for _, n := range BuiltinIcons {
		if _, err := s.CreateWorkspace(WorkspaceInput{Name: "w", Icon: "lucide:" + n}); err != nil {
			t.Fatalf("lucide:%s rejected: %v", n, err)
		}
	}
	for _, bad := range []string{"🏠", "lucide:", "lucide:nope", "house", "Lucide:house", "lucide:House", "lucide:house ", "emoji:🏠", "lucide:../x", "url:http://x"} {
		_, err := s.CreateWorkspace(WorkspaceInput{Name: "w", Icon: bad})
		if bad == "lucide:house " { // surrounding whitespace is trimmed, not an error
			if err != nil {
				t.Errorf("%q: %v", bad, err)
			}
			continue
		}
		if ErrKind(err) != KindInvalid {
			t.Errorf("create with icon %q: %v", bad, err)
		}
		bp := bad
		if _, err := s.UpdateWorkspace(w.ID, WorkspacePatch{Icon: &bp}); ErrKind(err) != KindInvalid {
			t.Errorf("update with icon %q: %v", bad, err)
		}
	}
	ic := "lucide:rocket"
	if u, err := s.UpdateWorkspace(w.ID, WorkspacePatch{Icon: &ic}); err != nil || u.Icon != ic {
		t.Fatalf("update icon: %+v %v", u, err)
	}
	empty := ""
	if u, err := s.UpdateWorkspace(w.ID, WorkspacePatch{Icon: &empty}); err != nil || u.Icon != "lucide:layout-grid" {
		t.Fatalf("clearing icon should give default: %+v %v", u, err)
	}
}

func TestMigrateWorkspaceIcon(t *testing.T) {
	cases := map[string]string{
		"🏠": "lucide:house", "🏡": "lucide:house",
		"🎬": "lucide:clapperboard", "🎞": "lucide:clapperboard", "📺": "lucide:clapperboard",
		"🛠": "lucide:wrench", "🛠️": "lucide:wrench", "🔧": "lucide:wrench", "⚙️": "lucide:wrench",
		"🌐": "lucide:globe", "🌍": "lucide:globe", "🌎": "lucide:globe",
		"💼": "lucide:briefcase", "🎮": "lucide:gamepad-2", "🎵": "lucide:music", "🎶": "lucide:music",
		"📚": "lucide:book-open", "📖": "lucide:book-open",
		"💻": "lucide:code", "🖥": "lucide:code", "🖥️": "lucide:code",
		"☁️": "lucide:cloud", "🔒": "lucide:lock", "🔐": "lucide:lock", "⭐": "lucide:star", "❤️": "lucide:heart",
		"🚀": "lucide:layout-grid", "": "lucide:layout-grid", "whatever": "lucide:layout-grid",
		"lucide:server": "lucide:server", "lucide:bogus": "lucide:layout-grid",
	}
	for in, want := range cases {
		if got := migrateWorkspaceIcon(in); got != want {
			t.Errorf("migrate(%q) = %q, want %q", in, got, want)
		}
	}
	for _, v := range legacyEmojiIcons {
		if !builtinIconSet[v] {
			t.Errorf("legacy mapping targets unknown icon %q", v)
		}
	}
}

// writeLegacy writes an old-format data file (emoji icons, no pin_sort).
func writeLegacy(t *testing.T, dir string) {
	t.Helper()
	legacy := `{
  "schema": 1,
  "auth": {"password_hash": "", "session_secret": "abc", "api_token": "gk_legacy"},
  "settings": {"title": "Old", "sort_mode": "custom", "guest_access": false, "search_engine": "https://www.google.com/search?q=%s", "open_in_new_tab": true, "health_interval": 60, "show_clock": true},
  "workspaces": [
    {"id": "w1", "name": "Home", "icon": "🏠", "wallpaper": "preset:aurora", "sort": 0},
    {"id": "w2", "name": "Media", "icon": "🎬", "wallpaper": "", "sort": 1},
    {"id": "w3", "name": "Odd", "icon": "🦄", "wallpaper": "preset:dusk", "sort": 2},
    {"id": "w4", "name": "None", "icon": "", "wallpaper": "preset:dusk", "sort": 3}
  ],
  "categories": [{"id": "c1", "workspace_id": "w1", "name": "Cat", "sort": 0}],
  "links": [
    {"id": "l1", "workspace_id": "w1", "category_id": "c1", "title": "a", "url": "http://a", "pinned": true, "sort": 4, "clicks": 0, "last_used_at": null, "health_check": false, "created_at": "t", "updated_at": "t"},
    {"id": "l2", "workspace_id": "w1", "category_id": "c1", "title": "b", "url": "http://b", "pinned": true, "sort": 7, "clicks": 0, "last_used_at": null, "health_check": false, "created_at": "t", "updated_at": "t"},
    {"id": "l3", "workspace_id": "w1", "category_id": "", "title": "c", "url": "http://c", "pinned": false, "sort": 2, "clicks": 0, "last_used_at": null, "health_check": false, "created_at": "t", "updated_at": "t"}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationFromLegacyFile(t *testing.T) {
	dir := t.TempDir()
	writeLegacy(t, dir)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	wantIcons := map[string]string{"w1": "lucide:house", "w2": "lucide:clapperboard", "w3": "lucide:layout-grid", "w4": "lucide:layout-grid"}
	for _, w := range snap.Workspaces {
		if w.Icon != wantIcons[w.ID] {
			t.Errorf("workspace %s icon %q, want %q", w.ID, w.Icon, wantIcons[w.ID])
		}
	}
	wantPin := map[string]int{"l1": 4, "l2": 7, "l3": 2}
	for _, l := range snap.Links {
		if l.PinSort != wantPin[l.ID] {
			t.Errorf("link %s pin_sort %d, want %d", l.ID, l.PinSort, wantPin[l.ID])
		}
	}
	s.Close()

	// persisted: the file itself was rewritten with lucide icons and pin_sort
	raw, _ := os.ReadFile(filepath.Join(dir, fileName))
	if strings.Contains(string(raw), "🏠") || strings.Contains(string(raw), "🎬") || !strings.Contains(string(raw), `"pin_sort": 7`) {
		t.Fatalf("migration not persisted:\n%s", raw)
	}
	var on struct {
		Workspaces []Workspace `json:"workspaces"`
		Links      []struct {
			ID      string `json:"id"`
			PinSort *int   `json:"pin_sort"`
		} `json:"links"`
	}
	if err := json.Unmarshal(raw, &on); err != nil || on.Workspaces[0].Icon != "lucide:house" {
		t.Fatalf("persisted ws: %v %+v", err, on.Workspaces)
	}
	for _, l := range on.Links {
		if l.PinSort == nil {
			t.Errorf("link %s lacks pin_sort on disk", l.ID)
		}
	}
	// reopening is stable (no further rewrite)
	st1, _ := os.Stat(filepath.Join(dir, fileName))
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s2.Close()
	st2, _ := os.Stat(filepath.Join(dir, fileName))
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Error("data file rewritten on a clean reopen")
	}
}

func TestPinSortZeroSurvivesReload(t *testing.T) {
	// a genuine pin_sort of 0 must not be overwritten by sort on reload
	s, dir := open(t)
	ws := firstWS(t, s)
	a, _ := s.CreateLink(LinkInput{WorkspaceID: ws.ID, Title: "a", URL: "http://a"})
	b, _ := s.CreateLink(LinkInput{WorkspaceID: ws.ID, Title: "b", URL: "http://b", Pinned: true})
	if b.PinSort != 0 || a.Sort != 0 || b.Sort != 1 {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	s.Close()
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, _ := s2.GetLink(b.ID)
	if got.PinSort != 0 {
		t.Fatalf("pin_sort changed on reload: %d", got.PinSort)
	}
}

// Pinned links ---------------------------------------------------------------------

func pinSorts(s *Store, ws string) map[string]int {
	out := map[string]int{}
	for _, l := range s.Snapshot().Links {
		if l.WorkspaceID == ws && l.Pinned {
			out[l.Title] = l.PinSort
		}
	}
	return out
}

func TestPinSortAssignment(t *testing.T) {
	s, _ := open(t)
	ws := firstWS(t, s)
	ws2, _ := s.CreateWorkspace(WorkspaceInput{Name: "Two"})
	mk := func(w, title string, pinned bool) Link {
		l, err := s.CreateLink(LinkInput{WorkspaceID: w, Title: title, URL: "http://" + title, Pinned: pinned})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	a, b, c := mk(ws.ID, "a", true), mk(ws.ID, "b", false), mk(ws.ID, "c", true)
	if a.PinSort != 0 || c.PinSort != 1 {
		t.Fatalf("create: a=%d c=%d", a.PinSort, c.PinSort)
	}
	// batch create assigns increasing values too
	res, _ := s.CreateLinks([]LinkInput{
		{WorkspaceID: ws.ID, Title: "d", URL: "http://d", Pinned: true},
		{WorkspaceID: ws.ID, Title: "e", URL: "http://e", Pinned: true},
	})
	if res[0].Link.PinSort != 2 || res[1].Link.PinSort != 3 {
		t.Fatalf("batch: %d %d", res[0].Link.PinSort, res[1].Link.PinSort)
	}
	// PATCH pinned false -> true appends at the end; sort/category untouched
	yes := true
	bp, err := s.UpdateLink(b.ID, LinkPatch{Pinned: &yes})
	if err != nil || bp.PinSort != 4 || bp.Sort != b.Sort || bp.CategoryID != b.CategoryID {
		t.Fatalf("patch pin: %+v %v", bp, err)
	}
	// re-sending pinned:true does not move it
	d := res[0].Link
	if u, _ := s.UpdateLink(d.ID, LinkPatch{Pinned: &yes}); u.PinSort != 2 {
		t.Fatalf("idempotent pin moved: %d", u.PinSort)
	}
	// unpin keeps the others; re-pin goes to the end (max+1)
	no := false
	if u, _ := s.UpdateLink(a.ID, LinkPatch{Pinned: &no}); u.Pinned {
		t.Fatal("unpin failed")
	}
	if u, _ := s.UpdateLink(a.ID, LinkPatch{Pinned: &yes}); u.PinSort != 5 {
		t.Fatalf("re-pin pin_sort %d, want 5", u.PinSort)
	}
	// pinning in another workspace is independent
	other := mk(ws2.ID, "o", false)
	if u, _ := s.UpdateLink(other.ID, LinkPatch{Pinned: &yes}); u.PinSort != 0 {
		t.Fatalf("other ws pin_sort %d", u.PinSort)
	}
	// moving a pinned link to another workspace re-appends it there
	if u, err := s.UpdateLink(c.ID, LinkPatch{WorkspaceID: &ws2.ID}); err != nil || u.PinSort != 1 || !u.Pinned {
		t.Fatalf("move pinned across workspaces: %+v %v", u, err)
	}
	// pinning via __pinned reorder assigns positions and sets pinned
	z := mk(ws.ID, "z", false)
	if err := s.ReorderLinks(ws.ID, PinnedGroup, []string{z.ID}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetLink(z.ID); !got.Pinned || got.PinSort != 0 {
		t.Fatalf("reorder __pinned: %+v", got)
	}
}

// Pinned links are ordinary members of their category again.
func TestReorderCategoryIncludesPinnedLinks(t *testing.T) {
	s, _ := open(t)
	ws := firstWS(t, s)
	c1, _ := s.CreateCategory(ws.ID, "One")
	mk := func(title string, pinned bool) string {
		l, err := s.CreateLink(LinkInput{WorkspaceID: ws.ID, CategoryID: c1.ID, Title: title, URL: "http://" + title, Pinned: pinned})
		if err != nil {
			t.Fatal(err)
		}
		return l.ID
	}
	p, a, b := mk("p", true), mk("a", false), mk("b", false)
	catOrder := func() string {
		var out string
		for _, l := range s.Snapshot().Links {
			if l.CategoryID == c1.ID {
				out += l.Title
			}
		}
		return out
	}
	if got := catOrder(); got != "pab" {
		t.Fatalf("initial order %q (pinned links keep their category slot)", got)
	}
	// reordering the category renumbers every member, pinned included, with no collisions
	if err := s.ReorderLinks(ws.ID, c1.ID, []string{b, p, a}); err != nil {
		t.Fatal(err)
	}
	if got := catOrder(); got != "bpa" {
		t.Fatalf("after reorder %q", got)
	}
	sorts := map[string]int{}
	for _, l := range s.Snapshot().Links {
		sorts[l.Title] = l.Sort
	}
	if sorts["b"] != 0 || sorts["p"] != 1 || sorts["a"] != 2 {
		t.Fatalf("sorts %v", sorts)
	}
	// a partial reorder lets unlisted pinned siblings follow
	if err := s.ReorderLinks(ws.ID, c1.ID, []string{a}); err != nil {
		t.Fatal(err)
	}
	if got := catOrder(); got != "abp" {
		t.Fatalf("partial reorder %q", got)
	}
	// pin_sort was not touched by category reordering
	if ps := pinSorts(s, ws.ID); ps["p"] != 0 || len(ps) != 1 {
		t.Fatalf("pin sorts %v", ps)
	}
	// and a pinned link can be dragged to another category like any other
	c2, _ := s.CreateCategory(ws.ID, "Two")
	if err := s.ReorderLinks(ws.ID, c2.ID, []string{p}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetLink(p); got.CategoryID != c2.ID || !got.Pinned {
		t.Fatalf("moved pinned: %+v", got)
	}
}

func TestReorderPinnedOnlyTouchesPinSort(t *testing.T) {
	s, _ := open(t)
	ws := firstWS(t, s)
	c1, _ := s.CreateCategory(ws.ID, "One")
	c2, _ := s.CreateCategory(ws.ID, "Two")
	mk := func(cat, title string, pinned bool) string {
		l, _ := s.CreateLink(LinkInput{WorkspaceID: ws.ID, CategoryID: cat, Title: title, URL: "http://" + title, Pinned: pinned})
		return l.ID
	}
	a, b, c, d := mk(c1.ID, "a", true), mk(c1.ID, "b", false), mk(c2.ID, "c", true), mk("", "d", false)
	before := map[string]Link{}
	for _, l := range s.Snapshot().Links {
		before[l.Title] = l
	}
	// c before a, plus unpinned d which becomes pinned
	if err := s.ReorderLinks(ws.ID, PinnedGroup, []string{c, d, a}); err != nil {
		t.Fatal(err)
	}
	after := map[string]Link{}
	for _, l := range s.Snapshot().Links {
		after[l.Title] = l
	}
	for title, l := range after {
		if l.Sort != before[title].Sort || l.CategoryID != before[title].CategoryID || l.WorkspaceID != before[title].WorkspaceID {
			t.Errorf("%s: sort/category changed: %+v -> %+v", title, before[title], l)
		}
	}
	if !after["d"].Pinned || after["b"].Pinned {
		t.Fatalf("pinned flags: d=%v b=%v", after["d"].Pinned, after["b"].Pinned)
	}
	if after["c"].PinSort != 0 || after["d"].PinSort != 1 || after["a"].PinSort != 2 {
		t.Fatalf("pin_sorts c=%d d=%d a=%d", after["c"].PinSort, after["d"].PinSort, after["a"].PinSort)
	}
	_ = b
	// unlisted pinned links follow the listed ones in their previous relative order
	if err := s.ReorderLinks(ws.ID, PinnedGroup, []string{a}); err != nil {
		t.Fatal(err)
	}
	ps := pinSorts(s, ws.ID)
	if ps["a"] != 0 || ps["c"] != 1 || ps["d"] != 2 {
		t.Fatalf("partial pinned reorder %v", ps)
	}
	// errors
	if err := s.ReorderLinks("nope", PinnedGroup, nil); ErrKind(err) != KindNotFound {
		t.Fatalf("unknown ws: %v", err)
	}
	ws2, _ := s.CreateWorkspace(WorkspaceInput{Name: "Two"})
	if err := s.ReorderLinks(ws2.ID, PinnedGroup, []string{a}); ErrKind(err) != KindInvalid {
		t.Fatalf("foreign link: %v", err)
	}
}

func TestExportImportCarriesPinSortAndMigratesEmoji(t *testing.T) {
	s, _ := open(t)
	ws := firstWS(t, s)
	_, _ = s.CreateLink(LinkInput{WorkspaceID: ws.ID, Title: "a", URL: "http://a", Pinned: true})
	l2, _ := s.CreateLink(LinkInput{WorkspaceID: ws.ID, Title: "b", URL: "http://b", Pinned: true})
	ex := s.Export()
	raw, _ := json.Marshal(ex)
	if !strings.Contains(string(raw), `"pin_sort":1`) || !strings.Contains(string(raw), `"icon":"lucide:house"`) {
		t.Fatalf("export: %s", raw)
	}
	var back Export
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	s2, _ := open(t)
	if _, _, err := s2.Import("replace", back); err != nil {
		t.Fatal(err)
	}
	for _, l := range s2.Snapshot().Links {
		if l.Title == "b" && (l.PinSort != 1 || !l.Pinned) {
			t.Fatalf("imported %+v", l)
		}
	}
	_ = l2

	// old export: emoji icon, no pin_sort -> pin_sort = sort, icon mapped
	old := `{"schema":1,"workspaces":[{"id":"w","name":"Old","icon":"🎮","wallpaper":"","sort":0},{"id":"x","name":"Blank","icon":"","wallpaper":"","sort":1}],
	  "categories":[],"links":[{"id":"l","workspace_id":"w","title":"t","url":"http://t","pinned":true,"sort":5}]}`
	var oe Export
	if err := json.Unmarshal([]byte(old), &oe); err != nil {
		t.Fatal(err)
	}
	if oe.Links[0].PinSort != 5 {
		t.Fatalf("legacy export pin_sort %d", oe.Links[0].PinSort)
	}
	s3, _ := open(t)
	if _, _, err := s3.Import("replace", oe); err != nil {
		t.Fatal(err)
	}
	snap := s3.Snapshot()
	if snap.Workspaces[0].Icon != "lucide:gamepad-2" || snap.Workspaces[1].Icon != "lucide:layout-grid" || snap.Links[0].PinSort != 5 {
		t.Fatalf("import: %+v %+v", snap.Workspaces, snap.Links)
	}
	// an unknown lucide name in an import is rejected
	bad := oe
	bad.Workspaces = []Workspace{{ID: "w", Name: "B", Icon: "lucide:nope"}}
	bad.Links = nil
	if _, _, err := s3.Import("replace", bad); ErrKind(err) != KindInvalid {
		t.Fatalf("bad lucide import: %v", err)
	}
}

// Wallpapers --------------------------------------------------------------------------

func TestResetWallpaperUsers(t *testing.T) {
	s, _ := open(t)
	w1 := firstWS(t, s)
	w2, _ := s.CreateWorkspace(WorkspaceInput{Name: "B", Wallpaper: "upload:aaaa.png"})
	w3, _ := s.CreateWorkspace(WorkspaceInput{Name: "C", Wallpaper: "upload:bbbb.png"})
	wp := "upload:aaaa.png"
	_, _ = s.UpdateWorkspace(w1.ID, WorkspacePatch{Wallpaper: &wp})
	v := s.Version()
	n, err := s.ResetWallpaperUsers("aaaa.png")
	if err != nil || n != 2 || s.Version() != v+1 {
		t.Fatalf("n=%d err=%v version %d->%d", n, err, v, s.Version())
	}
	got := map[string]string{}
	for _, w := range s.Snapshot().Workspaces {
		got[w.ID] = w.Wallpaper
	}
	if got[w1.ID] != "preset:aurora" || got[w2.ID] != "preset:aurora" || got[w3.ID] != "upload:bbbb.png" {
		t.Fatalf("%v", got)
	}
}
