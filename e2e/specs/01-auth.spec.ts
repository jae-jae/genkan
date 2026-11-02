import { test as base, expect } from '@playwright/test'
import { PASSWORD, adminContext, mainMenuItem, openMainMenu, origin, test, CSRF, wsNames } from '../helpers'
import { request } from '@playwright/test'

// ---- first run: runs before anything else touches the instance ----
base.describe.serial('first run', () => {
  base('setup page: validation, then password setup logs in', async ({ browser }) => {
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const page = await ctx.newPage()
    await page.goto('/')
    const boot = await (await ctx.request.get('/api/bootstrap')).json()
    base.skip(!boot.auth.setup_required, 'instance already initialised')

    await expect(page.getByTestId('setup-form')).toBeVisible()
    // too short
    await page.getByTestId('setup-password').fill('abc')
    await page.getByTestId('setup-confirm').fill('abc')
    await page.getByTestId('setup-submit').click()
    await expect(page.getByTestId('auth-error')).toBeVisible()
    // mismatch
    await page.getByTestId('setup-password').fill(PASSWORD)
    await page.getByTestId('setup-confirm').fill(PASSWORD + 'x')
    await page.getByTestId('setup-submit').click()
    await expect(page.getByTestId('auth-error')).toBeVisible()
    await expect(page.getByTestId('setup-form')).toBeVisible()
    // success
    await page.getByTestId('setup-password').fill(PASSWORD)
    await page.getByTestId('setup-confirm').fill(PASSWORD)
    await page.getByTestId('setup-submit').click()
    await expect(page.getByTestId('ws-menu-btn')).toBeVisible()
    await openMainMenu(page)
    await expect(page.getByTestId('logout-btn')).toBeVisible()
    await page.keyboard.press('Escape')
    // default workspace exists, and setup cannot be repeated
    expect(await wsNames(page)).toEqual(['Home'])
    const again = await ctx.request.post('/api/auth/setup', { data: { password: 'another-pass' }, headers: CSRF })
    expect(again.ok()).toBe(false)
    await ctx.close()
  })
})

test.describe('login / logout', () => {
  test('wrong password shows an error and stays on login', async ({ browser, api }) => {
    void api
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const page = await ctx.newPage()
    await page.goto('/')
    await expect(page.getByTestId('login-form')).toBeVisible()
    await page.getByTestId('login-password').fill('definitely-wrong')
    await page.getByTestId('login-submit').click()
    await expect(page.getByTestId('auth-error')).toBeVisible()
    await expect(page.getByTestId('login-form')).toBeVisible()
    await expect(page.getByTestId('ws-menu-btn')).toHaveCount(0)
    await ctx.close()
  })

  test('login with correct password, session survives reload, logout returns to login', async ({ browser, api }) => {
    void api
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const page = await ctx.newPage()
    await page.goto('/')
    await page.getByTestId('login-password').fill(PASSWORD)
    await page.getByTestId('login-submit').click()
    await expect(page.getByTestId('ws-menu-btn')).toBeVisible()
    const cookie = (await ctx.cookies()).find((c) => c.name === 'genkan_session')!
    expect(cookie.httpOnly).toBe(true)
    expect(cookie.sameSite).toBe('Lax')

    await page.reload()
    await expect(page.getByTestId('ws-menu-btn')).toBeVisible()

    await mainMenuItem(page, 'logout-btn')
    await expect(page.getByTestId('login-form')).toBeVisible()
    await expect(page.getByTestId('ws-menu-btn')).toHaveCount(0)
    await page.reload()
    await expect(page.getByTestId('login-form')).toBeVisible()
    await ctx.close()
  })

  test('Enter key submits the login form', async ({ browser, api }) => {
    void api
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const page = await ctx.newPage()
    await page.goto('/')
    await page.getByTestId('login-password').fill(PASSWORD)
    await page.getByTestId('login-password').press('Enter')
    await expect(page.getByTestId('ws-menu-btn')).toBeVisible()
    await ctx.close()
  })
})

test.describe('API auth & CSRF', () => {
  test('unauthenticated API access is rejected with 401', async ({ api }) => {
    void api
    const anon = await request.newContext({ baseURL: origin() })
    for (const [m, p] of [
      ['GET', '/api/health'],
      ['GET', '/api/export'],
      ['GET', '/api/token'],
      ['GET', '/api/skill'],
      ['POST', '/api/token/regenerate'],
      ['GET', '/api/events'],
      ['POST', '/api/links'],
      ['PATCH', '/api/settings'],
      ['POST', '/api/workspaces'],
      ['DELETE', '/api/workspaces/abc'],
      ['POST', '/api/import'],
    ] as const) {
      const r = await anon.fetch(p, { method: m, headers: CSRF, data: m === 'GET' ? undefined : {} })
      expect(r.status(), `${m} ${p}`).toBe(401)
      expect((await r.json()).error).toBeTruthy()
    }
    // bootstrap stays public but reveals nothing
    const b = await (await anon.get('/api/bootstrap')).json()
    expect(b.auth.role).toBe('none')
    expect(b.links).toEqual([])
    expect(b.workspaces).toEqual([])
    expect(Object.keys(b.settings)).toEqual(['title'])
    expect((await anon.get('/healthz')).status()).toBe(200)
    // wrong bearer token
    const bad = await anon.get('/api/export', { headers: { Authorization: 'Bearer gk_notarealtoken' } })
    expect(bad.status()).toBe(401)
    await anon.dispose()
  })

  test('cookie-authenticated writes without X-Requested-With are 403', async ({ api, ws }) => {
    const cookies = (await api.ctx.storageState()).cookies
    const raw = await request.newContext({ baseURL: origin() })
    const cookieHeader = cookies.map((c) => `${c.name}=${c.value}`).join('; ')
    const create = await raw.post('/api/links', { headers: { Cookie: cookieHeader }, data: { workspace_id: ws, title: 'x', url: 'http://example.com' } })
    expect(create.status()).toBe(403)
    const patch = await raw.patch('/api/settings', { headers: { Cookie: cookieHeader }, data: { title: 'pwned' } })
    expect(patch.status()).toBe(403)
    const del = await raw.delete(`/api/workspaces/${ws}`, { headers: { Cookie: cookieHeader } })
    expect(del.status()).toBe(403)
    // reads do not need the header; with the header the same write works
    expect((await raw.get('/api/export', { headers: { Cookie: cookieHeader } })).status()).toBe(200)
    const ok = await raw.post('/api/links', { headers: { Cookie: cookieHeader, ...CSRF }, data: { workspace_id: ws, title: 'x', url: 'http://example.com', fetch_icon: false } })
    expect(ok.status()).toBe(200)
    expect((await api.bootstrap()).settings.title).toBe('Genkan')
    await raw.dispose()
  })
})
