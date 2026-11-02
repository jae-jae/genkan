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
}

export interface WallpaperFile {
  value: string
  url: string
  size: number
  created_at: string
}
