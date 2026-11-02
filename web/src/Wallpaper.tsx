import { useEffect, useRef, useState } from 'preact/hooks'
import { presetOf, wallpaperUrl } from './util'

interface Layer {
  k: number
  v: string
}

function layerProps(v: string) {
  const u = wallpaperUrl(v)
  return u ? { class: 'wp-layer wp-img', style: { backgroundImage: `url("${u}")` } } : { class: 'wp-layer wp-' + presetOf(v) }
}

export function Wallpaper({ value }: { value: string }) {
  const [layers, setLayers] = useState<Layer[]>([{ k: 0, v: value }])
  const seq = useRef(0)
  useEffect(() => {
    let dead = false
    let t = 0
    const add = () => {
      if (dead) return
      setLayers((ls) => (ls[ls.length - 1].v === value ? ls : [ls[ls.length - 1], { k: ++seq.current, v: value }]))
      t = window.setTimeout(() => setLayers((ls) => ls.slice(-1)), 1000)
    }
    const u = wallpaperUrl(value)
    if (u) {
      const img = new Image()
      img.onload = img.onerror = add
      img.src = u
    } else add()
    return () => {
      dead = true
      clearTimeout(t)
    }
  }, [value])
  return (
    <div class="wp" aria-hidden="true" data-testid="wallpaper" data-wallpaper={value}>
      {layers.map((l, i) => {
        const p = layerProps(l.v)
        return <div key={l.k} {...p} class={p.class + (i > 0 ? ' wp-in' : '')} />
      })}
      <div class="wp-dim" />
    </div>
  )
}
