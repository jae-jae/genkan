import { useEffect, useRef, useState } from 'preact/hooks'
import { del, get, patch, post } from './api'
import { ConfigFields, useConfigForm } from './ConfigForm'
import { Icon } from './icons'
import { LUCIDE } from './lucide'
import { Modal } from './parts'
import { lang, t } from './i18n'
import { closeModal, curWs, editMode, isAdmin, load, modal, openSettings, plugins, sortedWs, toast, toastError, widgets, type ModalState } from './store'
import type { Block, ListItem, PluginInfo, PluginWidgetDef, StatBlock, WidgetData, WidgetInstance, WidgetView } from './types'

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
  const cfgKey = inst.plugin + '/' + inst.widget + JSON.stringify(inst.config || {})
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

async function moveWidget(id: string, dir: -1 | 1, shown: WidgetInstance[]) {
  const all = widgets.value.slice().sort((a, b) => a.sort - b.sort)
  const i = shown.findIndex((w) => w.id === id)
  const j = i + dir
  if (i < 0 || j < 0 || j >= shown.length) return
  const a = all.findIndex((w) => w.id === shown[i].id)
  const b = all.findIndex((w) => w.id === shown[j].id)
  ;[all[a], all[b]] = [all[b], all[a]]
  widgets.value = all.map((w, k) => ({ ...w, sort: k }))
  try {
    await post('/api/widgets/reorder', { ids: all.map((w) => w.id) })
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

function Skeleton({ size }: { size: string }) {
  return (
    <div class="wg-skel" data-testid="widget-skeleton" aria-hidden="true">
      <i style={{ width: '45%' }} />
      <i style={{ width: '80%' }} />
      {size !== 's' && <i style={{ width: '60%' }} />}
    </div>
  )
}

function Card({ inst, p, def, edit, shown }: { inst: WidgetInstance; p?: PluginInfo; def?: PluginWidgetDef; edit: boolean; shown: WidgetInstance[] }) {
  const active = !!p?.enabled && !!def
  const admin = isAdmin.value
  const { st, reload } = useWidgetData(inst, def?.refresh || 300, active)
  const size = def?.size || 'm'
  const name = def?.name || inst.widget
  const idx = shown.findIndex((w) => w.id === inst.id)
  const v = st.view
  const err = active ? st.err || v?.error : t('widget.disabled', { name: p?.name || inst.plugin })
  const title = v?.title || name
  const ic = wIcon(v?.icon) || wIcon(p?.icon)
  const href = httpUrl(v?.link)
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
  const body = arr<Block>(v?.body, 50)
  return (
    <section class={'wg glass wg-' + size + (active ? '' : ' off')} data-testid="widget" data-widget-id={inst.id} data-plugin={inst.plugin} data-widget={inst.widget} data-size={size} aria-busy={active && st.loading}>
      <header class="wg-head">
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
        <span class="cap-actions wg-actions">
          <button class="mini" aria-label={t('widget.movePrev')} title={t('widget.movePrev')} disabled={idx <= 0} data-testid={'widget-prev-' + inst.id} onClick={() => moveWidget(inst.id, -1, shown)}>
            <Icon n="arrow-left" size={12} />
          </button>
          <button class="mini" aria-label={t('widget.moveNext')} title={t('widget.moveNext')} disabled={idx >= shown.length - 1} data-testid={'widget-next-' + inst.id} onClick={() => moveWidget(inst.id, 1, shown)}>
            <Icon n="arrow-right" size={12} />
          </button>
          {active && (
            <button class="mini" aria-label={t('widget.edit')} title={t('widget.edit')} data-testid={'widget-edit-' + inst.id} onClick={() => (modal.value = { kind: 'widget', id: inst.id })}>
              <Icon n="pencil" size={12} />
            </button>
          )}
          <button class="mini danger" aria-label={t('widget.delete')} title={t('widget.delete')} data-testid={'widget-delete-' + inst.id} onClick={() => askDelete(inst, name)}>
            <Icon n="x" size={12} />
          </button>
        </span>
      )}
    </section>
  )
}

export function WidgetArea() {
  const ws = curWs.value
  const edit = editMode.value && isAdmin.value
  if (!ws) return null
  const ps = plugins.value
  const mine = widgets.value
    .filter((w) => !w.workspace_id || w.workspace_id === ws.id)
    .sort((a, b) => a.sort - b.sort)
  const shown = edit ? mine : mine.filter((w) => ps.find((p) => p.id === w.plugin)?.enabled)
  if (!shown.length && !edit) return null
  return (
    <section class="widgets" data-testid="widget-area" data-editing={edit ? '1' : undefined} aria-label={t('widget.area')}>
      {shown.map((w) => {
        const p = ps.find((x) => x.id === w.plugin)
        return <Card key={w.id} inst={w} p={p} def={p?.widgets?.find((d) => d.id === w.widget)} edit={edit} shown={shown} />
      })}
      {edit && (
        <button class="wg wg-add glass-lite" data-testid="widget-add" onClick={() => (modal.value = { kind: 'widget' })}>
          <Icon n="plus" size={16} />
          {t('widget.add')}
        </button>
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
                    <b>{d.name}</b>
                    <small>
                      {p.name} · {t('widget.size.' + (d.size || 'm'))}
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
        <b>{d.name}</b>
        <small>{p.name}</small>
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
