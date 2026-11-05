package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPluginsSeededDefaultsAndOldFiles(t *testing.T) {
	// a fresh store: an empty array, not null
	s, dir := open(t)
	if got := s.Settings().PluginsSeeded; got == nil || len(got) != 0 {
		t.Fatalf("fresh: %#v", got)
	}
	raw, _ := json.Marshal(s.Settings())
	if !strings.Contains(string(raw), `"plugins_seeded":[]`) {
		t.Fatalf("json: %s", raw)
	}
	s.Close()

	// an old genkan.json without the field loads with an empty list and keeps its other settings
	old := `{"schema":2,"auth":{"password_hash":"h","session_secret":"abc","api_token":"gk_x"},
	  "settings":{"title":"Mine","sort_mode":"recent","guest_access":true,"search_engine":"https://e.example/?q=%s","open_in_new_tab":false,"health_interval":30,"show_clock":false,"update_check":false},
	  "workspaces":[],"categories":[],"links":[]}`
	if err := os.WriteFile(filepath.Join(dir, "genkan.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := s2.Settings()
	if st.Title != "Mine" || st.SortMode != "recent" || !st.GuestAccess || st.ShowClock || st.UpdateCheck || st.HealthInterval != 30 {
		t.Errorf("old settings changed: %+v", st)
	}
	if st.PluginsSeeded == nil || len(st.PluginsSeeded) != 0 {
		t.Errorf("old file: %#v", st.PluginsSeeded)
	}
	s2.Close()
	rawFile, _ := os.ReadFile(filepath.Join(dir, "genkan.json"))
	if !strings.Contains(string(rawFile), `"plugins_seeded": []`) && !strings.Contains(string(rawFile), `"plugins_seeded":[]`) {
		t.Errorf("the field is written back:\n%s", rawFile)
	}
}

func TestMarkPluginsSeeded(t *testing.T) {
	s, dir := open(t)
	v0 := s.Version()
	if err := s.MarkPluginsSeeded("todo", "note", "todo"); err != nil {
		t.Fatal(err)
	}
	if got := s.Settings().PluginsSeeded; !reflect.DeepEqual(got, []string{"todo", "note"}) {
		t.Errorf("got %v", got)
	}
	v1 := s.Version()
	if v1 == v0 {
		t.Error("version must change")
	}
	if err := s.MarkPluginsSeeded("note"); err != nil || s.Version() != v1 {
		t.Errorf("recording an id twice is a no-op: %v", err)
	}
	for _, bad := range []string{"", "Todo", "a b", "x", strings.Repeat("a", 41), "../x"} {
		if err := s.MarkPluginsSeeded(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if got := s.Settings().PluginsSeeded; len(got) != 2 {
		t.Errorf("invalid ids were recorded: %v", got)
	}
	s.Close()
	s2, err := Open(dir) // survives a restart
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got := s2.Settings().PluginsSeeded; !reflect.DeepEqual(got, []string{"todo", "note"}) {
		t.Errorf("after reopen: %v", got)
	}
}

func TestPluginsSeededIsNotEditableThroughSettings(t *testing.T) {
	s, _ := open(t)
	_ = s.MarkPluginsSeeded("todo")
	// SettingsPatch has no such field; an unrelated patch keeps the list
	title := "X"
	ns, err := s.UpdateSettings(SettingsPatch{Title: &title})
	if err != nil || !reflect.DeepEqual(ns.PluginsSeeded, []string{"todo"}) {
		t.Errorf("%v %v", ns.PluginsSeeded, err)
	}
	// and decoding a patch body with plugins_seeded ignores it
	var p SettingsPatch
	if err := json.Unmarshal([]byte(`{"title":"Y","plugins_seeded":["evil"]}`), &p); err != nil {
		t.Fatal(err)
	}
	ns, _ = s.UpdateSettings(p)
	if ns.Title != "Y" || !reflect.DeepEqual(ns.PluginsSeeded, []string{"todo"}) {
		t.Errorf("%+v", ns)
	}
	// loading a file cleans junk
	got := cleanSeeded([]string{"todo", "todo", "BAD ID", "", "note"})
	if !reflect.DeepEqual(got, []string{"todo", "note"}) {
		t.Errorf("cleanSeeded: %v", got)
	}
}

func TestPluginsSeededSurvivesExportImport(t *testing.T) {
	src, _ := open(t)
	_ = src.MarkPluginsSeeded("todo", "note")
	exp := src.Export()
	raw, _ := json.Marshal(exp)
	if !strings.Contains(string(raw), `"plugins_seeded":["todo","note"]`) {
		t.Fatalf("export lacks the field: %s", raw)
	}
	var back Export
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}

	// replace into another store: the imported list arrives, what the target had stays
	dst, _ := open(t)
	_ = dst.MarkPluginsSeeded("extra")
	if _, err := dst.ImportData("replace", back); err != nil {
		t.Fatal(err)
	}
	if got := dst.Settings().PluginsSeeded; !reflect.DeepEqual(got, []string{"extra", "todo", "note"}) {
		t.Errorf("replace: %v", got)
	}

	// an old backup without the field must not make seeded plugins come back
	old := back
	old.Settings = DefaultSettings()
	old.Settings.PluginsSeeded = nil
	dst2, _ := open(t)
	_ = dst2.MarkPluginsSeeded("todo", "note")
	if _, err := dst2.ImportData("replace", old); err != nil {
		t.Fatal(err)
	}
	if got := dst2.Settings().PluginsSeeded; !reflect.DeepEqual(got, []string{"todo", "note"}) {
		t.Errorf("old backup: %v", got)
	}

	// merge leaves settings alone
	dst3, _ := open(t)
	if _, err := dst3.ImportData("merge", back); err != nil {
		t.Fatal(err)
	}
	if got := dst3.Settings().PluginsSeeded; len(got) != 0 {
		t.Errorf("merge: %v", got)
	}
}
