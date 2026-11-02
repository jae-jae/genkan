import type { Page } from '@playwright/test'
import { expect, linkCap, openApp, openSettings, origin, pinCap, test, toggleEdit } from '../helpers'

const L = (id: string, title: string, extra: Record<string, unknown> = {}) => ({ id, workspace_id: 'ws1', title, url: `${origin()}/healthz?l=${id}`, ...extra })
const itemIds = (page: Page, menu: string) =>
  page.locator(`[data-testid="${menu}"] [role^="menuitem"]`).evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')))

/** A right-click spot on the page background (well away from the clock, search box and capsules). */
const BG = { x: 1100, y: 650 }

test.describe('page context menu (right-click on the background)', () => {
  test('admin items; Esc / outside click / scroll close it; stays inside the viewport', async ({ page, api }) => {
    await api.seed({ categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }], links: [L('a', 'Alpha', { category_id: 'c1' })] })
    await openApp(page)
    await page.mouse.click(BG.x, BG.y, { button: 'right' })
    const menu = page.getByTestId('ctx-menu-page')
    await expect(menu).toBeVisible()
    await expect(menu).toHaveAttribute('role', 'menu')
    expect(await itemIds(page, 'ctx-menu-page')).toEqual(['ctx-new-link', 'ctx-new-category', 'ctx-wallpaper', 'ctx-edit-mode', 'ctx-ai', 'ctx-settings'])
    await expect(page.getByTestId('ctx-new-link')).toBeFocused() // first item focused
    await expect(page.getByTestId('ctx-new-link')).toHaveText('New link')
    await expect(page.getByTestId('ctx-wallpaper')).toHaveText('Change wallpaper')
    await expect(page.getByTestId('ctx-edit-mode')).toHaveAttribute('role', 'menuitemcheckbox')
    await expect(page.getByTestId('ctx-edit-mode')).toHaveAttribute('aria-checked', 'false')
    // keyboard
    await page.keyboard.press('ArrowDown')
    await expect(page.getByTestId('ctx-new-category')).toBeFocused()
    await page.keyboard.press('End')
    await expect(page.getByTestId('ctx-settings')).toBeFocused()
    await page.keyboard.press('ArrowDown') // wraps
    await expect(page.getByTestId('ctx-new-link')).toBeFocused()
    await page.keyboard.press('Escape')
    await expect(menu).toHaveCount(0)

    // opened in the bottom-right corner: clamped into the viewport
    await page.mouse.click(1275, 795, { button: 'right' })
    await expect(menu).toBeVisible()
    const vp = page.viewportSize()!
    await expect // (poll: the menu rises in over ~140 ms)
      .poll(async () => {
        const b = (await menu.boundingBox())!
        return b.x >= 0 && b.y >= 0 && b.x + b.width <= vp.width && b.y + b.height <= vp.height
      })
      .toBe(true)
    await page.keyboard.press('Escape')
    await expect(menu).toHaveCount(0)

    // outside click closes
    await page.mouse.click(300, 300, { button: 'right' })
    await expect(menu).toBeVisible()
    const m1 = (await menu.boundingBox())!
    expect(Math.abs(m1.x - 300)).toBeLessThan(2) // placed at the cursor when there is room
    expect(Math.abs(m1.y - 300)).toBeLessThan(14) // (still rising in)
    await page.mouse.click(640, 100)
    await expect(menu).toHaveCount(0)
    // a second right-click reopens at the new spot (and only one menu exists)
    await page.mouse.click(300, 300, { button: 'right' })
    await page.mouse.click(500, 350, { button: 'right' })
    await expect(page.locator('[data-testid="ctx-menu-page"]')).toHaveCount(1)
    // resize closes
    await page.setViewportSize({ width: 1100, height: 800 })
    await expect(menu).toHaveCount(0)
  })

  test('items: new link, new category, change wallpaper -> Appearance, edit mode check, AI, settings', async ({ page, api, ws }) => {
    await openApp(page)
    const open = async () => {
      await page.mouse.click(BG.x, BG.y, { button: 'right' })
      await expect(page.getByTestId('ctx-menu-page')).toBeVisible()
    }
    // new link (defaults to the current workspace, uncategorised, not pinned)
    await open()
    await page.getByTestId('ctx-new-link').click()
    await expect(page.getByTestId('link-modal')).toBeVisible()
    await expect(page.getByTestId('link-workspace')).toHaveValue(ws)
    await expect(page.getByTestId('link-pinned')).toHaveAttribute('aria-checked', 'false')
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('link-modal')).toHaveCount(0)
    // new category
    await open()
    await page.getByTestId('ctx-new-category').click()
    await page.getByTestId('category-name').fill('From menu')
    await page.getByTestId('category-save').click()
    await expect.poll(async () => (await api.bootstrap()).categories.map((c) => c.name)).toEqual(['From menu'])
    // change wallpaper opens Settings on the Appearance tab
    await open()
    await page.getByTestId('ctx-wallpaper').click()
    await expect(page.getByTestId('settings-modal')).toBeVisible()
    await expect(page.getByTestId('settings-tab-appearance')).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('wp-picker')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('settings-modal')).toHaveCount(0)
    // edit mode toggles and shows its state
    await open()
    await page.getByTestId('ctx-edit-mode').click()
    await expect(page.getByTestId('edit-toggle')).toHaveAttribute('aria-pressed', 'true')
    await open()
    await expect(page.getByTestId('ctx-edit-mode')).toHaveAttribute('aria-checked', 'true')
    await page.getByTestId('ctx-edit-mode').click()
    await expect(page.getByTestId('edit-toggle')).toHaveAttribute('aria-pressed', 'false')
    // AI
    await open()
    await page.getByTestId('ctx-ai').click()
    await expect(page.getByTestId('settings-panel-ai')).toBeVisible()
    await page.keyboard.press('Escape')
    // settings
    await open()
    await page.getByTestId('ctx-settings').click()
    await expect(page.getByTestId('settings-tab-general')).toHaveAttribute('aria-selected', 'true')
  })

  test('native menu is kept on inputs and on selected text; no page menu on top of modals', async ({ page, api }) => {
    await api.seed({ links: [L('a', 'Alpha')] })
    await openApp(page)
    await page.getByTestId('search-input').click({ button: 'right' })
    await expect(page.getByTestId('ctx-menu-page')).toHaveCount(0)
    await page.getByTestId('search-input').fill('')
    await openSettings(page)
    await page.getByTestId('set-title').click({ button: 'right' })
    await expect(page.getByTestId('ctx-menu-page')).toHaveCount(0)
  })

  test('guest: the page menu has only "Log in"', async ({ page, api, browser }) => {
    await api.settings({ guest_access: true })
    await api.seed({ settings: { guest_access: true }, links: [L('a', 'Alpha')] })
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const g = await ctx.newPage()
    await g.goto('/')
    await expect(g.getByRole('link', { name: 'Alpha' })).toBeVisible()
    await g.mouse.click(BG.x, BG.y, { button: 'right' })
    await expect(g.getByTestId('ctx-menu-page')).toBeVisible()
    expect(await itemIds(g, 'ctx-menu-page')).toEqual(['ctx-login'])
    await g.getByTestId('ctx-login').click()
    await expect(g.getByTestId('login-form')).toBeVisible()
    await ctx.close()
    void page
  })
})

test.describe('capsule context menu', () => {
  test('admin: open, open in new tab, copy link | edit, pin/unpin, delete', async ({ page, api, context }) => {
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: origin() })
    await api.seed({ categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }], links: [L('a', 'Alpha', { category_id: 'c1', description: 'first' }), L('b', 'Beta', { category_id: 'c1' })] })
    await openApp(page)
    await linkCap(page, 'a').click({ button: 'right' })
    const menu = page.getByTestId('ctx-menu-link')
    await expect(menu).toBeVisible()
    await expect(page.getByTestId('ctx-menu-page')).toHaveCount(0)
    expect(await itemIds(page, 'ctx-menu-link')).toEqual(['ctx-open', 'ctx-open-new', 'ctx-copy-link', 'ctx-edit', 'ctx-pin', 'ctx-delete'])
    await expect(page.getByTestId('ctx-pin')).toHaveText('Pin')
    await expect(page.getByTestId('ctx-delete')).toHaveClass(/danger/)
    // no page navigation happened by merely right-clicking
    expect(page.url()).toBe(`${origin()}/`)

    // copy link
    await page.getByTestId('ctx-copy-link').click()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`${origin()}/healthz?l=a`)
    await expect(menu).toHaveCount(0)

    // open in new tab
    await linkCap(page, 'a').click({ button: 'right' })
    const popup = context.waitForEvent('page')
    await page.getByTestId('ctx-open-new').click()
    const tab = await popup
    await expect.poll(() => tab.url()).toContain('/healthz?l=a')
    await tab.close()
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'a')!.clicks).toBe(1) // opening counts as a click

    // edit
    await page.bringToFront()
    await linkCap(page, 'a').click({ button: 'right' })
    await page.getByTestId('ctx-edit').click()
    await expect(page.getByTestId('link-modal')).toBeVisible()
    await expect(page.getByTestId('link-title')).toHaveValue('Alpha')
    await page.getByTestId('link-title').fill('Alpha 2')
    await page.getByTestId('link-save').click()
    await expect(linkCap(page, 'a').locator('.cap-title')).toHaveText('Alpha 2')

    // pin -> appears in the pinned row and stays in the category; menu now says Unpin
    await linkCap(page, 'a').click({ button: 'right' })
    await page.getByTestId('ctx-pin').click()
    await expect(pinCap(page, 'a')).toBeVisible()
    await expect(linkCap(page, 'a')).toBeVisible()
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'a')!.pinned).toBe(true)
    await pinCap(page, 'a').click({ button: 'right' })
    await expect(page.getByTestId('ctx-pin')).toHaveText('Unpin')
    await page.getByTestId('ctx-pin').click()
    await expect(pinCap(page, 'a')).toHaveCount(0)
    await expect(linkCap(page, 'a')).toBeVisible()

    // delete asks for confirmation (cancel keeps it)
    await linkCap(page, 'b').click({ button: 'right' })
    await page.getByTestId('ctx-delete').click()
    await expect(page.getByTestId('confirm-modal')).toBeVisible()
    await page.getByTestId('confirm-cancel').click()
    await expect(linkCap(page, 'b')).toBeVisible()
    await linkCap(page, 'b').click({ button: 'right' })
    await page.getByTestId('ctx-delete').click()
    await page.getByTestId('confirm-ok').click()
    await expect(linkCap(page, 'b')).toHaveCount(0)
    await expect.poll(async () => (await api.links()).map((l) => l.id)).toEqual(['a'])
  })

  test('"open" navigates the current tab; Esc closes; works in edit mode too', async ({ page, api }) => {
    await api.seed({ settings: { open_in_new_tab: false }, links: [L('a', 'Alpha')] })
    await openApp(page)
    await linkCap(page, 'a').click({ button: 'right' })
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('ctx-menu-link')).toHaveCount(0)
    await toggleEdit(page)
    await linkCap(page, 'a').click({ button: 'right' })
    await expect(page.getByTestId('ctx-menu-link')).toBeVisible()
    await expect(page.getByTestId('link-modal')).toHaveCount(0) // right-click does not open the editor
    await page.keyboard.press('Escape')
    await toggleEdit(page, false)
    await linkCap(page, 'a').click({ button: 'right' })
    await page.getByTestId('ctx-open').click()
    await expect(page).toHaveURL(/\/healthz\?l=a/)
  })

  test('guest: only open / open in new tab / copy link', async ({ api, browser }) => {
    await api.seed({ settings: { guest_access: true }, links: [L('a', 'Alpha')] })
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const g = await ctx.newPage()
    await g.goto('/')
    await expect(linkCap(g, 'a')).toBeVisible()
    await linkCap(g, 'a').click({ button: 'right' })
    await expect(g.getByTestId('ctx-menu-link')).toBeVisible()
    expect(await itemIds(g, 'ctx-menu-link')).toEqual(['ctx-open', 'ctx-open-new', 'ctx-copy-link'])
    for (const id of ['ctx-edit', 'ctx-pin', 'ctx-delete']) await expect(g.getByTestId(id)).toHaveCount(0)
    await ctx.close()
  })
})
