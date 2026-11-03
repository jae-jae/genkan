# Genkan plugin development guide (for AI coding assistants)

Genkan is a self-hosted homelab start page. A **plugin** is one server-side JavaScript ES module that adds widgets, wallpaper sources, search providers, scheduled jobs and widget buttons. Plugins run in [moejs](https://github.com/MoeForge/moejs), a pure-Go JS engine, **on the server**. The browser never runs plugin code: it only renders the declarative JSON the plugin returns.

This guide is self-contained. It assumes you have the **Genkan Agent Skill** (REST API with `curl` and the `gk` helper, see `skills/genkan/SKILL.md`). Follow it top to bottom: write, install **disabled**, test, read logs, iterate, enable, add a widget, verify.

## Rules in one screen

1. One file, one `export const manifest = {...}`, plus `export async function` hooks. **No `import`**, no `require`, no DOM, no filesystem.
2. Every capability in the manifest needs its exported function (`wallpapers` -> `wallpaper`, `widgets` -> `widget`, `search` -> `search`, `schedules` -> `schedule`). `action` is optional and powers `button` blocks.
3. **Network is deny-by-default.** List hosts in `permissions.fetch`, or use a `url` config field with `allowFetch: true` for user-specific hosts (LAN services).
4. **Never hardcode secrets or personal hostnames.** Credentials are `secret` config fields; endpoints are `url` fields.
5. **Install with `"enable": false`, run `/test` for every hook, read `ok`, `error`, `logs` and `result.error`, and only then enable.**
6. Globals do not persist between calls. Keep state in `genkan.storage`.
7. Return shapes are sanitized (unknown block types and fields are dropped silently). Compare `result` of `/test` with what you intended.
8. Hook time budgets: search 2 s, widget/wallpaper/action 10 s, schedule 60 s. Memory 32 MiB.

## 1. Quick start

### 1.1 A minimal widget plugin

Save as `hello-clock.js`:

```js
export const manifest = {
  id: "hello-clock",
  name: "Hello Clock",
  version: "1.0.0",
  description: "Greets you and shows the server time.",
  icon: "clock",
  widgets: [
    {
      id: "main",
      name: "Hello",
      size: "s",
      refresh: 60,
      config: [{ key: "name", type: "string", label: "Your name", default: "friend" }],
    },
  ],
};

export async function widget(ctx) {
  const zh = ctx.lang === "zh";
  const cfg = (ctx.instance && ctx.instance.config) || {};
  const time = new Intl.DateTimeFormat(zh ? "zh-CN" : "en-US", {
    hour: "2-digit", minute: "2-digit", timeZone: "UTC",
  }).format(new Date(ctx.now));
  genkan.log("rendering for", cfg.name);
  return {
    title: "Hello",
    icon: "clock",
    body: [
      { type: "text", text: (zh ? "你好，" : "Hello, ") + cfg.name, size: "l" },
      { type: "text", text: time + " UTC", muted: true },
    ],
  };
}
```

### 1.2 The dev loop

All commands use the Skill's `gk METHOD PATH [BODY]` helper (Bearer token; the response is JSON). `ID` is `manifest.id`. If you authenticate with a browser cookie instead of the token, every non-GET request also needs the header `X-Requested-With: genkan`.

**Build the JSON body from the file, never by string interpolation** (source contains quotes and newlines). `@file` makes `curl` read the body from that file:

```bash
jq -Rs '{source: ., enable: false}' hello-clock.js > /tmp/body.json     # TypeScript: '{source: ., lang: "ts", enable: false}'
```

**Step 1 to 2. Write the file, install it disabled.** `enable` defaults to **true** for new plugins, so always pass `false` while developing. Validation errors come back as `400 {"error": "invalid plugin: ..."}` with line and column for syntax errors.

```bash
gk POST /api/plugins @/tmp/body.json | jq '{id, enabled, permissions, widgets: [.widgets[].id], last_error}'
```

**Step 3. Test every hook** (works while disabled, ignores caches, never downloads wallpaper images). Body fields: `hook` (`wallpaper|widget|search|schedule|action`), `target` (widget, source or schedule id; default: the first one), `config` (the **widget instance** config, for `widget`/`action`), `instance` (id of a saved widget instance as config base; keeps its secrets), `query` (search; pass the text after the prefix), `action` + `params` (action), `lang` (`zh|en`, default `en`).

```bash
gk POST /api/plugins/hello-clock/test '{"hook":"widget","target":"main","config":{"name":"Ada"}}' | jq '{ok, error, result, logs, ms}'
gk POST /api/plugins/hello-clock/test '{"hook":"widget","lang":"zh"}' | jq -c '.result.body'
```

Reading the answer:

| Field | Meaning |
|---|---|
| `ok: false` + `error` | The hook threw, timed out, ran out of memory, or returned something unusable. Fix this first. |
| `ok: true` but `result.error` set | A widget returned `{error: "..."}` on purpose (e.g. a failed request it caught). **Still a failure for you.** |
| `result` | The **sanitized** value the UI would use. Missing blocks or fields here were dropped by the sanitizer (section 2.8). |
| `logs` | `genkan.log` / `console.*` output of this call plus host warnings such as `fetch blocked: ...`. |
| `ms` | Wall time. Compare against the hook budget. |

Test side effects are real: `genkan.storage.set` persists, `genkan.links.create` creates links, a `schedule` test does the whole job.

**Step 4. Logs** (last 200 entries, in memory, oldest first, secrets shown as `***`):

```bash
gk GET /api/plugins/hello-clock/logs | jq -r '.logs[] | "\(.level)\t\(.msg)"'
gk GET /api/plugins | jq '.[] | select(.id=="hello-clock") | .last_error'
```

**Step 5. Iterate.** `PUT .../source` replaces the code and **keeps** enabled state, config, widget instances and `source_url`. `manifest.id` must not change. Bump `version` when you publish, not on every edit. Use `POST /api/plugins/preview` (same body as install) to validate without installing.

```bash
jq -Rs '{source: .}' hello-clock.js > /tmp/body.json
gk PUT /api/plugins/hello-clock/source @/tmp/body.json | jq '{id, version, enabled, last_error}'
gk POST /api/plugins/preview @/tmp/body.json | jq '{error, installed, installed_version, permissions: .plugin.permissions}'
```

**Step 6. Plugin-level config** (`manifest.config`, read as `ctx.config`). `config` **replaces** the whole object, so send every field you want to keep. Secrets: `""` or omitted keeps the stored value, `null` clears it. Take secret values from the user; never invent or print them.

```bash
gk PATCH /api/plugins/ID "$(jq -n --arg k "$API_KEY" '{config:{api_key:$k}}')" | jq '{config, secrets_set}'
```

**Step 7. Enable.**

```bash
gk PATCH /api/plugins/hello-clock '{"enabled":true}' | jq '{id, enabled}'
```

**Step 8. Add a widget instance** (only for plugins with `widgets`). `workspace_id` `""` = every workspace (list them with `gk GET /api/bootstrap | jq '.workspaces[]|{id,name}'`). `config` follows `widgets[].config`. Optional layout: `w` = columns 0-6 of a 6-column grid (0 = default from `size`: s=2, m=4, l=6), `h` = height in px (0 = auto, else 80-1200). Out-of-range values are clamped. A missing `required` field gives `400`.

```bash
W=$(gk POST /api/widgets '{"plugin":"hello-clock","widget":"main","config":{"name":"Ada"},"w":3,"h":0}' | jq -r .id)
gk PATCH /api/widgets/$W '{"w":2,"h":160}'          # also: workspace_id, config (replaces the instance config), sort
```

**Step 9. Verify with the real endpoint** (what the page polls). `?force=1` bypasses the server cache (admin only). `502` = the hook failed (`504` = timeout), `409` = plugin disabled, `404` = unknown id.

```bash
gk GET "/api/widgets/$W/data?force=1" | jq '{error, title: .view.title, error_in_view: .view.error, body: .view.body}'
```

You cannot see the rendered page. If it matters, ask the user to look at it. For other hooks verify like this:

```bash
gk GET "/api/plugins/search?q=wiki%20turing" | jq '.groups'                          # search plugins
gk GET /api/plugins/wallpaper/daily-photo/daily | jq .                              # wallpaper: downloads and stores the image
gk PATCH /api/workspaces/WS_ID '{"wallpaper":"plugin:daily-photo/daily"}'           # use it (ask first: this changes the user's page)
gk POST /api/widgets/$W/action '{"action":"inc","params":{"by":1}}'                 # button action
```

**Step 10. After the plugin works: offer to publish it** (section 5). Never publish without the user's explicit yes.

Clean up experiments with `gk DELETE /api/plugins/ID` (also removes its widgets, storage and downloaded wallpapers).

### 1.3 Learn from the built-in examples

Read the closest one before writing yours. No install needed:

```bash
gk GET /api/plugins/catalog | jq -r '.[] | "\(.id)\t\(.description)"'
gk POST /api/plugins/preview '{"catalog":"pihole"}' | jq -r .source
```

| Example | Shows |
|---|---|
| `pihole` | LAN service with `url`+`allowFetch` and a `secret`, `button` + `action`, two API generations, always-closing a session |
| `rss` | XML parsing without DOMParser, `allowFetch` for arbitrary feed URLs, relative link resolving |
| `weather` | Per-instance config (`select`, `required`), two chained APIs, `chart` block, i18n |
| `hacker-news`, `uptime-summary` | `list`, `stats`, `kv`, `progress`; `uptime-summary` uses `permissions.links` |
| `world-clock`, `countdown` | Offline widgets, `Intl.DateTimeFormat` with IANA time zones, `number` fields |
| `calculator`, `github-search` | Search: no prefix with `copy` results; prefix `gh` with abort timer and optional token |
| `bing-wallpaper`, `nasa-apod`, `picsum` | Wallpaper sources (skipping unusable days, redirects to a CDN host) |
| `link-importer` | Schedule + `genkan.links.create`, idempotent import, result kept in storage |

## 2. Reference

### 2.1 Plugin file and manifest

One ES module, at most **512 KiB**, valid UTF-8, JavaScript (or TypeScript with `"lang":"ts"`: types are stripped, nothing is type-checked). Imports of any kind are rejected. Top-level code must stay pure (constants, helper functions): it runs with a 2 s limit and **cannot call `genkan.*`** (error `not available while the plugin is being installed`).

```js
export const manifest = {
  id: "my-plugin", name: "My Plugin", version: "1.0.0",
  description: "...", author: "...", homepage: "https://...", icon: "puzzle",
  permissions: { fetch: ["api.example.com"], links: "read" },
  config: [ /* plugin-level ConfigField[] -> ctx.config */ ],
  wallpapers: [{ id: "daily", name: "Daily", refresh: 3600 }],
  widgets:    [{ id: "main", name: "Main", size: "m", refresh: 300, config: [ /* per-instance -> ctx.instance.config */ ] }],
  search:     { name: "My search", prefix: "my" },
  schedules:  [{ id: "sync", every: 3600 }],
};
```

| Field | Rule |
|---|---|
| `id` | required, `^[a-z0-9][a-z0-9-]{1,39}$` (2-40 chars), unique per server; also the file name, so choose it once |
| `name` | required, <= 40 chars |
| `version` | required, <= 20 chars; bump it when you publish an update |
| `description` / `author` | optional, <= 300 / <= 60 chars |
| `homepage` | optional `http(s)` URL |
| `icon` | optional Lucide name `^[a-z0-9][a-z0-9-]{0,39}$`; the UI shows it only if it knows the name (section 2.9) |
| `permissions.fetch` | <= 30 entries: `host`, `host:port`, `*.suffix` (subdomains only, **not** the bare domain), or `*` (any host; shown with a warning, avoid). Lower-case, no scheme, no path. No port = any port. Redirect targets must match too. May be `[]`. |
| `permissions.links` | `"read"` = `genkan.links.list`, `workspaces.list`, `categories.list`; `"write"` adds `links.create` / `links.update`. Omitted = none. |
| `config` | plugin-level fields, <= 30 |
| `wallpapers[]` | `id` (`^[a-z0-9][a-z0-9_-]{0,39}$`, unique), `name` required <= 40, `refresh` seconds, **>= 300**, default 3600 |
| `widgets[]` | `id` (same rule), `name` required <= 40, `size` `"s"`, `"m"` or `"l"` (default `"m"`), `refresh` seconds, **>= 30**, default 300, `config` instance fields <= 30 |
| `search` | `name` required <= 40, `prefix` <= 20 chars, no spaces, lower-cased. Empty prefix = see 2.5 |
| `schedules[]` | `id`, `every` seconds, **required, >= 60** |

At least one of `wallpapers`, `widgets`, `search`, `schedules` is required. Unknown top-level manifest fields are ignored. Limits per server: 50 plugins, 200 widget instances.

### 2.2 ConfigField

```js
{ key: "url", type: "url", label: "Base URL", help: "shown under the input", default: undefined, required: true, allowFetch: true }
```

| Property | Rule |
|---|---|
| `key` | `^[a-zA-Z][a-zA-Z0-9_]{0,39}$`, unique in its list |
| `type` | `string`, `text` (multi-line), `number` (`min`, `max`), `boolean`, `select` (`options: [{value,label}]`, 1-50, required), `secret`, `url` (http(s) only) |
| `label` / `help` | required <= 60 / optional <= 200 chars |
| `default` | optional, must be valid for the type. The manifest is public: **never put a real secret in a default** |
| `required` | enforced when saving config or creating a widget (not in `/test`). A field with a `default` counts as satisfied |
| `allowFetch` | only on `url`. Adds that URL's `host:port` (80/443 when no port is written) to the fetch allow-list for calls that carry the value, and **skips TLS certificate verification for exactly that host** (self-signed homelab HTTPS). Hosts from `permissions.fetch` are always verified |

Semantics:

- Plugin-level fields -> `ctx.config` in every hook. Widget fields -> `ctx.instance.config` in `widget` and `action`. An `allowFetch` field only opens the network for hooks that can see it (a widget-level field does not help `search` or `schedule`).
- **Defaults are applied at read time** (`ctx.config.x` has the default; the stored config does not contain it). Empty strings and `null` remove a key. Unknown keys are dropped. Strings <= 2000 chars (`text` <= 5000), no control characters.
- `PATCH` of `config` (plugin or widget) **replaces** it. `secret`: `""` / omitted = keep, `null` = clear.
- `secret` values are never returned by the API: admins see `""` plus `secrets_set: ["key"]`, guests see nothing. Inside the plugin they are plain text in `ctx`. They are replaced by `***` in logs, errors and `/test` results (values shorter than 4 characters are not masked).

### 2.3 Capabilities and exports

| Manifest | Export | Called when | Returns |
|---|---|---|---|
| `wallpapers` | `wallpaper(ctx)` | a workspace uses `plugin:<id>/<source>` (cached `refresh` s) | WallpaperResult |
| `widgets` | `widget(ctx)` | the page polls `/api/widgets/{id}/data` (cached `refresh` s per instance and language) | WidgetView |
| `search` | `search(ctx)` | the user types in the search box | SearchResult[] |
| `schedules` | `schedule(ctx)` | every `every` s (first run ~15 s after server start, ~5 s after install/edit) | ignored |
| (none) | `action(ctx)` | a `button` block is clicked (admin only) | `{toast?, refresh?}` |

Declare a capability without exporting its function and install fails: ``the manifest declares widgets but the module does not export `async function widget(ctx)` ``. Hooks are `export async function name(ctx)`; a plain function returning a value or promise also works.

### 2.4 `ctx`

| Field | In | Value |
|---|---|---|
| `plugin` | all | `{id, version}` |
| `config` | all | plugin-level config with defaults and **secrets in clear text** |
| `lang` | all | `"zh"` or `"en"`: the requester's language (`?lang=` or `Accept-Language`); `"en"` for `schedule` and for `/test` without `lang` |
| `now` | all | Unix time in **ms** (number) |
| `instance` | widget, action | `{id, config}` of the widget instance (`id` is `"test"` in `/test` unless you pass `instance`) |
| `widget` | widget, action | widget id from the manifest |
| `source` | wallpaper | wallpaper source id |
| `query` | search | the query; with a prefix the prefix and the space are removed |
| `schedule` | schedule | schedule id |
| `action`, `params` | action | button's `action` name and `params` (<= 2 KiB serialized) |

### 2.5 Host API (`genkan`, `console`)

| API | Notes |
|---|---|
| `genkan.log(...args)`, `console.log/info/debug/warn/error(...)` | Plugin log: last 200 entries, <= 2000 chars each, in memory (lost on restart, kept across source updates). Objects are logged as JSON. |
| `genkan.storage.get(key)` | Persistent JSON per **plugin** (shared by all instances and hooks), `undefined` if missing |
| `genkan.storage.set(key, value)` | `value` must be JSON-serializable. Key 1-200 bytes. All keys + values <= **256 KiB**, else it throws `storage is full (max 256 KiB)`. Written to disk after ~2 s |
| `genkan.storage.delete(key)`, `genkan.storage.keys()` | |
| `genkan.links.list()` | needs `links: "read"`. Synchronous. Same objects as `GET /api/bootstrap` links: `id, workspace_id, category_id, title, url, description, icon, pinned, health_check, ...` |
| `genkan.links.create(input)` | needs `"write"`. Same body as `POST /api/links`. Throws on invalid input. Returns the link |
| `genkan.links.update(id, patch)` | needs `"write"`. Same body as `PATCH /api/links/{id}` |
| `genkan.workspaces.list()`, `genkan.categories.list()` | needs `"read"` |
| `genkan.version` | server version string |

Without the permission the call throws ``permission denied: genkan.links.list needs permissions.links = "read"``.

### 2.6 Runtime environment

| Available | NOT available |
|---|---|
| Modern syntax (async/await, `?.`, `??`, destructuring, regex named groups and lookbehind, BigInt, `replaceAll`, `at`, `toSorted`), `Promise.all/allSettled/any`, `Map/Set`, `JSON`, `Math`, `structuredClone` | `import`, `require`, `module`, `process`, `Buffer` |
| `fetch`, `Headers`, `Request`, `Response`, `URL`, `URLSearchParams`, `AbortController`, `TextEncoder/TextDecoder`, `btoa/atob`, `ReadableStream`, `Event/EventTarget` | **DOM: `document`, `window`, `DOMParser`**, `XMLHttpRequest`, `WebSocket`, `EventSource`, `localStorage`, `navigator`, `location` |
| `setTimeout/setInterval/clear*`, `queueMicrotask`, `performance` | `crypto` (no random bytes, UUIDs or hashing), `Blob`, `File`, `FormData`, `WebAssembly`, `Temporal` |
| `Intl.DateTimeFormat` (IANA time zones), `Intl.NumberFormat`, `toLocaleString` | `eval` and `new Function` (throw `EvalError`) |

Important behaviours:

- **No persistent globals.** Each call may run in a fresh or a pooled runtime. Module-level variables are only caches that may vanish; keep real state in `genkan.storage`.
- Clear your timers (`clearTimeout` in `finally`). A runtime that still has timers after the hook returned is discarded.
- `Date` uses the **server's** time zone (UTC in the Docker image). Use `Intl.DateTimeFormat(..., {timeZone})` or `getUTC*` for deterministic output.
- `fetch`: per request timeout 15 s (also bounded by the hook budget), at most 10 redirects, each checked against the allow-list. Only `http:`/`https:`. A blocked or failed request rejects with `TypeError: fetch failed`; the reason for a block is in the log (`fetch blocked: fetch to X is not allowed (add it to manifest.permissions.fetch)`). Many APIs reject requests without a `User-Agent` (Wikipedia answers 403): set the header yourself.
- A POST form body: use `new URLSearchParams({...})` (there is no `FormData`).
- A hook result must serialize to <= 1 MiB.

### 2.7 Limits and timeouts

| What | Limit |
|---|---|
| `search` | 2 s per plugin, and 2 s in total for all search plugins together |
| `widget`, `wallpaper`, `action` | 10 s (error `timed out after 10s`, HTTP 504) |
| `schedule` | 60 s; at most 2 scheduled jobs run at once |
| Module load (top level) | 2 s |
| Memory per runtime | 32 MiB (`exceeded the 32 MiB memory limit`); up to 4 runtimes per plugin at once |
| Source | 512 KiB; config object 16 KiB serialized; storage 256 KiB |
| Widget cache | result kept `refresh` s per instance and language (an `{error}` view is cached too). Failed calls (thrown) are not retried for 15 s. `?force=1` (admin) bypasses both. Concurrent identical calls are merged |
| Wallpaper image | downloaded by the server through your allow-list (30 s, redirects checked), <= 15 MiB, only jpg/png/webp/avif; newest 5 files kept per source |

### 2.8 Return shapes and sanitization

Output is normalized on the server. **Everything unknown is dropped without an error.** All text is rendered as plain text (no HTML, no Markdown). Whitespace in single-line strings is collapsed; control characters are removed; strings are cut at 500 chars unless noted.

#### WidgetView (`widget`)

```js
return { title: "Pi-hole", icon: "shield", link: "http://pi.hole/admin", body: [ /* blocks */ ], error: "optional message" };
```

`title`, `icon` (Lucide name), `link` (http(s), makes the title a link), `body` (<= **50 blocks**, nested ones count), `error` (shown as the card's error state). A non-object result becomes `error: "widget() must return an object"`.

| `type` | Fields (required in bold) | Limits |
|---|---|---|
| `text` | **`text`**, `size` s/m/l/xl, `muted` bool, `align` left/center/right | newlines kept |
| `stat` | **`label`**, **`value`** (string or finite number), `unit`, `icon`, `trend` up/down/flat | `unit` <= 40 |
| `stats` | **`items`**: stat objects | <= 6, rendered as a grid |
| `list` | **`items`**: `{title` (required)`, subtitle, right, badge, url, icon, image}` | <= 20 items; `url` http(s) (clickable); `image` http(s) or `data:image/...;base64` <= 256 KiB; items without `title` are dropped |
| `kv` | **`items`**: `{k, v}` (`v` may be a number or bool) | <= 20 |
| `progress` | **`value`** 0..1 (clamped), `label`, `text` | |
| `image` | **`src`** (http(s) or `data:image/<png, jpeg, gif, webp, avif or svg+xml>;base64,...` <= 256 KiB), `alt`, `href`, `ratio` (> 0, max 10) | |
| `chart` | **`kind`** `spark` or `bar`, **`values`** numbers, `labels` | <= 60 points, non-finite numbers dropped |
| `row` | **`children`**: blocks laid out horizontally | nesting depth <= 3, needs >= 1 valid child |
| `button` | **`label`**, **`action`** (`^[A-Za-z0-9_.:-]{1,40}$`), `params` (any JSON, <= 2 KiB else dropped), `icon` | admin only; plugin must export `action` |
| `divider` | | |

```js
body: [
  { type: "stats", items: [{ label: "Queries", value: 12034 }, { label: "Blocked", value: 18.2, unit: "%", trend: "up" }] },
  { type: "progress", label: "Disk", value: 0.73, text: "73%" },
  { type: "kv", items: [{ k: "Uptime", v: "4d 2h" }, { k: "Version", v: "6.0" }] },
  { type: "list", items: [{ title: "Post title", subtitle: "example.com", right: "3h", url: "https://example.com/p/1" }] },
  { type: "chart", kind: "spark", values: [3, 5, 4, 8, 6], labels: ["Mon", "Tue", "Wed", "Thu", "Fri"] },
  { type: "image", src: "https://example.com/cover.jpg", alt: "cover", ratio: 1.78 },
  { type: "text", text: "Last updated just now", size: "s", muted: true, align: "center" },
  { type: "divider" },
  { type: "row", children: [{ type: "button", label: "Pause", action: "pause", params: { minutes: 5 }, icon: "pin-off" }] },
]
```

#### WallpaperResult (`wallpaper`)

```js
return { url: "https://host/img.jpg", title: "optional", credit: "Photographer", link: "https://source/page" };
// or an array of those: the server picks one at random
```

`url`: http(s) image (the **server** downloads it through your allow-list, so its host, and any redirect host, must be allowed) or `data:image/...;base64` <= 256 KiB. `title`/`credit` <= 200 chars. `link` http(s). The image must really be jpg, png, webp or avif. There is no `error` field: **throw** (see 2.10). `/test` does not download the image; `GET /api/plugins/wallpaper/{plugin}/{source}` does.

#### SearchResult (`search`)

```js
return [{ title: "= 7", subtitle: "1+2*3", icon: "calculator", copy: "7" }, { title: "Alan Turing", url: "https://en.wikipedia.org/wiki/Alan_Turing" }];
```

<= **8** results; `title` required (<= 200), `subtitle` <= 300, `url` http(s) (Enter opens it), `icon`, `copy` <= 2000 chars (Enter copies it when there is no `url`). Not an array = no results.

Prefix rules: an empty `prefix` plugin is only called for queries of **>= 2 characters**; with `prefix: "gh"` only for queries starting with `gh ` (case-insensitive) and `ctx.query` is the rest (non-empty).

#### Action result (`action`)

`{ toast: "Done", refresh: true }`. `toast` <= 200 chars, `refresh: true` reloads the widget. Throwing shows the message as an error. The cached widget view is dropped after every action.

### 2.9 Icons

Icon names go in `manifest.icon`, `view.icon`, `stat.icon`, list `icon`, button `icon`, search `icon`. The server accepts any `^[a-z0-9][a-z0-9-]{0,39}$`, but the page draws only the names it ships (unknown ones show nothing or a default). Known at the time of writing:

`house layout-grid folder briefcase code terminal server database hard-drive cloud network router wifi globe shield lock key-round film clapperboard tv music headphones image camera gamepad-2 book-open graduation-cap newspaper shopping-cart wallet chart-line activity gauge cpu wrench settings bot sparkles heart star bookmark users message-circle mail calendar download flask-conical leaf coffee plane lightbulb rocket search pencil plus x trash-2 pin pin-off check copy upload refresh-cw compass eye eye-off link external-link arrow-up arrow-down arrow-left arrow-right triangle-alert circle-alert puzzle cloud-sun calculator trending-up trending-down minus scroll-text clock rss sun moon cloud-rain cloud-snow cloud-lightning cloud-fog`

### 2.10 Error handling conventions

| Hook | On an expected failure (network down, bad config, upstream error) |
|---|---|
| `widget` | **Catch and return** `{ title, icon, body: [], error: "Cannot reach X: reason" }`. The card shows a clear message instead of a broken page. Check `config` first and return a "set the URL in the widget settings" error. Throwing also works (`502`, retried after 15 s) but gives a less useful message |
| `wallpaper` | **Throw** `new Error("...")`. There is no error field; the page keeps the default wallpaper and the message goes to `last_error` and the log |
| `search` | Return `[]` (log the reason). Never throw: a slow or failing search plugin is silently ignored |
| `action` | Throw to show an error to the admin; return `{toast, refresh}` on success |
| `schedule` | Throw (logged as `error`), or catch and `genkan.log` + store a status in `genkan.storage` |

Never include secrets in messages you build yourself (the host masks known secret values, but not derived ones such as `btoa(user:pass)`).

### 2.11 i18n

`ctx.lang` is `"zh"` or `"en"`. Widget caches are per language. Keep manifest strings (`name`, `label`, `help`, `description`) in English; translate what the plugin renders:

```js
const zh = ctx.lang === "zh";
const T = { loading: zh ? "加载中" : "Loading", blocked: zh ? "已拦截" : "Blocked" };
new Intl.NumberFormat(zh ? "zh-CN" : "en-US").format(n);
```

## 3. Recipes

Every plugin below was installed and tested against a real Genkan build.

### 3.1 A LAN service: URL field + `allowFetch` + secret token

`permissions.fetch` stays empty: the user types the address, and `allowFetch` whitelists exactly that `host:port`. The token is a `secret`.

```js
export const manifest = {
  id: "nas-stats",
  name: "NAS Stats",
  version: "1.0.0",
  description: "CPU, memory and disk of a LAN service that serves /api/stats.",
  icon: "server",
  permissions: { fetch: [] }, // the host comes from the `url` field below
  widgets: [
    {
      id: "stats",
      name: "NAS stats",
      size: "m",
      refresh: 60,
      config: [
        { key: "url", type: "url", label: "Base URL", help: "e.g. http://192.168.1.10:8080", required: true, allowFetch: true },
        { key: "token", type: "secret", label: "API token" },
      ],
    },
  ],
};

export async function widget(ctx) {
  const cfg = (ctx.instance && ctx.instance.config) || {};
  const view = { title: "NAS", icon: "server", body: [] };
  if (!cfg.url) return { ...view, error: "Set the base URL in the widget settings" };
  const base = cfg.url.replace(/\/+$/, "");
  try {
    const res = await fetch(base + "/api/stats", {
      headers: cfg.token ? { Authorization: "Bearer " + cfg.token } : {},
    });
    if (res.status === 401) throw new Error("token rejected");
    if (!res.ok) throw new Error("HTTP " + res.status);
    const d = await res.json();
    view.link = base;
    view.body = [
      { type: "stats", items: [
        { label: "CPU", value: Math.round(d.cpu * 100), unit: "%" },
        { label: "Memory", value: Math.round(d.mem * 100), unit: "%" },
      ] },
      { type: "progress", label: "Disk", value: d.disk, text: Math.round(d.disk * 100) + "%" },
      { type: "kv", items: [{ k: "Uptime", v: Math.floor(d.uptime / 3600) + " h" }] },
    ];
  } catch (e) {
    view.error = "Cannot reach " + base + ": " + (e && e.message ? e.message : e);
  }
  return view;
}
```

```bash
gk POST /api/plugins/nas-stats/test '{"hook":"widget","config":{"url":"http://192.168.1.10:8080","token":"..."}}' | jq '{ok, error, view_error: .result.error, body: .result.body}'
gk POST /api/widgets "$(jq -n --arg t "$TOKEN" '{plugin:"nas-stats",widget:"stats",config:{url:"http://192.168.1.10:8080",token:$t}}')" | jq '{id, secrets_set}'
```

Notice `ok: true` with `result.error` when the service is down or the token is wrong: that is the widget's error state, not a pass.

### 3.2 Caching with storage (and surviving upstream failures)

Widgets are already cached for `refresh` seconds. Use storage when the upstream should be called **less often than the widget refreshes**, or when you want stale data on failure. Storage is shared by the whole plugin, so include the variable parts in the key.

```js
export const manifest = {
  id: "exchange-rate",
  name: "Exchange Rate",
  version: "1.0.0",
  description: "Rates for a base currency, cached for an hour.",
  icon: "trending-up",
  permissions: { fetch: ["open.er-api.com"] },
  widgets: [
    {
      id: "rates",
      name: "Exchange rates",
      size: "s",
      refresh: 300,
      config: [{ key: "base", type: "string", label: "Base currency", default: "USD" }],
    },
  ],
};

// Return a fresh cached value, else load() it; if loading fails fall back to the stale copy.
async function cached(key, ttlMs, load) {
  const hit = genkan.storage.get(key);
  if (hit && Date.now() - hit.at < ttlMs) return hit.value;
  try {
    const value = await load();
    genkan.storage.set(key, { at: Date.now(), value });
    return value;
  } catch (e) {
    if (hit) {
      genkan.log("using stale", key, "because", e.message);
      return hit.value;
    }
    throw e;
  }
}

export async function widget(ctx) {
  const base = String(ctx.instance.config.base).toUpperCase();
  try {
    const rates = await cached("rates:" + base, 3600_000, async () => {
      genkan.log("fetching rates for", base);
      const res = await fetch("https://open.er-api.com/v6/latest/" + encodeURIComponent(base));
      if (!res.ok) throw new Error("HTTP " + res.status);
      const data = await res.json();
      if (data.result !== "success") throw new Error(data["error-type"] || "bad response");
      return data.rates;
    });
    const items = ["EUR", "GBP", "JPY", "CNY"].filter((c) => c !== base).map((c) => ({ k: "1 " + base + " =", v: rates[c].toFixed(2) + " " + c }));
    return { title: "Rates", icon: "trending-up", body: [{ type: "kv", items }] };
  } catch (e) {
    return { title: "Rates", icon: "trending-up", body: [], error: String(e.message || e) };
  }
}
```

### 3.3 A search plugin with a prefix

Type `wiki alan turing` and the results appear as a group. The hook has 2 s, so abort early and return `[]` on any failure.

```js
export const manifest = {
  id: "wiki-search",
  name: "Wikipedia Search",
  version: "1.0.0",
  description: 'Type "wiki <words>" in the search box.',
  icon: "book-open",
  permissions: { fetch: ["*.wikipedia.org"] }, // matches en.wikipedia.org, de.wikipedia.org, ...
  config: [
    {
      key: "lang",
      type: "select",
      label: "Wikipedia language",
      default: "en",
      options: [
        { value: "en", label: "English" },
        { value: "de", label: "Deutsch" },
        { value: "zh", label: "中文" },
      ],
    },
  ],
  search: { name: "Wikipedia", prefix: "wiki" },
};

export async function search(ctx) {
  const q = ctx.query.trim();
  if (q.length < 2) return [];
  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), 1500); // the search hook only gets 2 s
  try {
    const url = `https://${ctx.config.lang}.wikipedia.org/w/api.php?action=opensearch&limit=6&format=json&search=${encodeURIComponent(q)}`;
    // Wikipedia rejects requests without a descriptive User-Agent (HTTP 403)
    const res = await fetch(url, { signal: ac.signal, headers: { "User-Agent": "genkan-wiki-search/1.0 (homelab start page)" } });
    if (!res.ok) throw new Error("HTTP " + res.status);
    const [, titles, descriptions, urls] = await res.json();
    return titles.map((title, i) => ({ title, subtitle: descriptions[i] || undefined, url: urls[i], icon: "book-open" }));
  } catch (e) {
    genkan.log("wikipedia failed:", e.message); // a search plugin never throws: return []
    return [];
  } finally {
    clearTimeout(timer);
  }
}
```

```bash
gk POST /api/plugins/wiki-search/test '{"hook":"search","query":"alan turing"}' | jq '{ok, result, logs, ms}'   # no prefix in /test
gk GET "/api/plugins/search?q=wiki%20alan%20turing" | jq '.groups'                                          # after enabling
```

### 3.4 A wallpaper source

The plugin only returns a URL. The server downloads, checks and stores the image, so both hosts are allowed. Throw on failure.

```js
export const manifest = {
  id: "daily-photo",
  name: "Daily Photo",
  version: "1.0.0",
  description: "A different landscape every day from picsum.photos.",
  icon: "camera",
  // The SERVER downloads the image, so the image hosts must be allowed too.
  // picsum.photos redirects to fastly.picsum.photos, and every redirect target is checked.
  permissions: { fetch: ["picsum.photos", "fastly.picsum.photos"] },
  wallpapers: [{ id: "daily", name: "Photo of the day", refresh: 3600 }],
};

export async function wallpaper(ctx) {
  const day = new Date(ctx.now).toISOString().slice(0, 10);
  return {
    url: `https://picsum.photos/seed/${day}/1920/1080.jpg`,
    title: "Photo of " + day,
    credit: "picsum.photos",
    link: "https://picsum.photos",
  };
}
```

### 3.5 A button action

A `button` block calls `action(ctx)` with its `action` and `params`. Per-instance state goes under a key that contains `ctx.instance.id`.

```js
export const manifest = {
  id: "click-counter",
  name: "Click Counter",
  version: "1.0.0",
  description: "A counter with buttons; the count lives in plugin storage.",
  icon: "plus",
  widgets: [{ id: "counter", name: "Counter", size: "s", refresh: 300 }],
};

const keyOf = (ctx) => "count:" + ctx.instance.id; // storage is shared by all instances of the plugin

export async function widget(ctx) {
  return {
    title: "Counter",
    icon: "plus",
    body: [
      { type: "stat", label: "Clicks", value: genkan.storage.get(keyOf(ctx)) || 0 },
      { type: "row", children: [
        { type: "button", label: "+1", action: "inc", params: { by: 1 } },
        { type: "button", label: "+10", action: "inc", params: { by: 10 } },
        { type: "button", label: "Reset", action: "reset" },
      ] },
    ],
  };
}

export async function action(ctx) {
  const key = keyOf(ctx);
  if (ctx.action === "reset") {
    genkan.storage.set(key, 0);
    return { toast: "Counter reset", refresh: true };
  }
  if (ctx.action === "inc") {
    const n = (genkan.storage.get(key) || 0) + (Number(ctx.params && ctx.params.by) || 1);
    genkan.storage.set(key, n);
    return { toast: "Now " + n, refresh: true };
  }
  throw new Error("unknown action: " + ctx.action); // shown to the admin as an error
}
```

```bash
gk POST /api/plugins/click-counter/test '{"hook":"action","action":"inc","params":{"by":10}}' | jq '{ok, error, result}'
```

### 3.6 A scheduled job that uses `genkan.links`

Needs `permissions.links`. A `schedule` test runs the whole job once, for real.

```js
export const manifest = {
  id: "lan-health",
  name: "LAN Health Checks",
  version: "1.0.0",
  description: "Turns on health checks for links that point at private addresses.",
  icon: "activity",
  permissions: { links: "write" }, // "read" would allow list() only
  schedules: [{ id: "tag", every: 3600 }],
};

const PRIVATE = /^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|[^.]+\.lan$|localhost$)/;

export async function schedule(ctx) {
  let changed = 0;
  for (const link of genkan.links.list()) {
    if (link.health_check) continue;
    let host = "";
    try { host = new URL(link.url).hostname; } catch (e) { continue; }
    if (PRIVATE.test(host)) {
      genkan.links.update(link.id, { health_check: true });
      changed++;
    }
  }
  genkan.log("health checks enabled on", changed, "links");
  genkan.storage.set("lastRun", { at: ctx.now, changed });
  return { changed }; // the return value is ignored when scheduled, but test shows it
}
```

### 3.7 Parsing XML without DOMParser

There is no DOM, so parse text yourself. For small, known formats, `String.match` with regexes is enough (see the built-in `rss` example). For tolerant general parsing, tokenize into a tree:

```js
export const manifest = {
  id: "feed-lite",
  name: "Feed Lite",
  version: "1.0.0",
  description: "Latest items of an RSS or Atom feed, parsed without DOMParser.",
  icon: "rss",
  widgets: [
    {
      id: "feed",
      name: "Feed",
      size: "m",
      refresh: 900,
      config: [{ key: "url", type: "url", label: "Feed URL", required: true, allowFetch: true }],
    },
  ],
};

// Tiny XML -> tree parser: { name, attrs, children, text }. Tolerant, not validating.
function parseXml(xml) {
  const root = { name: "#root", attrs: {}, children: [], text: "" };
  const stack = [root];
  const re = /<!--[\s\S]*?-->|<\?[\s\S]*?\?>|<!DOCTYPE[^>]*>|<!\[CDATA\[([\s\S]*?)\]\]>|<\/([\w:.-]+)\s*>|<([\w:.-]+)((?:\s+[\w:.-]+\s*=\s*(?:"[^"]*"|'[^']*'))*)\s*(\/?)>|([^<]+)/g;
  let m;
  while ((m = re.exec(xml))) {
    const top = stack[stack.length - 1];
    if (m[1] !== undefined) top.text += m[1];
    else if (m[2]) { if (stack.length > 1) stack.pop(); }
    else if (m[3]) {
      const attrs = {};
      for (const a of m[4].matchAll(/([\w:.-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')/g)) attrs[a[1]] = unescapeXml(a[2] ?? a[3]);
      const node = { name: m[3], attrs, children: [], text: "" };
      top.children.push(node);
      if (!m[5]) stack.push(node);
    } else if (m[6]) top.text += unescapeXml(m[6]);
  }
  return root;
}

const ENT = { amp: "&", lt: "<", gt: ">", quot: '"', apos: "'" };
const unescapeXml = (s) => s.replace(/&(#x[0-9a-f]+|#\d+|\w+);/gi, (all, e) =>
  e[0] === "#" ? String.fromCodePoint(e[1] === "x" || e[1] === "X" ? parseInt(e.slice(2), 16) : +e.slice(1)) : ENT[e] ?? all);

const find = (n, name) => n.children.find((c) => c.name === name);
const findAll = (n, name) => n.children.filter((c) => c.name === name);

export async function widget(ctx) {
  const cfg = (ctx.instance && ctx.instance.config) || {};
  const fail = (error) => ({ title: "Feed", icon: "rss", body: [], error });
  if (!cfg.url) return fail("Set the feed URL in the widget settings");
  try {
    const res = await fetch(cfg.url);
    if (!res.ok) throw new Error("HTTP " + res.status);
    const doc = parseXml((await res.text()).slice(0, 2_000_000));
    const feed = find(doc, "rss") ? find(find(doc, "rss"), "channel") : find(doc, "feed"); // RSS 2.0 or Atom
    if (!feed) throw new Error("not an RSS or Atom feed");
    const entries = [...findAll(feed, "item"), ...findAll(feed, "entry")].slice(0, 8);
    const items = entries.map((e) => {
      const atomLink = findAll(e, "link").find((l) => l.attrs.href);
      return {
        title: (find(e, "title") || {}).text?.trim() || "(untitled)",
        url: atomLink ? atomLink.attrs.href : (find(e, "link") || {}).text?.trim(),
      };
    });
    return { title: (find(feed, "title") || {}).text?.trim() || "Feed", icon: "rss", body: [{ type: "list", items }] };
  } catch (e) {
    return fail("Could not load feed: " + (e && e.message ? e.message : e));
  }
}
```

## 4. Debugging

### 4.1 Checklist

1. `POST /api/plugins` answered `400`? Read the message, it names the field (`manifest.widgets[0].refresh must be at least 30`) or the line and column of a syntax error.
2. `/test` -> `ok: false`: read `error`, then `logs`.
3. `/test` -> `ok: true` but nothing on screen: look at `result.error`, then compare `result.body` with what you returned (dropped blocks, wrong field names).
4. Network trouble: look in `logs` for `fetch blocked: ...`, add the host to `permissions.fetch` (or use an `allowFetch` url field). Remember redirects and CDN/image hosts. `TypeError: fetch failed` without a blocked line is DNS, connection or TLS.
5. Log liberally with `genkan.log(JSON.stringify(x).slice(0, 300))`, re-test, read `logs`. Remove noisy logs before publishing.
6. Wrong values from config? Defaults only apply at read time; required fields are not checked in `/test`; in `/test` pass `config` for widget/action, but plugin-level config must be saved with `PATCH /api/plugins/ID`.
7. Slow hook? Check `ms` in `/test`. Run requests in parallel (`Promise.all`), shorten timeouts, cache in storage.
8. Works in `/test` but fails live? The saved instance config may differ: `/test` with `"instance":"<widget id>"`. Admin `GET /api/widgets/{id}/data?force=1` shows the real error. After a failure the next live call is delayed 15 s.

### 4.2 Common errors

| Where | Message | Fix |
|---|---|---|
| install/PUT (400) | `invalid plugin: compile error: plugin.js:2:54: SyntaxError: ...` | fix the syntax at `line:column` |
| install | ``the module must `export const manifest = {...}` `` | export a `const manifest` object literal |
| install | ``the manifest declares widgets but the module does not export `async function widget(ctx)` `` | add the export or remove the capability |
| install | `manifest.id "X" is invalid: use 2-40 characters of a-z, 0-9 and '-'...` | lower-case, digits, hyphens |
| install | `plugins must be a single file: import "..." is not allowed` | inline the code; no imports |
| install | `manifest.widgets[0].refresh must be at least 30` | wallpapers >= 300, widgets >= 30, `schedules[].every` >= 60 |
| install | `manifest.permissions.fetch[0] must be a host such as "api.example.com", "*.example.com" or "*"` | host only: no `https://`, no path |
| install | `manifest.widgets[0].config[1].allowFetch is only valid for type "url"` | use `type: "url"` |
| install | `load error: Error: not available while the plugin is being installed` | top-level code called `genkan.*`; move it into a hook |
| PUT source | `manifest.id "a" must stay "b"` | keep the id (or install as a new plugin) |
| any | `provide exactly one of source, url or catalog` | send one of them |
| widget create/PATCH (400) | `Base URL is required` (label of the missing field) | fill the required fields |
| widget data (409) | `plugin hello-clock is disabled` | enable the plugin |
| hook | `TypeError: fetch failed` + log `fetch blocked: fetch to api.x.com is not allowed (add it to manifest.permissions.fetch)` | allow the host |
| hook | ``Error: permission denied: genkan.links.list needs permissions.links = "read"`` | add `permissions.links` |
| hook | `timed out after 10s` (504) | too slow: parallelize, abort earlier, cache |
| hook | `exceeded the 32 MiB memory limit` | do not buffer big responses or build huge arrays |
| hook | `widget() never settled: it returned a promise that does not resolve` | a promise never resolves (missing `resolve`, awaited a never-firing event) |
| hook | `ReferenceError: DOMParser is not defined` (also `document`, `window`, `require`, `crypto`) | not available, see 2.6 |
| hook | `EvalError: code generation from strings is disabled ...` | no `eval` / `new Function`; write a parser |
| hook | `Error: storage is full (max 256 KiB)` / `storage value must be JSON-serializable` | store less; only plain JSON values (no functions) |
| widget | `result.error: widget() must return an object` | `return { body: [...] }` (do not forget `return`) |
| wallpaper | `wallpaper() must return { url } (http(s) or data:image) or an array of those` | return an object with a `url` |
| wallpaper | `could not download the wallpaper: fetch to X is not allowed (...)` | allow the image host and CDN redirect host |
| wallpaper | `could not download the wallpaper: not a jpg, png, webp or avif image` / `HTTP 403` | wrong URL, hotlink protection, or a gif/svg |
| action | `plugin hello does not export action()` | export `action` |
| HTTP | `502` hook failed, `504` timeout, `409` plugin disabled, `404` unknown id, `403` guest/CSRF | |

## 5. Publishing

### 5.1 How sharing works

Any public GitHub repository with the topic **`genkan-plugin`** shows up in Genkan under **Settings -> Plugins -> Add -> Third-party** for everyone (search by keyword, sort by stars or recent update, "view plugins", select, install). The server scans the repository and installs from the raw file URL, so the installed copy remembers its `source_url`.

Scanning rules:

- Plugin files are `.js`/`.ts` files of **<= 512 KiB** containing `export const manifest`. At most **40** candidate files are examined.
- Skipped: hidden files and folders (`.github/`, ...), `node_modules/`, `test/`, `tests/`, `__tests__/`, `dist/`, `*.min.js`, `*.d.ts`.
- If the repository root has **`genkan-plugins.json`**, only the files listed there are used: `{ "plugins": ["my-plugin.js", "plugins/other.js"] }`.
- Each file is compiled and validated; failures are shown to the user with the error.
- The market caches search and scan results for about 10 minutes. Without a `GENKAN_GITHUB_TOKEN` on the server GitHub rate limits apply.

### 5.2 After the plugin works: offer to publish

Tell the user: *"Your plugin works. I can publish it to GitHub; with the `genkan-plugin` topic it appears in Genkan's plugin market for everyone to install. Want me to do that?"* Then follow this procedure.

1. **Ask first, and wait for a clear yes.** Publishing is public. Confirm the repository name (default: the plugin id), that it is **public**, and the license (suggest MIT). If the user says no, stop; the plugin stays installed locally.
2. **Pre-publish safety check.**
   - Remove secrets, tokens, passwords, personal hostnames and LAN IPs from the source and from any `default`. User-specific endpoints must be `url` fields with `allowFetch`; credentials must be `secret` fields. Test values used while developing do not belong in the repo.
   - Scan: `grep -nEi "token|passw|secret|api[_-]?key|bearer|192\.168\.|\b10\.[0-9]+\.|\.lan\b|\.local\b|[A-Za-z0-9_-]{32,}" my-plugin.js` and review every hit.
   - Set a proper `version` (for a first release `1.0.0`), `description`, `author`, `homepage` (the repo URL), `icon`.
   - Re-run the checklist in section 6.
3. **Repository layout** (plugin at the root or in `plugins/`):

   ```text
   my-plugin/
   ├── my-plugin.js           # the plugin (or plugins/my-plugin.js)
   ├── genkan-plugins.json    # optional: { "plugins": ["my-plugin.js"] }
   ├── README.md
   └── LICENSE
   ```

   `README.md` should cover: what it does (screenshots optional), the config fields (which are secret, which are URLs), the permissions it requests (`permissions.fetch` hosts, `links`), and how to install: *Settings -> Plugins -> Add -> Third-party -> search "my-plugin"*, or `POST /api/plugins` with `{"url": "https://raw.githubusercontent.com/OWNER/REPO/main/my-plugin.js"}`.
4. **Create the repo and add the topic** with the `gh` CLI, if it is installed and authenticated (`gh auth status`):

   ```bash
   git init -b main && git add . && git commit -m "Add my-plugin"
   gh repo create my-plugin --public --source . --push --description "Genkan plugin: <what it does>"
   gh repo edit OWNER/my-plugin --add-topic genkan-plugin
   ```

   Without `gh`: ask the user to create the repository on github.com, push the files, then add the topic under the repo's **About -> Settings (gear) -> Topics**. Or, with a token that may write to the repo: `curl -X PUT -H "Authorization: Bearer $GITHUB_TOKEN" -H "Accept: application/vnd.github+json" https://api.github.com/repos/OWNER/REPO/topics -d '{"names":["genkan-plugin"]}'` (this call replaces the repo's topic list). Do not use any other tool or workaround.
5. **Verify.**

   ```bash
   gh repo view OWNER/my-plugin --json repositoryTopics
   gk GET "/api/plugins/market/repo?repo=OWNER/my-plugin" | jq '.plugins[] | {path, url, error, name: .plugin.name, version: .plugin.version}'
   gk POST /api/plugins/preview '{"url":"https://raw.githubusercontent.com/OWNER/my-plugin/main/my-plugin.js"}' | jq '{error, name: .plugin.name}'
   ```

   Tell the user that the market caches for about 10 minutes and GitHub's search index can take a while to show a new repo, but installing by raw URL works immediately.
6. **Updates.** Change the code, **bump `version`**, commit and push. Users who installed from the market or a URL have the raw `source_url` recorded; the update button re-downloads it and replaces the code in place, keeping their config and widgets. Do not change `manifest.id` between versions (it would become a different plugin). Keep config keys stable or users lose values.

## 6. Done checklist

Run through this before telling the user the plugin is finished:

- [ ] `manifest.id` is unique and final; `version` set; `name`, `description`, `icon` filled in.
- [ ] Every declared capability has its exported function; `action` is exported if any `button` exists.
- [ ] `permissions.fetch` lists only needed hosts (no `*`), including image and redirect hosts for wallpapers; user-specific hosts use `url` + `allowFetch`.
- [ ] No secret, token, personal hostname or LAN IP in the source or in defaults; credentials are `secret` fields; no secret is logged or returned in the view.
- [ ] `refresh` / `every` values respect the minimums and are not wastefully low.
- [ ] Installed with `enable:false` and **each hook tested**: `ok: true`, no `result.error`, `logs` clean, `ms` well below the budget, `result` equals the intended output (nothing silently dropped).
- [ ] Failure paths tested: unreachable host, bad token, empty config -> a clear `error` in the widget, `[]` for search, a thrown `Error` for wallpapers.
- [ ] Works in both `lang`: `zh` and `en`.
- [ ] No state in globals; storage keys include instance or variable parts; storage usage far below 256 KiB.
- [ ] Plugin enabled, widget added with sensible `w`/`h`, `GET /api/widgets/{id}/data?force=1` returns the expected view.
- [ ] Debug-only logs and test widgets/plugins removed.
- [ ] The user was told: the plugin id, what it does, which hosts/permissions it uses, which config fields they must fill in.
- [ ] **Offered to publish it to GitHub (section 5.2); published only after an explicit yes.**
