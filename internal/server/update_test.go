package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jae-jae/genkan/internal/store"
	"github.com/jae-jae/genkan/internal/update"
)

// updEnv is a server wired to a fake GitHub that publishes v1.1.0 (signed).
type updEnv struct {
	*env
	gh      *httptest.Server
	svc     *update.Service
	release chan struct{} // closed to let the "new binary" report its version
}

func newUpdEnv(t *testing.T, version string, mod func(*update.Config)) *updEnv {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\necho genkan v1.1.0\n"
	tw.WriteHeader(&tar.Header{Name: "genkan", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write([]byte(body))
	tw.Close()
	gz.Close()
	name := "genkan_1.1.0_linux_amd64.tar.gz"
	h := sha256.Sum256(tarBuf.Bytes())
	sums := []byte(hex.EncodeToString(h[:]) + "  " + name + "\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)) + "\n")

	var gh *httptest.Server
	gh = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/releases":
			dl := func(n string) string { return gh.URL + "/dl/" + n }
			json.NewEncoder(w).Encode([]map[string]any{
				{"tag_name": "v1.1.0", "name": "v1.1.0", "body": "* faster", "published_at": "2025-11-04T10:00:00Z", "html_url": "https://github.com/jae-jae/genkan/releases/tag/v1.1.0",
					"assets": []map[string]any{
						{"name": name, "size": tarBuf.Len(), "browser_download_url": dl(name)},
						{"name": "checksums.txt", "size": len(sums), "browser_download_url": dl("checksums.txt")},
						{"name": "checksums.txt.sig", "size": len(sig), "browser_download_url": dl("checksums.txt.sig")},
					}},
				{"tag_name": "v1.2.0-rc1", "prerelease": true},
			})
		case r.URL.Path == "/dl/"+name:
			w.Write(tarBuf.Bytes())
		case r.URL.Path == "/dl/checksums.txt":
			w.Write(sums)
		case r.URL.Path == "/dl/checksums.txt.sig":
			w.Write(sig)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(gh.Close)

	u := &updEnv{gh: gh, release: make(chan struct{})}
	var execCalls int
	e := setupCfg(t, func(c *Config) {
		cfg := update.Config{
			DataDir: c.DataDir, Version: version, API: gh.URL, HTTPClient: gh.Client(), PubKey: pub,
			CheckEnabled: func() bool { return c.Store.Settings().UpdateCheck },
			Dismissed:    func() string { return c.Store.Settings().UpdateDismissed },
			Getenv:       func(string) string { return "" },
			GOOS:         "linux", GOARCH: "amd64", NoExec: func(string) bool { return false },
			Exec: func(string, []string, []string) error { execCalls++; return nil },
			RunVersion: func(ctx context.Context, bin string) (string, error) {
				select {
				case <-u.release:
					return "genkan v1.1.0", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			},
			Logf: func(string, ...any) {},
		}
		if mod != nil {
			mod(&cfg)
		}
		u.svc = update.New(cfg)
		c.Updater = u.svc
		c.Version = version
	})
	t.Cleanup(u.svc.Close)
	u.env = e
	return u
}

func TestUpdateAuth(t *testing.T) {
	u := newUpdEnv(t, "v1.0.0", nil)
	anon := u.anon()
	for _, ep := range [][2]string{{"GET", "/api/update"}, {"POST", "/api/update/check"}, {"POST", "/api/update/apply"}, {"POST", "/api/update/dismiss"}} {
		if code, _ := anon.do(ep[0], ep[1], `{}`); code != 401 {
			t.Errorf("anonymous %s %s: %d", ep[0], ep[1], code)
		}
	}
	admin := u.admin()
	u.st.UpdateSettings(store.SettingsPatch{GuestAccess: ptr(true)})
	guest := u.anon()
	for _, ep := range [][2]string{{"GET", "/api/update"}, {"POST", "/api/update/check"}, {"POST", "/api/update/apply"}, {"POST", "/api/update/dismiss"}} {
		if code, _ := guest.do(ep[0], ep[1], `{"version":"v1.1.0"}`); code != 403 {
			t.Errorf("guest %s %s: %d", ep[0], ep[1], code)
		}
	}
	// the cookie session needs the CSRF header on POST, not on GET
	admin.csrf = false
	for _, p := range []string{"/api/update/check", "/api/update/apply", "/api/update/dismiss"} {
		if code, _ := admin.do("POST", p, `{"version":"v1.1.0"}`); code != 403 {
			t.Errorf("no CSRF header %s: %d", p, code)
		}
	}
	if code, _ := admin.do("GET", "/api/update", ""); code != 200 {
		t.Errorf("GET without header: %d", code)
	}
	// a bearer token needs no CSRF header
	tok := u.token()
	if code, _ := tok.do("POST", "/api/update/check", ""); code != 200 {
		t.Errorf("token check: %d", code)
	}
}

func TestUpdateStatusShape(t *testing.T) {
	u := newUpdEnv(t, "v1.0.0", nil)
	c := u.admin()
	code, b := c.do("GET", "/api/update", "")
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	var m map[string]json.RawMessage
	json.Unmarshal(b, &m)
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	want := "apply available checked_at checking check_enabled current dev dismissed env_disabled error job latest releases repo rolled_back"
	for _, k := range strings.Fields(want) {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q", k)
		}
	}
	if len(m) != len(strings.Fields(want)) {
		t.Errorf("keys: %v", keys)
	}
	if string(m["releases"]) != "[]" || string(m["job"]) != `{"phase":"idle","version":"","progress":0,"error":""}` || string(m["apply"]) != `{"supported":true,"reason":""}` {
		t.Errorf("releases=%s job=%s apply=%s", m["releases"], m["job"], m["apply"])
	}
	if string(m["repo"]) != `"https://github.com/jae-jae/genkan"` || string(m["checked_at"]) != "0" || string(m["check_enabled"]) != "true" {
		t.Errorf("repo=%s checked_at=%s", m["repo"], m["checked_at"])
	}

	var st update.Status
	c.mustJSON("POST", "/api/update/check", "", &st)
	if st.Latest != "v1.1.0" || !st.Available || len(st.Releases) != 1 || st.Releases[0].Notes != "* faster" || st.CheckedAt == 0 || st.Error != "" {
		t.Fatalf("after check: %+v", st)
	}
	c.mustJSON("GET", "/api/update", "", &st)
	if st.Latest != "v1.1.0" {
		t.Fatalf("GET after check: %+v", st)
	}
}

func TestUpdateCheckManualWithSettingOffAndDev(t *testing.T) {
	u := newUpdEnv(t, "v1.0.0", nil)
	c := u.admin()
	c.mustJSON("PATCH", "/api/settings", `{"update_check":false}`, nil)
	var st update.Status
	c.mustJSON("POST", "/api/update/check", "", &st)
	if st.CheckEnabled || st.Latest != "v1.1.0" {
		t.Fatalf("manual check with the setting off: %+v", st)
	}

	d := newUpdEnv(t, "dev", nil)
	dc := d.admin()
	code, b := dc.do("POST", "/api/update/check", "")
	if code != 400 || !strings.Contains(string(b), "development") {
		t.Fatalf("dev check: %d %s", code, b)
	}
	dc.mustJSON("GET", "/api/update", "", &st)
	if !st.Dev || st.Apply.Supported || st.Apply.Reason != "dev" || st.Releases == nil {
		t.Fatalf("dev status: %+v", st)
	}

	env := newUpdEnv(t, "v1.0.0", func(c *update.Config) {
		c.Getenv = func(k string) string {
			if k == update.EnvCheck {
				return "off"
			}
			return ""
		}
	})
	ec := env.admin()
	if code, b := ec.do("POST", "/api/update/check", ""); code != 400 || !strings.Contains(string(b), "GENKAN_UPDATE_CHECK") {
		t.Fatalf("env check: %d %s", code, b)
	}
}

func TestUpdateApply(t *testing.T) {
	u := newUpdEnv(t, "v1.0.0", nil)
	c := u.admin()
	for body, want := range map[string]int{
		``:                     400,
		`{`:                    400,
		`{}`:                   400,
		`{"version":"latest"}`: 400,
		`{"version":"v9.9.9"}`: 400, // valid but not in the list (nothing checked yet)
		`{"version":"v1.1.0"}`: 400, // not checked yet
	} {
		if code, b := c.do("POST", "/api/update/apply", body); code != want {
			t.Errorf("apply %q: %d %s", body, code, b)
		}
	}
	c.mustJSON("POST", "/api/update/check", "", nil)
	if code, _ := c.do("POST", "/api/update/apply", `{"version":"v1.0.0"}`); code != 400 {
		t.Errorf("current version: %d", code)
	}
	code, b := c.do("POST", "/api/update/apply", `{"version":"v1.1.0"}`)
	if code != 202 {
		t.Fatalf("apply: %d %s", code, b)
	}
	var st update.Status
	json.Unmarshal(b, &st)
	if st.Job.Version != "v1.1.0" || st.Job.Phase == "idle" || st.Job.Phase == "failed" {
		t.Fatalf("job in 202: %+v", st.Job)
	}
	// already running (the version check is held back)
	waitUntil(t, func() bool { c.mustJSON("GET", "/api/update", "", &st); return st.Job.Phase == "installing" })
	if code, _ := c.do("POST", "/api/update/apply", `{"version":"v1.1.0"}`); code != 409 {
		t.Fatalf("second apply: %d", code)
	}
	close(u.release)
	waitUntil(t, func() bool { c.mustJSON("GET", "/api/update", "", &st); return st.Job.Phase == "restarting" })
	select {
	case <-u.svc.RestartRequested():
	case <-time.After(time.Second):
		t.Fatal("no restart request")
	}
	if _, err := os.Stat(filepath.Join(u.srv.cfg.DataDir, ".update", "state.json")); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateApplyUnsupported(t *testing.T) {
	cases := map[string]struct {
		mod  func(*update.Config)
		want string
	}{
		"platform": {func(c *update.Config) { c.GOOS = "darwin" }, "linux"},
		"noexec":   {func(c *update.Config) { c.NoExec = func(string) bool { return true } }, "noexec"},
		"disabled": {func(c *update.Config) {
			c.Getenv = func(k string) string {
				if k == update.EnvSelfUpdate {
					return "off"
				}
				return ""
			}
		}, "GENKAN_SELF_UPDATE"},
	}
	for reason, tc := range cases {
		u := newUpdEnv(t, "v1.0.0", tc.mod)
		c := u.admin()
		c.mustJSON("POST", "/api/update/check", "", nil)
		code, b := c.do("POST", "/api/update/apply", `{"version":"v1.1.0"}`)
		if code != 400 || !strings.Contains(string(b), tc.want) {
			t.Errorf("%s: %d %s", reason, code, b)
		}
		var st update.Status
		c.mustJSON("GET", "/api/update", "", &st)
		if st.Apply.Supported || st.Apply.Reason != reason || st.Job.Phase != "idle" {
			t.Errorf("%s: %+v", reason, st)
		}
	}
}

func TestUpdateDismiss(t *testing.T) {
	u := newUpdEnv(t, "v1.0.0", nil)
	c := u.admin()
	c.mustJSON("POST", "/api/update/check", "", nil)
	var st update.Status
	c.mustJSON("POST", "/api/update/dismiss", `{"version":"v1.1.0"}`, &st)
	if st.Dismissed != "v1.1.0" || st.Latest != "v1.1.0" || !st.Available {
		t.Fatalf("%+v", st)
	}
	if u.st.Settings().UpdateDismissed != "v1.1.0" {
		t.Fatal("not stored in settings")
	}
	var bs struct{ Settings store.Settings }
	c.mustJSON("GET", "/api/bootstrap", "", &bs)
	if bs.Settings.UpdateDismissed != "v1.1.0" || !bs.Settings.UpdateCheck {
		t.Fatalf("bootstrap settings: %+v", bs.Settings)
	}
	for _, body := range []string{``, `{}`, `{"version":"nope"}`, `{"version":"v1.1.0-rc1"}`, `{"version":"v1.1.0\n"}`} {
		if code, _ := c.do("POST", "/api/update/dismiss", body); code != 400 {
			t.Errorf("dismiss %q: %d", body, code)
		}
	}
	// update_dismissed is not settable through the settings API
	c.mustJSON("PATCH", "/api/settings", `{"update_dismissed":"v7.7.7"}`, nil)
	if u.st.Settings().UpdateDismissed != "v1.1.0" {
		t.Fatal("PATCH /api/settings changed update_dismissed")
	}

	// the rolled-back notice is cleared on request
	os.MkdirAll(filepath.Join(u.srv.cfg.DataDir, ".update"), 0o755)
	os.WriteFile(filepath.Join(u.srv.cfg.DataDir, ".update", "rolled_back.json"), []byte(`{"version":"v1.0.5","at":1}`), 0o600)
	c.mustJSON("GET", "/api/update", "", &st)
	if st.RolledBack != "v1.0.5" {
		t.Fatalf("rolled_back: %+v", st)
	}
	c.mustJSON("POST", "/api/update/dismiss", `{"rolled_back":true}`, &st)
	if st.RolledBack != "" || st.Dismissed != "v1.1.0" {
		t.Fatalf("after dismiss: %+v", st)
	}
}

func TestSettingsUpdateCheckDefaultsTrue(t *testing.T) {
	dir := t.TempDir()
	old := `{"schema":2,"auth":{"password_hash":"","session_secret":"s","api_token":"t"},
"settings":{"title":"Old","sort_mode":"recent","guest_access":false,"search_engine":"https://x.test/?q=%s","open_in_new_tab":true,"health_interval":60,"show_clock":false},
"workspaces":[],"categories":[],"links":[]}`
	if err := os.WriteFile(filepath.Join(dir, "genkan.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := st.Settings()
	if !s.UpdateCheck || s.UpdateDismissed != "" || s.Title != "Old" || s.ShowClock {
		t.Fatalf("%+v", s)
	}
	// and an explicit false survives a reload
	if _, err := st.UpdateSettings(store.SettingsPatch{UpdateCheck: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st2, _ := store.Open(dir)
	defer st2.Close()
	if st2.Settings().UpdateCheck {
		t.Fatal("update_check=false lost")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "genkan.json"))
	if !strings.Contains(string(raw), `"update_check": false`) {
		t.Fatalf("not persisted:\n%s", raw)
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out")
}
