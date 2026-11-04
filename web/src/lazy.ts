import { useEffect, useState } from 'preact/hooks'

/** Dynamic chunks: the plugin UI is only downloaded when it is first needed (keeps the first-screen JS small). */
export const loadPlugins = () => import('./Plugins')
export const loadWidgets = () => import('./Widgets')
export const loadPluginSource = () => import('./PluginSource')
export const loadPluginMarket = () => import('./PluginMarket')
export const loadPluginData = () => import('./PluginData')

/** Resolves a dynamic import once `on` is true; null until it has loaded (or if it failed). */
export function useChunk<T>(load: () => Promise<T>, on = true): T | null {
  const [m, setM] = useState<T | null>(null)
  useEffect(() => {
    if (on && !m) load().then(setM, () => {})
  }, [on])
  return m
}
