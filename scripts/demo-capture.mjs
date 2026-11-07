// Records a vertical 9:16 demo of a local Genkan instance.
// Example data only. The SSH widget reads a local sshd on 127.0.0.1:2222
// (user nasdemo). The private key is read from DEMO_SSH_KEY and is not printed.
//
//   GENKAN_URL=http://127.0.0.1:8787 GENKAN_DATA=/tmp/genkan-demo-data \
//   GENKAN_PASSWORD=genkan-demo DEMO_SSH_KEY=/tmp/genkan-demo-ssh/nasdemo_ed25519 \
//   node scripts/demo-capture.mjs
//
// Run from anywhere; Playwright is resolved from e2e/node_modules.

import playwright from '../e2e/node_modules/playwright/index.js'

const { chromium } = playwright
import { execFileSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const ORIGIN = (process.env.GENKAN_URL || 'http://127.0.0.1:8787').replace(/\/$/, '')
const DATA = process.env.GENKAN_DATA || '/tmp/genkan-demo-data'
const PASSWORD = process.env.GENKAN_PASSWORD || 'genkan-demo'
const SSH_KEY_PATH = process.env.DEMO_SSH_KEY || '/tmp/genkan-demo-ssh/nasdemo_ed25519'
const OUT = process.env.DEMO_OUT || '/opt/cursor/artifacts'
const SSH_URL = 'https://raw.githubusercontent.com/jae-jae/genkan-plugins/main/plugins/ssh-server.js'

mkdirSync(OUT, { recursive: true })
const token = JSON.parse(readFileSync(join(DATA, 'genkan.json'), 'utf8')).auth.api_token
const sshKey = readFileSync(SSH_KEY_PATH, 'utf8')

async function api(method, path, body) {
  const res = await fetch(ORIGIN + path, {
    method,
    headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let json
  try { json = text ? JSON.parse(text) : {} } catch { json = { raw: text.slice(0, 200) } }
  if (!res.ok) throw new Error(`${method} ${path} -> ${res.status} ${JSON.stringify(json).slice(0, 300)}`)
  return json
}

function sleep(ms) { return new Promise((r) => setTimeout(r, ms)) }

async function shot(page, name) {
  const path = join(OUT, name)
  await page.screenshot({ path, scale: 'device', animations: 'disabled' })
  console.log('shot', name)
}

/** Device-scale JPEG grab (720x1280) encoded to a 1080x1920 mp4. No audio. */
async function record(page, name, action) {
  const cdp = await page.context().newCDPSession(page)
  const frames = []
  let stop = false
  const clip = { x: 0, y: 0, width: 360, height: 640, scale: 2 }
  const loop = (async () => {
    while (!stop) {
      const t = Date.now()
      try {
        const r = await cdp.send('Page.captureScreenshot', {
          format: 'jpeg', quality: 60, clip, captureBeyondViewport: false,
        })
        frames.push({ buf: Buffer.from(r.data, 'base64'), t })
      } catch (err) {
        if (stop) break
        await sleep(30)
      }
    }
  })()
  try {
    await action()
  } finally {
    await sleep(250)
    stop = true
    await loop
  }
  if (frames.length < 8) throw new Error(`${name}: only ${frames.length} frames`)
  const dir = mkdtempSync(join(tmpdir(), 'gkvid-'))
  const lines = ['ffconcat version 1.0']
  for (let i = 0; i < frames.length; i++) {
    const file = `f${String(i).padStart(4, '0')}.jpg`
    writeFileSync(join(dir, file), frames[i].buf)
    const next = frames[i + 1]?.t ?? frames[i].t + 80
    const dur = Math.min(0.25, Math.max(0.03, (next - frames[i].t) / 1000))
    lines.push(`file ${file}`, `duration ${dur.toFixed(4)}`)
  }
  lines.push(`file f${String(frames.length - 1).padStart(4, '0')}.jpg`)
  writeFileSync(join(dir, 'list.txt'), lines.join('\n'))
  const out = join(OUT, name)
  execFileSync('ffmpeg', [
    '-y', '-f', 'concat', '-safe', '0', '-i', join(dir, 'list.txt'),
    '-vf', 'scale=1080:1920:flags=lanczos,fps=30,format=yuv420p',
    '-c:v', 'libx264', '-preset', 'veryfast', '-crf', '18', '-an',
    '-movflags', '+faststart', out,
  ], { stdio: ['ignore', 'ignore', 'inherit'] })
  rmSync(dir, { recursive: true, force: true })
  const probe = execFileSync('ffprobe', [
    '-v', 'error', '-select_streams', 'v:0',
    '-show_entries', 'stream=width,height,duration', '-of', 'csv=p=0', out,
  ], { encoding: 'utf8' }).trim()
  console.log('video', name, probe, 'frames', frames.length)
}

async function scrollBy(page, dy, ms = 900) {
  const steps = 8
  for (let i = 0; i < steps; i++) {
    await page.evaluate((d) => window.scrollBy(0, d), dy / steps)
    await sleep(ms / steps)
  }
}

async function scrubSecrets(page) {
  const last4 = token.slice(-4)
  const leaked = await page.evaluate((full) => document.body.innerText.includes(full), token)
  if (leaked) throw new Error('full API token is visible as text')
  await page.evaluate((last4) => {
    const scrub = (s) => s
      .replace(/gk_[0-9a-z]{8,}/gi, 'gk_••••••••••••')
      .split(last4).join('••••')
    const walk = (n) => {
      if (n.nodeType === 3 && n.nodeValue) n.nodeValue = scrub(n.nodeValue)
      for (const c of n.childNodes) walk(c)
    }
    walk(document.body)
  }, last4)
}

const browser = await chromium.launch()
const context = await browser.newContext({
  viewport: { width: 360, height: 640 },
  deviceScaleFactor: 3,
  isMobile: true,
  hasTouch: true,
  locale: 'zh-CN',
  timezoneId: 'Asia/Shanghai',
})
await context.addInitScript(() => localStorage.setItem('genkan.lang', 'zh'))
const login = await context.request.post(ORIGIN + '/api/auth/login', { data: { password: PASSWORD } })
if (!login.ok()) throw new Error('login failed ' + login.status())
const page = await context.newPage()
await page.goto(ORIGIN + '/', { waitUntil: 'networkidle' })
await page.getByTestId('ws-menu-btn').waitFor()
await sleep(400)

const boot = await api('GET', '/api/bootstrap')
const ws = Object.fromEntries(boot.workspaces.map((w) => [w.name, w.id]))
const cats = boot.categories
const dl = cats.find((c) => c.workspace_id === ws.Media && c.name === '下载')
const link = boot.links.find((l) => l.title === 'Sonarr')

// ---- 01 home: scroll, then switch to Media ----
await record(page, '01-demo-home.mp4', async () => {
  await sleep(700)
  await scrollBy(page, 520, 1600)
  await sleep(400)
  await page.evaluate(() => window.scrollTo(0, 0))
  await sleep(350)
  await page.getByTestId('ws-menu-btn').click()
  await page.getByTestId('ws-menu').waitFor()
  await sleep(500)
  await shot(page, '05-demo-workspaces.png')
  await page.getByTestId('ws-item-' + ws.Media).click()
  await sleep(1100)
})

// ---- 02 search across workspaces ----
await page.evaluate(() => window.scrollTo(0, 0))
await page.getByTestId('ws-menu-btn').click()
await page.getByTestId('ws-item-' + ws.Home).click()
await sleep(300)
await record(page, '02-demo-search.mp4', async () => {
  await sleep(300)
  await page.getByTestId('search-input').click()
  await page.getByTestId('search-input').pressSequentially('jelly', { delay: 140 })
  await page.getByTestId('search-results').waitFor()
  await sleep(1400)
  await shot(page, '02-demo-search.png')
  await page.keyboard.press('Escape')
  await sleep(200)
})

// ---- 03 install SSH Server from the plugin repository URL ----
await record(page, '03-demo-plugin-ssh-install.mp4', async () => {
  await sleep(200)
  await page.getByTestId('plugins-btn').click()
  await page.getByTestId('settings-panel-plugins').waitFor()
  await sleep(400)
  await page.getByTestId('plugin-add-btn').click()
  await page.getByTestId('plugin-add-tab-url').click()
  await page.getByTestId('plugin-url-input').fill(SSH_URL)
  await sleep(350)
  await page.getByTestId('plugin-url-install').click()
  await page.getByTestId('plugin-perm-modal').waitFor()
  await page.getByTestId('plugin-perm-ssh').waitFor()
  await sleep(1200)
  await shot(page, '03-demo-plugin-ssh-permission.png')
  await page.getByTestId('plugin-perm-allow').click()
  await page.locator('[data-testid="plugin-row"][data-plugin-id="ssh-server"][data-enabled="1"]').waitFor()
  await sleep(900)
})

// Prime two real collections while the board is still on Home, so the
// Network card can show a CPU line from actual samples.
const created = await api('POST', '/api/widgets', {
  plugin: 'ssh-server',
  widget: 'resources',
  workspace_id: ws.Network,
  config: {
    name: 'lab-nas',
    host: '127.0.0.1',
    port: 2222,
    username: 'nasdemo',
    privateKey: sshKey,
  },
})
const wid = created.id
for (let i = 0; i < 2; i++) {
  const data = await api('GET', `/api/widgets/${wid}/data?force=1&lang=zh`)
  if (data.view?.error) throw new Error('ssh widget error: ' + data.view.error)
  console.log('ssh sample', i, data.view?.title, (data.view?.body || []).find((b) => b.type === 'text')?.text)
  await sleep(400)
}

await page.getByTestId('modal-close').click()
await sleep(300)
await page.getByTestId('ws-menu-btn').click()
await page.getByTestId('ws-item-' + ws.Network).click()
await page.locator('[data-testid="widget"][data-plugin="ssh-server"]').waitFor()
await page.locator('[data-testid="widget"][data-plugin="ssh-server"]').getByText('在线').waitFor({ timeout: 15000 })

await record(page, '04-demo-plugin-ssh-board.mp4', async () => {
  await sleep(900)
  await shot(page, '04-demo-plugin-ssh-board.png')
  await scrollBy(page, 420, 1800)
  await sleep(800)
})

// ---- API call -> the page updates. Not an AI assistant. ----
await page.getByTestId('ws-menu-btn').click()
await page.getByTestId('ws-item-' + ws.Media).click()
await sleep(400)
const dlEl = page.getByTestId('category-' + dl.id)
await dlEl.scrollIntoViewIfNeeded()
await sleep(300)
await record(page, '06-demo-api-live-update.mp4', async () => {
  await sleep(600)
  const iconPath = join(ROOT, 'e2e/fixtures/icons/bazarr.svg')
  const iconBody = new FormData()
  iconBody.append('file', new Blob([readFileSync(iconPath)], { type: 'image/svg+xml' }), 'bazarr.svg')
  const up = await fetch(ORIGIN + '/api/uploads/icon', { method: 'POST', headers: { Authorization: 'Bearer ' + token }, body: iconBody })
  if (!up.ok) throw new Error('icon upload ' + up.status)
  const icon = (await up.json()).value
  await api('POST', '/api/links', {
    workspace_id: ws.Media,
    category_id: dl.id,
    title: 'Bazarr',
    url: 'http://bazarr.lan:6767',
    description: '字幕',
    icon,
    pinned: false,
    fetch_icon: false,
    health_check: false,
  })
  await page.getByText('Bazarr', { exact: true }).waitFor({ timeout: 8000 })
  await sleep(1200)
  await shot(page, '06-demo-api-live-update.png')
})

// ---- context menu (mobile sheet) ----
await page.evaluate(() => window.scrollTo(0, 0))
await sleep(200)
await page.locator('[data-testid^="link-"][data-link-id="' + link.id + '"]').click({ button: 'right' })
await page.getByTestId('ctx-menu-link').waitFor()
await sleep(300)
await shot(page, '07-demo-context-menu.png')
await page.keyboard.press('Escape')
await sleep(200)

// ---- content manager ----
await page.getByTestId('menu-btn').click()
await page.getByTestId('settings-btn').click()
await page.getByTestId('settings-tab-content').click()
await page.getByTestId('content-manager').waitFor()
await sleep(400)
await shot(page, '08-demo-content-manager.png')

// ---- Settings -> AI, token and prompt scrubbed ----
await page.getByTestId('settings-tab-ai').click()
await page.getByTestId('token-value').waitFor()
await sleep(300)
await scrubSecrets(page)
await shot(page, '09-demo-settings-ai-masked.png')
const still = await page.locator('body').innerText()
if (still.includes(token) || still.includes(token.slice(-4))) {
  throw new Error('settings screenshot still contains token material')
}

await browser.close()
console.log('capture done')
