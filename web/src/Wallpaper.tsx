import { useEffect, useRef, useState } from 'preact/hooks'
import { get } from './api'
import { plugins } from './store'
import type { PluginWallpaperMeta } from './types'
import { DEFAULT_WALLPAPER, presetOf, wallpaperUrl } from './util'

interface Layer {
  k: number
  v: string
}

const PLUGIN_WP = /^plugin:([a-z0-9][a-z0-9-]*)\/([A-Za-z0-9_-]+)$/
/** Resolved plugin wallpapers are passed down as `url:<path>`; only local paths and http(s)/data images are accepted. */
const urlOf = (v: string) => (v.startsWith('url:') && /^(\/uploads\/|https?:\/\/|data:image\/)/i.test(v.slice(4)) ? v.slice(4) : wallpaperUrl(v))

function layerProps(v: string) {
  const u = urlOf(v)
  return u ? { class: 'wp-layer wp-img', style: { backgroundImage: `url("${u.replace(/["\\\n]/g, encodeURIComponent)}")` } } : { class: 'wp-layer wp-' + presetOf(v) }
}

/**
 * `plugin:<id>/<source>` -> the image the plugin currently provides (fetched, refreshed every `refresh` seconds while the tab is visible).
 * Disabled / removed plugin or a failing source -> the default wallpaper (a failed *refresh* keeps the image already shown).
 */
function usePluginWallpaper(value: string): { v: string; meta?: PluginWallpaperMeta } {
  const m = PLUGIN_WP.exec(value)
  const def = m ? plugins.value.find((p) => p.id === m[1] && p.enabled)?.wallpapers?.find((w) => w.id === m[2]) : undefined
  const [got, setGot] = useState<{ key: string; meta: PluginWallpaperMeta } | null>(null)
  const [failed, setFailed] = useState('')
  const shown = useRef(DEFAULT_WALLPAPER)
  const refresh = def?.refresh || 3600
  useEffect(() => {
    if (!m || !def) return
    const every = Math.max(300, refresh) * 1000
    let dead = false
    let timer = 0
    let last = 0
    const run = async () => {
      clearTimeout(timer)
      try {
        const meta = await get<PluginWallpaperMeta>(`/api/plugins/wallpaper/${encodeURIComponent(m[1])}/${encodeURIComponent(m[2])}`)
        if (dead) return
        last = Date.now()
        if (meta?.url) {
          setGot({ key: value, meta })
          setFailed('')
        } else setFailed(value)
      } catch {
        if (dead) return
        setFailed(value)
      }
      if (!dead) timer = window.setTimeout(() => !document.hidden && run(), every)
    }
    const onVis = () => !document.hidden && Date.now() - last >= every - 1000 && run()
    document.addEventListener('visibilitychange', onVis)
    run()
    return () => {
      dead = true
      clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [value, !!def, refresh])
  let r: { v: string; meta?: PluginWallpaperMeta }
  if (!m) r = { v: value }
  else if (!def) r = { v: DEFAULT_WALLPAPER }
  else if (got?.key === value && urlOf('url:' + got.meta.url)) r = { v: 'url:' + got.meta.url, meta: got.meta }
  else if (failed === value) r = { v: DEFAULT_WALLPAPER }
  else r = { v: shown.current } // still loading: keep what is on screen
  useEffect(() => {
    shown.current = r.v
  })
  return r
}

export function Wallpaper({ value }: { value: string }) {
  const eff = usePluginWallpaper(value)
  const cur = eff.v
  const [layers, setLayers] = useState<Layer[]>([{ k: 0, v: cur }])
  const seq = useRef(0)
  useEffect(() => {
    let dead = false
    let t = 0
    const add = () => {
      if (dead) return
      setLayers((ls) => (ls[ls.length - 1].v === cur ? ls : [ls[ls.length - 1], { k: ++seq.current, v: cur }]))
      t = window.setTimeout(() => setLayers((ls) => ls.slice(-1)), 1000)
    }
    const u = urlOf(cur)
    if (u) {
      const img = new Image()
      img.onload = img.onerror = add
      img.src = u
    } else add()
    return () => {
      dead = true
      clearTimeout(t)
    }
  }, [cur])
  const credit = eff.meta && (eff.meta.credit || eff.meta.title)
  const link = eff.meta?.link && /^https?:\/\//i.test(eff.meta.link) ? eff.meta.link : null
  return (
    <>
      <div class="wp" aria-hidden="true" data-testid="wallpaper" data-wallpaper={value}>
        {layers.map((l, i) => {
          const p = layerProps(l.v)
          return <div key={l.k} {...p} class={p.class + (i > 0 ? ' wp-in' : '')} />
        })}
        <div class="wp-dim" />
      </div>
      {credit && (
        <div class="wp-credit" data-testid="wallpaper-credit">
          {link ? (
            <a href={link} target="_blank" rel="noopener noreferrer">
              {credit}
            </a>
          ) : (
            credit
          )}
        </div>
      )}
    </>
  )
}
