package plugin

import (
	"context"
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/jae-jae/genkan/internal/store"
)

type tenv struct {
	t   *testing.T
	st  *store.Store
	m   *Manager
	dir string
}

func newEnv(t *testing.T) *tenv {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Config{Store: st, DataDir: dir, Version: "test-1.2"})
	t.Cleanup(func() {
		m.Close()
		st.Close()
	})
	return &tenv{t: t, st: st, m: m, dir: dir}
}

// install installs source and fails the test on error.
func (e *tenv) install(source string) Info {
	e.t.Helper()
	inf, err := e.m.Install(context.Background(), InstallRequest{Source: source})
	if err != nil {
		e.t.Fatalf("install: %v", err)
	}
	return inf
}

func (e *tenv) installErr(source string) error {
	e.t.Helper()
	_, err := e.m.Install(context.Background(), InstallRequest{Source: source})
	if err == nil {
		e.t.Fatal("expected install error")
	}
	return err
}

// plug builds a minimal plugin source: manifest fields + body (exports).
func plug(manifest, body string) string {
	return "export const manifest = " + manifest + ";\n" + body
}

func mapFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for k, v := range files {
		m[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return m
}

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
