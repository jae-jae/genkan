import type { Locator } from '@playwright/test'
import { expect, openApp, openMainMenu, openSettings, openWsMenu, origin, switchWs, test, toggleEdit } from '../helpers'

/** Wait until an element stopped moving (slide-in animation finished). */
async function settled(l: Locator) {
  let last = ''
  await expect
    .poll(async () => {
      const b = JSON.stringify(await l.boundingBox())
      const same = b === last
      last = b
      return same
    })
    .toBe(true)
}

test.describe('mobile viewport (390x844)', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 })

  test('layout sanity: no horizontal overflow, two-column grid, header dropdowns fit, bottom-sheet modal', async ({ page, api }) => {
    await api.seed({
      workspaces: ['Home', 'Media', 'Dev', 'Network', 'Cloud', 'Archive'].map((n, i) => ({ id: 'w' + i, name: n + ' space', icon: 'lucide:house' })),
      categories: [{ id: 'c1', workspace_id: 'w0', name: 'Infrastructure' }],
      links: ['Proxmox', 'TrueNAS', 'Home Assistant', 'Jellyfin', 'Portainer', 'A very long service title that must truncate nicely'].map((t, i) => ({
        id: 'l' + i, workspace_id: 'w0', category_id: 'c1', title: t, url: `${origin()}/healthz?l=${i}`,
      })),
    })
    await openApp(page)
    // no horizontal scrolling of the page
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(overflow).toBeLessThanOrEqual(0)
    // the workspace dropdown fits the viewport and lists every workspace
    await openWsMenu(page)
    const wm = (await page.getByTestId('ws-menu').boundingBox())!
    expect(wm.x).toBeGreaterThanOrEqual(0)
    expect(wm.x + wm.width).toBeLessThanOrEqual(390)
    expect(wm.y + wm.height).toBeLessThanOrEqual(844)
    for (let i = 0; i < 6; i++) await expect(page.getByTestId(`ws-item-w${i}`)).toBeInViewport()
    await page.getByTestId('ws-item-w3').click()
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Network space')
    await switchWs(page, 'w0')
    // search is above the board
    const sb = (await page.getByTestId('search-input').boundingBox())!
    const bb = (await page.getByTestId('board').boundingBox())!
    expect(sb.y).toBeLessThan(bb.y)
    expect(sb.x).toBeGreaterThanOrEqual(0)
    expect(sb.x + sb.width).toBeLessThanOrEqual(390)
    // two columns of capsules, all inside the viewport
    const boxes = await Promise.all([0, 1, 2, 3].map((i) => page.getByTestId(`link-l${i}`).boundingBox()))
    const xs = new Set(boxes.map((b) => Math.round(b!.x)))
    expect(xs.size).toBe(2)
    for (const b of boxes) expect(b!.x + b!.width).toBeLessThanOrEqual(390 + 0.5)
    const long = (await page.getByTestId('link-l5').boundingBox())!
    expect(long.x + long.width).toBeLessThanOrEqual(390 + 0.5)
    expect(long.height).toBeLessThan(80) // truncated, not wrapped to many lines
    // topbar actions stay reachable
    for (const id of ['ws-menu-btn', 'edit-toggle', 'menu-btn']) {
      const b = (await page.getByTestId(id).boundingBox())!
      expect(b.x + b.width, id).toBeLessThanOrEqual(390)
      expect(b.x, id).toBeGreaterThanOrEqual(0)
    }
    await openMainMenu(page)
    for (const id of ['sort-frequency', 'sort-recent', 'sort-custom', 'settings-btn', 'logout-btn']) {
      const b = (await page.getByTestId(id).boundingBox())!
      expect(b.x + b.width, id).toBeLessThanOrEqual(390)
      expect(b.x, id).toBeGreaterThanOrEqual(0)
      expect(b.y + b.height, id).toBeLessThanOrEqual(844)
    }
    await page.keyboard.press('Escape')
    // modal becomes a bottom sheet
    await openSettings(page)
    await settled(page.getByTestId('settings-modal'))
    const m = (await page.getByTestId('settings-modal').boundingBox())!
    expect(m.width).toBeLessThanOrEqual(390)
    expect(m.y + m.height).toBeGreaterThan(844 - 4) // anchored to the bottom
    expect(m.y).toBeGreaterThan(0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
  })

  test('search on mobile, edit mode and link editor sheet fit the screen', async ({ page, api }) => {
    await api.seed({ links: [{ id: 'a', workspace_id: 'ws1', title: 'Alpha', url: `${origin()}/healthz?l=a` }] })
    await openApp(page)
    await page.getByTestId('search-input').fill('alp')
    await expect(page.getByTestId('search-result')).toHaveCount(1)
    const r = (await page.getByTestId('search-results').boundingBox())!
    expect(r.x).toBeGreaterThanOrEqual(0)
    expect(r.x + r.width).toBeLessThanOrEqual(390)
    await page.getByTestId('search-input').fill('')
    await toggleEdit(page)
    await page.getByTestId('link-edit-a').click()
    await settled(page.getByTestId('link-modal'))
    const m = (await page.getByTestId('link-modal').boundingBox())!
    expect(m.x).toBeGreaterThanOrEqual(0)
    expect(m.x + m.width).toBeLessThanOrEqual(390)
    expect(m.y + m.height).toBeLessThanOrEqual(844 + 1)
    await expect(page.getByTestId('link-save')).toBeInViewport()
  })

  test('swipe left/right changes workspace', async ({ page, api }) => {
    await api.seed({
      workspaces: [{ id: 'w1', name: 'One' }, { id: 'w2', name: 'Two' }],
      links: [
        { id: 'a', workspace_id: 'w1', title: 'In One', url: 'http://one.lan' },
        { id: 'b', workspace_id: 'w2', title: 'In Two', url: 'http://two.lan' },
      ],
    })
    await openApp(page)
    const cur = page.getByTestId('ws-menu-btn')
    await expect(cur).toHaveAttribute('data-ws-name', 'One')
    const swipe = async (dx: number) => {
      await page.evaluate((dx) => {
        const el = document.querySelector('.board') as HTMLElement
        const r = el.getBoundingClientRect()
        const y = r.top + 40
        const x0 = dx < 0 ? 300 : 80
        const mk = (type: string, x: number) =>
          new TouchEvent(type, { bubbles: true, cancelable: true, touches: type === 'touchend' ? [] : [new Touch({ identifier: 1, target: el, clientX: x, clientY: y })], changedTouches: [new Touch({ identifier: 1, target: el, clientX: x, clientY: y })] })
        el.dispatchEvent(mk('touchstart', x0))
        el.dispatchEvent(mk('touchend', x0 + dx))
      }, dx)
    }
    await swipe(-200)
    await expect(cur).toHaveAttribute('data-ws-name', 'Two')
    await expect(page.getByRole('link', { name: 'In Two' })).toBeVisible()
    await swipe(200)
    await expect(cur).toHaveAttribute('data-ws-name', 'One')
  })
})

test.describe('language', () => {
  test('Auto (default) follows a zh-CN browser; Settings switches explicit/auto, persisted', async ({ browser, admin, api }) => {
    await api.seed({ links: [{ id: 'a', workspace_id: 'ws1', title: 'Alpha', url: 'http://a.lan' }] })
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'zh-CN' })
    await ctx.addCookies((await admin.storageState()).cookies)
    const page = await ctx.newPage()
    await page.goto('/')
    await expect(page.getByTestId('ws-menu-btn')).toBeVisible()
    await expect(page.getByTestId('search-input')).toHaveAttribute('placeholder', /搜索/)
    await openMainMenu(page)
    await expect(page.getByTestId('sort-frequency')).toHaveText('频率')
    await expect(page.getByTestId('settings-btn')).toHaveText('设置')
    await expect(page.getByTestId('logout-btn')).toHaveText('退出登录')
    await expect(page.getByTestId('main-menu')).toContainText('排序')
    await page.keyboard.press('Escape')
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')

    await openSettings(page)
    const sel = page.getByTestId('set-lang')
    await expect(sel).toHaveValue('auto')
    await expect(sel.locator('option')).toHaveText(['自动（跟随浏览器）', '简体中文', 'English'])
    await sel.selectOption('en')
    await expect(sel).toHaveValue('en')
    await expect(sel.locator('option')).toHaveText(['Auto (follow browser)', '简体中文', 'English'])
    await page.getByTestId('modal-close').click()
    await expect(page.getByTestId('search-input')).toHaveAttribute('placeholder', /Search/)
    await expect(page.locator('html')).toHaveAttribute('lang', 'en')
    await openMainMenu(page)
    await expect(page.getByTestId('sort-frequency')).toHaveText(/Frequent/i)
    await expect(page.getByTestId('main-menu')).toContainText('Sort')
    await expect(page.getByTestId('logout-btn')).toHaveText('Sign out')
    await page.keyboard.press('Escape')
    expect(await page.evaluate(() => localStorage.getItem('genkan.lang'))).toBe('en')
    await page.reload()
    await expect(page.getByTestId('search-input')).toHaveAttribute('placeholder', /Search/)

    // back to Auto: follows the (Chinese) browser again, persisted as "auto"
    await openSettings(page)
    await page.getByTestId('set-lang').selectOption('auto')
    await page.getByTestId('modal-close').click()
    await expect(page.getByTestId('search-input')).toHaveAttribute('placeholder', /搜索/)
    expect(await page.evaluate(() => localStorage.getItem('genkan.lang'))).toBe('auto')
    await page.reload()
    await expect(page.getByTestId('search-input')).toHaveAttribute('placeholder', /搜索/)
    await openSettings(page)
    await expect(page.getByTestId('set-lang')).toHaveValue('auto')

    // explicit Chinese
    await page.getByTestId('set-lang').selectOption('zh')
    await page.getByTestId('modal-close').click()
    await expect(page.getByTestId('search-input')).toHaveAttribute('placeholder', /搜索/)
    expect(await page.evaluate(() => localStorage.getItem('genkan.lang'))).toBe('zh')
    // no raw i18n keys leak into the UI
    const text = await page.locator('body').innerText()
    expect(text).not.toMatch(/\b(top|set|sort|link|cat|ws|mcp|data|empty|common|auth)\.[a-zA-Z]+\b/)
    await ctx.close()
  })

  test('Auto with an en-US browser is English; an explicit choice overrides it and Auto goes back', async ({ page }) => {
    await openApp(page)
    await expect(page.getByTestId('search-input')).toHaveAttribute('placeholder', /Search/)
    await expect(page.locator('html')).toHaveAttribute('lang', 'en')
    await openSettings(page)
    await expect(page.getByTestId('set-lang')).toHaveValue('auto')
    await expect(page.getByTestId('set-lang').locator('option')).toHaveText(['Auto (follow browser)', '简体中文', 'English'])
    await page.getByTestId('set-lang').selectOption('zh')
    await expect(page.getByTestId('set-lang').locator('option')).toHaveText(['自动（跟随浏览器）', '简体中文', 'English'])
    await page.getByTestId('modal-close').click()
    await expect(page.getByTestId('search-input')).toHaveAttribute('placeholder', /搜索/)
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
    await page.reload()
    await expect(page.getByTestId('search-input')).toHaveAttribute('placeholder', /搜索/)
    await openSettings(page)
    await page.getByTestId('set-lang').selectOption('auto')
    await page.getByTestId('modal-close').click()
    await expect(page.getByTestId('search-input')).toHaveAttribute('placeholder', /Search/)
    await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  })

  test('Auto reacts to the browser languagechange event; priority order and unsupported languages', async ({ page }) => {
    await page.addInitScript(() => {
      const w = window as any
      w.__langs = ['en-US']
      Object.defineProperty(navigator, 'languages', { get: () => w.__langs, configurable: true })
      Object.defineProperty(navigator, 'language', { get: () => w.__langs[0], configurable: true })
    })
    const setBrowser = (langs: string[]) =>
      page.evaluate((l) => {
        ;(window as any).__langs = l
        window.dispatchEvent(new Event('languagechange'))
      }, langs)
    const ph = page.getByTestId('search-input')
    await openApp(page)
    await expect(ph).toHaveAttribute('placeholder', /Search/)
    await setBrowser(['zh-TW', 'en'])
    await expect(ph).toHaveAttribute('placeholder', /搜索/)
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
    await setBrowser(['fr-FR', 'zh-CN'])
    await expect(ph).toHaveAttribute('placeholder', /搜索/)
    await setBrowser(['en-GB', 'zh-CN']) // the first supported language wins
    await expect(ph).toHaveAttribute('placeholder', /Search/)
    await setBrowser(['zh-CN'])
    await expect(ph).toHaveAttribute('placeholder', /搜索/)
    await setBrowser(['de-DE']) // unsupported -> English
    await expect(ph).toHaveAttribute('placeholder', /Search/)
    // an explicit choice ignores the browser
    await setBrowser(['zh-CN'])
    await expect(ph).toHaveAttribute('placeholder', /搜索/)
    await openSettings(page)
    await page.getByTestId('set-lang').selectOption('en')
    await page.getByTestId('modal-close').click()
    await setBrowser(['zh-CN'])
    await expect(ph).toHaveAttribute('placeholder', /Search/)
  })

  test('stored values: legacy zh/en are kept, a missing or invalid value means Auto', async ({ browser, admin, api }) => {
    void api
    const run = async (locale: string, stored: string | null, expectZh: boolean, value: string) => {
      const ctx = await browser.newContext({ baseURL: origin(), locale })
      await ctx.addCookies((await admin.storageState()).cookies)
      if (stored !== null) await ctx.addInitScript((v) => localStorage.setItem('genkan.lang', v), stored)
      const page = await ctx.newPage()
      await page.goto('/')
      await expect(page.getByTestId('ws-menu-btn')).toBeVisible()
      await expect(page.getByTestId('search-input')).toHaveAttribute('placeholder', expectZh ? /搜索/ : /Search/)
      await openSettings(page)
      await expect(page.getByTestId('set-lang')).toHaveValue(value)
      await ctx.close()
    }
    await run('en-US', 'zh', true, 'zh') // existing explicit choice survives
    await run('zh-CN', 'en', false, 'en')
    await run('zh-CN', 'garbage', true, 'auto')
    await run('en-US', 'garbage', false, 'auto')
    await run('zh-CN', null, true, 'auto')
  })

  test('English default for en-US, login screen is translated too', async ({ browser, api }) => {
    void api
    for (const [locale, re] of [['en-US', /Sign in/i], ['zh-CN', /登录/]] as const) {
      const ctx = await browser.newContext({ baseURL: origin(), locale })
      const page = await ctx.newPage()
      await page.goto('/')
      await expect(page.getByTestId('login-form')).toBeVisible()
      await expect(page.getByTestId('login-submit')).toHaveText(re)
      await ctx.close()
    }
  })
})
