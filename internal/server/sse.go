package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Broker fans server-sent events out to connected clients.
type Broker struct {
	max  int
	mu   sync.Mutex
	subs map[chan []byte]struct{}
	done chan struct{}
	shut bool
}

func NewBroker(max int) *Broker {
	return &Broker{max: max, subs: map[chan []byte]struct{}{}, done: make(chan struct{})}
}

// Subscribe registers a client. ok is false when the limit is reached or the broker is closed.
func (b *Broker) Subscribe() (ch chan []byte, cancel func(), ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.shut || len(b.subs) >= b.max {
		return nil, nil, false
	}
	ch = make(chan []byte, 16)
	b.subs[ch] = struct{}{}
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch) // never closed by us: publishers hold b.mu while sending
		b.mu.Unlock()
	}, true
}

// Clients returns the number of connected clients.
func (b *Broker) Clients() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

func (b *Broker) publish(msg []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- msg:
		default:
			// Client is not draining: drop it so it reconnects and resyncs.
			delete(b.subs, ch)
			close(ch)
		}
	}
}

func changeEvent(v int64) []byte {
	s := strconv.FormatInt(v, 10)
	return []byte("id: " + s + "\nevent: change\ndata: {\"version\":" + s + "}\n\n")
}

func (b *Broker) PublishChange(v int64) { b.publish(changeEvent(v)) }

func (b *Broker) PublishHealth(m any) {
	b.mu.Lock()
	n := len(b.subs)
	b.mu.Unlock()
	if n == 0 {
		return
	}
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	b.publish([]byte("event: health\ndata: " + string(data) + "\n\n"))
}

// Close ends all streams; further Subscribe calls fail.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.shut {
		b.shut = true
		close(b.done)
	}
}

const pingEvery = 25 * time.Second

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request, _ Role) {
	rc := http.NewResponseController(w)
	ch, cancel, ok := s.broker.Subscribe()
	if !ok {
		writeErr(w, http.StatusServiceUnavailable, "too many event streams")
		return
	}
	defer cancel()
	// SSE streams are long-lived: lift the server-wide write deadline.
	_ = rc.SetWriteDeadline(time.Time{})
	_ = rc.SetReadDeadline(time.Time{})

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("retry: 3000\n\n"))
	// Reconnect catch-up: if the client missed changes, tell it right away.
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		if v := s.st.Version(); last != strconv.FormatInt(v, 10) {
			_, _ = w.Write(changeEvent(v))
		}
	}
	if err := rc.Flush(); err != nil {
		return
	}

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.broker.done:
			return
		case <-ping.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(msg); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
