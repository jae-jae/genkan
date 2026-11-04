package plugin

import (
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	logCap    = 200  // entries kept per plugin
	maxLogMsg = 2000 // characters per entry
)

// LogEntry is one line of a plugin's log. Ts is a Unix timestamp in milliseconds.
type LogEntry struct {
	Ts    int64  `json:"ts"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
	seq   uint64
}

// logRing is a fixed-size in-memory ring buffer.
type logRing struct {
	mu   sync.Mutex
	buf  []LogEntry
	next int
	full bool
	seq  uint64
}

func newLogRing() *logRing { return &logRing{buf: make([]LogEntry, logCap)} }

func (r *logRing) add(level, msg string, redact []string) {
	msg = redactSecrets(msg, redact)
	if utf8.RuneCountInString(msg) > maxLogMsg {
		msg = string([]rune(msg)[:maxLogMsg]) + "…"
	}
	r.mu.Lock()
	r.seq++
	r.buf[r.next] = LogEntry{Ts: time.Now().UnixMilli(), Level: level, Msg: msg, seq: r.seq}
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// all returns the entries oldest first.
func (r *logRing) all() []LogEntry { return r.since(0) }

// since returns the entries added after sequence number seq, oldest first.
func (r *logRing) since(seq uint64) []LogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []LogEntry{}
	n := r.next
	start := 0
	if r.full {
		n, start = len(r.buf), r.next
	}
	for i := 0; i < n; i++ {
		e := r.buf[(start+i)%len(r.buf)]
		if e.seq > seq {
			out = append(out, e)
		}
	}
	return out
}

func (r *logRing) mark() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seq
}

// redactSecrets replaces every occurrence of a secret value in s.
func redactSecrets(s string, secrets []string) string {
	for _, sec := range secrets {
		if len(sec) >= 4 && strings.Contains(s, sec) {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	return s
}
