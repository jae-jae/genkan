// Update UI (docs/UPDATES.md §7): the About section and the changelog / update panel. Lazy chunk: it also holds the markdown renderer and its strings.
import { signal } from '@preact/signals'
import { useEffect, useState } from 'preact/hooks'
import { get, post } from './api'
import { addStrings, lang, t } from './i18n'
import { Icon } from './icons'
import { renderMarkdown } from './md'
import { Modal, Row, Switch } from './parts'
import { closeModal, patchSettings, settings, showOver, toast, toastError } from './store'
import { refreshUpdate, setUpd, upd } from './updState'
import { copyText } from './util'
import type { UpdateStatus } from './types'

addStrings(
  {
    'upd.auto': 'Check for updates automatically',
    'upd.autoHint': 'Asks GitHub for new releases about every 6 hours. Only the release list is downloaded.',
    'upd.envOff': 'Update checks are turned off by the GENKAN_UPDATE_CHECK environment variable.',
    'upd.checkNow': 'Check now',
    'upd.checking': 'Checking…',
    'upd.checkedAt': 'Last checked: {when}',
    'upd.never': 'Not checked yet',
    'upd.checkErr': 'The last check failed: {err}',
    'upd.latest': 'Latest version',
    'upd.upToDate': 'You are up to date',
    'upd.avail': 'Update available',
    'upd.changelog': 'View changes',
    'upd.dev': 'This is a development build. Update checks and self-update are disabled.',
    'upd.rolledBack': 'The update to {v} did not start correctly and was rolled back automatically. Your data was restored from the backup.',
    'upd.dismiss': 'Dismiss',
    'upd.panelTitle': 'Update Genkan',
    'upd.from': 'You are running {cur}. The newest release is {latest}.',
    'upd.released': 'Released {date}',
    'upd.onGithub': 'GitHub',
    'upd.noNotes': 'No release notes.',
    'upd.now': 'Update now',
    'upd.skip': 'Skip this version',
    'upd.skipped': 'Version {v} skipped',
    'upd.confirmTitle': 'Update to {v}?',
    'upd.updating': 'Updating to {v}',
    'upd.confirmBody': 'Genkan downloads the release, verifies its signature and checksum, backs up your data, and restarts. The page reloads by itself when the new version is up.',
    'upd.confirmRollback': 'If the new version fails to start, Genkan rolls back to {cur} and restores the backup automatically.',
    'upd.confirmOk': 'Update and restart',
    'upd.back': 'Back',
    'upd.phase.downloading': 'Downloading…',
    'upd.phase.verifying': 'Verifying signature and checksum…',
    'upd.phase.installing': 'Installing and backing up your data…',
    'upd.phase.restarting': 'Restarting Genkan…',
    'upd.phase.failed': 'The update failed.',
    'upd.timeout': 'Genkan did not come back within 3 minutes. Check the container logs (for example docker compose logs genkan), then reload this page.',
    'upd.reload': 'Reload page',
    'upd.unsupported': 'This installation cannot update itself',
    'upd.reason.disabled': 'Self-update is turned off (GENKAN_SELF_UPDATE=off).',
    'upd.reason.platform': 'Self-update only works on Linux.',
    'upd.reason.noexec': 'The data directory is mounted noexec, so a downloaded binary cannot run.',
    'upd.reason.dev': 'This is a development build.',
    'upd.reason.other': 'Self-update is not available here.',
    'upd.pullHint': 'Pull the new image instead:',
    'upd.copyCmd': 'Copy command',
    'upd.applyErr': 'Could not start the update: {err}',
  },
  {
    'upd.auto': '自动检查更新',
    'upd.autoHint': '约每 6 小时向 GitHub 查询一次新版本，只会下载发布列表。',
    'upd.envOff': '环境变量 GENKAN_UPDATE_CHECK 已关闭更新检查。',
    'upd.checkNow': '立即检查',
    'upd.checking': '检查中…',
    'upd.checkedAt': '上次检查：{when}',
    'upd.never': '尚未检查',
    'upd.checkErr': '上次检查失败：{err}',
    'upd.latest': '最新版本',
    'upd.upToDate': '已是最新版本',
    'upd.avail': '有可用更新',
    'upd.changelog': '查看更新内容',
    'upd.dev': '这是开发版本，已禁用更新检查和自我更新。',
    'upd.rolledBack': '更新到 {v} 后未能正常启动，已自动回滚，数据已从备份恢复。',
    'upd.dismiss': '知道了',
    'upd.panelTitle': '更新 Genkan',
    'upd.from': '当前版本 {cur}，最新版本 {latest}。',
    'upd.released': '发布于 {date}',
    'upd.onGithub': 'GitHub',
    'upd.noNotes': '没有更新说明。',
    'upd.now': '立即更新',
    'upd.skip': '跳过此版本',
    'upd.skipped': '已跳过 {v}',
    'upd.confirmTitle': '更新到 {v}？',
    'upd.updating': '正在更新到 {v}',
    'upd.confirmBody': 'Genkan 会下载新版本，校验签名和校验和，备份数据后重启。新版本启动后页面会自动刷新。',
    'upd.confirmRollback': '如果新版本无法启动，Genkan 会自动回滚到 {cur} 并恢复备份。',
    'upd.confirmOk': '更新并重启',
    'upd.back': '返回',
    'upd.phase.downloading': '下载中…',
    'upd.phase.verifying': '正在校验签名和校验和…',
    'upd.phase.installing': '正在安装并备份数据…',
    'upd.phase.restarting': '正在重启 Genkan…',
    'upd.phase.failed': '更新失败。',
    'upd.timeout': 'Genkan 在 3 分钟内没有恢复。请查看容器日志（例如 docker compose logs genkan），然后刷新本页。',
    'upd.reload': '刷新页面',
    'upd.unsupported': '当前部署无法自我更新',
    'upd.reason.disabled': '自我更新已被关闭（GENKAN_SELF_UPDATE=off）。',
    'upd.reason.platform': '自我更新仅支持 Linux。',
    'upd.reason.noexec': '数据目录以 noexec 挂载，下载的程序无法运行。',
    'upd.reason.dev': '这是开发版本。',
    'upd.reason.other': '此处无法自我更新。',
    'upd.pullHint': '请改为拉取新镜像：',
    'upd.copyCmd': '复制命令',
    'upd.applyErr': '无法开始更新：{err}',
  },
)

const PULL_CMD = 'docker compose pull && docker compose up -d'
const TIMEOUT_MS = 180_000
const ACTIVE = ['downloading', 'verifying', 'installing', 'restarting']

const dateFmt = (ms: number) => (ms ? new Date(ms).toLocaleDateString(lang.value === 'zh' ? 'zh-CN' : 'en', { year: 'numeric', month: 'short', day: 'numeric' }) : '')
const timeFmt = (ms: number) => new Date(ms).toLocaleString(lang.value === 'zh' ? 'zh-CN' : 'en')

// ---------- applying: poll until the new version answers ----------

/** Set while this page waits for an update; `restartAt` is when the server went away / started restarting. */
const wait = signal<{ target: string; restartAt: number; error: string } | null>(null)
let polling = ''
let pollTimer = 0

function stopPoll() {
  polling = ''
  clearTimeout(pollTimer)
}

/** Polls GET /api/update (tolerating errors) until `current === target`, then reloads. Gives up 3 minutes after the restart began. */
function pollUpdate(target: string) {
  if (polling === target) return
  stopPoll()
  polling = target
  wait.value = { target, restartAt: 0, error: '' }
  const fail = (error: string) => {
    stopPoll()
    wait.value = { ...wait.value!, error }
  }
  const tick = async () => {
    if (polling !== target) return
    let s: UpdateStatus | null = null
    try {
      s = await get<UpdateStatus>('/api/update')
    } catch {
      /* the server is restarting */
    }
    if (polling !== target) return
    const w = wait.value!
    let restartAt = w.restartAt
    if (s && typeof s === 'object') {
      setUpd(s)
      if (s.current === target) {
        stopPoll()
        location.reload()
        return
      }
      if (s.job.phase === 'failed') return fail(s.job.error || t('upd.phase.failed'))
      if (s.rolled_back === target) return fail(t('upd.rolledBack', { v: target }))
      if (!restartAt && !['downloading', 'verifying', 'installing'].includes(s.job.phase)) restartAt = Date.now()
    } else if (!restartAt) restartAt = Date.now()
    if (restartAt && Date.now() - restartAt > TIMEOUT_MS) return fail(t('upd.timeout'))
    if (restartAt !== w.restartAt) wait.value = { ...w, restartAt }
    pollTimer = window.setTimeout(tick, restartAt ? 2000 : 800)
  }
  pollTimer = window.setTimeout(tick, 600)
}

// ---------- shared bits ----------

function Notice({ kind = 'warn', testid, children, action }: { kind?: 'warn' | 'danger'; testid: string; children: any; action?: any }) {
  return (
    <div class={'warn' + (kind === 'danger' ? ' danger' : '')} role="status" data-testid={testid}>
      <Icon n="triangle-alert" size={18} />
      <div>
        <p>{children}</p>
        {action && <div class="warn-actions">{action}</div>}
      </div>
    </div>
  )
}

function Commands({ testid }: { testid: string }) {
  const [ok, setOk] = useState(false)
  return (
    <div class="code wrap" data-testid={testid}>
      <pre data-testid={testid + '-text'}>{PULL_CMD}</pre>
      <button
        type="button"
        class="code-copy"
        aria-label={t('upd.copyCmd')}
        title={ok ? t('common.copied') : t('upd.copyCmd')}
        data-testid={testid + '-copy'}
        onClick={() =>
          copyText(PULL_CMD).then((r) => {
            if (!r) return toast(t('err.copy'), 'error')
            setOk(true)
            toast(t('common.copied'), 'success')
            setTimeout(() => setOk(false), 1500)
          })
        }
      >
        <Icon n={ok ? 'check' : 'copy'} size={15} />
      </button>
    </div>
  )
}

async function dismissRolledBack() {
  try {
    setUpd(await post<UpdateStatus>('/api/update/dismiss', { rolled_back: true }))
  } catch (e) {
    toastError(e)
  }
}

// ---------- Settings → About ----------

export function UpdateAbout() {
  const s = upd.value
  const [busy, setBusy] = useState(false)
  useEffect(() => void refreshUpdate(), [])
  const check = async () => {
    setBusy(true)
    try {
      setUpd(await post<UpdateStatus>('/api/update/check'))
    } catch (e) {
      toastError(e)
    }
    setBusy(false)
  }
  if (!s) return null
  const off = s.dev || s.env_disabled
  const checking = busy || s.checking
  const auto = settings.value.update_check !== false
  return (
    <div class="upd-about" data-testid="update-about">
      {s.dev && <Notice testid="update-dev-note">{t('upd.dev')}</Notice>}
      {s.rolled_back && (
        <Notice
          kind="danger"
          testid="update-rolled-back"
          action={
            <button class="btn sm" data-testid="update-rolled-back-dismiss" onClick={dismissRolledBack}>
              {t('upd.dismiss')}
            </button>
          }
        >
          {t('upd.rolledBack', { v: s.rolled_back })}
        </Notice>
      )}
      {!s.dev && (
        <div class="rows">
          <Row label={t('upd.auto')} hint={s.env_disabled ? t('upd.envOff') : t('upd.autoHint')}>
            <Switch label={t('upd.auto')} checked={auto && !s.env_disabled} onChange={(v) => patchSettings({ update_check: v })} testid="set-update-check" />
          </Row>
          <Row label={t('upd.checkNow')} hint={s.checked_at ? t('upd.checkedAt', { when: timeFmt(s.checked_at) }) : t('upd.never')}>
            <button class="btn sm" data-testid="update-check-now" disabled={off || checking} aria-busy={checking} onClick={check}>
              <Icon n="refresh-cw" size={14} />
              {checking ? t('upd.checking') : t('upd.checkNow')}
            </button>
          </Row>
          <Row label={t('upd.latest')} hint={s.available ? t('upd.avail') : t('upd.upToDate')}>
            {s.available ? (
              <button class="btn sm primary" data-testid="update-open-changelog" onClick={() => showOver({ kind: 'update' })}>
                <Icon n="circle-arrow-up" size={14} />
                <span data-testid="update-latest">{s.latest}</span>
              </button>
            ) : (
              <span data-testid="update-uptodate">{s.current}</span>
            )}
          </Row>
        </div>
      )}
      {!s.dev && s.checked_at > 0 && s.error && (
        <p class="form-error" role="alert" data-testid="update-error">
          {t('upd.checkErr', { err: s.error })}
        </p>
      )}
      {!s.dev && !s.available && s.releases.length > 0 && (
        <button class="btn sm" data-testid="update-open-changelog" onClick={() => showOver({ kind: 'update' })}>
          {t('upd.changelog')}
        </button>
      )}
    </div>
  )
}

// ---------- the changelog / update panel ----------

function Release({ r }: { r: UpdateStatus['releases'][number] }) {
  const gh = /^https?:\/\//i.test(r.url || '')
  return (
    <article class="rel" data-testid="update-release" data-version={r.version}>
      <header class="rel-head">
        <h3 data-testid="update-release-version">{r.version}</h3>
        {r.published_at > 0 && <time dateTime={new Date(r.published_at).toISOString()} data-testid="update-release-date">{dateFmt(r.published_at)}</time>}
        {gh && (
          <a class="rel-gh" href={r.url} target="_blank" rel="noopener noreferrer" data-testid="update-release-link">
            {t('upd.onGithub')}
            <Icon n="external-link" size={13} />
          </a>
        )}
      </header>
      <div class="md" data-testid="update-notes">
        {r.notes && r.notes.trim() ? renderMarkdown(r.notes) : <p class="hint">{t('upd.noNotes')}</p>}
      </div>
    </article>
  )
}

export function UpdateModal() {
  const s = upd.value
  const w = wait.value
  const [confirming, setConfirming] = useState(false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => void refreshUpdate(), [])
  // opened (or reloaded) while a job is running on the server: follow it
  const phase = s?.job.phase || 'idle'
  const jobTarget = s?.job.version || s?.latest || ''
  useEffect(() => {
    if (ACTIVE.includes(phase) && jobTarget) pollUpdate(jobTarget)
  }, [phase, jobTarget])

  const title = t('upd.panelTitle')
  if (!s) return <Modal title={title} onClose={closeModal} cls="modal-upd" testid="update-modal"><p class="hint">…</p></Modal>

  const target = s.latest
  const apply = async () => {
    setBusy(true)
    setErr('')
    try {
      setUpd(await post<UpdateStatus>('/api/update/apply', { version: target }))
      setConfirming(false)
      pollUpdate(target)
    } catch (e: any) {
      setErr(t('upd.applyErr', { err: e?.message || t('err.generic') }))
      setConfirming(false)
      void refreshUpdate()
    }
    setBusy(false)
  }
  const skip = async () => {
    setBusy(true)
    try {
      setUpd(await post<UpdateStatus>('/api/update/dismiss', { version: target }))
      toast(t('upd.skipped', { v: target }), 'info')
      closeModal()
    } catch (e) {
      toastError(e)
    }
    setBusy(false)
  }

  // --- progress ---
  if (w || ACTIVE.includes(phase)) {
    const ph = w?.restartAt ? 'restarting' : phase === 'idle' || phase === 'failed' ? 'downloading' : phase
    const failed = w?.error || (phase === 'failed' ? s.job.error || t('upd.phase.failed') : '')
    const pct = ph === 'downloading' ? Math.round(Math.max(0, Math.min(1, s.job.progress || 0)) * 100) : ph === 'restarting' || ph === 'installing' || ph === 'verifying' ? 100 : 0
    const close = () => {
      if (w?.error) (stopPoll(), (wait.value = null))
      closeModal()
    }
    return (
      <Modal
        title={title}
        onClose={close}
        cls="modal-upd"
        testid="update-modal"
        footer={
          failed ? (
            <>
              <button class="btn" data-testid="update-reload" onClick={() => location.reload()}>
                {t('upd.reload')}
              </button>
              <button class="btn primary" data-testid="update-progress-close" onClick={close}>
                {t('common.close')}
              </button>
            </>
          ) : undefined
        }
      >
        <div class="upd-progress" data-testid="update-progress" data-phase={failed ? 'failed' : ph}>
          <h3>{t('upd.updating', { v: w?.target || jobTarget })}</h3>
          {failed ? (
            <p class="form-error" role="alert" data-testid="update-failed">
              {failed}
            </p>
          ) : (
            <>
              <p data-testid="update-phase" aria-live="polite">
                {t('upd.phase.' + ph)}
                {ph === 'downloading' && <span data-testid="update-percent"> {pct}%</span>}
              </p>
              <div class={'bar' + (ph === 'downloading' ? '' : ' busy')} role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct}>
                <i style={{ width: pct + '%' }} />
              </div>
            </>
          )}
        </div>
      </Modal>
    )
  }

  // --- confirm ---
  if (confirming) {
    return (
      <Modal
        title={title}
        onClose={closeModal}
        cls="modal-upd"
        testid="update-modal"
        footer={
          <>
            <button class="btn" data-testid="update-confirm-cancel" disabled={busy} onClick={() => setConfirming(false)}>
              {t('upd.back')}
            </button>
            <button class="btn primary" data-testid="update-confirm" disabled={busy} onClick={apply}>
              {t('upd.confirmOk')}
            </button>
          </>
        }
      >
        <div data-testid="update-confirm-box">
          <h3 class="upd-h">{t('upd.confirmTitle', { v: target })}</h3>
          <p>{t('upd.confirmBody')}</p>
          <p class="hint">{t('upd.confirmRollback', { cur: s.current })}</p>
        </div>
      </Modal>
    )
  }

  // --- list ---
  const can = s.apply.supported
  const reason = s.apply.reason
  return (
    <Modal
      title={title}
      onClose={closeModal}
      cls="modal-upd"
      testid="update-modal"
      footer={
        s.available ? (
          <>
            <button class="btn" data-testid="update-skip" disabled={busy} onClick={skip}>
              {t('upd.skip')}
            </button>
            {can && (
              <button class="btn primary" data-testid="update-apply" disabled={busy} onClick={() => (setErr(''), setConfirming(true))}>
                <Icon n="circle-arrow-up" size={15} />
                {t('upd.now')}
              </button>
            )}
          </>
        ) : undefined
      }
    >
      {s.available && <p class="upd-h2" data-testid="update-summary">{t('upd.from', { cur: s.current, latest: s.latest })}</p>}
      {(err || phase === 'failed') && (
        <p class="form-error" role="alert" data-testid="update-failed">
          {err || s.job.error || t('upd.phase.failed')}
        </p>
      )}
      {s.available && !can && (
        <Notice testid="update-unsupported">
          <strong>{t('upd.unsupported')}</strong>
          <br />
          {t(['disabled', 'platform', 'noexec', 'dev'].includes(reason) ? 'upd.reason.' + reason : 'upd.reason.other')}
          <br />
          {t('upd.pullHint')}
        </Notice>
      )}
      {s.available && !can && <Commands testid="update-commands" />}
      <div class="rels" data-testid="update-releases">
        {s.releases.map((r) => (
          <Release key={r.version} r={r} />
        ))}
        {!s.releases.length && <p class="hint">{t('upd.upToDate')}</p>}
      </div>
    </Modal>
  )
}
