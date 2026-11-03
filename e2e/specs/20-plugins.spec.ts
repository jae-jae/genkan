import type { Page } from '@playwright/test'
import { Api, CSRF, expect, openApp, openSettings, origin, test, toggleEdit } from '../helpers'

// Plugin system (docs/PLUGINS.md). Only the fully offline built-in catalog plugins are used: calculator, world-clock
// and countdown. Every test starts from `api.reset()` (the `api` fixture), whose replace-import also drops all plugins and
// widgets, so tests install what they need and nothing leaks into later specs (see the afterAll below).

interface W { id: string; plugin: string; widget: string; workspace_id: string; sort: number; config: Record<string, unknown> }
const widgetsOf = async (api: Api): Promise<W[]> => ((await api.bootstrap()) as any).widgets as W[]
const pluginsOf = async (api: Api): Promise<{ id: string; enabled: boolean }[]> => (await api.call('GET', '/api/plugins')) || []
/** Install a built-in catalog plugin through the REST API, enabled. */
const install = (api: Api, id: string) => api.call('POST', '/api/plugins', { catalog: id, enable: true })
const addWidget = (api: Api, plugin: string, widget: string, workspace_id: string, config: Record<string, unknown> = {}) =>
  api.call<W>('POST', '/api/widgets', { plugin, widget, workspace_id, config })
/** data-widget-id of the visible widget cards, in DOM order. */
const domOrder = (page: Page) => page.getByTestId('widget').evaluateAll((els) => els.map((e) => e.getAttribute('data-widget-id')!))
const row = (page: Page, id: string) => page.locator(`[data-testid="plugin-row"][data-plugin-id="${id}"]`)

test.describe('plugins and widgets', () => {
  // The suite shares one container; leave it without plugins even when a test fails half way.
  test.afterAll(async ({ admin }) => {
    await new Api(admin).reset()
  })

  test('admin installs calculator, world-clock and countdown from the built-in catalog in Settings > Plugins', async ({ page, api }) => {
    expect(await pluginsOf(api)).toEqual([]) // clean baseline
    await openApp(page)
    await openSettings(page)
    // the tab sits between "content" and "ai"
    const ids = await page.locator('[data-testid^="settings-tab-"]').evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')!.replace('settings-tab-', '')))
    expect(ids).toEqual(['general', 'appearance', 'content', 'plugins', 'ai', 'security', 'data', 'about'])
    await page.getByTestId('settings-tab-plugins').click()
    await expect(page.getByTestId('settings-panel-plugins')).toBeVisible()
    await expect(page.getByTestId('plugin-empty')).toBeVisible()
    await expect(page.getByTestId('plugin-row')).toHaveCount(0)

    await page.getByTestId('plugin-add-btn').click()
    await expect(page.getByTestId('plugin-add')).toBeVisible()
    await expect(page.getByTestId('plugin-add-tab-catalog')).toHaveAttribute('aria-pressed', 'true')
    const item = (id: string) => page.locator(`[data-testid="plugin-catalog-item"][data-plugin-id="${id}"]`)
    for (const id of ['calculator', 'world-clock', 'countdown']) {
      await expect(item(id)).toHaveAttribute('data-installed', '0')
      await item(id).getByTestId('plugin-catalog-install').click()
      // These plugins ask for no permissions: no confirmation dialog, enabled right away, back on the list.
      await expect(page.getByTestId('plugin-perm-modal')).toHaveCount(0)
      await expect(page.getByTestId('toast').filter({ hasText: /installed/i }).last()).toBeVisible()
      await expect(row(page, id)).toHaveAttribute('data-enabled', '1')
      await page.getByTestId('plugin-add-btn').click() // back to the catalog for the next one
      await expect(item(id)).toHaveAttribute('data-installed', '1')
      await expect(item(id).getByTestId('plugin-catalog-install')).toBeDisabled()
    }
    await page.getByTestId('plugin-add-back').click()

    await expect(page.getByTestId('plugin-row')).toHaveCount(3)
    await expect(row(page, 'calculator').getByTestId('plugin-name')).toHaveText('Calculator')
    await expect(row(page, 'calculator').getByTestId('plugin-badge')).toHaveAttribute('data-kind', 'search')
    await expect(row(page, 'world-clock').getByTestId('plugin-badge')).toHaveAttribute('data-kind', 'widget')
    await expect(row(page, 'countdown').getByTestId('plugin-badge')).toHaveAttribute('data-kind', 'widget')
    for (const id of ['calculator', 'world-clock', 'countdown']) {
      await expect(row(page, id).getByTestId('plugin-toggle')).toHaveAttribute('aria-checked', 'true')
      await expect(row(page, id).getByTestId('plugin-error')).toHaveCount(0)
    }
    // server side agrees
    expect((await pluginsOf(api)).map((p) => `${p.id}:${p.enabled}`).sort()).toEqual(['calculator:true', 'countdown:true', 'world-clock:true'])

    // permissions panel of a plugin without permissions, and the (empty) log panel
    await row(page, 'calculator').getByTestId('plugin-config-btn').click()
    await expect(page.getByTestId('plugin-config-panel')).toBeVisible()
    await expect(page.getByTestId('plugin-permissions').locator('code')).toHaveCount(0)
    await row(page, 'calculator').getByTestId('plugin-logs-btn').click()
    await expect(page.getByTestId('plugin-config-panel')).toHaveCount(0) // one panel at a time
    await expect(page.getByTestId('plugin-logs-panel')).toBeVisible()
    await expect(page.getByTestId('plugin-logs')).toBeVisible()

    // survives a reload
    await page.reload()
    await openSettings(page, 'plugins')
    await expect(page.getByTestId('plugin-row')).toHaveCount(3)
  })

  test('a plugin that requests hosts / links access is installed disabled until the admin allows it', async ({ page, api }) => {
    const source = `export const manifest = {
  id: "e2e-perm", name: "E2E Perm", version: "1.0.0",
  permissions: { fetch: ["example.com", "*.example.org"], links: "read" },
  search: { name: "E2E Perm", prefix: "perm" },
}
export async function search(ctx) { return [{ title: "perm:" + ctx.query }] }
`
    await openApp(page)
    await openSettings(page, 'plugins')
    await page.getByTestId('plugin-add-btn').click()
    await page.getByTestId('plugin-add-tab-source').click()
    await page.getByTestId('plugin-source-input').fill(source)
    await page.getByTestId('plugin-source-install').click()

    const modal = page.getByTestId('plugin-perm-modal')
    await expect(modal).toBeVisible()
    await expect(modal).toContainText('E2E Perm')
    await expect(modal.getByTestId('plugin-perm-host')).toHaveText([/example\.com/, /\*\.example\.org/])
    await expect(modal.getByTestId('plugin-perm-any')).toHaveCount(0)
    await expect(modal.getByTestId('plugin-perm-links')).toHaveAttribute('data-level', 'read')
    // nothing runs before review: it is installed but not enabled
    expect((await pluginsOf(api)).map((p) => `${p.id}:${p.enabled}`)).toEqual(['e2e-perm:false'])

    // "keep disabled"
    await modal.getByTestId('plugin-perm-cancel').click()
    await expect(modal).toHaveCount(0)
    await expect(page.getByTestId('settings-modal')).toBeVisible()
    await expect(row(page, 'e2e-perm')).toHaveAttribute('data-enabled', '0')
    await expect(row(page, 'e2e-perm').getByTestId('plugin-toggle')).toHaveAttribute('aria-checked', 'false')

    // installing it again (same id = update of a disabled plugin) asks again; this time allow
    await page.getByTestId('plugin-add-btn').click()
    await page.getByTestId('plugin-add-tab-source').click()
    await page.getByTestId('plugin-source-input').fill(source)
    await page.getByTestId('plugin-source-install').click()
    await expect(modal).toBeVisible()
    await modal.getByTestId('plugin-perm-allow').click()
    await expect(modal).toHaveCount(0)
    await expect(row(page, 'e2e-perm')).toHaveAttribute('data-enabled', '1')
    await expect(row(page, 'e2e-perm').getByTestId('plugin-error')).toHaveCount(0)
    // the granted permissions are listed in the plugin's panel
    await row(page, 'e2e-perm').getByTestId('plugin-config-btn').click()
    await expect(page.getByTestId('plugin-permissions')).toContainText('example.com')
    await expect(page.getByTestId('plugin-permissions')).toContainText('*.example.org')
    expect((await pluginsOf(api)).map((p) => `${p.id}:${p.enabled}`)).toEqual(['e2e-perm:true'])
  })

  test('search: "1+2*3" shows a plugin group with "= 7"; keyboard select + Enter copies it', async ({ page, api }) => {
    await install(api, 'calculator')
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: origin() })
    await openApp(page)

    await page.keyboard.press('/')
    await expect(page.getByTestId('search-input')).toBeFocused()
    // not math: no plugin group, only the web-search fallback
    await page.keyboard.type('hello')
    await expect(page.getByTestId('search-web')).toBeVisible()
    await page.waitForTimeout(600) // debounce (250 ms) + request
    await expect(page.getByTestId('search-plugin-group')).toHaveCount(0)
    await page.getByTestId('search-input').fill('')

    await page.keyboard.type('1+2*3')
    const group = page.getByTestId('search-plugin-group')
    await expect(group).toHaveCount(1)
    await expect(group).toHaveAttribute('data-plugin', 'calculator')
    await expect(group).toHaveAttribute('role', 'group')
    await expect(group).toContainText('Calculator')
    const res = page.getByTestId('plugin-search-result')
    await expect(res).toHaveCount(1)
    await expect(res).toContainText('= 7')
    await expect(res).toContainText('1+2*3') // subtitle
    await expect(res).toHaveAttribute('data-copy', '1') // copy-only result (no url)
    await expect(res).toHaveAttribute('data-plugin', 'calculator')
    // the plugin group sits before the web-search row, and both take part in arrow-key selection
    await expect(res).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('ArrowDown')
    await expect(res).toHaveAttribute('aria-selected', 'false')
    await expect(page.getByTestId('search-web')).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('ArrowUp')
    await expect(res).toHaveAttribute('aria-selected', 'true')

    await page.keyboard.press('Enter')
    await expect(page.getByTestId('toast').filter({ hasText: 'Copied to clipboard' })).toBeVisible()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('7')
    await expect(page.getByTestId('search-results')).toHaveCount(0)
    await expect(page.getByTestId('search-input')).toHaveValue('')
  })

  test('widgets: add world-clock and countdown in edit mode, they render, reorder, delete', async ({ page, api, ws }) => {
    await install(api, 'world-clock')
    await install(api, 'countdown')
    await openApp(page)
    // no widgets and not editing: no widget area at all
    await expect(page.getByTestId('widget-area')).toHaveCount(0)
    await toggleEdit(page)
    const area = page.getByTestId('widget-area')
    await expect(area).toBeVisible()
    await expect(area).toHaveAttribute('data-editing', '1')
    await expect(page.getByTestId('widget')).toHaveCount(0)

    // ---- add a world clock (default config) ----
    await page.getByTestId('widget-add').click()
    await expect(page.getByTestId('widget-modal')).toBeVisible()
    await expect(page.getByTestId('widget-picker')).toBeVisible()
    await expect(page.getByTestId('widget-pick-countdown-timer')).toBeVisible()
    await page.getByTestId('widget-pick-world-clock-clock').click()
    await expect(page.getByTestId('widget-form')).toBeVisible()
    await expect(page.getByTestId('widget-workspace')).toHaveValue(ws)
    await page.getByTestId('widget-save').click()
    await expect(page.getByTestId('widget-modal')).toHaveCount(0)
    const clock = page.locator('[data-testid="widget"][data-plugin="world-clock"]')
    await expect(clock).toHaveCount(1)
    await expect(clock).toHaveAttribute('data-widget', 'clock')
    await expect(clock).toHaveAttribute('data-size', 'm')
    await expect(clock.getByTestId('widget-title')).toHaveText('World Clock')
    await expect(clock.getByTestId('widget-list-item')).toHaveCount(4) // UTC, Tokyo, London, New York
    await expect(clock).toContainText('Tokyo')
    await expect(clock).toContainText('UTC+9')
    await expect(clock.getByTestId('widget-list-item').first()).toContainText(/\d\d:\d\d/)
    await expect(clock.getByTestId('widget-error')).toHaveCount(0)

    // ---- add a countdown with a label and a far-away date ----
    await page.getByTestId('widget-add').click()
    await page.getByTestId('widget-pick-countdown-timer').click()
    await page.getByTestId('widget-cfg-label').fill('Launch day')
    await page.getByTestId('widget-cfg-date').fill('2099-01-01')
    await page.getByTestId('widget-save').click()
    await expect(page.getByTestId('widget-modal')).toHaveCount(0)
    const cd = page.locator('[data-testid="widget"][data-plugin="countdown"]')
    await expect(cd).toHaveCount(1)
    await expect(cd).toHaveAttribute('data-size', 's')
    await expect(cd.getByTestId('widget-title')).toHaveText('Launch day')
    await expect(cd.getByTestId('widget-stat')).toContainText('Days left')
    await expect(cd.getByTestId('widget-stat')).toContainText(/\d{4,}/) // ~26000 days to 2099
    await expect(cd).toContainText('2099-01-01 00:00 (UTC+0)')
    await expect(cd.getByTestId('widget-error')).toHaveCount(0)

    // persisted server-side, in creation order
    let ws2 = await widgetsOf(api)
    expect(ws2.map((w) => w.plugin)).toEqual(['world-clock', 'countdown'])
    expect(ws2[1].config).toMatchObject({ label: 'Launch day', date: '2099-01-01' })
    const [idClock, idCd] = ws2.map((w) => w.id)
    expect(await domOrder(page)).toEqual([idClock, idCd])

    // ---- reorder ----
    await expect(page.getByTestId(`widget-prev-${idClock}`)).toBeDisabled()
    await expect(page.getByTestId(`widget-next-${idCd}`)).toBeDisabled()
    await page.getByTestId(`widget-next-${idClock}`).click()
    await expect.poll(() => domOrder(page)).toEqual([idCd, idClock])
    await expect.poll(async () => (await widgetsOf(api)).sort((a, b) => a.sort - b.sort).map((w) => w.id)).toEqual([idCd, idClock])
    await expect(page.getByTestId(`widget-prev-${idCd}`)).toBeDisabled()
    await page.reload()
    await toggleEdit(page)
    await expect(page.getByTestId('widget')).toHaveCount(2)
    expect(await domOrder(page)).toEqual([idCd, idClock])
    await page.getByTestId(`widget-prev-${idClock}`).click() // and back
    await expect.poll(() => domOrder(page)).toEqual([idClock, idCd])

    // ---- edit the countdown ----
    await page.getByTestId(`widget-edit-${idCd}`).click()
    await expect(page.getByTestId('widget-cfg-label')).toHaveValue('Launch day')
    await page.getByTestId('widget-cfg-label').fill('Renamed')
    await page.getByTestId('widget-save').click()
    await expect(cd.getByTestId('widget-title')).toHaveText('Renamed')

    // ---- delete one (with confirmation) ----
    await page.getByTestId(`widget-delete-${idCd}`).click()
    await expect(page.getByTestId('confirm-modal')).toBeVisible()
    await page.getByTestId('confirm-cancel').click()
    await expect(page.getByTestId('widget')).toHaveCount(2)
    await page.getByTestId(`widget-delete-${idCd}`).click()
    await page.getByTestId('confirm-ok').click()
    await expect(page.getByTestId('widget')).toHaveCount(1)
    await expect(cd).toHaveCount(0)
    await expect(clock).toHaveCount(1)
    expect((await widgetsOf(api)).map((w) => w.id)).toEqual([idClock])

    // outside edit mode the controls disappear but the remaining widget stays
    await toggleEdit(page, false)
    await expect(clock).toBeVisible()
    await expect(page.getByTestId('widget-add')).toHaveCount(0)
    await expect(page.locator('[data-testid^="widget-prev-"], [data-testid^="widget-next-"], [data-testid^="widget-edit-"], [data-testid^="widget-delete-"]')).toHaveCount(0)
    await expect(page.getByTestId('widget-area')).not.toHaveAttribute('data-editing', '1')
  })

  test('disabling world-clock in Settings > Plugins hides its widget; enabling brings it back', async ({ page, api, ws }) => {
    await install(api, 'world-clock')
    await install(api, 'countdown')
    const clock = await addWidget(api, 'world-clock', 'clock', ws)
    const cd = await addWidget(api, 'countdown', 'timer', ws, { label: 'Stays', date: '2099-01-01' })
    await openApp(page)
    const wc = page.locator(`[data-testid="widget"][data-widget-id="${clock.id}"]`)
    await expect(wc).toBeVisible()
    await expect(wc).toContainText('Tokyo')
    await expect(page.locator(`[data-testid="widget"][data-widget-id="${cd.id}"]`)).toContainText('Stays')

    await openSettings(page, 'plugins')
    const toggle = row(page, 'world-clock').getByTestId('plugin-toggle')
    await expect(toggle).toHaveAttribute('aria-checked', 'true')
    await toggle.click()
    await expect(toggle).toHaveAttribute('aria-checked', 'false')
    await expect(row(page, 'world-clock')).toHaveAttribute('data-enabled', '0')
    await expect.poll(async () => (await pluginsOf(api)).find((p) => p.id === 'world-clock')!.enabled).toBe(false)
    await page.getByTestId('modal-close').click()
    await expect(wc).toHaveCount(0)
    await expect(page.getByTestId('widget')).toHaveCount(1) // the countdown is unaffected
    // the instance itself is kept
    expect((await widgetsOf(api)).map((w) => w.id).sort()).toEqual([clock.id, cd.id].sort())

    // in edit mode it is shown (dashed, with an error) with only the move / delete controls
    await toggleEdit(page)
    const off = page.locator(`[data-testid="widget"][data-widget-id="${clock.id}"]`)
    await expect(off).toBeVisible()
    await expect(off.getByTestId('widget-error')).toBeVisible()
    await expect(page.getByTestId(`widget-edit-${clock.id}`)).toHaveCount(0)
    await expect(page.getByTestId(`widget-delete-${clock.id}`)).toBeVisible()
    await toggleEdit(page, false)
    await expect(wc).toHaveCount(0)

    // a reload keeps it hidden; re-enabling shows it again
    await page.reload()
    await expect(page.getByTestId('widget')).toHaveCount(1)
    await openSettings(page, 'plugins')
    await row(page, 'world-clock').getByTestId('plugin-toggle').click()
    await expect(row(page, 'world-clock')).toHaveAttribute('data-enabled', '1')
    await page.getByTestId('modal-close').click()
    await expect(wc).toBeVisible()
    await expect(wc).toContainText('Tokyo')
    await expect(page.getByTestId('widget')).toHaveCount(2)
  })

  test('disabled plugins stop contributing search results', async ({ page, api }) => {
    await install(api, 'calculator')
    await openApp(page)
    await page.keyboard.press('/')
    await page.keyboard.type('2*21')
    await expect(page.getByTestId('plugin-search-result')).toContainText('= 42')
    await page.keyboard.press('Escape')
    await page.keyboard.press('Escape')

    await openSettings(page, 'plugins')
    await row(page, 'calculator').getByTestId('plugin-toggle').click()
    await expect(row(page, 'calculator')).toHaveAttribute('data-enabled', '0')
    await page.getByTestId('modal-close').click()
    await page.keyboard.press('/')
    await page.keyboard.type('2*21')
    await expect(page.getByTestId('search-web')).toBeVisible()
    await page.waitForTimeout(600)
    await expect(page.getByTestId('search-plugin-group')).toHaveCount(0)
    expect(((await api.call<{ groups: unknown[] | null }>('GET', '/api/plugins/search?q=2*21')).groups || []).length).toBe(0)
  })

  test('uninstalling in Settings removes the plugin, its widgets and its search results', async ({ page, api, ws }) => {
    await install(api, 'world-clock')
    await install(api, 'countdown')
    const clock = await addWidget(api, 'world-clock', 'clock', ws)
    await addWidget(api, 'countdown', 'timer', ws)
    await openApp(page)
    await expect(page.getByTestId('widget')).toHaveCount(2)
    await openSettings(page, 'plugins')
    await row(page, 'world-clock').getByTestId('plugin-delete').click()
    await expect(page.getByTestId('confirm-modal')).toBeVisible()
    await page.getByTestId('confirm-cancel').click() // the settings modal is still there afterwards
    await expect(page.getByTestId('settings-modal')).toBeVisible()
    await expect(row(page, 'world-clock')).toHaveCount(1)
    await row(page, 'world-clock').getByTestId('plugin-delete').click()
    await page.getByTestId('confirm-ok').click()
    await expect(row(page, 'world-clock')).toHaveCount(0)
    await expect(page.getByTestId('plugin-row')).toHaveCount(1)
    await page.getByTestId('modal-close').click()
    await expect(page.locator(`[data-testid="widget"][data-widget-id="${clock.id}"]`)).toHaveCount(0)
    await expect(page.getByTestId('widget')).toHaveCount(1)
    expect((await widgetsOf(api)).map((w) => w.plugin)).toEqual(['countdown'])
  })

  test('guests see the widgets (and search) but none of the admin-only controls', async ({ api, ws, browser }) => {
    await install(api, 'calculator')
    await install(api, 'world-clock')
    await install(api, 'countdown')
    const clock = await addWidget(api, 'world-clock', 'clock', ws)
    await addWidget(api, 'countdown', 'timer', ws, { label: 'Guest view', date: '2099-01-01' })
    await api.settings({ guest_access: true })

    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const g = await ctx.newPage()
    await g.goto('/')
    await expect(g.getByTestId('ws-menu-btn')).toBeVisible()
    expect(await g.evaluate(() => document.querySelector('.page')?.getAttribute('data-role'))).toBe('guest')
    await expect(g.getByTestId('widget-area')).toBeVisible()
    await expect(g.getByTestId('widget-area')).not.toHaveAttribute('data-editing', '1')
    await expect(g.getByTestId('widget')).toHaveCount(2)
    const wc = g.locator(`[data-testid="widget"][data-widget-id="${clock.id}"]`)
    await expect(wc).toContainText('Tokyo')
    await expect(wc.getByTestId('widget-list-item')).toHaveCount(4)
    await expect(g.locator('[data-testid="widget"][data-plugin="countdown"]')).toContainText('Guest view')
    await expect(g.locator('[data-testid="widget"][data-plugin="countdown"]').getByTestId('widget-stat')).toContainText('Days left')
    // no admin controls: edit toggle, settings, add / move / edit / delete / refresh / action buttons
    for (const id of ['edit-toggle', 'settings-btn', 'ai-btn', 'widget-add', 'widget-refresh', 'widget-button', 'widget-modal', 'plugin-add-btn']) {
      await expect(g.getByTestId(id), id).toHaveCount(0)
    }
    await expect(g.locator('[data-testid^="widget-prev-"], [data-testid^="widget-next-"], [data-testid^="widget-edit-"], [data-testid^="widget-delete-"]')).toHaveCount(0)
    // plugin search works for guests
    await g.keyboard.press('/')
    await g.keyboard.type('1+2*3')
    await expect(g.getByTestId('plugin-search-result')).toContainText('= 7')
    await g.keyboard.press('Escape')

    // guest bootstrap lists plugins and widgets, but never source code
    const gb = await (await ctx.request.get('/api/bootstrap')).json()
    expect(gb.auth.role).toBe('guest')
    expect(gb.widgets).toHaveLength(2)
    expect(gb.plugins.map((p: any) => p.id).sort()).toEqual(['calculator', 'countdown', 'world-clock'])
    expect(JSON.stringify(gb)).not.toContain('export const manifest')
    expect(JSON.stringify(gb)).not.toContain('export async function')

    // read endpoints are open to guests, everything else is not
    expect((await ctx.request.get(`/api/widgets/${clock.id}/data`)).status()).toBe(200)
    expect((await ctx.request.get('/api/plugins/search?q=1%2B2*3')).status()).toBe(200)
    for (const [m, p, data] of [
      ['GET', '/api/plugins', undefined],
      ['GET', '/api/plugins/catalog', undefined],
      ['GET', '/api/plugins/calculator/source', undefined],
      ['GET', '/api/plugins/calculator/logs', undefined],
      ['POST', '/api/plugins', { catalog: 'picsum' }],
      ['POST', '/api/plugins/calculator/test', { hook: 'search', query: '1+2*3' }],
      ['PATCH', '/api/plugins/calculator', { enabled: false }],
      ['DELETE', '/api/plugins/calculator', undefined],
      ['POST', '/api/widgets', { plugin: 'world-clock', widget: 'clock', workspace_id: ws }],
      ['POST', '/api/widgets/reorder', { ids: [clock.id] }],
      ['PATCH', `/api/widgets/${clock.id}`, { config: {} }],
      ['DELETE', `/api/widgets/${clock.id}`, undefined],
      ['POST', `/api/widgets/${clock.id}/action`, { action: 'x' }],
    ] as const) {
      for (const headers of [{}, CSRF]) {
        const r = await ctx.request.fetch(p, { method: m, headers, data })
        expect([401, 403], `guest ${m} ${p} ${JSON.stringify(headers)}`).toContain(r.status())
      }
    }
    // ?force=1 is admin-only, but it must not break the guest read
    expect((await ctx.request.get(`/api/widgets/${clock.id}/data?force=1`)).status()).toBe(200)
    // nothing was changed by those attempts
    expect((await pluginsOf(api)).length).toBe(3)
    expect((await widgetsOf(api)).length).toBe(2)
    await ctx.close()
  })

  test('REST: anonymous access is rejected, plugin test runs the search hook, uninstall cascades to widgets', async ({ api, ws, browser }) => {
    await install(api, 'calculator')
    await install(api, 'world-clock')
    const clock = await addWidget(api, 'world-clock', 'clock', ws)
    expect(await widgetsOf(api)).toHaveLength(1)

    // anonymous (guest access is off by default)
    const anon = await browser.newContext({ baseURL: origin() })
    for (const headers of [{}, CSRF]) {
      for (const [m, p, data] of [
        ['GET', '/api/plugins', undefined],
        ['GET', '/api/plugins/catalog', undefined],
        ['GET', '/api/plugins/search?q=1%2B2*3', undefined],
        ['GET', `/api/widgets/${clock.id}/data`, undefined],
        ['POST', '/api/plugins/calculator/test', { hook: 'search', query: '1+2*3' }],
        ['DELETE', '/api/plugins/calculator', undefined],
      ] as const) {
        const r = await anon.request.fetch(p, { method: m, headers, data })
        expect([401, 403], `anon ${m} ${p} ${JSON.stringify(headers)}`).toContain(r.status())
      }
    }
    await anon.close()

    // admin: list + catalog
    const list = await api.call<any[]>('GET', '/api/plugins')
    expect(list.map((p) => p.id).sort()).toEqual(['calculator', 'world-clock'])
    expect(list.find((p) => p.id === 'calculator')).toMatchObject({ enabled: true, name: 'Calculator' })
    const catalog = await api.call<{ id: string; installed: boolean }[]>('GET', '/api/plugins/catalog')
    for (const id of ['calculator', 'world-clock', 'countdown']) expect(catalog.map((c) => c.id)).toContain(id)
    expect(catalog.find((c) => c.id === 'calculator')!.installed).toBe(true)
    expect(catalog.find((c) => c.id === 'countdown')!.installed).toBe(false)

    // POST /api/plugins/calculator/test with the search hook
    const t = await api.call<{ ok: boolean; result: { title: string; subtitle?: string; copy?: string }[]; error?: string; logs: unknown[]; ms: number }>(
      'POST', '/api/plugins/calculator/test', { hook: 'search', query: '1+2*3' })
    expect(t.ok).toBe(true)
    expect(t.error ?? '').toBe('')
    expect(t.result).toHaveLength(1)
    expect(t.result[0]).toMatchObject({ title: '= 7', copy: '7' })
    // not math -> no results; a hook the plugin does not export -> a clear failure
    const none = await api.call<{ ok: boolean; result: unknown[] | null }>('POST', '/api/plugins/calculator/test', { hook: 'search', query: 'hello' })
    expect(none.ok).toBe(true)
    expect(none.result || []).toHaveLength(0)
    const bad = await api.ctx.post('/api/plugins/calculator/test', { data: { hook: 'widget', target: 'x' } })
    expect(bad.status()).toBeGreaterThanOrEqual(400)
    // the real search endpoint agrees
    const s = await api.call<{ groups: { plugin: string; results: { title: string }[] }[] }>('GET', '/api/plugins/search?q=' + encodeURIComponent('1+2*3'))
    expect(s.groups.map((x) => x.plugin)).toEqual(['calculator'])
    expect(s.groups[0].results[0].title).toBe('= 7')
    // widget data endpoint
    const d = await api.call<{ view: { title?: string; body: { type: string; items?: unknown[] }[] }; updated_at: number }>('GET', `/api/widgets/${clock.id}/data`)
    expect(d.view.title).toBe('World Clock')
    expect(d.view.body[0].type).toBe('list')
    expect(d.view.body[0].items).toHaveLength(4)

    // uninstall: plugin gone, its widgets gone from bootstrap, the other plugin untouched
    const cd = await install(api, 'countdown').then(() => addWidget(api, 'countdown', 'timer', ws))
    await api.call('DELETE', '/api/plugins/world-clock')
    const b: any = await api.bootstrap()
    expect(b.plugins.map((p: any) => p.id).sort()).toEqual(['calculator', 'countdown'])
    expect(b.widgets.map((w: W) => w.id)).toEqual([cd.id]) // the world-clock widget is gone, the countdown one stays
    expect((await api.ctx.get(`/api/widgets/${clock.id}/data`)).status()).toBe(404)
    expect((await api.ctx.get('/api/plugins/world-clock/source')).status()).toBe(404)
    expect((await api.ctx.post('/api/plugins/world-clock/test', { data: { hook: 'widget', target: 'clock' } })).status()).toBe(404)
    // calculator keeps working, and is installable again from the catalog
    const again = await api.call<any[]>('GET', '/api/plugins/catalog')
    expect(again.find((c) => c.id === 'world-clock')!.installed).toBe(false)
    await api.call('DELETE', '/api/plugins/calculator')
    await api.call('DELETE', '/api/plugins/countdown')
    const e: any = await api.bootstrap()
    expect(e.plugins).toEqual([])
    expect(e.widgets).toEqual([])
    expect((await pluginsOf(api)).length).toBe(0)
  })

  test('a data replace-import (the e2e baseline reset) leaves no plugins or widgets behind', async ({ api, ws }) => {
    await install(api, 'countdown')
    await addWidget(api, 'countdown', 'timer', ws)
    expect(await widgetsOf(api)).toHaveLength(1)
    await api.reset()
    expect(await pluginsOf(api)).toEqual([])
    const b: any = await api.bootstrap()
    expect(b.widgets).toEqual([])
    expect(b.plugins).toEqual([])
    // the export keeps its documented key set when there are no plugins
    const exp = await (await api.ctx.get('/api/export')).json()
    expect(Object.keys(exp).sort()).toEqual(['categories', 'links', 'schema', 'settings', 'workspaces'])
  })
})
