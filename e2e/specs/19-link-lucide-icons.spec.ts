import { request } from '@playwright/test'
import { CSRF, expect, openApp, openSettings, origin, test, toggleEdit } from '../helpers'

const ICONS = [
  'house', 'layout-grid', 'folder', 'briefcase', 'code', 'terminal', 'server', 'database', 'hard-drive', 'cloud', 'network', 'router', 'wifi', 'globe', 'shield', 'lock',
  'key-round', 'film', 'clapperboard', 'tv', 'music', 'headphones', 'image', 'camera', 'gamepad-2', 'book-open', 'graduation-cap', 'newspaper', 'shopping-cart', 'wallet',
  'chart-line', 'activity', 'gauge', 'cpu', 'wrench', 'settings', 'bot', 'sparkles', 'heart', 'star', 'bookmark', 'users', 'message-circle', 'mail', 'calendar', 'download',
  'flask-conical', 'leaf', 'coffee', 'plane', 'lightbulb', 'rocket',
]

test.describe('link icons (Lucide)', () => {
  test('create a link choosing a Lucide icon: preview, search, capsule glyph; edit reopens on the Lucide tab with the icon selected', async ({ page, api }) => {
    await openApp(page)
    await page.getByTestId('empty-add-link').click()
    await page.getByTestId('link-url').fill('http://nas.lan:5000')
    await page.getByTestId('link-title').fill('NAS')
    await page.getByTestId('icon-mode-lucide').click()
    await expect(page.getByTestId('icon-mode-lucide')).toHaveAttribute('aria-pressed', 'true')
    const grid = page.getByTestId('link-icon-lucide')
    const cells = grid.locator('[role="radio"]')
    await expect(cells).toHaveCount(52)
    const names = await cells.evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')!.replace('link-icon-lucide-', '')))
    expect(names.sort()).toEqual([...ICONS].sort())
    // search filters by name
    await page.getByTestId('link-icon-lucide-search').fill('hard')
    await expect(cells).toHaveCount(1)
    await page.getByTestId('link-icon-lucide-search').press('Enter')
    await expect(page.getByTestId('link-modal')).toBeVisible() // Enter in the search box does not submit
    await page.getByTestId('link-icon-lucide-hard-drive').click()
    await expect(grid).toHaveAttribute('data-value', 'lucide:hard-drive')
    await expect(page.getByTestId('link-icon-lucide-hard-drive')).toHaveAttribute('aria-checked', 'true')
    await expect(page.locator('.icon-prev svg[data-icon="hard-drive"]')).toHaveCount(1) // preview tile
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId('link-modal')).toHaveCount(0)

    const l = (await api.bootstrap()).links[0]
    expect(l).toMatchObject({ title: 'NAS', icon: 'lucide:hard-drive' })
    await expect(page.getByTestId(`link-${l.id}`).locator('.ico-lucide svg[data-icon="hard-drive"]')).toHaveCount(1)

    // edit: opens on the Lucide mode with the icon selected
    await toggleEdit(page)
    await page.getByTestId(`link-edit-${l.id}`).click()
    await expect(page.getByTestId('icon-mode-lucide')).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByTestId('link-icon-lucide')).toHaveAttribute('data-value', 'lucide:hard-drive')
    await expect(page.getByTestId('link-icon-lucide-hard-drive')).toHaveAttribute('aria-checked', 'true')
    await expect(page.getByTestId('link-icon-lucide-server')).toHaveAttribute('aria-checked', 'false')
    // other modes keep their own state; switching away and back keeps the choice
    await page.getByTestId('icon-mode-emoji').click()
    await page.getByTestId('icon-mode-lucide').click()
    await expect(page.getByTestId('link-icon-lucide')).toHaveAttribute('data-value', 'lucide:hard-drive')
    await page.getByTestId('link-icon-lucide-server').click()
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId('link-modal')).toHaveCount(0)
    await expect.poll(async () => (await api.bootstrap()).links[0].icon).toBe('lucide:server')
    await expect(page.getByTestId(`link-${l.id}`).locator('svg[data-icon="server"]')).toHaveCount(1)

    // switching to another mode replaces the icon (letter badge = empty icon)
    await page.getByTestId(`link-edit-${l.id}`).click()
    await page.getByTestId('icon-mode-letter').click()
    await page.getByTestId('link-save').click()
    await expect.poll(async () => (await api.bootstrap()).links[0].icon).toBe('')
    await expect(page.getByTestId(`link-${l.id}`).locator('.ico-letter')).toHaveCount(1)
    await expect(page.getByTestId(`link-${l.id}`).locator('.ico-lucide')).toHaveCount(0)
  })

  test('the Lucide glyph renders in the pinned row, search results and content manager; unknown stored names fall back to a letter badge', async ({ page, api, ws }) => {
    const a = await api.link({ workspace_id: ws, title: 'Router', url: `${origin()}/healthz?l=router`, icon: 'lucide:router', pinned: true })
    const b = await api.link({ workspace_id: ws, title: 'Plain', url: `${origin()}/healthz?l=plain`, icon: '' })
    await openApp(page)
    await expect(page.getByTestId(`pin-link-${a.id}`).locator('svg[data-icon="router"]')).toHaveCount(1)
    await expect(page.getByTestId(`link-${a.id}`).locator('svg[data-icon="router"]')).toHaveCount(1)
    await expect(page.getByTestId(`link-${b.id}`).locator('.ico-lucide')).toHaveCount(0)
    await page.getByTestId('search-input').fill('router')
    await expect(page.getByTestId('search-result').first().locator('svg[data-icon="router"]')).toHaveCount(1)
    await page.getByTestId('search-input').fill('')
    await page.keyboard.press('Escape')
    await openSettings(page, 'content')
    await expect(page.getByTestId(`cm-link-${a.id}`).locator('svg[data-icon="router"]')).toHaveCount(1)
  })

  test('REST accepts all 52 names on create/update and rejects unknown ones (400), also on import', async ({ api, ws }) => {
    for (const n of ICONS) {
      const l = await api.link({ workspace_id: ws, title: n, url: `http://${n}.lan`, icon: 'lucide:' + n })
      expect(l.icon).toBe('lucide:' + n)
    }
    for (const bad of ['lucide:nope', 'lucide:', 'lucide:House', 'lucide:server/../x', 'lucide: server']) {
      const r = await api.ctx.post('/api/links', { data: { workspace_id: ws, title: 'bad', url: 'http://bad.lan', icon: bad, fetch_icon: false } })
      expect(r.status(), `create with icon ${bad}`).toBe(400)
    }
    const first = (await api.bootstrap()).links[0]
    expect((await api.ctx.patch(`/api/links/${first.id}`, { data: { icon: 'lucide:nope' } })).status()).toBe(400)
    expect((await api.ctx.patch(`/api/links/${first.id}`, { data: { icon: 'lucide:cloud' } })).status()).toBe(200)
    expect((await api.bootstrap()).links.find((x) => x.id === first.id)!.icon).toBe('lucide:cloud')
    // the other icon forms keep working
    expect((await api.ctx.patch(`/api/links/${first.id}`, { data: { icon: 'emoji:🚀' } })).status()).toBe(200)
    expect((await api.ctx.patch(`/api/links/${first.id}`, { data: { icon: 'url:https://example.com/i.png' } })).status()).toBe(200)
    // import validates link icons too
    const ex = await api.call<any>('GET', '/api/export')
    ex.links[0].icon = 'lucide:nope'
    expect((await api.ctx.post('/api/import', { data: { mode: 'merge', data: ex } })).status()).toBe(400)
  })

  test('MCP create_links / update_link accept lucide:<name>, report unknown names per item, and the open page updates live', async ({ page, api, ws }) => {
    await openApp(page)
    const token = await api.token()
    const mcp = await request.newContext({ baseURL: origin(), extraHTTPHeaders: { ...CSRF, Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' } })
    let id = 0
    const tool = async (name: string, args: unknown) => (await (await mcp.post('/mcp', { data: { jsonrpc: '2.0', id: ++id, method: 'tools/call', params: { name, arguments: args } } })).json()).result
    const tl = (await (await mcp.post('/mcp', { data: { jsonrpc: '2.0', id: ++id, method: 'tools/list' } })).json()).result.tools
    const desc = tl.find((t: any) => t.name === 'create_links').inputSchema.properties.links.items.properties.icon.description as string
    expect(desc).toContain('lucide:<name>')

    const res = await tool('create_links', {
      links: [
        { workspace_id: ws, title: 'AI Server', url: `${origin()}/healthz?l=srv`, icon: 'lucide:server' },
        { workspace_id: ws, title: 'AI Bad', url: `${origin()}/healthz?l=bad`, icon: 'lucide:nope' },
      ],
    })
    expect(res.structuredContent.created_count).toBe(1)
    expect(res.structuredContent.errors).toHaveLength(1)
    expect(res.structuredContent.errors[0]).toMatchObject({ index: 1 })
    const lid = res.structuredContent.created[0].id
    expect(res.structuredContent.created[0].icon).toBe('lucide:server')
    await expect(page.getByTestId(`link-${lid}`).locator('svg[data-icon="server"]')).toHaveCount(1) // live, no reload
    expect((await tool('update_link', { id: lid, icon: 'lucide:nope' })).isError).toBe(true)
    const up = await tool('update_link', { id: lid, icon: 'lucide:database' })
    expect(up.structuredContent.link.icon).toBe('lucide:database')
    await expect(page.getByTestId(`link-${lid}`).locator('svg[data-icon="database"]')).toHaveCount(1)
    await mcp.dispose()
  })

  test.describe('mobile bottom sheet', () => {
    test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 })

    test('the Lucide grid is height-capped and scrolls; the sheet and the mode tabs fit the screen', async ({ page, api, ws }) => {
      const l = await api.link({ workspace_id: ws, title: 'Box', url: `${origin()}/healthz?l=box`, icon: 'lucide:rocket' })
      await openApp(page)
      await toggleEdit(page)
      await page.getByTestId(`link-edit-${l.id}`).click()
      await expect(page.getByTestId('icon-mode-lucide')).toHaveAttribute('aria-pressed', 'true')
      const grid = page.getByTestId('link-icon-lucide')
      const m = await grid.evaluate((g) => ({ ch: g.clientHeight, sh: g.scrollHeight }))
      expect(m.ch).toBeLessThanOrEqual(200)
      expect(m.sh).toBeGreaterThan(m.ch)
      const modal = (await page.getByTestId('link-modal').boundingBox())!
      expect(modal.x).toBeGreaterThanOrEqual(0)
      expect(modal.x + modal.width).toBeLessThanOrEqual(390.5)
      const seg = (await page.getByTestId('icon-mode').boundingBox())!
      expect(seg.x + seg.width).toBeLessThanOrEqual(390)
      expect(await page.getByTestId('icon-mode').evaluate((e) => e.scrollWidth <= e.clientWidth + 1)).toBe(true) // all six tabs visible
      await grid.locator('[data-testid="link-icon-lucide-leaf"]').scrollIntoViewIfNeeded()
      await page.getByTestId('link-icon-lucide-leaf').click()
      await page.getByTestId('link-save').click()
      await expect.poll(async () => (await api.bootstrap()).links[0].icon).toBe('lucide:leaf')
    })
  })
})
