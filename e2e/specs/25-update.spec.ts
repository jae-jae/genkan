import type { Page, Route } from '@playwright/test'
import { expect, openApp, openSettings, origin, test } from '../helpers'

// UI of update checks and self-update (docs/UPDATES.md §6/§7). The e2e server is a development build and never finds
// releases, so GET/POST /api/update* is mocked with page.route (the contract is what the UI is coded against).
// The `update_check` switch is the exception: it PATCHes the real settings API.

const REPO = 'https://github.com/jae-jae/genkan'
const CMD = 'docker compose pull && docker compose up -d'

const NOTES_12 = [
  "## What's Changed",
  '### Features',
  '* Add a **media** widget by @jae in https://github.com/jae-jae/genkan/pull/21.',
  '* Self-update with *automatic* rollback (#22), see `docs/UPDATES.md`',
  '',
  'Raw html <script>window.__pwned = 1</script> and <img src=x onerror="window.__pwned = 1"> stay text.',
  '',
  '[click me](javascript:window.__pwned=1) and [data link](data:text/html,<b>x</b>) and [docs](https://example.com/docs).',
  '',
  '**Full Changelog**: https://github.com/jae-jae/genkan/compare/v1.1.0...v1.2.0',
].join('\n')

const R12 = { version: 'v1.2.0', name: 'v1.2.0', notes: NOTES_12, published_at: Date.UTC(2026, 10, 3, 12), url: `${REPO}/releases/tag/v1.2.0` }
const R11 = { version: 'v1.1.0', name: 'v1.1.0', notes: '- Add the plugin market', published_at: Date.UTC(2026, 9, 16, 12), url: `${REPO}/releases/tag/v1.1.0` }

const status = (over: Record<string, any> = {}) => ({
  current: 'v1.0.0',
  dev: false,
  repo: REPO,
  check_enabled: true,
  env_disabled: false,
  checking: false,
  checked_at: Date.UTC(2026, 10, 5, 9, 30),
  error: '',
  latest: '',
  available: false,
  dismissed: '',
  releases: [] as any[],
  apply: { supported: true, reason: '' },
  job: { phase: 'idle', version: '', progress: 0, error: '' },
  rolled_back: '',
  ...over,
})
const available = (over: Record<string, any> = {}) => status({ latest: 'v1.2.0', available: true, releases: [R12, R11], ...over })

interface Ctl {
  s: ReturnType<typeof status>
  /** the API is unreachable (the server is restarting) */
  down: boolean
  calls: { method: string; path: string; body: any }[]
  count: (method: string, path: string) => number
  last: (method: string, path: string) => any
}

/** Serves /api/update* from `ctl.s`; tests mutate `ctl.s` / `ctl.down` as the "server" changes. */
async function mockUpdate(page: Page, initial: ReturnType<typeof status>): Promise<Ctl> {
  const ctl: Ctl = {
    s: initial,
    down: false,
    calls: [],
    count: (m, p) => ctl.calls.filter((c) => c.method === m && c.path === p).length,
    last: (m, p) => ctl.calls.filter((c) => c.method === m && c.path === p).at(-1)?.body,
  }
  const json = (route: Route, body: unknown, code = 200) => route.fulfill({ status: code, contentType: 'application/json', body: JSON.stringify(body) })
  await page.route(/\/api\/update(\/.*)?$/, async (route) => {
    const req = route.request()
    const path = new URL(req.url()).pathname
    const body = req.postData() ? JSON.parse(req.postData()!) : undefined
    ctl.calls.push({ method: req.method(), path, body })
    if (ctl.down) return route.abort('connectionrefused')
    if (req.method() === 'POST' && path === '/api/update/check') ctl.s = { ...ctl.s, checked_at: Date.now() }
    if (req.method() === 'POST' && path === '/api/update/dismiss') {
      if (body?.rolled_back) ctl.s = { ...ctl.s, rolled_back: '' }
      else if (body?.version) ctl.s = { ...ctl.s, dismissed: body.version }
    }
    if (req.method() === 'POST' && path === '/api/update/apply') {
      ctl.s = { ...ctl.s, job: { phase: 'downloading', version: body.version, progress: 0.4, error: '' } }
      return json(route, ctl.s, 202)
    }
    return json(route, ctl.s)
  })
  return ctl
}

const btn = (page: Page) => page.getByTestId('update-btn')
const panel = (page: Page) => page.getByTestId('update-modal')

async function openAbout(page: Page) {
  await openSettings(page, 'about')
  await expect(page.getByTestId('update-about')).toBeVisible()
}

test.describe('updates: topbar button, About section, changelog panel', () => {
  test('no update: no topbar button, About says up to date', async ({ page }) => {
    const ctl = await mockUpdate(page, status())
    await openApp(page)
    await expect.poll(() => ctl.count('GET', '/api/update')).toBeGreaterThanOrEqual(1)
    await expect(btn(page)).toHaveCount(0)
    await openAbout(page)
    await expect(page.getByTestId('update-uptodate')).toHaveText('v1.0.0')
    await expect(page.getByTestId('update-open-changelog')).toHaveCount(0)
    await expect(page.getByTestId('about-github')).toHaveAttribute('href', REPO)
    await expect(page.getByTestId('about-github')).toHaveAttribute('rel', /noopener/)
    await expect(page.getByTestId('about-github')).toHaveAttribute('target', '_blank')
  })

  test('the button sits left of the plugins button; the status is fetched on load and every hour', async ({ page }) => {
    await page.clock.install()
    const ctl = await mockUpdate(page, status())
    await openApp(page)
    await expect.poll(() => ctl.count('GET', '/api/update')).toBe(1)
    await expect(btn(page)).toHaveCount(0)
    ctl.s = available()
    await page.clock.fastForward(3600_000 + 1000)
    await expect.poll(() => ctl.count('GET', '/api/update')).toBeGreaterThanOrEqual(2)
    await expect(btn(page)).toBeVisible()
    await expect(btn(page)).toHaveAttribute('data-version', 'v1.2.0')
    await expect(btn(page).locator('svg[data-icon="circle-arrow-up"]')).toHaveCount(1)
    const x = async (id: string) => (await page.getByTestId(id).boundingBox())!.x
    expect(await x('edit-toggle')).toBeLessThan(await x('update-btn'))
    expect(await x('update-btn')).toBeLessThan(await x('plugins-btn'))
  })

  test('panel lists releases newest first with dates and renders notes safely; skip hides the button', async ({ page }) => {
    const ctl = await mockUpdate(page, available())
    await openApp(page)
    await btn(page).click()
    await expect(panel(page)).toBeVisible()
    await expect(page.getByTestId('update-summary')).toContainText('v1.0.0')
    await expect(page.getByTestId('update-summary')).toContainText('v1.2.0')
    const rels = page.getByTestId('update-release')
    await expect(rels).toHaveCount(2)
    await expect(rels.nth(0)).toHaveAttribute('data-version', 'v1.2.0')
    await expect(rels.nth(1)).toHaveAttribute('data-version', 'v1.1.0')
    await expect(rels.nth(0).getByTestId('update-release-date')).toHaveText(/2026/)
    await expect(rels.nth(0).getByTestId('update-release-link')).toHaveAttribute('href', `${REPO}/releases/tag/v1.2.0`)

    const notes = rels.nth(0).getByTestId('update-notes')
    // structure
    await expect(notes.locator('h4')).toHaveText("What's Changed")
    await expect(notes.locator('h5')).toHaveText('Features')
    await expect(notes.locator('ul > li')).toHaveCount(2)
    await expect(notes.locator('strong').first()).toHaveText('media')
    await expect(notes.locator('em')).toHaveText('automatic')
    await expect(notes.locator('code')).toHaveText('docs/UPDATES.md')
    await expect(rels.nth(1).locator('ul > li')).toHaveText('Add the plugin market')
    // GitHub @user / #123 stay plain text
    await expect(notes).toContainText('by @jae in')
    await expect(notes).toContainText('(#22)')
    // raw HTML is text, never elements
    await expect(notes.locator('script, img, b, iframe')).toHaveCount(0)
    await expect(notes).toContainText('<script>window.__pwned = 1</script>')
    await expect(notes).toContainText('<img src=x onerror="window.__pwned = 1">')
    // links: only http(s) become anchors; javascript: / data: are inert text
    const hrefs = await notes.locator('a').evaluateAll((as) => as.map((a) => a.getAttribute('href')))
    expect(hrefs).toEqual([
      'https://github.com/jae-jae/genkan/pull/21', // autolink, the trailing "." is not part of it
      'https://example.com/docs',
      'https://github.com/jae-jae/genkan/compare/v1.1.0...v1.2.0',
    ])
    for (const a of await notes.locator('a').all()) {
      await expect(a).toHaveAttribute('target', '_blank')
      await expect(a).toHaveAttribute('rel', /noopener/)
    }
    await expect(notes.locator('a[href^="javascript:" i], a[href^="data:" i]')).toHaveCount(0)
    await expect(notes).toContainText('click me')
    await expect(notes.getByText('click me')).not.toHaveAttribute('href', /.*/)
    await notes.getByText('click me').click()
    expect(await page.evaluate(() => (window as any).__pwned)).toBeUndefined()

    // skip this version
    await page.getByTestId('update-skip').click()
    await expect(panel(page)).toHaveCount(0)
    expect(ctl.last('POST', '/api/update/dismiss')).toEqual({ version: 'v1.2.0' })
    await expect(btn(page)).toHaveCount(0)
    // the About page still offers the changelog
    await openAbout(page)
    await expect(page.getByTestId('update-latest')).toHaveText('v1.2.0')
  })

  test('update now asks for confirmation (backup + rollback), can be cancelled', async ({ page }) => {
    const ctl = await mockUpdate(page, available())
    await openApp(page)
    await btn(page).click()
    await page.getByTestId('update-apply').click()
    const box = page.getByTestId('update-confirm-box')
    await expect(box).toBeVisible()
    await expect(box).toContainText('v1.2.0')
    await expect(box).toContainText(/back/i)
    await expect(box).toContainText(/rolls back/i)
    expect(ctl.count('POST', '/api/update/apply')).toBe(0)
    await page.getByTestId('update-confirm-cancel').click()
    await expect(page.getByTestId('update-releases')).toBeVisible()
    expect(ctl.count('POST', '/api/update/apply')).toBe(0)
  })

  test('apply flow: phases, progress, restarting survives network errors, reloads when the new version answers', async ({ page }) => {
    const ctl = await mockUpdate(page, available())
    await openApp(page)
    await page.evaluate(() => ((window as any).__marker = 1))
    await btn(page).click()
    await page.getByTestId('update-apply').click()
    await page.getByTestId('update-confirm').click()
    expect(ctl.last('POST', '/api/update/apply')).toEqual({ version: 'v1.2.0' })

    const prog = page.getByTestId('update-progress')
    await expect(prog).toHaveAttribute('data-phase', 'downloading')
    await expect(page.getByTestId('update-percent')).toHaveText('40%')
    ctl.s = available({ job: { phase: 'downloading', version: 'v1.2.0', progress: 0.9, error: '' } })
    await expect(page.getByTestId('update-percent')).toHaveText('90%')
    ctl.s = available({ job: { phase: 'verifying', version: 'v1.2.0', progress: 1, error: '' } })
    await expect(prog).toHaveAttribute('data-phase', 'verifying')
    ctl.s = available({ job: { phase: 'installing', version: 'v1.2.0', progress: 1, error: '' } })
    await expect(prog).toHaveAttribute('data-phase', 'installing')
    ctl.s = available({ job: { phase: 'restarting', version: 'v1.2.0', progress: 1, error: '' } })
    await expect(prog).toHaveAttribute('data-phase', 'restarting')
    // the server is gone for a while: errors are tolerated and the UI keeps waiting
    ctl.down = true
    const n = ctl.count('GET', '/api/update')
    await expect.poll(() => ctl.count('GET', '/api/update'), { timeout: 9000 }).toBeGreaterThan(n + 1)
    await expect(prog).toHaveAttribute('data-phase', 'restarting')
    expect(await page.evaluate(() => (window as any).__marker)).toBe(1)
    // the old version answers once more (still current !== target): no reload
    ctl.down = false
    await expect.poll(() => ctl.count('GET', '/api/update'), { timeout: 9000 }).toBeGreaterThan(n + 2)
    expect(await page.evaluate(() => (window as any).__marker)).toBe(1)
    // the new version is up: the page reloads by itself
    ctl.s = status({ current: 'v1.2.0' })
    await expect.poll(() => page.evaluate(() => (window as any).__marker), { timeout: 12_000 }).toBeUndefined()
    await expect(page.getByTestId('ws-menu-btn')).toBeVisible()
    await expect(btn(page)).toHaveCount(0)
  })

  test('restarting for more than 3 minutes shows an error and a way out', async ({ page }) => {
    await page.clock.install()
    const ctl = await mockUpdate(page, available())
    await openApp(page)
    await btn(page).click()
    await page.getByTestId('update-apply').click()
    await page.getByTestId('update-confirm').click()
    ctl.s = available({ job: { phase: 'restarting', version: 'v1.2.0', progress: 1, error: '' } })
    await expect(page.getByTestId('update-progress')).toHaveAttribute('data-phase', 'restarting')
    ctl.down = true
    await expect.poll(() => ctl.count('GET', '/api/update')).toBeGreaterThan(1)
    await page.clock.fastForward(181_000)
    await expect(page.getByTestId('update-failed')).toBeVisible()
    await expect(page.getByTestId('update-failed')).toContainText('3 minutes')
    await expect(page.getByTestId('update-failed')).toContainText('logs')
    await expect(page.getByTestId('update-reload')).toBeVisible()
    await page.getByTestId('update-progress-close').click()
    await expect(panel(page)).toHaveCount(0)
  })

  test('a failed job is shown and can be retried; a failed apply request shows its error', async ({ page }) => {
    const ctl = await mockUpdate(page, available({ job: { phase: 'failed', version: 'v1.2.0', progress: 0, error: 'checksum mismatch' } }))
    await openApp(page)
    await btn(page).click()
    await expect(page.getByTestId('update-failed')).toContainText('checksum mismatch')
    await expect(page.getByTestId('update-apply')).toBeVisible()
    // the server refuses (409 / 400): the message is shown, nothing is waiting
    await page.route('**/api/update/apply', (route) => route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: 'already running' }) }))
    ctl.s = available()
    await page.getByTestId('update-apply').click()
    await page.getByTestId('update-confirm').click()
    await expect(page.getByTestId('update-failed')).toContainText('already running')
    await expect(page.getByTestId('update-progress')).toHaveCount(0)
  })

  test('unsupported install: reason and copyable pull commands, no update-now', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: origin() })
    await mockUpdate(page, available({ apply: { supported: false, reason: 'noexec' } }))
    await openApp(page)
    await btn(page).click()
    await expect(page.getByTestId('update-unsupported')).toContainText('noexec')
    await expect(page.getByTestId('update-commands-text')).toHaveText(CMD)
    await expect(page.getByTestId('update-apply')).toHaveCount(0)
    await expect(page.getByTestId('update-skip')).toBeVisible()
    await page.getByTestId('update-commands-copy').click()
    await expect(page.getByTestId('toast').first()).toContainText('Copied')
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(CMD)
  })

  test('development build: notice, no switch / check button / topbar button', async ({ page }) => {
    await mockUpdate(page, status({ current: 'dev', dev: true, apply: { supported: false, reason: 'dev' } }))
    await openApp(page)
    await expect(btn(page)).toHaveCount(0)
    await openAbout(page)
    await expect(page.getByTestId('update-dev-note')).toContainText('development build')
    await expect(page.getByTestId('set-update-check')).toHaveCount(0)
    await expect(page.getByTestId('update-check-now')).toHaveCount(0)
    await expect(page.getByTestId('about-github')).toBeVisible()
  })

  test('rolled-back notice is shown until it is dismissed', async ({ page }) => {
    const ctl = await mockUpdate(page, status({ rolled_back: 'v1.1.0' }))
    await openApp(page)
    await expect(page.getByTestId('toast').first()).toContainText('rolled back')
    await openAbout(page)
    await expect(page.getByTestId('update-rolled-back')).toContainText('v1.1.0')
    await page.getByTestId('update-rolled-back-dismiss').click()
    await expect(page.getByTestId('update-rolled-back')).toHaveCount(0)
    expect(ctl.last('POST', '/api/update/dismiss')).toEqual({ rolled_back: true })
  })

  test('check now posts a check and shows the time or the error', async ({ page }) => {
    const ctl = await mockUpdate(page, status({ checked_at: 0 }))
    await openApp(page)
    await openAbout(page)
    const row = page.getByTestId('update-about')
    await expect(row).toContainText('Not checked yet')
    await page.getByTestId('update-check-now').click()
    await expect.poll(() => ctl.count('POST', '/api/update/check')).toBe(1)
    await expect(row).toContainText('Last checked:')
    await expect(page.getByTestId('update-error')).toHaveCount(0)
    ctl.s = status({ error: 'github unreachable' })
    await page.getByTestId('update-check-now').click()
    await expect(page.getByTestId('update-error')).toContainText('github unreachable')
    // a check that finds a release adds the button without a reload
    ctl.s = available()
    await page.getByTestId('update-check-now').click()
    await expect(btn(page)).toBeVisible()
    await expect(page.getByTestId('update-latest')).toHaveText('v1.2.0')
    await page.getByTestId('update-open-changelog').click()
    await expect(panel(page)).toBeVisible()
    await expect(page.getByTestId('update-release')).toHaveCount(2)
    await page.keyboard.press('Escape')
    // back to Settings (the panel was opened over it)
    await expect(page.getByTestId('settings-modal')).toBeVisible()
  })

  test('the auto-check switch is bound to settings.update_check (real settings API)', async ({ page, api }) => {
    await mockUpdate(page, status())
    await openApp(page)
    await openAbout(page)
    const sw = page.getByTestId('set-update-check')
    await expect(sw).toHaveAttribute('aria-checked', 'true') // default on
    await sw.click()
    await expect(sw).toHaveAttribute('aria-checked', 'false')
    await expect.poll(async () => (await api.bootstrap()).settings.update_check).toBe(false)
    await page.reload()
    await openAbout(page)
    await expect(page.getByTestId('set-update-check')).toHaveAttribute('aria-checked', 'false')
    await page.getByTestId('set-update-check').click()
    await expect.poll(async () => (await api.bootstrap()).settings.update_check).toBe(true)
  })

  test('the real /api/update answers with the documented shape (development build in e2e)', async ({ api }) => {
    const s = await api.call('GET', '/api/update')
    expect(typeof s.current).toBe('string')
    expect(typeof s.dev).toBe('boolean')
    expect(Array.isArray(s.releases)).toBe(true)
    expect(s.job.phase).toBe('idle')
    expect(typeof s.apply.supported).toBe('boolean')
  })

  test('a guest never asks for the update status', async ({ api, browser }) => {
    await api.settings({ guest_access: true })
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const g = await ctx.newPage()
    const seen: string[] = []
    g.on('request', (r) => r.url().includes('/api/update') && seen.push(r.url()))
    await g.goto('/')
    await expect(g.getByTestId('ws-menu-btn')).toBeVisible()
    await g.waitForTimeout(500)
    expect(seen).toEqual([])
    await expect(g.getByTestId('update-btn')).toHaveCount(0)
    await ctx.close()
  })
})

test.describe('updates on a phone', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 })

  test('topbar with the update button, panel and About fit the screen', async ({ page }) => {
    await mockUpdate(page, available())
    await openApp(page)
    await expect(btn(page)).toBeVisible()
    for (const id of ['ws-menu-btn', 'edit-toggle', 'update-btn', 'plugins-btn', 'menu-btn']) {
      const bb = (await page.getByTestId(id).boundingBox())!
      expect(bb.x, id).toBeGreaterThanOrEqual(0)
      expect(bb.x + bb.width, id).toBeLessThanOrEqual(390)
    }
    await btn(page).tap()
    await expect(page.getByTestId('update-release')).toHaveCount(2)
    const m = (await panel(page).boundingBox())!
    expect(m.x).toBeGreaterThanOrEqual(0)
    expect(m.x + m.width).toBeLessThanOrEqual(390.5)
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
    // actions stay reachable
    await expect(page.getByTestId('update-apply')).toBeInViewport()
    await page.getByTestId('update-apply').tap()
    await expect(page.getByTestId('update-confirm')).toBeInViewport()
    await page.getByTestId('update-confirm-cancel').tap()
    await page.keyboard.press('Escape')
    await expect(panel(page)).toHaveCount(0)

    await page.getByTestId('menu-btn').tap()
    await page.getByTestId('settings-btn').tap()
    await page.getByTestId('settings-tab-about').tap()
    await expect(page.getByTestId('update-about')).toBeVisible()
    for (const id of ['set-update-check', 'update-check-now', 'update-open-changelog']) {
      const bb = (await page.getByTestId(id).boundingBox())!
      expect(bb.x + bb.width, id).toBeLessThanOrEqual(390)
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
  })
})
