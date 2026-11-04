import { signal } from '@preact/signals'
import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks'
import { del, get, patch, post } from './api'
import { openCtx } from './ContextMenu'
import { ConfigFields, useConfigForm } from './ConfigForm'
import { Icon } from './icons'
import { LineChart } from './LineChart'
import { LUCIDE } from './lucide'
import { Modal } from './parts'
import { lang, loc, t } from './i18n'
import { track } from './wtrack'
import { closeModal, curWs, editMode, isAdmin, load, modal, openSettings, plugins, sortedWs, toast, toastError, widgets, type CtxItem, type ModalState } from './store'
import type { Block, ListItem, PluginInfo, PluginWidgetDef, StatBlock, WidgetData, WidgetInstance, WidgetView } from './types'
import { HtmlBlock, IframeBlock, MediaBlock, Slideshow } from './WidgetMedia'

// ---------- safe value helpers (the backend sanitizes too; this is defence in depth) ----------
const str = (v: unknown): string => (typeof v === 'string' ? v : typeof v === 'number' && Number.isFinite(v) ? String(v) : '')
const httpUrl = (u: unknown): string | null => (typeof u === 'string' && /^https?:\/\//i.test(u.trim()) ? u.trim() : null)
const imgSrc = (u: unknown): string | null => (typeof u === 'string' && /^(https?:\/\/|data:image\/)/i.test(u.trim()) ? u.trim() : null)
const arr = <T,>(v: unknown, max: number): T[] => (Array.isArray(v) ? (v.slice(0, max) as T[]) : [])
const wIcon = (n?: unknown) => (typeof n === 'string' && LUCIDE[n] ? n : '')
const nums = (v: unknown, max = 60): number[] => arr<unknown>(v, max).map(Number).filter(Number.isFinite)

interface Ctx {
  run: (action: string, params: unknown) => Promise<void>
  admin: boolean
}

// ---------- blocks ----------
function StatView({ b }: { b: StatBlock }) {
  const ic = wIcon(b.icon)
  const tr = b.trend === 'up' ? 'trending-up' : b.trend === 'down' ? 'trending-down' : b.trend === 'flat' ? 'minus' : ''
  return (
    <div class="wg-stat" data-trend={b.trend || undefined} data-testid="widget-stat">
      <div class="wg-stat-l">
        {ic && <Icon n={ic} size={13} />}
        <span>{str(b.label)}</span>
      </div>
      <div class="wg-stat-v">
        <span>{str(b.value)}</span>
        {b.unit && <small>{str(b.unit)}</small>}
        {tr && <Icon n={tr} size={15} />}
      </div>
    </div>
  )
}

function ListRow({ it }: { it: ListItem }) {
  const href = httpUrl(it.url)
  const img = imgSrc(it.image)
  const ic = wIcon(it.icon)
  const inner = (
    <>
      {img ? <img class="wg-li-img" src={img} alt="" loading="lazy" referrerpolicy="no-referrer" draggable={false} /> : ic ? <span class="wg-li-ico"><Icon n={ic} size={15} /></span> : null}
      <span class="wg-li-main">
        <span class="wg-li-t">{str(it.title)}</span>
        {it.subtitle && <span class="wg-li-s">{str(it.subtitle)}</span>}
      </span>
      {it.badge && <span class="wg-badge">{str(it.badge)}</span>}
      {it.right && <span class="wg-li-r">{str(it.right)}</span>}
    </>
  )
  return href ? (
    <a class="wg-li" href={href} target="_blank" rel="noopener noreferrer" data-testid="widget-list-item">
      {inner}
    </a>
  ) : (
    <div class="wg-li" data-testid="widget-list-item">
      {inner}
    </div>
  )
}

function Chart({ b }: { b: Extract<Block, { type: 'chart' }> }) {
  if (b.kind === 'line') return <LineChart points={b.points} format={b.format} color={b.color} />
  const v = nums(b.values)
  if (!v.length) return null
  const labels = arr<unknown>(b.labels, 60).map(str)
  const H = 36
  const lo = Math.min(...v, b.kind === 'bar' ? 0 : Infinity)
  const hi = Math.max(...v, b.kind === 'bar' ? 0 : -Infinity)
  const span = hi - lo || 1
  const y = (x: number) => (hi === lo ? H / 2 : H - 3 - ((x - lo) / span) * (H - 6))
  let svg
  if (b.kind === 'bar') {
    const w = 100 / v.length
    const zero = y(0)
    svg = v.map((x, i) => {
      const yy = y(x)
      return (
        <rect key={i} x={i * w + w * 0.15} width={w * 0.7} y={Math.min(yy, zero)} height={Math.max(Math.abs(zero - yy), 0.8)} rx="1">
          <title>{(labels[i] ? labels[i] + ': ' : '') + x}</title>
        </rect>
      )
    })
  } else {
    const pts = v.map((x, i) => `${v.length === 1 ? 50 : (i / (v.length - 1)) * 100},${y(x)}`)
    svg = (
      <>
        <polygon class="wg-area" points={`0,${H} ${pts.join(' ')} 100,${H}`} />
        <polyline class="wg-line" points={pts.join(' ')} fill="none" vector-effect="non-scaling-stroke" />
      </>
    )
  }
  const every = Math.ceil(v.length / 7)
  return (
    <div class="wg-chart" data-kind={b.kind} data-testid="widget-chart">
      <svg viewBox={`0 0 100 ${H}`} preserveAspectRatio="none" role="img" aria-label={b.kind}>
        {svg}
      </svg>
      {labels.length > 0 &&
        (b.kind === 'bar' ? (
          <div class="wg-chart-l">
            {v.map((_, i) => (
              <span key={i}>{i % every === 0 ? labels[i] : ''}</span>
            ))}
          </div>
        ) : (
          <div class="wg-chart-l spark">
            <span>{labels[0]}</span>
            <span>{labels[Math.min(labels.length, v.length) - 1]}</span>
          </div>
        ))}
    </div>
  )
}

function BlockView({ b, ctx, depth }: { b: Block; ctx: Ctx; depth: number }) {
  if (!b || typeof b !== 'object') return null
  switch (b.type) {
    case 'text':
      return (
        <div class={'wg-text s-' + (b.size || 'm') + (b.muted ? ' muted' : '')} style={b.align ? { textAlign: b.align } : undefined} data-testid="widget-text">
          {str(b.text)}
        </div>
      )
    case 'stat':
      return <StatView b={b} />
    case 'stats':
      return (
        <div class="wg-stats" data-testid="widget-stats">
          {arr<StatBlock>(b.items, 6).map((s, i) => (
            <StatView key={i} b={s} />
          ))}
        </div>
      )
    case 'list':
      return (
        <div class="wg-list" data-testid="widget-list">
          {arr<ListItem>(b.items, 20).map((it, i) => (
            <ListRow key={i} it={it} />
          ))}
        </div>
      )
    case 'kv':
      return (
        <dl class="wg-kv" data-testid="widget-kv">
          {arr<{ k: string; v: string }>(b.items, 20).map((it, i) => (
            <div key={i}>
              <dt>{str(it.k)}</dt>
              <dd>{str(it.v)}</dd>
            </div>
          ))}
        </dl>
      )
    case 'progress': {
      const v = Math.min(1, Math.max(0, Number(b.value) || 0))
      return (
        <div class="wg-prog" data-testid="widget-progress">
          {(b.label || b.text) && (
            <div class="wg-prog-h">
              <span>{str(b.label)}</span>
              <span>{str(b.text)}</span>
            </div>
          )}
          <div class="wg-track" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(v * 100)} aria-label={str(b.label) || undefined}>
            <i style={{ width: v * 100 + '%' }} />
          </div>
        </div>
      )
    }
    case 'image': {
      // slideshow, or a single image with fit/height (WidgetMedia.tsx); the plain single form stays below
      if (Array.isArray(b.images)) return <Slideshow b={b} />
      if (b.fit || b.height) return <Slideshow b={{ ...b, images: [{ src: str(b.src), alt: str(b.alt), href: str(b.href) }] }} />
      const src = imgSrc(b.src)
      if (!src) return null
      const href = httpUrl(b.href)
      const r = Number(b.ratio)
      const im = <img src={src} alt={str(b.alt)} loading="lazy" referrerpolicy="no-referrer" draggable={false} style={r > 0.1 && r < 10 ? { aspectRatio: String(r) } : undefined} />
      return href ? (
        <a class="wg-img" href={href} target="_blank" rel="noopener noreferrer" data-testid="widget-image">
          {im}
        </a>
      ) : (
        <div class="wg-img" data-testid="widget-image">
          {im}
        </div>
      )
    }
    case 'chart':
      return <Chart b={b} />
    case 'iframe':
      return <IframeBlock b={b} />
    case 'html':
      return <HtmlBlock b={b} />
    case 'media':
      return <MediaBlock b={b} />
    case 'row':
      if (depth >= 3) return null
      return (
        <div class="wg-row" data-testid="widget-row">
          {arr<Block>(b.children, 12).map((c, i) => (
            <div class="wg-cell" key={i}>
              <BlockView b={c} ctx={ctx} depth={depth + 1} />
            </div>
          ))}
        </div>
      )
    case 'button':
      return ctx.admin ? <ActionButton b={b} ctx={ctx} /> : null
    case 'divider':
      return <hr class="wg-hr" />
    default:
      return null
  }
}

function ActionButton({ b, ctx }: { b: Extract<Block, { type: 'button' }>; ctx: Ctx }) {
  const [busy, setBusy] = useState(false)
  const ic = wIcon(b.icon)
  return (
    <button
      class="btn sm wg-btn"
      data-testid="widget-button"
      data-action={str(b.action)}
      disabled={busy}
      onClick={async () => {
        setBusy(true)
        await ctx.run(str(b.action), b.params)
        setBusy(false)
      }}
    >
      {ic && <Icon n={ic} size={14} />}
      {str(b.label)}
    </button>
  )
}

// ---------- data polling ----------
interface DataState {
  view?: WidgetView
  err?: string
  loading: boolean
}

function useWidgetData(inst: WidgetInstance, refreshSec: number, active: boolean) {
  const [st, setSt] = useState<DataState>({ loading: true })
  const reload = useRef<(force: boolean) => void>(() => {})
  const lg = lang.value // a language switch refetches (the server renders the text in the request language)
  const cfgKey = inst.plugin + '/' + inst.widget + lg + JSON.stringify(inst.config || {})
  useEffect(() => {
    if (!active) return
    const every = Math.max(30, refreshSec || 300) * 1000
    let dead = false
    let timer = 0
    let last = 0
    let inflight = false
    const run = async (force: boolean) => {
      clearTimeout(timer)
      if (inflight) return
      inflight = true
      try {
        const d = await get<WidgetData>(`/api/widgets/${inst.id}/data${force && isAdmin.value ? '?force=1' : ''}`, { 'Accept-Language': lang.value })
        if (dead) return
        last = Date.now()
        setSt({ view: d.view || {}, loading: false })
      } catch (e: any) {
        if (dead) return
        setSt((s) => ({ view: s.view, err: e?.message || t('err.generic'), loading: false }))
      } finally {
        inflight = false
      }
      if (!dead) timer = window.setTimeout(tick, every)
    }
    // paused while the tab is hidden: the timer just stops, and coming back refetches if the data is stale
    const tick = () => {
      if (!document.hidden) run(false)
    }
    const onVis = () => {
      if (!document.hidden && Date.now() - last >= every - 1000) run(false)
    }
    reload.current = run
    document.addEventListener('visibilitychange', onVis)
    run(false)
    return () => {
      dead = true
      clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [cfgKey, active, refreshSec])
  return { st, reload: (force: boolean) => reload.current(force) }
}

// ---------- layout: masonry grid (docs/PLUGINS.md 11.1) ----------
// `.widgets` is a dense grid of 4px rows. Every card sits in a cell (`.wgi`, the card + the gap below it) whose row span is
// measured with a ResizeObserver (h = 0, content height) or follows the fixed height (h > 0); `w` is the width in columns of 6.
const ROW = 4
const DEF_W: Record<string, number> = { s: 2, m: 4, l: 6 }
const H_MIN = 80
const H_MAX = 1200
const H_STEP = 8
const effW = (w: number | undefined, size: string) => (w && w > 0 ? Math.min(6, Math.round(w)) : DEF_W[size] || 3)
/** column spans at the three grid densities (6 / 4 / 2 columns; one column is always span 1) */
const cellVars = (w6: number) => ({ '--c6': String(w6), '--c4': String(Math.ceil((w6 * 4) / 6)), '--c2': String(Math.ceil((w6 * 2) / 6)) })
/** columns the grid has right now (6 / 4 / 2 / 1) */
const gridCols = (area: HTMLElement) => Math.max(1, getComputedStyle(area).gridTemplateColumns.split(' ').length)
/** the `w` (of 6) to store for a width of `n` columns of a `cols`-column grid */
const toW = (n: number, cols: number) => (n >= cols ? 6 : Math.floor(((n - 1) * 6) / cols) + 1)
const snapH = (h: number) => Math.min(H_MAX, Math.max(H_MIN, Math.round(h / H_STEP) * H_STEP))

/** Keeps `--r` (the cell's row span) in sync with its rendered height. */
function useRows(ref: { current: HTMLElement | null }) {
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const set = () => el.style.setProperty('--r', String(Math.max(1, Math.ceil(el.getBoundingClientRect().height / ROW - 0.01))))
    set()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(set)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
}

const liveMsg = signal('')

// ---------- persistence ----------
async function patchWidget(id: string, body: { w?: number; h?: number; workspace_id?: string }) {
  widgets.value = widgets.value.map((x) => (x.id === id ? { ...x, ...body } : x))
  try {
    await patch('/api/widgets/' + id, body)
  } catch (e) {
    toastError(e)
    load()
  }
}

/** `ids` = the new order of the widgets of the current view; the widgets of other workspaces keep their slots. */
async function reorderWidgets(ids: string[]) {
  const all = widgets.value.slice().sort((a, b) => a.sort - b.sort)
  const by = new Map(all.map((w) => [w.id, w]))
  const set = new Set(ids)
  let k = 0
  const next = all.map((w) => (set.has(w.id) ? by.get(ids[k++])! : w))
  widgets.value = next.map((w, i) => ({ ...w, sort: i }))
  try {
    await post('/api/widgets/reorder', { ids: next.map((w) => w.id) })
  } catch (e) {
    toastError(e)
    load()
  }
}

function askDelete(w: WidgetInstance, name: string) {
  modal.value = {
    kind: 'confirm',
    title: t('widget.deleteTitle'),
    message: t('widget.deleteMsg', { name }),
    confirmLabel: t('common.delete'),
    onConfirm: async () => {
      widgets.value = widgets.value.filter((x) => x.id !== w.id)
      try {
        await del('/api/widgets/' + w.id)
      } catch (e) {
        toastError(e)
        load()
      }
    },
  }
}

// ---------- drag to reorder (pointer events + touch long press) ----------
function rectDist(r: DOMRect, x: number, y: number) {
  return Math.hypot(Math.max(r.left - x, 0, x - r.right), Math.max(r.top - y, 0, y - r.bottom))
}

/**
 * Pure DOM preview while dragging: the dragged cell becomes a dashed placeholder and the other cells are re-ordered with CSS `order`
 * (no DOM moves, so focus and pointer capture survive), animated FLIP-style. Nothing touches the store until the drop.
 */
function beginReorder(e: PointerEvent, area: HTMLElement, id: string, order: string[]) {
  const cell = area.querySelector<HTMLElement>(`:scope > [data-wid="${id}"]`)
  const card = cell?.firstElementChild as HTMLElement | null
  if (!cell || !card) return
  let cur = order.slice()
  let ghost: HTMLElement | null = null
  let offX = 0
  let offY = 0
  let last = 0
  const cells = () => Array.from(area.querySelectorAll<HTMLElement>(':scope > .wgi'))
  const setOrder = () => {
    const els = cells()
    const before = new Map(els.map((c) => [c, c.getBoundingClientRect()]))
    for (const c of els) c.style.order = c.dataset.wid ? String(cur.indexOf(c.dataset.wid) + 1) : '0'
    for (const c of els) {
      const a = before.get(c)!
      const b = c.getBoundingClientRect()
      const dx = a.left - b.left
      const dy = a.top - b.top
      if (c === cell || (!dx && !dy)) continue
      c.style.transition = 'none'
      c.style.transform = `translate(${dx}px,${dy}px)`
    }
    void area.offsetWidth
    for (const c of els) {
      if (!c.style.transform) continue
      c.style.transition = 'transform 0.2s cubic-bezier(0.2, 0.8, 0.2, 1)'
      c.style.transform = ''
    }
  }
  track(e, {
    touchDelay: 260,
    threshold: 5,
    onStart: (sx, sy) => {
      const r = card.getBoundingClientRect()
      offX = sx - r.left
      offY = sy - r.top
      ghost = card.cloneNode(true) as HTMLElement
      ghost.removeAttribute('data-testid')
      ghost.querySelectorAll('[data-testid]').forEach((n) => n.removeAttribute('data-testid'))
      ghost.classList.add('dnd-ghost')
      ghost.style.width = r.width + 'px'
      ghost.style.height = r.height + 'px'
      document.body.appendChild(ghost)
      cell.dataset.ph = '1'
      area.dataset.dragging = '1'
      setOrder()
    },
    onMove: (x, y) => {
      if (!ghost) return
      ghost.style.transform = `translate(${x - offX}px,${y - offY}px) scale(1.03)`
      const now = performance.now()
      if (now - last < 170) return // let the previous re-flow settle before judging again
      const els = cells()
      const rc = cell.getBoundingClientRect()
      if (x >= rc.left && x <= rc.right && y >= rc.top && y <= rc.bottom) return // over the placeholder: stay
      let hit: HTMLElement | null = null
      let bd = 1e9
      for (const c of els) {
        if (c === cell) continue
        const r = c.getBoundingClientRect()
        const d = rectDist(r, x, y)
        if (d < bd) {
          bd = d
          hit = c
        }
      }
      if (!hit) return
      const rest = cur.filter((k) => k !== id)
      let at = 0
      if (hit.dataset.wid) {
        const r = hit.getBoundingClientRect()
        // the cell's top-left half inserts before it, the bottom-right half after it
        const before = (x - r.left) / (r.width || 1) + (y - r.top) / (r.height || 1) < 1
        at = rest.indexOf(hit.dataset.wid) + (before ? 0 : 1)
      }
      const next = rest.slice()
      next.splice(at, 0, id)
      if (next.join() === cur.join()) return
      cur = next
      last = now
      setOrder()
    },
    onEnd: (commit) => {
      ghost?.remove()
      delete cell.dataset.ph
      delete area.dataset.dragging
      for (const c of cells()) {
        c.style.order = ''
        c.style.transform = ''
        c.style.transition = ''
      }
      if (commit && cur.join() !== order.join()) reorderWidgets(cur)
    },
  })
}

/** keyboard: arrows on the grip move the card one place */
function moveBy(id: string, d: number, order: string[]) {
  const i = order.indexOf(id)
  const j = i + d
  if (i < 0 || j < 0 || j >= order.length) return
  const next = order.slice()
  next.splice(i, 1)
  next.splice(j, 0, id)
  liveMsg.value = t('widget.moved', { n: j + 1, total: order.length })
  reorderWidgets(next)
  requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-testid="widget-grip-${id}"]`)?.focus())
}

// ---------- resize handle: columns snap horizontally, 8px steps vertically, live preview, PATCH {w,h} on release ----------
function beginResize(e: PointerEvent, cell: HTMLElement, inst: WidgetInstance, size: string) {
  const area = cell.parentElement as HTMLElement
  const card = cell.firstElementChild as HTMLElement
  const cols = gridCols(area)
  const gap = parseFloat(getComputedStyle(area).columnGap) || 0
  const colW = (area.clientWidth + gap) / cols
  const w0 = cell.getBoundingClientRect().width
  const n0 = Math.min(cols, Math.max(1, Math.round((w0 + gap) / colW)))
  const h0 = card.getBoundingClientRect().height
  const h0Stored = inst.h || 0
  let sx = 0
  let sy = 0
  let tip: HTMLElement | null = null
  let res = { w: inst.w || 0, h: h0Stored }
  const apply = (w: number, h: number) => {
    const vars = cellVars(effW(w, size))
    for (const k in vars) cell.style.setProperty(k, (vars as any)[k])
    card.style.height = h ? h + 'px' : ''
    if (h) card.dataset.fixed = '1'
    else delete card.dataset.fixed
  }
  track(e, {
    touchDelay: 0,
    threshold: 3,
    capture: e.currentTarget as HTMLElement,
    onStart: (x, y) => {
      sx = x
      sy = y
      tip = document.createElement('span')
      tip.className = 'wg-size-tip'
      card.appendChild(tip)
      cell.dataset.resizing = '1'
    },
    onMove: (x, y) => {
      const dx = x - sx
      const dy = y - sy
      const n = cols === 1 ? 1 : Math.min(cols, Math.max(1, Math.round((w0 + dx + gap) / colW)))
      const w = n === n0 ? inst.w || 0 : toW(n, cols)
      const h = Math.abs(dy) < 6 ? h0Stored : snapH(h0 + dy)
      res = { w, h }
      apply(w, h)
      if (tip) tip.textContent = `${n}/${cols} × ${h ? h + 'px' : t('widget.auto')}`
    },
    onEnd: (commit) => {
      tip?.remove()
      delete cell.dataset.resizing
      if (commit && (res.w !== (inst.w || 0) || res.h !== h0Stored)) patchWidget(inst.id, res)
      else apply(inst.w || 0, h0Stored)
    },
  })
}

/** keyboard: left/right = one column, up/down = 40px, Home / Delete = auto height */
function resizeKey(e: KeyboardEvent, cell: HTMLElement, inst: WidgetInstance) {
  const k = e.key
  if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'Delete', 'Backspace'].includes(k)) return
  e.preventDefault()
  e.stopPropagation()
  const area = cell.parentElement as HTMLElement
  const card = cell.firstElementChild as HTMLElement
  if (k === 'Home' || k === 'Delete' || k === 'Backspace') return void (inst.h && patchWidget(inst.id, { h: 0 }))
  if (k === 'ArrowUp' || k === 'ArrowDown') {
    const base = inst.h || card.getBoundingClientRect().height
    return void patchWidget(inst.id, { h: snapH(base + (k === 'ArrowDown' ? 40 : -40)) })
  }
  const cols = gridCols(area)
  if (cols === 1) return
  const gap = parseFloat(getComputedStyle(area).columnGap) || 0
  const colW = (area.clientWidth + gap) / cols
  const n0 = Math.min(cols, Math.max(1, Math.round((cell.getBoundingClientRect().width + gap) / colW)))
  const n = Math.min(cols, Math.max(1, n0 + (k === 'ArrowRight' ? 1 : -1)))
  if (n !== n0) patchWidget(inst.id, { w: toW(n, cols) })
}

// ---------- context menu ----------
function widgetMenu(inst: WidgetInstance, o: { size: string; admin: boolean; active: boolean; link: string | null; reload: () => void; name: string }): CtxItem[] {
  const items: CtxItem[] = []
  const sep = () => items.push({ id: 'sep' + items.length, label: '', sep: true, onSelect() {} })
  if (o.active) items.push({ id: 'ctx-widget-refresh', label: t('plugin.refresh'), icon: 'refresh-cw', onSelect: o.reload })
  if (o.link) items.push({ id: 'ctx-widget-open', label: t('ctx.openNew'), icon: 'external-link', onSelect: () => window.open(o.link!, '_blank', 'noopener') })
  if (!o.admin) return items
  if (items.length) sep()
  if (o.active) items.push({ id: 'ctx-widget-config', label: t('widget.edit'), icon: 'pencil', onSelect: () => (modal.value = { kind: 'widget', id: inst.id }) })
  const w = effW(inst.w, o.size)
  const sizes: [string, number, string][] = [
    ['s', 2, 'widget.sz.s'],
    ['m', 3, 'widget.sz.m'],
    ['l', 4, 'widget.sz.l'],
    ['full', 6, 'widget.sz.full'],
  ]
  for (const [k, v, key] of sizes) items.push({ id: 'ctx-widget-size-' + k, label: t(key), icon: 'layout-grid', checked: w === v, onSelect: () => patchWidget(inst.id, { w: v }) })
  items.push({ id: 'ctx-widget-height-auto', label: t('widget.sz.auto'), icon: 'arrow-down', checked: !inst.h, onSelect: () => patchWidget(inst.id, { h: 0 }) })
  sep()
  const ws = curWs.value
  if (ws) items.push({ id: 'ctx-widget-scope-ws', label: t('widget.scope.ws'), icon: 'folder', checked: !!inst.workspace_id, onSelect: () => patchWidget(inst.id, { workspace_id: ws.id }) })
  items.push({ id: 'ctx-widget-scope-all', label: t('widget.scope.all'), icon: 'globe', checked: !inst.workspace_id, onSelect: () => patchWidget(inst.id, { workspace_id: '' }) })
  sep()
  items.push({ id: 'ctx-widget-delete', label: t('common.delete'), icon: 'trash-2', danger: true, onSelect: () => askDelete(inst, o.name) })
  return items
}

const LONG_PRESS = 500

function Skeleton({ size }: { size: string }) {
  return (
    <div class="wg-skel" data-testid="widget-skeleton" aria-hidden="true">
      <i style={{ width: '45%' }} />
      <i style={{ width: '80%' }} />
      {size !== 's' && <i style={{ width: '60%' }} />}
    </div>
  )
}

function Card({ inst, p, def, edit, order }: { inst: WidgetInstance; p?: PluginInfo; def?: PluginWidgetDef; edit: boolean; order: string[] }) {
  const active = !!p?.enabled && !!def
  const admin = isAdmin.value
  const { st, reload } = useWidgetData(inst, def?.refresh || 300, active)
  const cell = useRef<HTMLDivElement>(null)
  const lp = useRef({ timer: 0, x: 0, y: 0 })
  useRows(cell)
  const size = def?.size || 'm'
  const name = loc(def?.name) || inst.widget
  const v = st.view
  const err = active ? st.err || v?.error : t('widget.disabled', { name: loc(p?.name) || inst.plugin })
  const title = v?.title || name
  const ic = wIcon(v?.icon) || wIcon(p?.icon)
  const href = httpUrl(v?.link)
  const fixed = !!inst.h && inst.h > 0
  const ctx: Ctx = {
    admin,
    run: async (action, params) => {
      try {
        const r = await post<{ toast?: string; refresh?: boolean }>(`/api/widgets/${inst.id}/action`, { action, params })
        toast(str(r?.toast) || t('widget.done'), 'success')
        if (r?.refresh) reload(true)
      } catch (e) {
        toastError(e)
      }
    },
  }
  const openMenu = (x: number, y: number) => openCtx(x, y, widgetMenu(inst, { size, admin, active, link: href, reload: () => reload(true), name }), 'ctx-menu-widget', title)
  const onMenu = (e: MouseEvent) => {
    if (String(window.getSelection() || '')) return // keep the native menu for selected text
    e.preventDefault()
    if (edit && (e as PointerEvent).pointerType === 'touch') return // long-press is drag in edit mode
    openMenu(e.clientX, e.clientY)
  }
  const cancelLp = () => clearTimeout(lp.current.timer)
  const press = (e: PointerEvent) => {
    if (edit) {
      if ((e.target as HTMLElement).closest('.wg-actions,.wg-resize')) return
      return beginReorder(e, cell.current!.parentElement as HTMLElement, inst.id, order)
    }
    if (e.pointerType === 'mouse') return
    const q = lp.current
    q.x = e.clientX
    q.y = e.clientY
    cancelLp()
    q.timer = window.setTimeout(() => openMenu(q.x, q.y), LONG_PRESS)
  }
  const body = arr<Block>(v?.body, 50)
  return (
    <div class="wgi" ref={cell} data-wid={inst.id} style={cellVars(effW(inst.w, size))}>
      <section
        class={'wg glass wg-' + size + (active ? '' : ' off')}
        style={fixed ? { height: inst.h + 'px' } : undefined}
        data-fixed={fixed ? '1' : undefined}
        data-testid="widget"
        data-widget-id={inst.id}
        data-plugin={inst.plugin}
        data-widget={inst.widget}
        data-size={size}
        data-w={effW(inst.w, size)}
        data-h={inst.h || 0}
        aria-busy={active && st.loading}
        onContextMenu={onMenu}
        onPointerDown={press}
        onPointerMove={(e) => Math.hypot(e.clientX - lp.current.x, e.clientY - lp.current.y) > 8 && cancelLp()}
        onPointerUp={cancelLp}
        onPointerCancel={cancelLp}
        onPointerLeave={cancelLp}
      >
        <header class="wg-head">
          {edit && (
            <button
              class="wg-grip"
              aria-label={t('widget.drag')}
              title={t('widget.drag')}
              data-testid={'widget-grip-' + inst.id}
              onKeyDown={(e) => {
                const d = e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : 0
                if (d) {
                  e.preventDefault()
                  moveBy(inst.id, d, order)
                }
              }}
            >
              <Icon n="grip-vertical" size={15} />
            </button>
          )}
          {ic && <Icon n={ic} size={15} />}
          {href ? (
            <a class="wg-title" href={href} target="_blank" rel="noopener noreferrer" data-testid="widget-title">
              {title}
            </a>
          ) : (
            <h2 class="wg-title" data-testid="widget-title">
              {title}
            </h2>
          )}
          {active && admin && !edit && (
            <button class="wg-refresh" aria-label={t('plugin.refresh')} title={t('plugin.refresh')} data-testid="widget-refresh" onClick={() => reload(true)}>
              <Icon n="refresh-cw" size={13} />
            </button>
          )}
        </header>
        {active && st.loading && !v && <Skeleton size={size} />}
        {err && (
          <div class="wg-err" role="alert" data-testid="widget-error">
            <Icon n="triangle-alert" size={15} />
            <span>{err}</span>
            {active && (
              <button class="linkbtn" data-testid="widget-retry" onClick={() => reload(true)}>
                {t('common.retry')}
              </button>
            )}
          </div>
        )}
        {active && v && (
          <div class={'wg-body' + (st.err ? ' stale' : '')}>
            {body.map((b, i) => (
              <BlockView key={i} b={b} ctx={ctx} depth={0} />
            ))}
          </div>
        )}
        {edit && (
          <>
            <span class="cap-actions wg-actions">
              {active && (
                <button class="mini" aria-label={t('widget.edit')} title={t('widget.edit')} data-testid={'widget-edit-' + inst.id} onClick={() => (modal.value = { kind: 'widget', id: inst.id })}>
                  <Icon n="pencil" size={12} />
                </button>
              )}
              <button
                class="mini"
                aria-label={t('widget.more')}
                title={t('widget.more')}
                aria-haspopup="menu"
                data-testid={'widget-more-' + inst.id}
                onClick={(e) => {
                  const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
                  openMenu(r.left, r.bottom + 4)
                }}
              >
                <Icon n="ellipsis" size={12} />
              </button>
              <button class="mini danger" aria-label={t('widget.delete')} title={t('widget.delete')} data-testid={'widget-delete-' + inst.id} onClick={() => askDelete(inst, name)}>
                <Icon n="x" size={12} />
              </button>
            </span>
            <button
              class="wg-resize"
              aria-label={t('widget.resize')}
              title={t('widget.resizeTip')}
              data-testid={'widget-resize-' + inst.id}
              onPointerDown={(e) => {
                e.stopPropagation()
                beginResize(e, cell.current!, inst, size)
              }}
              onDblClick={() => inst.h && patchWidget(inst.id, { h: 0 })}
              onKeyDown={(e) => resizeKey(e, cell.current!, inst)}
            >
              <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                <path d="M14 6 6 14M14 10l-4 4" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
              </svg>
            </button>
          </>
        )}
      </section>
    </div>
  )
}

export function WidgetArea() {
  const area = useRef<HTMLElement>(null)
  const ws = curWs.value
  const edit = editMode.value && isAdmin.value
  if (!ws) return null
  const ps = plugins.value
  const mine = widgets.value
    .filter((w) => !w.workspace_id || w.workspace_id === ws.id)
    .sort((a, b) => a.sort - b.sort)
  const shown = edit ? mine : mine.filter((w) => ps.find((p) => p.id === w.plugin)?.enabled)
  if (!shown.length && !edit) return null
  const order = shown.map((w) => w.id)
  return (
    <section class="widgets" ref={area} data-testid="widget-area" data-editing={edit ? '1' : undefined} aria-label={t('widget.area')}>
      {edit && (
        <div class="wgi wgi-add" style={cellVars(2)}>
          <button class="wg wg-add glass-lite" data-testid="widget-add" onClick={() => (modal.value = { kind: 'widget' })}>
            <Icon n="plus" size={16} />
            {t('widget.add')}
          </button>
        </div>
      )}
      {shown.map((w) => {
        const p = ps.find((x) => x.id === w.plugin)
        return <Card key={w.id} inst={w} p={p} def={p?.widgets?.find((d) => d.id === w.widget)} edit={edit} order={order} />
      })}
      {edit && (
        <span class="vh" aria-live="polite">
          {liveMsg.value}
        </span>
      )}
    </section>
  )
}

// ---------- add / edit ----------
export function WidgetModal({ state }: { state: Extract<ModalState, { kind: 'widget' }> }) {
  const inst = state.id ? widgets.value.find((w) => w.id === state.id) : undefined
  const ps = plugins.value.filter((p) => p.enabled && p.widgets?.length)
  const [pick, setPick] = useState<{ p: PluginInfo; d: PluginWidgetDef } | null>(() => {
    if (!inst) return null
    const p = plugins.value.find((x) => x.id === inst.plugin)
    const d = p?.widgets?.find((x) => x.id === inst.widget)
    return p && d ? { p, d } : null
  })
  if (state.id && !pick) return null
  return (
    <Modal title={t(inst ? 'widget.editTitle' : 'widget.addTitle')} onClose={closeModal} testid="widget-modal" focusFirst={false}>
      {!pick ? (
        ps.length ? (
          <div class="wg-picks" data-testid="widget-picker">
            <p class="hint first">{t('widget.pick')}</p>
            {ps.flatMap((p) =>
              (p.widgets || []).map((d) => (
                <button key={p.id + '/' + d.id} type="button" class="wg-pick" data-testid={`widget-pick-${p.id}-${d.id}`} onClick={() => setPick({ p, d })}>
                  <span class="ico ico-lucide" style={{ '--s': '34px', '--h': 200 } as any}>
                    <Icon n={wIcon(p.icon) || 'puzzle'} size={19} />
                  </span>
                  <span class="pl-main">
                    <b>{loc(d.name)}</b>
                    <small>
                      {loc(p.name)} · {t('widget.size.' + (d.size || 'm'))}
                    </small>
                  </span>
                </button>
              )),
            )}
          </div>
        ) : (
          <div class="pl-empty" data-testid="widget-none">
            <Icon n="puzzle" size={30} />
            <p>{t('widget.noPlugins')}</p>
            <button
              class="btn"
              data-testid="widget-open-plugins"
              onClick={() => {
                closeModal()
                openSettings('plugins')
              }}
            >
              {t('plugin.browse')}
            </button>
          </div>
        )
      ) : (
        <WidgetForm key={pick.p.id + pick.d.id} p={pick.p} d={pick.d} inst={inst} onBack={inst ? undefined : () => setPick(null)} />
      )}
    </Modal>
  )
}

function WidgetForm({ p, d, inst, onBack }: { p: PluginInfo; d: PluginWidgetDef; inst?: WidgetInstance; onBack?: () => void }) {
  const schema = d.config || []
  // an instance's secret fields come back empty/stripped: treat a non-empty stored value as "set"
  const isSet = schema.filter((f) => f.type === 'secret' && inst?.config?.[f.key]).map((f) => f.key)
  const form = useConfigForm(schema, inst?.config, isSet)
  const [wsv, setWsv] = useState(inst ? inst.workspace_id : curWs.value?.id || '')
  const [busy, setBusy] = useState(false)
  const submit = async (e: Event) => {
    e.preventDefault()
    if (!form.validate()) return
    setBusy(true)
    try {
      const body = { workspace_id: wsv, config: form.build() }
      if (inst) await patch('/api/widgets/' + inst.id, body)
      else {
        const w = await post<WidgetInstance>('/api/widgets', { plugin: p.id, widget: d.id, ...body })
        if (w?.id) widgets.value = [...widgets.value.filter((x) => x.id !== w.id), w]
      }
      closeModal()
      load()
    } catch (er) {
      toastError(er)
      setBusy(false)
    }
  }
  return (
    <form class="form cfg" onSubmit={submit} noValidate data-testid="widget-form">
      <div class="wg-form-h">
        <b>{loc(d.name)}</b>
        <small>{loc(p.name)}</small>
      </div>
      <ConfigFields schema={schema} form={form} secretsSet={isSet} pre="widget-cfg" />
      <div class="field">
        <label for="wg-ws">{t('widget.where')}</label>
        <select id="wg-ws" data-testid="widget-workspace" value={wsv} onChange={(e) => setWsv((e.target as HTMLSelectElement).value)}>
          <option value="">{t('widget.allWs')}</option>
          {sortedWs.value.map((w) => (
            <option key={w.id} value={w.id}>
              {w.name}
            </option>
          ))}
        </select>
      </div>
      <div class="wg-form-foot">
        {onBack && (
          <button type="button" class="btn left" data-testid="widget-back" onClick={onBack}>
            {t('common.back')}
          </button>
        )}
        <button type="button" class="btn" data-testid="widget-cancel" onClick={closeModal}>
          {t('common.cancel')}
        </button>
        <button type="submit" class="btn primary" data-testid="widget-save" disabled={busy}>
          {t(inst ? 'common.save' : 'widget.addBtn')}
        </button>
      </div>
    </form>
  )
}
