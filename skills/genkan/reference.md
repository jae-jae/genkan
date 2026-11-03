# Genkan REST API reference

Complete reference for what the API token can do on the user's Genkan instance. Part of the Genkan Skill: see [SKILL.md](SKILL.md) for the credentials setup, the `gk` helper, the workflow and good practice.

> **WARNING: the API token (`$GENKAN_TOKEN`) is full administrator access.** Never print, share or commit it.

## Conventions

- Base URL: `$GENKAN_URL`. Every request sends `Authorization: Bearer $GENKAN_TOKEN`; Bearer requests need no CSRF header. Both values come from the environment, `~/.config/genkan/env` or the `.env` next to SKILL.md (see SKILL.md).
- Bodies and responses are JSON (`Content-Type: application/json`), except file uploads (multipart). Request bodies are limited to 1 MiB (import: 32 MiB). Ids are 12-character lowercase strings. Timestamps are RFC 3339 strings, `null` when unset.
- Examples use the `gk METHOD PATH [JSON_BODY]` helper from SKILL.md and `jq`. A body starting with `@` is read from that file by curl. The examples run top to bottom and share the shell variables they set (`WS`, `CAT`, `LINK`, ...); each section creates what it needs and cleans up its throwaway objects.
- Unknown JSON fields are ignored silently, so check spelling and re-read the object to verify a change.
- Errors: `{"error":"message"}` with a status code:

| Status | Meaning |
|---|---|
| 400 | Invalid input (empty or too long name, bad URL, unknown icon name or wallpaper, malformed JSON, bad `mode`...) |
| 401 | Missing or wrong token (`GET /api/bootstrap` is public: with a wrong token it answers 200 with `"role":"none"` and empty lists, so check that `auth.role` is `admin`) |
| 404 | Unknown id or file |
| 409 | Conflict (deleting the last workspace, using a disabled plugin) |
| 413 | Body or upload too large |
| 502 / 504 / 503 | Icon refresh failed or a plugin hook failed / a plugin hook timed out / icon fetching disabled |

```bash
gk POST /api/workspaces '{"name":"Oops","icon":"lucide:nope"}'   # HTTP 400, nothing is created
```

```json
{"error":"workspace icon must be lucide:<name> with a built-in icon name (e.g. lucide:house)"}
```

Not covered on purpose, never call them: login, logout, setup and password endpoints, `GET /api/token` and `POST /api/token/regenerate` (**never regenerate the token: it invalidates the stored token and locks you out until the user sends you a new setup prompt**), `GET /api/events` (live-update stream), `POST /api/links/{id}/click` (usage counter).

## Objects

**Settings**

| Field | Type | Notes |
|---|---|---|
| `title` | string | Site title, 1-60 characters (default `Genkan`) |
| `sort_mode` | string | `frequency` (default), `recent` or `custom` |
| `guest_access` | boolean | Allow read-only viewing without login (default false). Changing it exposes the whole page: only on request |
| `search_engine` | string | http(s) URL containing `%s` (default `https://www.google.com/search?q=%s`) |
| `open_in_new_tab` | boolean | Open links in a new tab (default true) |
| `health_interval` | integer | Seconds between health checks, clamped to 15-86400 (default 60) |
| `show_clock` | boolean | Show the clock (default true) |

**Workspace**: `{"id","name","icon","wallpaper","sort"}`. `name` 1-40 characters. `icon` is `lucide:<name>` (see the icon list below; empty on create = `lucide:layout-grid`). `wallpaper` is `preset:<id>` or `upload:<file>` (empty on create = `preset:dusk`). `sort` is the position among workspaces.

**Category**: `{"id","workspace_id","name","sort"}`. `name` 1-60 characters; `sort` is the position within the workspace.

**Link**

| Field | Notes |
|---|---|
| `id`, `workspace_id` | `workspace_id` is required on create |
| `category_id` | `""` = uncategorized (shown last); must belong to the same workspace |
| `title` | 1-120 characters, required |
| `url` | Required, up to 2048 characters. A bare `host:port/path` gets `http://`. `javascript:`, `data:`, `vbscript:`, `blob:`, `file:` and `about:` are rejected |
| `description` | Up to 500 characters (newlines allowed) |
| `icon` | See "Icon formats". Empty = initials badge, replaced by the real favicon once fetched |
| `pinned` | Shown in the pinned row of its workspace AND still in its category |
| `sort` | Position inside its category (used in `custom` sort mode) |
| `pin_sort` | Position inside the pinned row (only meaningful while pinned) |
| `health_check` | Show an online/offline dot, checked every `health_interval` seconds |
| `clicks`, `last_used_at`, `created_at`, `updated_at` | Read-only |

New objects get `sort` = last + 1 in their group. A link that becomes pinned gets `pin_sort` = last + 1 in the pinned row of its workspace.

## Endpoints

### GET /api/bootstrap

Everything in one response. Call it first and again to verify changes.

Response: `{"auth":{"setup_required":false,"role":"admin"},"settings":{...},"workspaces":[...],"categories":[...],"links":[...],"health":{"<link id>":{...}},"plugins":[...],"widgets":[...],"version":42,"server_version":"v0.1.0"}`. `version` increases with every change. `plugins` and `widgets` are described under Plugins below.

```bash
gk GET /api/bootstrap | jq '{role: .auth.role, version, workspaces: [.workspaces[] | {id,name,icon,wallpaper}]}'
WS=$(gk GET /api/bootstrap | jq -r '.workspaces[0].id')
```

### PATCH /api/settings

Body: any subset of the Settings fields. Response: the full Settings object. 400 for an invalid `sort_mode`, `search_engine` or title.

```bash
gk PATCH /api/settings '{"sort_mode":"custom","health_interval":30}'
```

```json
{"title":"Genkan","sort_mode":"custom","guest_access":false,"search_engine":"https://www.google.com/search?q=%s","open_in_new_tab":true,"health_interval":30,"show_clock":true}
```

### POST /api/workspaces

Body: `{"name","icon"?,"wallpaper"?}`. Response: the Workspace.

```bash
LAB=$(gk POST /api/workspaces '{"name":"Lab","icon":"lucide:flask-conical","wallpaper":"preset:ocean"}' | jq -r .id)
```

```json
{"id":"k3x9m2q7a1bz","name":"Lab","icon":"lucide:flask-conical","wallpaper":"preset:ocean","sort":1}
```

### PATCH /api/workspaces/{id}

Body: any subset of `name`, `icon`, `wallpaper`. Response: the Workspace.

```bash
gk PATCH /api/workspaces/$LAB '{"name":"Playground","icon":"lucide:rocket","wallpaper":"preset:forest"}'
```

### POST /api/workspaces/reorder

Body: `{"ids":[...]}` in the desired order. Listed workspaces come first; unlisted ones follow in their old order. Response: `{"ok":true}`.

```bash
gk POST /api/workspaces/reorder "$(jq -n --arg a "$LAB" --arg b "$WS" '{ids:[$a,$b]}')"
gk POST /api/workspaces/reorder "$(jq -n --arg a "$WS" --arg b "$LAB" '{ids:[$a,$b]}')"
```

### DELETE /api/workspaces/{id}

Deletes the workspace together with its categories and links (irreversible). The last remaining workspace cannot be deleted (409). Response: `{"ok":true}`.

```bash
gk DELETE /api/workspaces/$LAB
```

### POST /api/categories

Body: `{"workspace_id","name"}`. Response: the Category.

```bash
CAT=$(gk POST /api/categories "$(jq -n --arg ws "$WS" '{workspace_id:$ws,name:"Media"}')" | jq -r .id)
CAT2=$(gk POST /api/categories "$(jq -n --arg ws "$WS" '{workspace_id:$ws,name:"Monitoring"}')" | jq -r .id)
```

```json
{"id":"p4t8w1n6c0dy","workspace_id":"dkulsd564ewp","name":"Media","sort":0}
```

### PATCH /api/categories/{id}

Body: `{"name"}` (rename). Response: the Category.

```bash
gk PATCH /api/categories/$CAT2 '{"name":"Observability"}'
```

### POST /api/categories/reorder

Body: `{"workspace_id","ids":[...]}`. Listed categories come first; unlisted ones follow. Response: `{"ok":true}`.

```bash
gk POST /api/categories/reorder "$(jq -n --arg ws "$WS" --arg a "$CAT2" --arg b "$CAT" '{workspace_id:$ws,ids:[$a,$b]}')"
```

### DELETE /api/categories/{id}

Query `delete_links=1` also deletes the category's links; by default they become uncategorized (appended after existing uncategorized links). Response: `{"ok":true}`.

```bash
TMPCAT=$(gk POST /api/categories "$(jq -n --arg ws "$WS" '{workspace_id:$ws,name:"Temp"}')" | jq -r .id)
gk POST /api/links "$(jq -n --arg ws "$WS" --arg c "$TMPCAT" '{workspace_id:$ws,category_id:$c,title:"Temp link",url:"http://temp.lan",fetch_icon:false}')" > /dev/null
gk DELETE "/api/categories/$TMPCAT"
gk GET /api/bootstrap | jq '[.links[] | select(.title=="Temp link") | {title,category_id}]'
TMPCAT=$(gk POST /api/categories "$(jq -n --arg ws "$WS" '{workspace_id:$ws,name:"Temp 2"}')" | jq -r .id)
gk POST /api/links "$(jq -n --arg ws "$WS" --arg c "$TMPCAT" '{workspace_id:$ws,category_id:$c,title:"Temp link 2",url:"http://temp2.lan",fetch_icon:false}')" > /dev/null
gk DELETE "/api/categories/$TMPCAT?delete_links=1"
```

### POST /api/links

Body: `{"workspace_id","category_id"?,"title","url","description"?,"icon"?,"pinned"?,"health_check"?,"fetch_icon"?}`. Response: the Link. Does not check for duplicate URLs. When `icon` is empty the favicon is fetched in the background (the returned link still has an empty icon; it updates a moment later). Set `"fetch_icon":false` to skip that.

```bash
LINK=$(gk POST /api/links "$(jq -n --arg ws "$WS" --arg c "$CAT" '{workspace_id:$ws,category_id:$c,title:"Jellyfin",url:"http://nas.lan:8096",description:"Movies and TV",health_check:true,fetch_icon:false}')" | jq -r .id)
LINK2=$(gk POST /api/links "$(jq -n --arg ws "$WS" --arg c "$CAT2" '{workspace_id:$ws,category_id:$c,title:"Grafana",url:"http://nas.lan:3000",icon:"lucide:chart-line",pinned:true}')" | jq -r .id)
```

```json
{"id":"r7b2v5h9e3sf","workspace_id":"dkulsd564ewp","category_id":"p4t8w1n6c0dy","title":"Jellyfin","url":"http://nas.lan:8096","description":"Movies and TV","icon":"","pinned":false,"sort":0,"pin_sort":0,"clicks":0,"last_used_at":null,"health_check":true,"created_at":"2026-11-02T14:30:00Z","updated_at":"2026-11-02T14:30:00Z"}
```

### PATCH /api/links/{id}

Body: any subset of `title`, `url`, `description`, `icon`, `pinned`, `health_check`, `workspace_id`, `category_id`. Response: the Link.

- Move to another category: `{"category_id":"<id>"}` (`""` = uncategorized). The link goes to the end of the new category.
- Move to another workspace: `{"workspace_id":"<id>","category_id":"<id of a category in that workspace>"}`; without `category_id` it lands uncategorized.
- `{"pinned":true}` pins (end of the pinned row) and `{"pinned":false}` unpins; the category is never changed by pinning.
- `{"icon":""}` clears the icon (initials badge).

```bash
gk PATCH /api/links/$LINK '{"description":"Media server","pinned":true}' | jq '{title,description,pinned,pin_sort}'
gk PATCH /api/links/$LINK "$(jq -n --arg c "$CAT2" '{category_id:$c}')" | jq '{title,category_id,sort}'
gk PATCH /api/links/$LINK '{"category_id":""}' | jq '{title,category_id}'
```

### POST /api/links/reorder

Body: `{"workspace_id","category_id","ids":[...]}`. Response: `{"ok":true}`.

- Real category id or `""` (uncategorized): the listed links are moved into that category (and workspace) and get `sort` 0..n-1 in the order given; other links of that category follow in their old order. Pinned links are ordinary members of their category.
- `"category_id":"__pinned"`: reorders only the pinned row. Only `pin_sort` changes (listed links that are not pinned yet become pinned); `sort` and `category_id` are never touched, and unlisted pinned links follow in their old order. All ids must belong to `workspace_id`.

```bash
gk POST /api/links/reorder "$(jq -n --arg ws "$WS" --arg c "$CAT" --arg a "$LINK" --arg b "$LINK2" '{workspace_id:$ws,category_id:$c,ids:[$a,$b]}')"
gk POST /api/links/reorder "$(jq -n --arg ws "$WS" --arg a "$LINK2" --arg b "$LINK" '{workspace_id:$ws,category_id:"__pinned",ids:[$a,$b]}')"
gk GET /api/bootstrap | jq '[.links[] | select(.pinned) | {title,category_id,sort,pin_sort}] | sort_by(.pin_sort)'
```

### POST /api/links/{id}/refresh-icon

Fetches the link's favicon right now (synchronously, up to 8 seconds), stores it and sets `icon` to `favicon:<file>`. Response: the Link. 502 if the page or icon could not be fetched, 503 if icon fetching is disabled.

```bash
SELF=$(gk POST /api/links "$(jq -n --arg ws "$WS" --arg u "$GENKAN_URL" '{workspace_id:$ws,title:"Genkan",url:$u,fetch_icon:false}')" | jq -r .id)
gk POST /api/links/$SELF/refresh-icon | jq '{title,icon}'
```

### DELETE /api/links/{id}

Response: `{"ok":true}`, 404 if the link does not exist.

```bash
gk DELETE /api/links/$SELF
```

### GET /api/health

Latest health-check results for links with `health_check` enabled (also included in `/api/bootstrap` as `health`). Response: object keyed by link id, each `{"status":"up"|"down"|"unknown","latency_ms":12,"checked_at":"<RFC 3339>","code":200}` (`code` is the HTTP status, 0 on network error). A newly enabled link is checked within a few seconds; any HTTP status below 500 counts as `up`. Empty object when no link has checks enabled yet.

```bash
HC=$(gk POST /api/links "$(jq -n --arg ws "$WS" --arg u "$GENKAN_URL/healthz" '{workspace_id:$ws,title:"Genkan health",url:$u,health_check:true,fetch_icon:false}')" | jq -r .id)
sleep 6
gk GET /api/health | jq
gk DELETE /api/links/$HC
```

### POST /api/uploads/icon

Multipart form with a `file` field: PNG, SVG, ICO, WebP or JPEG, up to 1 MiB (the type is detected from the content). Use plain `curl -F`, not `gk`. Response: `{"value":"upload:<file>","url":"/uploads/icons/<file>"}`. The same file uploaded twice gives the same name. Put `value` into a link's `icon` field.

```bash
printf '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10" fill="#4f46e5"/></svg>' > logo.svg
ICON=$(curl -sS -X POST "$GENKAN_URL/api/uploads/icon" -H "Authorization: Bearer $GENKAN_TOKEN" -F file=@logo.svg | jq -r .value)
gk PATCH /api/links/$LINK "$(jq -n --arg i "$ICON" '{icon:$i}')" | jq '{title,icon}'
```

### POST /api/uploads/wallpaper

Same as the icon upload, but JPEG, PNG, WebP or AVIF up to 15 MiB. Response: `{"value":"upload:<file>","url":"/uploads/wallpapers/<file>"}`. Uploading only adds the image to the gallery; apply it with `PATCH /api/workspaces/{id}` and `{"wallpaper":"<value>"}`.

```bash
# wallpaper.png is any local JPEG, PNG, WebP or AVIF image
WP=$(curl -sS -X POST "$GENKAN_URL/api/uploads/wallpaper" -H "Authorization: Bearer $GENKAN_TOKEN" -F file=@wallpaper.png | jq -r .value)
gk PATCH /api/workspaces/$WS "$(jq -n --arg w "$WP" '{wallpaper:$w}')" | jq '{id,wallpaper}'
```

### GET /api/wallpapers

The uploaded wallpaper gallery, newest first. Response: `[{"value":"upload:<file>","url":"/uploads/wallpapers/<file>","size":123456,"created_at":"<RFC 3339>"}]`. The built-in presets are not listed.

```bash
gk GET /api/wallpapers | jq
```

### DELETE /api/wallpapers/{file}

Deletes an uploaded wallpaper (`<file>` is the part after `upload:`, letters, digits, `.`, `_`, `-` only) and resets every workspace that used it to `preset:dusk`. Response: `{"ok":true}`; 404 if the file does not exist.

```bash
FILE=${WP#upload:}
gk DELETE /api/wallpapers/$FILE
gk GET /api/bootstrap | jq '[.workspaces[] | {name,wallpaper}]'
```

### GET /api/export

Full backup as JSON: `{"schema":2,"settings":{...},"workspaces":[...],"categories":[...],"links":[...]}`, plus `"plugins":[...]` (with their source code) and `"widgets":[...]` when plugins are installed. Contains no secrets (plugin secret config values are left out). Uploaded image files are not included, only references to them.

```bash
gk GET /api/export > genkan-backup.json
jq '{schema, workspaces: (.workspaces|length), categories: (.categories|length), links: (.links|length)}' genkan-backup.json
```

### POST /api/import

Body: `{"mode":"merge"|"replace","data":<export JSON>}`. Response: `{"ok":true,"workspaces":2,"links":14,"plugins":0,"widgets":0}` (counts of imported items). Imported plugin code is compiled and validated first; `merge` skips plugins whose id is already installed.

- `merge`: adds everything from `data` under new ids, after the existing content; settings in `data` are ignored. Existing links are not deduplicated.
- `replace`: **deletes everything and loads `data`**, including its settings (fails with 400 if `data` has no workspaces). Only use it to restore a backup the user wants restored, and take a fresh `GET /api/export` first.

The whole import is validated first: one invalid item rejects the request (400) and changes nothing. Pass large bodies as a file (`@file`).

```bash
jq '{mode:"merge", data:.}' genkan-backup.json > import-merge.json
gk POST /api/import @import-merge.json
jq '{mode:"replace", data:.}' genkan-backup.json > import-replace.json
gk POST /api/import @import-replace.json
```

## Plugins

Plugins are single JavaScript (or TypeScript) files that run on the server and add wallpaper sources, widgets, search providers, scheduled jobs and widget button actions. The browser never runs plugin code: widgets return declarative JSON that the UI renders. The full contract (manifest, `ctx`, host API, block types, limits) is in `docs/PLUGINS.md` of the Genkan repository; read it before writing a plugin.

A plugin is an ES module (no imports) with this shape:

```js
export const manifest = {
  id: "pi-hole-count", name: "Pi-hole", version: "1.0.0",           // id: [a-z0-9][a-z0-9-]{1,39}
  permissions: { fetch: [] },                                       // hosts fetch() may reach; default deny, "*" = any host
  widgets: [{ id: "stats", name: "Queries", size: "s", refresh: 120,
    config: [{ key: "url", type: "url", label: "Pi-hole URL", required: true, allowFetch: true },   // allowFetch: this host is added to the fetch allow list
             { key: "token", type: "secret", label: "API token" }] }],
};
export async function widget(ctx) {            // ctx.instance.config, ctx.config, ctx.lang, ctx.now
  const r = await fetch(ctx.instance.config.url + "/admin/api.php?summary&auth=" + ctx.instance.config.token);
  const j = await r.json();
  return { title: "Pi-hole", body: [{ type: "stat", label: "Queries today", value: j.dns_queries_today }] };
}
```

Capabilities and the functions they need: `wallpapers` -> `wallpaper(ctx)`, `widgets` -> `widget(ctx)` (+ optional `action(ctx)` for `button` blocks), `search` -> `search(ctx)`, `schedules` -> `schedule(ctx)`. Host API inside plugins: `genkan.log()`, `genkan.storage.get/set/delete/keys` (256 KiB per plugin), `genkan.links.*` / `genkan.workspaces.list` / `genkan.categories.list` (needs `permissions.links`), standard `fetch`, timers, `URL`. Limits: 32 MiB memory, search 2 s, widget/wallpaper/action 10 s, schedule 60 s. Config field types: `string`, `text`, `number`, `boolean`, `select`, `secret`, `url`. Secret values are never returned by the API.

Write a plugin, install it, then **always run `POST /api/plugins/{id}/test`** and read `error` and `logs` before adding widgets.

### GET /api/plugins

Installed plugins (`PluginInfo`): `id, name, version, description, author, homepage, icon, enabled, permissions {fetch[], links}, config_schema[], wallpapers[], widgets[] (each with its instance config schema), search, schedules[]` plus `config` (secret values blanked), `secrets_set` (keys of secrets that have a value), `source_url`, `last_error`, `installed_at`, `updated_at`.

```bash
gk GET /api/plugins | jq '[.[] | {id, enabled, last_error}]'
```

### GET /api/plugins/catalog

Built-in example plugins that can be installed with `{"catalog":"<id>"}`: `[{id,name,description,icon,version,installed}]` (`calculator`, `weather`, `world-clock`, `countdown`, `hacker-news`, `rss`, `pihole`, `bing-wallpaper`, ...).

```bash
gk GET /api/plugins/catalog | jq -r '.[] | "\(.id)\t\(.installed)\t\(.description)"'
```

### POST /api/plugins

Installs or updates a plugin. Body: exactly one of `{"source":"<code>","lang":"js"|"ts"}`, `{"url":"https://..."}` (downloaded, max 512 KiB, http(s) only) or `{"catalog":"<id>"}`, optionally `"enable":true|false` (new plugins are enabled by default; an update keeps the current state unless `enable` is given). The plugin id comes from `manifest.id`; an existing plugin keeps its config and widgets. The manifest and the exported functions are validated: a 400 explains what to fix. Response: `PluginInfo`.

```bash
jq -n --rawfile src my-plugin.js '{source:$src}' > install.json
gk POST /api/plugins @install.json | jq '{id, enabled, permissions}'
gk POST /api/plugins '{"catalog":"calculator"}' | jq '{id, enabled}'
```

### GET /api/plugins/{id}/source

`{"source":"...","lang":"js"}`.

```bash
gk GET /api/plugins/calculator/source | jq -r .source | head -5
```

### PUT /api/plugins/{id}/source

Replaces the source of an installed plugin (edit, fix, upgrade). Body: `{"source":"<code>","lang":"js"|"ts"}` (`lang` defaults to the plugin's current language). `manifest.id` must stay `{id}`; enabled state, config, widget instances and `source_url` are kept. Validation errors are 400 and include `line:column` for syntax errors (for example `compile error: plugin.js:3:8: SyntaxError: ...`). Response: `PluginInfo`. Run `POST /api/plugins/{id}/test` afterwards.

```bash
gk GET /api/plugins/my-plugin/source | jq -r .source > my-plugin.js   # edit the file, then:
jq -n --rawfile src my-plugin.js '{source:$src}' > edit.json
gk PUT /api/plugins/my-plugin/source @edit.json | jq '{id, version, last_error}'
```

### POST /api/plugins/preview

Validates a plugin **without installing it**: nothing is written and no hook runs. Body as for install: exactly one of `{"source","lang"}`, `{"url"}` or `{"catalog"}`. Response: `{"plugin":<PluginInfo without config, secrets_set, source_url, last_error and timestamps> | null,"source":"...","lang":"js","installed":false,"installed_version":"1.0.0","error":"..."}`. `plugin` is null and `error` says what is wrong (with `line:column`) when the code does not validate or the URL cannot be downloaded; `installed_version` is only present when the same id is installed. Use it to check permissions (`plugin.permissions`) and code before installing a third-party plugin.

```bash
gk POST /api/plugins/preview '{"url":"https://raw.githubusercontent.com/owner/repo/main/plugin.js"}' | jq '{error, id: .plugin.id, permissions: .plugin.permissions, installed}'
```

### GET /api/plugins/market

Searches GitHub repositories with the topic `genkan-plugin` (third-party plugins). Query: `sort=stars|updated` (default `stars`), `q=<keywords>`, `page=<n>` (20 per page). Response: `{"items":[{repo,description,stars,updated_at,html_url,owner_avatar,default_branch}],"total":57,"cached_at":<ms>,"error":"..."}`. Cached for 10 minutes. GitHub failures (rate limit, network) come back as 200 with `error` set and no items; set the `GENKAN_GITHUB_TOKEN` environment variable on the server to raise the GitHub rate limit.

```bash
gk GET "/api/plugins/market?sort=stars&q=weather" | jq -r '.items[] | "\(.repo)\t\(.stars)\t\(.description)"'
```

### GET /api/plugins/market/repo

Scans one repository (`?repo=owner/name`, strictly validated) for plugin files and validates each one. A root `genkan-plugins.json` (`{"plugins":["path/a.js"]}`) lists the files; without it every `.js`/`.ts` file that looks like a plugin is checked (not under `node_modules`, `test(s)`, `dist` or hidden directories; at most 40). Response: `{"repo","default_branch","plugins":[{path,url,plugin:<PluginInfo>|null,error,installed,installed_version}],"cached_at":<ms>,"error":"..."}`. `url` is the raw file URL: install it with `POST /api/plugins {"url": ...}` (this records `source_url`), after reviewing it with `POST /api/plugins/preview`. Third-party code is not reviewed: check the permissions first. Cached for 10 minutes; failures set `error`.

```bash
gk GET "/api/plugins/market/repo?repo=owner/genkan-weather" | jq '.plugins[] | {path, id: .plugin.id, error, installed}'
```

### POST /api/plugins/market/cache/clear

Empties the server's third-party market caches (the search results and the repository scans, both cached for 10 minutes) so the next `GET /api/plugins/market` or `/market/repo` asks GitHub again. Admin only, no body. Response: `{"ok":true}`. The "Clear cache" button of the Third-party tab uses it.

```bash
gk POST /api/plugins/market/cache/clear
```

### PATCH /api/plugins/{id}

Body: `{"enabled":true}` and/or `{"config":{...}}`. `config` **replaces** the plugin-level config: send every field you want to keep. Unknown keys are dropped; invalid values or missing required fields give 400. Secret fields: `""` or omitting the key keeps the stored value, `null` clears it. Response: `PluginInfo`.

```bash
gk PATCH /api/plugins/calculator '{"enabled":false}' | jq .enabled
gk PATCH /api/plugins/calculator '{"enabled":true}' | jq .enabled
```

### DELETE /api/plugins/{id}

Uninstalls the plugin with its widgets, stored data and downloaded wallpapers; workspaces using its wallpaper fall back to `preset:dusk`. Response: `{"ok":true}`.

```bash
gk DELETE /api/plugins/calculator
```

### GET /api/plugins/{id}/logs

`{"logs":[{"ts":1730000000000,"level":"info","msg":"..."}]}`: the last 200 lines (`genkan.log`, `console.*`, errors), `ts` in Unix milliseconds.

```bash
gk GET /api/plugins/calculator/logs | jq -r '.logs[] | "\(.level) \(.msg)"'
```

### POST /api/plugins/{id}/test

Runs one hook for debugging. Body: `{"hook":"wallpaper"|"widget"|"search"|"schedule"|"action","target":"<source, widget or schedule id>","query":"...","config":{...},"action":"...","params":{...},"instance":"<widget instance id>"}`. `config` is the widget instance config to test with; `instance` uses a saved instance as the base (keeps its secrets). Nothing is cached or downloaded. Response: `{"ok":true,"result":...,"error":"...","logs":[...],"ms":12}` where `result` is the sanitized value the UI would use. Side effects of the plugin (storage, links it creates, schedule work) do happen.

```bash
gk POST /api/plugins/calculator/test '{"hook":"search","query":"2*21"}' | jq '{ok, result, error}'
gk POST /api/plugins/world-clock/test '{"hook":"widget","config":{"zones":"Asia/Tokyo, UTC"}}' | jq '.result.body'
```

### POST /api/widgets

Places a widget instance. Body: `{"plugin":"weather","widget":"current","workspace_id":"","config":{...},"w":0,"h":0}` (`workspace_id` empty = shown in every workspace; `config` follows the widget's schema). Optional layout: `w` is the width in columns of a 6-column grid (integer 0-6, `0` = default from the manifest size: s=2, m=4, l=6), `h` the height in pixels (`0` = automatic by content, otherwise 80-1200); out-of-range values are clamped. Response: `{id, plugin, widget, workspace_id, config, sort, w, h, secrets_set?}` (secret values blanked).

```bash
gk POST /api/plugins '{"catalog":"world-clock"}' > /dev/null
W=$(gk POST /api/widgets '{"plugin":"world-clock","widget":"clock","config":{"zones":"UTC, Asia/Tokyo"}}' | jq -r .id)
```

### PATCH /api/widgets/{id}

Body: any of `workspace_id`, `sort`, `w`, `h` (layout, see POST /api/widgets), `config` (replaces the instance config; secrets: `""` keeps, `null` clears). Response: the instance.

```bash
gk PATCH /api/widgets/$W '{"config":{"zones":"UTC, Europe/Berlin"}}' | jq .config
```

### POST /api/widgets/reorder

Body: `{"ids":["<widget id>", ...]}`; listed widgets come first in that order. Response: `{"ok":true}`.

```bash
gk POST /api/widgets/reorder "{\"ids\":[\"$W\"]}"
```

### GET /api/widgets/{id}/data

`{"view":{"title","icon","link","body":[blocks],"error"},"updated_at":<ms>,"next_at":<ms>}`. Cached on the server for the widget's `refresh` seconds; `?force=1` bypasses the cache. A failing plugin gives 502 (504 on timeout) with `{"error":...}`; a disabled plugin 409.

```bash
gk GET "/api/widgets/$W/data?force=1" | jq '.view.body[0]'
```

### POST /api/widgets/{id}/action

Runs the plugin's `action(ctx)` for a `button` block. Body: `{"action":"<name>","params":{...}}`. Response: `{"toast":"...","refresh":true}`.

```bash
gk POST /api/widgets/$W/action '{"action":"noop"}'   # 400 unless the plugin exports action()
```

### DELETE /api/widgets/{id}

Removes the instance. Response: `{"ok":true}`.

```bash
gk DELETE /api/widgets/$W
```

### GET /api/plugins/search

`?q=<text>`: results of all enabled search plugins (no-prefix plugins only for queries of 2+ characters; a plugin with a `prefix` such as `gh` only for `gh <text>`), at most 2 s in total. Response: `{"groups":[{"plugin","name","icon","results":[{title,subtitle,url,icon,copy}]}]}`.

```bash
gk GET "/api/plugins/search?q=1%2B2*3" | jq '.groups[].results'
```

### GET /api/plugins/wallpaper/{plugin}/{source}

Current image of a plugin wallpaper source: `{"url":"/uploads/plugin-wallpapers/<file>","title","credit","link","updated_at":<ms>}`. The server downloads and stores the image (the 5 newest per source are kept). Set it with `PATCH /api/workspaces/{id}` `{"wallpaper":"plugin:<plugin>/<source>"}`.

```bash
gk GET /api/plugins/wallpaper/bing-wallpaper/daily | jq .   # after installing bing-wallpaper
```

## Icon formats

Link `icon` values:

| Value | Meaning |
|---|---|
| `""` | Initials badge; the favicon is fetched automatically on create |
| `lucide:<name>` | Built-in Lucide icon (names below); unknown names are rejected with 400 |
| `emoji:🎬` | A single emoji (up to 16 characters) |
| `url:https://host/logo.png` | Image loaded from an http(s) URL by the browser |
| `upload:<file>` | File from `POST /api/uploads/icon` |
| `favicon:<file>` | Favicon already stored by the server (set by automatic fetching or `refresh-icon`) |

Workspace `icon` accepts only `lucide:<name>`.

The 52 Lucide icon names (use as `lucide:<name>`):

`house`, `layout-grid`, `folder`, `briefcase`, `code`, `terminal`, `server`, `database`, `hard-drive`, `cloud`, `network`, `router`, `wifi`, `globe`, `shield`, `lock`, `key-round`, `film`, `clapperboard`, `tv`, `music`, `headphones`, `image`, `camera`, `gamepad-2`, `book-open`, `graduation-cap`, `newspaper`, `shopping-cart`, `wallet`, `chart-line`, `activity`, `gauge`, `cpu`, `wrench`, `settings`, `bot`, `sparkles`, `heart`, `star`, `bookmark`, `users`, `message-circle`, `mail`, `calendar`, `download`, `flask-conical`, `leaf`, `coffee`, `plane`, `lightbulb`, `rocket`.

## Wallpaper formats

Workspace `wallpaper`: `preset:aurora`, `preset:dusk` (default), `preset:ocean`, `preset:forest`, `preset:sakura`, `preset:graphite`, `upload:<file>` for an image in the gallery (`GET /api/wallpapers`), or `plugin:<pluginId>/<sourceId>` for a wallpaper source of an installed plugin (see Plugins; uninstalling the plugin resets such workspaces to `preset:dusk`). Empty on create means `preset:dusk`; an empty value on `PATCH` also resets to `preset:dusk`.
