// "Third-party" tab of the Add-plugin view: GitHub repositories tagged `genkan-plugin` (docs/PLUGINS.md §11.4). Lazy chunk.
// State is module-level because the Settings modal is unmounted while the combined permission dialog is open on top of it.
import { signal } from '@preact/signals'
import { useEffect, useState } from 'preact/hooks'
import { ApiError, get } from './api'
import { Icon } from './icons'
import { t } from './i18n'
import type { MarketList, MarketPlugin, MarketRepo, MarketScan } from './types'
import { hueOf, safeHref } from './util'
import { Badges, PermChips, PlIcon, PreviewPanel, busy, installMany, progress, relTime } from './pluginKit'

type Sort = 'stars' | 'updated'
interface Scan {
  loading: boolean
  data?: MarketScan
  error?: string
}

const q = signal('')
const sort = signal<Sort>('stars')
const repos = signal<MarketRepo[]>([])
const total = signal(0)
const page = signal(0)
const more = signal(false)
const loading = signal(false)
const loaded = signal(false)
const errText = signal('')
const scans = signal<Record<string, Scan>>({})
const expanded = signal<Record<string, boolean>>({})
const sel = signal<Record<string, boolean>>({}) // plugin raw URL -> selected
const previews = signal<Record<string, boolean>>({}) // plugin raw URL -> source preview open
const progRepo = signal('')

const msgOf = (e: unknown) => (e instanceof ApiError ? (e.status === 0 ? t('err.network') : e.message) : t('err.generic'))

const MAX_PAGE = 30
let seq = 0
async function fetchRepos(reset: boolean) {
  if (loading.value && !reset) return
  const my = ++seq
  loading.value = true
  errText.value = ''
  const pg = reset ? 1 : page.value + 1
  try {
    const r = await get<MarketList>(`/api/plugins/market?sort=${sort.value}&q=${encodeURIComponent(q.value)}&page=${pg}`)
    if (my !== seq) return
    const items = r.items || []
    if (r.error) errText.value = r.error
    // the server clamps `page` to 1-30 (20 per page): never ask beyond it, and drop repeats
    const seen = new Set(reset ? [] : repos.value.map((x) => x.repo))
    repos.value = reset ? items : [...repos.value, ...items.filter((x) => !seen.has(x.repo))]
    total.value = r.total ?? repos.value.length
    page.value = pg
    more.value = !r.error && items.length > 0 && pg < MAX_PAGE && repos.value.length < total.value
  } catch (e) {
    if (my !== seq) return
    errText.value = msgOf(e)
    if (reset) {
      repos.value = []
      more.value = false
    }
  } finally {
    if (my === seq) {
      loading.value = false
      loaded.value = true
    }
  }
}

async function fetchScan(repo: string) {
  scans.value = { ...scans.value, [repo]: { loading: true } }
  try {
    const d = await get<MarketScan>('/api/plugins/market/repo?repo=' + encodeURIComponent(repo))
    scans.value = { ...scans.value, [repo]: { loading: false, data: d, error: d.error } }
  } catch (e) {
    scans.value = { ...scans.value, [repo]: { loading: false, error: msgOf(e) } }
  }
}

function toggleRepo(repo: string) {
  const on = !expanded.value[repo]
  expanded.value = { ...expanded.value, [repo]: on }
  const sc = scans.value[repo]
  if (on && (!sc || (sc.error && !sc.loading))) fetchScan(repo)
}

const fmtStars = (n: number) => (n >= 10000 ? Math.round(n / 1000) + 'k' : n >= 1000 ? (n / 1000).toFixed(1).replace(/\.0$/, '') + 'k' : String(n))
const valid = (x: MarketPlugin) => !!x.plugin && !x.error

async function runInstall(repo: string, items: MarketPlugin[]) {
  if (busy.value || !items.length) return
  progRepo.value = repo
  const ok = await installMany(items.map((x) => ({ url: x.url, name: x.plugin?.name || x.path })))
  if (!ok.length) return
  const done = new Set(ok)
  const sc = scans.value[repo]
  if (sc?.data?.plugins)
    scans.value = {
      ...scans.value,
      [repo]: { ...sc, data: { ...sc.data, plugins: sc.data.plugins.map((x) => (done.has(x.url) ? { ...x, installed: true, installed_version: x.plugin?.version } : x)) } },
    }
  const s = { ...sel.value }
  for (const u of ok) delete s[u]
  sel.value = s
}

function Avatar({ r }: { r: MarketRepo }) {
  const [bad, setBad] = useState(false)
  const owner = r.repo.split('/')[0] || '?'
  if (r.owner_avatar && !bad)
    return <img class="mk-avatar" src={safeHref(r.owner_avatar)} alt="" loading="lazy" referrerpolicy="no-referrer" draggable={false} onError={() => setBad(true)} />
  return (
    <span class="mk-avatar mk-avatar-l" style={{ '--h': hueOf(owner) } as any}>
      {owner.charAt(0).toUpperCase()}
    </span>
  )
}

function PluginRow({ x }: { x: MarketPlugin }) {
  const p = x.plugin
  const ok = valid(x)
  const prev = !!previews.value[x.url]
  const upd = x.installed && p && x.installed_version && x.installed_version !== p.version
  return (
    <li class="mk-pl" data-testid="market-plugin" data-path={x.path} data-installed={x.installed ? '1' : '0'} data-valid={ok ? '1' : '0'}>
      <div class="mk-pl-row">
        <input
          type="checkbox"
          class="mk-chk"
          id={'mk-' + x.url}
          aria-label={p?.name || x.path}
          data-testid="market-plugin-check"
          disabled={!ok}
          checked={!!sel.value[x.url]}
          onChange={(e) => (sel.value = { ...sel.value, [x.url]: (e.target as HTMLInputElement).checked })}
        />
        {p ? <PlIcon p={p} size={32} /> : <span class="ico ico-lucide pl-ico" style={{ '--s': '32px', '--h': 355 } as any}><Icon n="triangle-alert" size={17} /></span>}
        <label class="pl-main" for={'mk-' + x.url}>
          <span class="pl-name">
            <b data-testid="market-plugin-name">{p?.name || x.path}</b>
            {p && <small>v{p.version}</small>}
            {p && <Badges p={p} />}
          </span>
          <span class="mk-path" title={x.url}>{x.path}</span>
          {p?.description && <span class="pl-desc">{p.description}</span>}
          {p && <PermChips p={p} />}
          {x.error && (
            <span class="pl-error" data-testid="market-plugin-error" title={x.error}>
              <Icon n="triangle-alert" size={13} />
              <span>{x.error}</span>
            </span>
          )}
        </label>
        <div class="mk-pl-side">
          {x.installed && (
            <span class="mk-inst" data-testid="market-plugin-installed">
              {upd ? t('mk.update', { v: x.installed_version! }) : t('mk.installedV', { v: x.installed_version || p?.version || '' })}
            </span>
          )}
          <button class="btn sm" aria-expanded={prev} data-testid="market-plugin-preview" onClick={() => (previews.value = { ...previews.value, [x.url]: !prev })}>
            <Icon n="eye" size={14} />
            {t('pv.btn')}
          </button>
        </div>
      </div>
      {prev && <PreviewPanel body={{ url: x.url }} testid="market-preview" />}
    </li>
  )
}

function Progress({ repo }: { repo: string }) {
  const pr = progress.value
  if (!pr || progRepo.value !== repo) return null
  const pct = pr.total ? Math.round((pr.done / pr.total) * 100) : 0
  return (
    <div class="mk-prog" role="status" data-testid="market-progress" data-finished={pr.finished ? '1' : '0'}>
      <div class="mk-prog-t">
        {pr.finished ? (
          <span data-testid="market-progress-summary">{t('mk.summary', { ok: pr.ok.length, failed: pr.failed.length })}</span>
        ) : (
          <span data-testid="market-progress-text">{t('mk.progress', { done: pr.done, total: pr.total, name: pr.cur })}</span>
        )}
        {pr.finished && (
          <button class="ib" aria-label={t('common.close')} data-testid="market-progress-dismiss" onClick={() => (progress.value = null)}>
            <Icon n="x" size={14} />
          </button>
        )}
      </div>
      <div class="mk-bar-track" aria-hidden="true">
        <i style={{ width: pct + '%' }} />
      </div>
      {pr.failed.length > 0 && (
        <ul class="mk-fails" data-testid="market-progress-failed">
          {pr.failed.map((f) => (
            <li key={f.name}>
              <b>{f.name}</b> — {f.error}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function Scanned({ repo }: { repo: string }) {
  const sc = scans.value[repo]
  const b = busy.value
  if (!sc || sc.loading) return <p class="hint mk-scan-load" data-testid="market-scan-loading">{t('mk.scanning')}</p>
  if (sc.error)
    return (
      <div class="form-error mk-err" role="alert" data-testid="market-scan-error">
        <span>{sc.error}</span>
        <button class="btn sm" onClick={() => fetchScan(repo)}>
          {t('common.retry')}
        </button>
      </div>
    )
  const items = sc.data?.plugins || []
  if (!items.length) return <p class="hint" data-testid="market-scan-empty">{t('mk.noPlugins')}</p>
  const ok = items.filter(valid)
  const chosen = ok.filter((x) => sel.value[x.url])
  const allChosen = ok.length > 0 && chosen.length === ok.length
  return (
    <div class="mk-scan" data-testid="market-scan">
      <ul class="mk-pls">
        {items.map((x) => (
          <PluginRow key={x.url} x={x} />
        ))}
      </ul>
      <div class="mk-actions">
        <button
          class="btn sm"
          data-testid="market-select-all"
          disabled={!ok.length}
          onClick={() => {
            const s = { ...sel.value }
            for (const x of ok) allChosen ? delete s[x.url] : (s[x.url] = true)
            sel.value = s
          }}
        >
          {allChosen ? t('mk.selectNone') : t('mk.selectAll')}
        </button>
        <span class="mk-grow" />
        <button class="btn sm" data-testid="market-install-selected" disabled={!chosen.length || !!b} onClick={() => runInstall(repo, chosen)}>
          {t('mk.installSelected', { n: chosen.length })}
        </button>
        <button class="btn primary sm" data-testid="market-install-all" disabled={!ok.length || !!b} onClick={() => runInstall(repo, ok)}>
          {t('mk.installAll', { n: ok.length })}
        </button>
      </div>
      <Progress repo={repo} />
    </div>
  )
}

function RepoCard({ r }: { r: MarketRepo }) {
  const open = !!expanded.value[r.repo]
  return (
    <li class="mk-card" data-testid="market-repo" data-repo={r.repo} data-open={open ? '1' : '0'}>
      <div class="mk-head">
        <Avatar r={r} />
        <div class="pl-main">
          <div class="pl-name">
            <b data-testid="market-repo-name">{r.repo}</b>
          </div>
          <div class="pl-desc mk-desc">{r.description || t('mk.noDesc')}</div>
          <div class="mk-meta">
            <span title={t('mk.stars')} data-testid="market-repo-stars">
              <Icon n="star" size={13} />
              {fmtStars(r.stars || 0)}
            </span>
            <span title={r.updated_at ? new Date(r.updated_at).toLocaleString() : ''} data-testid="market-repo-updated">
              <Icon n="clock" size={13} />
              {t('mk.updated', { when: relTime(r.updated_at) })}
            </span>
            <a href={safeHref(r.html_url)} target="_blank" rel="noopener noreferrer" data-testid="market-repo-link">
              <Icon n="external-link" size={13} />
              GitHub
            </a>
          </div>
        </div>
        <button class="btn sm" aria-expanded={open} data-testid="market-repo-toggle" onClick={() => toggleRepo(r.repo)}>
          {t('mk.view')}
          <Icon n={open ? 'chevron-up' : 'chevron-down'} size={14} />
        </button>
      </div>
      {open && <Scanned repo={r.repo} />}
    </li>
  )
}

export default function MarketTab() {
  const [input, setInput] = useState(q.value)
  useEffect(() => {
    if (!loaded.value) fetchRepos(true)
  }, [])
  useEffect(() => {
    if (input.trim() === q.value) return
    const h = setTimeout(() => {
      q.value = input.trim()
      fetchRepos(true)
    }, 400)
    return () => clearTimeout(h)
  }, [input])
  const submit = (e: Event) => {
    e.preventDefault()
    q.value = input.trim()
    fetchRepos(true)
  }
  const list = repos.value
  return (
    <div data-testid="plugin-market">
      <p class="warn-box" role="note" data-testid="market-warning">
        <Icon n="triangle-alert" size={16} />
        <span>{t('mk.warn')}</span>
      </p>
      <form class="mk-tools" onSubmit={submit} role="search">
        <span class="mk-q">
          <Icon n="search" size={15} />
          <input type="text" autocomplete="off" spellcheck={false} placeholder={t('mk.search')} aria-label={t('mk.search')} data-testid="market-search" value={input} onInput={(e) => setInput((e.target as HTMLInputElement).value)} />
        </span>
        <div class="seg seg-sm glass-lite" role="group" aria-label={t('mk.sort')} data-testid="market-sort">
          {(['stars', 'updated'] as const).map((k) => (
            <button
              key={k}
              type="button"
              class="seg-b"
              aria-pressed={sort.value === k}
              data-testid={'market-sort-' + k}
              onClick={() => {
                if (sort.value === k) return
                sort.value = k
                fetchRepos(true)
              }}
            >
              {t('mk.sort.' + k)}
            </button>
          ))}
        </div>
      </form>
      {errText.value && (
        <div class="form-error mk-err" role="alert" data-testid="market-error">
          <span>{errText.value}</span>
          <button class="btn sm" data-testid="market-retry" disabled={loading.value} onClick={() => fetchRepos(true)}>
            {t('common.retry')}
          </button>
        </div>
      )}
      {loading.value && !list.length && <p class="hint" data-testid="market-loading">{t('mk.loading')}</p>}
      {loaded.value && !loading.value && !list.length && !errText.value && (
        <div class="pl-empty" data-testid="market-empty">
          <Icon n="search" size={26} />
          <p>{t('mk.empty')}</p>
        </div>
      )}
      <ul class="mk-list" data-testid="market-list" aria-busy={loading.value}>
        {list.map((r) => (
          <RepoCard key={r.repo} r={r} />
        ))}
      </ul>
      {list.length > 0 && (
        <div class="mk-more">
          <span class="hint">{t('mk.count', { n: list.length, total: total.value })}</span>
          {more.value && (
            <button class="btn sm" data-testid="market-more" disabled={loading.value} onClick={() => fetchRepos(false)}>
              {loading.value ? t('mk.loading') : t('mk.more')}
            </button>
          )}
        </div>
      )}
      <p class="hint mk-topic">{t('mk.topic')}</p>
    </div>
  )
}
