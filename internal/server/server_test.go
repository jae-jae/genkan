package server

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jae-jae/genkan/internal/favicon"
	"github.com/jae-jae/genkan/internal/health"
	"github.com/jae-jae/genkan/internal/store"
)

type env struct {
	t   *testing.T
	ts  *httptest.Server
	srv *Server
	st  *store.Store
	ws  string
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	web := fstest.MapFS{
		"index.html":      {Data: []byte("<!doctype html><title>x</title>" + strings.Repeat("<p>hello</p>", 100))},
		"assets/app-1.js": {Data: []byte(strings.Repeat("console.log(1);", 100))},
		"favicon.svg":     {Data: []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")},
	}
	hc := health.New(st, nil)
	srv := New(Config{Store: st, Health: hc, Favicon: favicon.NewFetcher(dir + "/uploads/icons"), DataDir: dir, Web: web, Version: "test"})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
		srv.Close()
		st.Close()
	})
	return &env{t: t, ts: ts, srv: srv, st: st, ws: st.Snapshot().Workspaces[0].ID}
}

type client struct {
	e    *env
	c    *http.Client
	hdr  map[string]string
	csrf bool
}

func (e *env) anon() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, c: &http.Client{Jar: jar, Timeout: 10 * time.Second}, csrf: true}
}

// admin returns a cookie-authenticated client (does setup on first call).
func (e *env) admin() *client {
	c := e.anon()
	if e.st.SetupRequired() {
		if code, _ := c.do("POST", "/api/auth/setup", `{"password":"secret1"}`); code != 200 {
			e.t.Fatalf("setup: %d", code)
		}
	} else if code, _ := c.do("POST", "/api/auth/login", `{"password":"secret1"}`); code != 200 {
		e.t.Fatalf("login: %d", code)
	}
	return c
}

func (e *env) token() *client {
	return e.tokenWith(e.st.APIToken())
}

func (e *env) tokenWith(plain string) *client {
	e.admin()
	c := e.anon()
	c.csrf = false
	c.hdr = map[string]string{"Authorization": "Bearer " + plain}
	return c
}

func (c *client) req(method, path string, body io.Reader, ctype string) (*http.Response, error) {
	req, _ := http.NewRequest(method, c.e.ts.URL+path, body)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if c.csrf {
		req.Header.Set("X-Requested-With", "genkan")
	}
	for k, v := range c.hdr {
		req.Header.Set(k, v)
	}
	return c.c.Do(req)
}

func (c *client) do(method, path, body string) (int, []byte) {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	resp, err := c.req(method, path, r, "application/json")
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (c *client) mustJSON(method, path, body string, out any) {
	c.e.t.Helper()
	code, b := c.do(method, path, body)
	if code != 200 {
		c.e.t.Fatalf("%s %s: %d %s", method, path, code, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			c.e.t.Fatal(err)
		}
	}
}

func TestSetupLoginLogout(t *testing.T) {
	e := setup(t)
	c := e.anon()
	var bs struct {
		Auth struct {
			SetupRequired bool   `json:"setup_required"`
			Role          string `json:"role"`
		}
		Workspaces    []store.Workspace
		Settings      map[string]any
		Version       int64
		ServerVersion string `json:"server_version"`
	}
	c.mustJSON("GET", "/api/bootstrap", "", &bs)
	if bs.ServerVersion != "" {
		t.Fatalf("server_version leaked before login: %q", bs.ServerVersion)
	}
	if !bs.Auth.SetupRequired || bs.Auth.Role != "none" || len(bs.Workspaces) != 0 || len(bs.Settings) != 1 {
		t.Fatalf("pre-setup bootstrap: %+v", bs)
	}
	if code, _ := c.do("POST", "/api/auth/login", `{"password":"x"}`); code != 409 {
		t.Fatalf("login before setup: %d", code)
	}
	if code, _ := c.do("POST", "/api/auth/setup", `{"password":"123"}`); code != 400 {
		t.Fatalf("short password: %d", code)
	}
	resp, _ := c.req("POST", "/api/auth/setup", strings.NewReader(`{"password":"secret1"}`), "application/json")
	ck := resp.Header.Get("Set-Cookie")
	for _, want := range []string{"genkan_session=", "HttpOnly", "SameSite=Lax", "Path=/", "Max-Age=2592000"} {
		if !strings.Contains(ck, want) {
			t.Errorf("cookie %q missing %s", ck, want)
		}
	}
	if strings.Contains(ck, "Secure") {
		t.Error("Secure on plain http")
	}
	resp.Body.Close()
	if code, _ := c.do("POST", "/api/auth/setup", `{"password":"another1"}`); code != 403 {
		t.Fatalf("second setup: %d", code)
	}
	c.mustJSON("GET", "/api/bootstrap", "", &bs)
	if bs.Auth.Role != "admin" || bs.Auth.SetupRequired || len(bs.Workspaces) != 1 {
		t.Fatalf("post-setup bootstrap: %+v", bs)
	}
	if bs.ServerVersion != "test" {
		t.Fatalf("server_version = %q, want test", bs.ServerVersion)
	}
	// logout clears; then wrong/right login
	c.mustJSON("POST", "/api/auth/logout", "", nil)
	c.mustJSON("GET", "/api/bootstrap", "", &bs)
	if bs.Auth.Role != "none" {
		t.Fatalf("role after logout: %s", bs.Auth.Role)
	}
	if code, _ := c.do("POST", "/api/auth/login", `{"password":"nope"}`); code != 401 {
		t.Fatalf("bad login: %d", code)
	}
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/auth/login", strings.NewReader(`{"password":"secret1"}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	r2, _ := http.DefaultClient.Do(req)
	if !strings.Contains(r2.Header.Get("Set-Cookie"), "Secure") {
		t.Error("expected Secure behind X-Forwarded-Proto: https")
	}
	r2.Body.Close()
}

func TestLoginRateLimit(t *testing.T) {
	e := setup(t)
	e.admin()
	c := e.anon()
	for i := 0; i < 5; i++ {
		if code, _ := c.do("POST", "/api/auth/login", `{"password":"wrong"}`); code != 401 {
			t.Fatalf("attempt %d: %d", i, code)
		}
	}
	resp, _ := c.req("POST", "/api/auth/login", strings.NewReader(`{"password":"secret1"}`), "application/json")
	defer resp.Body.Close()
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("expected 429 with Retry-After, got %d", resp.StatusCode)
	}
}

func TestCSRF(t *testing.T) {
	e := setup(t)
	c := e.admin()
	c.csrf = false
	if code, _ := c.do("POST", "/api/categories", `{"workspace_id":"`+e.ws+`","name":"x"}`); code != 403 {
		t.Fatalf("cookie POST without header: %d", code)
	}
	if code, _ := c.do("PATCH", "/api/settings", `{"show_clock":false}`); code != 403 {
		t.Fatalf("cookie PATCH without header: %d", code)
	}
	if code, _ := c.do("GET", "/api/export", ""); code != 200 {
		t.Fatalf("GET needs no header: %d", code)
	}
	var l store.Link
	c.csrf = true
	c.mustJSON("POST", "/api/links", `{"workspace_id":"`+e.ws+`","title":"a","url":"http://a","fetch_icon":false}`, &l)
	c.csrf = false
	if code, _ := c.do("POST", "/api/links/"+l.ID+"/click", ""); code != 204 {
		t.Fatalf("click must be CSRF-exempt: %d", code)
	}
	if got, _ := e.st.GetLink(l.ID); got.Clicks != 1 {
		t.Fatalf("clicks = %d", got.Clicks)
	}
	// Bearer tokens need no header
	tc := e.token()
	if code, _ := tc.do("POST", "/api/categories", `{"workspace_id":"`+e.ws+`","name":"viaToken"}`); code != 200 {
		t.Fatalf("token POST: %d", code)
	}
}

func TestPermissionMatrix(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	var l store.Link
	admin.mustJSON("POST", "/api/links", `{"workspace_id":"`+e.ws+`","title":"a","url":"http://a","fetch_icon":false}`, &l)
	none := e.anon()
	adminOnly := []struct{ m, p, b string }{
		{"POST", "/api/auth/password", `{"old_password":"secret1","new_password":"secret2"}`},
		{"PATCH", "/api/settings", `{}`},
		{"POST", "/api/workspaces", `{"name":"x"}`},
		{"PATCH", "/api/workspaces/" + e.ws, `{}`},
		{"DELETE", "/api/workspaces/" + e.ws, ``},
		{"POST", "/api/workspaces/reorder", `{"ids":[]}`},
		{"POST", "/api/categories", `{"workspace_id":"` + e.ws + `","name":"x"}`},
		{"POST", "/api/categories/reorder", `{"workspace_id":"` + e.ws + `","ids":[]}`},
		{"POST", "/api/links", `{}`},
		{"PATCH", "/api/links/" + l.ID, `{}`},
		{"DELETE", "/api/links/" + l.ID, ``},
		{"POST", "/api/links/reorder", `{}`},
		{"POST", "/api/links/" + l.ID + "/refresh-icon", ``},
		{"GET", "/api/export", ``},
		{"POST", "/api/import", `{}`},
		{"GET", "/api/token", ``},
		{"POST", "/api/token/regenerate", ``},
		{"GET", "/api/skill", ``},
		{"POST", "/api/uploads/wallpaper", ``},
		{"POST", "/api/uploads/icon", ``},
		{"GET", "/api/wallpapers", ``},
		{"DELETE", "/api/wallpapers/none.png", ``},
	}
	readable := []struct{ m, p string }{
		{"GET", "/api/health"}, {"GET", "/uploads/icons/none.png"}, {"POST", "/api/links/" + l.ID + "/click"},
	}
	check := func(c *client, role string, adminWant func(int) bool, readWant func(int) bool) {
		t.Helper()
		for _, r := range adminOnly {
			if code, _ := c.do(r.m, r.p, r.b); !adminWant(code) {
				t.Errorf("%s: %s %s -> %d", role, r.m, r.p, code)
			}
		}
		for _, r := range readable {
			if code, _ := c.do(r.m, r.p, ""); !readWant(code) {
				t.Errorf("%s: %s %s -> %d", role, r.m, r.p, code)
			}
		}
	}
	// guest access disabled: anonymous gets 401 everywhere
	check(none, "none", func(c int) bool { return c == 401 }, func(c int) bool { return c == 401 })
	if code, _ := none.do("GET", "/api/events", ""); code != 401 {
		t.Errorf("events anon: %d", code)
	}
	// guest enabled: reads OK, admin routes 403
	admin.mustJSON("PATCH", "/api/settings", `{"guest_access":true}`, nil)
	guest := e.anon()
	check(guest, "guest", func(c int) bool { return c == 403 }, func(c int) bool { return c == 200 || c == 204 || c == 404 })
	var bs struct {
		Auth  struct{ Role string }
		Links []store.Link
	}
	guest.mustJSON("GET", "/api/bootstrap", "", &bs)
	if bs.Auth.Role != "guest" || len(bs.Links) != 1 {
		t.Fatalf("guest bootstrap: %+v", bs)
	}
	// guest clicks are acknowledged but not counted
	before, _ := e.st.GetLink(l.ID)
	if code, _ := guest.do("POST", "/api/links/"+l.ID+"/click", ""); code != 204 {
		t.Fatalf("guest click: %d", code)
	}
	after, _ := e.st.GetLink(l.ID)
	if after.Clicks != before.Clicks {
		t.Fatal("guest click counted")
	}
	// invalid token is not a guest
	bad := e.anon()
	bad.hdr = map[string]string{"Authorization": "Bearer gk_nope"}
	if code, _ := bad.do("GET", "/api/health", ""); code != 401 {
		t.Fatalf("bad bearer: %d", code)
	}
	// admin routes work for admin
	if code, _ := admin.do("GET", "/api/token", ""); code != 200 {
		t.Fatalf("admin token: %d", code)
	}
}

func TestPasswordChangeInvalidatesOldSessions(t *testing.T) {
	e := setup(t)
	a := e.admin()
	b := e.anon()
	b.mustJSON("POST", "/api/auth/login", `{"password":"secret1"}`, nil)
	if code, _ := a.do("POST", "/api/auth/password", `{"old_password":"wrong","new_password":"newpass1"}`); code != 401 {
		t.Fatalf("wrong old password: %d", code)
	}
	if code, _ := a.do("POST", "/api/auth/password", `{"old_password":"secret1","new_password":"123"}`); code != 400 {
		t.Fatalf("short new password: %d", code)
	}
	a.mustJSON("POST", "/api/auth/password", `{"old_password":"secret1","new_password":"newpass1"}`, nil)
	if code, _ := a.do("GET", "/api/token", ""); code != 200 {
		t.Fatalf("current session must survive: %d", code)
	}
	if code, _ := b.do("GET", "/api/token", ""); code != 401 {
		t.Fatalf("other session must be invalidated: %d", code)
	}
	if code, _ := e.anon().do("POST", "/api/auth/login", `{"password":"newpass1"}`); code != 200 {
		t.Fatalf("new password login: %d", code)
	}
}

func TestCRUDFlowAndErrors(t *testing.T) {
	e := setup(t)
	c := e.admin()
	var ws store.Workspace
	c.mustJSON("POST", "/api/workspaces", `{"name":"Work","icon":"lucide:briefcase","wallpaper":"preset:ocean"}`, &ws)
	if ws.Wallpaper != "preset:ocean" {
		t.Fatalf("%+v", ws)
	}
	var cat store.Category
	c.mustJSON("POST", "/api/categories", `{"workspace_id":"`+ws.ID+`","name":"Dev"}`, &cat)
	var l store.Link
	c.mustJSON("POST", "/api/links", `{"workspace_id":"`+ws.ID+`","category_id":"`+cat.ID+`","title":"Gitea","url":"git.lan:3000","fetch_icon":false}`, &l)
	if l.URL != "http://git.lan:3000" {
		t.Fatalf("%+v", l)
	}
	c.mustJSON("PATCH", "/api/links/"+l.ID, `{"pinned":true,"description":"git"}`, &l)
	if !l.Pinned || l.Description != "git" {
		t.Fatalf("%+v", l)
	}
	for _, tc := range []struct {
		m, p, b string
		want    int
	}{
		{"POST", "/api/links", `{"workspace_id":"` + ws.ID + `","title":"x","url":"javascript:alert(1)"}`, 400},
		{"POST", "/api/links", `{"workspace_id":"` + ws.ID + `","title":" ","url":"http://x"}`, 400},
		{"POST", "/api/links", `{not json`, 400},
		{"POST", "/api/links", ``, 400},
		{"PATCH", "/api/links/nope", `{}`, 404},
		{"DELETE", "/api/links/nope", ``, 404},
		{"PATCH", "/api/workspaces/nope", `{}`, 404},
		{"POST", "/api/categories", `{"workspace_id":"nope","name":"x"}`, 404},
		{"POST", "/api/workspaces", `{"name":"x","wallpaper":"preset:bogus"}`, 400},
		{"GET", "/api/nothing-here", ``, 404},
		{"POST", "/api/links", `{"title":"` + strings.Repeat("x", 2<<20) + `"}`, 413},
	} {
		code, body := c.do(tc.m, tc.p, tc.b)
		if code != tc.want {
			t.Errorf("%s %s -> %d (%s), want %d", tc.m, tc.p, code, body, tc.want)
		}
		if code >= 400 && !strings.Contains(string(body), `"error"`) {
			t.Errorf("%s %s: error body not JSON error: %s", tc.m, tc.p, body)
		}
	}
	// delete the only other workspace OK, then default is the last => 409
	c.mustJSON("DELETE", "/api/workspaces/"+e.ws, "", nil)
	if code, _ := c.do("DELETE", "/api/workspaces/"+ws.ID, ""); code != 409 {
		t.Fatalf("last workspace: %d", code)
	}
	// reorder + category delete default behaviour
	c.mustJSON("POST", "/api/links/reorder", `{"workspace_id":"`+ws.ID+`","category_id":"__pinned","ids":["`+l.ID+`"]}`, nil)
	c.mustJSON("DELETE", "/api/categories/"+cat.ID, "", nil)
	got, _ := e.st.GetLink(l.ID)
	if got.CategoryID != "" {
		t.Fatal("link should become uncategorized")
	}
}

func TestExportImportOverHTTP(t *testing.T) {
	e := setup(t)
	c := e.admin()
	c.mustJSON("POST", "/api/links", `{"workspace_id":"`+e.ws+`","title":"a","url":"http://a","fetch_icon":false}`, nil)
	resp, _ := c.req("GET", "/api/export", nil, "")
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") || bytes.Contains(raw, []byte("password_hash")) || bytes.Contains(raw, []byte("session_secret")) {
		t.Fatalf("export headers/leak: %v", resp.Header)
	}
	var out struct{ Workspaces, Links int }
	c.mustJSON("POST", "/api/import", `{"mode":"merge","data":`+string(raw)+`}`, &out)
	if out.Workspaces != 1 || out.Links != 1 || len(e.st.Snapshot().Links) != 2 {
		t.Fatalf("merge: %+v", out)
	}
	c.mustJSON("POST", "/api/import", `{"mode":"replace","data":`+string(raw)+`}`, &out)
	if len(e.st.Snapshot().Links) != 1 {
		t.Fatal("replace")
	}
	if code, _ := c.do("POST", "/api/import", `{"mode":"replace","data":{"workspaces":[]}}`); code != 400 {
		t.Fatalf("empty replace: %d", code)
	}
}

func readEvent(t *testing.T, r *bufio.Reader, want string) string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	res := make(chan string, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				res <- "ERR " + err.Error()
				return
			}
			if strings.HasPrefix(line, "event: "+want) {
				data, _ := r.ReadString('\n')
				res <- strings.TrimSpace(data)
				return
			}
		}
	}()
	select {
	case s := <-res:
		return s
	case <-deadline:
		t.Fatalf("timeout waiting for %s event", want)
		return ""
	}
}

func TestSSEChangeEventsAndDisconnect(t *testing.T) {
	e := setup(t)
	c := e.admin()
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/events", nil)
	resp, err := c.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content-type %q", resp.Header.Get("Content-Type"))
	}
	r := bufio.NewReader(resp.Body)
	c.mustJSON("POST", "/api/workspaces", `{"name":"live"}`, nil)
	data := readEvent(t, r, "change")
	var ev struct{ Version int64 }
	if err := json.Unmarshal([]byte(strings.TrimPrefix(data, "data: ")), &ev); err != nil || ev.Version != e.st.Version() {
		t.Fatalf("event %q (version %d)", data, e.st.Version())
	}
	// health events are pushed too
	e.srv.PublishHealth(map[string]health.Result{"abc": {Status: "up", Code: 200}})
	if d := readEvent(t, r, "health"); !strings.Contains(d, `"abc"`) {
		t.Fatalf("health event %q", d)
	}
	if e.srv.broker.Clients() != 1 {
		t.Fatalf("clients = %d", e.srv.broker.Clients())
	}
	resp.Body.Close() // client disconnects
	waitFor(t, func() bool { return e.srv.broker.Clients() == 0 })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if cond() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func TestSSEReconnectCatchUpAndShutdown(t *testing.T) {
	e := setup(t)
	c := e.admin()
	e.st.CreateWorkspace(store.WorkspaceInput{Name: "x"})
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/events", nil)
	req.Header.Set("Last-Event-ID", "1")
	resp, err := c.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)
	if d := readEvent(t, r, "change"); !strings.Contains(d, "version") {
		t.Fatalf("catch-up event: %q", d)
	}
	// server shutdown ends the stream
	e.srv.Broker().Close()
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, resp.Body); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not end on broker close")
	}
}

func TestSSEClientLimit(t *testing.T) {
	e := setup(t)
	c := e.admin()
	var bodies []io.Closer
	defer func() {
		for _, b := range bodies {
			b.Close()
		}
	}()
	for i := 0; i < 64; i++ {
		resp, err := c.c.Get(e.ts.URL + "/api/events")
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("client %d: %v", i, err)
		}
		bodies = append(bodies, resp.Body)
	}
	resp, err := c.c.Get(e.ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("65th client: %d", resp.StatusCode)
	}
}

func multipartBody(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="`+field+`"; filename="`+filename+`"`)
	h.Set("Content-Type", "application/octet-stream") // lies are irrelevant: content is sniffed
	w, _ := mw.CreatePart(h)
	w.Write(data)
	mw.Close()
	return &buf, mw.FormDataContentType()
}

var tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")

func TestUploads(t *testing.T) {
	e := setup(t)
	c := e.admin()
	up := func(path, name string, data []byte) (int, []byte) {
		body, ct := multipartBody(t, "file", name, data)
		resp, err := c.req("POST", path, body, ct)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	code, b := up("/api/uploads/wallpaper", "evil.html", tinyPNG)
	var r struct{ Value, URL string }
	_ = json.Unmarshal(b, &r)
	if code != 200 || !strings.HasPrefix(r.Value, "upload:") || !strings.HasSuffix(r.Value, ".png") || !strings.HasPrefix(r.URL, "/uploads/wallpapers/") {
		t.Fatalf("wallpaper upload: %d %s", code, b)
	}
	if len(strings.TrimSuffix(strings.TrimPrefix(r.Value, "upload:"), ".png")) != 16 {
		t.Fatalf("name not sha-16: %s", r.Value)
	}
	// fetch it back (cookie auth), with security headers
	resp, _ := c.c.Get(e.ts.URL + r.URL)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(got, tinyPNG) || resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("serve upload: %d %v", resp.StatusCode, resp.Header)
	}
	// anonymous cannot read while guest access is off
	if resp, _ := http.Get(e.ts.URL + r.URL); resp.StatusCode != 401 {
		t.Fatalf("anonymous upload read: %d", resp.StatusCode)
	}
	// content sniffing, not names
	if code, _ := up("/api/uploads/icon", "x.png", []byte("<html><script>alert(1)</script></html>")); code != 400 {
		t.Fatalf("html as png: %d", code)
	}
	if code, _ := up("/api/uploads/wallpaper", "x.svg", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")); code != 400 {
		t.Fatalf("svg wallpaper must be rejected: %d", code)
	}
	code, b = up("/api/uploads/icon", "x.bin", []byte("<svg xmlns='http://www.w3.org/2000/svg'><script>1</script></svg>"))
	_ = json.Unmarshal(b, &r)
	if code != 200 || !strings.HasSuffix(r.Value, ".svg") {
		t.Fatalf("svg icon: %d %s", code, b)
	}
	resp, _ = c.c.Get(e.ts.URL + r.URL)
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") || resp.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("svg headers: %v", resp.Header)
	}
	// size limits
	big := append(append([]byte{}, tinyPNG...), make([]byte, 1<<20)...)
	if code, _ := up("/api/uploads/icon", "big.png", big); code != 413 {
		t.Fatalf("oversized icon: %d", code)
	}
	if code, _ := up("/api/uploads/wallpaper", "big.png", big); code != 200 {
		t.Fatalf("1MB wallpaper should pass: %d", code)
	}
	// non-multipart
	if code, _ := c.do("POST", "/api/uploads/icon", `{}`); code != 400 {
		t.Fatalf("non-multipart: %d", code)
	}
	// path traversal
	for _, p := range []string{"/uploads/icons/..%2Fgenkan.json", "/uploads/icons/../genkan.json", "/uploads/other/x.png", "/uploads/icons/.hidden"} {
		resp, _ := c.c.Get(e.ts.URL + p)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			t.Errorf("%s served", p)
		}
	}
	// guest can read once enabled
	c.mustJSON("PATCH", "/api/settings", `{"guest_access":true}`, nil)
	resp, _ = http.Get(e.ts.URL + "/uploads/wallpapers/" + strings.TrimPrefix(r.Value, "upload:"))
	resp.Body.Close()
	resp2, _ := http.Get(e.ts.URL + "/uploads/icons/" + strings.TrimPrefix(r.Value, "upload:"))
	resp2.Body.Close()
	if resp2.StatusCode != 200 || !strings.Contains(resp2.Header.Get("Cache-Control"), "max-age=86400") {
		t.Fatalf("guest upload read: %d %v", resp2.StatusCode, resp2.Header)
	}
}

func TestStatic(t *testing.T) {
	e := setup(t)
	get := func(path string, hdr map[string]string) *http.Response {
		req, _ := http.NewRequest("GET", e.ts.URL+path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		tr := &http.Transport{DisableCompression: true}
		resp, err := (&http.Client{Transport: tr}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := get("/assets/app-1.js", map[string]string{"Accept-Encoding": "gzip"})
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if resp.Header.Get("Content-Encoding") != "gzip" || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") ||
		!strings.Contains(resp.Header.Get("Content-Type"), "javascript") || !strings.HasPrefix(string(plain), "console.log") {
		t.Fatalf("asset: %v", resp.Header)
	}
	resp = get("/assets/app-1.js", nil)
	raw, _ := io.ReadAll(resp.Body)
	if resp.Header.Get("Content-Encoding") != "" || len(raw) != len(plain) {
		t.Fatal("identity encoding expected without Accept-Encoding")
	}
	etag := resp.Header.Get("ETag")
	if resp = get("/assets/app-1.js", map[string]string{"If-None-Match": etag}); resp.StatusCode != 304 {
		t.Fatalf("etag revalidation: %d", resp.StatusCode)
	}
	resp = get("/", nil)
	if resp.Header.Get("Cache-Control") != "no-cache" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("index: %v", resp.Header)
	}
	resp = get("/some/spa/route", nil)
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "<title>x</title>") {
		t.Fatalf("spa fallback: %d", resp.StatusCode)
	}
	if resp = get("/assets/missing.js", nil); resp.StatusCode != 404 {
		t.Fatalf("missing asset: %d", resp.StatusCode)
	}
	if resp = get("/api/unknown", nil); resp.StatusCode != 404 || !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		t.Fatalf("unknown api: %d %v", resp.StatusCode, resp.Header)
	}
	if resp = get("/healthz", nil); resp.StatusCode != 200 {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}
	if resp = get("/favicon.svg", nil); resp.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("svg type: %v", resp.Header)
	}
}

func TestMCPEndpoint(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	// cookie auth is not enough
	if code, _ := admin.do("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); code != 401 {
		t.Fatalf("cookie on /mcp: %d", code)
	}
	tc := e.token()
	if code, _ := tc.do("GET", "/mcp", ""); code != 405 {
		t.Fatalf("GET /mcp: %d", code)
	}
	if code, _ := e.anon().do("POST", "/mcp", `{}`); code != 401 {
		t.Fatalf("anon /mcp: %d", code)
	}
	code, b := tc.do("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != 200 || !bytes.Contains(b, []byte("create_links")) {
		t.Fatalf("tools/list: %d %s", code, b)
	}
	if code, _ := tc.do("POST", "/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`); code != 202 {
		t.Fatalf("notification: %d", code)
	}
	// mutation via MCP is visible in REST and broadcast over SSE
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/events", nil)
	sseResp, err := admin.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sseResp.Body.Close()
	r := bufio.NewReader(sseResp.Body)
	call := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_links","arguments":{"links":[{"workspace_id":"` + e.ws + `","title":"Grafana","url":"http://127.0.0.1:1","icon":"emoji:📈"}]}}}`
	code, b = tc.do("POST", "/mcp", call)
	if code != 200 || !bytes.Contains(b, []byte(`created_count`)) {
		t.Fatalf("create_links: %d %s", code, b)
	}
	readEvent(t, r, "change")
	var bs struct{ Links []store.Link }
	admin.mustJSON("GET", "/api/bootstrap", "", &bs)
	if len(bs.Links) != 1 || bs.Links[0].Title != "Grafana" {
		t.Fatalf("bootstrap after MCP: %+v", bs.Links)
	}
	// regenerating invalidates the old token immediately
	old := e.st.APIToken()
	var nt struct{ Token string }
	admin.mustJSON("POST", "/api/token/regenerate", "", &nt)
	if nt.Token == "" || nt.Token == old {
		t.Fatalf("regenerate returned %q (old %q)", nt.Token, old)
	}
	if code, _ := tc.do("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); code != 401 {
		t.Fatalf("old token after regenerate: %d", code)
	}
	if code, _ := e.tokenWith(nt.Token).do("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); code != 200 {
		t.Fatalf("new token: %d", code)
	}
}

func TestAPITokenEndpoints(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	var got struct{ Token string }
	admin.mustJSON("GET", "/api/token", "", &got)
	if got.Token != e.st.APIToken() || !strings.HasPrefix(got.Token, "gk_") || len(got.Token) != 35 {
		t.Fatalf("GET /api/token: %q", got.Token)
	}
	// the token authenticates REST as admin, with no CSRF header
	bearer := e.tokenWith(got.Token)
	if code, _ := bearer.do("POST", "/api/categories", `{"workspace_id":"`+e.ws+`","name":"viaToken"}`); code != 200 {
		t.Fatalf("bearer REST: %d", code)
	}
	if code, _ := bearer.do("GET", "/api/token", ""); code != 200 {
		t.Fatalf("bearer GET /api/token: %d", code)
	}
	// wrong tokens, including prefixes and extensions of the real one
	for _, bad := range []string{"gk_nope", got.Token[:10], got.Token + "x", ""} {
		c := e.tokenWith(bad)
		if code, _ := c.do("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); code != 401 {
			t.Errorf("bad token %q on /mcp: %d", bad, code)
		}
	}
	// regenerate requires the CSRF header for cookie auth
	admin.csrf = false
	if code, _ := admin.do("POST", "/api/token/regenerate", ""); code != 403 {
		t.Fatalf("regenerate without CSRF header: %d", code)
	}
	admin.csrf = true
	// old token dies, new one persists
	var re struct{ Token string }
	admin.mustJSON("POST", "/api/token/regenerate", "", &re)
	if re.Token == got.Token || e.st.APIToken() != re.Token {
		t.Fatalf("regenerate: %q vs %q / %q", re.Token, got.Token, e.st.APIToken())
	}
	if code, _ := bearer.do("GET", "/api/token", ""); code != 401 {
		t.Fatalf("old bearer after regenerate: %d", code)
	}
	var again struct{ Token string }
	admin.mustJSON("GET", "/api/token", "", &again)
	if again.Token != re.Token {
		t.Fatal("GET after regenerate returns a different token")
	}
	// removed multi-token endpoints
	for _, r := range [][2]string{{"GET", "/api/tokens"}, {"POST", "/api/tokens"}, {"DELETE", "/api/tokens/abc"}} {
		if code, _ := admin.do(r[0], r[1], `{}`); code != 404 && code != 405 {
			t.Errorf("%s %s should be gone, got %d", r[0], r[1], code)
		}
	}
	// the token never leaks through export or bootstrap
	for _, p := range []string{"/api/export", "/api/bootstrap"} {
		if _, b := admin.do("GET", p, ""); bytes.Contains(b, []byte(re.Token)) {
			t.Errorf("%s leaks the API token", p)
		}
	}
}

func TestSkillDownload(t *testing.T) {
	e := setup(t)
	admin := e.admin()
	tok := e.st.APIToken()
	get := func(hdr map[string]string) []byte {
		t.Helper()
		req, _ := http.NewRequest("GET", e.ts.URL+"/api/skill", nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := admin.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/zip" ||
			!strings.Contains(resp.Header.Get("Content-Disposition"), "genkan-skill.zip") {
			t.Fatalf("skill: %d %v", resp.StatusCode, resp.Header)
		}
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil || len(zr.File) != 1 || zr.File[0].Name != "genkan/SKILL.md" {
			t.Fatalf("zip layout: %v", err)
		}
		rc, _ := zr.File[0].Open()
		defer rc.Close()
		md, _ := io.ReadAll(rc)
		return md
	}
	md := string(get(nil))
	if !strings.HasPrefix(md, "---\nname: genkan\n") {
		t.Fatalf("frontmatter missing: %.80q", md)
	}
	if !strings.Contains(md, tok) || !strings.Contains(md, e.ts.URL+"/mcp") {
		t.Fatalf("token or URL missing")
	}
	if strings.Contains(md, "{{") || strings.Contains(md, "}}") {
		t.Fatal("unreplaced placeholder")
	}
	if !strings.Contains(md, `claude mcp add --transport http genkan `+e.ts.URL+`/mcp --header "Authorization: Bearer `+tok+`"`) {
		t.Fatal("ready-to-use mcp command missing")
	}
	// reverse proxy headers decide the instance URL
	md = string(get(map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "nav.example.com"}))
	if !strings.Contains(md, "https://nav.example.com/mcp") || strings.Contains(md, e.ts.URL) {
		t.Fatal("forwarded URL not used")
	}
	md = string(get(map[string]string{"X-Forwarded-Host": "evil.com/\"x y"}))
	if strings.Contains(md, "evil.com") {
		t.Fatal("unsafe forwarded host accepted")
	}
	// after regenerate the download carries the new token
	var nt struct{ Token string }
	admin.mustJSON("POST", "/api/token/regenerate", "", &nt)
	md = string(get(nil))
	if !strings.Contains(md, nt.Token) || strings.Contains(md, tok) {
		t.Fatal("skill not updated after regenerate")
	}
}

func TestIconFetchOnCreateIsAsyncAndNotifies(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<link rel="icon" href="/i.png">`))
		case "/i.png":
			w.Write(tinyPNG)
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()
	e := setup(t)
	c := e.admin()
	var l store.Link
	c.mustJSON("POST", "/api/links", `{"workspace_id":"`+e.ws+`","title":"s","url":"`+site.URL+`"}`, &l)
	if l.Icon != "" {
		t.Fatalf("create must return before the icon is fetched, got %q", l.Icon)
	}
	waitFor(t, func() bool { got, _ := e.st.GetLink(l.ID); return strings.HasPrefix(got.Icon, "favicon:") })
	// refresh-icon is synchronous and returns the Link
	var l2 store.Link
	c.mustJSON("POST", "/api/links/"+l.ID+"/refresh-icon", "", &l2)
	if !strings.HasPrefix(l2.Icon, "favicon:") {
		t.Fatalf("refresh: %+v", l2)
	}
	// fetch_icon:false is honored
	var l3 store.Link
	c.mustJSON("POST", "/api/links", `{"workspace_id":"`+e.ws+`","title":"s","url":"`+site.URL+`","fetch_icon":false}`, &l3)
	time.Sleep(300 * time.Millisecond)
	if got, _ := e.st.GetLink(l3.ID); got.Icon != "" {
		t.Fatalf("fetch_icon=false ignored: %q", got.Icon)
	}
	// unreachable site => 502 on refresh
	c.mustJSON("PATCH", "/api/links/"+l3.ID, `{"url":"http://127.0.0.1:1"}`, nil)
	if code, _ := c.do("POST", "/api/links/"+l3.ID+"/refresh-icon", ""); code != 502 {
		t.Fatalf("unreachable refresh: %d", code)
	}
}

func TestBootstrapGzipForLargePayload(t *testing.T) {
	e := setup(t)
	c := e.admin()
	for i := 0; i < 30; i++ {
		c.mustJSON("POST", "/api/links", `{"workspace_id":"`+e.ws+`","title":"link `+strings.Repeat("x", 20)+`","url":"http://example.lan/`+strings.Repeat("p", 30)+`","fetch_icon":false}`, nil)
	}
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/bootstrap", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}, Jar: c.c.Jar}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip: %v", resp.Header)
	}
	zr, _ := gzip.NewReader(resp.Body)
	var out struct{ Links []store.Link }
	if err := json.NewDecoder(zr).Decode(&out); err != nil || len(out.Links) != 30 {
		t.Fatalf("decode gz: %v %d", err, len(out.Links))
	}
}
