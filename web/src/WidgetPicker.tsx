// The "add widget" picker (docs/PLUGINS.md 15.5): a searchable grid of cards, grouped as installed, disabled, then not-installed built-ins. Enabled plugins first, then disabled ones
// (picking enables them), then the built-in catalog plugins that are not installed yet (picking installs and enables them).
import { useEffect, useRef, useState } from 'preact/hooks'
import { get, patch, post } from './api'
import { Icon } from './icons'
import { loc, t } from './i18n'
import { closeModal, load, openSettings, plugins, toastError } from './store'
import type { CatalogItem, LString, PluginInfo, PluginWidgetDef } from './types'
import { wIcon } from './WidgetBlocks'

type State = 'enabled' | 'disabled' | 'available'
interface Entry {
  key: string
  pid: string
  wid: string
  state: State
  icon: string
  pname: LString
  wname: LString
  pdesc: LString | undefined
  size: string
  /** installed plugin (enabled or disabled); absent for catalog entries */
  p?: PluginInfo
  d?: PluginWidgetDef
}

/** every language of a localized string (a search for "天气" must find "Weather" while the UI is English) */
const all = (v: LString | null | undefined): string[] => (v == null ? [] : typeof v === 'string' ? [v] : Object.values(v).filter((x) => typeof x === 'string'))

const matches = (e: Entry, q: string) => [e.pid, e.wid, ...all(e.pname), ...all(e.wname), ...all(e.pdesc)].some((s) => s.toLowerCase().includes(q))


export function WidgetPicker({ onPick }: { onPick: (p: PluginInfo, d: PluginWidgetDef) => void }) {
  const [q, setQ] = useState('')
  const [cat, setCat] = useState<CatalogItem[] | null>(null)
  const [catDone, setCatDone] = useState(false)
  const [busy, setBusy] = useState('')
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => {
    input.current?.focus()
    let dead = false
    get<CatalogItem[] | null>('/api/plugins/catalog').then(
      (c) => !dead && (setCat(c || []), setCatDone(true)),
      () => !dead && setCatDone(true),
    )
    return () => {
      dead = true
    }
  }, [])

  const ps = plugins.value
  const entries = (() => {
    const out: Entry[] = []
    for (const p of ps)
      for (const d of p.widgets || [])
        out.push({ key: `${p.id}-${d.id}`, pid: p.id, wid: d.id, state: p.enabled ? 'enabled' : 'disabled', icon: wIcon(p.icon) || 'puzzle', pname: p.name, wname: d.name, pdesc: p.description, size: d.size || 'm', p, d })
    for (const c of cat || [])
      if (!c.installed && !ps.some((p) => p.id === c.id))
        for (const w of c.widgets || [])
          out.push({ key: `${c.id}-${w.id}`, pid: c.id, wid: w.id, state: 'available', icon: wIcon(c.icon) || 'puzzle', pname: c.name, wname: w.name, pdesc: c.description, size: w.size || 'm' })
    return out
  })()

  const needle = q.trim().toLowerCase()
  const shown = needle ? entries.filter((e) => matches(e, needle)) : entries
  // groups: installed (enabled) widgets, then widgets of disabled plugins, then the built-in plugins that are not installed.
  // Cards carry their plugin name, so one group per state reads better than many one-card groups per plugin.
  const groups: { id: string; name: string; entries: Entry[]; state: State }[] = []
  const sorted = (es: Entry[]) => es.sort((a, b) => loc(a.pname).localeCompare(loc(b.pname)) || loc(a.wname).localeCompare(loc(b.wname)))
  const on = sorted(shown.filter((e) => e.state === 'enabled'))
  const off = sorted(shown.filter((e) => e.state === 'disabled'))
  if (on.length) groups.push({ id: 'installed', name: t('widget.group.installed'), entries: on, state: 'enabled' })
  if (off.length) groups.push({ id: 'disabled', name: t('widget.group.disabled'), entries: off, state: 'disabled' })
  const fresh = shown.filter((e) => e.state === 'available')
  if (fresh.length) groups.push({ id: 'new', name: t('widget.group.install'), entries: fresh, state: 'available' })

  const pick = async (e: Entry) => {
    if (busy) return
    if (e.state === 'enabled') return onPick(e.p!, e.d!)
    setBusy(e.key)
    try {
      let info: PluginInfo | undefined
      if (e.state === 'disabled') {
        await patch('/api/plugins/' + e.pid, { enabled: true })
        info = ((await get<PluginInfo[] | null>('/api/plugins')) || []).find((x) => x.id === e.pid)
      } else {
        info = await post<PluginInfo>('/api/plugins', { catalog: e.pid, enable: true })
      }
      load()
      const d = info?.widgets?.find((x) => x.id === e.wid)
      if (!info || !d) throw new Error(t('err.generic'))
      onPick(info, d)
    } catch (er) {
      toastError(er)
      setBusy('')
    }
  }

  const none = catDone && !entries.length
  return (
    <div class="wg-picker" data-testid="widget-picker">
      <div class="wg-pick-search search-mini grow">
        <Icon n="search" size={15} />
        <input
          ref={input}
          type="text"
          data-testid="widget-search"
          aria-label={t('widget.search')}
          placeholder={t('widget.search')}
          value={q}
          autocomplete="off"
          onInput={(e) => setQ((e.target as HTMLInputElement).value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              const first = groups[0]?.entries[0]
              if (first) pick(first)
            }
          }}
        />
      </div>
      <div class="wg-pick-list" data-testid="widget-pick-list">
        {none ? (
          <WidgetNone />
        ) : !shown.length ? (
          catDone || entries.length ? (
            <div class="wg-pick-empty" data-testid="widget-search-empty" role="status">
              <Icon n="search" size={26} />
              <p>{t('widget.searchNone', { q: q.trim() })}</p>
            </div>
          ) : (
            <div class="wg-pick-empty" role="status">
              <span class="spin" />
            </div>
          )
        ) : (
          groups.map((g) => (
            <section key={g.id} class="wg-pick-group" data-testid={'widget-group-' + g.id} data-state={g.state}>
              <h3 class="wg-pick-g">
                {g.name}
              </h3>
              <div class="wg-pick-grid">
                {g.entries.map((e) => (
                  <button
                    key={e.key}
                    type="button"
                    class="wg-pick"
                    data-testid={`widget-pick-${e.pid}-${e.wid}`}
                    data-state={e.state}
                    aria-busy={busy === e.key}
                    disabled={!!busy && busy !== e.key}
                    onClick={() => pick(e)}
                  >
                    <span class="ico ico-lucide" style={{ '--s': '34px', '--h': 200 } as any}>
                      {busy === e.key ? <Icon n="loader-circle" size={19} /> : <Icon n={e.icon} size={19} />}
                    </span>
                    <span class="pl-main">
                      <b>{loc(e.wname)}</b>
                      <small>
                        {loc(e.pname)} · {t('widget.size.' + e.size)}
                      </small>
                      {e.state !== 'enabled' && (
                        <span class="wg-pick-badge" data-testid={`widget-badge-${e.pid}-${e.wid}`}>
                          {busy === e.key ? t(e.state === 'available' ? 'widget.installing' : 'widget.enabling') : t(e.state === 'available' ? 'widget.badge.new' : 'widget.badge.disabled')}
                        </span>
                      )}
                    </span>
                  </button>
                ))}
              </div>
            </section>
          ))
        )}
      </div>
    </div>
  )
}

/** nothing to pick from at all (no installed plugin and no catalog plugin offers a widget) */
function WidgetNone() {
  return (
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
}
