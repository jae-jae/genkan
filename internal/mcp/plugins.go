package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jae-jae/genkan/internal/plugin"
)

const pluginDocs = " Plugin authoring guide: docs/PLUGINS.md (an ES module exporting `manifest`, optionally `messages` for localization with t()/ctx.t, and async hook functions wallpaper/widget/search/schedule/action). 插件开发说明见 docs/PLUGINS.md。"

var errNoPlugins = errors.New("plugins are not available")

// pluginTools are the plugin management tools (docs/PLUGINS.md §6).
func (s *Server) pluginTools() []tool {
	mgr := func() (*plugin.Manager, error) {
		if s.Plugins == nil {
			return nil, errNoPlugins
		}
		return s.Plugins, nil
	}
	ctx := func(d time.Duration) (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), d)
	}
	cfgObj := func(desc string) M { return M{"type": "object", "description": desc} }

	return []tool{
		{
			name: "list_plugins", title: "List plugins", readOnly: true, idempotent: true,
			desc:   "List installed plugins (manifest, enabled flag, current config with secrets blanked and secrets_set, last_error) and the built-in example catalog that can be installed with install_plugin {catalog}. User-visible manifest strings (name, description, labels) are either a plain string or an object {language: text}, e.g. {\"en\":\"Weather\",\"zh\":\"天气\"}." + pluginDocs,
			schema: obj(M{}),
			fn: func(json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				return M{"plugins": m.Infos(true), "catalog": m.Catalog()}, nil
			},
		},
		{
			name: "install_plugin", title: "Install plugin", idempotent: true,
			desc: "Install or update a plugin from JavaScript/TypeScript source, an http(s) URL (max 512 KiB) or a built-in catalog id. An existing plugin with the same manifest id keeps its config and widgets. The manifest and exports are validated; errors say what to fix. Test it with test_plugin afterwards." + pluginDocs,
			schema: obj(M{
				"source":  str("Plugin source code (ES module)."),
				"lang":    enumStr("Language of source/url: js (default) or ts.", []string{"js", "ts"}),
				"url":     str("Download the source from this http(s) URL."),
				"catalog": str("Id of a built-in example plugin, e.g. calculator, weather."),
				"enable":  boolean("Enable right away (default true for new plugins; updates keep their state)."),
			}),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var req plugin.InstallRequest
				if err := decode(args, &req); err != nil {
					return nil, err
				}
				c, cancel := ctx(30 * time.Second)
				defer cancel()
				inf, err := m.Install(c, req)
				if err != nil {
					return nil, err
				}
				return M{"plugin": inf}, nil
			},
		},
		{
			name: "preview_plugin", title: "Preview plugin", readOnly: true, idempotent: true,
			desc: "Validate plugin source (or an http(s) URL / built-in catalog id) WITHOUT installing it: nothing is written and no hook runs. Returns {plugin (manifest derived info: permissions, widgets, config schema...), source, lang, installed, installed_version, error}. Use it to check a draft or to review a third-party plugin's permissions and code before install_plugin." + pluginDocs,
			schema: obj(M{
				"source":  str("Plugin source code (ES module)."),
				"lang":    enumStr("Language of source/url: js (default) or ts.", []string{"js", "ts"}),
				"url":     str("Download the source from this http(s) URL."),
				"catalog": str("Id of a built-in example plugin."),
			}),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var req plugin.InstallRequest
				if err := decode(args, &req); err != nil {
					return nil, err
				}
				c, cancel := ctx(30 * time.Second)
				defer cancel()
				res, err := m.Preview(c, req)
				if err != nil {
					return nil, err
				}
				if res.Error != "" {
					return isErr{res}, nil
				}
				return res, nil
			},
		},
		{
			name: "get_plugin_source", title: "Get plugin source", readOnly: true, idempotent: true,
			desc:   "Return the source code and language of an installed plugin ({source, lang}). Pair with update_plugin_source to edit it. 读取已安装插件的源码。",
			schema: obj(M{"id": str("Plugin id.")}, "id"),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				src, lang, err := m.Source(a.ID)
				if err != nil {
					return nil, err
				}
				return M{"source": src, "lang": lang}, nil
			},
		},
		{
			name: "update_plugin_source", title: "Update plugin source", idempotent: true,
			desc: "Replace the source of an installed plugin. manifest.id must stay the same; enabled state, config and widget instances are kept. Validation errors include line:column. Run test_plugin afterwards. 更新已安装插件的源码。",
			schema: obj(M{
				"id":     str("Plugin id."),
				"source": str("New plugin source code (ES module)."),
				"lang":   enumStr("js or ts (default: keep the current language).", []string{"js", "ts"}),
			}, "id", "source"),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var a struct {
					ID     string `json:"id"`
					Source string `json:"source"`
					Lang   string `json:"lang"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				inf, err := m.UpdateSource(a.ID, a.Source, a.Lang)
				if err != nil {
					return nil, err
				}
				return M{"plugin": inf}, nil
			},
		},
		{
			name: "update_plugin", title: "Update plugin", idempotent: true,
			desc: "Enable/disable a plugin or change its configuration. config is merged into the current config: given keys change, null removes a key (for secret fields null clears the secret, an empty string keeps it). Only keys declared in the manifest config are stored. 启用/禁用插件或修改配置（合并更新）。",
			schema: obj(M{
				"id":      str("Plugin id."),
				"enabled": boolean("Enable or disable."),
				"config":  cfgObj("Plugin-level config values to change."),
			}, "id"),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var a struct {
					ID string `json:"id"`
					plugin.PatchRequest
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				a.Merge = true
				inf, err := m.Patch(a.ID, a.PatchRequest)
				if err != nil {
					return nil, err
				}
				return M{"plugin": inf}, nil
			},
		},
		{
			name: "uninstall_plugin", title: "Uninstall plugin", destructive: true,
			desc:   "Uninstall a plugin with its widget instances, stored data and downloaded wallpapers; workspaces using its wallpaper fall back to the default. Only when the user asked. 卸载插件（同时删除其小组件和数据）。",
			schema: obj(M{"id": str("Plugin id.")}, "id"),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				if err := m.Uninstall(a.ID); err != nil {
					return nil, err
				}
				return M{"ok": true, "uninstalled": a.ID}, nil
			},
		},
		{
			name: "test_plugin", title: "Test plugin", readOnly: false,
			desc: "Dry-run one plugin hook and return {ok, result, error, logs, ms}. result is the sanitized value the UI would use (nothing is cached or downloaded). Use it to debug a plugin after install_plugin. widget/action use the config given here (or the saved instance with `instance`)." + pluginDocs,
			schema: obj(M{
				"id":       str("Plugin id."),
				"hook":     enumStr("Hook to run.", []string{"wallpaper", "widget", "search", "schedule", "action"}),
				"target":   str("Wallpaper source id, widget id or schedule id (default: the first one)."),
				"query":    str("Search text (hook=search)."),
				"config":   cfgObj("Widget instance config values (hook=widget/action)."),
				"instance": str("Optional saved widget instance id whose config is the base (keeps its secrets)."),
				"action":   str("Action name (hook=action)."),
				"params":   M{"type": []string{"object", "array", "string", "number", "boolean", "null"}, "description": "Action params (hook=action)."},
			}, "id", "hook"),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var a struct {
					ID string `json:"id"`
					plugin.TestRequest
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				c, cancel := ctx(90 * time.Second)
				defer cancel()
				res, err := m.Test(c, a.ID, a.TestRequest)
				if err != nil {
					return nil, err
				}
				if !res.OK {
					return isErr{res}, nil
				}
				return res, nil
			},
		},
		{
			name: "plugin_logs", title: "Plugin logs", readOnly: true, idempotent: true,
			desc:   "Return the last 200 log lines of a plugin (genkan.log/console output and errors). 插件最近日志。",
			schema: obj(M{"id": str("Plugin id.")}, "id"),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				logs, err := m.Logs(a.ID)
				if err != nil {
					return nil, err
				}
				return M{"logs": logs}, nil
			},
		},
		{
			name: "plugin_storage", title: "Plugin storage", readOnly: true, idempotent: true,
			desc:   "Inspect what a plugin keeps in its persistent key/value storage: {size, limit (bytes), entries: [{key, size, value, instance?}]}. Entries with `instance` belong to one widget instance (ctx.instance.storage). Plugin data can contain cached sensitive data. 查看插件的持久化存储。",
			schema: obj(M{"id": str("Plugin id.")}, "id"),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				return m.Storage(a.ID)
			},
		},
		{
			name: "clear_plugin_storage", title: "Clear plugin storage", destructive: true,
			desc: "Delete a plugin's stored data: everything (only key omitted), or one key; add instance for a key of one widget instance (see plugin_storage). The plugin starts over (e.g. cached history is lost). Only when the user asked. 清除插件的存储数据。",
			schema: obj(M{
				"id":       str("Plugin id."),
				"key":      str("Delete only this key (default: everything)."),
				"instance": str("Widget instance id when key is an instance-scoped key."),
			}, "id"),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var a struct {
					ID       string `json:"id"`
					Key      string `json:"key"`
					Instance string `json:"instance"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				if err := m.ClearStorage(a.ID, a.Key, a.Instance); err != nil {
					return nil, err
				}
				return M{"ok": true, "cleared": a.ID}, nil
			},
		},
		{
			name: "list_widgets", title: "List widgets", readOnly: true, idempotent: true,
			desc:   "List widget instances placed on the start page (id, plugin, widget kind, workspace_id (\"\" = all workspaces), config with secrets blanked, sort, w columns, h pixels). 小组件实例列表。",
			schema: obj(M{}),
			fn: func(json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				return M{"widgets": m.Present(s.Store.Snapshot().Widgets, true)}, nil
			},
		},
		{
			name: "add_widget", title: "Add widget",
			desc: "Place a widget of an installed plugin on the start page. plugin/widget come from list_plugins (plugin.widgets[].id); config follows that widget's config schema. workspace_id empty = shown in every workspace. 添加小组件。",
			schema: obj(M{
				"plugin":       str("Plugin id."),
				"widget":       str("Widget id declared by the plugin."),
				"workspace_id": str("Workspace to show it in (optional, default all)."),
				"config":       cfgObj("Instance config values."),
				"w":            M{"type": "integer", "minimum": 0, "maximum": 6, "description": "Width in grid columns of 6 (0 = default from the widget's size: s=2, m=4, l=6)."},
				"h":            M{"type": "integer", "minimum": 0, "maximum": 1200, "description": "Height in pixels (0 = automatic by content, else 80..1200)."},
			}, "plugin", "widget"),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var in plugin.WidgetAdd
				if err := decode(args, &in); err != nil {
					return nil, err
				}
				w, err := m.AddWidget(in)
				if err != nil {
					return nil, err
				}
				return M{"widget": w}, nil
			},
		},
		{
			name: "update_widget", title: "Update widget", idempotent: true,
			desc: "Change a widget instance: workspace, order, size (w columns 0-6, h pixels) or config (merged: given keys change, null removes a key). 修改小组件。",
			schema: obj(M{
				"id":           str("Widget instance id."),
				"workspace_id": str("Workspace id, or \"\" for all workspaces."),
				"config":       cfgObj("Instance config values to change."),
				"sort":         M{"type": "integer", "description": "Display order."},
				"w":            M{"type": "integer", "minimum": 0, "maximum": 6, "description": "Width in grid columns of 6 (0 = default from the widget's size)."},
				"h":            M{"type": "integer", "minimum": 0, "maximum": 1200, "description": "Height in pixels (0 = automatic, else 80..1200)."},
			}, "id"),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var a struct {
					ID string `json:"id"`
					plugin.WidgetPatch
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				a.Merge = true
				w, err := m.UpdateWidget(a.ID, a.WidgetPatch)
				if err != nil {
					return nil, err
				}
				return M{"widget": w}, nil
			},
		},
		{
			name: "remove_widget", title: "Remove widget", destructive: true,
			desc:   "Remove a widget instance from the start page. 移除小组件。",
			schema: obj(M{"id": str("Widget instance id.")}, "id"),
			fn: func(args json.RawMessage) (any, error) {
				m, err := mgr()
				if err != nil {
					return nil, err
				}
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(args, &a); err != nil {
					return nil, err
				}
				if err := m.DeleteWidget(a.ID); err != nil {
					return nil, err
				}
				return M{"ok": true, "removed": a.ID}, nil
			},
		},
	}
}
