// Package auth implements password hashing, stateless session cookies,
// API tokens and login rate limiting.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	CookieName   = "genkan_session"
	SessionTTL   = 30 * 24 * time.Hour
	MinPassword  = 6
	maxPassBytes = 72 // bcrypt limit
	TokenPrefix  = "gk_"
)

// HashPassword returns a bcrypt hash.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword compares pw against a bcrypt hash.
func CheckPassword(hash, pw string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// ValidatePassword returns a message if pw is unacceptable, else "".
func ValidatePassword(pw string) string {
	if len(pw) < MinPassword {
		return "password must be at least 6 characters"
	}
	if len(pw) > maxPassBytes {
		return "password must be at most 72 bytes"
	}
	return ""
}

func mac(secret, msg string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}

// SignSession returns "<unix expiry>.<hmac-sha256 hex>".
func SignSession(secret string, expires time.Time) string {
	ts := strconv.FormatInt(expires.Unix(), 10)
	return ts + "." + mac(secret, ts)
}

// VerifySession checks signature and expiry.
func VerifySession(secret, value string, now time.Time) bool {
	ts, sig, ok := strings.Cut(value, ".")
	if !ok || secret == "" {
		return false
	}
	exp, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || now.Unix() >= exp {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(sig), []byte(mac(secret, ts))) == 1
}

var tokEnc = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewToken creates the API token: gk_ + 32 random base32 chars.
func NewToken() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return TokenPrefix + tokEnc.EncodeToString(b) // 20 bytes -> 32 chars
}

// Limiter rate-limits failed logins per key (IP): at most Max failures per Window.
type Limiter struct {
	Max    int
	Window time.Duration
	Now    func() time.Time

	mu sync.Mutex
	m  map[string][]time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{Max: max, Window: window, Now: time.Now, m: map[string][]time.Time{}}
}

func (l *Limiter) prune(key string, now time.Time) []time.Time {
	ts := l.m[key]
	i := 0
	for i < len(ts) && now.Sub(ts[i]) >= l.Window {
		i++
	}
	ts = ts[i:]
	if len(ts) == 0 {
		delete(l.m, key)
		return nil
	}
	l.m[key] = ts
	return ts
}

// Allow reports whether key may attempt a login now; if not, how long to wait.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	ts := l.prune(key, now)
	if len(ts) >= l.Max {
		return false, ts[0].Add(l.Window).Sub(now)
	}
	return true, 0
}

// Fail records a failed attempt.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	if len(l.m) > 10000 { // bound memory under address-spraying
		for k := range l.m {
			l.prune(k, now)
		}
		if len(l.m) > 10000 {
			l.m = map[string][]time.Time{}
		}
	}
	l.prune(key, now)
	l.m[key] = append(l.m[key], now)
}

// Reset clears failures for key (after a successful login).
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.m, key)
	l.mu.Unlock()
}
