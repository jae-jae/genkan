import { signal } from '@preact/signals'
import { useEffect, useState } from 'preact/hooks'
import { del, get, patch } from './api'
import { ConfigFields, useConfigForm } from './ConfigForm'
import { Icon } from './icons'
import { fetchCatalog, loadPluginData, loadPluginMarket, loadPluginSource, useChunk } from './lazy'
import { Modal, Switch } from './parts'
import { lang, loc, t } from './i18n'
import { closeModal, load, modal, openSettings, settingsTab, showOver, toast, toastError, version, type ModalState } from './store'
import type { CatalogItem, PluginInfo, PluginLog } from './types'
import { copyText, safeHref } from './util'
import {
  Badges,
  CodeEditor,
  InstalledSource,
  PermList,
  PlIcon,
  PluginDetails,
  PreviewPanel,
  addTab,
  batch,
  busy,
  clearList,
  hosts,
  install,
  linksPerm,
  list,
  loadList,
  needsConfig,
  open,
  progress,
  schemaOf,
  seedList,
  setEnabled,
  usePreview,
  usePreviewData,
  view,
} from './pluginKit'

type PermState = Extract<ModalState, { kind: 'plugin-perm' }>

const catalog = signal<CatalogItem[] | null>(null)
let catSeq = 0
async function loadCatalog(silent = false) {
  const my = ++catSeq
  try {
    const r = await fetchCatalog()
    if (my === catSeq) catalog.value = r
  } catch {
    if (!silent) catalog.value ??= []
  }
}

export function seedPlugins(lp: Promise<PluginInfo[] | null>, cp: Promise<CatalogItem[] | null>) {
  seedList(lp)
  const my = catSeq
  cp.then((v) => {
    if (v && my === catSeq) catalog.value = v
  })
}

export function clearPlugins() {
  clearList()
  catSeq++
  catalog.value = null
}

// ---------- permission confirmation (stacked over Settings) ----------
/** The one modal kind App.tsx renders for plugins: it dispatches on `dlg` (source editor / combined review / catalog source / Develop with AI). */
export function PermModal({ state }: { state: PermState }) {
  if ('dlg' in state) {
    if (state.dlg === 'source') return <SourceHost plugin={state.plugin} />
    if (state.dlg === 'batch') return <BatchModal />
    if (state.dlg === 'catalog') return <CatalogSourceModal id={state.id} />
    if (state.dlg === 'data') return <DataHost plugin={state.plugin} />
    return <DevAiModal />
  }
  return <SingleModal state={state} />
}

function SourceHost({ plugin }: { plugin: PluginInfo }) {
  const m = useChunk(loadPluginSource)
  return m ? <m.default plugin={plugin} onSaved={() => void (loadList(), load())} /> : null
}

function DataHost({ plugin }: { plugin: PluginInfo }) {
  const m = useChunk(loadPluginData)
  return m ? <m.default plugin={plugin} /> : null
}

function SingleModal({ state }: { state: Extract<PermState, { fresh: boolean }> }) {
  const p = state.plugin
  const [src, setSrc] = useState(false)
  const allow = async () => {
    closeModal()
    if (await setEnabled(p, true)) {
      toast(t('plugin.enabled', { name: loc(p.name) }), 'success')
      if (needsConfig(p)) open.value = { id: p.id, panel: 'config' }
    }
  }
  return (
    <Modal
      title={t('perm.title', { name: loc(p.name) })}
      onClose={closeModal}
      testid="plugin-perm-modal"
      focusFirst={false}
      wide={src}
      footer={
        <>
          <button class="btn" data-testid="plugin-perm-cancel" onClick={closeModal}>
            {t('perm.keep')}
          </button>
          <button class="btn primary" data-testid="plugin-perm-allow" onClick={allow}>
            {t('perm.allow')}
          </button>
        </>
      }
    >
      <div class="pl-perm-head">
        <PlIcon p={p} size={40} />
        <div>
          <b>
            {loc(p.name)} <small>v{p.version}</small>
          </b>
          {p.author && <small class="pl-by">{t('plugin.by', { author: p.author })}</small>}
        </div>
      </div>
      {p.description && <p class="hint">{loc(p.description)}</p>}
      <h3 class="sub first">{t('perm.requests')}</h3>
      <PermList p={p} />
      <p class="hint">{t('perm.note')}</p>
      {!state.fresh && <p class="hint">{t('perm.updateNote')}</p>}
      <button class="btn sm" aria-expanded={src} data-testid="plugin-perm-source-btn" onClick={() => setSrc(!src)}>
        <Icon n="eye" size={14} />
        {src ? t('pv.hide') : t('pv.btn')}
      </button>
      {src && (
        <div class="pv-inline" data-testid="plugin-perm-source">
          <InstalledSource id={p.id} />
        </div>
      )}
    </Modal>
  )
}

/** One combined review for everything a multi-install put on the server (disabled). The admin can leave individual plugins disabled. */
function BatchModal() {
  const items = batch.value
  const [chosen, setChosen] = useState<Record<string, boolean>>(() => Object.fromEntries(items.map((i) => [i.info.id, true])))
  const [srcOpen, setSrcOpen] = useState<Record<string, boolean>>({})
  const n = items.filter((i) => chosen[i.info.id]).length
  const allow = async () => {
    const todo = items.filter((i) => chosen[i.info.id])
    closeModal()
    let ok = 0
    let cfg: PluginInfo | undefined
    for (const it of todo) {
      if (await setEnabled(it.info, true)) {
        ok++
        if (!cfg && needsConfig(it.info)) cfg = it.info
      }
    }
    if (ok) toast(t('mk.enabledN', { n: ok }), 'success')
    if (!progress.value?.failed.length) view.value = 'list'
    if (cfg) open.value = { id: cfg.id, panel: 'config' }
  }
  return (
    <Modal
      title={t('batch.title', { n: items.length })}
      onClose={closeModal}
      wide
      testid="plugin-batch-modal"
      focusFirst={false}
      footer={
        <>
          <button class="btn" data-testid="plugin-batch-cancel" onClick={closeModal}>
            {t('perm.keepAll')}
          </button>
          <button class="btn primary" data-testid="plugin-batch-allow" disabled={!n} onClick={allow}>
            {t('batch.allow', { n })}
          </button>
        </>
      }
    >
      <p class="warn-box" role="note">
        <Icon n="triangle-alert" size={16} />
        <span>{t('mk.warn')}</span>
      </p>
      <p class="hint">{t('batch.hint')}</p>
      <ul class="pl-batch" data-testid="plugin-batch-list">
        {items.map(({ info: p, fresh }) => {
          const so = !!srcOpen[p.id]
          return (
            <li key={p.id} class="pl-bitem" data-testid="plugin-perm-item" data-plugin-id={p.id}>
              <div class="pl-bhead">
                <input
                  type="checkbox"
                  class="mk-chk"
                  aria-label={t('plugin.enable', { name: loc(p.name) })}
                  data-testid="plugin-perm-check"
                  checked={!!chosen[p.id]}
                  onChange={(e) => setChosen({ ...chosen, [p.id]: (e.target as HTMLInputElement).checked })}
                />
                <PlIcon p={p} size={32} />
                <div class="pl-main">
                  <div class="pl-name">
                    <b>{loc(p.name)}</b>
                    <small>v{p.version}</small>
                    {!fresh && <span class="pl-tag">{t('batch.update')}</span>}
                    <Badges p={p} />
                  </div>
                  {p.author && <small class="pl-by">{t('plugin.by', { author: p.author })}</small>}
                </div>
                <button class="btn sm" aria-expanded={so} data-testid="plugin-perm-source-btn" onClick={() => setSrcOpen({ ...srcOpen, [p.id]: !so })}>
                  <Icon n="eye" size={14} />
                  {so ? t('pv.hide') : t('pv.btn')}
                </button>
              </div>
              <PermList p={p} />
              {so && (
                <div class="pv-inline" data-testid="plugin-perm-source">
                  <InstalledSource id={p.id} />
                </div>
              )}
            </li>
          )
        })}
      </ul>
      <p class="hint">{t('perm.note')}</p>
    </Modal>
  )
}

/** Built-in catalog item: what the plugin is (manifest) and its highlighted read-only source, fetched with POST /api/plugins/preview {catalog}. */
function CatalogSourceModal({ id }: { id: string }) {
  const { data, err } = usePreviewData({ catalog: id })
  const c = catalog.value?.find((x) => x.id === id)
  const p = data?.plugin
  const name = loc(p?.name) || loc(c?.name) || id
  const installed = c ? c.installed : !!data?.installed
  return (
    <Modal
      title={t('cat.srcTitle', { name })}
      onClose={closeModal}
      wide
      cls="modal-src"
      testid="plugin-catalog-source-modal"
      focusFirst={false}
      footer={
        <>
          <button class="btn" data-testid="plugin-catalog-source-close" onClick={closeModal}>
            {t('common.close')}
          </button>
          <button
            class={'btn' + (installed ? '' : ' primary')}
            data-testid="plugin-catalog-source-install"
            disabled={installed || !!busy.value}
            onClick={() => (closeModal(), install({ catalog: id }, 'c-' + id))}
          >
            {installed ? t('plugin.installedLbl') : t('plugin.install')}
          </button>
        </>
      }
    >
      <div class="src">
        {!data && !err && <p class="hint">…</p>}
        {err && (
          <p class="form-error" role="alert" data-testid="plugin-catalog-source-error">
            {err}
          </p>
        )}
        {p && <PluginDetails p={p} testid="plugin-catalog-source-info" />}
        {data && data.source !== '' && (
          <>
            <h4 class="pl-h">{t('pv.source')}</h4>
            <CodeEditor value={data.source} readOnly fill testid="plugin-catalog-source-code" label={t('src.view')} height="420px" />
          </>
        )}
      </div>
    </Modal>
  )
}

// ---------- Develop with AI (docs/PLUGINS.md §11.3, §15.7) ----------
// The Genkan Skill carries the whole plugin guide and workflow (skills/genkan/plugin-dev.md),
// so the prompt the user sends is one short sentence plus their requirement.
const DEV_HEAD = {
  zh: '用 Genkan Skill 帮我开发一个 Genkan 插件：',
  en: 'Use the Genkan Skill to develop a Genkan plugin for me: ',
}
const DEV_PLACEHOLDER = {
  zh: '【在这里写下你的需求…】',
  en: '[Describe what you want here…]',
}

/** The prompt the user sends to their AI assistant; it ends with a placeholder for the requirement. */
export function devPrompt(l: 'zh' | 'en'): string {
  return DEV_HEAD[l] + DEV_PLACEHOLDER[l]
}

function DevAiModal() {
  const l = lang.value
  const prompt = devPrompt(l)
  const [ok, setOk] = useState(false)
  const copy = () =>
    copyText(prompt).then((r) => {
      if (!r) return toast(t('err.copy'), 'error')
      toast(t('common.copied'), 'success')
      setOk(true)
      setTimeout(() => setOk(false), 1800)
    })
  const toAi = () => {
    closeModal()
    settingsTab.value = 'ai'
    if (modal.value?.kind !== 'settings') openSettings('ai')
  }
  const ph = DEV_PLACEHOLDER[l]
  const body = DEV_HEAD[l]
  return (
    <Modal title={t('dev.title')} onClose={closeModal} testid="plugin-dev-modal" focusFirst={false} footer={<button class="btn" data-testid="plugin-dev-close" onClick={closeModal}>{t('common.close')}</button>}>
      <p class="hint first">{t('dev.intro')}</p>
      <ol class="dev-steps">
        <li data-testid="dev-step-1">
          <span class="dev-n">1</span>
          <div class="dev-c">
            <b>{t('dev.s1')}</b>
            <p class="hint">{t('dev.s1d')}</p>
            <button class="btn sm" data-testid="dev-open-ai" onClick={toAi}>
              <Icon n="bot" size={14} />
              {t('dev.openAi')}
            </button>
          </div>
        </li>
        <li data-testid="dev-step-2">
          <span class="dev-n">2</span>
          <div class="dev-c">
            <b>{t('dev.s2')}</b>
            <p class="hint">{t('dev.s2d')}</p>
            <div class="code wrap">
              <pre data-testid="dev-prompt" tabIndex={0}>
                {body}
                <mark class="dev-ph" data-testid="dev-placeholder">{ph}</mark>
              </pre>
              <button type="button" class="code-copy" aria-label={t('common.copy')} title={ok ? t('common.copied') : t('common.copy')} data-testid="dev-copy" onClick={copy}>
                <Icon n={ok ? 'check' : 'copy'} size={15} />
              </button>
            </div>
            <button class="btn primary sm dev-copybtn" data-testid="dev-copy-btn" onClick={copy}>
              <Icon n={ok ? 'check' : 'copy'} size={14} />
              {t('dev.copyPrompt')}
            </button>
          </div>
        </li>
      </ol>
    </Modal>
  )
}

// ---------- list ----------
function ConfigPanel({ p }: { p: PluginInfo }) {
  const schema = schemaOf(p)
  const form = useConfigForm(schema, p.config, p.secrets_set || [])
  const [saving, setSaving] = useState(false)
  const save = async (e: Event) => {
    e.preventDefault()
    if (!form.validate()) return
    setSaving(true)
    try {
      await patch('/api/plugins/' + p.id, { config: form.build() })
      await loadList()
      load()
      toast(t('plugin.saved'), 'success')
    } catch (er) {
      toastError(er)
    }
    setSaving(false)
  }
  const hs = hosts(p)
  const lp = linksPerm(p)
  return (
    <div class="pl-panel" data-testid="plugin-config-panel">
      {schema.length > 0 ? (
        <form class="form cfg" onSubmit={save} noValidate>
          <ConfigFields schema={schema} form={form} secretsSet={p.secrets_set || []} pre="plugin-cfg" />
          <div>
            <button class="btn primary" type="submit" data-testid="plugin-config-save" disabled={saving}>
              {t('common.save')}
            </button>
          </div>
        </form>
      ) : (
        <p class="hint first">{t('plugin.noConfig')}</p>
      )}
      <h4 class="pl-h">{t('perm.requests')}</h4>
      <div class="pl-meta" data-testid="plugin-permissions">
        {hs.map((h) => (
          <code key={h} class={h === '*' ? 'warn-code' : ''} title={h === '*' ? t('perm.fetchAny') : undefined}>
            {h}
          </code>
        ))}
        {lp && <code class={lp === 'write' ? 'warn-code' : ''}>{t(lp === 'write' ? 'perm.linksWriteShort' : 'perm.linksReadShort')}</code>}
        {!hs.length && !lp && <span class="hint">{t('perm.none')}</span>}
      </div>
      <p class="pl-foot">
        {p.author && <span>{t('plugin.by', { author: p.author })}</span>}
        {p.homepage && /^https?:\/\//i.test(p.homepage) && (
          <a href={safeHref(p.homepage)} target="_blank" rel="noopener noreferrer">
            {t('plugin.homepage')}
          </a>
        )}
        {p.source_url && <span class="ellipsis" title={p.source_url}>{p.source_url}</span>}
      </p>
    </div>
  )
}

function LogsPanel({ p }: { p: PluginInfo }) {
  const [logs, setLogs] = useState<PluginLog[] | null>(null)
  const fetchLogs = async () => {
    try {
      setLogs(((await get<{ logs: PluginLog[] | null }>('/api/plugins/' + p.id + '/logs')).logs || []).slice())
    } catch (e) {
      setLogs([])
      toastError(e)
    }
  }
  useEffect(() => void fetchLogs(), [p.id])
  const fmt = (ts: string | number) => {
    const d = new Date(ts)
    return isNaN(+d) ? String(ts) : d.toLocaleTimeString([], { hour12: false })
  }
  return (
    <div class="pl-panel" data-testid="plugin-logs-panel">
      <div class="pl-logs-bar">
        <span class="hint">{t('plugin.logsHint')}</span>
        <button class="btn sm" data-testid="plugin-logs-refresh" onClick={fetchLogs}>
          <Icon n="refresh-cw" size={13} />
          {t('plugin.refresh')}
        </button>
      </div>
      <pre class="pl-logs" data-testid="plugin-logs" tabIndex={0}>
        {logs === null ? '…' : logs.length ? logs.map((l) => `${fmt(l.ts)} ${String(l.level || 'info').toUpperCase().padEnd(5)} ${l.msg}`).join('\n') : t('plugin.logsEmpty')}
      </pre>
    </div>
  )
}

function Row({ p }: { p: PluginInfo }) {
  const o = open.value?.id === p.id ? open.value.panel : null
  const toggle = (panel: 'config' | 'logs') => (open.value = o === panel ? null : { id: p.id, panel })
  const askDelete = () =>
    showOver({
      kind: 'confirm',
      title: t('plugin.deleteTitle'),
      message: t('plugin.deleteMsg', { name: loc(p.name) }),
      confirmLabel: t('common.delete'),
      onConfirm: async () => {
        try {
          await del('/api/plugins/' + p.id)
          if (open.value?.id === p.id) open.value = null
          await loadList()
          load()
        } catch (e) {
          toastError(e)
        }
      },
    })
  return (
    <li class="pl-row" data-testid="plugin-row" data-plugin-id={p.id} data-enabled={p.enabled ? '1' : '0'}>
      <div class="pl-head">
        <PlIcon p={p} />
        <div class="pl-main">
          <div class="pl-name">
            <b data-testid="plugin-name">{loc(p.name)}</b>
            <small>v{p.version}</small>
            <Badges p={p} />
          </div>
          {p.description && <div class="pl-desc">{loc(p.description)}</div>}
          {p.last_error && (
            <div class="pl-error" data-testid="plugin-error" title={p.last_error}>
              <Icon n="triangle-alert" size={13} />
              <span>{p.last_error}</span>
            </div>
          )}
        </div>
        <div class="pl-tools">
          <button class="ib" aria-pressed={o === 'config'} aria-label={t('plugin.config')} title={t('plugin.config')} data-testid="plugin-config-btn" onClick={() => toggle('config')}>
            <Icon n="settings" size={16} />
          </button>
          <button class="ib" aria-pressed={o === 'logs'} aria-label={t('plugin.logs')} title={t('plugin.logs')} data-testid="plugin-logs-btn" onClick={() => toggle('logs')}>
            <Icon n="scroll-text" size={16} />
          </button>
          <button class="ib" aria-label={t('plugin.data')} title={t('plugin.data')} data-testid="plugin-data-btn" onClick={() => showOver({ kind: 'plugin-perm', dlg: 'data', plugin: p })}>
            <Icon n="database" size={16} />
          </button>
          <button class="ib" aria-label={t('src.btn')} title={t('src.btn')} data-testid="plugin-source-btn" onClick={() => showOver({ kind: 'plugin-perm', dlg: 'source', plugin: p })}>
            <Icon n="code" size={16} />
          </button>
          {p.source_url && (
            <button class="ib" disabled={busy.value === 'u-' + p.id} aria-label={t('plugin.update')} title={t('plugin.update')} data-testid="plugin-update" onClick={() => install({ url: p.source_url }, 'u-' + p.id)}>
              <Icon n="refresh-cw" size={16} />
            </button>
          )}
          <button class="ib danger" aria-label={t('common.delete')} title={t('common.delete')} data-testid="plugin-delete" onClick={askDelete}>
            <Icon n="trash-2" size={16} />
          </button>
          <Switch label={t('plugin.enable', { name: loc(p.name) })} checked={p.enabled} onChange={(v) => v && p.permissions?.ssh ? showOver({ kind: 'plugin-perm', plugin: p, fresh: false }) : setEnabled(p, v)} testid="plugin-toggle" />
        </div>
      </div>
      {o === 'config' && <ConfigPanel key={p.id + (p.updated_at || '')} p={p} />}
      {o === 'logs' && <LogsPanel p={p} />}
    </li>
  )
}

// ---------- add ----------
function CatalogItemRow({ c }: { c: CatalogItem }) {
  const b = busy.value
  return (
    <li class="pl-cat-item" data-testid="plugin-catalog-item" data-plugin-id={c.id} data-installed={c.installed ? '1' : '0'}>
      <div class="pl-cat-row">
        <PlIcon p={c} size={34} />
        <div class="pl-main">
          <div class="pl-name">
            <b>{loc(c.name)}</b>
            <small>v{c.version}</small>
            <button
              class="ib xs"
              aria-label={t('cat.viewSrc', { name: loc(c.name) })}
              title={t('cat.viewSrc', { name: loc(c.name) })}
              data-testid="plugin-catalog-source"
              onClick={() => showOver({ kind: 'plugin-perm', dlg: 'catalog', id: c.id })}
            >
              <Icon n="code" size={14} />
            </button>
          </div>
          <div class="pl-desc">{loc(c.description)}</div>
        </div>
        <div class="pl-cat-btns">
          <button
            class={'btn sm' + (c.installed ? '' : ' primary')}
            data-testid="plugin-catalog-install"
            disabled={c.installed || !!b}
            aria-busy={b === 'c-' + c.id}
            onClick={() => install({ catalog: c.id }, 'c-' + c.id)}
          >
            {c.installed ? t('plugin.installedLbl') : t('plugin.install')}
          </button>
        </div>
      </div>
    </li>
  )
}

function AddView() {
  const tab = addTab.value
  const [url, setUrl] = useState('')
  const [src, setSrc] = useState('')
  const [lg, setLg] = useState<'js' | 'ts'>('js')
  const urlPv = usePreview()
  const srcPv = usePreview()
  const cat = catalog.value
  const market = useChunk(loadPluginMarket, tab === 'market')
  useEffect(() => void loadCatalog(), [version.value])
  const b = busy.value
  return (
    <div data-testid="plugin-add">
      <div class="pl-addbar">
        <button class="btn sm" data-testid="plugin-add-back" onClick={() => (view.value = 'list')}>
          <Icon n="arrow-left" size={14} />
          {t('common.back')}
        </button>
        <div class="seg seg-sm glass-lite" role="group" aria-label={t('plugin.add')} data-testid="plugin-add-tabs">
          {(['catalog', 'market', 'url', 'source'] as const).map((k) => (
            <button key={k} class="seg-b" aria-pressed={tab === k} data-testid={'plugin-add-tab-' + k} onClick={() => (addTab.value = k)}>
              {t('plugin.tab.' + k)}
            </button>
          ))}
        </div>
      </div>
      {tab === 'catalog' && (
        <div data-testid="plugin-catalog">
          <p class="hint">{t('plugin.catalogHint')}</p>
          {cat === null && <p class="hint">…</p>}
          <ul class="pl-cat">
            {(cat || []).map((c) => (
              <CatalogItemRow key={c.id} c={c} />
            ))}
          </ul>
        </div>
      )}
      {tab === 'url' && (
        <form
          class="form"
          onSubmit={(e) => {
            e.preventDefault()
            if (url.trim()) install({ url: url.trim() }, 'url')
          }}
        >
          <p class="hint">{t('plugin.urlHint')}</p>
          <div class="inline-form">
            <input
              type="url"
              inputMode="url"
              autocomplete="off"
              spellcheck={false}
              placeholder="https://example.com/plugin.js"
              aria-label={t('plugin.tab.url')}
              data-testid="plugin-url-input"
              value={url}
              onInput={(e) => (setUrl((e.target as HTMLInputElement).value), urlPv.close())}
            />
            <button class="btn" type="button" aria-expanded={!!urlPv.pv} data-testid="plugin-url-preview" disabled={!url.trim()} onClick={() => urlPv.toggle({ url: url.trim() })}>
              <Icon n="eye" size={14} />
              {urlPv.pv ? t('pv.hide') : t('pv.btn')}
            </button>
            <button class="btn primary" type="submit" data-testid="plugin-url-install" disabled={!url.trim() || !!b}>
              {t('plugin.install')}
            </button>
          </div>
          {urlPv.pv && <PreviewPanel key={urlPv.pv.n} body={urlPv.pv.body} />}
        </form>
      )}
      {tab === 'source' && (
        <div class="form">
          <p class="hint">{t('plugin.sourceHint')}</p>
          <textarea
            class="pl-src"
            spellcheck={false}
            placeholder={'export const messages = {\n  en: { name: "My plugin", demo: "Demo" },\n  zh: { name: "我的插件", demo: "演示" },\n}\nexport const manifest = {\n  id: "my-plugin", name: t("name"), version: "1.0.0",\n  search: { name: t("demo") },\n}\nexport async function search(ctx) { return [] }'}
            aria-label={t('plugin.tab.source')}
            data-testid="plugin-source-input"
            value={src}
            onInput={(e) => (setSrc((e.target as HTMLTextAreaElement).value), srcPv.close())}
          />
          <div class="inline-form">
            <select aria-label={t('plugin.lang')} data-testid="plugin-source-lang" value={lg} onChange={(e) => (setLg((e.target as HTMLSelectElement).value as 'js' | 'ts'), srcPv.close())} style={{ width: 'auto' }}>
              <option value="js">JavaScript</option>
              <option value="ts">TypeScript</option>
            </select>
            <button class="btn" type="button" aria-expanded={!!srcPv.pv} data-testid="plugin-source-preview" disabled={!src.trim()} onClick={() => srcPv.toggle({ source: src, lang: lg })}>
              <Icon n="eye" size={14} />
              {srcPv.pv ? t('pv.hide') : t('pv.check')}
            </button>
            <button class="btn primary" data-testid="plugin-source-install" disabled={!src.trim() || !!b} onClick={() => install({ source: src, lang: lg }, 'src')}>
              {t('plugin.install')}
            </button>
          </div>
          {srcPv.pv && <PreviewPanel key={srcPv.pv.n} body={srcPv.pv.body} />}
        </div>
      )}
      {tab === 'market' && (market ? <market.default /> : <p class="hint">…</p>)}
    </div>
  )
}

export function PluginsTab() {
  useEffect(() => {
    void loadList(false)
    void loadCatalog(true)
  }, [version.value])
  const l = list.value
  if (view.value === 'add') return <AddView />
  return (
    <div data-testid="plugins-tab">
      <div class="gal-top">
        <p class="hint top">{t('plugin.intro')}</p>
        <div class="pl-top-btns">
          <button class="btn sm" data-testid="plugin-dev-ai-btn" onClick={() => showOver({ kind: 'plugin-perm', dlg: 'ai' })}>
            <Icon n="sparkles" size={14} />
            {t('dev.btn')}
          </button>
          <button class="btn primary sm" data-testid="plugin-add-btn" onClick={() => (view.value = 'add')}>
            <Icon n="plus" size={14} />
            {t('plugin.add')}
          </button>
        </div>
      </div>
      {l && !l.length && (
        <div class="pl-empty" data-testid="plugin-empty">
          <Icon n="puzzle" size={30} />
          <p>{t('plugin.empty')}</p>
          <button class="btn" data-testid="plugin-empty-add" onClick={() => (view.value = 'add')}>
            {t('plugin.browse')}
          </button>
        </div>
      )}
      <ul class="pl-list" data-testid="plugin-list">
        {(l || []).map((p) => (
          <Row key={p.id} p={p} />
        ))}
      </ul>
    </div>
  )
}
