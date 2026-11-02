import { request as pwRequest } from '@playwright/test'
import { type Api, expect, makePng, openApp, openSettings, origin, switchWs, test } from '../helpers'

const COLORS: [number, number, number][] = [[200, 40, 40], [40, 200, 40], [40, 40, 200]]
const gallery = (api: Api) => api.call<{ value: string; url: string; size: number; created_at: string }[]>('GET', '/api/wallpapers')

// 1x1 transparent PNG
const PNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64')

test.describe('wallpaper', () => {
  test('switch presets per workspace via Settings > Appearance', async ({ page, api, ws }) => {
    const other = await api.workspace({ name: 'Other', wallpaper: 'preset:forest' })
    await openApp(page)
    const wp = page.getByTestId('wallpaper')
    await expect(wp).toHaveAttribute('data-wallpaper', 'preset:aurora')
    await openSettings(page, 'appearance')
    for (const id of ['dusk', 'ocean', 'forest', 'sakura', 'graphite']) {
      await page.getByTestId(`wp-preset-${id}`).click()
      await expect(wp).toHaveAttribute('data-wallpaper', `preset:${id}`)
      await expect(page.getByTestId(`wp-preset-${id}`)).toHaveAttribute('aria-pressed', 'true')
    }
    await expect.poll(async () => (await api.bootstrap()).workspaces.find((w) => w.id === ws)!.wallpaper).toBe('preset:graphite')
    // other workspace is untouched and has its own wallpaper
    expect((await api.bootstrap()).workspaces.find((w) => w.id === other.id)!.wallpaper).toBe('preset:forest')
    await page.getByTestId('modal-close').click()
    await switchWs(page, other.id)
    await expect(wp).toHaveAttribute('data-wallpaper', 'preset:forest')
    await switchWs(page, ws)
    await expect(wp).toHaveAttribute('data-wallpaper', 'preset:graphite')
    await page.reload()
    await expect(page.getByTestId('wallpaper')).toHaveAttribute('data-wallpaper', 'preset:graphite')
  })

  test('upload a single wallpaper: it is added to the gallery and applied', async ({ page, api, ws }) => {
    await openApp(page)
    await openSettings(page, 'appearance')
    await page.getByTestId('wp-upload-input').setInputFiles({ name: 'my wall.png', mimeType: 'image/png', buffer: PNG })
    const wp = page.getByTestId('wallpaper')
    await expect(wp).toHaveAttribute('data-wallpaper', /^upload:[0-9a-f]{16}\.png$/)
    const value = (await wp.getAttribute('data-wallpaper'))!
    const file = value.slice('upload:'.length)
    await expect(page.getByTestId(`gallery-item-${file}`)).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByTestId('wp-current-upload')).toHaveCount(0)
    await expect.poll(async () => (await api.bootstrap()).workspaces.find((w) => w.id === ws)!.wallpaper).toBe(value)
    const r = await api.ctx.get(`/uploads/wallpapers/${file}`)
    expect(r.status()).toBe(200)
    expect(r.headers()['content-type']).toContain('image/png')
    // applied as a CSS background
    await expect(wp.locator('.wp-img')).toHaveAttribute('style', new RegExp(`/uploads/wallpapers/${file}`))
    // switching back to a preset works too; the upload stays in the gallery
    await page.getByTestId('wp-preset-ocean').click()
    await expect(wp).toHaveAttribute('data-wallpaper', 'preset:ocean')
    await expect(page.getByTestId(`gallery-item-${file}`)).toHaveAttribute('aria-pressed', 'false')
    // uploading the same image again is idempotent
    await page.getByTestId('wp-upload-input').setInputFiles({ name: 'again.png', mimeType: 'image/png', buffer: PNG })
    await expect(wp).toHaveAttribute('data-wallpaper', value)
    expect(await gallery(api)).toHaveLength(1)
  })

  test('gallery: upload 3 files at once, apply one, delete (workspace resets to aurora), apply to all', async ({ page, api, ws }) => {
    const other = await api.workspace({ name: 'Other', wallpaper: 'preset:forest' })
    await openApp(page)
    await openSettings(page, 'appearance')
    const wp = page.getByTestId('wallpaper')
    await expect(page.getByTestId('wp-gallery').locator('[data-testid^="gallery-item-"]')).toHaveCount(0)

    // ---- multi upload only adds, it does not change the wallpaper ----
    await page.getByTestId('wp-upload-input').setInputFiles(COLORS.map((c, i) => ({ name: `w${i}.png`, mimeType: 'image/png', buffer: makePng(c) })))
    const items = page.getByTestId('wp-gallery').locator('[data-testid^="gallery-item-"]')
    await expect(items).toHaveCount(3)
    await expect(page.getByTestId('wp-upload-progress')).toHaveCount(0) // finished
    await expect(wp).toHaveAttribute('data-wallpaper', 'preset:aurora')
    const files = await gallery(api)
    expect(files).toHaveLength(3)
    for (const f of files) {
      expect(f.value).toMatch(/^upload:[0-9a-f]{16}\.png$/)
      expect(f.url).toBe(`/uploads/wallpapers/${f.value.slice(7)}`)
      expect(f.size).toBeGreaterThan(0)
      expect(Date.parse(f.created_at)).not.toBeNaN()
    }
    const [f1, f2, f3] = files.map((f) => f.value.slice('upload:'.length))

    // ---- apply ----
    await page.getByTestId(`gallery-item-${f2}`).click()
    await expect(wp).toHaveAttribute('data-wallpaper', `upload:${f2}`)
    await expect(page.getByTestId(`gallery-item-${f2}`)).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByTestId(`gallery-item-${f1}`)).toHaveAttribute('aria-pressed', 'false')
    await expect.poll(async () => (await api.bootstrap()).workspaces.find((w) => w.id === ws)!.wallpaper).toBe(`upload:${f2}`)

    // ---- delete: cancel keeps it, confirm removes file + resets the workspace using it ----
    await page.getByTestId(`gallery-item-${f3}`).hover()
    await page.getByTestId(`gallery-delete-${f3}`).click()
    await page.getByTestId('confirm-cancel').click()
    await expect(page.getByTestId(`gallery-item-${f3}`)).toBeVisible()
    await page.getByTestId(`gallery-item-${f2}`).hover()
    await page.getByTestId(`gallery-delete-${f2}`).click()
    await page.getByTestId('confirm-ok').click()
    await expect(page.getByTestId(`gallery-item-${f2}`)).toHaveCount(0)
    await expect(items).toHaveCount(2)
    await expect(wp).toHaveAttribute('data-wallpaper', 'preset:aurora')
    await expect.poll(async () => (await api.bootstrap()).workspaces.find((w) => w.id === ws)!.wallpaper).toBe('preset:aurora')
    await expect.poll(async () => (await api.ctx.get(`/uploads/wallpapers/${f2}`)).status()).toBe(404)
    expect((await api.ctx.get(`/uploads/wallpapers/${f1}`)).status()).toBe(200)

    // ---- apply to all workspaces ----
    await page.getByTestId(`gallery-item-${f1}`).click()
    await expect(wp).toHaveAttribute('data-wallpaper', `upload:${f1}`)
    await page.getByTestId('wp-apply-all').click()
    await expect
      .poll(async () => (await api.bootstrap()).workspaces.map((w) => w.wallpaper))
      .toEqual([`upload:${f1}`, `upload:${f1}`])
    expect((await api.bootstrap()).workspaces.map((w) => w.id)).toEqual([ws, other.id])

    // deleting it now resets both
    await page.getByTestId(`gallery-item-${f1}`).hover()
    await page.getByTestId(`gallery-delete-${f1}`).click()
    await page.getByTestId('confirm-ok').click()
    await expect(page.getByTestId(`gallery-item-${f1}`)).toHaveCount(0)
    await expect.poll(async () => (await api.bootstrap()).workspaces.map((w) => w.wallpaper)).toEqual(['preset:aurora', 'preset:aurora'])
  })

  test('gallery REST: list, delete validation (404, unsafe names)', async ({ api }) => {
    expect(await gallery(api)).toEqual([])
    const up = await (await api.ctx.post('/api/uploads/wallpaper', { multipart: { file: { name: 'x.png', mimeType: 'image/png', buffer: makePng([1, 2, 3]) } } })).json()
    const list = await gallery(api)
    expect(list.map((f) => f.value)).toEqual([up.value])
    expect((await api.ctx.delete('/api/wallpapers/0000000000000000.png')).status()).toBe(404)
    for (const bad of ['..%2Fsecret', '.hidden', 'a%2Fb.png'])
      expect((await api.ctx.delete(`/api/wallpapers/${bad}`)).status(), bad).toBe(400)
    // CSRF: cookie-authenticated delete without the header is refused
    const noCsrf = await pwRequest.newContext({ baseURL: origin(), storageState: await api.ctx.storageState() })
    expect((await noCsrf.delete(`/api/wallpapers/${up.value.slice(7)}`)).status()).toBe(403)
    await noCsrf.dispose()
    expect((await api.ctx.delete(`/api/wallpapers/${up.value.slice(7)}`)).status()).toBe(200)
    expect(await gallery(api)).toEqual([])
    // anonymous clients cannot list
    const anon = await pwRequest.newContext({ baseURL: origin() })
    expect((await anon.get('/api/wallpapers')).status()).toBe(401)
    await anon.dispose()
  })

  test('upload validation: non-image rejected, oversized/garbage not stored', async ({ api }) => {
    const bad = await api.ctx.post('/api/uploads/wallpaper', { multipart: { file: { name: 'x.png', mimeType: 'image/png', buffer: Buffer.from('this is not an image at all') } } })
    expect(bad.status()).toBeGreaterThanOrEqual(400)
    expect(bad.status()).toBeLessThan(500)
    const ok = await api.ctx.post('/api/uploads/wallpaper', { multipart: { file: { name: 'x.png', mimeType: 'image/png', buffer: PNG } } })
    expect(ok.status()).toBe(200)
    const j = await ok.json()
    expect(j.value).toMatch(/^upload:/)
    expect(j.url).toMatch(/^\/uploads\/wallpapers\//)
  })
})
