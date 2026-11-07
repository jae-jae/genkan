#!/usr/bin/env bash
# FICTIONAL DEMO DATA
#
# Fills a local Genkan instance with made-up homelab bookmarks for screenshots
# and short videos. Every hostname (*.lan, 192.168.88.x) is invented. This
# script does not talk to real services, and it does not install plugins from
# the network. Link icons are the Apache-2.0 SVGs already in
# e2e/fixtures/icons (homarr-labs/dashboard-icons).
#
# Usage (a fresh data directory — the server must already be running):
#   GENKAN_URL=http://127.0.0.1:8787 GENKAN_DATA=/tmp/genkan-demo-data \
#     bash scripts/demo-seed.sh
#
# The API token is read from $GENKAN_DATA/genkan.json (or $GENKAN_TOKEN).
# It is never printed. Re-running on an instance that already has a "Media"
# workspace does nothing, unless DEMO_SEED_FORCE=1 (which is refused: delete
# the data directory instead).

set -euo pipefail

URL=${GENKAN_URL:-http://127.0.0.1:8787}
URL=${URL%/}
DATA=${GENKAN_DATA:-/tmp/genkan-demo-data}
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
ICONS="$ROOT/e2e/fixtures/icons"

if [[ -z "${GENKAN_TOKEN:-}" ]]; then
  GENKAN_TOKEN=$(jq -r '.auth.api_token // empty' "$DATA/genkan.json")
fi
if [[ -z "${GENKAN_TOKEN:-}" || "$GENKAN_TOKEN" == "null" ]]; then
  echo "demo-seed: no API token (set GENKAN_TOKEN or GENKAN_DATA)" >&2
  exit 1
fi

gk() {
  local method=$1 path=$2 body=${3:-}
  local tmp code
  tmp=$(mktemp)
  if [[ -n "$body" ]]; then
    code=$(curl -sS -o "$tmp" -w '%{http_code}' -X "$method" "$URL$path" \
      -H "Authorization: Bearer $GENKAN_TOKEN" \
      -H 'Content-Type: application/json' \
      --data "$body")
  else
    code=$(curl -sS -o "$tmp" -w '%{http_code}' -X "$method" "$URL$path" \
      -H "Authorization: Bearer $GENKAN_TOKEN" \
      -H 'Content-Type: application/json')
  fi
  if [[ "$code" != 2* ]]; then
    echo "demo-seed: $method $path -> HTTP $code" >&2
    cat "$tmp" >&2
    echo >&2
    rm -f "$tmp"
    exit 1
  fi
  cat "$tmp"
  rm -f "$tmp"
}

if gk GET /api/bootstrap | jq -e '.workspaces[]? | select(.name=="Media")' >/dev/null; then
  echo "demo-seed: Media workspace already exists; leaving this instance alone."
  echo "demo-seed: use a fresh GENKAN_DATA directory to seed again."
  exit 0
fi

declare -A ICON
upload_icon() {
  local name=$1 file=$2 tmp code
  [[ -n "${ICON[$name]:-}" ]] && return 0
  tmp=$(mktemp)
  code=$(curl -sS -o "$tmp" -w '%{http_code}' -X POST "$URL/api/uploads/icon" \
    -H "Authorization: Bearer $GENKAN_TOKEN" \
    -F "file=@${file};type=image/svg+xml;filename=${name}.svg")
  if [[ "$code" != 2* ]]; then
    echo "demo-seed: icon $name -> HTTP $code" >&2
    cat "$tmp" >&2
    echo >&2
    rm -f "$tmp"
    exit 1
  fi
  ICON[$name]=$(jq -r '.value' "$tmp")
  rm -f "$tmp"
}

# Service logos used only as link icons. Missing files fall back to a Lucide icon.
for pair in \
  home-assistant:home-assistant.svg \
  homebridge:homebridge.svg \
  proxmox:proxmox.svg \
  truenas:truenas-scale.svg \
  portainer:portainer.svg \
  grafana:grafana.svg \
  nextcloud:nextcloud.svg \
  immich:immich.svg \
  vaultwarden:vaultwarden.svg \
  paperless:paperless-ngx.svg \
  jellyfin:jellyfin.svg \
  plex:plex.svg \
  navidrome:navidrome.svg \
  sonarr:sonarr.svg \
  radarr:radarr.svg \
  qbittorrent:qbittorrent.svg \
  prowlarr:prowlarr.svg \
  overseerr:overseerr.svg \
  gitea:gitea.svg \
  pihole:pi-hole.svg \
  openwrt:openwrt.svg \
  adguard:adguard-home.svg \
  traefik:traefik.svg \
  bazarr:bazarr.svg
 do
  key=${pair%%:*}
  file=$ICONS/${pair#*:}
  if [[ -f "$file" ]]; then
    upload_icon "$key" "$file"
  fi
done

echo "demo-seed: icons uploaded (${#ICON[@]})"

# Settings. Language is a browser preference (localStorage), not an API field.
gk PATCH /api/settings "$(jq -n '{
  title: "Genkan",
  sort_mode: "custom",
  show_clock: true,
  open_in_new_tab: true,
  guest_access: false,
  update_check: false
}')" >/dev/null

HOME=$(gk GET /api/bootstrap | jq -r '.workspaces[0].id')
gk PATCH "/api/workspaces/$HOME" '{"name":"Home","icon":"lucide:house","wallpaper":"preset:dusk"}' >/dev/null

ws_new() {
  gk POST /api/workspaces "$1" | jq -r '.id'
}
MEDIA=$(ws_new '{"name":"Media","icon":"lucide:clapperboard","wallpaper":"preset:sakura"}')
DEV=$(ws_new '{"name":"Dev","icon":"lucide:code","wallpaper":"preset:aurora"}')
NET=$(ws_new '{"name":"Network","icon":"lucide:network","wallpaper":"preset:ocean"}')

cat_new() {
  local ws=$1 name=$2
  gk POST /api/categories "$(jq -n --arg ws "$ws" --arg name "$name" '{workspace_id:$ws,name:$name}')" | jq -r '.id'
}

C_SMART=$(cat_new "$HOME" "智能家居")
C_INFRA=$(cat_new "$HOME" "基础设施")
C_MON=$(cat_new "$HOME" "监控")
C_TOOLS=$(cat_new "$HOME" "工具")
C_WATCH=$(cat_new "$MEDIA" "影视")
C_DL=$(cat_new "$MEDIA" "下载")
C_CODE=$(cat_new "$DEV" "开发")
C_NET=$(cat_new "$NET" "网络")

link_new() {
  # $1 ws  $2 cat  $3 title  $4 url  $5 description  $6 icon-or-empty  $7 pinned true/false
  local ws=$1 cat=$2 title=$3 url=$4 desc=$5 icon=$6 pinned=$7
  gk POST /api/links "$(jq -n \
    --arg ws "$ws" --arg cat "$cat" --arg title "$title" --arg url "$url" --arg desc "$desc" --arg icon "$icon" \
    --argjson pinned "$pinned" \
    '{workspace_id:$ws,category_id:$cat,title:$title,url:$url,description:$desc,icon:$icon,pinned:$pinned,fetch_icon:false,health_check:false}')" \
    | jq -e '.id' >/dev/null
}

ico() { printf '%s' "${ICON[$1]:-}"; }

# ---- Home (fictional LAN) ----
link_new "$HOME" "$C_SMART" "Home Assistant" "http://ha.lan:8123" "智能家居中枢" "$(ico home-assistant)" true
link_new "$HOME" "$C_SMART" "Homebridge" "http://homebridge.lan:8581" "HomeKit 桥接" "$(ico homebridge)" false
link_new "$HOME" "$C_INFRA" "Proxmox" "https://pve.lan:8006" "虚拟化" "$(ico proxmox)" false
link_new "$HOME" "$C_INFRA" "TrueNAS" "https://nas.lan" "存储" "$(ico truenas)" false
link_new "$HOME" "$C_INFRA" "Portainer" "https://docker.lan:9443" "容器" "$(ico portainer)" false
link_new "$HOME" "$C_MON" "Grafana" "http://grafana.lan:3000" "仪表盘" "$(ico grafana)" true
link_new "$HOME" "$C_TOOLS" "Nextcloud" "https://cloud.lan" "网盘" "$(ico nextcloud)" true
link_new "$HOME" "$C_TOOLS" "Immich" "http://photos.lan:2283" "相册" "$(ico immich)" false
link_new "$HOME" "$C_TOOLS" "Vaultwarden" "http://vault.lan:8222" "密码" "$(ico vaultwarden)" false
link_new "$HOME" "$C_TOOLS" "Paperless" "http://paper.lan:8000" "文档" "$(ico paperless)" false

# ---- Media ----
link_new "$MEDIA" "$C_WATCH" "Jellyfin" "http://jellyfin.lan:8096" "电影和剧集" "$(ico jellyfin)" true
link_new "$MEDIA" "$C_WATCH" "Plex" "http://plex.lan:32400" "流媒体" "$(ico plex)" false
link_new "$MEDIA" "$C_WATCH" "Navidrome" "http://music.lan:4533" "音乐" "$(ico navidrome)" false
link_new "$MEDIA" "$C_DL" "Sonarr" "http://sonarr.lan:8989" "剧集" "$(ico sonarr)" false
link_new "$MEDIA" "$C_DL" "Radarr" "http://radarr.lan:7878" "电影" "$(ico radarr)" false
link_new "$MEDIA" "$C_DL" "qBittorrent" "http://qbit.lan:8081" "下载" "$(ico qbittorrent)" false
link_new "$MEDIA" "$C_DL" "Prowlarr" "http://prowlarr.lan:9696" "索引" "$(ico prowlarr)" false
link_new "$MEDIA" "$C_DL" "Overseerr" "http://request.lan:5055" "点播" "$(ico overseerr)" false

# ---- Dev ----
link_new "$DEV" "$C_CODE" "Gitea" "http://git.lan:3000" "Git" "$(ico gitea)" true
link_new "$DEV" "$C_CODE" "Woodpecker" "http://ci.lan:8000" "持续集成" "lucide:rocket" false
link_new "$DEV" "$C_CODE" "Adminer" "http://db.lan:8088" "数据库" "lucide:database" false

# ---- Network ----
link_new "$NET" "$C_NET" "OpenWrt" "http://192.168.88.1" "路由" "$(ico openwrt)" false
link_new "$NET" "$C_NET" "Pi-hole" "http://pihole.lan/admin" "DNS 过滤" "$(ico pihole)" true
link_new "$NET" "$C_NET" "AdGuard Home" "http://adguard.lan:3000" "备用 DNS" "$(ico adguard)" false
link_new "$NET" "$C_NET" "Traefik" "http://proxy.lan:8080" "反向代理" "$(ico traefik)" false

# Built-in todo and note install themselves on first start. This script does
# not place their widgets and does not install anything from GitHub.
# Bazarr is intentionally absent: the live-update recording adds it over the API.

counts=$(gk GET /api/bootstrap | jq -c '{workspaces:(.workspaces|length),categories:(.categories|length),links:(.links|length)}')
echo "demo-seed: done $counts"
echo "demo-seed: fictional data only. No plugin from jae-jae/genkan-plugins was installed."
