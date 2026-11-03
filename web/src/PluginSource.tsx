// Source editor for an installed plugin, with a "Test run" panel above the source. Lazy chunk: only loaded when someone clicks "Source".
import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { ApiError, get, post, put } from './api'
import { Icon } from './icons'
import { lang, loc, t } from './i18n'
import { Modal } from './parts'
import { closeModal, sortedWs, toast, widgets } from './store'
import type { PluginInfo, PluginLog, PluginTestResult } from './types'
import { CodeEditor, errPos, gotoLine, list, permKey } from './pluginKit'

type Hook = 'wallpaper' | 'widget' | 'search' | 'schedule' | 'action'

const errMsg = (e: unknown) => (e instanceof ApiError ? (e.status === 0 ? t('err.network') : e.message) : t('err.generic'))

function hooksOf(p: PluginInfo): Hook[] {
  const h: Hook[] = []
  if (p.wallpapers?.length) h.push('wallpaper')
  if (p.widgets?.length) h.push('widget', 'action')
  if (p.search) h.push('search')
  if (p.schedules?.length) h.push('schedule')
  return h
}
const targetsOf = (p: PluginInfo, h: Hook): { id: string; name: string }[] =>
  h === 'wallpaper' ? (p.wallpapers || []).map((w) => ({ id: w.id, name: loc(w.name) })) : h === 'widget' || h === 'action' ? (p.widgets || []).map((w) => ({ id: w.id, name: loc(w.name) })) : h === 'schedule' ? (p.schedules || []).map((s) => ({ id: s.id, name: loc(s.name) || s.id })) : []

const fmtTs = (ts: string | number) => {
  const d = new Date(ts)
  return isNaN(+d) ? String(ts) : d.toLocaleTimeString([], { hour12: false })
}
const logLine = (l: PluginLog | string) => (typeof l === 'string' ? l : `${fmtTs(l.ts)} ${String(l.level || 'info').toUpperCase().padEnd(5)} ${l.msg}`)
const MAX = 20000
function pretty(v: unknown): string {
  let s: string
  try {
    s = JSON.stringify(v, null, 2) ?? 'undefined'
  } catch {
    s = String(v)
  }
  return s.length > MAX ? s.slice(0, MAX) + '\n… ' + t('src.truncated') : s
}

/** True at phone width (the same breakpoint as the stylesheet's mobile rules). */
function useNarrow(): boolean {
  const mq = useMemo(() => matchMedia('(max-width: 640px)'), [])
  const [on, setOn] = useState(mq.matches)
  useEffect(() => {
    const f = () => setOn(mq.matches)
    mq.addEventListener('change', f)
    return () => mq.removeEventListener('change', f)
  }, [mq])
  return on
}

function TestPanel({ p, dirty, save }: { p: PluginInfo; dirty: boolean; save: () => Promise<boolean> }) {
  const hooks = hooksOf(p)
  const [hook, setHook] = useState<Hook>(hooks[0] || 'widget')
  const [target, setTarget] = useState('')
  const [query, setQuery] = useState('')
  const [action, setAction] = useState('')
  const [params, setParams] = useState('')
  const [instance, setInstance] = useState('')
  const [cfg, setCfg] = useState('')
  const [running, setRunning] = useState(false)
  const [res, setRes] = useState<PluginTestResult | null>(null)
  const [localErr, setLocalErr] = useState('')
  const targets = targetsOf(p, hook)
  const tgt = targets.find((x) => x.id === target)?.id || targets[0]?.id || ''
  const usesInstance = hook === 'widget' || hook === 'action'
  const insts = usesInstance ? widgets.value.filter((w) => w.plugin === p.id && w.widget === tgt) : []
  const narrow = useNarrow()
  const wsName = (id: string) => (id ? sortedWs.value.find((w) => w.id === id)?.name || id : t('widget.allWs'))

  const parseObj = (s: string, field: string): Record<string, unknown> | undefined | null => {
    if (!s.trim()) return undefined
    try {
      const v = JSON.parse(s)
      if (v && typeof v === 'object' && !Array.isArray(v)) return v
    } catch {}
    setLocalErr(t('src.badJson', { field }))
    return null
  }
  const run = async () => {
    setLocalErr('')
    const body: Record<string, unknown> = { hook, lang: lang.value }
    if (tgt) body.target = tgt
    if (hook === 'search') body.query = query
    if (hook === 'action') {
      if (!action.trim()) return setLocalErr(t('src.needAction'))
      body.action = action.trim()
      const pr = parseObj(params, t('src.params'))
      if (pr === null) return
      if (pr) body.params = pr
    }
    if (usesInstance) {
      if (instance && insts.some((i) => i.id === instance)) body.instance = instance
      const c = parseObj(cfg, t('src.config'))
      if (c === null) return
      if (c) body.config = c
    }
    setRunning(true)
    setRes(null)
    if (dirty && !(await save())) return setRunning(false)
    try {
      setRes(await post<PluginTestResult>('/api/plugins/' + p.id + '/test', body))
    } catch (e) {
      // hook failures may come back as 4xx/5xx {error}; show them like any other failed run
      setRes({ ok: false, error: errMsg(e), logs: [] })
    }
    setRunning(false)
  }
  if (!hooks.length)
    return (
      <div class="tp" data-testid="plugin-test-panel">
        <div class="tp-head">
          <b>
            <Icon n="play" size={13} />
            {t('src.test')}
          </b>
          <span class="hint">{t('src.noHooks')}</span>
        </div>
      </div>
    )
  const logs = (res?.logs || []).map(logLine)
  const more = [
    usesInstance && (
      <div class="field" key="inst">
        <label for="tp-inst">{t('src.instance')}</label>
        <select id="tp-inst" data-testid="plugin-test-instance" value={instance} onChange={(e) => setInstance((e.target as HTMLSelectElement).value)}>
          <option value="">{t('src.instNone')}</option>
          {insts.map((i, n) => {
            const first = Object.values(i.config || {}).find((v) => typeof v === 'string' && v && v.length < 30)
            return (
              <option key={i.id} value={i.id}>
                #{n + 1} · {wsName(i.workspace_id)}
                {first ? ' · ' + first : ''}
              </option>
            )
          })}
        </select>
      </div>
    ),
    hook === 'action' && (
      <div class="field" key="params">
        <label for="tp-params">{t('src.params')}</label>
        <input id="tp-params" type="text" data-testid="plugin-test-params" autocomplete="off" spellcheck={false} placeholder='{"minutes": 5}' value={params} onInput={(e) => setParams((e.target as HTMLInputElement).value)} />
      </div>
    ),
    usesInstance && (
      <div class="field" key="cfg">
        <label for="tp-cfg">{t('src.config')}</label>
        <input id="tp-cfg" type="text" data-testid="plugin-test-config" autocomplete="off" spellcheck={false} placeholder='{"city": "Tokyo"}' value={cfg} onInput={(e) => setCfg((e.target as HTMLInputElement).value)} />
      </div>
    ),
  ].filter(Boolean)
  return (
    <div class="tp" data-testid="plugin-test-panel">
      <div class="tp-head">
        <b>
          <Icon n="play" size={13} />
          {t('src.test')}
        </b>
        <span class="hint">{dirty ? t('src.saveRunHint') : t('src.runsSaved')}</span>
      </div>
      <div class="tp-grid">
        <div class="field">
          <label for="tp-hook">{t('src.hook')}</label>
          <select id="tp-hook" data-testid="plugin-test-hook" value={hook} onChange={(e) => (setHook((e.target as HTMLSelectElement).value as Hook), setTarget(''), setInstance(''))}>
            {hooks.map((h) => (
              <option key={h} value={h}>
                {t('src.hook.' + h)}
              </option>
            ))}
          </select>
        </div>
        {targets.length > 0 && (
          <div class="field">
            <label for="tp-target">{t('src.target')}</label>
            <select id="tp-target" data-testid="plugin-test-target" value={tgt} onChange={(e) => (setTarget((e.target as HTMLSelectElement).value), setInstance(''))}>
              {targets.map((x) => (
                <option key={x.id} value={x.id}>
                  {x.name === x.id ? x.id : `${x.name} (${x.id})`}
                </option>
              ))}
            </select>
          </div>
        )}
        {hook === 'search' && (
          <div class="field tp-wide">
            <label for="tp-q">{t('src.query')}</label>
            <input id="tp-q" type="text" data-testid="plugin-test-query" autocomplete="off" value={query} onInput={(e) => setQuery((e.target as HTMLInputElement).value)} />
          </div>
        )}
        {hook === 'action' && (
          <div class="field tp-wide">
            <label for="tp-action">{t('src.action')}</label>
            <input id="tp-action" type="text" data-testid="plugin-test-action" autocomplete="off" spellcheck={false} placeholder="refresh" value={action} onInput={(e) => setAction((e.target as HTMLInputElement).value)} />
          </div>
        )}
        {/* phone width: the optional fields fold away so the editor keeps its height */}
        {narrow && more.length ? (
          <details class="tp-more">
            <summary>{t('src.more')}</summary>
            <div class="tp-grid">{more}</div>
          </details>
        ) : (
          more
        )}
        <div class="tp-run">
          <button class="btn primary sm" data-testid="plugin-test-run" disabled={running} aria-busy={running} onClick={run}>
            <Icon n="play" size={13} />
            {running ? t('src.running') : dirty ? t('src.saveRun') : t('src.run')}
          </button>
        </div>
      </div>
      {localErr && (
        <p class="form-error" role="alert" data-testid="plugin-test-localerror">
          {localErr}
        </p>
      )}
      {res && (
        <div class="tr" data-testid="plugin-test-result" data-ok={res.ok ? '1' : '0'}>
          <div class="tr-head">
            <span class={'tr-badge ' + (res.ok ? 'ok' : 'bad')} data-testid="plugin-test-status">
              <Icon n={res.ok ? 'circle-check' : 'circle-x'} size={14} />
              {res.ok ? t('src.ok') : t('src.fail')}
            </span>
            {res.ms !== undefined && (
              <span class="tr-ms" data-testid="plugin-test-ms">
                {res.ms} ms
              </span>
            )}
          </div>
          {res.error && (
            <p class="form-error" role="alert" data-testid="plugin-test-error">
              {res.error}
            </p>
          )}
          <div class="tr-out">
            {res.result !== undefined && res.result !== null && (
              <div>
                <h4 class="pl-h">{t('src.result')}</h4>
                <pre class="pl-logs" data-testid="plugin-test-json" tabIndex={0}>
                  {pretty(res.result)}
                </pre>
              </div>
            )}
            <div>
              <h4 class="pl-h">{t('src.logs')}</h4>
              <pre class="pl-logs" data-testid="plugin-test-logs" tabIndex={0}>
                {logs.length ? logs.join('\n') : t('src.noLogs')}
              </pre>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default function SourceModal({ plugin, onSaved }: { plugin: PluginInfo; onSaved: (i: PluginInfo) => void }) {
  const id = plugin.id
  const [info, setInfo] = useState(() => list.value?.find((x) => x.id === id) || plugin)
  const [orig, setOrig] = useState<{ text: string; lang: 'js' | 'ts' } | null>(null)
  const [text, setText] = useState('')
  const [lg, setLg] = useState<'js' | 'ts'>('js')
  const [loadErr, setLoadErr] = useState('')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [errLine, setErrLine] = useState(0)
  const [note, setNote] = useState('')
  const [confirm, setConfirm] = useState(false)
  const ta = useRef<HTMLTextAreaElement>(null)
  const inflight = useRef(false)
  useEffect(() => {
    get<{ source: string; lang?: string }>('/api/plugins/' + id + '/source').then(
      (r) => {
        const l = r.lang === 'ts' ? 'ts' : 'js'
        setOrig({ text: r.source, lang: l })
        setText(r.source)
        setLg(l)
        requestAnimationFrame(() => {
          ta.current?.setSelectionRange(0, 0) // a fresh value puts the caret at the end, which would scroll the editor to the bottom
          ta.current?.focus()
        })
      },
      (e) => setLoadErr(errMsg(e)),
    )
  }, [id])
  const dirty = !!orig && (text !== orig.text || lg !== orig.lang)

  const save = async (): Promise<boolean> => {
    if (!orig) return false
    if (!dirty) return true
    if (inflight.current) return false
    inflight.current = true
    setSaving(true)
    setErr('')
    setErrLine(0)
    const sent = { text, lang: lg }
    try {
      const r = await put<PluginInfo>('/api/plugins/' + id + '/source', { source: sent.text, lang: sent.lang })
      setOrig(sent)
      setNote(r.enabled && permKey(r) !== permKey(info) ? t('src.permChanged') : '')
      setInfo(r)
      onSaved(r)
      toast(t('src.saved'), 'success')
      return true
    } catch (e) {
      const m = errMsg(e)
      setErr(m)
      const pos = errPos(m)
      if (pos) {
        setErrLine(pos.line)
        gotoLine(ta.current, pos.line, pos.col)
      }
      return false
    } finally {
      inflight.current = false
      setSaving(false)
    }
  }
  // Ctrl/Cmd+S saves from anywhere in the dialog (the editor handles it itself when focused and stops the event)
  const saveRef = useRef(save)
  saveRef.current = save
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 's') {
        e.preventDefault()
        saveRef.current()
      }
    }
    document.addEventListener('keydown', k)
    return () => document.removeEventListener('keydown', k)
  }, [])
  const requestClose = () => (confirm ? setConfirm(false) : dirty ? setConfirm(true) : closeModal())
  const pos = errPos(err)
  const lines = text.split('\n').length

  return (
    <Modal
      title={t('src.title', { name: loc(info.name) })}
      onClose={requestClose}
      wide
      cls="modal-src"
      testid="plugin-source-modal"
      focusFirst={false}
      footer={
        confirm ? (
          <div class="src-confirm" role="alertdialog" aria-label={t('src.discardTitle')} data-testid="plugin-source-confirm">
            <span>{t('src.discardTitle')}</span>
            <button class="btn" data-testid="plugin-source-keep" onClick={() => setConfirm(false)}>
              {t('src.keep')}
            </button>
            <button class="btn danger-solid" data-testid="plugin-source-discard" onClick={closeModal}>
              {t('src.discard')}
            </button>
          </div>
        ) : (
          <>
            <span class="src-state" data-testid="plugin-source-state" data-dirty={dirty ? '1' : '0'}>
              {dirty ? <><i class="src-dot" />{t('src.unsaved')}</> : orig ? t('src.clean') : ''}
            </span>
            <button class="btn" data-testid="plugin-source-close" onClick={requestClose}>
              {t('common.close')}
            </button>
            <button class="btn primary" data-testid="plugin-source-save" disabled={!dirty || saving} aria-busy={saving} onClick={save}>
              {t('common.save')}
            </button>
          </>
        )
      }
    >
      {loadErr && (
        <p class="form-error" role="alert" data-testid="plugin-source-loaderror">
          {loadErr}
        </p>
      )}
      {!orig && !loadErr && <p class="hint">…</p>}
      {orig && (
        <div class="src">
          <div class="src-bar">
            <code class="src-file">
              {id}.{lg}
            </code>
            <span class="hint src-meta">{t('src.lines', { n: lines })}</span>
            <select class="src-lang" aria-label={t('plugin.lang')} data-testid="plugin-source-lang" value={lg} onChange={(e) => setLg((e.target as HTMLSelectElement).value as 'js' | 'ts')}>
              <option value="js">JavaScript</option>
              <option value="ts">TypeScript</option>
            </select>
          </div>
          <TestPanel p={info} dirty={dirty} save={save} />
          {err && (
            <p class="form-error src-err" role="alert" data-testid="plugin-source-error">
              <span>{err}</span>
              {pos && (
                <button type="button" class="linkbtn" data-testid="plugin-source-goto" onClick={() => gotoLine(ta.current, pos.line, pos.col)}>
                  {t('src.gotoLine', { n: pos.line })}
                </button>
              )}
            </p>
          )}
          {note && (
            <p class="warn-line" role="status" data-testid="plugin-source-permnote">
              <Icon n="triangle-alert" size={15} />
              <span>{note}</span>
            </p>
          )}
          <CodeEditor
            value={text}
            onChange={(v) => (setText(v), setErrLine(0))}
            onSave={save}
            errorLine={errLine}
            testid="plugin-source-editor"
            label={t('src.edit')}
            height="320px"
            fill
            inputRef={ta}
          />
          <p class="hint src-keys">{t('src.keys')}</p>
        </div>
      )}
    </Modal>
  )
}
