import { expect, openApp, openSettings, openWsMenu, origin, test } from '../helpers'

const L = (id: string, title: string, extra: Record<string, unknown> = {}) => ({ id, workspace_id: 'ws1', title, url: `${origin()}/healthz?l=${id}`, ...extra })

test.describe('workspace menu: always-on admin items', () => {
  test('"New workspace" and "Manage workspaces..." are always there (no edit mode needed); manage opens the Content tab', async ({ page, api }) => {
    await openApp(page)
    await expect(page.getByTestId('edit-toggle')).toHaveAttribute('aria-pressed', 'false')
    await openWsMenu(page)
    await expect(page.getByTestId('ws-add')).toHaveText('New workspace')
    await expect(page.getByTestId('ws-manage')).toHaveText('Manage workspaces…')
    // divider before the two items, "manage" last
    await expect(page.locator('[data-testid="ws-menu"] [role="separator"]')).toHaveCount(1)
    // create a workspace right from the menu
    await page.getByTestId('ws-add').click()
    await expect(page.getByTestId('workspace-modal')).toBeVisible()
    await page.getByTestId('ws-name').fill('Fresh')
    await page.getByTestId('ws-icon-rocket').click()
    await page.getByTestId('ws-save').click()
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Fresh')
    await expect(page.getByTestId('ws-menu-btn').locator('svg[data-icon="rocket"]')).toHaveCount(1)
    await expect.poll(async () => (await api.bootstrap()).workspaces.map((w) => [w.name, w.icon])).toEqual([['Home', 'lucide:house'], ['Fresh', 'lucide:rocket']])
    // manage
    await openWsMenu(page)
    await page.getByTestId('ws-manage').click()
    await expect(page.getByTestId('settings-modal')).toBeVisible()
    await expect(page.getByTestId('settings-tab-content')).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('content-manager')).toBeVisible()
    await expect(page.getByTestId('ws-menu')).toHaveCount(0)
  })

  test('the menu items are in Chinese too; guests do not get them', async ({ page, api, browser }) => {
    await page.addInitScript(() => localStorage.setItem('genkan.lang', 'zh'))
    await openApp(page)
    await openWsMenu(page)
    await expect(page.getByTestId('ws-add')).toHaveText('新建工作空间')
    await expect(page.getByTestId('ws-manage')).toHaveText('管理工作空间…')

    await api.settings({ guest_access: true })
    const ctx = await browser.newContext({ baseURL: origin() })
    const g = await ctx.newPage()
    await g.goto('/')
    await openWsMenu(g)
    await expect(g.getByTestId('ws-add')).toHaveCount(0)
    await expect(g.getByTestId('ws-manage')).toHaveCount(0)
    await expect(g.getByTestId('ai-btn')).toHaveCount(0)
    await ctx.close()
  })
})

test.describe('content manager (Settings > Content)', () => {
  test('workspace CRUD: add (with icon), edit, reorder, delete with confirmation', async ({ page, api }) => {
    await api.seed({
      workspaces: [{ id: 'w1', name: 'Alpha', icon: 'lucide:house' }, { id: 'w2', name: 'Beta', icon: 'lucide:code' }],
      links: [L('a', 'In Alpha', { workspace_id: 'w1' }), L('b', 'In Beta 1', { workspace_id: 'w2' }), L('c', 'In Beta 2', { workspace_id: 'w2' })],
    })
    await openApp(page)
    await openSettings(page, 'content')
    await expect(page.getByTestId('content-manager')).toBeVisible()
    // list with icon, name and link count
    const ws = page.getByTestId('cm-ws-list').locator('[data-testid^="cm-ws-"]')
    await expect(ws).toHaveCount(2)
    await expect(page.getByTestId('cm-ws-w1')).toContainText('Alpha')
    await expect(page.getByTestId('cm-ws-w1')).toContainText('1')
    await expect(page.getByTestId('cm-ws-w2')).toContainText('2')
    await expect(page.getByTestId('cm-ws-w2').locator('svg[data-icon="code"]')).toHaveCount(1)
    // the current workspace is preselected
    await expect(page.getByTestId('cm-ws-w1')).toHaveAttribute('aria-current', 'true')
    await expect(page.getByTestId('cm-ws-title')).toHaveText('Alpha')
    await page.getByTestId('cm-ws-w2').click()
    await expect(page.getByTestId('cm-ws-title')).toHaveText('Beta')
    await expect(page.getByTestId('cm-ws-w2')).toHaveAttribute('aria-current', 'true')

    // add: editor opens on top of Settings, closing it returns to the Content tab
    await page.getByTestId('cm-ws-add').click()
    await expect(page.getByTestId('workspace-modal')).toBeVisible()
    await page.getByTestId('ws-name').fill('Gamma')
    await page.getByTestId('ws-icon-search').fill('film')
    await expect(page.locator('[data-testid^="ws-icon-"][role="radio"]')).toHaveCount(1) // only "film" matches
    await page.getByTestId('ws-icon-film').click()
    await page.getByTestId('ws-save').click()
    await expect(page.getByTestId('workspace-modal')).toHaveCount(0)
    await expect(page.getByTestId('settings-modal')).toBeVisible()
    await expect(page.getByTestId('content-manager')).toBeVisible()
    await expect(ws).toHaveCount(3)
    await expect(page.getByTestId('cm-ws-title')).toHaveText('Gamma') // the new one is selected
    expect((await api.bootstrap()).workspaces.map((w) => [w.name, w.icon])).toEqual([['Alpha', 'lucide:house'], ['Beta', 'lucide:code'], ['Gamma', 'lucide:film']])

    // edit (rename + new icon); reflected in the top bar menu live
    await page.getByTestId('cm-ws-edit').click()
    await page.getByTestId('ws-name').fill('Movies')
    await page.getByTestId('ws-icon-search').fill('clapper')
    await page.getByTestId('ws-icon-clapperboard').click()
    await page.getByTestId('ws-save').click()
    await expect(page.getByTestId('cm-ws-title')).toHaveText('Movies')
    await expect(page.getByTestId('cm-ws-list').locator('svg[data-icon="clapperboard"]')).toHaveCount(1)

    // move up / down
    await expect(page.getByTestId('cm-ws-down')).toBeDisabled() // last one
    await page.getByTestId('cm-ws-up').click()
    await expect.poll(async () => (await api.bootstrap()).workspaces.map((w) => w.name)).toEqual(['Alpha', 'Movies', 'Beta'])
    await page.getByTestId('cm-ws-up').click()
    await expect.poll(async () => (await api.bootstrap()).workspaces.map((w) => w.name)).toEqual(['Movies', 'Alpha', 'Beta'])
    await expect(page.getByTestId('cm-ws-up')).toBeDisabled()
    await page.getByTestId('cm-ws-down').click()
    await expect.poll(async () => (await api.bootstrap()).workspaces.map((w) => w.name)).toEqual(['Alpha', 'Movies', 'Beta'])

    // delete asks first; links of the workspace go with it
    await page.getByTestId('cm-ws-w2').click()
    await page.getByTestId('cm-ws-delete').click()
    await page.getByTestId('confirm-cancel').click()
    expect((await api.bootstrap()).workspaces).toHaveLength(3)
    await page.getByTestId('cm-ws-delete').click()
    await page.getByTestId('confirm-ok').click()
    await expect(ws).toHaveCount(2)
    await expect.poll(async () => (await api.bootstrap()).workspaces.map((w) => w.name)).toEqual(['Alpha', 'Movies'])
    await expect.poll(async () => (await api.links()).map((l) => l.id)).toEqual(['a'])
    // the last remaining workspace can't be deleted: the button is disabled
    await page.getByTestId('cm-ws-title').click() // no-op, keeps focus in the pane
    await page.getByTestId('cm-ws-list').locator('[data-ws-name="Movies"]').click()
    await page.getByTestId('cm-ws-delete').click()
    await page.getByTestId('confirm-ok').click()
    await expect(ws).toHaveCount(1)
    await expect(page.getByTestId('cm-ws-title')).toHaveText('Alpha')
    await expect(page.getByTestId('cm-ws-delete')).toBeDisabled()
    await expect.poll(async () => (await api.bootstrap()).workspaces.map((w) => w.name)).toEqual(['Alpha'])
  })

  test('categories: add, rename, reorder, collapse, delete (links become uncategorised)', async ({ page, api }) => {
    await api.seed({
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'One' }, { id: 'c2', workspace_id: 'ws1', name: 'Two' }],
      links: [L('a', 'A', { category_id: 'c1' }), L('b', 'B', { category_id: 'c2' }), L('u', 'Loose')],
    })
    await openApp(page)
    await openSettings(page, 'content')
    await expect(page.getByTestId('cm-cat-c1')).toContainText('One')
    await expect(page.getByTestId('cm-cat-c2')).toContainText('Two')
    await expect(page.getByTestId('cm-cat-uncat')).toContainText('Loose')
    await expect(page.getByTestId('cm-link-a')).toBeVisible()
    // collapse / expand
    await expect(page.getByTestId('cm-toggle-c1')).toHaveAttribute('aria-expanded', 'true')
    await page.getByTestId('cm-toggle-c1').click()
    await expect(page.getByTestId('cm-toggle-c1')).toHaveAttribute('aria-expanded', 'false')
    await expect(page.getByTestId('cm-link-a')).toHaveCount(0)
    await page.getByTestId('cm-toggle-c1').click()
    await expect(page.getByTestId('cm-link-a')).toBeVisible()

    // new category
    await page.getByTestId('cm-cat-add').click()
    await page.getByTestId('category-name').fill('Three')
    await page.getByTestId('category-save').click()
    await expect(page.getByTestId('category-modal')).toHaveCount(0)
    await expect.poll(async () => (await api.bootstrap()).categories.map((c) => c.name)).toEqual(['One', 'Two', 'Three'])
    const c3 = (await api.bootstrap()).categories.find((c) => c.name === 'Three')!
    await expect(page.getByTestId(`cm-cat-${c3.id}`)).toContainText('Three')
    // rename
    await page.getByTestId('cm-cat-rename-c1').click()
    await page.getByTestId('category-name').fill('Uno')
    await page.getByTestId('category-save').click()
    await expect(page.getByTestId('cm-cat-c1')).toContainText('Uno')
    // reorder: the first can't go up, the last can't go down
    await expect(page.getByTestId('cm-cat-up-c1')).toBeDisabled()
    await expect(page.getByTestId(`cm-cat-down-${c3.id}`)).toBeDisabled()
    await page.getByTestId('cm-cat-down-c1').click()
    await expect.poll(async () => (await api.bootstrap()).categories.sort((x, y) => x.sort - y.sort).map((c) => c.name)).toEqual(['Two', 'Uno', 'Three'])
    // the board follows live (close Settings and look)
    await page.getByTestId('modal-close').click()
    await expect(page.getByTestId('category-c2-title')).toHaveText('Two')
    await expect(page.getByTestId('category-c1-title')).toHaveText('Uno')
    expect((await page.getByTestId('category-c2').boundingBox())!.y).toBeLessThan((await page.getByTestId('category-c1').boundingBox())!.y)
    await openSettings(page, 'content')
    // delete: confirm, links move to "uncategorised"
    await page.getByTestId('cm-cat-delete-c1').click()
    await page.getByTestId('confirm-ok').click()
    await expect(page.getByTestId('cm-cat-c1')).toHaveCount(0)
    await expect(page.getByTestId('cm-cat-uncat').getByTestId('cm-link-a')).toBeVisible()
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'a')!.category_id).toBe('')
  })

  test('links: add (per category), edit, pin toggle (star), delete; pin shows on the board live', async ({ page, api }) => {
    await api.seed({
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: [L('a', 'Alpha', { category_id: 'c1', description: 'the first one' }), L('b', 'Beta', { category_id: 'c1' })],
    })
    await openApp(page)
    await openSettings(page, 'content')
    // rows show title, description (muted) and url
    const row = page.getByTestId('cm-link-a')
    await expect(row).toContainText('Alpha')
    await expect(row.locator('.cm-desc')).toHaveText('the first one')
    await expect(row.locator('.cm-url')).toContainText('/healthz?l=a')

    // pin toggle
    const pin = page.getByTestId('cm-pin-a')
    await expect(pin).toHaveAttribute('aria-pressed', 'false')
    await pin.click()
    await expect(pin).toHaveAttribute('aria-pressed', 'true')
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'a')!.pinned).toBe(true)
    await expect(page.getByTestId('pin-link-a')).toBeVisible() // the board behind the modal follows
    await pin.click()
    await expect(pin).toHaveAttribute('aria-pressed', 'false')
    await expect(page.getByTestId('pin-link-a')).toHaveCount(0)

    // add a link into the category (editor stacked over Settings)
    await page.getByTestId('cm-add-link-c1').click()
    await expect(page.getByTestId('link-modal')).toBeVisible()
    await expect(page.getByTestId('link-category')).toHaveValue('c1')
    await page.getByTestId('icon-mode-letter').click()
    await page.getByTestId('link-url').fill('http://new.lan')
    await page.getByTestId('link-title').fill('Newbie')
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId('link-modal')).toHaveCount(0)
    await expect(page.getByTestId('content-manager')).toBeVisible()
    await expect(page.getByTestId('cm-cat-c1')).toContainText('Newbie')
    await expect.poll(async () => (await api.links()).find((l) => l.title === 'Newbie')).toMatchObject({ category_id: 'c1', workspace_id: 'ws1' })
    // add into "uncategorised" via the toolbar button
    await page.getByTestId('cm-link-add').click()
    await page.getByTestId('icon-mode-letter').click()
    await page.getByTestId('link-url').fill('http://loose.lan')
    await page.getByTestId('link-title').fill('Loose one')
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId('cm-cat-uncat')).toContainText('Loose one')

    // edit
    await page.getByTestId('cm-link-edit-b').click()
    await page.getByTestId('link-title').fill('Beta 2')
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId('cm-link-b')).toContainText('Beta 2')
    // Esc closes only the editor, not Settings
    await page.getByTestId('cm-link-edit-b').click()
    await expect(page.getByTestId('link-modal')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('link-modal')).toHaveCount(0)
    await expect(page.getByTestId('settings-modal')).toBeVisible()

    // delete (cancel keeps)
    await page.getByTestId('cm-link-delete-b').click()
    await page.getByTestId('confirm-cancel').click()
    await expect(page.getByTestId('cm-link-b')).toBeVisible()
    await page.getByTestId('cm-link-delete-b').click()
    await page.getByTestId('confirm-ok').click()
    await expect(page.getByTestId('cm-link-b')).toHaveCount(0)
    await expect.poll(async () => (await api.links()).some((l) => l.id === 'b')).toBe(false)
  })

  test('search filters links across ALL workspaces with a workspace > category breadcrumb', async ({ page, api }) => {
    await api.seed({
      workspaces: [{ id: 'ws1', name: 'Home', icon: 'lucide:house' }, { id: 'ws2', name: 'Lab', icon: 'lucide:flask-conical' }],
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }, { id: 'c2', workspace_id: 'ws2', name: 'Tools' }],
      links: [
        L('a', 'Grafana', { category_id: 'c1' }),
        L('b', 'Grafana Lab', { workspace_id: 'ws2', category_id: 'c2' }),
        L('c', 'Plex', { workspace_id: 'ws2' }),
        L('d', 'Other', { description: 'dashboards by grafana' }),
      ],
    })
    await openApp(page)
    await openSettings(page, 'content')
    await expect(page.getByTestId('cm-results')).toHaveCount(0)
    await page.getByTestId('cm-search').fill('grafana')
    await expect(page.getByTestId('cm-results').locator('[data-testid^="cm-result-"]')).toHaveCount(3)
    await expect(page.getByTestId('cm-result-a')).toContainText('Home › Apps')
    await expect(page.getByTestId('cm-result-b')).toContainText('Lab › Tools')
    await expect(page.getByTestId('cm-result-d')).toContainText(/Home › (Uncategorized|Uncategorised)/i)
    await expect(page.getByTestId('cm-result-c')).toHaveCount(0)
    // result rows have the same actions
    await page.getByTestId('cm-pin-b').click()
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'b')!.pinned).toBe(true)
    await page.getByTestId('cm-link-edit-b').click()
    await page.getByTestId('link-title').fill('Grafana Lab 2')
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId('cm-result-b')).toContainText('Grafana Lab 2')
    // no match
    await page.getByTestId('cm-search').fill('zzzzzz')
    await expect(page.getByTestId('cm-no-results')).toBeVisible()
    // clear returns to the workspace view
    await page.getByTestId('cm-search-clear').click()
    await expect(page.getByTestId('cm-ws-title')).toBeVisible()
    await expect(page.getByTestId('cm-search')).toHaveValue('')
  })

  test('changes made elsewhere (REST / MCP) show up live in the manager', async ({ page, api, ws }) => {
    await openApp(page)
    await openSettings(page, 'content')
    await api.link({ workspace_id: ws, title: 'Late arrival', url: 'http://late.lan' })
    await expect(page.getByTestId('content-manager')).toContainText('Late arrival')
    await api.workspace({ name: 'Remote WS', icon: 'lucide:cloud' })
    await expect(page.getByTestId('cm-ws-list')).toContainText('Remote WS')
  })

  test('mobile: Content tab fits the screen', async ({ browser, api }) => {
    await api.seed({
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: [L('a', 'A fairly long link title to test truncation', { category_id: 'c1', description: 'with a long description as well, to make sure nothing overflows' })],
    })
    const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, baseURL: origin(), locale: 'en-US' })
    await ctx.addCookies((await api.ctx.storageState()).cookies)
    const page = await ctx.newPage()
    await openApp(page)
    await openSettings(page, 'content')
    await expect(page.getByTestId('content-manager')).toBeVisible()
    await expect(page.getByTestId('cm-link-a')).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
    const m = (await page.getByTestId('settings-modal').boundingBox())!
    expect(m.x).toBeGreaterThanOrEqual(0)
    expect(m.x + m.width).toBeLessThanOrEqual(390.5)
    const r = (await page.getByTestId('cm-link-delete-a').boundingBox())!
    expect(r.x + r.width).toBeLessThanOrEqual(390.5)
    await ctx.close()
  })
})
