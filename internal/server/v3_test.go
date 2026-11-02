package server

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jae-jae/genkan/internal/store"
)

func (c *client) upload(path, name string, data []byte) (int, []byte) {
	c.e.t.Helper()
	body, ct := multipartBody(c.e.t, "file", name, data)
	resp, err := c.req("POST", path, body, ct)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// pngWith returns a distinct tiny PNG (content hash differs per tag).
func pngWith(tag string) []byte { return append(append([]byte{}, tinyPNG...), []byte(tag)...) }

type wpItem struct {
	Value     string `json:"value"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"created_at"`
}

func TestWorkspaceIconAPI(t *testing.T) {
	e := setup(t)
	c := e.admin()
	var ws store.Workspace
	c.mustJSON("POST", "/api/workspaces", `{"name":"Lab","icon":"lucide:flask-conical"}`, &ws)
	if ws.Icon != "lucide:flask-conical" {
		t.Fatalf("%+v", ws)
	}
	c.mustJSON("POST", "/api/workspaces", `{"name":"NoIcon"}`, &ws)
	if ws.Icon != "lucide:layout-grid" {
		t.Fatalf("default icon: %+v", ws)
	}
	for _, bad := range []string{`"💼"`, `"lucide:nope"`, `"house"`, `"emoji:🏠"`} {
		if code, b := c.do("POST", "/api/workspaces", `{"name":"x","icon":`+bad+`}`); code != 400 || !strings.Contains(string(b), "icon") {
			t.Errorf("create icon %s -> %d %s", bad, code, b)
		}
		if code, _ := c.do("PATCH", "/api/workspaces/"+ws.ID, `{"icon":`+bad+`}`); code != 400 {
			t.Errorf("patch icon %s -> %d", bad, code)
		}
	}
	c.mustJSON("PATCH", "/api/workspaces/"+ws.ID, `{"icon":"lucide:rocket"}`, &ws)
	if ws.Icon != "lucide:rocket" {
		t.Fatalf("%+v", ws)
	}
	var bs struct{ Workspaces []store.Workspace }
	c.mustJSON("GET", "/api/bootstrap", "", &bs)
	if bs.Workspaces[0].Icon != "lucide:house" {
		t.Fatalf("bootstrap default workspace icon %q", bs.Workspaces[0].Icon)
	}
}

func TestPinnedAPI(t *testing.T) {
	e := setup(t)
	c := e.admin()
	var cat store.Category
	c.mustJSON("POST", "/api/categories", `{"workspace_id":"`+e.ws+`","name":"Dev"}`, &cat)
	var a, b, d store.Link
	c.mustJSON("POST", "/api/links", `{"workspace_id":"`+e.ws+`","category_id":"`+cat.ID+`","title":"a","url":"http://a","pinned":true,"fetch_icon":false}`, &a)
	c.mustJSON("POST", "/api/links", `{"workspace_id":"`+e.ws+`","category_id":"`+cat.ID+`","title":"b","url":"http://b","fetch_icon":false}`, &b)
	c.mustJSON("POST", "/api/links", `{"workspace_id":"`+e.ws+`","title":"d","url":"http://d","fetch_icon":false}`, &d)
	if a.PinSort != 0 {
		t.Fatalf("a pin_sort %d", a.PinSort)
	}
	c.mustJSON("PATCH", "/api/links/"+b.ID, `{"pinned":true}`, &b)
	if !b.Pinned || b.PinSort != 1 || b.CategoryID != cat.ID {
		t.Fatalf("patch pin: %+v", b)
	}
	// __pinned reorder: pin_sort only
	c.mustJSON("POST", "/api/links/reorder", `{"workspace_id":"`+e.ws+`","category_id":"__pinned","ids":["`+d.ID+`","`+b.ID+`","`+a.ID+`"]}`, nil)
	var bs struct{ Links []store.Link }
	c.mustJSON("GET", "/api/bootstrap", "", &bs)
	by := map[string]store.Link{}
	for _, l := range bs.Links {
		by[l.Title] = l
	}
	if by["d"].PinSort != 0 || by["b"].PinSort != 1 || by["a"].PinSort != 2 || !by["d"].Pinned {
		t.Fatalf("pin_sorts: %+v", by)
	}
	if by["a"].Sort != a.Sort || by["b"].Sort != b.Sort || by["d"].CategoryID != "" || by["a"].CategoryID != cat.ID {
		t.Fatalf("__pinned reorder touched sort/category: %+v", by)
	}
	// category reorder includes pinned members
	c.mustJSON("POST", "/api/links/reorder", `{"workspace_id":"`+e.ws+`","category_id":"`+cat.ID+`","ids":["`+b.ID+`","`+a.ID+`"]}`, nil)
	c.mustJSON("GET", "/api/bootstrap", "", &bs)
	by = map[string]store.Link{}
	for _, l := range bs.Links {
		by[l.Title] = l
	}
	if by["b"].Sort != 0 || by["a"].Sort != 1 || by["a"].PinSort != 2 {
		t.Fatalf("category reorder: %+v", by)
	}
	// export carries pin_sort
	code, raw := c.do("GET", "/api/export", "")
	if code != 200 || !strings.Contains(string(raw), `"pin_sort"`) {
		t.Fatalf("export: %d %s", code, raw)
	}
}

func TestWallpaperGallery(t *testing.T) {
	e := setup(t)
	c := e.admin()

	// empty gallery is an empty array, not null
	code, b := c.do("GET", "/api/wallpapers", "")
	if code != 200 || strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("empty list: %d %s", code, b)
	}

	var u1, u2, u3 struct{ Value, URL string }
	for i, tag := range []string{"one", "two"} {
		code, b := c.upload("/api/uploads/wallpaper", tag+".png", pngWith(tag))
		if code != 200 {
			t.Fatalf("upload %s: %d %s", tag, code, b)
		}
		_ = json.Unmarshal(b, map[int]any{0: &u1, 1: &u2}[i])
	}
	// re-uploading identical bytes is idempotent
	code, b = c.upload("/api/uploads/wallpaper", "again.png", pngWith("one"))
	_ = json.Unmarshal(b, &u3)
	if code != 200 || u3.Value != u1.Value {
		t.Fatalf("idempotent upload: %d %s vs %s", code, b, u1.Value)
	}
	// stray files that are not ours must not be listed
	dir := filepath.Join(e.srv.cfg.DataDir, "uploads", "wallpapers")
	_ = os.WriteFile(filepath.Join(dir, ".upload-123"), []byte("x"), 0o644)
	_ = os.Mkdir(filepath.Join(dir, "subdir.png"), 0o755)
	// deterministic mtimes: "one" is older than "two"
	now := time.Now()
	_ = os.Chtimes(filepath.Join(dir, strings.TrimPrefix(u1.Value, "upload:")), now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	_ = os.Chtimes(filepath.Join(dir, strings.TrimPrefix(u2.Value, "upload:")), now.Add(-1*time.Hour), now.Add(-1*time.Hour))

	var list []wpItem
	c.mustJSON("GET", "/api/wallpapers", "", &list)
	if len(list) != 2 || list[0].Value != u2.Value || list[1].Value != u1.Value {
		t.Fatalf("list (newest first): %+v", list)
	}
	f := strings.TrimPrefix(u1.Value, "upload:")
	if list[1].URL != "/uploads/wallpapers/"+f || list[1].Size != int64(len(pngWith("one"))) {
		t.Fatalf("item: %+v", list[1])
	}
	if ts, err := time.Parse(time.RFC3339, list[1].CreatedAt); err != nil || now.Sub(ts) < time.Hour || now.Sub(ts) > 3*time.Hour {
		t.Fatalf("created_at %q (%v)", list[1].CreatedAt, err)
	}
	// the listed url actually serves the file
	resp, _ := c.c.Get(e.ts.URL + list[1].URL)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(got, pngWith("one")) {
		t.Fatalf("serve listed url: %d", resp.StatusCode)
	}

	// two workspaces use u1, one uses u2
	var ws2 store.Workspace
	c.mustJSON("POST", "/api/workspaces", `{"name":"Second","wallpaper":"`+u1.Value+`"}`, &ws2)
	c.mustJSON("PATCH", "/api/workspaces/"+e.ws, `{"wallpaper":"`+u1.Value+`"}`, nil)
	var ws3 store.Workspace
	c.mustJSON("POST", "/api/workspaces", `{"name":"Third","wallpaper":"`+u2.Value+`"}`, &ws3)

	// deletion: file gone, users reset, version bumped (=> SSE change)
	ch, cancel, ok := e.srv.broker.Subscribe()
	if !ok {
		t.Fatal("subscribe")
	}
	defer cancel()
	v0 := e.st.Version()
	if code, b := c.do("DELETE", "/api/wallpapers/"+f, ""); code != 200 || !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("delete: %d %s", code, b)
	}
	if e.st.Version() <= v0 {
		t.Fatal("version not bumped")
	}
	select {
	case msg := <-ch:
		if !strings.Contains(string(msg), "event: change") {
			t.Fatalf("unexpected event %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no SSE event after wallpaper delete")
	}
	if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
		t.Fatalf("file still there: %v", err)
	}
	wp := map[string]string{}
	for _, w := range e.st.Snapshot().Workspaces {
		wp[w.ID] = w.Wallpaper
	}
	if wp[e.ws] != "preset:aurora" || wp[ws2.ID] != "preset:aurora" || wp[ws3.ID] != u2.Value {
		t.Fatalf("workspace wallpapers after delete: %v", wp)
	}
	c.mustJSON("GET", "/api/wallpapers", "", &list)
	if len(list) != 1 || list[0].Value != u2.Value {
		t.Fatalf("list after delete: %+v", list)
	}
	if resp, _ := c.c.Get(e.ts.URL + "/uploads/wallpapers/" + f); resp.StatusCode != 404 {
		t.Fatalf("deleted file still served: %d", resp.StatusCode)
	}
	// deleting again -> 404
	if code, _ := c.do("DELETE", "/api/wallpapers/"+f, ""); code != 404 {
		t.Fatalf("second delete: %d", code)
	}
}

func TestWallpaperDeleteRejectsUnsafeNames(t *testing.T) {
	e := setup(t)
	c := e.admin()
	// a sentinel one level above the wallpaper dir must survive every attempt
	sentinel := filepath.Join(e.srv.cfg.DataDir, "uploads", "keep.png")
	_ = os.MkdirAll(filepath.Dir(sentinel), 0o755)
	_ = os.WriteFile(sentinel, []byte("keep"), 0o644)
	_ = os.WriteFile(filepath.Join(e.srv.cfg.DataDir, "genkan.json.bak"), []byte("keep"), 0o644)
	_ = os.MkdirAll(filepath.Join(e.srv.cfg.DataDir, "uploads", "wallpapers", "adir"), 0o755)

	for _, name := range []string{
		"..%2Fkeep.png", "..%2F..%2Fgenkan.json", "%2e%2e%2fkeep.png", "..", ".hidden", "a%2Fb.png",
		"a%5Cb.png", "%2Fetc%2Fpasswd", "x%00.png", "sp ace.png", strings.Repeat("a", 100) + ".png",
	} {
		code, b := c.do("DELETE", "/api/wallpapers/"+name, "")
		if code != 400 && code != 404 && code != 301 && code != 307 && code != 308 {
			t.Errorf("DELETE %s -> %d %s (want 4xx)", name, code, b)
		}
		if code == 200 {
			t.Errorf("DELETE %s succeeded", name)
		}
	}
	// a directory with a safe name is not a wallpaper
	if code, _ := c.do("DELETE", "/api/wallpapers/adir", ""); code != 404 {
		t.Errorf("directory delete: %d", code)
	}
	if code, _ := c.do("DELETE", "/api/wallpapers/..%2Fkeep.png", ""); code != 400 {
		t.Errorf("encoded traversal should be a 400, got %d", code)
	}
	for _, p := range []string{sentinel, filepath.Join(e.srv.cfg.DataDir, "genkan.json"), filepath.Join(e.srv.cfg.DataDir, "genkan.json.bak"), filepath.Join(e.srv.cfg.DataDir, "uploads", "wallpapers", "adir")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was damaged: %v", p, err)
		}
	}
}

func TestWallpaperEndpointsAdminOnly(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	_, b := admin.upload("/api/uploads/wallpaper", "w.png", pngWith("x"))
	var u struct{ Value string }
	_ = json.Unmarshal(b, &u)
	f := strings.TrimPrefix(u.Value, "upload:")
	admin.mustJSON("PATCH", "/api/settings", `{"guest_access":true}`, nil)
	guest := e.anon()
	if code, _ := guest.do("GET", "/api/wallpapers", ""); code != 403 {
		t.Errorf("guest list: %d", code)
	}
	if code, _ := guest.do("DELETE", "/api/wallpapers/"+f, ""); code != 403 {
		t.Errorf("guest delete: %d", code)
	}
	admin.mustJSON("PATCH", "/api/settings", `{"guest_access":false}`, nil)
	none := e.anon()
	if code, _ := none.do("GET", "/api/wallpapers", ""); code != 401 {
		t.Errorf("anon list: %d", code)
	}
	if code, _ := none.do("DELETE", "/api/wallpapers/"+f, ""); code != 401 {
		t.Errorf("anon delete: %d", code)
	}
	// cookie-authenticated DELETE needs the CSRF header
	noCSRF := &client{e: e, c: admin.c, csrf: false}
	if code, _ := noCSRF.do("DELETE", "/api/wallpapers/"+f, ""); code != 403 {
		t.Errorf("csrf: %d", code)
	}
	if _, err := os.Stat(filepath.Join(e.srv.cfg.DataDir, "uploads", "wallpapers", f)); err != nil {
		t.Fatalf("file removed by rejected requests: %v", err)
	}
	// bearer token works without the CSRF header
	tok := e.token()
	if code, _ := tok.do("DELETE", "/api/wallpapers/"+f, ""); code != 200 {
		t.Errorf("token delete: %d", code)
	}
}
