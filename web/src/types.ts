export type SortMode = 'frequency' | 'recent' | 'custom'
export type Role = 'admin' | 'guest' | 'none'

export interface Settings {
  title: string
  sort_mode: SortMode
  guest_access: boolean
  search_engine: string
  open_in_new_tab: boolean
  health_interval: number
  show_clock: boolean
  /** check GitHub for new releases (default true) */
  update_check?: boolean
}

export interface Workspace {
  id: string
  name: string
  icon: string
  wallpaper: string
  sort: number
}

export interface Category {
  id: string
  workspace_id: string
  name: string
  sort: number
}

export interface Link {
  id: string
  workspace_id: string
  category_id: string
  title: string
  url: string
  description: string
  icon: string
  pinned: boolean
  sort: number
  pin_sort: number
  clicks: number
  last_used_at: string | null
  health_check: boolean
  created_at: string
  updated_at: string
}

export interface HealthEntry {
  status: 'up' | 'down' | 'unknown'
  latency_ms?: number
  checked_at?: string
  code?: number
}
export type HealthMap = Record<string, HealthEntry>

export interface Bootstrap {
  auth: { setup_required: boolean; role: Role }
  settings: Partial<Settings>
  workspaces: Workspace[] | null
  categories: Category[] | null
  links: Link[] | null
  health: HealthMap | null
  version: number
  server_version?: string
  plugins?: PluginInfo[] | null
  widgets?: WidgetInstance[] | null
}

export interface WallpaperFile {
  value: string
  url: string
  size: number
  created_at: string
}

// ---------- plugins (docs/PLUGINS.md) ----------
/** A user-visible manifest string: plain, or one text per language tag. Show it with `loc()` (i18n.ts), never directly. */
export type LString = string | Record<string, string>
export interface ConfigField {
  key: string
  type: 'string' | 'text' | 'number' | 'boolean' | 'select' | 'secret' | 'url'
  label: LString
  help?: LString
  default?: unknown
  required?: boolean
  options?: { value: string; label: LString }[]
  min?: number
  max?: number
  allowFetch?: boolean
  multiline?: boolean
  verifyTLS?: boolean
}
export type WidgetSize = 's' | 'm' | 'l'
export interface PluginWidgetDef {
  id: string
  name: LString
  size?: WidgetSize
  refresh?: number
  config?: ConfigField[] | null
}
export interface PluginWallpaperDef {
  id: string
  name: LString
  refresh?: number
}
export interface PluginInfo {
  id: string
  name: LString
  version: string
  description?: LString
  author?: string
  homepage?: string
  icon?: string
  enabled: boolean
  permissions?: { fetch?: string[] | null; links?: '' | 'read' | 'write'; ssh?: boolean } | null
  config_schema?: ConfigField[] | null
  wallpapers?: PluginWallpaperDef[] | null
  widgets?: PluginWidgetDef[] | null
  search?: { name: LString; prefix?: string } | null
  schedules?: { id: string; name?: LString; every: number }[] | null
  // admin only
  config?: Record<string, any> | null
  secrets_set?: string[] | null
  source_url?: string
  last_error?: string
  installed_at?: string
  updated_at?: string
}
export interface CatalogItem {
  id: string
  name: LString
  description: LString
  icon: string
  version: string
  installed: boolean
  widgets?: { id: string; name: LString; size?: string }[] | null
}
export interface WidgetInstance {
  id: string
  plugin: string
  widget: string
  workspace_id: string
  config: Record<string, any> | null
  sort: number
  /** columns of 6 (0 = default by manifest size: s 2, m 4, l 6) */
  w?: number
  /** fixed height in px (80-1200); 0 / absent = auto (content height) */
  h?: number
}
export interface PluginLog {
  ts: string | number
  level: string
  msg: string
}
/** POST /api/plugins/preview (docs/PLUGINS.md §11.2): nothing is installed. */
export interface PluginPreview {
  plugin: PluginInfo | null
  source: string
  lang?: 'js' | 'ts'
  installed: boolean
  installed_version?: string
  error?: string
}
/** POST /api/plugins/{id}/test */
export interface PluginTestResult {
  ok: boolean
  result?: unknown
  error?: string
  logs?: (PluginLog | string)[] | null
  ms?: number
}
/** GET /api/plugins/market (docs/PLUGINS.md §11.4) */
export interface MarketRepo {
  repo: string
  description?: string
  stars: number
  updated_at: string
  html_url: string
  owner_avatar?: string
  default_branch?: string
}
export interface MarketList {
  items?: MarketRepo[] | null
  total?: number
  /** Unix milliseconds */
  cached_at?: number
  error?: string
}
export interface MarketPlugin {
  path: string
  url: string
  plugin: PluginInfo | null
  error?: string
  installed: boolean
  installed_version?: string
}
export interface MarketScan {
  repo: string
  default_branch?: string
  cached_at?: number
  plugins?: MarketPlugin[] | null
  error?: string
}

export type Trend = 'up' | 'down' | 'flat'
export interface StatBlock {
  type?: 'stat'
  label: string
  value: string | number
  unit?: string
  icon?: string
  trend?: Trend
}
export interface ListItem {
  title: string
  subtitle?: string
  url?: string
  icon?: string
  image?: string
  right?: string
  badge?: string
  /** interactive widgets (docs/PLUGINS.md 15.2) */
  id?: string
  check?: { checked: boolean; action: string; params?: unknown }
  edit?: { action: string; params?: unknown }
  actions?: { icon: string; label: string; action: string; params?: unknown }[]
}
export type Block =
  | { type: 'text'; text: string; size?: 's' | 'm' | 'l' | 'xl'; muted?: boolean; align?: 'left' | 'center' | 'right' }
  | ({ type: 'stat' } & StatBlock)
  | { type: 'stats'; items: StatBlock[] }
  | { type: 'list'; items: ListItem[]; sortAction?: string; empty?: string }
  | { type: 'kv'; items: { k: string; v: string }[] }
  | { type: 'progress'; label?: string; value: number; text?: string }
  | { type: 'image'; src?: string; alt?: string; href?: string; ratio?: number; images?: { src: string; alt?: string; href?: string }[]; interval?: number; fit?: 'cover' | 'contain'; height?: number }
  | { type: 'iframe'; src: string; height?: number; ratio?: number; crop?: { x?: number; y?: number; width: number; height: number }; zoom?: number; refresh?: number }
  | { type: 'html'; html: string }
  | { type: 'media'; kind: 'video' | 'audio'; src: string; poster?: string; title?: string; autoplay?: boolean; muted?: boolean; loop?: boolean; controls?: boolean }
  | { type: 'chart'; kind: 'spark' | 'bar'; values: number[]; labels?: string[] }
  | { type: 'chart'; kind: 'line'; points: [number, number][]; format?: 'int' | 'compact' | 'percent' | 'decimal'; color?: 'accent' | 'green' | 'red' | 'blue' | 'orange' }
  | { type: 'row'; children: Block[] }
  | { type: 'button'; label: string; action: string; params?: unknown; icon?: string }
  | { type: 'input'; action: string; params?: unknown; placeholder?: string; submit?: string; icon?: string; maxLength?: number; value?: string }
  | { type: 'markdown'; text: string; editAction?: string; params?: unknown; placeholder?: string }
  | { type: 'divider' }
export interface WidgetView {
  title?: string
  icon?: string
  link?: string
  body?: Block[] | null
  error?: string
  tone?: 'yellow' | 'pink' | 'blue' | 'green' | 'purple'
}
/** POST /api/widgets/{id}/action (docs/PLUGINS.md 15.3); `view` has the shape of GET /data (a bare WidgetView is tolerated) */
export interface ActionResult {
  toast?: string
  refresh?: boolean
  view?: WidgetData | WidgetView | null
}
export interface WidgetData {
  view: WidgetView
  updated_at?: string | number
  next_at?: string | number
}
export interface SearchResult {
  title: string
  subtitle?: string
  url?: string
  icon?: string
  copy?: string
}
export interface SearchGroup {
  plugin: string
  name: string
  icon?: string
  results: SearchResult[] | null
}
export interface PluginWallpaperMeta {
  url: string
  title?: string
  credit?: string
  link?: string
  updated_at?: string | number
}

// ---------- updates (docs/UPDATES.md §6) ----------
export interface UpdateRelease {
  version: string
  name: string
  /** markdown */
  notes: string
  published_at: number
  url: string
}
export type UpdatePhase = 'idle' | 'downloading' | 'verifying' | 'installing' | 'restarting' | 'failed'
export interface UpdateStatus {
  current: string
  dev: boolean
  repo: string
  check_enabled: boolean
  env_disabled: boolean
  checking: boolean
  checked_at: number
  error: string
  latest: string
  available: boolean
  dismissed: string
  releases: UpdateRelease[]
  apply: { supported: boolean; reason: '' | 'disabled' | 'platform' | 'noexec' | 'dev' }
  job: { phase: UpdatePhase; version: string; progress: number; error: string }
  rolled_back: string
}
