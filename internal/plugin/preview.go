package plugin

import (
	"context"
	"errors"

	"github.com/jae-jae/genkan/internal/store"
)

// PreviewResult is the response of POST /api/plugins/preview (docs/PLUGINS.md §11.2).
// Plugin holds only manifest derived fields (no config, errors or timestamps).
type PreviewResult struct {
	Plugin           *Info  `json:"plugin"`
	Source           string `json:"source"`
	Lang             string `json:"lang"`
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installed_version,omitempty"`
	Error            string `json:"error,omitempty"`
}

// Preview compiles and validates a plugin exactly like Install but changes
// nothing: no files, no store records, no hooks. A source that cannot be
// downloaded or does not validate is reported in Error (with the source when
// there is one); malformed requests are errors.
func (m *Manager) Preview(ctx context.Context, req InstallRequest) (PreviewResult, error) {
	source, lang, _, err := m.resolve(ctx, req)
	if err != nil {
		var he *HookError
		if errors.As(err, &he) {
			return PreviewResult{Lang: normSourceLang(req.Lang), Error: he.Msg}, nil
		}
		return PreviewResult{}, err
	}
	res := PreviewResult{Source: source, Lang: lang}
	man, _, err := m.inspect(source, lang)
	if err != nil {
		res.Error = "invalid plugin: " + err.Error()
		return res, nil
	}
	res.Plugin = &Info{Manifest: *man}
	res.Installed, res.InstalledVersion = m.installedVersion(man.ID)
	return res, nil
}

func normSourceLang(l string) string {
	if l == "ts" {
		return "ts"
	}
	return "js"
}

// installedVersion reports whether a plugin id is installed and its version
// (empty when the installed source is broken).
func (m *Manager) installedVersion(id string) (bool, string) {
	rec, ok := m.st.GetPlugin(id)
	if !ok {
		return false, ""
	}
	if e := m.entryRec(rec); e.man != nil {
		return true, e.man.Version
	}
	return true, ""
}

// installedVersions maps every installed plugin id to its version.
func (m *Manager) installedVersions() map[string]string {
	out := map[string]string{}
	for _, rec := range m.st.Plugins() {
		out[rec.ID] = ""
		if e := m.entryRec(rec); e.man != nil {
			out[rec.ID] = e.man.Version
		}
	}
	return out
}

// UpdateSource replaces the source of an installed plugin (docs/PLUGINS.md
// §11.2). The manifest id must stay the same; enabled state, configuration,
// widget instances and source_url are kept. An omitted lang keeps the current one.
func (m *Manager) UpdateSource(id, source, lang string) (Info, error) {
	rec, ok := m.st.GetPlugin(id)
	if !ok {
		return Info{}, notFoundf("plugin not found: %s", id)
	}
	if lang == "" {
		lang = rec.Lang
	}
	if lang == "" {
		lang = "js"
	}
	if lang != "js" && lang != "ts" {
		return Info{}, invalidf(`lang must be "js" or "ts"`)
	}
	if len(source) > store.MaxPluginSource {
		return Info{}, invalidf("plugin source is too large (max %d KiB)", store.MaxPluginSource>>10)
	}
	man, mod, err := m.inspect(source, lang)
	if err != nil {
		return Info{}, invalidf("invalid plugin: %v", err)
	}
	if man.ID != id {
		return Info{}, invalidf("manifest.id %q must stay %q", man.ID, id)
	}
	rec, _, err = m.st.SavePlugin(store.PluginSave{ID: id, Source: source, Lang: lang, SourceURL: rec.SourceURL})
	if err != nil {
		return Info{}, err
	}
	m.prime(rec, man, mod)
	m.sched.kick()
	return m.info(rec, true), nil
}
