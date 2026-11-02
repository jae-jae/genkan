import { expect, openApp, openMainMenu, openWsMenu, origin, setSort, test, toggleEdit, wsNames } from '../helpers'

const L = (id: string, title: string, extra: Record<string, unknown> = {}) => ({ id, workspace_id: 'ws1', title, url: `${origin()}/healthz?l=${id}`, ...extra })

test.describe('workspace dropdown (top-left)', () => {
  test.beforeEach(async ({ api }) => {
    await api.seed({
      workspaces: [
        { id: 'ws1', name: 'Home', icon: 'lucide:house' },
        { id: 'ws2', name: 'Lab', icon: 'lucide:flask-conical' },
        { id: 'ws3', name: 'Media', icon: 'lucide:clapperboard' },
      ],
      links: [L('a', 'Alpha'), L('b', 'Beta', { workspace_id: 'ws2' })],
    })
  })

  test('one capsule shows the current workspace; the menu lists all with shortcut hints and a check mark', async ({ page }) => {
    await openApp(page)
    const btn = page.getByTestId('ws-menu-btn')
    await expect(btn.locator('svg[data-icon="house"]')).toHaveCount(1) // flat Lucide icon, no emoji
    await expect(btn).toContainText('Home')
    await expect(page.locator('[data-testid^="ws-tab-"], [role="tab"]')).toHaveCount(0) // the old tab strip is gone
    await expect(btn).toHaveAttribute('aria-haspopup', 'menu')
    await expect(btn).toHaveAttribute('aria-expanded', 'false')
    await expect(page.getByTestId('ws-menu')).toHaveCount(0)

    await btn.click()
    await expect(btn).toHaveAttribute('aria-expanded', 'true')
    const menu = page.getByTestId('ws-menu')
    await expect(menu).toHaveAttribute('role', 'menu')
    const items = menu.locator('[role="menuitemradio"]')
    await expect(items).toHaveCount(3)
    await expect(items.nth(0)).toContainText('Home')
    await expect(items.nth(1).locator('svg[data-icon="flask-conical"]')).toHaveCount(1)
    await expect(items.nth(2).locator('svg[data-icon="clapperboard"]')).toHaveCount(1)
    await expect(items.nth(1).locator('.mi-key')).toHaveText('2')
    await expect(items.nth(2).locator('.mi-key')).toHaveText('3')
    await expect(items.nth(0)).toHaveAttribute('aria-checked', 'true')
    await expect(items.nth(1)).toHaveAttribute('aria-checked', 'false')
    await expect(items.nth(0).locator('svg[data-icon="check"]')).toHaveCount(1) // the check mark
    // not edit mode: no edit affordances, but "New workspace" / "Manage workspaces..." are always there for admins
    await expect(page.locator('[data-testid^="ws-edit-"], [data-testid^="ws-up-"]')).toHaveCount(0)
    await expect(page.getByTestId('ws-add')).toBeVisible()
    await expect(page.getByTestId('ws-manage')).toBeVisible()
  })

  test('selecting an item switches workspace, closes the menu and returns focus to the button', async ({ page }) => {
    await openApp(page)
    await openWsMenu(page)
    await page.getByTestId('ws-item-ws2').click()
    await expect(page.getByTestId('ws-menu')).toHaveCount(0)
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Lab')
    await expect(page.getByTestId('ws-menu-btn')).toBeFocused()
    await expect(page.getByRole('link', { name: 'Beta' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Alpha' })).toHaveCount(0)
    expect(await page.evaluate(() => localStorage.getItem('genkan.ws'))).toBe('ws2')
    // the choice survives a reload
    await page.reload()
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Lab')
  })

  test('closes on Esc (focus back on the button), outside click and re-click of the button', async ({ page }) => {
    await openApp(page)
    await openWsMenu(page)
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('ws-menu')).toHaveCount(0)
    await expect(page.getByTestId('ws-menu-btn')).toBeFocused()
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('aria-expanded', 'false')

    await openWsMenu(page)
    await page.mouse.click(640, 600) // empty page area
    await expect(page.getByTestId('ws-menu')).toHaveCount(0)

    await openWsMenu(page)
    await page.getByTestId('ws-menu-btn').click()
    await expect(page.getByTestId('ws-menu')).toHaveCount(0)

    // opening one menu closes the other
    await openWsMenu(page)
    await page.getByTestId('menu-btn').click()
    await expect(page.getByTestId('main-menu')).toBeVisible()
    await expect(page.getByTestId('ws-menu')).toHaveCount(0)
  })

  test('keyboard: ArrowDown opens, arrows/Home/End move, Enter selects', async ({ page }) => {
    await openApp(page)
    await page.getByTestId('ws-menu-btn').focus()
    await page.keyboard.press('ArrowDown')
    await expect(page.getByTestId('ws-menu')).toBeVisible()
    await expect(page.getByTestId('ws-item-ws1')).toBeFocused() // the checked item gets focus
    await page.keyboard.press('ArrowDown')
    await expect(page.getByTestId('ws-item-ws2')).toBeFocused()
    await page.keyboard.press('ArrowDown')
    await expect(page.getByTestId('ws-item-ws3')).toBeFocused()
    await page.keyboard.press('ArrowDown') // then the admin items: new workspace, manage
    await expect(page.getByTestId('ws-add')).toBeFocused()
    await page.keyboard.press('End')
    await expect(page.getByTestId('ws-manage')).toBeFocused()
    await page.keyboard.press('ArrowDown') // wraps
    await expect(page.getByTestId('ws-item-ws1')).toBeFocused()
    await page.keyboard.press('ArrowUp') // wraps back
    await expect(page.getByTestId('ws-manage')).toBeFocused()
    await page.keyboard.press('Home')
    await expect(page.getByTestId('ws-item-ws1')).toBeFocused()
    await page.keyboard.press('ArrowDown')
    await page.keyboard.press('Enter')
    await expect(page.getByTestId('ws-menu')).toHaveCount(0)
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Lab')
    await expect(page.getByTestId('ws-menu-btn')).toBeFocused()
  })

  test('digit shortcuts still switch workspaces', async ({ page }) => {
    await openApp(page)
    await page.keyboard.press('3')
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Media')
    await page.keyboard.press('1')
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Home')
  })

  test('edit mode: edit button opens the workspace editor, "New workspace" at the bottom, right-click edits', async ({ page, api }) => {
    await openApp(page)
    await toggleEdit(page)
    await openWsMenu(page)
    for (const id of ['ws1', 'ws2', 'ws3']) {
      await expect(page.getByTestId(`ws-edit-${id}`)).toBeVisible()
      await expect(page.getByTestId(`ws-up-${id}`)).toBeVisible()
      await expect(page.getByTestId(`ws-down-${id}`)).toBeVisible()
    }
    const last = page.locator('[data-testid="ws-menu"] [role^="menuitem"]').last()
    await expect(last).toHaveAttribute('data-testid', 'ws-manage') // add + manage are the last two items
    await expect(page.locator('[data-testid="ws-menu"] [role^="menuitem"]').nth(-2)).toHaveAttribute('data-testid', 'ws-add')
    await page.getByTestId('ws-edit-ws2').click()
    await expect(page.getByTestId('workspace-modal')).toBeVisible()
    await expect(page.getByTestId('ws-menu')).toHaveCount(0) // menu closed when the modal opens
    await expect(page.getByTestId('ws-name')).toHaveValue('Lab')
    await page.getByTestId('ws-name').fill('Laboratory')
    await page.getByTestId('ws-save').click()
    await expect(page.getByTestId('workspace-modal')).toHaveCount(0)
    expect(await wsNames(page)).toEqual(['Home', 'Laboratory', 'Media'])
    expect((await api.bootstrap()).workspaces.map((w) => w.name)).toEqual(['Home', 'Laboratory', 'Media'])
    // creating via the menu item
    await openWsMenu(page)
    await page.getByTestId('ws-add').click()
    await expect(page.getByTestId('workspace-modal')).toBeVisible()
    await page.getByTestId('ws-name').fill('Fresh')
    await page.getByTestId('ws-save').click()
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Fresh')
    // leaving edit mode removes the edit affordances again, but not the always-on admin items
    await toggleEdit(page, false)
    await openWsMenu(page)
    await expect(page.locator('[data-testid^="ws-edit-"]')).toHaveCount(0)
    await expect(page.getByTestId('ws-add')).toBeVisible()
  })

  test('the menu paints above the search box and results', async ({ page }) => {
    await openApp(page)
    await page.getByTestId('search-input').fill('a')
    await expect(page.getByTestId('search-results')).toBeVisible()
    await openWsMenu(page)
    const m = (await page.getByTestId('ws-menu').boundingBox())!
    const top = await page.evaluate(({ x, y }) => (document.elementFromPoint(x, y) as HTMLElement | null)?.closest('[data-testid="ws-menu"]') !== null, { x: m.x + m.width / 2, y: m.y + m.height - 6 })
    expect(top).toBe(true)
  })
})

test.describe('main menu (top-right)', () => {
  test('contains sort (radio), settings and logout; the old toolbar sort control and loose buttons are gone', async ({ page, api }) => {
    await api.seed({ links: [L('a', 'Alpha')] })
    await openApp(page)
    await expect(page.getByTestId('sort-control')).toHaveCount(0)
    await expect(page.getByTestId('settings-btn')).toHaveCount(0)
    await expect(page.getByTestId('logout-btn')).toHaveCount(0)
    const btn = page.getByTestId('menu-btn')
    await expect(btn).toHaveAttribute('aria-haspopup', 'menu')
    await expect(btn).toHaveAttribute('aria-expanded', 'false')
    // the AI button sits immediately left of the menu button
    const e = (await page.getByTestId('ai-btn').boundingBox())!
    const m = (await btn.boundingBox())!
    expect(e.x + e.width).toBeLessThanOrEqual(m.x)
    expect(m.x - (e.x + e.width)).toBeLessThan(20)

    await openMainMenu(page)
    const menu = page.getByTestId('main-menu')
    await expect(menu).toHaveAttribute('role', 'menu')
    await expect(page.getByTestId('sort-control')).toHaveAttribute('role', 'group')
    await expect(menu.locator('[role="menuitemradio"]')).toHaveCount(3)
    await expect(page.getByTestId('sort-frequency')).toHaveAttribute('aria-checked', 'true')
    await expect(menu.locator('[role="menuitem"]')).toHaveCount(2)
    await expect(page.getByTestId('settings-btn')).toBeVisible()
    await expect(page.getByTestId('logout-btn')).toBeVisible()

    // choosing a sort mode saves it and closes the menu
    await page.getByTestId('sort-recent').click()
    await expect(menu).toHaveCount(0)
    await expect(btn).toBeFocused()
    await expect.poll(async () => (await api.bootstrap()).settings.sort_mode).toBe('recent')
    await setSort(page, 'custom')
    await expect.poll(async () => (await api.bootstrap()).settings.sort_mode).toBe('custom')
  })

  test('closes on Esc and on outside click; keyboard navigation and Enter work', async ({ page }) => {
    await openApp(page)
    await openMainMenu(page)
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('main-menu')).toHaveCount(0)
    await expect(page.getByTestId('menu-btn')).toBeFocused()

    await openMainMenu(page)
    await page.mouse.click(300, 600)
    await expect(page.getByTestId('main-menu')).toHaveCount(0)

    await page.getByTestId('menu-btn').focus()
    await page.keyboard.press('ArrowDown')
    await expect(page.getByTestId('sort-frequency')).toBeFocused() // checked item
    await page.keyboard.press('ArrowDown')
    await expect(page.getByTestId('sort-recent')).toBeFocused()
    await page.keyboard.press('End')
    await expect(page.getByTestId('logout-btn')).toBeFocused()
    await page.keyboard.press('ArrowUp')
    await expect(page.getByTestId('settings-btn')).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(page.getByTestId('settings-modal')).toBeVisible()
    await expect(page.getByTestId('main-menu')).toHaveCount(0)
    // closing the modal returns focus to the menu button
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('settings-modal')).toHaveCount(0)
    await expect(page.getByTestId('menu-btn')).toBeFocused()
  })

  test('edit mode toolbar only keeps "New category"', async ({ page, api }) => {
    await api.seed({ links: [L('a', 'Alpha')] })
    await openApp(page)
    await expect(page.locator('.toolbar')).toHaveCount(0)
    await toggleEdit(page)
    await expect(page.locator('.toolbar > *')).toHaveCount(1)
    await expect(page.getByTestId('new-category-btn')).toBeVisible()
    await expect(page.getByTestId('sort-control')).toHaveCount(0)
  })

  test('logout from the menu', async ({ page }) => {
    await openApp(page)
    await openMainMenu(page)
    await page.getByTestId('logout-btn').click()
    await expect(page.getByTestId('login-form')).toBeVisible()
  })
})

test('search dropdown: the globe icon of the web-search row is vertically centred', async ({ page }) => {
  await openApp(page)
  await page.getByTestId('search-input').fill('zzzz-no-match')
  const row = page.getByTestId('search-web')
  await expect(row).toBeVisible()
  const ico = (await row.locator('.ico-web').boundingBox())!
  const svg = (await row.locator('.ico-web svg').boundingBox())!
  expect(Math.abs(ico.y + ico.height / 2 - (svg.y + svg.height / 2))).toBeLessThan(1)
  const title = (await row.locator('.result-main').boundingBox())!
  expect(Math.abs(ico.y + ico.height / 2 - (title.y + title.height / 2))).toBeLessThan(2)
})
