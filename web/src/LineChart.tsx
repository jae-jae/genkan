// Time-series line chart for the `chart` block (kind "line"): inline SVG, no dependency (docs/PLUGINS.md 13.2).
// Part of the lazy Widgets chunk.
import { useEffect, useId, useRef, useState } from 'preact/hooks'
import { lang, t } from './i18n'

type Pt = [number, number]
export type LineFormat = 'int' | 'compact' | 'percent' | 'decimal'

const COLORS: Record<string, string> = { accent: '#8e9dff', green: '#3ddc97', red: '#ff6b81', blue: '#4da3ff', orange: '#ffb454' }
const H = 3600e3
const D = 24 * H

/** Valid [ts, value] pairs, sorted by time; a repeated timestamp keeps the last value. */
function clean(v: unknown): Pt[] {
  const out: Pt[] = []
  if (Array.isArray(v))
    for (const p of v.slice(0, 500)) {
      const ts = Number(p?.[0])
      const y = Number(p?.[1])
      if (Number.isFinite(ts) && ts > 0 && Number.isFinite(y)) out.push([ts, y])
    }
  out.sort((a, b) => a[0] - b[0])
  return out.filter((p, i) => i === out.length - 1 || out[i + 1][0] !== p[0])
}

/** 3-4 "nice" ticks (1, 2, 2.5, 5 x 10^k) covering [lo, hi]; the first and last tick are the y domain. */
function niceTicks(lo: number, hi: number, whole: boolean): number[] {
  if (lo === hi) {
    const pad = whole ? 1 : Math.abs(lo) * 0.1 || 1
    lo -= pad
    hi += pad
  }
  const k0 = Math.floor(Math.log10(hi - lo)) - 1
  for (let k = k0; ; k++)
    for (const m of [1, 2, 2.5, 5]) {
      const step = m * 10 ** k
      const first = Math.floor(lo / step + 1e-9) * step
      const n = Math.round((Math.ceil(hi / step - 1e-9) * step - first) / step) + 1
      if (n <= 4 || k > k0 + 12) return Array.from({ length: n }, (_, i) => +(first + i * step).toPrecision(12))
    }
}

/** Fritsch-Butland monotone cubic Bezier through the points (no overshoot between samples). */
function curve(p: Pt[]): string {
  const f = (n: number) => +n.toFixed(1)
  const n = p.length
  if (n < 3) return 'M' + p.map((q) => f(q[0]) + ',' + f(q[1])).join('L')
  const h = p.slice(1).map((q, i) => q[0] - p[i][0])
  const d = p.slice(1).map((q, i) => (q[1] - p[i][1]) / h[i])
  const m = [d[0]]
  for (let i = 1; i < n - 1; i++) m.push(d[i - 1] * d[i] <= 0 ? 0 : (3 * (h[i - 1] + h[i])) / ((2 * h[i] + h[i - 1]) / d[i - 1] + (h[i] + 2 * h[i - 1]) / d[i]))
  m.push(d[n - 2])
  let s = `M${f(p[0][0])},${f(p[0][1])}`
  for (let i = 0; i < n - 1; i++) s += `C${f(p[i][0] + h[i] / 3)},${f(p[i][1] + (m[i] * h[i]) / 3)} ${f(p[i + 1][0] - h[i] / 3)},${f(p[i + 1][1] - (m[i + 1] * h[i]) / 3)} ${f(p[i + 1][0])},${f(p[i + 1][1])}`
  return s
}

const loc = () => (lang.value === 'zh' ? 'zh-CN' : 'en')
const dtf = (o: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat(loc(), o)

/** The smallest tick step (of `steps`, in `unit` ms) that gives at most `max` ticks over `span`. */
const fit = (span: number, unit: number, steps: number[], max: number) => steps.find((s) => Math.floor(span / (s * unit)) + 1 <= max) || steps[steps.length - 1]

/** Date ticks at local hour / day / month boundaries, by the span of the data (at most `max` of them). */
function xTicks(t0: number, t1: number, max: number): [number, string][] {
  const span = t1 - t0
  const out: [number, string][] = []
  const d = new Date(t0)
  if (span <= 2 * D) {
    const step = fit(span, H, [1, 2, 3, 6, 12], max)
    d.setMinutes(0, 0, 0)
    const hh = dtf({ hour: '2-digit', minute: '2-digit', hour12: false })
    const dd = dtf({ month: 'short', day: 'numeric' })
    for (; d.getTime() <= t1; d.setHours(d.getHours() + 1)) if (d.getTime() >= t0 && d.getHours() % step === 0) out.push([d.getTime(), d.getHours() === 0 ? dd.format(d) : hh.format(d)])
  } else if (span <= 75 * D) {
    const step = fit(span, D, [1, 2, 3, 7, 14], max)
    d.setHours(0, 0, 0, 0)
    if (d.getTime() < t0) d.setDate(d.getDate() + 1)
    const f = dtf({ month: 'short', day: 'numeric' })
    for (; d.getTime() <= t1; d.setDate(d.getDate() + step)) out.push([d.getTime(), f.format(d)])
  } else {
    const step = fit(span, 30.4 * D, [1, 2, 3, 6, 12, 24, 60], max)
    d.setDate(1)
    d.setHours(0, 0, 0, 0)
    if (d.getTime() < t0) d.setMonth(d.getMonth() + 1)
    while (d.getMonth() % step !== 0) d.setMonth(d.getMonth() + 1)
    const f = dtf(step >= 12 ? { year: 'numeric' } : span > 14 * 30.4 * D ? { month: 'short', year: 'numeric' } : { month: 'short' })
    for (; d.getTime() <= t1; d.setMonth(d.getMonth() + step)) out.push([d.getTime(), f.format(d)])
  }
  return out
}

/** Axis labels are always compact (1.2k); the tooltip shows the value the way `format` asks for. */
function fmtAxis(v: number, format: LineFormat, step: number): string {
  const s = new Intl.NumberFormat(loc(), { notation: 'compact', maximumFractionDigits: step < 1 ? 2 : 1 }).format(v)
  return format === 'percent' ? s + '%' : s
}
function fmtValue(v: number, format: LineFormat): string {
  const o: Intl.NumberFormatOptions =
    format === 'compact' ? { notation: 'compact', maximumFractionDigits: 2 } : format === 'decimal' ? { maximumFractionDigits: 2, minimumFractionDigits: 2 } : format === 'percent' ? { maximumFractionDigits: 1 } : { maximumFractionDigits: 0 }
  const s = new Intl.NumberFormat(loc(), o).format(v)
  return format === 'percent' ? s + '%' : s
}

export function LineChart({ points, format, color }: { points: unknown; format?: string; color?: string }) {
  const pts = clean(points)
  const wrap = useRef<HTMLDivElement>(null)
  const [w, setW] = useState(300)
  const [hover, setHover] = useState(-1)
  const hide = useRef(0)
  const gid = useId().replace(/[^a-zA-Z0-9_-]/g, '')
  useEffect(() => {
    const el = wrap.current
    if (!el) return
    const set = () => {
      const n = Math.max(120, Math.round(el.getBoundingClientRect().width))
      setW((o) => (o === n ? o : (setHover(-1), n)))
    }
    set()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(set)
    ro.observe(el)
    return () => ro.disconnect()
  }, [pts.length ? 1 : 0])
  if (!pts.length) {
    return (
      <div class="wg-chart wg-lc" data-kind="line" data-testid="widget-chart">
        <div class="wg-lc-empty" data-testid="widget-chart-empty">
          {t('chart.noData')}
        </div>
      </div>
    )
  }

  const fmt: LineFormat = format === 'int' || format === 'compact' || format === 'percent' || format === 'decimal' ? format : 'int'
  const col = COLORS[color || ''] || COLORS.accent
  const ys = pts.map((p) => p[1])
  const ticks = niceTicks(Math.min(...ys), Math.max(...ys), ys.every(Number.isInteger))
  const [lo, hi] = [ticks[0], ticks[ticks.length - 1]]
  const labels = ticks.map((v) => fmtAxis(v, fmt, ticks[1] - ticks[0]))
  const ht = Math.min(168, Math.max(116, Math.round(w * 0.42)))
  const padL = Math.max(...labels.map((s) => s.length)) * 6.2 + 8
  const [padR, padT, padB] = [10, 8, 20]
  const pw = w - padL - padR
  const ph = ht - padT - padB
  const t0 = pts[0][0]
  const t1 = pts[pts.length - 1][0]
  const X = (ts: number) => padL + (t1 === t0 ? pw / 2 : ((ts - t0) / (t1 - t0)) * pw)
  const Y = (v: number) => padT + ph - ((v - lo) / (hi - lo || 1)) * ph
  const xy: Pt[] = pts.map((p) => [X(p[0]), Y(p[1])])
  const line = curve(xy)
  const base = padT + ph
  const xt = pts.length > 1 ? xTicks(t0, t1, Math.max(2, Math.floor(pw / 72))) : []

  const move = (e: PointerEvent) => {
    clearTimeout(hide.current)
    const box = (e.currentTarget as SVGElement).getBoundingClientRect()
    const x = e.clientX - box.left
    let best = 0
    for (let i = 1; i < xy.length; i++) if (Math.abs(xy[i][0] - x) < Math.abs(xy[best][0] - x)) best = i
    setHover(best)
  }
  const leave = (e: PointerEvent) => {
    if (e.pointerType === 'mouse') setHover(-1)
    else hide.current = window.setTimeout(() => setHover(-1), 1800) // a touch keeps the readout for a moment
  }
  const hp = hover >= 0 && hover < pts.length ? hover : -1
  const span = t1 - t0
  const when = dtf(span <= 14 * D ? { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false } : { year: 'numeric', month: 'short', day: 'numeric' })
  const tipX = hp >= 0 ? Math.min(w - 62, Math.max(62, xy[hp][0])) : 0

  return (
    <div class="wg-chart wg-lc" data-kind="line" data-testid="widget-chart" ref={wrap}>
      <svg
        width={w}
        height={ht}
        viewBox={`0 0 ${w} ${ht}`}
        role="img"
        aria-label={`${fmtValue(pts[0][1], fmt)} → ${fmtValue(pts[pts.length - 1][1], fmt)}, ${pts.length}`}
        data-testid="widget-line"
        onPointerDown={move}
        onPointerMove={move}
        onPointerLeave={leave}
        onPointerUp={leave}
        onPointerCancel={leave}
      >
        <defs>
          <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color={col} stop-opacity="0.42" />
            <stop offset="100%" stop-color={col} stop-opacity="0" />
          </linearGradient>
        </defs>
        {ticks.map((v, i) => (
          <g key={i}>
            <line class="wg-lc-grid" x1={padL} x2={w - padR} y1={Y(v)} y2={Y(v)} />
            <text class="wg-lc-t" x={padL - 6} y={Y(v)} text-anchor="end" dominant-baseline="middle">
              {labels[i]}
            </text>
          </g>
        ))}
        {xt.map(([ts, s], i) => {
          const x = X(ts)
          const half = s.length * 3.1
          return (
            <text key={i} class="wg-lc-t" x={Math.min(w - half - 2, Math.max(padL - 4 + half, x))} y={ht - 5} text-anchor="middle">
              {s}
            </text>
          )
        })}
        {xy.length > 1 && <path d={`${line}L${xy[xy.length - 1][0].toFixed(1)},${base}L${xy[0][0].toFixed(1)},${base}Z`} fill={`url(#${gid})`} />}
        {xy.length > 1 ? <path class="wg-lc-line" d={line} stroke={col} data-testid="widget-line-path" /> : <circle cx={xy[0][0]} cy={xy[0][1]} r="3.5" fill={col} />}
        {hp >= 0 && (
          <g pointer-events="none">
            <line class="wg-lc-cross" x1={xy[hp][0]} x2={xy[hp][0]} y1={padT} y2={base} />
            <circle cx={xy[hp][0]} cy={xy[hp][1]} r="4.5" fill={col} stroke="#fff" stroke-width="2" />
          </g>
        )}
      </svg>
      {hp >= 0 && (
        <div class="wg-lc-tip" style={{ left: tipX + 'px' }} data-testid="widget-chart-tip">
          <span>{when.format(pts[hp][0])}</span>
          <b>{fmtValue(pts[hp][1], fmt)}</b>
        </div>
      )}
    </div>
  )
}
