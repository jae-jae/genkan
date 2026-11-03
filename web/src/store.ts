import { signal, computed, batch } from '@preact/signals'
import { ApiError, get, post, patch, del, setUnauthorizedHandler } from './api'
import { t } from './i18n'
import type { Bootstrap, Category, HealthMap, Link, PluginInfo, Role, Settings, WallpaperFile, WidgetInstance, Workspace } from './types'
import { lsGet, lsSet, sortLinks } from './util'

export const DEFAULTS: Settings = {
  title: 'Genkan',
  sort_mode: 'frequency',
  guest_access: false,
  search_engine: 'https://www.google.com/search?q=%s',
  open_in_new_tab: true,
  health_interval: 60,
  show_clock: true,
}

export const loaded = signal(false)
export const loadError = signal(false)
export const role = signal<Role>('none')
export const setupRequired = signal(false)
export const settings = signal<Settings>(DEFAULTS)
export const workspaces = signal<Workspace[]>([])
export const categories = signal<Category[]>([])
export const links = signal<Link[]>([])
export const health = signal<HealthMap>({})
export const plugins = signal<PluginInfo[]>([])
export const widgets = signal<WidgetInstance[]>([])
export const version = signal(0)
export const serverVersion = signal('')
export const wsId = signal(lsGet('genkan.ws') || '')
export const editMode = signal(false)
export const showLogin = signal(false)

export type ModalState =
  | { kind: 'link'; id?: string; defaults?: { workspace_id: string; category_id: string; pinned: boolean } }
  | { kind: 'workspace'; id?: string }
  | { kind: 'category'; id?: string; workspace_id: string }
  | { kind: 'settings'; tab?: string }
  | { kind: 'widget'; id?: string }
  | { kind: 'plugin-perm'; plugin: PluginInfo; fresh: boolean }
  // other plugin dialogs reuse this kind: it is the one App.tsx already renders (via the lazy Plugins chunk)
  | { kind: 'plugin-perm'; dlg: 'source'; plugin: PluginInfo }
  | { kind: 'plugin-perm'; dlg: 'batch' | 'ai' }
  | {
      kind: 'confirm'
      title: string
      message: string
      confirmLabel: string
      checkbox?: string
      onConfirm: (checked: boolean) => void
    }
export const modal = signal<ModalState | null>(null)
/** Settings modal parked underneath an editor / confirm opened from inside it (content manager, gallery). */
const under = signal<ModalState | null>(null)
export const closeModal = () => {
  modal.value = under.value
  under.value = null
}
export const resetModals = () => {
  modal.value = null
  under.value = null
}
/** Open `s` on top of the current Settings modal; closing it returns to Settings. */
export function showOver(s: ModalState) {
  if (modal.value?.kind === 'settings') under.value = modal.value
  modal.value = s
}
export function openSettings(tab?: string) {
  settingsTab.value = tab === 'mcp' ? 'ai' : tab || 'general'
  cmQuery.value = ''
  under.value = null
  modal.value = { kind: 'settings', tab }
}
export const settingsTab = signal('general')
/** search text of the content manager (a signal so it survives the editors stacked on top of Settings) */
export const cmQuery = signal('')
/** workspace selected in Settings → Content ('' = follow the current workspace) */
export const cmWs = signal('')

export interface CtxItem {
  id: string
  label: string
  icon?: string
  onSelect: () => void
  danger?: boolean
  /** renders as a menuitemcheckbox */
  checked?: boolean
  /** separator instead of an item */
  sep?: boolean
}
export interface CtxState {
  key: number
  x: number
  y: number
  items: CtxItem[]
  sheet: boolean
  testid: string
  title?: string
}
export const ctx = signal<CtxState | null>(null)
export const closeCtx = () => (ctx.value = null)

export const isAdmin = computed(() => role.value === 'admin')
/** true when at least one enabled plugin provides search (no plugin search requests otherwise) */
export const hasSearchPlugin = computed(() => plugins.value.some((p) => p.enabled && !!p.search))
export const sortedWs = computed(() => workspaces.value.slice().sort((a, b) => a.sort - b.sort))
export const curWs = computed(() => sortedWs.value.find((w) => w.id === wsId.value) || sortedWs.value[0] || null)

export function setWs(id: string) {
  wsId.value = id
  lsSet('genkan.ws', id)
}

// ---------- toasts ----------
export interface Toast {
  id: number
  msg: string
  kind: 'info' | 'error' | 'success'
}
export const toasts = signal<Toast[]>([])
let toastSeq = 0
export function toast(msg: string, kind: Toast['kind'] = 'info') {
  const id = ++toastSeq
  toasts.value = [...toasts.value.slice(-3), { id, msg, kind }]
  setTimeout(() => dismissToast(id), kind === 'error' ? 5200 : 3200)
}
export const dismissToast = (id: number) => (toasts.value = toasts.value.filter((x) => x.id !== id))
export function toastError(e: unknown) {
  if (e instanceof ApiError) toast(e.status === 0 ? t('err.network') : e.message || t('err.generic'), 'error')
  else toast(t('err.generic'), 'error')
}

// ---------- bootstrap ----------
let inflight = false
let again = false
export async function load(): Promise<void> {
  if (inflight) {
    again = true
    return
  }
  inflight = true
  try {
    const b = await get<Bootstrap>('/api/bootstrap')
    batch(() => {
      role.value = b.auth.role
      setupRequired.value = b.auth.setup_required
      settings.value = { ...DEFAULTS, ...(b.settings || {}) }
      workspaces.value = b.workspaces || []
      categories.value = b.categories || []
      links.value = b.links || []
      health.value = b.health || {}
      plugins.value = b.plugins || []
      widgets.value = b.widgets || []
      version.value = b.version || 0
      serverVersion.value = b.server_version || ''
      loadError.value = false
      loaded.value = true
      if (b.auth.role !== 'admin') editMode.value = false
      if (b.auth.role === 'none') resetModals()
      else showLogin.value = false
    })
    if (b.auth.role === 'none') disconnect()
    else connect()
  } catch {
    if (!loaded.value) loadError.value = true
  } finally {
    inflight = false
    if (again) {
      again = false
      load()
    }
  }
}

setUnauthorizedHandler(() => {
  if (role.value !== 'none') {
    toast(t('err.session'), 'error')
    load()
  }
})

// ---------- SSE ----------
let es: EventSource | null = null
let backoff = 1000
let retryTimer = 0
let changeTimer = 0
let wasDown = false
function connect() {
  if (es || typeof EventSource === 'undefined') return
  clearTimeout(retryTimer)
  const src = new EventSource('/api/events')
  es = src
  src.onopen = () => {
    backoff = 1000
    if (wasDown) {
      wasDown = false
      load()
    }
  }
  src.addEventListener('change', (ev) => {
    let v = 0
    try {
      v = JSON.parse((ev as MessageEvent).data).version
    } catch {}
    if (v && v === version.value) return
    clearTimeout(changeTimer)
    changeTimer = window.setTimeout(load, 300)
  })
  src.addEventListener('health', (ev) => {
    try {
      health.value = JSON.parse((ev as MessageEvent).data)
    } catch {}
  })
  src.onerror = () => {
    src.close()
    if (es === src) es = null
    wasDown = true
    clearTimeout(retryTimer)
    retryTimer = window.setTimeout(() => {
      if (role.value !== 'none') connect()
    }, backoff)
    backoff = Math.min(backoff * 2, 30000)
  }
}
function disconnect() {
  clearTimeout(retryTimer)
  es?.close()
  es = null
}
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible' && loaded.value) {
    load()
  }
})

// ---------- helpers ----------
async function attempt<T>(fn: () => Promise<T>, rollback = true): Promise<T | undefined> {
  try {
    return await fn()
  } catch (e) {
    toastError(e)
    if (rollback) load()
    return undefined
  }
}

// ---------- auth ----------
export async function login(password: string) {
  await post('/api/auth/login', { password })
  await load()
}
export async function setup(password: string) {
  await post('/api/auth/setup', { password })
  await load()
}
export async function logout() {
  await attempt(() => post('/api/auth/logout'), false)
  editMode.value = false
  resetModals()
  closeCtx()
  await load()
}

// ---------- settings ----------
export async function patchSettings(p: Partial<Settings>) {
  settings.value = { ...settings.value, ...p }
  const s = await attempt(() => patch<Settings>('/api/settings', p))
  if (s) settings.value = { ...DEFAULTS, ...s }
}

// ---------- workspaces ----------
export async function createWorkspace(b: { name: string; icon?: string; wallpaper?: string }) {
  const w = await attempt(() => post<Workspace>('/api/workspaces', b), false)
  if (w) {
    workspaces.value = [...workspaces.value, w]
    setWs(w.id)
  }
  return w
}
export async function updateWorkspace(id: string, p: Partial<Workspace>) {
  workspaces.value = workspaces.value.map((w) => (w.id === id ? { ...w, ...p } : w))
  const w = await attempt(() => patch<Workspace>('/api/workspaces/' + id, p))
  if (w) workspaces.value = workspaces.value.map((x) => (x.id === id ? w : x))
  return w
}
export async function deleteWorkspace(id: string) {
  const r = await attempt(() => del('/api/workspaces/' + id), false)
  if (r) {
    if (wsId.value === id) setWs('')
    await load()
  }
  return !!r
}
export async function reorderWorkspaces(ids: string[]) {
  workspaces.value = workspaces.value.map((w) => ({ ...w, sort: ids.indexOf(w.id) }))
  await attempt(() => post('/api/workspaces/reorder', { ids }))
}

// ---------- categories ----------
export async function createCategory(workspace_id: string, name: string) {
  const c = await attempt(() => post<Category>('/api/categories', { workspace_id, name }), false)
  if (c) categories.value = [...categories.value, c]
}
export async function renameCategory(id: string, name: string) {
  categories.value = categories.value.map((c) => (c.id === id ? { ...c, name } : c))
  await attempt(() => patch('/api/categories/' + id, { name }))
}
export async function deleteCategory(id: string, deleteLinks: boolean) {
  const r = await attempt(() => del('/api/categories/' + id + (deleteLinks ? '?delete_links=1' : '')), false)
  if (r) await load()
}
export async function reorderCategories(workspace_id: string, ids: string[]) {
  categories.value = categories.value.map((c) => (ids.includes(c.id) ? { ...c, sort: ids.indexOf(c.id) } : c))
  await attempt(() => post('/api/categories/reorder', { workspace_id, ids }))
}

// ---------- links ----------
export interface LinkInput {
  workspace_id: string
  category_id: string
  title: string
  url: string
  description: string
  icon: string
  pinned: boolean
  health_check: boolean
}
export async function createLink(b: LinkInput & { fetch_icon: boolean }) {
  const l = await attempt(() => post<Link>('/api/links', b), false)
  if (l) links.value = [...links.value, l]
  return l
}
export async function updateLink(id: string, p: Partial<LinkInput>) {
  links.value = links.value.map((l) => (l.id === id ? { ...l, ...p } : l))
  const l = await attempt(() => patch<Link>('/api/links/' + id, p))
  if (l) links.value = links.value.map((x) => (x.id === id ? l : x))
  return l
}
export async function deleteLink(id: string) {
  const old = links.value
  links.value = old.filter((l) => l.id !== id)
  await attempt(() => del('/api/links/' + id))
}
export async function togglePin(l: Link) {
  const pinned = !l.pinned
  const next = Math.max(-1, ...links.value.filter((x) => x.workspace_id === l.workspace_id && x.pinned).map((x) => x.pin_sort ?? 0)) + 1
  links.value = links.value.map((x) => (x.id === l.id ? { ...x, pinned, pin_sort: pinned ? next : x.pin_sort } : x))
  const r = await attempt(() => patch<Link>('/api/links/' + l.id, { pinned }))
  if (r) links.value = links.value.map((x) => (x.id === l.id ? r : x))
}
export function askDeleteLink(l: Link) {
  const s: ModalState = {
    kind: 'confirm',
    title: t('link.deleteTitle'),
    message: t('link.deleteMsg', { name: l.title }),
    confirmLabel: t('common.delete'),
    onConfirm: () => deleteLink(l.id),
  }
  modal.value?.kind === 'settings' ? showOver(s) : (modal.value = s)
}
export function askDeleteCategory(cat: Category) {
  showOver({
    kind: 'confirm',
    title: t('cat.deleteTitle'),
    message: t('cat.deleteMsg', { name: cat.name }),
    confirmLabel: t('common.delete'),
    checkbox: t('cat.deleteLinks'),
    onConfirm: (c) => deleteCategory(cat.id, c),
  })
}
export function askDeleteWorkspace(w: Workspace) {
  showOver({
    kind: 'confirm',
    title: t('ws.deleteTitle'),
    message: t('ws.deleteMsg', { name: w.name }),
    confirmLabel: t('common.delete'),
    onConfirm: () => {
      if (cmWs.value === w.id) cmWs.value = ''
      deleteWorkspace(w.id)
    },
  })
}
export async function refreshIcon(id: string) {
  try {
    const l = await post<Link>(`/api/links/${id}/refresh-icon`)
    links.value = links.value.map((x) => (x.id === id ? l : x))
    return l
  } catch {
    toast(t('err.icon'), 'error')
  }
}

export function trackClick(l: Link) {
  if (role.value !== 'admin') return
  const url = `/api/links/${l.id}/click`
  try {
    if (navigator.sendBeacon && navigator.sendBeacon(url)) return
  } catch {}
  fetch(url, { method: 'POST', keepalive: true, headers: { 'X-Requested-With': 'genkan' } }).catch(() => {})
}
export function openLink(l: Link) {
  trackClick(l)
  if (settings.value.open_in_new_tab) window.open(l.url, '_blank', 'noopener')
  else location.assign(l.url)
}
export function webSearch(q: string) {
  const eng = settings.value.search_engine || DEFAULTS.search_engine
  const enc = encodeURIComponent(q)
  const url = eng.includes('%s') ? eng.replace('%s', enc) : eng + enc
  if (settings.value.open_in_new_tab) window.open(url, '_blank', 'noopener')
  else location.assign(url)
}

/**
 * Drag-drop of a link. `containers` maps container key (pinned | uncat | <catId>) to DOM order, dragged id excluded.
 * Pinned links live in BOTH the pinned row (ordered by pin_sort) and their category (ordered by sort):
 *  - drop on the pinned row: pin it if needed, then rewrite pin_sort only;
 *  - drop on a category: rewrite sort / category_id only (a capsule dragged out of the pinned row is cancelled).
 */
export async function dropLink(id: string, targetKey: string, index: number, containers: Record<string, string[]>, fromPinned: boolean) {
  const ws = curWs.value
  const dragged = links.value.find((l) => l.id === id)
  if (!ws || !dragged) return
  if (fromPinned && targetKey !== 'pinned') return
  const mode = settings.value.sort_mode
  const catIds = new Set(categories.value.filter((c) => c.workspace_id === ws.id).map((c) => c.id))
  const srcKey = fromPinned ? 'pinned' : catIds.has(dragged.category_id) ? dragged.category_id : 'uncat'
  const cont: Record<string, string[]> = { ...containers }
  const tgt = (cont[targetKey] || []).slice()
  tgt.splice(index, 0, id)
  cont[targetKey] = tgt
  const inWs = links.value.filter((l) => l.workspace_id === ws.id)
  // the dragged capsule is hidden from every DOM list by id; its twin in the pinned row keeps its place
  if (targetKey !== 'pinned') cont.pinned = sortLinks(inWs.filter((l) => l.pinned), mode, true).map((l) => l.id)
  if (mode === 'custom' && (srcKey === targetKey || (targetKey === 'pinned' && dragged.pinned))) {
    const cur =
      targetKey === 'pinned'
        ? inWs.filter((l) => l.pinned).sort((x, y) => x.pin_sort - y.pin_sort)
        : inWs.filter((l) => (catIds.has(l.category_id) ? l.category_id : 'uncat') === targetKey).sort((x, y) => x.sort - y.sort)
    if (cur.map((l) => l.id).join() === tgt.join()) return
  }
  // which containers get (re)numbered: everything visible when leaving a computed order, else just what changed
  const keys = mode !== 'custom' ? Object.keys(cont) : [targetKey]
  if (mode === 'custom' && targetKey !== 'pinned' && srcKey !== targetKey && !keys.includes(srcKey)) keys.push(srcKey)
  const pinOf = new Map<string, number>()
  const sortOf = new Map<string, number>()
  for (const k of keys) cont[k]?.forEach((lid, i) => (k === 'pinned' ? pinOf : sortOf).set(lid, i))
  const doPin = targetKey === 'pinned' && !dragged.pinned
  batch(() => {
    if (mode !== 'custom') settings.value = { ...settings.value, sort_mode: 'custom' }
    links.value = links.value.map((l) => {
      if (l.workspace_id !== ws.id) return l
      let n = l
      if (sortOf.has(l.id)) n = { ...n, sort: sortOf.get(l.id)! }
      if (pinOf.has(l.id)) n = { ...n, pin_sort: pinOf.get(l.id)!, pinned: true }
      if (l.id === id && targetKey !== 'pinned') n = { ...n, category_id: targetKey === 'uncat' ? '' : targetKey }
      return n
    })
  })
  if (mode !== 'custom') toast(t('toast.customSort'), 'info')
  try {
    if (mode !== 'custom') await patch('/api/settings', { sort_mode: 'custom' })
    if (doPin) await patch('/api/links/' + id, { pinned: true })
    for (const k of keys) {
      const ids = cont[k] || []
      if (k === 'pinned' && !ids.length) continue
      await post('/api/links/reorder', { workspace_id: ws.id, category_id: k === 'pinned' ? '__pinned' : k === 'uncat' ? '' : k, ids })
    }
  } catch (e) {
    toastError(e)
    load()
  }
}

// ---------- wallpaper gallery ----------
export const gallery = signal<WallpaperFile[] | null>(null)
export const uploading = signal<{ done: number; total: number } | null>(null)
export async function loadGallery() {
  try {
    gallery.value = (await get<WallpaperFile[] | null>('/api/wallpapers')) || []
  } catch {
    gallery.value ??= []
  }
}
/** Uploads sequentially (one request per file); returns the `upload:<file>` values that succeeded. */
export async function uploadWallpapers(files: File[]): Promise<string[]> {
  const out: string[] = []
  uploading.value = { done: 0, total: files.length }
  for (const f of files) {
    const fd = new FormData()
    fd.append('file', f)
    try {
      out.push((await post<{ value: string }>('/api/uploads/wallpaper', fd)).value)
    } catch (e) {
      toastError(e)
    }
    uploading.value = { done: out.length, total: files.length }
  }
  uploading.value = null
  await loadGallery()
  return out
}
export async function deleteWallpaper(value: string) {
  const r = await attempt(() => del('/api/wallpapers/' + encodeURIComponent(value.replace(/^upload:/, ''))), false)
  await loadGallery()
  if (r) await load()
  return !!r
}
// ---------- data ----------
export async function importData(mode: 'replace' | 'merge', data: unknown) {
  const r = await attempt(() => post<{ workspaces: number; links: number }>('/api/import', { mode, data }), false)
  if (r) {
    toast(t('toast.imported', { w: r.workspaces, l: r.links }), 'success')
    await load()
  }
  return r
}
