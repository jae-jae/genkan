package plugin

import (
	"context"
	"log"
	"slices"
)

// autoInstall lists the built-in plugins installed once, on first start
// (docs/PLUGINS.md §15.4).
var autoInstall = []string{"todo", "note"}

// SeedDefaults installs the built-in plugins of autoInstall that were not
// offered before. Call it once at startup, after New.
func (m *Manager) SeedDefaults(ctx context.Context) { m.seed(ctx, autoInstall) }

// seed handles every id that is not yet in Settings.PluginsSeeded: it installs
// (and enables) the catalog plugin unless a plugin with that id exists, then
// records the id, so a plugin the admin uninstalls later never comes back. A
// failed install is only logged and not recorded: it is retried at the next
// start. No widgets are added.
func (m *Manager) seed(ctx context.Context, ids []string) {
	enable := true
	for _, id := range ids {
		if slices.Contains(m.st.Settings().PluginsSeeded, id) {
			continue
		}
		if _, installed := m.st.GetPlugin(id); !installed {
			if _, err := m.Install(ctx, InstallRequest{Catalog: id, Enable: &enable}); err != nil {
				log.Printf("plugins: install built-in %s: %v", id, err)
				continue
			}
			log.Printf("plugins: installed built-in plugin %s", id)
		}
		if err := m.st.MarkPluginsSeeded(id); err != nil {
			log.Printf("plugins: record seeded %s: %v", id, err)
		}
	}
}
