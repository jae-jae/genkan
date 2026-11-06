package plugin

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const fetchTimeout = 15 * time.Second

// allowList is a set of fetch host patterns: "host", "host:port",
// "*.suffix" (subdomains only) and "*" (anything). Matching is case-insensitive.
type allowList struct {
	all     bool
	entries []allowEntry
}

type allowEntry struct {
	suffix bool   // "*.example.com"
	host   string // lower-case; for suffix entries ".example.com"
	port   string // "" = any port
}

func newAllowList(patterns ...[]string) *allowList {
	a := &allowList{}
	for _, ps := range patterns {
		for _, p := range ps {
			p = strings.ToLower(strings.TrimSpace(p))
			switch {
			case p == "":
			case p == "*":
				a.all = true
			default:
				e := allowEntry{}
				if h, port, err := net.SplitHostPort(p); err == nil {
					p, e.port = h, port
				}
				if strings.HasPrefix(p, "*.") {
					e.suffix, e.host = true, p[1:]
				} else {
					e.host = p
				}
				a.entries = append(a.entries, e)
			}
		}
	}
	return a
}

func (a *allowList) hasWildcard() bool { return a != nil && a.all }

func (a *allowList) match(host, port string) bool {
	if a == nil {
		return false
	}
	if a.all {
		return true
	}
	host = strings.ToLower(host)
	for _, e := range a.entries {
		if e.port != "" && e.port != port {
			continue
		}
		if e.suffix {
			if strings.HasSuffix(host, e.host) && len(host) > len(e.host) {
				return true
			}
		} else if host == e.host {
			return true
		}
	}
	return false
}

func defaultPort(r *http.Request) string {
	if p := r.URL.Port(); p != "" {
		return p
	}
	if r.URL.Scheme == "https" {
		return "443"
	}
	return "80"
}

// check approves a request: scheme http(s), host (and the Host header, when
// set) in the list.
func (a *allowList) check(r *http.Request) error {
	if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
		return fmt.Errorf("scheme %q is not allowed", r.URL.Scheme)
	}
	port := defaultPort(r)
	if !a.match(r.URL.Hostname(), port) {
		return fmt.Errorf("fetch to %s is not allowed (add it to manifest.permissions.fetch)", r.URL.Host)
	}
	if r.Host != "" && r.Host != r.URL.Host {
		h, p, err := net.SplitHostPort(r.Host)
		if err != nil {
			h, p = r.Host, port
		}
		if !a.match(h, p) {
			return fmt.Errorf("fetch with Host header %s is not allowed", r.Host)
		}
	}
	return nil
}

// hostTransport verifies TLS certificates, except for hosts the admin
// configured through an allowFetch url field without verifyTLS (homelab
// services often use self-signed certificates). Strict declarations win.
type hostTransport struct {
	strict   *http.Transport
	lenient  *http.Transport
	lenients atomic.Pointer[allowList]
}

func newHostTransport() *hostTransport {
	strict := http.DefaultTransport.(*http.Transport).Clone()
	strict.MaxIdleConns = 4
	strict.IdleConnTimeout = 30 * time.Second
	lenient := strict.Clone()
	lenient.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // admin-configured homelab hosts only
	return &hostTransport{strict: strict, lenient: lenient}
}

func (t *hostTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme == "https" && t.lenients.Load().match(r.URL.Hostname(), defaultPort(r)) {
		return t.lenient.RoundTrip(r)
	}
	return t.strict.RoundTrip(r)
}

func (t *hostTransport) close() {
	t.strict.CloseIdleConnections()
	t.lenient.CloseIdleConnections()
}

var errRedirects = errors.New("too many redirects")

// newClient builds the http.Client plugins use (fetch and wallpaper downloads).
func newClient(rt http.RoundTripper, allow func(*http.Request) error) *http.Client {
	return &http.Client{
		Timeout:   fetchTimeout,
		Transport: rt,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errRedirects
			}
			return allow(req)
		},
	}
}
