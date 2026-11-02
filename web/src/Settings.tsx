import { useEffect, useState } from 'preact/hooks'
import { post } from './api'
import { AiTab } from './Ai'
import { ContentTab } from './Content'
import { WallpaperGallery } from './Gallery'
import { Icon } from './icons'
import { Modal, Row, Switch } from './parts'
import { lang, setLang, t } from './i18n'
import { closeModal, curWs, importData, patchSettings, settings, settingsTab, sortedWs, toast, serverVersion, toastError, updateWorkspace, version, type ModalState } from './store'


function TextSetting({ k, testid, type = 'text', min, list }: { k: 'title' | 'search_engine' | 'health_interval'; testid: string; type?: string; min?: number; list?: string }) {
  const cur = String(settings.value[k])
  const [v, setV] = useState(cur)
  useEffect(() => setV(cur), [cur])
  const commit = () => {
    if (v === cur) return
    if (k === 'health_interval') {
      const n = Math.max(15, parseInt(v, 10) || 60)
      setV(String(n))
      patchSettings({ health_interval: n })
    } else if (k === 'title') {
      const s = v.trim() || 'Genkan'
      setV(s)
      patchSettings({ title: s })
    } else patchSettings({ [k]: v.trim() })
  }
  return <input type={type} min={min} list={list} data-testid={testid} value={v} onInput={(e) => setV((e.target as HTMLInputElement).value)} onChange={commit} onKeyDown={(e) => e.key === 'Enter' && commit()} />
}

function General() {
  const s = settings.value
  return (
    <div class="rows">
      <Row label={t('set.title')}>
        <TextSetting k="title" testid="set-title" />
      </Row>
      <Row label={t('set.language')}>
        <select data-testid="set-lang" value={lang.value} onChange={(e) => setLang((e.target as HTMLSelectElement).value as any)}>
          <option value="zh">中文</option>
          <option value="en">English</option>
        </select>
      </Row>
      <Row label={t('set.clock')}>
        <Switch label={t('set.clock')} checked={s.show_clock} onChange={(v) => patchSettings({ show_clock: v })} testid="set-clock" />
      </Row>
      <Row label={t('set.newtab')}>
        <Switch label={t('set.newtab')} checked={s.open_in_new_tab} onChange={(v) => patchSettings({ open_in_new_tab: v })} testid="set-newtab" />
      </Row>
      <Row label={t('set.engine')} hint={t('set.engineHint')}>
        <TextSetting k="search_engine" testid="set-engine" list="engines" />
        <datalist id="engines">
          <option value="https://www.google.com/search?q=%s" />
          <option value="https://www.bing.com/search?q=%s" />
          <option value="https://duckduckgo.com/?q=%s" />
          <option value="https://www.baidu.com/s?wd=%s" />
        </datalist>
      </Row>
      <Row label={t('set.sort')}>
        <select data-testid="set-sort" value={s.sort_mode} onChange={(e) => patchSettings({ sort_mode: (e.target as HTMLSelectElement).value as any })}>
          <option value="frequency">{t('sort.frequency')}</option>
          <option value="recent">{t('sort.recent')}</option>
          <option value="custom">{t('sort.custom')}</option>
        </select>
      </Row>
      <Row label={t('set.health')} hint={t('set.healthHint')}>
        <TextSetting k="health_interval" type="number" min={15} testid="set-health" />
      </Row>
    </div>
  )
}

function Appearance() {
  const ws = curWs.value
  const [busy, setBusy] = useState(false)
  if (!ws) return null
  const others = sortedWs.value.filter((w) => w.id !== ws.id && w.wallpaper !== ws.wallpaper)
  const applyAll = async () => {
    setBusy(true)
    await Promise.all(others.map((w) => updateWorkspace(w.id, { wallpaper: ws.wallpaper })))
    setBusy(false)
    toast(t('wp.appliedAll'), 'success')
  }
  return (
    <div>
      <div class="gal-top">
        <p class="hint top">{t('set.wallpaperFor', { name: ws.name })}</p>
        {sortedWs.value.length > 1 && (
          <button class="btn sm" data-testid="wp-apply-all" disabled={busy || !others.length} onClick={applyAll}>
            {t('wp.applyAll')}
          </button>
        )}
      </div>
      <WallpaperGallery value={ws.wallpaper} onPick={(v) => updateWorkspace(ws.id, { wallpaper: v })} manage />
    </div>
  )
}

function Security() {
  const [o, setO] = useState('')
  const [n, setN] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: Event) => {
    e.preventDefault()
    setErr('')
    if (n.length < 6) return setErr(t('auth.tooShort'))
    setBusy(true)
    try {
      await post('/api/auth/password', { old_password: o, new_password: n })
      setO('')
      setN('')
      toast(t('toast.pwChanged'), 'success')
    } catch (er: any) {
      setErr(er?.message || t('err.generic'))
    }
    setBusy(false)
  }
  return (
    <div>
      <div class="rows">
        <Row label={t('set.guest')} hint={t('set.guestHint')}>
          <Switch label={t('set.guest')} checked={settings.value.guest_access} onChange={(v) => patchSettings({ guest_access: v })} testid="set-guest" />
        </Row>
      </div>
      <h3 class="sub">{t('set.password')}</h3>
      <form class="form" onSubmit={submit}>
        <div class="field">
          <label for="pw-o">{t('set.pwOld')}</label>
          <input id="pw-o" type="password" autocomplete="current-password" data-testid="pw-old" value={o} onInput={(e) => setO((e.target as HTMLInputElement).value)} />
        </div>
        <div class="field">
          <label for="pw-n">{t('set.pwNew')}</label>
          <input id="pw-n" type="password" autocomplete="new-password" data-testid="pw-new" value={n} onInput={(e) => setN((e.target as HTMLInputElement).value)} />
        </div>
        {err && (
          <p class="form-error" role="alert" data-testid="pw-error">
            {err}
          </p>
        )}
        <div>
          <button class="btn primary" type="submit" data-testid="pw-save" disabled={busy || !o || !n}>
            {t('set.pwSave')}
          </button>
        </div>
      </form>
    </div>
  )
}

function DataTab() {
  const [mode, setMode] = useState<'merge' | 'replace'>('merge')
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)
  const run = async () => {
    if (!file) return
    setBusy(true)
    try {
      const data = JSON.parse(await file.text())
      await importData(mode, data)
      setFile(null)
    } catch (e) {
      if (e instanceof SyntaxError) toast(t('data.badFile'), 'error')
      else toastError(e)
    }
    setBusy(false)
  }
  return (
    <div>
      <h3 class="sub first">{t('data.export')}</h3>
      <p class="hint">{t('data.exportHint')}</p>
      <a class="btn" href="/api/export" download="genkan-export.json" data-testid="export-btn">
        <Icon n="download" size={15} />
        {t('data.exportBtn')}
      </a>
      <h3 class="sub">{t('data.import')}</h3>
      <p class="hint">{t('data.importHint')}</p>
      <div class="seg seg-sm glass-lite" role="group" aria-label={t('data.import')} data-testid="import-mode">
        <button class="seg-b" aria-pressed={mode === 'merge'} data-testid="import-mode-merge" onClick={() => setMode('merge')}>
          {t('data.merge')}
        </button>
        <button class="seg-b" aria-pressed={mode === 'replace'} data-testid="import-mode-replace" onClick={() => setMode('replace')}>
          {t('data.replace')}
        </button>
      </div>
      <p class="hint">{mode === 'merge' ? t('data.mergeHint') : t('data.replaceHint')}</p>
      <div class="inline-form">
        <label class="btn upload-btn">
          <Icon n="upload" size={15} />
          <span class="ellipsis">{file ? file.name : t('data.choose')}</span>
          <input type="file" hidden accept="application/json,.json" data-testid="import-file" onChange={(e) => setFile((e.target as HTMLInputElement).files?.[0] || null)} />
        </label>
        <button class={'btn ' + (mode === 'replace' ? 'danger-solid' : 'primary')} disabled={!file || busy} data-testid="import-btn" onClick={run}>
          {t('data.importBtn')}
        </button>
      </div>
    </div>
  )
}

function About() {
  return (
    <div class="about">
      <div class="about-t">Genkan 玄関</div>
      <p class="hint">{t('about.desc')}</p>
      <dl>
        <dt>{t('about.version')}</dt>
        <dd data-testid="about-version">{serverVersion.value || '-'}</dd>
        <dt>{t('about.revision')}</dt>
        <dd data-testid="about-revision">{version.value}</dd>
      </dl>
    </div>
  )
}

const TABS = ['general', 'appearance', 'content', 'ai', 'security', 'data', 'about'] as const
export function SettingsModal(_: { state: Extract<ModalState, { kind: 'settings' }> }) {
  const raw = settingsTab.value
  const tab = (TABS as readonly string[]).includes(raw) ? raw : 'general'
  return (
    <Modal title={t('top.settings')} onClose={closeModal} wide testid="settings-modal" focusFirst={false}>
      <div class="settings">
        <div class="stabs" role="tablist" aria-orientation="vertical">
          {TABS.map((k) => (
            <button key={k} role="tab" class="stab" aria-selected={tab === k} data-testid={'settings-tab-' + k} onClick={() => (settingsTab.value = k)}>
              {t('tab.' + k)}
            </button>
          ))}
        </div>
        <div class="spanel" role="tabpanel" data-testid={'settings-panel-' + tab}>
          {tab === 'general' && <General />}
          {tab === 'appearance' && <Appearance />}
          {tab === 'content' && <ContentTab />}
          {tab === 'ai' && <AiTab />}
          {tab === 'security' && <Security />}
          {tab === 'data' && <DataTab />}
          {tab === 'about' && <About />}
        </div>
      </div>
    </Modal>
  )
}
