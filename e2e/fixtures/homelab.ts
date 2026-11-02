// Realistic homelab dataset used by screenshots.spec.ts.
// Icons come from e2e/fixtures/icons (downloaded once from homarr-labs/dashboard-icons) and are uploaded
// through POST /api/uploads/icon, so the screenshots are reproducible offline.

export type Health = 'up' | 'down' | undefined

export interface SvcDef {
  id: string
  ws: 'home' | 'media' | 'dev' | 'net'
  cat?: string // category key, undefined = uncategorised
  title: string
  /** hostname (all *.lan names resolve to 127.0.0.1 inside the e2e container) */
  host: string
  port: number
  scheme?: 'http' | 'https'
  icon?: string // file name in fixtures/icons (without extension)
  clicks: number
  /** minutes since last use (undefined = never) */
  ago?: number
  pinned?: boolean
  health?: Health
  desc_zh: string
  desc_en: string
  /** public site, not a lan host */
  external?: string
}

export const WORKSPACES = [
  { key: 'home', icon: 'lucide:house', zh: '家庭', en: 'Home', wallpaper: 'preset:dusk' },
  { key: 'media', icon: 'lucide:clapperboard', zh: '媒体', en: 'Media', wallpaper: 'preset:ocean' },
  { key: 'dev', icon: 'lucide:code', zh: '开发', en: 'Dev', wallpaper: 'preset:aurora' },
  { key: 'net', icon: 'lucide:network', zh: '网络', en: 'Network', wallpaper: 'preset:graphite' },
] as const

export const CATEGORIES = [
  { key: 'infra', ws: 'home', zh: '基础设施', en: 'Infrastructure' },
  { key: 'smart', ws: 'home', zh: '智能家居', en: 'Smart Home' },
  { key: 'mon', ws: 'home', zh: '监控', en: 'Monitoring' },
  { key: 'tools', ws: 'home', zh: '工具', en: 'Tools' },
  { key: 'media', ws: 'media', zh: '媒体', en: 'Media' },
  { key: 'dl', ws: 'media', zh: '下载', en: 'Downloads' },
  { key: 'code', ws: 'dev', zh: '开发', en: 'Development' },
  { key: 'net', ws: 'net', zh: '网络', en: 'Network' },
] as const

export const SERVICES: SvcDef[] = [
  // ---- Home ----
  { id: 'ha', ws: 'home', cat: 'smart', title: 'Home Assistant', host: 'ha.lan', port: 8123, icon: 'home-assistant', clicks: 212, ago: 12, pinned: true, health: 'up', desc_zh: '智能家居中枢', desc_en: 'Smart home hub' },
  { id: 'immich', ws: 'home', cat: 'tools', title: 'Immich', host: 'immich.lan', port: 2283, icon: 'immich', clicks: 148, ago: 95, pinned: true, health: 'up', desc_zh: '照片备份与相册', desc_en: 'Photo backup and gallery' },
  { id: 'vault', ws: 'home', cat: 'tools', title: 'Vaultwarden', host: 'vault.lan', port: 8222, icon: 'vaultwarden', clicks: 120, ago: 240, pinned: true, health: 'up', desc_zh: '密码管理器', desc_en: 'Password manager' },
  { id: 'cloud', ws: 'home', cat: 'tools', title: 'Nextcloud', host: 'cloud.lan', port: 443, scheme: 'https', icon: 'nextcloud', clicks: 95, ago: 600, pinned: true, health: 'up', desc_zh: '私有云网盘', desc_en: 'Private cloud storage' },
  { id: 'pve', ws: 'home', cat: 'infra', title: 'Proxmox VE', host: 'pve.lan', port: 8006, scheme: 'https', icon: 'proxmox', clicks: 86, ago: 30, health: 'up', desc_zh: '虚拟化平台', desc_en: 'Virtualization host' },
  { id: 'truenas', ws: 'home', cat: 'infra', title: 'TrueNAS', host: 'nas.lan', port: 443, scheme: 'https', icon: 'truenas-scale', clicks: 64, ago: 400, health: 'up', desc_zh: '存储与 NAS', desc_en: 'Storage and NAS' },
  { id: 'portainer', ws: 'home', cat: 'infra', title: 'Portainer', host: 'docker.lan', port: 9443, scheme: 'https', icon: 'portainer', clicks: 41, ago: 1500, health: 'up', desc_zh: 'Docker 容器管理', desc_en: 'Docker management' },
  { id: 'homebridge', ws: 'home', cat: 'smart', title: 'Homebridge', host: 'homebridge.lan', port: 8581, icon: 'homebridge', clicks: 12, ago: 4000, desc_zh: 'HomeKit 桥接', desc_en: 'HomeKit bridge' },
  { id: 'frigate', ws: 'home', cat: 'smart', title: 'Frigate', host: 'frigate.lan', port: 5000, icon: 'frigate', clicks: 37, ago: 2200, health: 'down', desc_zh: '摄像头 NVR', desc_en: 'Camera NVR' },
  { id: 'grafana', ws: 'home', cat: 'mon', title: 'Grafana', host: 'grafana.lan', port: 3000, icon: 'grafana', clicks: 58, ago: 180, health: 'up', desc_zh: '指标仪表盘', desc_en: 'Metrics dashboards' },
  { id: 'kuma', ws: 'home', cat: 'mon', title: 'Uptime Kuma', host: 'status.lan', port: 3001, icon: 'uptime-kuma', clicks: 73, ago: 55, health: 'up', desc_zh: '服务可用性监控', desc_en: 'Uptime monitoring' },
  { id: 'paperless', ws: 'home', cat: 'tools', title: 'Paperless-ngx', host: 'paperless.lan', port: 8000, icon: 'paperless-ngx', clicks: 33, ago: 3000, health: 'up', desc_zh: '文档归档', desc_en: 'Document archive' },
  { id: 'syncthing', ws: 'home', cat: 'tools', title: 'Syncthing', host: 'sync.lan', port: 8384, icon: 'syncthing', clicks: 19, ago: 9000, health: 'down', desc_zh: '文件同步', desc_en: 'File sync' },
  // ---- Media ----
  { id: 'jellyfin', ws: 'media', cat: 'media', title: 'Jellyfin', host: 'jellyfin.lan', port: 8096, icon: 'jellyfin', clicks: 260, ago: 8, pinned: true, health: 'up', desc_zh: '影视媒体库', desc_en: 'Movies and TV' },
  { id: 'plex', ws: 'media', cat: 'media', title: 'Plex', host: 'plex.lan', port: 32400, icon: 'plex', clicks: 140, ago: 70, pinned: true, health: 'up', desc_zh: '流媒体服务器', desc_en: 'Media server' },
  { id: 'navidrome', ws: 'media', cat: 'media', title: 'Navidrome', host: 'music.lan', port: 4533, icon: 'navidrome', clicks: 52, ago: 360, health: 'up', desc_zh: '音乐服务器', desc_en: 'Music server' },
  { id: 'abs', ws: 'media', cat: 'media', title: 'Audiobookshelf', host: 'books.lan', port: 13378, icon: 'audiobookshelf', clicks: 26, ago: 5000, desc_zh: '有声书与播客', desc_en: 'Audiobooks and podcasts' },
  { id: 'youtube', ws: 'media', cat: 'media', title: 'YouTube', host: 'www.youtube.com', port: 443, scheme: 'https', icon: 'youtube', clicks: 180, ago: 20, external: 'https://www.youtube.com', desc_zh: '视频网站', desc_en: 'Video' },
  { id: 'qbit', ws: 'media', cat: 'dl', title: 'qBittorrent', host: 'qbit.lan', port: 8080, icon: 'qbittorrent', clicks: 88, ago: 45, health: 'up', desc_zh: 'BT 下载', desc_en: 'Torrent client' },
  { id: 'sonarr', ws: 'media', cat: 'dl', title: 'Sonarr', host: 'sonarr.lan', port: 8989, icon: 'sonarr', clicks: 79, ago: 100, health: 'up', desc_zh: '剧集自动化', desc_en: 'TV automation' },
  { id: 'radarr', ws: 'media', cat: 'dl', title: 'Radarr', host: 'radarr.lan', port: 7878, icon: 'radarr', clicks: 71, ago: 110, health: 'up', desc_zh: '电影自动化', desc_en: 'Movie automation' },
  { id: 'prowlarr', ws: 'media', cat: 'dl', title: 'Prowlarr', host: 'prowlarr.lan', port: 9696, icon: 'prowlarr', clicks: 30, ago: 800, desc_zh: '索引器管理', desc_en: 'Indexer manager' },
  { id: 'bazarr', ws: 'media', cat: 'dl', title: 'Bazarr', host: 'bazarr.lan', port: 6767, icon: 'bazarr', clicks: 14, ago: 7000, desc_zh: '字幕下载', desc_en: 'Subtitles' },
  { id: 'overseerr', ws: 'media', cat: 'dl', title: 'Overseerr', host: 'request.lan', port: 5055, icon: 'overseerr', clicks: 44, ago: 500, health: 'up', desc_zh: '影视点播请求', desc_en: 'Media requests' },
  // ---- Dev ----
  { id: 'gitea', ws: 'dev', cat: 'code', title: 'Gitea', host: 'git.lan', port: 3000, icon: 'gitea', clicks: 110, ago: 25, pinned: false, health: 'up', desc_zh: '自托管 Git 服务', desc_en: 'Self-hosted Git' },
  { id: 'github', ws: 'dev', cat: 'code', title: 'GitHub', host: 'github.com', port: 443, scheme: 'https', icon: 'github', clicks: 205, ago: 5, external: 'https://github.com', desc_zh: '代码托管', desc_en: 'Code hosting' },
  { id: 'dockerhub', ws: 'dev', cat: 'code', title: 'Docker Hub', host: 'hub.docker.com', port: 443, scheme: 'https', icon: 'docker', clicks: 47, ago: 700, external: 'https://hub.docker.com', desc_zh: '镜像仓库', desc_en: 'Image registry' },
  // ---- Network ----
  { id: 'openwrt', ws: 'net', cat: 'net', title: 'OpenWrt', host: 'router.lan', port: 80, icon: 'openwrt', clicks: 66, ago: 35, health: 'up', desc_zh: '主路由', desc_en: 'Main router' },
  { id: 'pihole', ws: 'net', cat: 'net', title: 'Pi-hole', host: 'pihole.lan', port: 80, icon: 'pi-hole', clicks: 90, ago: 15, health: 'up', desc_zh: '广告拦截 DNS', desc_en: 'DNS ad blocking' },
  { id: 'adguard', ws: 'net', cat: 'net', title: 'AdGuard Home', host: 'adguard.lan', port: 3000, icon: 'adguard-home', clicks: 35, ago: 1800, health: 'down', desc_zh: '备用 DNS', desc_en: 'Secondary DNS' },
  { id: 'traefik', ws: 'net', cat: 'net', title: 'Traefik', host: 'traefik.lan', port: 8080, icon: 'traefik', clicks: 22, ago: 3500, health: 'up', desc_zh: '反向代理', desc_en: 'Reverse proxy' },
  { id: 'tailscale', ws: 'net', cat: 'net', title: 'Tailscale', host: 'login.tailscale.com', port: 443, scheme: 'https', icon: 'tailscale', clicks: 28, ago: 2600, external: 'https://login.tailscale.com/admin', desc_zh: '组网控制台', desc_en: 'Mesh VPN console' },
  { id: 'cloudflare', ws: 'net', cat: 'net', title: 'Cloudflare', host: 'dash.cloudflare.com', port: 443, scheme: 'https', icon: 'cloudflare', clicks: 17, ago: 6000, external: 'https://dash.cloudflare.com', desc_zh: 'DNS 与隧道', desc_en: 'DNS and tunnels' },
]

/** Every *.lan host used above: the e2e container maps them to 127.0.0.1 so health checks give mixed up/down. */
export const LAN_HOSTS = [...new Set(SERVICES.filter((s) => !s.external).map((s) => s.host))]

/** The server-side URL a service points at: "up" services target the container's own /healthz. */
export function svcUrl(s: SvcDef): string {
  if (s.external) return s.external
  if (s.health === 'up') return `http://${s.host}:8080/healthz`
  return `${s.scheme ?? 'http'}://${s.host}:${s.port}`
}
