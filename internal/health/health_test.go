package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jae-jae/genkan/internal/store"
)

type src struct {
	mu      sync.Mutex
	targets []store.HealthTarget
}

func (s *src) HealthConfig() ([]store.HealthTarget, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.HealthTarget(nil), s.targets...), 60
}

func TestProbe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login", 302) })
	mux.HandleFunc("/err", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	mux.HandleFunc("/nohead", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(405)
			return
		}
		w.WriteHeader(200)
	})
	ts := httptest.NewTLSServer(mux) // self-signed cert must be accepted
	defer ts.Close()
	c := New(&src{}, nil)
	cases := map[string]struct {
		status string
		code   int
	}{"/ok": {"up", 200}, "/auth": {"up", 401}, "/redir": {"up", 302}, "/err": {"down", 503}, "/nohead": {"up", 200}}
	for p, want := range cases {
		r := c.Probe(context.Background(), ts.URL+p)
		if r.Status != want.status || r.Code != want.code || r.CheckedAt == "" {
			t.Errorf("%s: %+v", p, r)
		}
	}
	if r := c.Probe(context.Background(), "http://127.0.0.1:1/"); r.Status != "down" {
		t.Errorf("refused: %+v", r)
	}
	if r := c.Probe(context.Background(), "ssh://host"); r.Status != "unknown" {
		t.Errorf("scheme: %+v", r)
	}
}

func TestRunPublishesAndCleansUp(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer ts.Close()
	s := &src{targets: []store.HealthTarget{{ID: "a", URL: ts.URL}}}
	updates := make(chan map[string]Result, 8)
	c := New(s, func(m map[string]Result) { updates <- m })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()

	select {
	case m := <-updates:
		if m["a"].Status != "up" {
			t.Fatalf("%+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no update")
	}
	// removing the target drops its result
	s.mu.Lock()
	s.targets = nil
	s.mu.Unlock()
	c.Kick()
	deadline := time.After(5 * time.Second)
	for {
		if len(c.Snapshot()) == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("stale result kept")
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
}
