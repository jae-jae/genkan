// Renderers for the iframe, html, media blocks and the image slideshow (docs/PLUGINS.md §14).
// Lives in the lazy widgets chunk (imported only by Widgets.tsx). Everything a plugin sends is validated again here.
import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks'
import { Icon } from './icons'
import { t } from './i18n'
import { absHttp, sanitizeToFragment } from './htmlSan'
import type { Block } from './types'

const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n))
const num = (v: unknown): number | undefined => (typeof v === 'number' && Number.isFinite(v) ? v : undefined)
const txt = (v: unknown): string => (typeof v === 'string' ? v : '')
const imgUrl = (u: unknown): string | null => (typeof u === 'string' && /^data:image\//i.test(u.trim()) ? u.trim() : absHttp(u))

// ---------- html ----------
export function HtmlBlock({ b }: { b: Extract<Block, { type: 'html' }> }) {
  const ref = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    ref.current?.replaceChildren(sanitizeToFragment(b.html))
  }, [b.html])
  return <div class="wg-html" ref={ref} data-testid="widget-html" />
}

// ---------- iframe ----------
const SANDBOX = 'allow-scripts allow-same-origin allow-forms allow-popups allow-popups-to-escape-sandbox'

/**
 * The browser loads the page directly. `allow-same-origin` + `allow-scripts` together would let the page escape the
 * sandbox if it shared Genkan's origin (and with it the admin session), so a same-origin src is refused here.
 */
export function IframeBlock({ b }: { b: Extract<Block, { type: 'iframe' }> }) {
  const src = absHttp(b.src)
  if (!src) return null
  if (new URL(src).origin === location.origin) {
    return (
      <div class="wg-err" role="alert" data-testid="widget-iframe-blocked">
        <span>{t('wg.sameOrigin')}</span>
      </div>
    )
  }
  return <Frame b={b} src={src} />
}

function Frame({ b, src }: { b: Extract<Block, { type: 'iframe' }>; src: string }) {
  const zoom = clamp(num(b.zoom) ?? 1, 0.25, 2)
  const c = b.crop
  const crop = c && num(c.width) && num(c.height) && c.width >= 1 && c.height >= 1 ? { x: clamp(num(c.x) ?? 0, 0, 20000), y: clamp(num(c.y) ?? 0, 0, 20000), w: clamp(c.width, 1, 20000), h: clamp(c.height, 1, 20000) } : null
  const ratio = num(b.ratio) && b.ratio! >= 0.2 && b.ratio! <= 5 ? b.ratio! : 0
  const height = clamp(num(b.height) ?? 360, 120, 1600)
  const refresh = num(b.refresh) && b.refresh! >= 5 ? Math.min(b.refresh!, 86400) : 0
  const [tick, setTick] = useState(0)
  useEffect(() => {
    if (!refresh) return
    const id = setInterval(() => setTick((n) => n + 1), refresh * 1000)
    return () => clearInterval(id)
  }, [refresh])

  const wrap = useRef<HTMLDivElement>(null)
  const [cw, setCw] = useState(0)
  useLayoutEffect(() => {
    const el = wrap.current
    if (!el || !crop || typeof ResizeObserver === 'undefined') return
    const upd = () => setCw(el.clientWidth)
    upd()
    const ro = new ResizeObserver(upd)
    ro.observe(el)
    return () => ro.disconnect()
  }, [!!crop])

  let wrapStyle: Record<string, string>
  let frameStyle: Record<string, string>
  if (crop) {
    // show only the rectangle: the frame is as large as the page up to the region's far corner, shifted and scaled;
    // on a narrow card the scale shrinks so the whole width of the region stays visible
    const s = cw > 0 ? Math.min(zoom, cw / crop.w) : zoom
    wrapStyle = { height: Math.round(crop.h * s) + 'px' }
    frameStyle = { width: crop.x + crop.w + 'px', height: crop.y + crop.h + 'px', transform: `translate(${-crop.x * s}px, ${-crop.y * s}px) scale(${s})` }
  } else {
    wrapStyle = ratio ? { aspectRatio: String(ratio) } : { height: height + 'px' }
    frameStyle = zoom === 1 ? { width: '100%', height: '100%' } : { width: 100 / zoom + '%', height: 100 / zoom + '%', transform: `scale(${zoom})` }
  }
  let host = ''
  try {
    host = new URL(src).hostname
  } catch {
    /* unreachable: src was validated */
  }
  return (
    <div class="wg-frame-w" data-testid="widget-iframe-block">
      <div class="wg-frame" ref={wrap} style={wrapStyle}>
        <iframe
          key={tick}
          src={src}
          title={host}
          style={frameStyle}
          sandbox={SANDBOX}
          referrerpolicy="no-referrer"
          loading="lazy"
          allow="fullscreen; autoplay; picture-in-picture"
          data-testid="widget-iframe"
        />
      </div>
      <div class="wg-frame-hint">
        <span>{t('wg.embedHint')}</span>
        <a href={src} target="_blank" rel="noopener noreferrer" data-testid="widget-iframe-open">
          {t('wg.openNew')}
          <Icon n="external-link" size={12} />
        </a>
      </div>
    </div>
  )
}

// ---------- image slideshow ----------
export function Slideshow({ b }: { b: Extract<Block, { type: 'image' }> }) {
  const slides = (Array.isArray(b.images) ? b.images.slice(0, 20) : []).flatMap((s) => {
    const src = imgUrl(s?.src)
    return src ? [{ src, alt: txt(s.alt), href: absHttp(s.href) }] : []
  })
  const n = slides.length
  const interval = clamp(num(b.interval) ?? 6, 3, 3600)
  const [i, setI] = useState(0)
  const cur = n ? i % n : 0
  useEffect(() => {
    if (n < 2) return
    const id = setTimeout(() => setI((x) => (x + 1) % n), interval * 1000)
    return () => clearTimeout(id)
  }, [cur, n, interval])
  if (!n) return null
  const h = num(b.height)
  return (
    <div class="wg-slides" data-fit={b.fit === 'contain' ? 'contain' : 'cover'} data-testid="widget-slideshow" data-index={cur} data-count={n} role="group" aria-roledescription="carousel" aria-label={t('wg.slides')} style={h ? { height: clamp(h, 60, 1000) + 'px' } : undefined}>
      {slides.map((s, k) => {
        const im = <img src={s.src} alt={s.alt} loading={k === 0 ? 'eager' : 'lazy'} referrerpolicy="no-referrer" draggable={false} />
        return (
          <div class="wg-slide" key={k} data-active={k === cur ? '1' : undefined} aria-hidden={k === cur ? undefined : 'true'} data-testid="widget-slide">
            {s.href ? (
              <a href={s.href} target="_blank" rel="noopener noreferrer" tabIndex={k === cur ? 0 : -1}>
                {im}
              </a>
            ) : (
              im
            )}
          </div>
        )
      })}
      {n > 1 && (
        <div class="wg-dots">
          {slides.map((_, k) => (
            <button key={k} type="button" aria-label={t('wg.slide', { n: k + 1 })} aria-current={k === cur ? 'true' : undefined} data-testid="widget-slide-dot" onClick={() => setI(k)} />
          ))}
        </div>
      )}
    </div>
  )
}

// ---------- media ----------
type MediaB = Extract<Block, { type: 'media' }>

export function MediaBlock({ b }: { b: MediaB }) {
  const src = absHttp(b.src)
  if (!src || (b.kind !== 'video' && b.kind !== 'audio')) return null
  return b.kind === 'video' ? <VideoPlayer b={b} src={src} /> : <AudioPlayer b={b} src={src} />
}

function VideoPlayer({ b, src }: { b: MediaB; src: string }) {
  const [bad, setBad] = useState(false)
  const controls = b.controls !== false
  const muted = b.muted === true
  const poster = absHttp(b.poster) || undefined
  return (
    <div class="wg-video" data-testid="widget-video">
      <video
        src={src}
        poster={poster}
        controls={controls}
        muted={muted}
        autoplay={b.autoplay === true && muted} // browsers only autoplay without sound; so do we
        loop={b.loop === true}
        playsInline
        preload="metadata"
        onError={() => setBad(true)}
        onClick={controls ? undefined : (e) => { const v = e.currentTarget as HTMLVideoElement; v.paused ? void v.play().catch(() => {}) : v.pause() }}
      />
      {b.title && <div class="wg-media-t">{txt(b.title)}</div>}
      {bad && <div class="wg-media-err">{t('wg.mediaErr')}</div>}
    </div>
  )
}

const mmss = (s: number) => {
  if (!Number.isFinite(s) || s < 0) return '0:00'
  const m = Math.floor(s / 60)
  return m + ':' + String(Math.floor(s % 60)).padStart(2, '0')
}

const PAUSE = <path d="M8 5v14M16 5v14" stroke-width="3" />
const SPEAKER = 'M11 4.7a.7.7 0 0 0-1.2-.5L6.4 7.6A1.4 1.4 0 0 1 5.4 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.4a1.4 1.4 0 0 1 1 .4l3.4 3.4a.7.7 0 0 0 1.2-.5z'

function AudioPlayer({ b, src }: { b: MediaB; src: string }) {
  const au = useRef<HTMLAudioElement>(null)
  const [playing, setPlaying] = useState(false)
  const [cur, setCur] = useState(0)
  const [dur, setDur] = useState(0)
  const [muted, setMuted] = useState(b.muted === true)
  const [bad, setBad] = useState(false)
  const controls = b.controls !== false
  const live = !Number.isFinite(dur) || dur <= 0
  const title = txt(b.title)
  const toggle = () => {
    const a = au.current
    if (!a) return
    if (a.paused) {
      setBad(false)
      void a.play().catch(() => {})
    } else a.pause()
  }
  return (
    <div class="wg-audio" data-testid="widget-audio" data-state={bad ? 'error' : playing ? 'playing' : 'paused'}>
      <button type="button" class="wg-au-btn" onClick={toggle} aria-label={playing ? t('wg.pause') : t('wg.play')} data-testid="audio-toggle">
        <svg viewBox="0 0 24 24" width="16" height="16" fill={playing ? 'none' : 'currentColor'} stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          {playing ? PAUSE : <polygon points="7 4 20 12 7 20 7 4" />}
        </svg>
      </button>
      <div class="wg-au-main">
        <div class="wg-au-t" title={title || undefined} data-testid="audio-title">
          {title || (bad ? t('wg.mediaErr') : '')}
        </div>
        {controls && (
          <div class="wg-au-row">
            {live ? (
              <span class="wg-au-live">{playing ? t('wg.live') : ''}</span>
            ) : (
              <input
                type="range"
                class="wg-au-seek"
                min={0}
                max={dur}
                step={0.1}
                value={cur}
                aria-label={t('wg.seek')}
                data-testid="audio-seek"
                style={{ '--p': (cur / dur) * 100 + '%' }}
                onInput={(e) => {
                  const a = au.current
                  if (a) a.currentTime = Number((e.currentTarget as HTMLInputElement).value)
                }}
              />
            )}
            <span class="wg-au-time" data-testid="audio-time">
              {live ? mmss(cur) : mmss(cur) + ' / ' + mmss(dur)}
            </span>
          </div>
        )}
      </div>
      {controls && (
        <button type="button" class="wg-au-btn ghost" onClick={() => { const a = au.current; if (a) a.muted = !a.muted }} aria-label={muted ? t('wg.unmute') : t('wg.mute')} aria-pressed={muted} data-testid="audio-mute">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d={SPEAKER} />
            {muted ? <path d="m22 9-6 6m0-6 6 6" /> : <path d="M16 9a5 5 0 0 1 0 6M19.4 18.4a9 9 0 0 0 0-12.8" />}
          </svg>
        </button>
      )}
      <audio
        ref={au}
        src={src}
        preload="metadata"
        loop={b.loop === true}
        muted={b.muted === true}
        autoplay={b.autoplay === true && b.muted === true}
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        onEnded={() => setPlaying(false)}
        onTimeUpdate={(e) => setCur((e.currentTarget as HTMLAudioElement).currentTime)}
        onDurationChange={(e) => setDur((e.currentTarget as HTMLAudioElement).duration)}
        onLoadedMetadata={(e) => setDur((e.currentTarget as HTMLAudioElement).duration)}
        onVolumeChange={(e) => setMuted((e.currentTarget as HTMLAudioElement).muted)}
        onError={() => { setBad(true); setPlaying(false) }}
      />
    </div>
  )
}
