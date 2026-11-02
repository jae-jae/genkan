package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

func firstWS(t *testing.T, s *Store) Workspace {
	t.Helper()
	return s.Snapshot().Workspaces[0]
}

func TestDefaults(t *testing.T) {
	s, dir := open(t)
	snap := s.Snapshot()
	if len(snap.Workspaces) != 1 || snap.Workspaces[0].Name != "Home" || snap.Workspaces[0].Wallpaper != "preset:aurora" {
		t.Fatalf("default workspace: %+v", snap.Workspaces)
	}
	want := Settings{Title: "Genkan", SortMode: "frequency", SearchEngine: "https://www.google.com/search?q=%s", OpenInNewTab: true, HealthInterval: 60, ShowClock: true}
	if snap.Settings != want {
		t.Fatalf("settings: %+v", snap.Settings)
	}
	if !s.SetupRequired() {
		t.Fatal("setup should be required")
	}
	if _, secret := s.AuthState(); len(secret) != 64 {
		t.Fatalf("secret: %q", secret)
	}
	if _, err := os.Stat(filepath.Join(dir, "genkan.json")); err != nil {
		t.Fatal(err)
	}
}

func TestSortAssignmentAndCRUD(t *testing.T) {
	s, _ := open(t)
	ws := firstWS(t, s)
	c1, _ := s.CreateCategory(ws.ID, "  Media ")
	c2, _ := s.CreateCategory(ws.ID, "Tools")
	if c1.Name != "Media" || c1.Sort != 0 || c2.Sort != 1 {
		t.Fatalf("categories %+v %+v", c1, c2)
	}
	l1, err := s.CreateLink(LinkInput{WorkspaceID: ws.ID, CategoryID: c1.ID, Title: "A", URL: "http://a.lan"})
	if err != nil {
		t.Fatal(err)
	}
	l2, _ := s.CreateLink(LinkInput{WorkspaceID: ws.ID, CategoryID: c1.ID, Title: "B", URL: "b.lan:81"})
	if l1.Sort != 0 || l2.Sort != 1 || l2.URL != "http://b.lan:81" {
		t.Fatalf("links %+v %+v", l1, l2)
	}
	// validation
	bad := []LinkInput{
		{WorkspaceID: ws.ID, Title: "", URL: "http://x"},
		{WorkspaceID: ws.ID, Title: "x", URL: "javascript:alert(1)"},
		{WorkspaceID: ws.ID, Title: "x", URL: "JavaScript:alert(1)"},
		{WorkspaceID: ws.ID, Title: "x", URL: "http://"},
		{WorkspaceID: ws.ID, Title: "x", URL: "http://x", Icon: "bogus"},
		{WorkspaceID: ws.ID, Title: "x", URL: "http://x", Icon: "favicon:../etc"},
		{WorkspaceID: "nope", Title: "x", URL: "http://x"},
		{WorkspaceID: ws.ID, CategoryID: "nope", Title: "x", URL: "http://x"},
		{WorkspaceID: ws.ID, Title: strings.Repeat("x", 500), URL: "http://x"},
	}
	for i, b := range bad {
		if _, err := s.CreateLink(b); err == nil {
			t.Errorf("bad[%d] accepted", i)
		}
	}
	// move to other category => sort = max+1 there
	l3, _ := s.CreateLink(LinkInput{WorkspaceID: ws.ID, CategoryID: c2.ID, Title: "C", URL: "http://c"})
	cat := c2.ID
	moved, err := s.UpdateLink(l1.ID, LinkPatch{CategoryID: &cat})
	if err != nil || moved.CategoryID != c2.ID || moved.Sort != l3.Sort+1 {
		t.Fatalf("move: %+v %v", moved, err)
	}
	// category from another workspace is rejected
	ws2, _ := s.CreateWorkspace(WorkspaceInput{Name: "Work", Icon: "lucide:briefcase"})
	if ws2.Sort != 1 || ws2.Wallpaper != "preset:aurora" {
		t.Fatalf("ws2 %+v", ws2)
	}
	if _, err := s.UpdateLink(l2.ID, LinkPatch{WorkspaceID: &ws2.ID, CategoryID: &c1.ID}); ErrKind(err) != KindInvalid {
		t.Fatalf("cross-ws category: %v", err)
	}
	// moving workspace clears category
	m2, err := s.UpdateLink(l2.ID, LinkPatch{WorkspaceID: &ws2.ID})
	if err != nil || m2.WorkspaceID != ws2.ID || m2.CategoryID != "" {
		t.Fatalf("ws move: %+v %v", m2, err)
	}
	// delete category => links uncategorized
	if err := s.DeleteCategory(c2.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetLink(l3.ID)
	if got.CategoryID != "" {
		t.Fatalf("link should be uncategorized: %+v", got)
	}
	c3, _ := s.CreateCategory(ws.ID, "Del")
	l4, _ := s.CreateLink(LinkInput{WorkspaceID: ws.ID, CategoryID: c3.ID, Title: "D", URL: "http://d"})
	_ = s.DeleteCategory(c3.ID, true)
	if _, ok := s.GetLink(l4.ID); ok {
		t.Fatal("link should be deleted with category")
	}
	// delete workspace cascades; last one is protected
	if err := s.DeleteWorkspace(ws2.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.GetLink(l2.ID); ok {
		t.Fatal("cascade failed")
	}
	if err := s.DeleteWorkspace(ws.ID); ErrKind(err) != KindConflict {
		t.Fatalf("last workspace delete: %v", err)
	}
	if _, err := s.UpdateLink("nope", LinkPatch{}); ErrKind(err) != KindNotFound {
		t.Fatalf("not found: %v", err)
	}
}

func TestReorder(t *testing.T) {
	s, _ := open(t)
	ws := firstWS(t, s)
	c1, _ := s.CreateCategory(ws.ID, "One")
	c2, _ := s.CreateCategory(ws.ID, "Two")
	mk := func(cat, title string) string {
		l, err := s.CreateLink(LinkInput{WorkspaceID: ws.ID, CategoryID: cat, Title: title, URL: "http://" + title})
		if err != nil {
			t.Fatal(err)
		}
		return l.ID
	}
	a, b, c := mk(c1.ID, "a"), mk(c1.ID, "b"), mk(c2.ID, "c")
	order := func(cat string) []string {
		var out []string
		for _, l := range s.Snapshot().Links {
			if l.CategoryID == cat {
				out = append(out, l.Title)
			}
		}
		return out
	}
	// cross-category drag: c goes between a and b in category one
	if err := s.ReorderLinks(ws.ID, c1.ID, []string{a, c, b}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order(c1.ID), ""); got != "acb" {
		t.Fatalf("order %s", got)
	}
	if len(order(c2.ID)) != 0 {
		t.Fatal("c should have left category two")
	}
	// unlisted siblings follow listed ones
	if err := s.ReorderLinks(ws.ID, c1.ID, []string{b}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order(c1.ID), ""); got != "bac" {
		t.Fatalf("order %s", got)
	}
	// pinned area
	_ = s.ReorderLinks(ws.ID, PinnedGroup, []string{c, a})
	snap := s.Snapshot()
	pl := []Link{}
	for _, l := range snap.Links {
		if l.Pinned {
			pl = append(pl, l)
		}
	}
	sort.Slice(pl, func(i, j int) bool { return pl[i].PinSort < pl[j].PinSort })
	pinned := ""
	for _, l := range pl {
		pinned += l.Title
	}
	if pinned != "ca" {
		t.Fatalf("pinned order %q", pinned)
	}
	// categories & workspaces
	if err := s.ReorderCategories(ws.ID, []string{c2.ID, c1.ID}); err != nil {
		t.Fatal(err)
	}
	cs := s.Snapshot().Categories
	if cs[0].ID != c2.ID || cs[0].Sort != 0 || cs[1].Sort != 1 {
		t.Fatalf("cats %+v", cs)
	}
	w2, _ := s.CreateWorkspace(WorkspaceInput{Name: "Two"})
	if err := s.ReorderWorkspaces([]string{w2.ID, ws.ID}); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Workspaces[0].ID != w2.ID {
		t.Fatal("workspace order")
	}
	if err := s.ReorderLinks(ws.ID, c1.ID, []string{"nope"}); ErrKind(err) != KindNotFound {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestVersionAndNotify(t *testing.T) {
	s, _ := open(t)
	var got []int64
	s.OnChange(func(v int64) { got = append(got, v) })
	v0 := s.Version()
	ws := firstWS(t, s)
	l, _ := s.CreateLink(LinkInput{WorkspaceID: ws.ID, Title: "x", URL: "http://x"})
	if s.Version() != v0+1 {
		t.Fatalf("version %d", s.Version())
	}
	_ = s.Click(l.ID) // clicks do not bump the version
	if s.Version() != v0+1 || len(got) != 1 {
		t.Fatalf("click bumped version: %d %v", s.Version(), got)
	}
	// invalid ops do not bump
	_, _ = s.CreateLink(LinkInput{WorkspaceID: ws.ID, Title: "", URL: "http://x"})
	if s.Version() != v0+1 {
		t.Fatal("failed op bumped version")
	}
}

func TestPersistenceAndDebouncedFlush(t *testing.T) {
	s, dir := open(t)
	ws := firstWS(t, s)
	l, _ := s.CreateLink(LinkInput{WorkspaceID: ws.ID, Title: "x", URL: "http://x"})
	_ = s.Click(l.ID)
	_ = s.Click(l.ID)
	// not yet on disk (debounced)
	raw, _ := os.ReadFile(filepath.Join(dir, "genkan.json"))
	var disk fileData
	_ = json.Unmarshal(raw, &disk)
	if disk.Links[0].Clicks != 0 {
		t.Fatalf("clicks persisted immediately: %d", disk.Links[0].Clicks)
	}
	if err := s.Close(); err != nil { // flush on shutdown
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, ok := s2.GetLink(l.ID)
	if !ok || got.Clicks != 2 || got.LastUsedAt == nil {
		t.Fatalf("reloaded: %+v", got)
	}
	// no temp files left behind, file mode private
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}
	if st, _ := os.Stat(filepath.Join(dir, "genkan.json")); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}

func TestDebounceTimerFires(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	s, dir := open(t)
	ws := firstWS(t, s)
	l, _ := s.CreateLink(LinkInput{WorkspaceID: ws.ID, Title: "x", URL: "http://x"})
	_ = s.Click(l.ID)
	time.Sleep(flushDebounce + 700*time.Millisecond)
	raw, _ := os.ReadFile(filepath.Join(dir, "genkan.json"))
	var disk fileData
	_ = json.Unmarshal(raw, &disk)
	if disk.Links[0].Clicks != 1 {
		t.Fatalf("debounced flush did not happen: %d", disk.Links[0].Clicks)
	}
}

func TestCorruptFileIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "genkan.json")
	_ = os.WriteFile(p, []byte("{not json"), 0o600)
	if _, err := Open(dir); err == nil {
		t.Fatal("expected error")
	}
	if b, _ := os.ReadFile(p); string(b) != "{not json" {
		t.Fatal("corrupt file was modified")
	}
}

func TestExportImport(t *testing.T) {
	s, _ := open(t)
	ws := firstWS(t, s)
	c, _ := s.CreateCategory(ws.ID, "Media")
	_, _ = s.CreateLink(LinkInput{WorkspaceID: ws.ID, CategoryID: c.ID, Title: "J", URL: "http://j", Pinned: true})
	_, _ = s.CreateLink(LinkInput{WorkspaceID: ws.ID, Title: "K", URL: "http://k", Icon: "emoji:🚀"})
	exp := s.Export()
	raw, _ := json.Marshal(exp)
	if strings.Contains(string(raw), "password") || strings.Contains(string(raw), "token") || strings.Contains(string(raw), s.APIToken()) {
		t.Fatal("export leaks auth")
	}

	// merge: new IDs, existing untouched
	nw, nl, err := s.Import("merge", exp)
	if err != nil || nw != 1 || nl != 2 {
		t.Fatalf("merge: %d %d %v", nw, nl, err)
	}
	snap := s.Snapshot()
	if len(snap.Workspaces) != 2 || len(snap.Links) != 4 || len(snap.Categories) != 2 {
		t.Fatalf("merge result: %d/%d/%d", len(snap.Workspaces), len(snap.Categories), len(snap.Links))
	}
	ids := map[string]bool{}
	for _, l := range snap.Links {
		if ids[l.ID] {
			t.Fatal("duplicate link id after merge")
		}
		ids[l.ID] = true
	}
	for _, l := range snap.Links { // each link's category belongs to its workspace
		if l.CategoryID == "" {
			continue
		}
		ok := false
		for _, c := range snap.Categories {
			if c.ID == l.CategoryID && c.WorkspaceID == l.WorkspaceID {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("link %s has foreign category", l.Title)
		}
	}

	// replace
	exp.Workspaces = exp.Workspaces[:1]
	exp.Settings.Title = "Imported"
	exp.Settings.HealthInterval = 3 // clamped
	if _, _, err := s.Import("replace", exp); err != nil {
		t.Fatal(err)
	}
	snap = s.Snapshot()
	if len(snap.Workspaces) != 1 || len(snap.Links) != 2 || snap.Settings.Title != "Imported" || snap.Settings.HealthInterval != 15 {
		t.Fatalf("replace: %+v", snap.Settings)
	}
	// invalid imports are rejected without changing state
	v := s.Version()
	bad := exp
	bad.Links = append([]Link{{WorkspaceID: exp.Workspaces[0].ID, Title: "x", URL: "javascript:1"}}, exp.Links...)
	if _, _, err := s.Import("replace", bad); ErrKind(err) != KindInvalid {
		t.Fatalf("bad import: %v", err)
	}
	if _, _, err := s.Import("replace", Export{}); ErrKind(err) != KindInvalid {
		t.Fatalf("empty import: %v", err)
	}
	if _, _, err := s.Import("bogus", exp); ErrKind(err) != KindInvalid {
		t.Fatalf("bad mode: %v", err)
	}
	if s.Version() != v {
		t.Fatal("failed import changed state")
	}
}

func TestSettingsPatch(t *testing.T) {
	s, _ := open(t)
	m, tt := "recent", true
	got, err := s.UpdateSettings(SettingsPatch{SortMode: &m, GuestAccess: &tt})
	if err != nil || got.SortMode != "recent" || !got.GuestAccess || got.Title != "Genkan" {
		t.Fatalf("%+v %v", got, err)
	}
	bad := "weird"
	if _, err := s.UpdateSettings(SettingsPatch{SortMode: &bad}); err == nil {
		t.Fatal("bad sort mode accepted")
	}
	se := "https://x/search"
	if _, err := s.UpdateSettings(SettingsPatch{SearchEngine: &se}); err == nil {
		t.Fatal("search engine without placeholder accepted")
	}
}

func TestPassword(t *testing.T) {
	s, dir := open(t)
	ok, _ := s.SetInitialPassword("hash1")
	ok2, _ := s.SetInitialPassword("hash2")
	if !ok || ok2 {
		t.Fatal("initial password semantic")
	}
	_, old := s.AuthState()
	secret, _ := s.ChangePassword("hash3")
	if secret == old {
		t.Fatal("secret not rotated")
	}
	_ = s.Close()
	s2, _ := Open(dir)
	defer s2.Close()
	if h, sec := s2.AuthState(); h != "hash3" || sec != secret {
		t.Fatal("auth not persisted")
	}
}

func TestAPIToken(t *testing.T) {
	s, dir := open(t)
	tok := s.APIToken()
	if !strings.HasPrefix(tok, "gk_") || len(tok) != 35 {
		t.Fatalf("generated token %q", tok)
	}
	if !s.CheckToken(tok) || s.CheckToken("") || s.CheckToken(tok+"x") || s.CheckToken(tok[:10]) || s.CheckToken("gk_nope") {
		t.Fatal("CheckToken")
	}
	// persisted in plaintext under auth.api_token
	raw, _ := os.ReadFile(filepath.Join(dir, "genkan.json"))
	var disk struct {
		Auth struct {
			APIToken string `json:"api_token"`
		}
		Tokens json.RawMessage `json:"tokens"`
	}
	_ = json.Unmarshal(raw, &disk)
	if disk.Auth.APIToken != tok || disk.Tokens != nil {
		t.Fatalf("on disk: %+v", disk)
	}
	_ = s.Close()
	s2, _ := Open(dir)
	if s2.APIToken() != tok {
		t.Fatal("token regenerated on reopen")
	}
	nt, err := s2.RegenerateToken()
	if err != nil || nt == tok || !strings.HasPrefix(nt, "gk_") {
		t.Fatalf("regenerate: %q %v", nt, err)
	}
	if s2.CheckToken(tok) || !s2.CheckToken(nt) {
		t.Fatal("old token must stop working immediately")
	}
	_ = s2.Close()
	s3, _ := Open(dir)
	defer s3.Close()
	if s3.APIToken() != nt {
		t.Fatal("regenerated token not persisted")
	}
}

func TestLegacyTokensArrayIsDropped(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"schema":1,"auth":{"password_hash":"h","session_secret":"abc"},
	  "tokens":[{"id":"t1","name":"ci","hash":"deadbeef","prefix":"gk_abcd","created_at":"x","last_used_at":null}],
	  "settings":{},"workspaces":[],"categories":[],"links":[]}`
	if err := os.WriteFile(filepath.Join(dir, "genkan.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !strings.HasPrefix(s.APIToken(), "gk_") {
		t.Fatalf("no token generated for legacy file: %q", s.APIToken())
	}
	if h, sec := s.AuthState(); h != "h" || sec != "abc" {
		t.Fatal("legacy auth lost")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "genkan.json"))
	if strings.Contains(string(raw), `"tokens"`) || !strings.Contains(string(raw), s.APIToken()) {
		t.Fatalf("legacy tokens array not rewritten away:\n%s", raw)
	}
}

func TestCleanURL(t *testing.T) {
	ok := map[string]string{
		"https://a.b/c?d=1":  "https://a.b/c?d=1",
		"nas.lan:8096":       "http://nas.lan:8096",
		" 192.168.1.2/x ":    "http://192.168.1.2/x",
		"ssh://host.lan":     "ssh://host.lan",
		"http://[::1]:3000/": "http://[::1]:3000/",
	}
	for in, want := range ok {
		got, err := CleanURL(in)
		if err != nil || got != want {
			t.Errorf("CleanURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "javascript:alert(1)", "data:text/html,x", "vbscript:x", "file:///etc/passwd", "http://", "http://exa mple.com"} {
		if _, err := CleanURL(in); err == nil {
			t.Errorf("CleanURL(%q) accepted", in)
		}
	}
}

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID()
		if len(id) != 12 || strings.Trim(id, "abcdefghijklmnopqrstuvwxyz234567") != "" || seen[id] {
			t.Fatalf("bad id %q", id)
		}
		seen[id] = true
	}
}

func TestSetIconOnlyIfEmpty(t *testing.T) {
	s, _ := open(t)
	ws := firstWS(t, s)
	l, _ := s.CreateLink(LinkInput{WorkspaceID: ws.ID, Title: "x", URL: "http://x"})
	if _, ok := s.SetIcon(l.ID, "favicon:a.png", true); !ok {
		t.Fatal("first set")
	}
	if _, ok := s.SetIcon(l.ID, "favicon:b.png", true); ok {
		t.Fatal("overwrote existing icon")
	}
	if got, _ := s.GetLink(l.ID); got.Icon != "favicon:a.png" {
		t.Fatal(got.Icon)
	}
}
