// Package health periodically probes links that have health_check enabled.
package health

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jae-jae/genkan/internal/store"
)

const (
	maxConcurrent = 8
	probeTimeout  = 5 * time.Second
	scanEvery     = 3 * time.Second
)

// Result is one link's health state (JSON shape is part of the API).
type Result struct {
	Status    string `json:"status"` // up | down | unknown
	LatencyMS int64  `json:"latency_ms"`
	CheckedAt string `json:"checked_at"`
	Code      int    `json:"code"`
}

// Source supplies probe targets.
type Source interface {
	HealthConfig() ([]store.HealthTarget, int)
}

// Checker runs probes in the background and holds the latest results in memory.
type Checker struct {
	src      Source
	client   *http.Client
	onUpdate func(map[string]Result)

	mu      sync.RWMutex
	results map[string]Result
	last    map[string]probed // last probe per id

	kick chan struct{}
}

// New creates a Checker. onUpdate (may be nil) is called with a copy of the
// result map after every round in which something was probed or changed.
func New(src Source, onUpdate func(map[string]Result)) *Checker {
	tr := &http.Transport{
		Proxy:                  http.ProxyFromEnvironment,
		DialContext:            (&net.Dialer{Timeout: probeTimeout}).DialContext,
		TLSClientConfig:        &tls.Config{InsecureSkipVerify: true}, // self-signed certs are common in homelabs
		TLSHandshakeTimeout:    probeTimeout,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 64 << 10,
	}
	return &Checker{
		src: src,
		client: &http.Client{
			Transport: tr,
			Timeout:   probeTimeout,
			// A redirect is a response from a live service; do not follow it.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		onUpdate: onUpdate,
		results:  map[string]Result{},
		last:     map[string]probed{},
		kick:     make(chan struct{}, 1),
	}
}

type probed struct {
	at  time.Time
	url string
}

// Kick asks for an immediate scan (e.g. after links changed).
func (c *Checker) Kick() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

// Snapshot returns a copy of the current results.
func (c *Checker) Snapshot() map[string]Result {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]Result, len(c.results))
	for k, v := range c.results {
		out[k] = v
	}
	return out
}

// Run blocks until ctx is cancelled.
func (c *Checker) Run(ctx context.Context) {
	t := time.NewTicker(scanEvery)
	defer t.Stop()
	c.scan(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-c.kick:
			time.Sleep(200 * time.Millisecond) // coalesce bursts of edits
		}
		c.scan(ctx)
	}
}

func (c *Checker) scan(ctx context.Context) {
	targets, interval := c.src.HealthConfig()
	every := time.Duration(interval) * time.Second
	if every < 15*time.Second {
		every = 15 * time.Second
	}
	now := time.Now()

	live := make(map[string]bool, len(targets))
	var due []store.HealthTarget
	changed := false

	c.mu.Lock()
	for _, t := range targets {
		live[t.ID] = true
		if _, ok := c.results[t.ID]; !ok {
			c.results[t.ID] = Result{Status: "unknown"}
			changed = true
		}
		if last, ok := c.last[t.ID]; !ok || last.url != t.URL || now.Sub(last.at) >= every {
			due = append(due, t)
		}
	}
	for id := range c.results {
		if !live[id] {
			delete(c.results, id)
			delete(c.last, id)
			changed = true
		}
	}
	for _, t := range due {
		c.last[t.ID] = probed{now, t.URL}
	}
	c.mu.Unlock()

	if len(due) > 0 {
		sem := make(chan struct{}, maxConcurrent)
		var wg sync.WaitGroup
		for _, t := range due {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				return
			}
			wg.Add(1)
			go func(t store.HealthTarget) {
				defer wg.Done()
				defer func() { <-sem }()
				r := c.Probe(ctx, t.URL)
				if ctx.Err() != nil {
					return
				}
				c.mu.Lock()
				if live[t.ID] {
					c.results[t.ID] = r
				}
				c.mu.Unlock()
			}(t)
		}
		wg.Wait()
		changed = true
	}
	if changed && c.onUpdate != nil && ctx.Err() == nil {
		c.onUpdate(c.Snapshot())
	}
}

// Probe checks one URL: HEAD first, then GET if the server rejects HEAD.
func (c *Checker) Probe(ctx context.Context, rawURL string) Result {
	checked := time.Now().UTC().Format(time.RFC3339)
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Result{Status: "unknown", CheckedAt: checked}
	}
	start := time.Now()
	code, err := c.do(ctx, http.MethodHead, rawURL)
	if err == nil && (code == http.StatusMethodNotAllowed || code == http.StatusNotImplemented) {
		code, err = c.do(ctx, http.MethodGet, rawURL)
	}
	lat := time.Since(start).Milliseconds()
	if err != nil {
		return Result{Status: "down", LatencyMS: lat, CheckedAt: checked}
	}
	st := "up"
	if code >= 500 {
		st = "down"
	}
	return Result{Status: st, LatencyMS: lat, CheckedAt: checked, Code: code}
}

func (c *Checker) do(ctx context.Context, method, rawURL string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Genkan-HealthCheck/1.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
