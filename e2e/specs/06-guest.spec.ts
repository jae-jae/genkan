import { CSRF, PASSWORD, expect, mainMenuItem, openApp, openMainMenu, openSettings, openWsMenu, origin, test, wsNames } from '../helpers'

test.describe('guest (read-only) mode', () => {
  test('enable in settings, then a fresh context sees a read-only page', async ({ page, api, ws, browser }) => {
    const cat = await api.category(ws, 'Apps')
    const a = await api.link({ workspace_id: ws, category_id: cat.id, title: 'Alpha', url: `${origin()}/healthz?l=a` })
    await api.link({ workspace_id: ws, title: 'Pinned one', url: `${origin()}/healthz?l=p`, pinned: true })
    await api.workspace({ name: 'Second' })

    // anonymous + guest disabled: login page only
    const anonCtx = await browser.newContext({ baseURL: origin() })
    const anon = await anonCtx.newPage()
    await anon.goto('/')
    await expect(anon.getByTestId('login-form')).toBeVisible()
    await expect(anon.getByRole('link', { name: 'Alpha' })).toHaveCount(0)

    // admin switches guest access on in Settings > Security
    await openApp(page)
    await openSettings(page)
    await page.getByTestId('settings-tab-security').click()
    await expect(page.getByTestId('set-guest')).toHaveAttribute('aria-checked', 'false')
    await page.getByTestId('set-guest').click()
    await expect(page.getByTestId('set-guest')).toHaveAttribute('aria-checked', 'true')
    await expect.poll(async () => (await api.bootstrap()).settings.guest_access).toBe(true)

    // fresh context: read-only view
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const g = await ctx.newPage()
    await g.goto('/')
    await expect(g.getByTestId('ws-menu-btn')).toBeVisible()
    await expect(g.getByRole('link', { name: 'Alpha' })).toBeVisible()
    await expect(g.getByTestId('section-pinned')).toContainText('Pinned one')
    // the workspace dropdown lists workspaces only: no edit affordances, no "new workspace"
    expect(await wsNames(g)).toEqual(['Home', 'Second'])
    await openWsMenu(g)
    for (const id of ['ws-add', 'ws-manage']) await expect(g.getByTestId(id), id).toHaveCount(0)
    await expect(g.locator('[data-testid^="ws-edit-"], [data-testid^="ws-up-"], [data-testid^="ws-down-"]')).toHaveCount(0)
    await g.keyboard.press('Escape')
    // the main menu offers nothing but "Log in" (sort mode, settings and logout are admin-only)
    await openMainMenu(g)
    await expect(g.getByTestId('login-btn')).toBeVisible()
    await expect(g.locator('[data-testid="main-menu"] [role^="menuitem"]')).toHaveCount(1)
    await g.keyboard.press('Escape')
    for (const id of ['edit-toggle', 'settings-btn', 'logout-btn', 'sort-control', 'new-category-btn', 'ws-add', 'ws-manage', 'ai-btn', 'pinned-add-link', 'uncat-add-link']) {
      await expect(g.getByTestId(id), id).toHaveCount(0)
    }
    await expect(g.locator('[data-testid^="link-edit-"], [data-testid^="link-delete-"], [data-testid^="pin-link-edit-"], [data-testid^="category-rename-"]')).toHaveCount(0)
    expect(await g.evaluate(() => document.querySelector('.page')?.getAttribute('data-role'))).toBe('guest')
    // search works for guests
    await g.keyboard.press('/')
    await g.keyboard.type('alpha')
    await expect(g.getByTestId('search-result')).toHaveCount(1)
    await g.keyboard.press('Escape')

    // writes are rejected (cookie-less, with and without the CSRF header)
    for (const headers of [{}, CSRF]) {
      const w = await ctx.request.post('/api/links', { headers, data: { workspace_id: ws, title: 'nope', url: 'http://x.lan' } })
      expect([401, 403], `create as guest ${JSON.stringify(headers)}`).toContain(w.status())
    }
    expect([401, 403]).toContain((await ctx.request.patch('/api/settings', { headers: CSRF, data: { title: 'x' } })).status())
    expect([401, 403]).toContain((await ctx.request.delete(`/api/links/${a.id}`, { headers: CSRF })).status())
    expect([401, 403]).toContain((await ctx.request.get('/api/export')).status())
    expect([401, 403]).toContain((await ctx.request.get('/api/token')).status())
    expect([401, 403]).toContain((await ctx.request.get('/api/skill')).status())
    expect((await ctx.request.get('/api/health')).status()).toBe(200)
    expect((await api.bootstrap()).links).toHaveLength(2)
    const gb = await (await ctx.request.get('/api/bootstrap')).json()
    expect(gb.auth.role).toBe('guest')
    expect(gb.links).toHaveLength(2)

    // guest clicks do not count (204 but not recorded), and the UI click doesn't either
    expect((await ctx.request.post(`/api/links/${a.id}/click`)).status()).toBe(204)
    const popup = ctx.waitForEvent('page')
    await g.getByTestId(`link-${a.id}`).click()
    await (await popup).close()
    await g.bringToFront()
    await expect.poll(async () => (await api.links()).find((l) => l.id === a.id)!.clicks).toBe(0)

    // the "sign in" button leads to the login form and back; logging in gives edit controls
    await mainMenuItem(g, 'login-btn')
    await expect(g.getByTestId('login-form')).toBeVisible()
    await g.getByTestId('login-back').click()
    await expect(g.getByTestId('menu-btn')).toBeVisible()
    await mainMenuItem(g, 'login-btn')
    await g.getByTestId('login-password').fill(PASSWORD)
    await g.getByTestId('login-submit').click()
    await expect(g.getByTestId('edit-toggle')).toBeVisible()
    await openMainMenu(g)
    await expect(g.getByTestId('login-btn')).toHaveCount(0)
    await expect(g.getByTestId('settings-btn')).toBeVisible()
    await g.keyboard.press('Escape')

    // turning guest access off again locks anonymous visitors out
    await page.getByTestId('set-guest').click()
    await expect.poll(async () => (await api.bootstrap()).settings.guest_access).toBe(false)
    await anon.reload()
    await expect(anon.getByTestId('login-form')).toBeVisible()
    await anonCtx.close()
    await ctx.close()
  })

  test('uploads are not readable by anonymous visitors unless guest mode is on', async ({ api, ws, browser }) => {
    const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64')
    const up = await api.ctx.post('/api/uploads/icon', { multipart: { file: { name: 'i.png', mimeType: 'image/png', buffer: png } } })
    expect(up.status()).toBe(200)
    const { url } = await up.json()
    void ws
    const ctx = await browser.newContext({ baseURL: origin() })
    expect([401, 403, 404]).toContain((await ctx.request.get(url)).status())
    await api.settings({ guest_access: true })
    expect((await ctx.request.get(url)).status()).toBe(200)
    await ctx.close()
  })
})
