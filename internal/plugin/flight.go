package plugin

import "sync"

// flight merges concurrent calls with the same key into one execution
// (a minimal singleflight; the stdlib has none and x/sync is not a dependency).
type flight struct {
	mu sync.Mutex
	m  map[string]*flightCall
}

type flightCall struct {
	done chan struct{}
	val  any
	err  error
}

// do runs fn once per key at a time. Every caller waiting for the same key
// gets the same result; shared reports whether the result was shared.
func (f *flight) do(key string, fn func() (any, error)) (v any, err error, shared bool) {
	f.mu.Lock()
	if f.m == nil {
		f.m = map[string]*flightCall{}
	}
	if c, ok := f.m[key]; ok {
		f.mu.Unlock()
		<-c.done
		return c.val, c.err, true
	}
	c := &flightCall{done: make(chan struct{})}
	f.m[key] = c
	f.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			c.err = errPanic(r)
			err = c.err
		}
		f.mu.Lock()
		delete(f.m, key)
		f.mu.Unlock()
		close(c.done)
	}()
	c.val, c.err = fn()
	return c.val, c.err, false
}
