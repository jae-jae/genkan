---
name: genkan
description: Organize and maintain a self-hosted Genkan homelab navigation page (workspaces, categories, links, pinned links, health checks). Use when the user asks to add, move, group, rename, sort, clean up or bulk-import bookmarks/services/links on their Genkan (玄関) dashboard, to "organize my homelab start page", or mentions Genkan MCP tools or a gk_ API token.
---

> **WARNING: this file contains a secret API token.** The token below is equivalent to full administrator access to the Genkan instance at `{{GENKAN_URL}}`. Do not share this file, commit it to a repository, or paste it anywhere public. If it leaks, open Genkan, go to Settings, then AI, and click Regenerate; the old token stops working immediately.

# Genkan: organizing a navigation page

This Skill is pre-configured for the Genkan instance at `{{GENKAN_URL}}`.

Genkan is a lightweight self-hosted start page. Data model:

- **Workspace**: a top-level tab (e.g. "Home", "Work"). Has a Lucide `icon` (`lucide:<name>`, one of the 52 built-in names listed below) and a `wallpaper` (`preset:dusk|aurora|ocean|forest|sakura|graphite`; default `preset:dusk`).
- **Category**: a titled group inside one workspace (e.g. "Media", "Monitoring").
- **Link**: a service/bookmark inside a workspace, optionally in a category (no category = "uncategorized", shown last). Fields: `title`, `url`, `description`, `icon`, `pinned` (shown in the pinned row at the top of the workspace AND still in its category; `pin_sort` orders the pinned row, `sort` orders the category), `health_check` (online/offline dot), plus read-only `clicks` and `last_used_at`.

All changes take effect immediately and the open UI refreshes live. There is no undo, so be careful with deletes.

## Preferred path: MCP tools

If the `genkan` MCP server is connected (tools such as `get_overview`, `create_links`), use it. If it is not connected yet, the user can add it with this ready-to-use command (the token is already filled in):

```bash
claude mcp add --transport http genkan {{GENKAN_URL}}/mcp --header "Authorization: Bearer {{GENKAN_TOKEN}}"
```

Other MCP clients: endpoint `{{GENKAN_URL}}/mcp` (Streamable HTTP) with the header `Authorization: Bearer {{GENKAN_TOKEN}}`.

### Tools

| Tool | Purpose |
|---|---|
| `get_overview` | Full tree: workspaces, then categories, then links (ids, clicks, pinned, pin_sort, health_check) plus settings. **Call first.** |
| `search_links` | `query` (space-separated words, all must match title/url/description/category/workspace), optional `workspace_id`. Use before creating, to avoid duplicates. |
| `create_workspace` | `name`, optional `icon` (`lucide:<name>`, see the list below; default `lucide:layout-grid`), `wallpaper`. |
| `update_workspace` | `id`, optional `name`, `icon` (`lucide:<name>`), `wallpaper`. |
| `delete_workspace` | `id`. Deletes all its categories and links; the last workspace cannot be deleted. |
| `create_category` | `workspace_id`, `name`. |
| `update_category` | `id`, `name` (rename). |
| `delete_category` | `id`, optional `delete_links` (default false: links become uncategorized). |
| `create_links` | `links: [{workspace_id, category_id?, title, url, description?, icon?, pinned?, health_check?}]`, up to 200 per call. Icons are fetched automatically when `icon` is empty; `icon` may also be `lucide:<name>`. Duplicate URLs in the same workspace are skipped and reported. |
| `update_link` | `id` plus any of `title`, `url`, `description`, `icon`, `pinned`, `health_check`, `workspace_id`, `category_id` (use `""` for uncategorized). Also used to move links. |
| `delete_links` | `ids: []`. |
| `reorder` | `kind` = `workspaces`, `categories` or `links`; `ids` in desired order; `workspace_id` (categories, links); `category_id` (links: `""` uncategorized, `"__pinned"` the pinned row). Listed links are moved into that category and ordered by `sort`; pinned links are ordinary members of their category. With `"__pinned"` only the pinned row order (`pin_sort`) changes: categories and `sort` are untouched and listed links become pinned. |
| `update_settings` | Any of `title`, `sort_mode` (`frequency`, `recent`, `custom`), `guest_access`, `search_engine`, `open_in_new_tab`, `health_interval`, `show_clock`. |

### Lucide icons (workspaces and links)

Workspace `icon` must be `lucide:<name>` with one of these 52 names (anything else is rejected; the MCP schema lists them as an enum):

`house`, `layout-grid`, `folder`, `briefcase`, `code`, `terminal`, `server`, `database`, `hard-drive`, `cloud`, `network`, `router`, `wifi`, `globe`, `shield`, `lock`, `key-round`, `film`, `clapperboard`, `tv`, `music`, `headphones`, `image`, `camera`, `gamepad-2`, `book-open`, `graduation-cap`, `newspaper`, `shopping-cart`, `wallet`, `chart-line`, `activity`, `gauge`, `cpu`, `wrench`, `settings`, `bot`, `sparkles`, `heart`, `star`, `bookmark`, `users`, `message-circle`, `mail`, `calendar`, `download`, `flask-conical`, `leaf`, `coffee`, `plane`, `lightbulb`, `rocket`.

Pick a fitting one per workspace (e.g. `lucide:house` for Home, `lucide:briefcase` for Work, `lucide:flask-conical` for Lab, `lucide:clapperboard` for Media). Links accept the same names as `lucide:<name>` in their own `icon` field (see Good practice #7).

### Good practice

1. **Always call `get_overview` first.** Work from real ids; never guess them. Re-read after large changes if you need fresh ids.
2. **Group by service type**, with short category names the user would recognize: Media (Jellyfin, Plex), Downloads, Monitoring (Grafana, Uptime Kuma), Network (router, Pi-hole, AdGuard), Storage (NAS, Nextcloud), Dev (Gitea, Portainer), Smart Home (Home Assistant). Reuse existing categories instead of creating near-duplicates ("Media" vs "Medias").
3. **Create categories first, then links in bulk.** One `create_links` call with many items beats many single calls. Use category ids returned by `create_category` or from the overview.
4. **Use workspaces for contexts, not for types** (e.g. "Home" vs "Work" vs "Lab"), and keep the count small.
5. **Pin sparingly.** Pin the handful of links the user opens daily (`pinned: true`); pinned links appear in the pinned row at the top AND stay in their category (pinning does not move them). New pins go to the end of the pinned row; use `reorder` with `category_id: "__pinned"` to change that order.
6. **Turn on `health_check`** for self-hosted services on the LAN that the user wants status dots for (not for external sites unless asked).
7. **Write full URLs** with scheme and port (`http://nas.lan:8096`). `javascript:` URLs are rejected. Leave `icon` empty to auto-fetch a favicon. For a service that has no good favicon (internal tools, bare IPs, generic dashboards) set `icon` to `lucide:<name>` using one of the 52 built-in names listed above (e.g. `lucide:server`, `lucide:database`, `lucide:film`, `lucide:router`); unknown names are rejected per item. Use `emoji:🎬` only when asked.
8. **Titles are short** (service name), put detail in `description`.
9. **Never delete without being asked.** Deleting links, categories or workspaces needs an explicit user request. To "clean up", propose a plan and confirm first, or move things (`update_link`, `reorder`) rather than delete. Prefer `delete_category` with `delete_links=false`.
10. **Don't fight the sort mode.** With `sort_mode: "frequency"` or `"recent"`, link order is automatic; `reorder` only matters for `custom`. Do not change `sort_mode` unless the user asks. Categories and workspaces are always in custom order.
11. After finishing, give the user a brief summary of what changed (counts and names).

### Typical workflow: "organize my services"

1. `get_overview`.
2. Decide the target structure; `search_links` for anything you intend to add.
3. `create_workspace` / `create_category` for what is missing.
4. `create_links` in one batch; check `errors` in the result (invalid URL, duplicate, unknown id) and fix only those items.
5. `update_link` to move existing links into the right categories; `reorder categories` if the order matters.

## Fallback: REST API with curl

Use when MCP is not available. The instance URL and token are already filled in below (the token is secret, see the warning at the top).

Token requests need no CSRF header. All responses are JSON; errors look like `{"error":"..."}`.

```bash
# overview (workspaces, categories, links, settings, health)
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" "{{GENKAN_URL}}/api/bootstrap"

# create a workspace / category / link
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" -H "Content-Type: application/json" -X POST "{{GENKAN_URL}}/api/workspaces" -d '{"name":"Lab","icon":"lucide:flask-conical"}'
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" -H "Content-Type: application/json" -X POST "{{GENKAN_URL}}/api/categories" -d '{"workspace_id":"WS_ID","name":"Media"}'
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" -H "Content-Type: application/json" -X POST "{{GENKAN_URL}}/api/links" \
  -d '{"workspace_id":"WS_ID","category_id":"CAT_ID","title":"Jellyfin","url":"http://nas.lan:8096","health_check":true}'

# edit / move / pin
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" -H "Content-Type: application/json" -X PATCH "{{GENKAN_URL}}/api/links/LINK_ID" -d '{"category_id":"OTHER_CAT_ID","pinned":true}'

# reorder links inside a category (also moves listed links into it); "__pinned" reorders only the pinned row (pin_sort) and leaves categories untouched
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" -H "Content-Type: application/json" -X POST "{{GENKAN_URL}}/api/links/reorder" \
  -d '{"workspace_id":"WS_ID","category_id":"CAT_ID","ids":["L1","L2","L3"]}'
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" -H "Content-Type: application/json" -X POST "{{GENKAN_URL}}/api/categories/reorder" -d '{"workspace_id":"WS_ID","ids":["C1","C2"]}'

# delete (only when the user asked)
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" -H "Content-Type: application/json" -X DELETE "{{GENKAN_URL}}/api/links/LINK_ID"
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" -H "Content-Type: application/json" -X DELETE "{{GENKAN_URL}}/api/categories/CAT_ID?delete_links=1"
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" -H "Content-Type: application/json" -X DELETE "{{GENKAN_URL}}/api/workspaces/WS_ID"

# settings, backup, restore
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" -H "Content-Type: application/json" -X PATCH "{{GENKAN_URL}}/api/settings" -d '{"sort_mode":"custom"}'
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" "{{GENKAN_URL}}/api/export" -o genkan-backup.json
curl -s -H "Authorization: Bearer {{GENKAN_TOKEN}}" -H "Content-Type: application/json" -X POST "{{GENKAN_URL}}/api/import" -d "{\"mode\":\"merge\",\"data\":$(cat genkan-backup.json)}"
```

Tips: `GET /api/wallpapers` lists the uploaded wallpaper gallery and `DELETE /api/wallpapers/FILE` removes one (workspaces using it fall back to `preset:dusk`); take an `/api/export` backup before large restructures; parse ids from `/api/bootstrap` with `jq` (`.workspaces[] | {id,name}`, `.categories[]`, `.links[]`); new links get `sort = last + 1` in their category. `POST /api/links` fetches the favicon in the background unless `"fetch_icon": false` or an `icon` is given.
