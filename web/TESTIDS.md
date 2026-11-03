# Stable `data-testid` hooks

`<id>` = the 12-char object id. Everything is plain `data-testid`
(Playwright: `page.getByTestId(...)`, or `[data-testid=...]`).

## Auth / app shell
| testid | element |
|---|---|
| `login-form`, `login-password`, `login-submit`, `auth-error` | login screen |
| `setup-form`, `setup-password`, `setup-confirm`, `setup-submit` | first-run setup (error also `auth-error`) |
| `login-back` | "back to start page" (guest who opened the login screen) |
| `login-btn` | "Log in" item in the main menu (guest mode only; the only item a guest sees) |
| `logout-btn` | "Sign out" item in the main menu (admin) |
| `retry-btn` | shown when bootstrap fails |
| `wallpaper` | wallpaper root; attr `data-wallpaper` = current value (`preset:dusk` by default, `upload:x.jpg`) |
| `toast-container`, `toast` | toasts; `toast` has `data-kind` = `info`/`error`/`success` |

## Top bar: workspace dropdown (top-left) and main menu (top-right)
Both are glass dropdowns: trigger button `aria-haspopup="menu"` + `aria-expanded`; the menu element only exists while open
(`role=menu`). Esc / outside click / selection close it (focus returns to the trigger); ArrowUp/Down/Home/End move, Enter selects.

| testid | element |
|---|---|
| `ws-menu-btn` | workspace capsule (current Lucide icon + name + chevron); attr `data-ws-name` = current workspace name |
| `ws-menu` | the open workspace menu |
| `ws-item-<id>` | workspace row (`role=menuitemradio`, `aria-checked` = current, `data-ws-name`; a `1`-`9` shortcut hint is shown) |
| `ws-up-<id>`, `ws-down-<id>` | move up / down (edit mode only; `aria-disabled` at the ends; workspaces are reordered with these, not by dragging) |
| `ws-edit-<id>` | pencil: opens the workspace editor (edit mode only; right-click on a row does the same) |
| `ws-add` | "New workspace" (admin, ALWAYS present: not only in edit mode) |
| `ws-manage` | "Manage workspaces…" (admin, last item; opens Settings → 内容 tab) |
| `edit-toggle` | edit mode toggle, leftmost of the top-right group (`aria-pressed`; admin only) |
| `ai-btn` | AI button (bot icon, `aria-label="AI"`, admin only): opens Settings on the AI tab; between the edit toggle and the main menu button |
| `menu-btn` | main menu button (sliders icon) |
| `main-menu` | the open main menu |
| `sort-control` | the "Sort" `role=group` inside the main menu (admin only) |
| `sort-frequency`, `sort-recent`, `sort-custom` | sort items (`role=menuitemradio`, `aria-checked`) |
| `settings-btn`, `logout-btn` | settings / sign out items (admin); guests get `login-btn` only |
| `clock` | clock block |

## Search
`search-input`, `search-results` (dropdown, `role=listbox`), `search-result` (one per hit,
attr `data-link-id`, `aria-selected` marks the keyboard selection), `search-web` ("search the web" row,
always last and the only row when nothing matches).
Plugin search groups sit between the link hits and `search-web`: `search-plugin-group` (attr `data-plugin`, `role=group`, header = plugin name),
`plugin-search-result` (attr `data-plugin`, `data-copy="1"` for copy-only results, `aria-selected`; Enter opens `url` or copies `copy` and shows a toast).
They take part in ArrowUp/Down selection. Requests (`GET /api/plugins/search?q=`) are debounced 250 ms and only sent while an enabled plugin has `search`.
Shortcuts: `/`, `Ctrl/Cmd+K` focus, `ArrowUp/Down`, `Enter`, `Esc`, digits `1-9` switch workspace (the dropdown shows the hint).

## Board
| testid | element |
|---|---|
| `board` | main area (`data-editing="1"` in edit mode) |
| `empty-state`, `empty-add-link`, `empty-mcp` | empty workspace |
| `section-pinned` | pinned row at the top, NO heading (shown if any pinned link, or in edit mode). The `section-pinned-title` testid no longer exists |
| `category-<id>` (+ `category-<id>-title`) | category section (`h2` is the title) |
| `category-uncategorized` | uncategorized section (title only shown when categories/pinned exist) |
| `link-<id>` | link capsule in its category / uncategorized section (`<a>` in normal mode; `<div>` in edit mode; attr `data-link-id`) |
| `pin-link-<id>` | the SAME link's capsule in the pinned row (pinned links appear in both places; every testid of the pinned copy has a `pin-` prefix so ids stay unique: `pin-health-<id>`, `pin-link-edit-<id>`, `pin-link-delete-<id>`) |
| `health-<id>` (`pin-health-<id>` in the pinned row) | health dot, attr `data-status` = `up`/`down`/`unknown` (only rendered if `health_check`) |
| `link-edit-<id>`, `link-delete-<id>` | small buttons on a capsule (edit mode; `pin-` prefixed in the pinned row) |
| `pinned-add-link`, `category-add-link-<id>`, `uncat-add-link` | "+" capsule at the end of a section (edit mode) |
| `new-category-btn` | new category (edit mode; the only thing in the board toolbar) |
| `category-handle-<id>`, `category-rename-<id>`, `category-delete-<id>` | category tools (edit mode) |

Capsule layout: one category per row (full width). A link with a description renders `.cap-desc` (second line); sections
containing any description use taller capsules.
Drag & drop semantics: dropping a category capsule on the pinned row pins it (PATCH `pinned:true`, then reorder `__pinned`);
reordering inside the pinned row only reorders `__pinned` (`pin_sort`); dragging a pinned-row capsule into a category does nothing.

Drag & drop: pointer events (mouse: >5px move; touch: ~260ms long press). Items carry
`data-dnd-kind` (`link`/`category`) + `data-dnd-item=<id>`; link containers carry
`data-dnd-container` (`pinned`, `uncat` or the category id). A pinned link has TWO elements with the same `data-dnd-item`. Category drag starts on `category-handle-<id>`.
Use `page.mouse.down/move(steps)/up`. First drag while sort mode != custom switches to custom and shows a toast.

## Modals
All modals: `modal-close` (X) ; Esc closes.
| modal testid | fields / buttons |
|---|---|
| `link-modal` | `link-url`, `link-title`, `link-desc`, `link-workspace`, `link-category` (selects), `icon-mode` + `icon-mode-auto/emoji/lucide/upload/url/letter`, `link-icon-emoji`, Lucide picker (mode `lucide`, the shared `IconPicker`): `link-icon-lucide` (the `role=radiogroup` grid, attr `data-value=lucide:<name>` or empty), `link-icon-lucide-<name>` (one `role=radio` per name, `aria-checked`), `link-icon-lucide-search`; the preview tile and capsules draw the glyph as `svg[data-icon=<name>]` inside `.ico-lucide`, `link-icon-file` (file input), `link-icon-url`, `link-refresh-icon`, `link-pinned` (switch, `aria-checked`), `link-health` (switch), `link-save`, `link-cancel` |
| `category-modal` | `category-name`, `category-save`, `category-cancel` |
| `workspace-modal` | `ws-name`, icon picker: `ws-icon` (the `role=radiogroup` grid, attr `data-value=lucide:<name>`), `ws-icon-<name>` (one `role=radio` button per Lucide name, `aria-checked`), `ws-icon-search` (filter input), `ws-icon-preview`; wallpaper gallery (below); `ws-save`, `ws-cancel`, `ws-delete` (edit only). The old emoji text input `ws-icon` is gone |
| `confirm-modal` | `confirm-ok`, `confirm-cancel`, `confirm-checkbox` (category delete: "also delete links") |
| `settings-modal` | tabs `settings-tab-general/appearance/content/plugins/ai/security/data/about` (order as listed; `mcp` was renamed `ai`), panel `settings-panel-<tab>` |

Stacked modals: editors / confirms opened from inside Settings (content manager, gallery delete) sit on top of Settings; closing them
returns to Settings on the same tab.

Wallpaper gallery (`wp-picker`; Settings → 外观 and the workspace editor): `wp-presets`, `wp-preset-aurora|dusk|ocean|forest|sakura|graphite`
(`aria-pressed`), `wp-gallery` (my gallery grid, also a file drop target), `gallery-item-<file>` (`data-value=upload:<file>`, `aria-pressed`),
`gallery-delete-<file>` (Settings only; opens `confirm-modal`; visible on hover/focus, always on touch), `wp-upload` (tile `<label>`),
`wp-upload-input` (`multiple` file input), `wp-upload-progress` (`done/total` while uploading), `wp-apply-all` (Settings → 外观). Plugin sources (enabled plugins only): grid `wp-plugins`, tile `wp-plugin-<pluginId>-<sourceId>` (`data-value=plugin:<id>/<source>`, `aria-pressed`).
The wallpaper root keeps `data-wallpaper=plugin:<id>/<source>`; the plugin's credit shows as `wallpaper-credit` (bottom-left; absent for presets / uploads / when falling back to the default).
Uploading exactly one file also applies it; several files only add them to the gallery. `wp-current-upload` was removed.

## Context menus (`role=menu`, closes on Esc / outside press / scroll / resize; ArrowUp/Down/Home/End; first item focused on open)
| testid | element |
|---|---|
| `ctx-menu-page` | right-click on the page background (not on inputs / text selections / capsules) |
| `ctx-new-link`, `ctx-new-category`, `ctx-wallpaper` (→ Settings 外观), `ctx-edit-mode` (`menuitemcheckbox`, `aria-checked`), `ctx-ai` (→ Settings AI), `ctx-settings` | admin items of the page menu |
| `ctx-login` | the only item a guest gets on the page menu |
| `ctx-menu-link` | right-click on a capsule (desktop popup) / long-press ≈500 ms on touch (bottom sheet at ≤640px, which has no focused item); long-press does not navigate; in edit mode touch long-press is drag |
| `ctx-open` (same tab), `ctx-open-new`, `ctx-copy-link` | everyone |
| `ctx-edit`, `ctx-pin` (label 收藏 / 取消收藏), `ctx-delete` (opens `confirm-modal`) | admin only |

## Settings panels
- General: `set-title`, `set-lang` (`auto`/`zh`/`en`; default `auto`), `set-clock`, `set-newtab` (switches), `set-engine`, `set-sort`, `set-health`
  (text inputs commit on change/blur/Enter).
- Security: `set-guest` (switch), `pw-old`, `pw-new`, `pw-save`, `pw-error`.
- AI (`settings-tab-ai`, panel `settings-panel-ai`): `token-card`, `token-value` (attr `data-masked` = `1`/`0`; masked text is
  `gk_••••••••…last4`), `token-toggle` (show/hide, `aria-pressed`), `token-copy`, `token-regenerate` (icon button, aria-label only) →
  inline `token-confirm` with `token-regenerate-confirm` / `token-regenerate-cancel`, `mcp-warning` (the single amber warning line).
  Connect: `connect-tabs` (`role=tablist`), `connect-skill|mcp` (`role=tab`, `aria-selected`; only the selected pane is rendered),
  panes `pane-skill|mcp`. Skill pane (prompt first): `skill-prompt` (the ready-to-paste setup prompt, wrapping `<pre>`; collapsed to a 3-line preview by default, `data-collapsed="true"`) + `skill-prompt-toggle` (expand/collapse, `aria-expanded`) + `copy-skill-prompt` (always copies the full prompt). MCP pane:
  `mcp-command` + `copy-mcp-command` (Claude Code) and `mcp-json` + `copy-mcp-json` (generic client). Prompt and snippets are masked
  while the token is masked; copy buttons (icon-only, check for 1.5 s) always copy the real value. Below it a quiet manual-install row `skill-manual` with the package download link `skill-download` (`<a href=/api/skill download>`; the zip contains the token).
  NOTE: `mcp-command`/`mcp-json` only exist while the MCP pane is selected (default pane is Skill).
- Content (`settings-tab-content`): `content-manager`, `cm-search` (+ `cm-search-clear`), `cm-results` / `cm-result-<linkId>` / `cm-no-results`
  (cross-workspace search, rows show `workspace › category`), workspace list `cm-ws-list`, `cm-ws-<id>` (select; `aria-current`), `cm-ws-add`;
  selected workspace: `cm-ws-title`, `cm-ws-edit`, `cm-ws-up`, `cm-ws-down`, `cm-ws-delete`, `cm-cat-add`, `cm-link-add`;
  category groups `cm-cat-<id|uncat>` with `cm-toggle-<id|uncat>` (`aria-expanded`), `cm-cat-rename-<id>`, `cm-cat-up-<id>`, `cm-cat-down-<id>`,
  `cm-cat-delete-<id>`, `cm-add-link-<id|uncat>`; link rows `cm-link-<id>` with `cm-pin-<id>` (star, `aria-pressed`), `cm-link-edit-<id>`,
  `cm-link-delete-<id>`. Edit / delete actions open the usual modals (`link-modal`, `category-modal`, `workspace-modal`, `confirm-modal`) over Settings.
- Data: `export-btn` (`<a href=/api/export download>`), `import-mode`, `import-mode-merge`, `import-mode-replace`,
  `import-file` (file input), `import-btn`.
- About: `about-version` (server build version from `/api/bootstrap` `server_version`; `dev` for unversioned builds), `about-revision` (data revision).

## Behaviours worth knowing
- Every `<svg>` icon carries `data-icon=<lucide name>` (e.g. `svg[data-icon="house"]`), so tests can tell which icon is drawn.
- The content manager's search text lives in a store signal (`cmQuery`), so it survives the editors stacked on top of Settings.
- Language: `localStorage['genkan.lang']` (`zh`|`en`), default from `navigator.language`. Current workspace: `localStorage['genkan.ws']`.
- Menus and `toggleEdit`/`openSettings`-style helpers live in `e2e/helpers.ts` (`openWsMenu`, `switchWs`, `openMainMenu`, `mainMenuItem`, `openSettings`, `setSort`, `expectSort`).
- Link capsules are real anchors (`href`, `target=_blank` when `open_in_new_tab`), click sends `navigator.sendBeacon(/api/links/<id>/click)` (admin only).
- Keyboard-opened results use `window.open(url,'_blank','noopener')` (or `location.assign` if `open_in_new_tab=false`).

## Plugins and widgets (docs/PLUGINS.md)
**Settings → Plugins** (`settings-tab-plugins`, panel root `plugins-tab`): `plugin-add-btn`, `plugin-empty` / `plugin-empty-add`, list `plugin-list`.
Row `plugin-row` (attrs `data-plugin-id`, `data-enabled=1|0`) contains `plugin-name`, `plugin-badge` (attr `data-kind=wallpaper|widget|search|schedule`),
`plugin-error` (last error), `plugin-toggle` (switch, `aria-checked`), `plugin-config-btn`, `plugin-logs-btn` (`aria-pressed`, one panel open at a time),
`plugin-update` (only with `source_url`: re-installs from it), `plugin-delete` (opens `confirm-modal`).
Config panel `plugin-config-panel`: generated fields `plugin-cfg-<key>` (switch for boolean; secret: `data-set=1|0`, `plugin-cfg-secret-clear-<key>` toggles "clear"), inline errors `plugin-cfg-error-<key>`,
`plugin-config-save`, `plugin-permissions` (hosts, `*` has the warning style). Logs panel `plugin-logs-panel` with `plugin-logs` (`<pre>`) and `plugin-logs-refresh`.
Secrets: the input is empty; blank = keep (sent as `""`), "clear" sends `null`.

**Add plugin** (`plugin-add`): `plugin-add-back`, tabs `plugin-add-tab-catalog|url|source` (group `plugin-add-tabs`).
Catalog `plugin-catalog` with `plugin-catalog-item` (attrs `data-plugin-id`, `data-installed`) + `plugin-catalog-install` (disabled when installed).
URL: `plugin-url-input`, `plugin-url-install`. Source: `plugin-source-input` (textarea), `plugin-source-lang` (`js`/`ts`), `plugin-source-install`.

**Install flow** (permission confirmation): every install is POSTed with `enable:false`, so no plugin code runs before review. If the manifest asks for
fetch hosts, `links` access or has `url` fields with `allowFetch`, the `plugin-perm-modal` (stacked over Settings) lists them:
`plugin-perms`, `plugin-perm-host` (one per host), `plugin-perm-any` (the `*` entry, warning style), `plugin-perm-urlfields`, `plugin-perm-links` (`data-level=read|write`);
`plugin-perm-allow` PATCHes `enabled:true`, `plugin-perm-cancel` leaves it installed but disabled. Plugins requesting nothing (e.g. calculator) are enabled immediately with a toast,
and so is an update of an enabled plugin whose permissions did not change. After enabling a plugin with required settings its config panel opens.

**Widget area** (`widget-area`, between the hero and `board`; absent when the workspace has no visible widget, unless admin edit mode; `data-editing=1` in edit mode):
`widget` (attrs `data-widget-id`, `data-plugin`, `data-widget`, `data-size=s|m|l`, `aria-busy`), `widget-title`, `widget-skeleton` (while the first load runs),
`widget-error` + `widget-retry`, `widget-refresh` (admin, hover). Instances of disabled plugins are hidden (edit mode: shown dashed with an error, only move/delete).
Block testids: `widget-text`, `widget-stat`, `widget-stats`, `widget-list` / `widget-list-item`, `widget-kv`, `widget-progress`, `widget-image`, `widget-chart` (attr `data-kind=spark|bar`),
`widget-row`, `widget-button` (admin only, attr `data-action`; calls the action endpoint, toasts the result, `refresh:true` forces a reload). A divider is a bare `hr.wg-hr`.
Edit mode (admin): `widget-add` (dashed card), per card `widget-prev-<id>`, `widget-next-<id>` (reorder, no dragging), `widget-edit-<id>`, `widget-delete-<id>` (opens `confirm-modal`).
Data polling: each instance refetches at the widget's `refresh` seconds (min 30); paused while `document.hidden`, refetched on return when stale.

`widget-modal`: picker `widget-picker` with `widget-pick-<pluginId>-<widgetId>` (or `widget-none` + `widget-open-plugins` when no plugin provides widgets) -> form `widget-form`:
`widget-cfg-<key>` fields (+ `widget-cfg-error-<key>`, `widget-cfg-secret-clear-<key>`), `widget-workspace` (select; empty value = all workspaces), `widget-back`, `widget-cancel`, `widget-save`.
