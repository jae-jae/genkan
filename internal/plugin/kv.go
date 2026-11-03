package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jae-jae/genkan/internal/store"
)

const (
	// MaxKVBytes caps the total size of one plugin's key/value storage (keys + JSON values).
	MaxKVBytes = 256 << 10
	maxKVKey   = 200
	kvDebounce = 2 * time.Second

	// instPrefix starts the keys of instance-scoped storage ("@i/<instance id>/<key>").
	// Plugin-level storage can neither read, write nor list such keys.
	instPrefix = "@i/"
)

// instKey is the internal key of key in the storage of widget instance id.
func instKey(id, key string) string { return instPrefix + id + "/" + key }

// splitInstKey parses an internal key: ("", key, false) for a plugin-level key.
func splitInstKey(full string) (inst, key string, ok bool) {
	rest, found := strings.CutPrefix(full, instPrefix)
	if !found {
		return "", full, false
	}
	inst, key, found = strings.Cut(rest, "/")
	if !found {
		return "", full, false
	}
	return inst, key, true
}

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

// newScratchKV is a storage that is never written to disk (test runs of unsaved widget instances).
func newScratchKV() *kvStore {
	return &kvStore{exists: func() bool { return false }, loaded: true, m: map[string]json.RawMessage{}}
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

func (k *kvStore) getRaw(full string) (json.RawMessage, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.loadLocked()
	v, ok := k.m[full]
	return v, ok
}

// get / keys / set / delete are the plugin-level storage: the instance
// namespace is invisible to them.
func (k *kvStore) get(key string) (json.RawMessage, bool) {
	if strings.HasPrefix(key, instPrefix) {
		return nil, false
	}
	return k.getRaw(key)
}

func (k *kvStore) keys() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.loadLocked()
	out := make([]string, 0, len(k.m))
	for key := range k.m {
		if !strings.HasPrefix(key, instPrefix) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

var errKVFull = fmt.Errorf("storage is full (max %d KiB)", MaxKVBytes>>10)

func (k *kvStore) set(key string, val json.RawMessage) error {
	if key == "" || len(key) > maxKVKey {
		return fmt.Errorf("storage key must be 1-%d bytes", maxKVKey)
	}
	if strings.HasPrefix(key, instPrefix) {
		return fmt.Errorf("storage keys starting with %q are reserved", instPrefix)
	}
	return k.setRaw(key, val)
}

func (k *kvStore) delete(key string) {
	if !strings.HasPrefix(key, instPrefix) {
		k.deleteRaw(key)
	}
}

// Instance-scoped storage (ctx.instance.storage) shares the file and the size cap.

func (k *kvStore) iget(id, key string) (json.RawMessage, bool) { return k.getRaw(instKey(id, key)) }

func (k *kvStore) iset(id, key string, val json.RawMessage) error {
	if key == "" || len(key) > maxKVKey {
		return fmt.Errorf("storage key must be 1-%d bytes", maxKVKey)
	}
	return k.setRaw(instKey(id, key), val)
}

func (k *kvStore) idelete(id, key string) { k.deleteRaw(instKey(id, key)) }

func (k *kvStore) ikeys(id string) []string {
	pre := instKey(id, "")
	k.mu.Lock()
	defer k.mu.Unlock()
	k.loadLocked()
	out := []string{}
	for key := range k.m {
		if rest, ok := strings.CutPrefix(key, pre); ok {
			out = append(out, rest)
		}
	}
	sort.Strings(out)
	return out
}

// dropInstances removes the whole storage of the given widget instances.
func (k *kvStore) dropInstances(ids map[string]bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.loadLocked()
	changed := false
	for key, v := range k.m {
		if inst, _, ok := splitInstKey(key); ok && ids[inst] {
			k.size -= len(key) + len(v)
			delete(k.m, key)
			changed = true
		}
	}
	if changed {
		k.scheduleLocked()
	}
}

func (k *kvStore) setRaw(full string, val json.RawMessage) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.loadLocked()
	size := k.size + len(full) + len(val)
	if old, ok := k.m[full]; ok {
		size -= len(full) + len(old)
	}
	if size > MaxKVBytes {
		return errKVFull
	}
	k.m[full] = val
	k.size = size
	k.scheduleLocked()
	return nil
}

func (k *kvStore) deleteRaw(full string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.loadLocked()
	if old, ok := k.m[full]; ok {
		k.size -= len(full) + len(old)
		delete(k.m, full)
		k.scheduleLocked()
	}
}

// snapshot copies the whole content (internal keys) and its size.
func (k *kvStore) snapshot() (map[string]json.RawMessage, int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.loadLocked()
	out := make(map[string]json.RawMessage, len(k.m))
	for key, v := range k.m {
		out[key] = v
	}
	return out, k.size
}

// replaceAll swaps the whole content (nil/empty clears it) and persists at once.
func (k *kvStore) replaceAll(m map[string]json.RawMessage) error {
	size := 0
	for key, v := range m {
		size += len(key) + len(v)
	}
	if size > MaxKVBytes {
		return errKVFull
	}
	k.mu.Lock()
	k.loadLocked()
	k.m = make(map[string]json.RawMessage, len(m))
	for key, v := range m {
		k.m[key] = v
	}
	k.size = size
	k.mu.Unlock()
	return k.flush()
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
