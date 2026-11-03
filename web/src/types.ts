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
export interface ConfigField {
  key: string
  type: 'string' | 'text' | 'number' | 'boolean' | 'select' | 'secret' | 'url'
  label: string
  help?: string
  default?: unknown
  required?: boolean
  options?: { value: string; label: string }[]
  min?: number
  max?: number
  allowFetch?: boolean
}
export type WidgetSize = 's' | 'm' | 'l'
export interface PluginWidgetDef {
  id: string
  name: string
  size?: WidgetSize
  refresh?: number
  config?: ConfigField[] | null
}
export interface PluginWallpaperDef {
  id: string
  name: string
  refresh?: number
}
export interface PluginInfo {
  id: string
  name: string
  version: string
  description?: string
  author?: string
  homepage?: string
  icon?: string
  enabled: boolean
  permissions?: { fetch?: string[] | null; links?: '' | 'read' | 'write' } | null
  config_schema?: ConfigField[] | null
  wallpapers?: PluginWallpaperDef[] | null
  widgets?: PluginWidgetDef[] | null
  search?: { name: string; prefix?: string } | null
  schedules?: { id: string; every: number }[] | null
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
  name: string
  description: string
  icon: string
  version: string
  installed: boolean
}
export interface WidgetInstance {
  id: string
  plugin: string
  widget: string
  workspace_id: string
  config: Record<string, any> | null
  sort: number
}
export interface PluginLog {
  ts: string | number
  level: string
  msg: string
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
}
export type Block =
  | { type: 'text'; text: string; size?: 's' | 'm' | 'l' | 'xl'; muted?: boolean; align?: 'left' | 'center' | 'right' }
  | ({ type: 'stat' } & StatBlock)
  | { type: 'stats'; items: StatBlock[] }
  | { type: 'list'; items: ListItem[] }
  | { type: 'kv'; items: { k: string; v: string }[] }
  | { type: 'progress'; label?: string; value: number; text?: string }
  | { type: 'image'; src: string; alt?: string; href?: string; ratio?: number }
  | { type: 'chart'; kind: 'spark' | 'bar'; values: number[]; labels?: string[] }
  | { type: 'row'; children: Block[] }
  | { type: 'button'; label: string; action: string; params?: unknown; icon?: string }
  | { type: 'divider' }
export interface WidgetView {
  title?: string
  icon?: string
  link?: string
  body?: Block[] | null
  error?: string
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
