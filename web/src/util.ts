import type { Category, Link, SortMode } from './types'

export function hueOf(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0
  return h % 360
}

export function firstChar(s: string): string {
  const t = s.trim()
  if (!t) return '?'
  return (Array.from(t)[0] || '?').toUpperCase()
}

export function hostOf(url: string): string {
  try {
    return new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(url) ? url : 'http://' + url).hostname.replace(/^www\./, '')
  } catch {
    return ''
  }
}

export function normalizeUrl(u: string): string {
  u = u.trim()
  if (!u) return u
  return /^([a-z][a-z0-9+.-]*:\/\/|mailto:|tel:)/i.test(u) ? u : 'http://' + u
}

export type IconKind = 'letter' | 'emoji' | 'img'
export function parseIcon(icon: string): { kind: IconKind; value: string } {
  if (icon.startsWith('emoji:')) return { kind: 'emoji', value: icon.slice(6) }
  if (icon.startsWith('favicon:') || icon.startsWith('upload:')) return { kind: 'img', value: '/uploads/icons/' + icon.slice(icon.indexOf(':') + 1) }
  if (icon.startsWith('url:')) return { kind: 'img', value: icon.slice(4) }
  return { kind: 'letter', value: '' }
}

export function iconMode(icon: string): 'auto' | 'emoji' | 'upload' | 'url' | 'letter' {
  if (icon.startsWith('favicon:')) return 'auto'
  if (icon.startsWith('emoji:')) return 'emoji'
  if (icon.startsWith('upload:')) return 'upload'
  if (icon.startsWith('url:')) return 'url'
  return 'letter'
}

/** `pinned`: ordering for the top row, where custom order is `pin_sort` instead of `sort`. */
export function sortLinks(arr: Link[], mode: SortMode, pinned = false): Link[] {
  const out = arr.slice()
  const key = (l: Link) => (pinned ? l.pin_sort ?? l.sort : l.sort)
  const bySort = (a: Link, b: Link) => key(a) - key(b)
  if (mode === 'frequency') out.sort((a, b) => b.clicks - a.clicks || bySort(a, b))
  else if (mode === 'recent')
    out.sort((a, b) => {
      const ta = a.last_used_at ? Date.parse(a.last_used_at) : -1
      const tb = b.last_used_at ? Date.parse(b.last_used_at) : -1
      return tb - ta || bySort(a, b)
    })
  else out.sort(bySort)
  return out
}

// ---- fuzzy search ----
function fieldScore(q: string, s: string): number {
  if (!s) return 0
  const i = s.indexOf(q)
  if (i >= 0) {
    let sc = 100 - Math.min(i, 40)
    if (i === 0) sc += 40
    else if (/[\s\-_./:]/.test(s[i - 1])) sc += 20
    if (q.length === s.length) sc += 30
    return sc
  }
  // subsequence
  let qi = 0
  let sc = 0
  let run = 0
  for (let k = 0; k < s.length && qi < q.length; k++) {
    if (s[k] === q[qi]) {
      run++
      sc += 2 + run * 2 + (k === 0 || /[\s\-_./:]/.test(s[k - 1]) ? 4 : 0)
      qi++
    } else run = 0
  }
  return qi === q.length ? Math.min(sc, 60) : 0
}

export interface Hit {
  link: Link
  score: number
}
export function searchLinks(query: string, links: Link[], cats: Map<string, Category>): Hit[] {
  const tokens = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (!tokens.length) return []
  const hits: Hit[] = []
  for (const l of links) {
    const fields: [string, number][] = [
      [l.title.toLowerCase(), 1],
      [(cats.get(l.category_id)?.name || '').toLowerCase(), 0.6],
      [l.description.toLowerCase(), 0.5],
      [l.url.toLowerCase().replace(/^https?:\/\//, ''), 0.55],
    ]
    let total = 0
    for (const tk of tokens) {
      let best = 0
      for (const [f, w] of fields) best = Math.max(best, fieldScore(tk, f) * w)
      if (!best) {
        total = 0
        break
      }
      total += best
    }
    if (total > 0) hits.push({ link: l, score: total + Math.min(l.clicks, 50) * 0.05 })
  }
  hits.sort((a, b) => b.score - a.score)
  return hits
}

export function wallpaperUrl(v: string): string | null {
  return v.startsWith('upload:') ? '/uploads/wallpapers/' + v.slice(7) : null
}
export const PRESETS = ['aurora', 'dusk', 'ocean', 'forest', 'sakura', 'graphite'] as const
/** The wallpaper used when none is set (first run, login screens, new workspaces). Keep in sync with store.DefaultWallpaper. */
export const DEFAULT_WALLPAPER = 'preset:dusk'
export function presetOf(v: string): string {
  const id = v.startsWith('preset:') ? v.slice(7) : DEFAULT_WALLPAPER.slice(7)
  return (PRESETS as readonly string[]).includes(id) ? id : DEFAULT_WALLPAPER.slice(7)
}

export function lsGet(k: string): string | null {
  try {
    return localStorage.getItem(k)
  } catch {
    return null
  }
}
export function lsSet(k: string, v: string) {
  try {
    localStorage.setItem(k, v)
  } catch {}
}

export function safeHref(u: string): string {
  return /^\s*(javascript|data|vbscript):/i.test(u) ? '#' : u
}

export function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text).then(() => true, () => false)
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.cssText = 'position:fixed;opacity:0;top:0;left:0'
  document.body.appendChild(ta)
  ta.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {}
  ta.remove()
  return Promise.resolve(ok)
}
