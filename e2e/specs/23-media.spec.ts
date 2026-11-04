import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { Api, expect, openApp, origin, test } from '../helpers'

// Media plugin and the iframe / html / media / slideshow blocks (docs/PLUGINS.md §14). No external network: pages, images
// and audio come from a tiny server inside the test process on another port of 127.0.0.1, which is a different origin
// from Genkan's. (The Genkan container does not need to reach it: the browser loads all of it directly.)

let fx: Server
let fxOrigin = ''

/** 2 seconds of silence, 8 kHz 8-bit mono PCM WAV. */
function wav(): Buffer {
  const n = 16000
  const b = Buffer.alloc(44 + n, 0x80)
  b.write('RIFF', 0)
  b.writeUInt32LE(36 + n, 4)
  b.write('WAVEfmt ', 8)
  b.writeUInt32LE(16, 16)
  b.writeUInt16LE(1, 20)
  b.writeUInt16LE(1, 22)
  b.writeUInt32LE(8000, 24)
  b.writeUInt32LE(8000, 28)
  b.writeUInt16LE(1, 32)
  b.writeUInt16LE(8, 34)
  b.write('data', 36)
  b.writeUInt32LE(n, 40)
  return b
}
const svg = (c: string) => `<svg xmlns="http://www.w3.org/2000/svg" width="160" height="90"><rect width="160" height="90" fill="${c}"/></svg>`

test.beforeAll(async () => {
  const audio = wav()
  fx = createServer((req, res) => {
    const p = (req.url || '').split('?')[0]
    const send = (type: string, body: string | Buffer) => {
      res.writeHead(200, { 'Content-Type': type, 'Content-Length': Buffer.byteLength(body) })
      res.end(body)
    }
    if (p === '/page') return send('text/html; charset=utf-8', '<!doctype html><body style="margin:0;background:#fff;color:#123"><h1 id="t">Framed page</h1><p>Content far below</p></body>')
    if (p === '/tone.wav') return send('audio/wav', audio)
    const m = /^\/(\d)\.svg$/.exec(p)
    if (m) return send('image/svg+xml', svg(['#e33', '#3a3', '#33e'][+m[1] - 1] || '#999'))
    res.writeHead(404).end()
  })
  await new Promise<void>((r) => fx.listen(0, '127.0.0.1', r))
  fxOrigin = `http://127.0.0.1:${(fx.address() as AddressInfo).port}`
})
test.afterAll(async ({ admin }) => {
  await new Promise<void>((r) => fx.close(() => r()))
  await new Api(admin).reset()
})

interface W { id: string }
const install = (api: Api, id: string) => api.call('POST', '/api/plugins', { catalog: id, enable: true })
const addWidget = (api: Api, ws: string, widget: string, config: Record<string, unknown>) =>
  api.call<W>('POST', '/api/widgets', { plugin: 'media', widget, workspace_id: ws, config })
const card = (page: import('@playwright/test').Page, id: string) => page.locator(`[data-testid="widget"][data-widget-id="${id}"]`)

const XSS = [
  '<p>Hello <b>bold</b> <a href="https://ok.example/page" onclick="window.__xss=1">fine</a></p>',
  '<img src=x onerror="window.__xss=2">',
  '<script>window.__xss=3</script>',
  '<a href="javascript:window.__xss=4">js link</a>',
  '<a href="java&#x09;script:window.__xss=5">entity link</a>',
  '<a href="data:text/html,<script>window.__xss=6</script>">data link</a>',
  '<svg onload="window.__xss=7"><circle r="1"/></svg>',
  '<iframe src="https://evil.example/"></iframe>',
  '<style>body{display:none}</style>',
  '<p style="position:fixed;inset:0" class="x" id="y">styled</p>',
  '<img src="https://ok.example/i.png" srcset="https://evil.example/x.png 2x" alt="pic">',
  '<a href="/relative">relative link</a>',
].join('\n')

test.describe('media plugin and blocks', () => {
  test('page widget: sandboxed cross-origin iframe, crop, and a same-origin src is refused', async ({ page, api, ws }) => {
    await install(api, 'media')
    const w = await addWidget(api, ws, 'page', { url: `${fxOrigin}/page`, height: 260 })
    await openApp(page)
    const c = card(page, w.id)
    const frame = c.getByTestId('widget-iframe')
    await expect(frame).toHaveCount(1)
    await expect(frame).toHaveAttribute('sandbox', 'allow-scripts allow-same-origin allow-forms allow-popups allow-popups-to-escape-sandbox')
    await expect(frame).toHaveAttribute('referrerpolicy', 'no-referrer')
    await expect(frame).toHaveAttribute('loading', 'lazy')
    await expect(frame).toHaveAttribute('allow', 'fullscreen; autoplay; picture-in-picture')
    await expect(frame).toHaveAttribute('src', `${fxOrigin}/page`)
    // the browser really loads it, and it is another origin than Genkan
    await expect(c.frameLocator('iframe').locator('#t')).toHaveText('Framed page')
    expect(new URL(fxOrigin).origin).not.toBe(new URL(origin()).origin)
    expect((await c.locator('.wg-frame').boundingBox())!.height).toBeCloseTo(260, 0)
    // the "may refuse to be embedded" hint is a plain link to open the page in a new tab
    const open = c.getByTestId('widget-iframe-open')
    await expect(open).toHaveAttribute('href', `${fxOrigin}/page`)
    await expect(open).toHaveAttribute('target', '_blank')
    await expect(open).toHaveAttribute('rel', /noopener/)

    // crop: the frame wrapper takes the height of the region and the frame is shifted
    await api.call('PATCH', `/api/widgets/${w.id}`, { config: { url: `${fxOrigin}/page`, cropX: 0, cropY: 10, cropW: 300, cropH: 120 } })
    await page.reload()
    await expect(c.locator('.wg-frame')).toBeVisible()
    await expect.poll(async () => Math.round((await c.locator('.wg-frame').boundingBox())!.height)).toBe(120)
    expect(await c.getByTestId('widget-iframe').evaluate((e: HTMLElement) => e.style.transform)).toMatch(/translate\(0px, -10px\) scale\(1\)/)

    // same origin as Genkan: never rendered as a frame (it would run with the admin session)
    await api.call('PATCH', `/api/widgets/${w.id}`, { config: { url: `${origin()}/healthz` } })
    await page.reload()
    await expect(c.getByTestId('widget-iframe-blocked')).toBeVisible()
    await expect(c.locator('iframe')).toHaveCount(0)
    await expect(c.getByTestId('widget-iframe-blocked')).toContainText(/Genkan itself/)
  })

  test('html block: server and browser both strip scripts, handlers, javascript:/data: URLs, svg, iframes and styles', async ({ page, api, ws }) => {
    const src = `export const manifest = { id: "e2e-html", name: "E2E html", version: "1.0.0", widgets: [{ id: "w", name: "W", size: "m" }] };
export async function widget() { return { title: "x", body: [{ type: "html", base: "https://base.example/dir/", html: ${JSON.stringify(XSS)} }] }; }`
    await api.call('POST', '/api/plugins', { source: src, enable: true })
    const w = await api.call<W>('POST', '/api/widgets', { plugin: 'e2e-html', widget: 'w', workspace_id: ws, config: {} })
    const body = await api.call<any>('GET', `/api/widgets/${w.id}/data`)
    const served: string = body.view.body[0].html
    expect(served).toContain('<b>bold</b>')
    expect(served).toContain('href="https://base.example/relative"') // relative link resolved against base
    for (const bad of ['<script', '<svg', '<iframe', '<style', 'onerror', 'onclick', 'javascript:', 'data:', 'srcset', 'style=', 'class=', 'id=']) expect(served.toLowerCase()).not.toContain(bad)

    await openApp(page)
    const h = card(page, w.id).getByTestId('widget-html')
    await expect(h.locator('b')).toHaveText('bold')
    await expectSafeHtml(page, h)
    const a = h.locator('a', { hasText: 'fine' })
    await expect(a).toHaveAttribute('href', 'https://ok.example/page')
    await expect(a).toHaveAttribute('target', '_blank')
    await expect(a).toHaveAttribute('rel', 'noopener noreferrer')
    await expect(h.locator('a', { hasText: 'relative link' })).toHaveAttribute('href', 'https://base.example/relative')
    await expect(h.locator('a', { hasText: /js link|entity link|data link/ })).toHaveCount(0) // unusable links are plain text
    await expect(h).toContainText('js link')
  })

  test('html block: the frontend re-sanitizes even when the server response is raw', async ({ page, api, ws }) => {
    await install(api, 'media')
    const w = await addWidget(api, ws, 'page', { url: `${fxOrigin}/page` })
    await page.route(`**/api/widgets/${w.id}/data*`, (route) =>
      route.fulfill({ json: { view: { title: 'raw', body: [{ type: 'html', html: XSS + '<div>' + '<div>'.repeat(30) + 'deep text' + '</div>'.repeat(31) }] }, updated_at: Date.now(), next_at: Date.now() + 600_000 } }),
    )
    await openApp(page)
    const h = card(page, w.id).getByTestId('widget-html')
    await expect(h.locator('b')).toHaveText('bold')
    await expectSafeHtml(page, h)
    await expect(h).toContainText('deep text')
    // nesting is capped at 20 levels
    expect(await h.evaluate((el) => { let d = 0; el.querySelectorAll('div').forEach((n) => { let k = 0; for (let p: Element | null = n; p && p !== el; p = p.parentElement) k++; d = Math.max(d, k) }); return d })).toBeLessThanOrEqual(20)
  })

  test('image widget: slideshow advances on its own and with the dots', async ({ page, api, ws }) => {
    await install(api, 'media')
    const w = await addWidget(api, ws, 'image', { urls: [1, 2, 3].map((n) => `${fxOrigin}/${n}.svg`).join('\n'), interval: 3, fit: 'contain', height: 180, link: 'https://example.com/gallery' })
    await openApp(page)
    const s = card(page, w.id).getByTestId('widget-slideshow')
    await expect(s).toHaveAttribute('data-count', '3')
    await expect(s).toHaveAttribute('data-index', '0')
    await expect(s).toHaveAttribute('data-fit', 'contain')
    expect((await s.boundingBox())!.height).toBeCloseTo(180, 0)
    await expect(s.locator('[data-testid=widget-slide][data-active] img')).toHaveAttribute('src', `${fxOrigin}/1.svg`)
    await expect(s.locator('[data-testid=widget-slide][data-active] a')).toHaveAttribute('href', 'https://example.com/gallery')
    // advances without any interaction (interval 3 s)
    await expect(s).toHaveAttribute('data-index', '1', { timeout: 7_000 })
    await expect(s.locator('[data-testid=widget-slide][data-active] img')).toHaveAttribute('src', `${fxOrigin}/2.svg`)
    // dots jump to a slide and restart the timer
    await s.getByTestId('widget-slide-dot').nth(2).click()
    await expect(s).toHaveAttribute('data-index', '2')
    await expect(s.getByTestId('widget-slide-dot').nth(2)).toHaveAttribute('aria-current', 'true')
    await expect(s).toHaveAttribute('data-index', '0', { timeout: 7_000 }) // wraps around
  })

  test('audio widget: compact player bar with title, seek bar and time; play and pause work', async ({ page, api, ws }) => {
    await install(api, 'media')
    const w = await addWidget(api, ws, 'audio', { url: `${fxOrigin}/tone.wav`, title: 'Test tone' })
    await openApp(page)
    const a = card(page, w.id).getByTestId('widget-audio')
    await expect(a).toBeVisible()
    await expect(a.getByTestId('audio-title')).toHaveText('Test tone')
    await expect(a.getByTestId('audio-toggle')).toHaveAttribute('aria-label', 'Play')
    await expect(a.getByTestId('audio-seek')).toBeVisible()
    await expect(a.getByTestId('audio-time')).toHaveText('0:00 / 0:02')
    await expect(a.getByTestId('audio-mute')).toBeVisible()
    await expect(a).toHaveAttribute('data-state', 'paused')
    await a.getByTestId('audio-toggle').click()
    await expect(a).toHaveAttribute('data-state', 'playing')
    await expect(a.getByTestId('audio-toggle')).toHaveAttribute('aria-label', 'Pause')
    await a.getByTestId('audio-toggle').click()
    await expect(a).toHaveAttribute('data-state', 'paused')
    await a.getByTestId('audio-mute').click()
    await expect(a.getByTestId('audio-mute')).toHaveAttribute('aria-pressed', 'true')
    // media element: http src only, no autoplay
    const el = a.locator('audio')
    await expect(el).toHaveAttribute('src', `${fxOrigin}/tone.wav`)
    expect(await el.evaluate((e: HTMLAudioElement) => e.autoplay)).toBe(false)
  })

  test('video widget: YouTube and Bilibili links become iframes, other URLs a native video', async ({ page, api, ws }) => {
    await install(api, 'media')
    const yt = await addWidget(api, ws, 'video', { url: 'https://youtu.be/dQw4w9WgXcQ' })
    const bili = await addWidget(api, ws, 'video', { url: 'https://www.bilibili.com/video/BV1GJ411x7h7' })
    const file = await addWidget(api, ws, 'video', { url: `${fxOrigin}/clip.mp4`, autoplay: true, loop: true })
    await openApp(page)
    await expect(card(page, yt.id).getByTestId('widget-iframe')).toHaveAttribute('src', 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ')
    await expect(card(page, bili.id).getByTestId('widget-iframe')).toHaveAttribute('src', 'https://player.bilibili.com/player.html?bvid=BV1GJ411x7h7&autoplay=0')
    const v = card(page, file.id).locator('video')
    await expect(v).toHaveAttribute('src', `${fxOrigin}/clip.mp4`)
    await expect(v).toHaveAttribute('playsinline', '')
    expect(await v.evaluate((e: HTMLVideoElement) => [e.muted, e.autoplay, e.loop, e.controls])).toEqual([true, true, true, true]) // autoplay only together with muted
  })
})

/** Nothing executable or loadable beyond the allow-list is in the rendered html block. */
async function expectSafeHtml(page: import('@playwright/test').Page, h: import('@playwright/test').Locator) {
  await expect(h.locator('script, svg, iframe, style, object, embed, form, input')).toHaveCount(0)
  expect(await h.evaluate((el) => el.innerHTML.toLowerCase())).not.toMatch(/onerror|onclick|onload|javascript:|data:text|srcset|style=|class=|\sid=/)
  expect(await h.evaluate((el) => el.querySelectorAll('[style],[class],[id]').length)).toBe(0)
  expect(await page.evaluate(() => (window as any).__xss)).toBeUndefined()
  const img = h.locator('img')
  if (await img.count()) {
    await expect(img.first()).toHaveAttribute('referrerpolicy', 'no-referrer')
    await expect(img.first()).not.toHaveAttribute('srcset', /.*/)
  }
}
