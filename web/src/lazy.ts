import { useEffect, useState } from 'preact/hooks'
import { get } from './api'
import type { CatalogItem, PluginInfo } from './types'

/** Dynamic chunks: the plugin UI is only downloaded when it is first needed (keeps the first-screen JS small). */
export const loadPlugins = () => import('./Plugins')
export const loadWidgets = () => import('./Widgets')
export const loadPluginSource = () => import('./PluginSource')
export const loadPluginMarket = () => import('./PluginMarket')
export const loadPluginData = () => import('./PluginData')
export const loadUpdate = () => import('./Update')

const chunks = new WeakMap<() => Promise<any>, any>()

/** Starts a chunk download and caches the resolved module for synchronous reads in `useChunk`. */
export function preload<T>(load: () => Promise<T>): Promise<T> {
  const hit = chunks.get(load)
  if (hit) return Promise.resolve(hit)
  return load().then((m) => {
    chunks.set(load, m)
    return m
  })
}

/** Resolves a dynamic import once `on` is true; synchronous once cached, null until it has loaded (or if it failed). */
export function useChunk<T>(load: () => Promise<T>, on = true): T | null {
  const hit: T | null = on ? (chunks.get(load) ?? null) : null
  const [m, setM] = useState<T | null>(hit)
  useEffect(() => {
    if (on && !hit && !m) preload(load).then(setM, () => {})
  }, [on])
  return hit || m
}

let plFlight: Promise<PluginInfo[]> | null = null
let catFlight: Promise<CatalogItem[]> | null = null

export function fetchPluginList(fresh = false): Promise<PluginInfo[]> {
  if (!fresh && plFlight) return plFlight
  const p = get<PluginInfo[] | null>('/api/plugins')
    .then((r) => r || [])
    .finally(() => {
      if (plFlight === p) plFlight = null
    })
  return (plFlight = p)
}

export function fetchCatalog(fresh = false): Promise<CatalogItem[]> {
  if (!fresh && catFlight) return catFlight
  const p = get<CatalogItem[] | null>('/api/plugins/catalog')
    .then((r) => r || [])
    .finally(() => {
      if (catFlight === p) catFlight = null
    })
  return (catFlight = p)
}

/** Warms the plugin manager chunks and list/catalog requests in parallel; errors are ignored. */
export function warmPlugins() {
  const lp = fetchPluginList().catch(() => null)
  const cp = fetchCatalog().catch(() => null)
  preload(loadPlugins).then((m) => m.seedPlugins(lp, cp), () => {})
  preload(loadPluginMarket).catch(() => {})
}

/** Schedules `warmPlugins` on idle while admin is signed in; clears cached plugin state on sign-out. */
export function watchPlugins(on: boolean): () => void {
  if (!on) {
    chunks.get(loadPlugins)?.clearPlugins()
    return () => {}
  }
  const ric = (window as any).requestIdleCallback as ((cb: () => void, o?: { timeout: number }) => number) | undefined
  if (ric) {
    const id = ric(warmPlugins, { timeout: 1000 })
    return () => (window as any).cancelIdleCallback?.(id)
  }
  const id = window.setTimeout(warmPlugins, 150)
  return () => clearTimeout(id)
}
