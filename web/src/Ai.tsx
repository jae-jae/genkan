import { useEffect, useState } from 'preact/hooks'
import { get, post } from './api'
import { Icon } from './icons'
import { t } from './i18n'
import { serverVersion, toast, toastError } from './store'
import { copyText } from './util'

function CopyBtn({ text, testid, cls }: { text: string; testid: string; cls: string }) {
  const [ok, setOk] = useState(false)
  return (
    <button
      type="button"
      class={cls}
      data-testid={testid}
      aria-label={t('common.copy')}
      title={ok ? t('common.copied') : t('common.copy')}
      onClick={() =>
        copyText(text).then((r) => {
          if (!r) return toast(t('err.copy'), 'error')
          setOk(true)
          setTimeout(() => setOk(false), 1500)
        })
      }
    >
      <Icon n={ok ? 'check' : 'copy'} size={15} />
    </button>
  )
}

/** `text` is what is displayed (possibly masked); `copy` is what the button copies. */
function CodeBox({ text, copy, testid, copyId, wrap }: { text: string; copy: string; testid: string; copyId: string; wrap?: boolean }) {
  return (
    <div class={wrap ? 'code wrap' : 'code'}>
      <pre data-testid={testid} tabIndex={0}>
        {text}
      </pre>
      <CopyBtn text={copy} testid={copyId} cls="code-copy" />
    </div>
  )
}

const maskToken = (tok: string) => (tok ? tok.slice(0, 3) + '••••••••…' + tok.slice(-4) : '')

type Pane = 'skill' | 'mcp'
const PANES: Pane[] = ['skill', 'mcp']

/** Git ref the Skill is fetched from: the release tag for release builds (0.2.0 / v0.2.0), otherwise main. */
export function skillRef(v: string): string {
  const m = /^v?(\d+\.\d+\.\d+)$/.exec(v.trim())
  return m ? 'v' + m[1] : 'main'
}

/** The setup prompt the user pastes into their AI assistant. */
export function skillPrompt(origin: string, tok: string, ref: string): string {
  return [
    'Install the Genkan Skill and connect it to my Genkan instance.',
    '',
    `1. Download SKILL.md and reference.md from https://raw.githubusercontent.com/jae-jae/genkan/${ref}/skills/genkan/ into your skills directory, in a folder named genkan/ (Claude Code: ~/.claude/skills/genkan/; other agents: their own skills directory). Overwrite existing files.`,
    '2. Create ~/.config/genkan/env (make the directory if needed) with exactly these two lines, then run chmod 600 on the file:',
    `GENKAN_URL=${origin}`,
    `GENKAN_TOKEN=${tok}`,
    '3. Verify: call GET $GENKAN_URL/api/bootstrap with the header "Authorization: Bearer $GENKAN_TOKEN" and tell me how many workspaces and links there are. Never print the token.',
  ].join('\n')
}

export function AiTab() {
  const [token, setToken] = useState('')
  const [shown, setShown] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  const [pane, setPane] = useState<Pane>('skill')
  useEffect(() => void get<{ token: string }>('/api/token').then((r) => setToken(r.token), toastError), [])
  const regenerate = async () => {
    setBusy(true)
    try {
      const r = await post<{ token: string }>('/api/token/regenerate')
      setToken(r.token)
      setConfirm(false)
      toast(t('mcp.regenDone'), 'success')
    } catch (er) {
      toastError(er)
    }
    setBusy(false)
  }
  const origin = location.origin
  const ref = skillRef(serverVersion.value)
  const build = (tok: string) => ({
    prompt: skillPrompt(origin, tok, ref),
    cmd: `claude mcp add --transport http genkan ${origin}/mcp --header "Authorization: Bearer ${tok}"`,
    json: JSON.stringify({ mcpServers: { genkan: { type: 'http', url: origin + '/mcp', headers: { Authorization: 'Bearer ' + tok } } } }, null, 2),
  })
  const real = build(token || '…')
  const shownTok = shown ? token : maskToken(token)
  const disp = shown ? real : build(shownTok || '…')
  const onKey = (e: KeyboardEvent) => {
    const d = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0
    if (!d) return
    e.preventDefault()
    const n = PANES[(PANES.indexOf(pane) + d + PANES.length) % PANES.length]
    setPane(n)
    requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-testid="connect-${n}"]`)?.focus())
  }
  return (
    <div class="ai">
      <p class="ai-intro">{t('ai.intro')}</p>

      <section class="card" data-testid="token-card" aria-labelledby="ai-token-h">
        <h3 class="card-h" id="ai-token-h">
          <Icon n="key-round" size={16} />
          {t('mcp.token')}
        </h3>
        <div class="tokrow">
          <code class="tokval" data-testid="token-value" data-masked={shown ? '0' : '1'}>
            {shownTok || '…'}
          </code>
          <button
            class="btn sm sq"
            aria-label={shown ? t('mcp.hide') : t('mcp.show')}
            title={shown ? t('mcp.hide') : t('mcp.show')}
            aria-pressed={shown}
            data-testid="token-toggle"
            onClick={() => setShown(!shown)}
          >
            <Icon n={shown ? 'eye-off' : 'eye'} size={15} />
          </button>
          <CopyBtn text={token} testid="token-copy" cls="btn sm sq" />
          <button class="btn sm sq" aria-label={t('mcp.regen')} title={t('mcp.regen')} data-testid="token-regenerate" aria-expanded={confirm} disabled={!token || confirm} onClick={() => setConfirm(true)}>
            <Icon n="refresh-cw" size={15} />
          </button>
        </div>
        {confirm && (
          <div class="regen" role="alertdialog" aria-label={t('mcp.regen')} data-testid="token-confirm">
            <p>{t('mcp.regenWarn')}</p>
            <div class="regen-actions">
              <button class="btn sm danger-solid" data-testid="token-regenerate-confirm" disabled={busy} onClick={regenerate}>
                {t('mcp.regenConfirm')}
              </button>
              <button class="btn sm" data-testid="token-regenerate-cancel" onClick={() => setConfirm(false)}>
                {t('common.cancel')}
              </button>
            </div>
          </div>
        )}
        <p class="warn-line" role="note" data-testid="mcp-warning">
          <Icon n="triangle-alert" size={15} />
          <span>{t('ai.warn')}</span>
        </p>
      </section>

      <h3 class="sub">{t('ai.connect')}</h3>
      <div class="seg seg-wide glass-lite" role="tablist" aria-label={t('ai.connect')} data-testid="connect-tabs" onKeyDown={onKey}>
        {PANES.map((p) => (
          <button key={p} class="seg-b" role="tab" id={'connect-tab-' + p} aria-selected={pane === p} aria-controls={'connect-pane-' + p} tabIndex={pane === p ? 0 : -1} data-testid={'connect-' + p} onClick={() => setPane(p)}>
            {t('ai.' + p)}
          </button>
        ))}
      </div>
      <div class="pane" role="tabpanel" id={'connect-pane-' + pane} aria-labelledby={'connect-tab-' + pane} data-testid={'pane-' + pane} key={pane}>
        {pane === 'skill' && (
          <>
            <p class="hint first">{t('ai.skillHint')}</p>
            <CodeBox text={disp.prompt} copy={real.prompt} testid="skill-prompt" copyId="copy-skill-prompt" wrap />
            <p class="manual" data-testid="skill-manual">
              <span>{t('ai.manual')}</span>
              <a class="btn sm" href="/api/skill" download="genkan-skill.zip" data-testid="skill-download">
                <Icon n="download" size={14} />
                {t('ai.manualBtn')}
              </a>
              <span>{t('ai.manualTail')}</span>
            </p>
          </>
        )}
        {pane === 'mcp' && (
          <>
            <p class="hint first">{t('ai.mcpHint')}</p>
            <h4 class="mini">{t('ai.mcpClaude')}</h4>
            <CodeBox text={disp.cmd} copy={real.cmd} testid="mcp-command" copyId="copy-mcp-command" />
            <h4 class="mini">{t('ai.mcpOther')}</h4>
            <CodeBox text={disp.json} copy={real.json} testid="mcp-json" copyId="copy-mcp-json" />
          </>
        )}
      </div>
    </div>
  )
}
