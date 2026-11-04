---
name: genkan
description: Manage and organize the user's self-hosted Genkan (玄関) start page through its REST API with curl. Use when the user wants to add, move, group, rename, sort, pin, clean up or bulk-import bookmarks and services on their Genkan homelab dashboard; create or edit workspaces and categories; change icons, wallpapers or settings; set up health checks; or back up and restore the page. Triggers include "organize my homelab start page", "add these services to Genkan" and any mention of a Genkan instance or a gk_ API token.
---

> **WARNING: the API token is full administrator access to the user's Genkan.** It is stored in `~/.config/genkan/env` or in the `.env` file next to this SKILL.md (or in the `GENKAN_TOKEN` environment variable). Never print it (do not `cat` those files or `echo $GENKAN_TOKEN`), share it, write it into files of a repository, or paste it anywhere. Never regenerate it.

# Genkan: organizing a start page

Genkan is a lightweight self-hosted start page. This Skill drives the user's instance with plain `curl` against its REST API. Every change takes effect immediately and the open page refreshes live. There is no undo, so be careful with deletes. The complete endpoint list (bodies, responses, one curl example each) is in [reference.md](reference.md): read it before using an endpoint you are not sure about.

## Setup

Credentials are looked up in this order: the environment variables `GENKAN_URL` (for example `http://nas.lan:8080`, no trailing slash) and `GENKAN_TOKEN` (starts with `gk_`); then the file `~/.config/genkan/env`; then the `.env` file in this Skill's own directory (the directory you loaded this SKILL.md from, for example `~/.claude/skills/genkan`; the downloaded package ships one). Both files hold `KEY=VALUE` lines. Define this helper once per shell; set `SKILL_DIR` to the real directory of this Skill if it is not the default below. The helper sends JSON with the token (Bearer requests need no CSRF header) and prints the JSON response. `jq` is used below to read responses.

```bash
SKILL_DIR=${SKILL_DIR:-$HOME/.claude/skills/genkan}
for f in "$HOME/.config/genkan/env" "$SKILL_DIR/.env"; do
  [ -n "$GENKAN_URL" ] && [ -n "$GENKAN_TOKEN" ] && break
  [ -r "$f" ] && . "$f"
done
GENKAN_URL=${GENKAN_URL%/}

# gk METHOD PATH [JSON_BODY]
gk() {
  if [ -z "$GENKAN_URL" ] || [ -z "$GENKAN_TOKEN" ]; then
    echo "Genkan is not configured: GENKAN_URL / GENKAN_TOKEN are missing" >&2
    return 1
  fi
  local args=(-sS -X "$1" "$GENKAN_URL$2" -H "Authorization: Bearer $GENKAN_TOKEN" -H "Content-Type: application/json")
  [ -n "$3" ] && args+=(--data "$3")
  curl "${args[@]}"
}
```

If the credentials are missing everywhere (or every call answers 401), stop and ask the user to open Genkan, go to Settings, then AI, and either copy the setup prompt and send it to you (it writes `~/.config/genkan/env`) or download the Skill package again and unzip it over this directory. Check that it works with `gk GET /api/bootstrap | jq '.auth.role'` (must print `"admin"`).

The helper does not fail on HTTP errors, so look for an `error` key in the output. Errors are JSON like `{"error":"..."}` with status 400 (invalid input), 401 (bad token), 404 (unknown id), 409 (conflict) or 413 (too large). Unknown JSON fields are ignored silently, so a misspelled field name does nothing: re-read to verify. Build JSON bodies with `jq -n --arg k v '{...}'` when values come from variables or contain quotes.

## Data model

- **Workspace**: a top-level tab (Home, Work, Lab). Fields: `id`, `name`, `icon` (always `lucide:<name>`, one of 52 built-in names, default `lucide:layout-grid`), `wallpaper`, `sort`.
- **Category**: a titled group inside one workspace (Media, Monitoring). Fields: `id`, `workspace_id`, `name`, `sort`.
- **Link**: a service or bookmark in a workspace, optionally in a category (`category_id` empty = uncategorized, shown last). Fields: `id`, `workspace_id`, `category_id`, `title`, `url`, `description`, `icon`, `pinned`, `sort`, `pin_sort`, `health_check`, plus read-only `clicks`, `last_used_at`, `created_at`, `updated_at`.
- **Link icon** (`icon`): empty = favicon fetched automatically in the background; `lucide:<name>` (one of the 52 names listed in reference.md); `emoji:🎬`; `url:https://…/logo.png`; `upload:<file>` or `favicon:<file>` (files the server already stores; upload your own through `POST /api/uploads/icon`).
- **Wallpaper** (per workspace): `preset:dusk|aurora|ocean|forest|sakura|graphite` (default `preset:dusk`) or `upload:<file>` from the gallery (`POST /api/uploads/wallpaper`, `GET /api/wallpapers`).
- **Pinned**: a pinned link is shown in the pinned row at the top of its workspace AND still in its category. `sort` orders links inside a category, `pin_sort` orders the pinned row. Newly pinned links go to the end of the pinned row.
- **Sort modes** (`settings.sort_mode`): `frequency` (most clicked first), `recent` (last used first), `custom` (by `sort`). Link order only follows `sort` in `custom` mode; categories and workspaces are always in custom order. Do not change the mode unless the user asks.
- **Settings**: `title`, `sort_mode`, `guest_access`, `search_engine`, `open_in_new_tab`, `health_interval`, `show_clock`.

## Workflow

1. **Read first.** `GET /api/bootstrap` returns everything (settings, workspaces, categories, links, health). Work from real ids, never guess them. A compact view:
   ```bash
   gk GET /api/bootstrap | jq '{settings, workspaces: [.workspaces[] | {id,name}], categories: [.categories[] | {id,workspace_id,name}], links: [.links[] | {id,workspace_id,category_id,title,url,pinned}]}'
   ```
2. **Plan.** Decide the target structure; for a large reorganization show the plan to the user and wait for approval.
3. **Back up** before big restructures: `gk GET /api/export > genkan-backup-$(date +%F).json` (restore with `POST /api/import`, see reference.md).
4. **Apply** in dependency order: workspaces, then categories, then links; one request at a time, checking each response for `"error"`.
5. **Verify.** Re-read with `GET /api/bootstrap` and report what changed (counts and names).

## Good practice

1. **Never delete or replace anything unless the user asked.** To "clean up", propose a plan and confirm, or move things (`PATCH`, reorder) instead of deleting. Prefer deleting a category without `delete_links` so its links survive as uncategorized. Never use `POST /api/import` with `"mode":"replace"` unless the user explicitly wants to overwrite everything.
2. **Never regenerate or print the API token**, and never call the auth endpoints: regenerating the token locks this Skill out.
3. **Deduplicate by URL** before creating links: `POST /api/links` does not check for duplicates. Compare against `GET /api/bootstrap` (same workspace, ignore a trailing slash).
4. **Group by service type** with short names the user recognizes: Media (Jellyfin, Plex), Downloads, Monitoring (Grafana, Uptime Kuma), Network (router, Pi-hole), Storage (NAS, Nextcloud), Dev (Gitea, Portainer), Smart Home. Reuse existing categories instead of creating near-duplicates ("Media" vs "Medias"). Use workspaces for contexts (Home, Work, Lab), not for types, and keep their number small.
5. **Keep titles short** (the service name) and put detail in `description`. Write full URLs with scheme and port (`http://nas.lan:8096`); `javascript:` and similar URLs are rejected.
6. **Icons:** leave `icon` empty to get the real favicon automatically. When a service has no good favicon (internal tools, bare IPs, generic dashboards), set `icon` to a fitting `lucide:<name>` (for example `lucide:server`, `lucide:database`, `lucide:film`, `lucide:router`). Use `emoji:` only when asked. Give each workspace a fitting Lucide icon (`lucide:house` for Home, `lucide:briefcase` for Work, `lucide:flask-conical` for Lab).
7. **Pin sparingly:** only the handful of links the user opens daily.
8. **Enable `health_check`** for self-hosted services on the LAN that deserve an online/offline dot, not for external websites unless asked.
9. **Respect the sort mode:** reordering links only has a visible effect in `custom` mode.
10. **Finish with a short summary** of what changed.

## Recipes

Create a category and bulk-add links (skips URLs that already exist in the workspace):

```bash
WS=$(gk GET /api/bootstrap | jq -r '.workspaces[0].id')
CAT=$(gk POST /api/categories "$(jq -n --arg ws "$WS" '{workspace_id:$ws,name:"Media"}')" | jq -r .id)
EXISTING=$(gk GET /api/bootstrap | jq -r --arg ws "$WS" '.links[] | select(.workspace_id==$ws) | .url | sub("/$";"")')
while IFS='|' read -r title url desc; do
  if grep -qxF "$url" <<<"$EXISTING"; then echo "skip $url"; continue; fi
  gk POST /api/links "$(jq -n --arg ws "$WS" --arg cat "$CAT" --arg t "$title" --arg u "$url" --arg d "$desc" \
    '{workspace_id:$ws,category_id:$cat,title:$t,url:$u,description:$d,health_check:true}')" | jq -c '{id,title,error}'
done <<'LINKS'
Jellyfin|http://nas.lan:8096|Movies and TV
Sonarr|http://nas.lan:8989|TV automation
Radarr|http://nas.lan:7878|Movie automation
LINKS
```

Move a link to another category (`""` = uncategorized; also set `workspace_id` to move it to another workspace):

```bash
LINK=$(gk GET /api/bootstrap | jq -r '.links[] | select(.title=="Sonarr") | .id')
DL=$(gk POST /api/categories "$(jq -n --arg ws "$WS" '{workspace_id:$ws,name:"Downloads"}')" | jq -r .id)
gk PATCH /api/links/$LINK "$(jq -n --arg c "$DL" '{category_id:$c}')" | jq '{id,title,category_id}'
```

Pin or unpin a link, and order the pinned row (the ids you list become pinned and are placed first; other pinned links follow):

```bash
gk PATCH /api/links/$LINK '{"pinned":true}' | jq '{title,pinned,pin_sort}'
FIRST=$(gk GET /api/bootstrap | jq -r '.links[] | select(.title=="Jellyfin") | .id')
gk POST /api/links/reorder "$(jq -n --arg ws "$WS" --arg a "$FIRST" --arg b "$LINK" '{workspace_id:$ws,category_id:"__pinned",ids:[$a,$b]}')"
gk PATCH /api/links/$LINK '{"pinned":false}' | jq '{title,pinned}'
```

Reorder categories, and links inside a category (listed links move into that category and come first):

```bash
gk POST /api/categories/reorder "$(jq -n --arg ws "$WS" --arg a "$DL" --arg b "$CAT" '{workspace_id:$ws,ids:[$a,$b]}')"
IDS=$(gk GET /api/bootstrap | jq -c --arg c "$CAT" '[.links[] | select(.category_id==$c)] | sort_by(.title) | map(.id)')
gk POST /api/links/reorder "$(jq -n --arg ws "$WS" --arg c "$CAT" --argjson ids "$IDS" '{workspace_id:$ws,category_id:$c,ids:$ids}')"
```

Change a workspace wallpaper or rename it:

```bash
gk PATCH /api/workspaces/$WS '{"wallpaper":"preset:aurora"}' | jq '{id,name,wallpaper}'
gk PATCH /api/workspaces/$WS '{"name":"Home Lab","icon":"lucide:server"}' | jq '{id,name,icon}'
```

Give a link without a good favicon a Lucide icon, and turn health checks on:

```bash
gk PATCH /api/links/$LINK '{"icon":"lucide:download","health_check":true}' | jq '{title,icon,health_check}'
```

## Plugins

The page can be extended with JavaScript plugins (widgets, wallpaper sources, search providers, scheduled jobs). To add one: `POST /api/plugins` with a built-in `{"catalog":"<id>"}` (see `GET /api/plugins/catalog`), a `{"url":...}` or your own `{"source":...}`; check it with `POST /api/plugins/{id}/test` and the plugin's logs; then place widgets with `POST /api/widgets`. Plugins only reach the hosts they declare, so tell the user which hosts and permissions a plugin asks for (`permissions` in the install response) before enabling one that uses `"*"`. Never put secrets into plugin source; use `secret` config fields.

### Developing plugins

Before writing a plugin, **read the development guide** (manifest, `ctx`, host API, block types, limits, recipes, error list): `curl -sL https://raw.githubusercontent.com/jae-jae/genkan/main/docs/PLUGIN_DEV.md`. Endpoints are in the Plugins section of [reference.md](reference.md). The loop is always:

1. Install **disabled**: `jq -Rs '{source: ., enable: false}' my-plugin.js > body.json`, then `gk POST /api/plugins @body.json` (new plugins are enabled by default, so never omit `enable`).
2. `POST /api/plugins/{id}/test` for every hook; read `ok`, `error`, `result.error` and `logs` (and `GET /api/plugins/{id}/logs`). Fix with `PUT /api/plugins/{id}/source`.
3. Only when the tests pass: `PATCH /api/plugins/{id}` `{"enabled":true}`, add the widget with `POST /api/widgets`, check `GET /api/widgets/{id}/data?force=1`.
4. When it works, offer to publish it: a public GitHub repo with the topic `genkan-plugin` appears in Genkan's plugin market for everyone. Never publish without asking; the guide has the safe procedure (remove secrets and LAN hosts first).

## Next

See [reference.md](reference.md) for every endpoint (settings, workspaces, categories, links, reorder, icon refresh, health, uploads, wallpapers, export, import, plugins and widgets), request and response shapes, limits, error codes and the full list of Lucide icon names.
