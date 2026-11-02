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
| `wallpaper` | wallpaper root; attr `data-wallpaper` = current value (`preset:aurora`, `upload:x.jpg`) |
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
| `link-modal` | `link-url`, `link-title`, `link-desc`, `link-workspace`, `link-category` (selects), `icon-mode` + `icon-mode-auto/emoji/upload/url/letter`, `link-icon-emoji`, `link-icon-file` (file input), `link-icon-url`, `link-refresh-icon`, `link-pinned` (switch, `aria-checked`), `link-health` (switch), `link-save`, `link-cancel` |
| `category-modal` | `category-name`, `category-save`, `category-cancel` |
| `workspace-modal` | `ws-name`, icon picker: `ws-icon` (the `role=radiogroup` grid, attr `data-value=lucide:<name>`), `ws-icon-<name>` (one `role=radio` button per Lucide name, `aria-checked`), `ws-icon-search` (filter input), `ws-icon-preview`; wallpaper gallery (below); `ws-save`, `ws-cancel`, `ws-delete` (edit only). The old emoji text input `ws-icon` is gone |
| `confirm-modal` | `confirm-ok`, `confirm-cancel`, `confirm-checkbox` (category delete: "also delete links") |
| `settings-modal` | tabs `settings-tab-general/appearance/content/ai/security/data/about` (order as listed; `mcp` was renamed `ai`), panel `settings-panel-<tab>` |

Stacked modals: editors / confirms opened from inside Settings (content manager, gallery delete) sit on top of Settings; closing them
returns to Settings on the same tab.

Wallpaper gallery (`wp-picker`; Settings → 外观 and the workspace editor): `wp-presets`, `wp-preset-aurora|dusk|ocean|forest|sakura|graphite`
(`aria-pressed`), `wp-gallery` (my gallery grid, also a file drop target), `gallery-item-<file>` (`data-value=upload:<file>`, `aria-pressed`),
`gallery-delete-<file>` (Settings only; opens `confirm-modal`; visible on hover/focus, always on touch), `wp-upload` (tile `<label>`),
`wp-upload-input` (`multiple` file input), `wp-upload-progress` (`done/total` while uploading), `wp-apply-all` (Settings → 外观).
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
- General: `set-title`, `set-lang` (`zh`/`en`), `set-clock`, `set-newtab` (switches), `set-engine`, `set-sort`, `set-health`
  (text inputs commit on change/blur/Enter).
- Security: `set-guest` (switch), `pw-old`, `pw-new`, `pw-save`, `pw-error`.
- AI (`settings-tab-ai`, panel `settings-panel-ai`): `token-card`, `token-value` (attr `data-masked` = `1`/`0`; masked text is
  `gk_••••••••…last4`), `token-toggle` (show/hide, `aria-pressed`), `token-copy`, `token-regenerate` (icon button, aria-label only) →
  inline `token-confirm` with `token-regenerate-confirm` / `token-regenerate-cancel`, `mcp-warning` (the single amber warning line).
  Connect: `connect-tabs` (`role=tablist`), `connect-skill|claude|mcp` (`role=tab`, `aria-selected`; only the selected pane is rendered),
  panes `pane-skill|claude|mcp`. Skill pane: `skill-download` (`<a href=/api/skill download>`). Claude pane: `mcp-command` + `copy-mcp-command`.
  Other-MCP pane: `mcp-json` + `copy-mcp-json`. Snippets are masked while the token is masked; copy buttons (icon-only, check for 1.5 s)
  always copy the real value. NOTE: `mcp-command`/`mcp-json` only exist when their pane is selected (default pane is Skill).
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
