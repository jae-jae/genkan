package plugin

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jae-jae/genkan/internal/store"
)

// StorageEntry is one stored value as shown to the admin. Size counts the
// stored (internal) key plus the JSON value, like the 256 KiB cap does.
type StorageEntry struct {
	Key      string          `json:"key"`
	Size     int             `json:"size"`
	Value    json.RawMessage `json:"value"`
	Instance string          `json:"instance,omitempty"` // widget instance id for instance-scoped keys
}

// StorageInfo is the response of GET /api/plugins/{id}/storage.
type StorageInfo struct {
	Size    int            `json:"size"`
	Limit   int            `json:"limit"`
	Entries []StorageEntry `json:"entries"`
}

// Storage lists everything a plugin stored: plugin-level keys first, then the
// widget instance keys, each group sorted by key.
func (m *Manager) Storage(id string) (StorageInfo, error) {
	e, _, err := m.entryFor(id)
	if err != nil {
		return StorageInfo{}, err
	}
	all, size := e.kv.snapshot()
	out := StorageInfo{Size: size, Limit: MaxKVBytes, Entries: make([]StorageEntry, 0, len(all))}
	for full, v := range all {
		inst, key, ok := splitInstKey(full)
		se := StorageEntry{Key: key, Size: len(full) + len(v), Value: v}
		if ok {
			se.Instance = inst
		}
		out.Entries = append(out.Entries, se)
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if (a.Instance == "") != (b.Instance == "") {
			return a.Instance == ""
		}
		if a.Instance != b.Instance {
			return a.Instance < b.Instance
		}
		return a.Key < b.Key
	})
	return out, nil
}

// ClearStorage deletes the whole storage of a plugin (key == ""), or one key
// (of widget instance `instance`, when given). Cached widget views are dropped.
func (m *Manager) ClearStorage(id, key, instance string) error {
	e, _, err := m.entryFor(id)
	if err != nil {
		return err
	}
	switch {
	case key == "" && instance != "":
		return invalidf("instance needs key")
	case key == "":
		if err := e.kv.replaceAll(nil); err != nil {
			return err
		}
	case instance != "":
		e.kv.idelete(instance, key)
	default:
		e.kv.delete(key)
	}
	m.forgetPluginWidgets(id)
	return nil
}

// forgetPluginWidgets drops the cached views of all widget instances of a plugin.
func (m *Manager) forgetPluginWidgets(plugin string) {
	for _, w := range m.st.Widgets() {
		if w.Plugin == plugin {
			m.forgetWidget(w.ID)
		}
	}
}

// clearInstances is the store's widgets-gone callback: the storage of removed
// widget instances is deleted.
func (m *Manager) clearInstances(refs []store.WidgetRef) {
	byPlugin := map[string]map[string]bool{}
	for _, r := range refs {
		if byPlugin[r.Plugin] == nil {
			byPlugin[r.Plugin] = map[string]bool{}
		}
		byPlugin[r.Plugin][r.ID] = true
	}
	for plugin, ids := range byPlugin {
		if e, _, err := m.entryFor(plugin); err == nil {
			e.kv.dropInstances(ids)
			m.forgetPluginWidgets(plugin)
		}
	}
}

// ExportData returns the storage of every installed plugin that has any, for
// GET /api/export?plugin_data=1 (instance keys included, with their internal names).
func (m *Manager) ExportData() map[string]map[string]json.RawMessage {
	out := map[string]map[string]json.RawMessage{}
	for _, rec := range m.st.Plugins() {
		all, _ := m.entryRec(rec).kv.snapshot()
		if len(all) > 0 {
			out[rec.ID] = all
		}
	}
	return out
}

// ValidatePluginData checks the plugin_data of an import before anything is written.
func ValidatePluginData(data map[string]map[string]json.RawMessage) error {
	for id, kv := range data {
		if !store.ValidPluginID(id) {
			return fmt.Errorf("plugin_data: invalid plugin id %q", id)
		}
		size := 0
		for key, v := range kv {
			if key == "" || len(key) > maxKVKey+len(instPrefix)+40 {
				return fmt.Errorf("plugin_data: %s: invalid key", id)
			}
			if strings.HasPrefix(key, instPrefix) {
				if _, k, ok := splitInstKey(key); !ok || k == "" || len(k) > maxKVKey {
					return fmt.Errorf("plugin_data: %s: invalid instance key", id)
				}
			} else if len(key) > maxKVKey {
				return fmt.Errorf("plugin_data: %s: key too long", id)
			}
			size += len(key) + len(v)
		}
		if size > MaxKVBytes {
			return fmt.Errorf("plugin_data: %s: more than %d KiB", id, MaxKVBytes>>10)
		}
	}
	return nil
}

// RestoreData applies the plugin_data of an import (present in the file only):
// the storage of the plugins the import wrote (res.PluginIDs: all of them on
// replace, only the new ones on merge) is replaced. Instance keys follow their
// widgets to the new ids; keys of instances that were not imported are dropped.
func (m *Manager) RestoreData(data map[string]map[string]json.RawMessage, res store.ImportResult) error {
	var first error
	for _, id := range res.PluginIDs {
		rec, ok := m.st.GetPlugin(id)
		if !ok {
			continue
		}
		next := map[string]json.RawMessage{}
		for key, v := range data[id] {
			if inst, k, isInst := splitInstKey(key); isInst {
				w, ok := res.WidgetMap[inst]
				if !ok || w.Plugin != id {
					continue
				}
				key = instKey(w.ID, k)
			}
			next[key] = v
		}
		if err := m.entryRec(rec).kv.replaceAll(next); err != nil && first == nil {
			first = fmt.Errorf("plugin %s: %w", id, err)
		}
		m.forgetPluginWidgets(id)
	}
	return first
}
