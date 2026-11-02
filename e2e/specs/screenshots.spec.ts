// Product screenshots -> .tmp/screenshots/*.png (gitignored; run with `npm run screenshots`).
// English UI by default; GENKAN_SHOTS_LANG=zh renders the Chinese UI and data instead. The curated, optimized
// images committed under docs/images/ are composed from these raw captures (see CONTRIBUTING.md).
// Seeds a realistic homelab through the public API, then captures desktop (1440x900) and mobile (390x844)
// at deviceScaleFactor 2. Icons are uploaded from e2e/fixtures/icons and the wallpaper gallery is filled with
// locally rendered artwork (canvas), so everything works offline.
import { test, expect, type Browser, type BrowserContext, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { Api, adminContext, openMainMenu, openSettings, openWsMenu, origin, touchGesture } from '../helpers'
import { CATEGORIES, SERVICES, WORKSPACES, svcUrl } from '../fixtures/homelab'

const here = dirname(fileURLToPath(import.meta.url))
const OUT = resolve(here, '../../.tmp/screenshots')
const ZH = process.env.GENKAN_SHOTS_LANG === 'zh'
const LOCALE = ZH ? 'zh-CN' : 'en-US'
const ICONS = resolve(here, '../fixtures/icons')
mkdirSync(OUT, { recursive: true })

// A believable, fixed moment (Wednesday morning) so clocks look the same on every run.
const NOW = new Date('2026-11-04T09:41:00')

const DESKTOP = { width: 1440, height: 1000 }
const DESKTOP_EDIT = { width: 1440, height: 1180 }
const MOBILE = { width: 390, height: 844 }

test.describe.configure({ mode: 'serial' })
test.use({ deviceScaleFactor: 2, locale: LOCALE })

let api: Api
let wsId: Record<string, string> = {}
let svcId: Record<string, string> = {}
let gallery: string[] = []

const wsKey = (k: string) => 'ws' + k
const catKey = (k: string) => 'c' + k
const linkKey = (k: string) => 'l' + k

test.beforeAll(async () => {
  api = new Api(await adminContext())

  // ---- upload icons once ----
  const iconValue: Record<string, string> = {}
  for (const name of new Set(SERVICES.map((s) => s.icon).filter(Boolean) as string[])) {
    const buffer = readFileSync(resolve(ICONS, `${name}.svg`))
    const r = await api.ctx.post('/api/uploads/icon', { multipart: { file: { name: `${name}.svg`, mimeType: 'image/svg+xml', buffer } } })
    if (!r.ok()) throw new Error(`icon upload ${name}: ${r.status()} ${await r.text()}`)
    iconValue[name] = (await r.json()).value
  }

  // ---- seed through POST /api/import (replace): a whole homelab in one request ----
  const minutes = (m?: number) => (m == null ? null : new Date(NOW.getTime() - m * 60_000).toISOString().replace(/\.\d+Z$/, 'Z'))
  const catsOf = (ws: string) => CATEGORIES.filter((c) => c.ws === ws)
  await api.call('POST', '/api/import', {
    mode: 'replace',
    data: {
      schema: 1,
      settings: { title: 'Genkan', sort_mode: 'frequency', show_clock: true, open_in_new_tab: true, health_interval: 60 },
      workspaces: WORKSPACES.map((w, i) => ({ id: wsKey(w.key), name: ZH ? w.zh : w.en, icon: w.icon, wallpaper: w.wallpaper, sort: i })),
      categories: CATEGORIES.map((c) => ({ id: catKey(c.key), workspace_id: wsKey(c.ws), name: ZH ? c.zh : c.en, sort: catsOf(c.ws).findIndex((x) => x.key === c.key) })),
      links: SERVICES.map((s, i) => ({
        id: linkKey(s.id),
        workspace_id: wsKey(s.ws),
        category_id: s.cat ? catKey(s.cat) : '',
        title: s.title,
        url: svcUrl(s),
        description: ZH ? s.desc_zh : s.desc_en,
        icon: s.icon ? iconValue[s.icon] : '',
        pinned: !!s.pinned,
        sort: i,
        pin_sort: i,
        clicks: s.clicks,
        last_used_at: minutes(s.ago),
        health_check: !!s.health,
      })),
    },
  })
  const b = await api.bootstrap()
  wsId = Object.fromEntries(WORKSPACES.map((w) => [w.key, wsKey(w.key)]))
  svcId = Object.fromEntries(SERVICES.map((s) => [s.id, linkKey(s.id)]))
  expect(b.links.length).toBe(SERVICES.length)
})

test('prepare: wallpaper gallery', async ({ browser }) => {
  gallery = await uploadWallpapers(browser)
  expect(gallery).toHaveLength(4)
})


/** Four wallpapers (2560x1440 JPEG) rendered on a canvas: no stock photos, fully reproducible. */
async function renderWallpapers(browser: Browser): Promise<Buffer[]> {
  const ctx = await browser.newContext({ viewport: { width: 800, height: 450 }, deviceScaleFactor: 1 })
  const page = await ctx.newPage()
  await page.setContent('<canvas id="c" width="2560" height="1440"></canvas>')
  const b64 = await page.evaluate(() => {
    const W = 2560
    const H = 1440
    const cv = document.getElementById('c') as HTMLCanvasElement
    const g = cv.getContext('2d')!
    const rng = (seed: number) => () => {
      seed |= 0
      seed = (seed + 0x6d2b79f5) | 0
      let t = Math.imul(seed ^ (seed >>> 15), 1 | seed)
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296
    }
    const lin = (x0: number, y0: number, x1: number, y1: number, stops: [number, string][]) => {
      const gr = g.createLinearGradient(x0, y0, x1, y1)
      for (const [o, c] of stops) gr.addColorStop(o, c)
      return gr
    }
    const glow = (x: number, y: number, r: number, c: string) => {
      const gr = g.createRadialGradient(x, y, 0, x, y, r)
      gr.addColorStop(0, c)
      gr.addColorStop(1, 'rgba(0,0,0,0)')
      g.fillStyle = gr
      g.fillRect(0, 0, W, H)
    }
    const stars = (seed: number, n: number, maxY: number) => {
      const r = rng(seed)
      for (let i = 0; i < n; i++) {
        const y = r() * maxY
        g.fillStyle = `rgba(255,255,255,${(0.25 + r() * 0.6) * (1 - y / maxY)})`
        g.beginPath()
        g.arc(r() * W, y, 0.6 + r() * 1.6, 0, 7)
        g.fill()
      }
    }
    /** smooth ridge line: sum of sines, filled down to the bottom */
    const ridge = (base: number, amp: number, seed: number, fill: string | CanvasGradient, rough = 1) => {
      const r = rng(seed)
      const ph = [r() * 6, r() * 6, r() * 6, r() * 6]
      g.beginPath()
      g.moveTo(0, H)
      for (let x = 0; x <= W; x += 8) {
        const y = base + amp * (Math.sin(x / 620 + ph[0]) * 0.55 + Math.sin(x / 260 + ph[1]) * 0.3 * rough + Math.sin(x / 110 + ph[2]) * 0.12 * rough + Math.sin(x / 47 + ph[3]) * 0.04 * rough)
        g.lineTo(x, y)
      }
      g.lineTo(W, H)
      g.closePath()
      g.fillStyle = fill
      g.fill()
    }
    const out: string[] = []
    const grab = () => out.push(cv.toDataURL('image/jpeg', 0.86).split(',')[1])

    // 1 --- dusk ridges: violet sky, low sun, layered mountains in haze
    g.fillStyle = lin(0, 0, 0, H, [[0, '#14123d'], [0.35, '#4a2a7d'], [0.62, '#c0507f'], [0.8, '#ff9a6b'], [1, '#ffd29a']])
    g.fillRect(0, 0, W, H)
    stars(11, 260, H * 0.4)
    glow(1750, 860, 700, 'rgba(255,214,150,0.85)')
    g.fillStyle = '#ffe6bd'
    g.beginPath()
    g.arc(1750, 860, 96, 0, 7)
    g.fill()
    const cols1 = ['#7a4a8f', '#5b3a82', '#43307a', '#2c2565', '#1b1848', '#0f0d2c']
    cols1.forEach((c, i) => {
      ridge(820 + i * 120, 150 - i * 12, 40 + i * 7, c, 1 + i * 0.15)
      if (i < 4) {
        g.fillStyle = lin(0, 820 + i * 120, 0, 1080 + i * 120, [[0, 'rgba(255,170,140,0.22)'], [1, 'rgba(255,170,140,0)']])
        g.fillRect(0, 820 + i * 120 - 80, W, 360)
      }
    })
    grab()

    // 2 --- mesh gradient: soft overlapping colour blooms
    g.fillStyle = '#0b0f33'
    g.fillRect(0, 0, W, H)
    g.globalCompositeOperation = 'screen'
    g.filter = 'blur(120px)'
    const blobs: [number, number, number, string][] = [
      [380, 300, 720, '#7a3bff'], [1500, 220, 640, '#19b5fe'], [2300, 520, 760, '#ff4fa3'], [900, 1100, 820, '#2b6bff'],
      [1900, 1250, 700, '#ff9a5a'], [2450, 1300, 520, '#7a3bff'], [200, 1250, 520, '#00d4b4'],
    ]
    for (const [x, y, r, c] of blobs) {
      g.fillStyle = c
      g.globalAlpha = 0.78
      g.beginPath()
      g.arc(x, y, r, 0, 7)
      g.fill()
    }
    g.filter = 'none'
    g.globalAlpha = 1
    g.globalCompositeOperation = 'source-over'
    g.fillStyle = 'rgba(8,10,40,0.18)'
    g.fillRect(0, 0, W, H)
    grab()

    // 3 --- teal dunes / sea: pale sky and rolling layers
    g.fillStyle = lin(0, 0, 0, H, [[0, '#1b5a7a'], [0.45, '#4fb3b0'], [0.72, '#f3e3b8'], [1, '#f6c98e']])
    g.fillRect(0, 0, W, H)
    glow(640, 760, 780, 'rgba(255,245,215,0.9)')
    g.fillStyle = 'rgba(255,248,225,0.95)'
    g.beginPath()
    g.arc(640, 760, 84, 0, 7)
    g.fill()
    const cols3 = ['#7fd0c4', '#47b0ae', '#2b8c9a', '#1d6a84', '#134b6b', '#0b3050']
    cols3.forEach((c, i) => ridge(880 + i * 105, 90 - i * 6, 90 + i * 5, c, 0.5))
    g.fillStyle = lin(0, 880, 0, H, [[0, 'rgba(255,255,255,0.08)'], [1, 'rgba(0,10,30,0.25)']])
    g.fillRect(0, 880, W, H - 880)
    grab()

    // 4 --- aurora night over a pine ridge
    g.fillStyle = lin(0, 0, 0, H, [[0, '#050818'], [0.55, '#0b1c3a'], [1, '#12354a']])
    g.fillRect(0, 0, W, H)
    stars(77, 520, H * 0.75)
    g.globalCompositeOperation = 'screen'
    g.filter = 'blur(46px)'
    const bands: [string, number, number][] = [['rgba(60,255,170,0.75)', 470, 150], ['rgba(40,200,255,0.55)', 560, 120], ['rgba(150,90,255,0.5)', 380, 110]]
    bands.forEach(([c, y0, th], k) => {
      g.strokeStyle = c
      g.lineWidth = th
      g.lineCap = 'round'
      g.beginPath()
      for (let x = -100; x <= W + 100; x += 20) {
        const y = y0 + Math.sin(x / 430 + k * 1.7) * 130 + Math.sin(x / 170 + k) * 34
        x === -100 ? g.moveTo(x, y) : g.lineTo(x, y)
      }
      g.stroke()
    })
    g.filter = 'none'
    g.globalCompositeOperation = 'source-over'
    ridge(1130, 90, 5, '#0b2230', 0.6)
    ridge(1230, 70, 9, '#071722', 0.5)
    const r4 = rng(3)
    g.fillStyle = '#040d15'
    for (let x = -20; x < W + 40; x += 26 + r4() * 34) {
      const h = 90 + r4() * 190
      const base = 1290 + r4() * 40
      g.beginPath()
      g.moveTo(x, base)
      g.lineTo(x + 24 + h * 0.08, base - h)
      g.lineTo(x + 48 + h * 0.16, base)
      g.closePath()
      g.fill()
    }
    g.fillRect(0, 1300, W, H - 1300)
    grab()
    return out
  })
  await ctx.close()
  return b64.map((x) => Buffer.from(x, 'base64'))
}

async function uploadWallpapers(browser: Browser): Promise<string[]> {
  for (const f of await api.call<{ value: string }[]>('GET', '/api/wallpapers')) await api.call('DELETE', `/api/wallpapers/${f.value.slice(7)}`)
  const values: string[] = []
  for (const [i, buffer] of (await renderWallpapers(browser)).entries()) {
    const r = await api.ctx.post('/api/uploads/wallpaper', { multipart: { file: { name: `wallpaper-${i + 1}.jpg`, mimeType: 'image/jpeg', buffer } } })
    if (!r.ok()) throw new Error(`wallpaper upload: ${r.status()} ${await r.text()}`)
    values.push((await r.json()).value)
  }
  return values
}

async function newPage(browser: Browser, opts: { viewport: { width: number; height: number }; locale?: string; mobile?: boolean; anonymous?: boolean; ws?: string }) {
  const ctx = await browser.newContext({
    baseURL: origin(),
    viewport: opts.viewport,
    deviceScaleFactor: 2,
    locale: opts.locale ?? LOCALE,
    isMobile: opts.mobile,
    hasTouch: opts.mobile,
    colorScheme: 'dark',
  })
  if (!opts.anonymous) await ctx.addCookies((await api.ctx.storageState()).cookies)
  await ctx.clock.setFixedTime(NOW)
  const page = await ctx.newPage()
  await page.addInitScript((ws) => {
    try {
      if (ws) localStorage.setItem('genkan.ws', ws)
    } catch {}
  }, opts.ws ?? wsKey('home'))
  return { ctx, page }
}

/** Wait until fonts, icons, wallpaper cross-fade, health dots and CSS animations have settled. */
async function settle(page: Page) {
  await page.evaluate(() => document.fonts.ready)
  await expect(page.locator('.wp-layer')).toHaveCount(1)
  await expect.poll(() => page.evaluate(() => [...document.images].every((i) => i.complete && i.naturalWidth > 0)), { timeout: 15_000 }).toBe(true)
  await expect(page.locator('[data-status="unknown"]')).toHaveCount(0, { timeout: 25_000 })
  await page.evaluate(() => Promise.all(document.getAnimations().filter((a) => (a.effect?.getComputedTiming().iterations ?? 1) !== Infinity).map((a) => a.finished.catch(() => {}))))
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))))
}

async function openHome(page: Page) {
  await page.goto('/')
  await expect(page.getByTestId('ws-menu-btn')).toBeVisible()
  await expect(page.locator('[data-testid^="link-"]').first()).toBeVisible()
}

async function shot(page: Page, name: string) {
  await page.mouse.move(0, 0)
  await settle(page)
  await page.screenshot({ path: resolve(OUT, `${name}.png`), animations: 'disabled', caret: 'hide' })
}

test('desktop: home', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP })
  await openHome(page)
  await shot(page, 'desktop-home')
  await ctx.close()
})

test('desktop: search with results', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP })
  await openHome(page)
  await page.keyboard.press('/')
  await page.keyboard.type('arr')
  await expect(page.getByTestId('search-result').first()).toBeVisible()
  await shot(page, 'desktop-search')
  await ctx.close()
})

test('desktop: edit mode', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP_EDIT })
  await openHome(page)
  await page.getByTestId('edit-toggle').click()
  await expect(page.getByTestId('board')).toHaveAttribute('data-editing', '1')
  await shot(page, 'desktop-edit-mode')
  await ctx.close()
})

test('desktop: link editor modal', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP })
  await openHome(page)
  await page.getByTestId('edit-toggle').click()
  await page.getByTestId(`link-edit-${svcId.frigate}`).click()
  await expect(page.getByTestId('link-modal')).toBeVisible()
  await shot(page, 'desktop-link-editor')
  await ctx.close()
})

test('desktop: workspace dropdown open (with New workspace / Manage)', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP })
  await openHome(page)
  await openWsMenu(page)
  await expect(page.getByTestId('ws-menu')).toBeVisible()
  await expect(page.getByTestId('ws-add')).toBeVisible()
  await shot(page, 'desktop-workspace-menu')
  await ctx.close()
})

test('desktop: main menu open', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP })
  await openHome(page)
  await openMainMenu(page)
  await expect(page.getByTestId('main-menu')).toBeVisible()
  await shot(page, 'desktop-main-menu')
  await ctx.close()
})

test('desktop: page context menu', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP })
  await openHome(page)
  // an empty stretch of the page, right of the capsules
  await page.mouse.click(930, 450, { button: 'right' })
  await expect(page.getByTestId('ctx-menu-page')).toBeVisible()
  await shot(page, 'desktop-context-menu-page')
  await ctx.close()
})

test('desktop: capsule context menu', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP })
  await openHome(page)
  await page.getByTestId(`link-${svcId.pve}`).click({ button: 'right', position: { x: 40, y: 20 } })
  await expect(page.getByTestId('ctx-menu-link')).toBeVisible()
  await shot(page, 'desktop-context-menu-link')
  await ctx.close()
})

test('desktop: settings, AI tab (token stays masked)', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP })
  await openHome(page)
  await page.getByTestId('ai-btn').click()
  await expect(page.getByTestId('settings-panel-ai')).toBeVisible()
  await expect(page.getByTestId('token-value')).toHaveAttribute('data-masked', '1')
  await shot(page, 'desktop-settings-ai')
  await page.getByTestId('connect-claude').click()
  await expect(page.getByTestId('mcp-command')).toBeVisible()
  await shot(page, 'desktop-settings-ai-claude')
  await ctx.close()
})

test('desktop: settings, appearance (wallpaper gallery)', async ({ browser }) => {
  await api.call('PATCH', `/api/workspaces/${wsKey('home')}`, { wallpaper: gallery[0] })
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP })
  await openHome(page)
  await openSettings(page, 'appearance')
  await expect(page.locator('[data-testid^="gallery-item-"]')).toHaveCount(4)
  await expect(page.locator('[data-testid^="gallery-item-"][aria-pressed="true"]')).toHaveCount(1)
  await expect.poll(() => page.evaluate(() => [...document.images].every((i) => i.complete && i.naturalWidth > 0))).toBe(true)
  await shot(page, 'desktop-settings-appearance')
  await ctx.close()
  await api.call('PATCH', `/api/workspaces/${wsKey('home')}`, { wallpaper: 'preset:dusk' })
})

test('desktop: settings, content manager', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP })
  await openHome(page)
  await openSettings(page, 'content')
  await expect(page.getByTestId('content-manager')).toBeVisible()
  await shot(page, 'desktop-settings-content')
  await ctx.close()
})

test('desktop: workspace editor (icon picker)', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP })
  await openHome(page)
  await page.getByTestId('edit-toggle').click()
  await openWsMenu(page)
  await page.getByTestId(`ws-edit-${wsKey('media')}`).click()
  await expect(page.getByTestId('workspace-modal')).toBeVisible()
  await expect(page.getByTestId('ws-icon')).toHaveAttribute('data-value', 'lucide:clapperboard')
  await shot(page, 'desktop-workspace-editor')
  await ctx.close()
})

test('desktop: login page', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: DESKTOP, anonymous: true })
  await page.goto('/')
  await expect(page.getByTestId('login-form')).toBeVisible()
  await page.getByTestId('login-password').fill('correct horse battery')
  await shot(page, 'desktop-login')
  await ctx.close()
})

test('mobile: home', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: MOBILE, mobile: true })
  await openHome(page)
  await shot(page, 'mobile-home')
  await ctx.close()
})

test('mobile: search', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: MOBILE, mobile: true })
  await openHome(page)
  await page.getByTestId('search-input').fill('arr')
  await expect(page.getByTestId('search-result').first()).toBeVisible()
  await shot(page, 'mobile-search')
  await ctx.close()
})

test('mobile: capsule long-press sheet', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: MOBILE, mobile: true })
  await openHome(page)
  const b = (await page.getByTestId(`link-${svcId.pve}`).boundingBox())!
  await touchGesture(page, b.x + b.width / 2, b.y + b.height / 2, { hold: 700 })
  await expect(page.getByTestId('ctx-menu-link')).toBeVisible()
  await shot(page, 'mobile-link-sheet')
  await ctx.close()
})

test('mobile: settings, content manager', async ({ browser }) => {
  const { ctx, page } = await newPage(browser, { viewport: MOBILE, mobile: true })
  await openHome(page)
  await openSettings(page, 'content')
  await expect(page.getByTestId('content-manager')).toBeVisible()
  await shot(page, 'mobile-settings-content')
  await ctx.close()
})
