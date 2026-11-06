// Shared by the plugin manager chunks (Plugins, PluginSource, PluginMarket): module-level state (the Settings modal is unmounted while a
// permission / editor dialog is stacked on top of it), the install flows, and small components (icons, permission list, code view, preview).
import { signal } from '@preact/signals'
import type { Ref } from 'preact'
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'preact/hooks'
import { ApiError, get, patch, post } from './api'
import { paint } from './highlight'
import './plStrings'
import { Icon } from './icons'
import { LUCIDE } from './lucide'
import { lang, loc, t } from './i18n'
import { load, showOver, toast, toastError } from './store'
import type { ConfigField, LString, PluginInfo, PluginPreview } from './types'
import { hueOf } from './util'

// ---------- state ----------
export const list = signal<PluginInfo[] | null>(null)
export const view = signal<'list' | 'add'>('list')
export const addTab = signal<'catalog' | 'url' | 'source' | 'market'>('catalog')
export const open = signal<{ id: string; panel: 'config' | 'logs' } | null>(null)
export const busy = signal('')
export interface BatchItem {
  info: PluginInfo
  fresh: boolean
}
/** Plugins installed (disabled) by a multi-install, waiting for one combined permission review. */
export const batch = signal<BatchItem[]>([])
export interface Progress {
  total: number
  done: number
  cur: string
  ok: string[]
  failed: { name: string; error: string }[]
  finished: boolean
}
export const progress = signal<Progress | null>(null)

export async function loadList() {
  try {
    list.value = (await get<PluginInfo[] | null>('/api/plugins')) || []
  } catch (e) {
    list.value ??= []
    toastError(e)
  }
}

// ---------- manifest helpers ----------
export const plIcon = (n?: string) => (n && LUCIDE[n] ? n : 'puzzle')
export const hosts = (p: PluginInfo) => p.permissions?.fetch || []
export const linksPerm = (p: PluginInfo) => p.permissions?.links || ''
export const schemaOf = (p: PluginInfo) => p.config_schema || []
export const hasUrlFetch = (p: PluginInfo) => [...schemaOf(p), ...(p.widgets || []).flatMap((w) => w.config || [])].some((f) => f.type === 'url' && f.allowFetch)
export const permKey = (p: PluginInfo) => JSON.stringify([hosts(p).slice().sort(), linksPerm(p), hasUrlFetch(p), !!p.permissions?.ssh])
export const needsConfig = (p: PluginInfo) => schemaOf(p).some((f: ConfigField) => f.required && f.type !== 'boolean')
export const isRisky = (p: PluginInfo) => hosts(p).length > 0 || !!linksPerm(p) || hasUrlFetch(p) || !!p.permissions?.ssh

export async function setEnabled(p: { id: string }, enabled: boolean) {
  busy.value = p.id
  try {
    await patch('/api/plugins/' + p.id, { enabled })
    await loadList()
    load()
    return true
  } catch (e) {
    toastError(e)
    return false
  } finally {
    busy.value = ''
  }
}

/**
 * Install flow (documented in web/TESTIDS.md): the source is POSTed with `enable:false` so that no plugin code can run before
 * the admin has seen what it may access. The response carries the manifest of what was *actually installed*, so we then show a
 * permission dialog (fetch hosts, links access) and PATCH `enabled:true` on confirm. Plugins that request nothing, and updates of
 * an already enabled plugin whose permissions did not change, are enabled right away.
 *
 * `POST /api/plugins/preview` is deliberately not the gate: for URL installs the server downloads again, so the reviewed text could
 * differ from what gets installed. Preview is an optional look beforehand; the binding review is of the installed manifest.
 */
export async function install(body: Record<string, unknown>, key: string): Promise<boolean> {
  busy.value = key
  try {
    const before = list.value || (await get<PluginInfo[] | null>('/api/plugins').catch(() => null)) || []
    const info = await post<PluginInfo>('/api/plugins', { ...body, enable: false })
    const prev = before.find((x) => x.id === info.id)
    await loadList()
    load()
    view.value = 'list'
    if (!isRisky(info) || (prev?.enabled && permKey(prev) === permKey(info))) {
      if (await setEnabled(info, true)) {
        toast(t(prev ? 'plugin.updated' : 'plugin.installed', { name: loc(info.name) }), 'success')
        if (needsConfig(info)) open.value = { id: info.id, panel: 'config' }
      }
    } else showOver({ kind: 'plugin-perm', plugin: info, fresh: !prev })
    return true
  } catch (e) {
    toastError(e)
    return false
  } finally {
    busy.value = ''
  }
}

/**
 * Third-party multi-install: every file is POSTed `{url, enable:false}` one after the other (progress in `progress`), then ONE combined
 * permission review lists everything that was installed. Returns the URLs that installed.
 */
export async function installMany(items: { url: string; name: string }[]): Promise<string[]> {
  if (busy.value || !items.length) return []
  busy.value = 'many'
  const before = list.value || (await get<PluginInfo[] | null>('/api/plugins').catch(() => null)) || []
  const p: Progress = { total: items.length, done: 0, cur: '', ok: [], failed: [], finished: false }
  progress.value = { ...p }
  const got = new Map<string, BatchItem>()
  const okUrls: string[] = []
  for (const it of items) {
    p.cur = it.name
    progress.value = { ...p }
    try {
      const info = await post<PluginInfo>('/api/plugins', { url: it.url, enable: false })
      got.set(info.id, { info, fresh: !before.some((x) => x.id === info.id) })
      p.ok.push(it.name)
      okUrls.push(it.url)
    } catch (e) {
      p.failed.push({ name: it.name, error: e instanceof ApiError ? (e.status === 0 ? t('err.network') : e.message) : t('err.generic') })
    }
    p.done++
    progress.value = { ...p, ok: p.ok.slice(), failed: p.failed.slice() }
  }
  progress.value = { ...p, cur: '', finished: true, ok: p.ok.slice(), failed: p.failed.slice() }
  busy.value = ''
  await loadList()
  load()
  if (got.size) {
    batch.value = [...got.values()]
    showOver({ kind: 'plugin-perm', dlg: 'batch' })
  } else toast(t('mk.allFailed'), 'error')
  return okUrls
}

// ---------- small components ----------
export function PlIcon({ p, size = 36 }: { p: { id: string; icon?: string; name: LString }; size?: number }) {
  return (
    <span class="ico ico-lucide pl-ico" style={{ '--s': size + 'px', '--h': hueOf(p.id) } as any}>
      <Icon n={plIcon(p.icon)} size={Math.round(size * 0.55)} />
    </span>
  )
}

export function Badges({ p }: { p: PluginInfo }) {
  const b: string[] = []
  if (p.wallpapers?.length) b.push('wallpaper')
  if (p.widgets?.length) b.push('widget')
  if (p.search) b.push('search')
  if (p.schedules?.length) b.push('schedule')
  return (
    <span class="pl-badges">
      {b.map((k) => (
        <span key={k} class="pl-badge" data-kind={k} data-testid="plugin-badge">
          {t('cap.' + k)}
        </span>
      ))}
    </span>
  )
}

/** What a plugin may do, one line per permission (`*` in the warning style). */
export function PermList({ p }: { p: PluginInfo }) {
  const hs = hosts(p)
  const lp = linksPerm(p)
  return (
    <ul class="pl-perms" data-testid="plugin-perms">
      {hs.map((h) =>
        h === '*' ? (
          <li key={h} class="pl-perm warn" data-testid="plugin-perm-any">
            <Icon n="triangle-alert" size={16} />
            <span>
              <code class="warn-code">*</code> {t('perm.fetchAny')}
            </span>
          </li>
        ) : (
          <li key={h} class="pl-perm" data-testid="plugin-perm-host">
            <Icon n="globe" size={16} />
            <span>
              {t('perm.fetchHost')} <code>{h}</code>
            </span>
          </li>
        ),
      )}
      {hasUrlFetch(p) && (
        <li class="pl-perm" data-testid="plugin-perm-urlfields">
          <Icon n="network" size={16} />
          <span>{t('perm.fetchUser')}</span>
        </li>
      )}
      {p.permissions?.ssh && <li class="pl-perm warn" data-testid="plugin-perm-ssh"><Icon n="server" size={16} /><span>{t('perm.ssh')}</span></li>}
      {lp && (
        <li class={'pl-perm' + (lp === 'write' ? ' warn' : '')} data-testid="plugin-perm-links" data-level={lp}>
          <Icon n={lp === 'write' ? 'triangle-alert' : 'bookmark'} size={16} />
          <span>{t(lp === 'write' ? 'perm.linksWrite' : 'perm.linksRead')}</span>
        </li>
      )}
      {!isRisky(p) && (
        <li class="pl-perm" data-testid="plugin-perm-none">
          <Icon n="shield" size={16} />
          <span>{t('perm.none')}</span>
        </li>
      )}
    </ul>
  )
}

/** Compact permission chips (hosts, links access) for list rows. */
export function PermChips({ p }: { p: PluginInfo }) {
  const hs = hosts(p)
  const lp = linksPerm(p)
  if (!isRisky(p)) return <span class="pl-meta"><span class="hint">{t('perm.none')}</span></span>
  return (
    <span class="pl-meta" data-testid="plugin-permchips">
      {hs.slice(0, 3).map((h) => (
        <code key={h} class={h === '*' ? 'warn-code' : ''} title={h === '*' ? t('perm.fetchAny') : undefined}>
          {h}
        </code>
      ))}
      {hs.length > 3 && <code>+{hs.length - 3}</code>}
      {p.permissions?.ssh && <code class="warn-code">SSH</code>}
      {hasUrlFetch(p) && <code>{t('perm.urlShort')}</code>}
      {lp && <code class={lp === 'write' ? 'warn-code' : ''}>{t(lp === 'write' ? 'perm.linksWriteShort' : 'perm.linksReadShort')}</code>}
    </span>
  )
}

export function relTime(iso?: string): string {
  const d = iso ? new Date(iso).getTime() : NaN
  if (isNaN(d)) return ''
  const diff = (d - Date.now()) / 1000
  const rtf = new Intl.RelativeTimeFormat(lang.value === 'zh' ? 'zh-CN' : 'en', { numeric: 'auto' })
  const units: [Intl.RelativeTimeFormatUnit, number][] = [['year', 31536000], ['month', 2592000], ['week', 604800], ['day', 86400], ['hour', 3600], ['minute', 60]]
  for (const [u, s] of units) if (Math.abs(diff) >= s) return rtf.format(Math.round(diff / s), u)
  return rtf.format(0, 'second')
}

// ---------- code editor / viewer ----------
const LH = 20 // line height in px: gutter, textarea and the error band must agree

function replaceRange(ta: HTMLTextAreaElement, from: number, to: number, text: string) {
  ta.setSelectionRange(from, to)
  // execCommand keeps the native undo stack; setRangeText is the fallback
  if (!document.execCommand('insertText', false, text)) {
    ta.setRangeText(text, from, to, 'end')
    ta.dispatchEvent(new Event('input', { bubbles: true }))
  }
}

/** Tab / Shift+Tab: insert or remove two spaces (whole lines when the selection spans several). */
function indentKey(ta: HTMLTextAreaElement, shift: boolean) {
  const v = ta.value
  const s = ta.selectionStart
  const e = ta.selectionEnd
  if (s === e && !shift) return replaceRange(ta, s, e, '  ')
  const from = v.lastIndexOf('\n', s - 1) + 1
  const to = e > s && v[e - 1] === '\n' ? e - 1 : e
  const lines = v.slice(from, to).split('\n')
  let first = 0
  let total = 0
  const out = lines.map((ln, i) => {
    let d = 2
    if (shift) {
      const m = /^( {1,2}|\t)/.exec(ln)
      d = m ? m[0].length : 0
      ln = ln.slice(d)
    } else ln = '  ' + ln
    if (i === 0) first = d
    total += d
    return ln
  })
  replaceRange(ta, from, to, out.join('\n'))
  const sign = shift ? -1 : 1
  ta.setSelectionRange(Math.max(from, s + sign * first), Math.max(from, e + sign * total))
}

const HL_SYNC = 60000 // up to this many characters the highlight is repainted before the browser paints (no frame where new text is invisible)
const HL_MAX = 400000 // beyond this the overlay is dropped and the textarea shows its own text

/**
 * Code view: a transparent textarea laid over a highlighted <pre> (same font, padding and tab size; the pre is moved by the textarea's scroll
 * offsets), plus a line-number gutter kept in sync on scroll. No dependencies; the source is only ever put into text nodes (highlight.ts).
 * Editable: Tab = two spaces, Enter keeps the indentation, Ctrl/Cmd+S calls `onSave`. `errorLine` (1-based) marks a line. `fill` = flex to the
 * height the parent gives it.
 */
export function CodeEditor(p: {
  value: string
  onChange?: (v: string) => void
  onSave?: () => void
  readOnly?: boolean
  errorLine?: number
  testid: string
  label: string
  height?: string
  fill?: boolean
  inputRef?: Ref<HTMLTextAreaElement>
}) {
  const gut = useRef<HTMLPreElement>(null)
  const box = useRef<HTMLDivElement>(null)
  const hl = useRef<HTMLPreElement>(null)
  const code = useRef<HTMLElement>(null)
  const n = useMemo(() => p.value.split('\n').length, [p.value])
  const nums = useMemo(() => Array.from({ length: n }, (_, i) => i + 1).join('\n'), [n])
  const plain = p.value.length > HL_MAX
  useLayoutEffect(() => {
    const el = code.current
    if (!el) return
    if (plain) return void (el.textContent = '')
    if (p.value.length <= HL_SYNC) return paint(el, p.value)
    const id = requestAnimationFrame(() => paint(el, p.value)) // big file: at most one repaint per frame while typing
    return () => cancelAnimationFrame(id)
  }, [p.value, plain])
  const sync = (e: Event) => {
    const ta = e.target as HTMLTextAreaElement
    if (gut.current) gut.current.scrollTop = ta.scrollTop
    if (hl.current) hl.current.style.transform = `translate(${-ta.scrollLeft}px,${-ta.scrollTop}px)`
    box.current?.style.setProperty('--st', ta.scrollTop + 'px')
  }
  const onKey = (e: KeyboardEvent) => {
    const ta = e.currentTarget as HTMLTextAreaElement
    if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 's') {
      e.preventDefault()
      e.stopPropagation()
      return p.onSave?.()
    }
    if (p.readOnly || e.isComposing) return
    if (e.key === 'Tab' && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault()
      indentKey(ta, e.shiftKey)
    } else if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey && !e.shiftKey && ta.selectionStart === ta.selectionEnd) {
      const s = ta.selectionStart
      const ind = /^[ \t]*/.exec(ta.value.slice(ta.value.lastIndexOf('\n', s - 1) + 1, s))![0]
      if (ind) {
        e.preventDefault()
        replaceRange(ta, s, s, '\n' + ind)
      }
    }
  }
  const el = p.errorLine && p.errorLine > 0 && p.errorLine <= n ? p.errorLine : 0
  return (
    <div
      class={'ce' + (p.readOnly ? ' ro' : '') + (p.fill ? ' ce-fill' : '') + (plain ? ' ce-plain' : '')}
      ref={box}
      style={{ '--ce-h': p.readOnly ? `min(${p.height || '320px'}, ${n * LH + 22}px)` : p.height || '320px', '--ce-min': p.readOnly ? `min(${p.height || '320px'}, ${n * LH + 22}px)` : '240px', '--ln': el, '--gw': String(n).length + 'ch', '--lh': LH + 'px' } as any}
      data-testid={p.testid + '-wrap'}
    >
      {el > 0 && <div class="ce-band" data-testid={p.testid + '-errline'} />}
      <pre class="ce-gut" aria-hidden="true" ref={gut}>
        {nums}
      </pre>
      <div class="ce-main">
        <pre class="ce-hl" aria-hidden="true" ref={hl}>
          <code ref={code} />
        </pre>
        <textarea
          class="ce-ta"
          ref={p.inputRef}
          data-testid={p.testid}
          aria-label={p.label}
          wrap="off"
          spellcheck={false}
          autocapitalize="off"
          autocomplete="off"
          readOnly={p.readOnly}
          value={p.value}
          onInput={(e) => p.onChange?.((e.target as HTMLTextAreaElement).value)}
          onScroll={sync}
          onKeyDown={onKey}
        />
      </div>
    </div>
  )
}

/** Jumps the caret of an editor to a 1-based line / column and scrolls it into view. */
export function gotoLine(ta: HTMLTextAreaElement | null, line: number, col = 1) {
  if (!ta) return
  const lines = ta.value.split('\n')
  const l = Math.min(Math.max(line, 1), lines.length)
  let pos = 0
  for (let i = 0; i < l - 1; i++) pos += lines[i].length + 1
  pos += Math.min(Math.max(col - 1, 0), lines[l - 1].length)
  ta.focus()
  ta.setSelectionRange(pos, pos)
  ta.scrollTop = Math.max(0, (l - 1) * LH - ta.clientHeight / 2)
}

/** "12:5" / "line 12, col 5" style positions in a validation message. */
export function errPos(msg: string): { line: number; col: number } | null {
  const m = /(?:line\s+)?(\d+)\s*[:,]\s*(?:col(?:umn)?\s*)?(\d+)/i.exec(msg)
  return m ? { line: +m[1], col: +m[2] } : null
}

/** Read-only source of an already installed plugin (the exact text the server holds). */
export function InstalledSource({ id, testid = 'plugin-src-view' }: { id: string; testid?: string }) {
  const [src, setSrc] = useState<string | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    get<{ source: string }>('/api/plugins/' + id + '/source').then(
      (r) => setSrc(r.source),
      (e) => setErr(e?.message || t('err.generic')),
    )
  }, [id])
  if (err) return <p class="form-error" role="alert">{err}</p>
  if (src === null) return <p class="hint">…</p>
  return <CodeEditor value={src} readOnly testid={testid} label={t('src.view')} height="260px" />
}

/** Fetches `POST /api/plugins/preview` once for `body` (the install body: catalog / url / source). Nothing is installed. */
export function usePreviewData(body: Record<string, unknown>) {
  const [data, setData] = useState<PluginPreview | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    let dead = false
    post<PluginPreview>('/api/plugins/preview', body).then(
      (d) => !dead && setData(d),
      (e) => !dead && setErr(e instanceof ApiError && e.status === 0 ? t('err.network') : e?.message || t('err.generic')),
    )
    return () => void (dead = true)
  }, [])
  return { data, err: err || data?.error || '' }
}

/** "Preview source" content for something that is not installed yet: what the plugin is, what it may access, and its source read-only. */
export function PreviewPanel({ body, testid = 'plugin-preview' }: { body: Record<string, unknown>; testid?: string }) {
  const { data, err } = usePreviewData(body)
  const p = data?.plugin
  return (
    <div class="pv" data-testid={testid}>
      {!data && !err && <p class="hint pv-load">…</p>}
      {err && (
        <p class="form-error" role="alert" data-testid={testid + '-error'}>
          {err}
        </p>
      )}
      {p && (
        <>
          <div class="pv-sum">
            <PlIcon p={p} size={32} />
            <div class="pl-main">
              <div class="pl-name">
                <b data-testid={testid + '-name'}>{loc(p.name)}</b>
                <small>v{p.version}</small>
                <Badges p={p} />
              </div>
              {data!.installed && <small class="pv-note">{t('pv.installed', { v: data!.installed_version || '?' })}</small>}
            </div>
          </div>
          <h4 class="pl-h">{t('perm.requests')}</h4>
          <PermList p={p} />
        </>
      )}
      {data && data.source !== undefined && data.source !== '' && (
        <>
          <h4 class="pl-h">{t('pv.source')}</h4>
          <CodeEditor value={data.source} readOnly testid={testid + '-code'} label={t('src.view')} height="260px" />
        </>
      )}
    </div>
  )
}

/** Settings a plugin (and each of its widgets) asks the admin to fill in, read from the manifest. */
function CfgList({ title, fields, testid }: { title?: string; fields: ConfigField[]; testid: string }) {
  return (
    <div class="pv-cfg" data-testid={testid}>
      {title && <b class="pv-cfg-t">{title}</b>}
      <ul>
        {fields.map((f) => (
          <li key={f.key} data-testid={testid + '-field'} data-key={f.key}>
            <code>{f.key}</code>
            <span class="pv-cfg-l">{loc(f.label)}</span>
            <span class="pv-cfg-m">
              {f.type}
              {f.required ? ' · ' + t('cat.required') : ''}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}

/** Name, version, author, description, capabilities, permissions and settings of a plugin (from its manifest). */
export function PluginDetails({ p, testid }: { p: PluginInfo; testid: string }) {
  const groups = [{ title: '', fields: schemaOf(p) }, ...(p.widgets || []).map((w) => ({ title: loc(w.name), fields: w.config || [] }))].filter((g) => g.fields.length)
  return (
    <div class="pv-det" data-testid={testid}>
      <div class="pv-sum">
        <PlIcon p={p} size={40} />
        <div class="pl-main">
          <div class="pl-name">
            <b data-testid={testid + '-name'}>{loc(p.name)}</b>
            <small data-testid={testid + '-version'}>v{p.version}</small>
          </div>
          {p.author && (
            <small class="pl-by" data-testid={testid + '-author'}>
              {t('plugin.by', { author: p.author })}
            </small>
          )}
        </div>
      </div>
      {p.description && (
        <p class="hint" data-testid={testid + '-desc'}>
          {loc(p.description)}
        </p>
      )}
      <h4 class="pl-h">{t('cat.caps')}</h4>
      <div data-testid={testid + '-caps'}>
        <Badges p={p} />
        {!p.wallpapers?.length && !p.widgets?.length && !p.search && !p.schedules?.length && <span class="hint">—</span>}
      </div>
      <h4 class="pl-h">{t('perm.requests')}</h4>
      <PermList p={p} />
      <h4 class="pl-h">{t('cat.cfg')}</h4>
      {groups.length ? groups.map((g, i) => <CfgList key={i} title={g.title} fields={g.fields} testid={testid + '-config'} />) : <p class="hint" data-testid={testid + '-config-none'}>{t('plugin.noConfig')}</p>}
    </div>
  )
}

/** Open/close state for one PreviewPanel; `toggle(body)` snapshots the body so later edits of the input do not refetch. */
export function usePreview() {
  const [pv, setPv] = useState<{ body: Record<string, unknown>; n: number } | null>(null)
  return {
    pv,
    toggle: (body: Record<string, unknown>) => setPv((cur) => (cur ? null : { body, n: Date.now() })),
    close: () => setPv(null),
  }
}
