package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jae-jae/genkan/internal/store"
)

const (
	// MaxKVBytes caps the total size of one plugin's key/value storage (keys + JSON values).
	MaxKVBytes = 256 << 10
	maxKVKey   = 200
	kvDebounce = 2 * time.Second
)

// kvStore is a plugin's persistent JSON key/value storage, backed by
// <data>/plugins/<id>.kv.json. Writes are debounced.
type kvStore struct {
	path   string
	exists func() bool // false once the plugin was uninstalled: never write then

	mu     sync.Mutex
	loaded bool
	m      map[string]json.RawMessage
	size   int
	timer  *time.Timer
	closed bool
}

func newKV(dir, id string, exists func() bool) *kvStore {
	return &kvStore{path: filepath.Join(dir, "plugins", id+".kv.json"), exists: exists}
}

func (k *kvStore) loadLocked() {
	if k.loaded {
		return
	}
	k.loaded = true
	k.m = map[string]json.RawMessage{}
	b, err := os.ReadFile(k.path)
	if err != nil {
		return
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return
	}
	k.m = m
	for key, v := range m {
		k.size += len(key) + len(v)
	}
}

func (k *kvStore) get(key string) (json.RawMessage, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.loadLocked()
	v, ok := k.m[key]
	return v, ok
}

func (k *kvStore) keys() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.loadLocked()
	out := make([]string, 0, len(k.m))
	for key := range k.m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

var errKVFull = fmt.Errorf("storage is full (max %d KiB)", MaxKVBytes>>10)

func (k *kvStore) set(key string, val json.RawMessage) error {
	if key == "" || len(key) > maxKVKey {
		return fmt.Errorf("storage key must be 1-%d bytes", maxKVKey)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.loadLocked()
	size := k.size + len(key) + len(val)
	if old, ok := k.m[key]; ok {
		size -= len(key) + len(old)
	}
	if size > MaxKVBytes {
		return errKVFull
	}
	k.m[key] = val
	k.size = size
	k.scheduleLocked()
	return nil
}

func (k *kvStore) delete(key string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.loadLocked()
	if old, ok := k.m[key]; ok {
		k.size -= len(key) + len(old)
		delete(k.m, key)
		k.scheduleLocked()
	}
}

func (k *kvStore) scheduleLocked() {
	if k.timer == nil && !k.closed {
		k.timer = time.AfterFunc(kvDebounce, func() { _ = k.flush() })
	}
}

// flush writes pending changes.
func (k *kvStore) flush() error {
	k.mu.Lock()
	k.timer = nil
	if !k.loaded || (k.exists != nil && !k.exists()) {
		k.mu.Unlock()
		return nil
	}
	data, err := json.Marshal(k.m)
	k.mu.Unlock()
	if err != nil {
		return err
	}
	return store.AtomicWriteFile(k.path, data, 0o600)
}

// close flushes and stops the debounce timer.
func (k *kvStore) close() error {
	k.mu.Lock()
	k.closed = true
	pending := k.timer != nil
	if k.timer != nil {
		k.timer.Stop()
	}
	k.mu.Unlock()
	if pending {
		return k.flush()
	}
	return nil
}

var errPanicType = errors.New("plugin panic")

func errPanic(r any) error { return fmt.Errorf("%w: %v", errPanicType, r) }
