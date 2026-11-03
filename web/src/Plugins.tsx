import { signal } from '@preact/signals'
import { useEffect, useState } from 'preact/hooks'
import { del, get, patch, post } from './api'
import { ConfigFields, useConfigForm } from './ConfigForm'
import { Icon } from './icons'
import { LUCIDE } from './lucide'
import { Modal, Switch } from './parts'
import { t } from './i18n'
import { closeModal, load, showOver, toast, toastError, version, type ModalState } from './store'
import type { CatalogItem, ConfigField, PluginInfo, PluginLog } from './types'
import { hueOf, safeHref } from './util'

// ---- state lives at module level: the Settings modal is unmounted while a confirm / permission dialog is stacked on top ----
const list = signal<PluginInfo[] | null>(null)
const catalog = signal<CatalogItem[] | null>(null)
const view = signal<'list' | 'add'>('list')
const addTab = signal<'catalog' | 'url' | 'source'>('catalog')
const open = signal<{ id: string; panel: 'config' | 'logs' } | null>(null)
const busy = signal('')

async function loadList() {
  try {
    list.value = (await get<PluginInfo[] | null>('/api/plugins')) || []
  } catch (e) {
    list.value ??= []
    toastError(e)
  }
}
async function loadCatalog() {
  try {
    catalog.value = (await get<CatalogItem[] | null>('/api/plugins/catalog')) || []
  } catch {
    catalog.value ??= []
  }
}

const plIcon = (n?: string) => (n && LUCIDE[n] ? n : 'puzzle')
const hosts = (p: PluginInfo) => p.permissions?.fetch || []
const linksPerm = (p: PluginInfo) => p.permissions?.links || ''
const schemaOf = (p: PluginInfo) => p.config_schema || []
const hasUrlFetch = (p: PluginInfo) => [...schemaOf(p), ...(p.widgets || []).flatMap((w) => w.config || [])].some((f) => f.type === 'url' && f.allowFetch)
const permKey = (p: PluginInfo) => JSON.stringify([hosts(p).slice().sort(), linksPerm(p), hasUrlFetch(p)])
const needsConfig = (p: PluginInfo) => schemaOf(p).some((f: ConfigField) => f.required && f.type !== 'boolean')

async function setEnabled(p: PluginInfo, enabled: boolean) {
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
 * the admin has seen what it may access. The response carries the manifest, so we then show a permission dialog
 * (fetch hosts, links access) and PATCH `enabled:true` on confirm. Plugins that request nothing, and updates of an already
 * enabled plugin whose permissions did not change, are enabled right away.
 */
async function install(body: Record<string, unknown>, key: string): Promise<boolean> {
  busy.value = key
  try {
    const before = list.value || (await get<PluginInfo[] | null>('/api/plugins').catch(() => null)) || []
    const info = await post<PluginInfo>('/api/plugins', { ...body, enable: false })
    const prev = before.find((x) => x.id === info.id)
    await loadList()
    load()
    view.value = 'list'
    const risky = hosts(info).length > 0 || !!linksPerm(info) || hasUrlFetch(info)
    if (!risky || (prev?.enabled && permKey(prev) === permKey(info))) {
      if (await setEnabled(info, true)) {
        toast(t(prev ? 'plugin.updated' : 'plugin.installed', { name: info.name }), 'success')
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

// ---------- permission confirmation (stacked over Settings) ----------
export function PermModal({ state }: { state: Extract<ModalState, { kind: 'plugin-perm' }> }) {
  const p = state.plugin
  const hs = hosts(p)
  const lp = linksPerm(p)
  const allow = async () => {
    closeModal()
    if (await setEnabled(p, true)) {
      toast(t('plugin.enabled', { name: p.name }), 'success')
      if (needsConfig(p)) open.value = { id: p.id, panel: 'config' }
    }
  }
  return (
    <Modal
      title={t('perm.title', { name: p.name })}
      onClose={closeModal}
      testid="plugin-perm-modal"
      focusFirst={false}
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
            {p.name} <small>v{p.version}</small>
          </b>
          {p.author && <small class="pl-by">{t('plugin.by', { author: p.author })}</small>}
        </div>
      </div>
      {p.description && <p class="hint">{p.description}</p>}
      <h3 class="sub first">{t('perm.requests')}</h3>
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
        {lp && (
          <li class={'pl-perm' + (lp === 'write' ? ' warn' : '')} data-testid="plugin-perm-links" data-level={lp}>
            <Icon n={lp === 'write' ? 'triangle-alert' : 'bookmark'} size={16} />
            <span>{t(lp === 'write' ? 'perm.linksWrite' : 'perm.linksRead')}</span>
          </li>
        )}
      </ul>
      <p class="hint">{t('perm.note')}</p>
      {!state.fresh && <p class="hint">{t('perm.updateNote')}</p>}
    </Modal>
  )
}

// ---------- list ----------
function PlIcon({ p, size = 36 }: { p: { id: string; icon?: string; name: string }; size?: number }) {
  return (
    <span class="ico ico-lucide pl-ico" style={{ '--s': size + 'px', '--h': hueOf(p.id) } as any}>
      <Icon n={plIcon(p.icon)} size={Math.round(size * 0.55)} />
    </span>
  )
}

function Badges({ p }: { p: PluginInfo }) {
  const b: [string, number][] = []
  if (p.wallpapers?.length) b.push(['wallpaper', p.wallpapers.length])
  if (p.widgets?.length) b.push(['widget', p.widgets.length])
  if (p.search) b.push(['search', 1])
  if (p.schedules?.length) b.push(['schedule', p.schedules.length])
  return (
    <span class="pl-badges">
      {b.map(([k]) => (
        <span key={k} class="pl-badge" data-kind={k} data-testid="plugin-badge">
          {t('cap.' + k)}
        </span>
      ))}
    </span>
  )
}

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
      message: t('plugin.deleteMsg', { name: p.name }),
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
            <b data-testid="plugin-name">{p.name}</b>
            <small>v{p.version}</small>
            <Badges p={p} />
          </div>
          {p.description && <div class="pl-desc">{p.description}</div>}
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
          {p.source_url && (
            <button class="ib" disabled={busy.value === 'u-' + p.id} aria-label={t('plugin.update')} title={t('plugin.update')} data-testid="plugin-update" onClick={() => install({ url: p.source_url }, 'u-' + p.id)}>
              <Icon n="refresh-cw" size={16} />
            </button>
          )}
          <button class="ib danger" aria-label={t('common.delete')} title={t('common.delete')} data-testid="plugin-delete" onClick={askDelete}>
            <Icon n="trash-2" size={16} />
          </button>
          <Switch label={t('plugin.enable', { name: p.name })} checked={p.enabled} onChange={(v) => setEnabled(p, v)} testid="plugin-toggle" />
        </div>
      </div>
      {o === 'config' && <ConfigPanel key={p.id + (p.updated_at || '')} p={p} />}
      {o === 'logs' && <LogsPanel p={p} />}
    </li>
  )
}

// ---------- add ----------
function AddView() {
  const tab = addTab.value
  const [url, setUrl] = useState('')
  const [src, setSrc] = useState('')
  const [lang, setLang] = useState<'js' | 'ts'>('js')
  const cat = catalog.value
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
          {(['catalog', 'url', 'source'] as const).map((k) => (
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
              <li key={c.id} class="pl-cat-item" data-testid="plugin-catalog-item" data-plugin-id={c.id} data-installed={c.installed ? '1' : '0'}>
                <PlIcon p={c} size={34} />
                <div class="pl-main">
                  <div class="pl-name">
                    <b>{c.name}</b>
                    <small>v{c.version}</small>
                  </div>
                  <div class="pl-desc">{c.description}</div>
                </div>
                <button
                  class={'btn sm' + (c.installed ? '' : ' primary')}
                  data-testid="plugin-catalog-install"
                  disabled={c.installed || !!b}
                  aria-busy={b === 'c-' + c.id}
                  onClick={() => install({ catalog: c.id }, 'c-' + c.id)}
                >
                  {c.installed ? t('plugin.installedLbl') : t('plugin.install')}
                </button>
              </li>
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
            <input type="url" inputMode="url" autocomplete="off" spellcheck={false} placeholder="https://example.com/plugin.js" aria-label={t('plugin.tab.url')} data-testid="plugin-url-input" value={url} onInput={(e) => setUrl((e.target as HTMLInputElement).value)} />
            <button class="btn primary" type="submit" data-testid="plugin-url-install" disabled={!url.trim() || !!b}>
              {t('plugin.install')}
            </button>
          </div>
        </form>
      )}
      {tab === 'source' && (
        <div class="form">
          <p class="hint">{t('plugin.sourceHint')}</p>
          <textarea class="pl-src" spellcheck={false} placeholder={'export const manifest = {\n  id: "my-plugin", name: "My plugin", version: "1.0.0",\n  search: { name: "Demo" },\n}\nexport async function search(ctx) { return [] }'} aria-label={t('plugin.tab.source')} data-testid="plugin-source-input" value={src} onInput={(e) => setSrc((e.target as HTMLTextAreaElement).value)} />
          <div class="inline-form">
            <select aria-label={t('plugin.lang')} data-testid="plugin-source-lang" value={lang} onChange={(e) => setLang((e.target as HTMLSelectElement).value as 'js' | 'ts')} style={{ width: 'auto' }}>
              <option value="js">JavaScript</option>
              <option value="ts">TypeScript</option>
            </select>
            <button class="btn primary" data-testid="plugin-source-install" disabled={!src.trim() || !!b} onClick={() => install({ source: src, lang }, 'src')}>
              {t('plugin.install')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

export function PluginsTab() {
  useEffect(() => void loadList(), [version.value])
  const l = list.value
  if (view.value === 'add') return <AddView />
  return (
    <div data-testid="plugins-tab">
      <div class="gal-top">
        <p class="hint top">{t('plugin.intro')}</p>
        <button class="btn primary sm" data-testid="plugin-add-btn" onClick={() => (view.value = 'add')}>
          <Icon n="plus" size={14} />
          {t('plugin.add')}
        </button>
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
