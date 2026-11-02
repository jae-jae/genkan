import { CSRF, expect, openApp, openSettings, openWsMenu, origin, test, wsNames } from '../helpers'

const ICONS = [
  'house', 'layout-grid', 'folder', 'briefcase', 'code', 'terminal', 'server', 'database', 'hard-drive', 'cloud', 'network', 'router', 'wifi', 'globe', 'shield', 'lock',
  'key-round', 'film', 'clapperboard', 'tv', 'music', 'headphones', 'image', 'camera', 'gamepad-2', 'book-open', 'graduation-cap', 'newspaper', 'shopping-cart', 'wallet',
  'chart-line', 'activity', 'gauge', 'cpu', 'wrench', 'settings', 'bot', 'sparkles', 'heart', 'star', 'bookmark', 'users', 'message-circle', 'mail', 'calendar', 'download',
  'flask-conical', 'leaf', 'coffee', 'plane', 'lightbulb', 'rocket',
]

test.describe('workspace icons (Lucide)', () => {
  test('the picker offers exactly the 52 built-in icons; the default workspace is "house"; no emoji field', async ({ page, api }) => {
    expect(ICONS).toHaveLength(52)
    expect((await api.bootstrap()).workspaces[0].icon).toBe('lucide:house')
    await openApp(page)
    await openWsMenu(page)
    await page.getByTestId('ws-add').click()
    const grid = page.getByTestId('ws-icon')
    await expect(grid).toHaveAttribute('role', 'radiogroup')
    await expect(grid).toHaveAttribute('data-value', 'lucide:layout-grid') // new workspace default
    const cells = grid.locator('[role="radio"]')
    await expect(cells).toHaveCount(52)
    const names = await cells.evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')!.replace('ws-icon-', '')))
    expect(names.sort()).toEqual([...ICONS].sort())
    // every cell draws an svg, none renders text (no emoji)
    for (const c of await cells.all()) {
      await expect(c.locator('svg')).toHaveCount(1)
      expect((await c.innerText()).trim()).toBe('')
    }
    await expect(page.locator('input[data-testid="ws-icon"]')).toHaveCount(0)
    // selecting updates aria-checked, data-value and the preview
    await page.getByTestId('ws-icon-rocket').click()
    await expect(page.getByTestId('ws-icon-rocket')).toHaveAttribute('aria-checked', 'true')
    await expect(page.getByTestId('ws-icon-layout-grid')).toHaveAttribute('aria-checked', 'false')
    await expect(grid).toHaveAttribute('data-value', 'lucide:rocket')
    await expect(page.getByTestId('ws-icon-preview').locator('svg[data-icon="rocket"]')).toHaveCount(1)
    // search filters by name
    await page.getByTestId('ws-icon-search').fill('server')
    await expect(cells).toHaveCount(1)
    await page.getByTestId('ws-icon-search').fill('nope-nothing')
    await expect(cells).toHaveCount(0)
    await page.getByTestId('ws-icon-search').fill('')
    await expect(cells).toHaveCount(52)
    // Enter inside the search field does not submit the form
    await page.getByTestId('ws-icon-search').press('Enter')
    await expect(page.getByTestId('workspace-modal')).toBeVisible()
    await page.getByTestId('ws-name').fill('Launch')
    await page.getByTestId('ws-save').click()
    await expect(page.getByTestId('ws-menu-btn').locator('svg[data-icon="rocket"]')).toHaveCount(1)
    await expect.poll(async () => (await api.bootstrap()).workspaces[1]).toMatchObject({ name: 'Launch', icon: 'lucide:rocket' })
    // the icon shows in the dropdown rows too
    await openWsMenu(page)
    await expect(page.locator('[data-testid^="ws-item-"][data-ws-name="Launch"] svg[data-icon="rocket"]')).toHaveCount(1)
  })

  test('REST and MCP accept all 52 names and reject emoji / bare names with 400', async ({ api }) => {
    for (const n of ICONS) {
      const w = await api.workspace({ name: 'x-' + n, icon: 'lucide:' + n })
      expect(w.icon).toBe('lucide:' + n)
      await api.call('DELETE', `/api/workspaces/${w.id}`)
    }
    for (const bad of ['🎬', 'house', 'lucide:nope', 'lucide:', 'Lucide:house', 'lucide:house/../x']) {
      const r = await api.ctx.post('/api/workspaces', { data: { name: 'bad', icon: bad } })
      expect(r.status(), `create with icon ${bad}`).toBe(400)
    }
    const ws = (await api.bootstrap()).workspaces[0]
    expect((await api.ctx.patch(`/api/workspaces/${ws.id}`, { data: { icon: '🏠' } })).status()).toBe(400)
    expect((await api.ctx.patch(`/api/workspaces/${ws.id}`, { data: { icon: 'lucide:cloud' } })).status()).toBe(200)
    // empty icon -> the default
    const e = await api.workspace({ name: 'empty-icon' })
    expect(e.icon).toBe('lucide:layout-grid')

    // MCP
    const token = await api.token()
    const rpc = async (name: string, args: unknown) => {
      const r = await api.ctx.post('/mcp', { headers: { Authorization: `Bearer ${token}`, Accept: 'application/json, text/event-stream' }, data: { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } } })
      return (await r.json()).result
    }
    const okRes = await rpc('create_workspace', { name: 'MCP ws', icon: 'lucide:bot' })
    expect(okRes.isError).toBeFalsy()
    expect(okRes.structuredContent.workspace.icon).toBe('lucide:bot')
    for (const bad of ['🤖', 'bot']) {
      const r = await rpc('create_workspace', { name: 'MCP bad', icon: bad })
      expect(r.isError, `mcp create ${bad}`).toBe(true)
    }
    const upd = await rpc('update_workspace', { id: okRes.structuredContent.workspace.id, icon: '🤖' })
    expect(upd.isError).toBe(true)
    // schema advertises the names
    const list = await api.ctx.post('/mcp', { headers: { Authorization: `Bearer ${token}`, Accept: 'application/json, text/event-stream' }, data: { jsonrpc: '2.0', id: 2, method: 'tools/list' } })
    const tools = (await list.json()).result.tools
    const cw = tools.find((t: any) => t.name === 'create_workspace')
    expect(JSON.stringify(cw.inputSchema.properties.icon)).toContain('rocket')
    void CSRF
  })

  test('old data with emoji icons is migrated on import: the UI shows Lucide icons', async ({ page, api }) => {
    const old = {
      schema: 1,
      settings: {},
      workspaces: [
        { id: 'w1', name: 'Home', icon: '🏠', wallpaper: 'preset:aurora', sort: 0 },
        { id: 'w2', name: 'Media', icon: '🎬', wallpaper: 'preset:ocean', sort: 1 },
        { id: 'w3', name: 'Tools', icon: '🛠️', wallpaper: 'preset:dusk', sort: 2 },
        { id: 'w4', name: 'Net', icon: '🌐', wallpaper: 'preset:forest', sort: 3 },
        { id: 'w5', name: 'Mystery', icon: '🦄', wallpaper: 'preset:sakura', sort: 4 },
        { id: 'w6', name: 'Empty', icon: '', wallpaper: 'preset:graphite', sort: 5 },
      ],
      categories: [],
      // an old export: no pin_sort anywhere
      links: [
        { id: 'l1', workspace_id: 'w1', category_id: '', title: 'P1', url: 'http://p1.lan', description: '', icon: '', pinned: true, sort: 3, clicks: 0, last_used_at: null, health_check: false },
        { id: 'l2', workspace_id: 'w1', category_id: '', title: 'P2', url: 'http://p2.lan', description: '', icon: '', pinned: true, sort: 1, clicks: 0, last_used_at: null, health_check: false },
      ],
    }
    await openApp(page)
    await openSettings(page, 'data')
    await page.getByTestId('import-mode-replace').click()
    await page.getByTestId('import-file').setInputFiles({ name: 'old-export.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(old)) })
    await page.getByTestId('import-btn').click()
    await expect.poll(async () => (await api.bootstrap()).workspaces.map((w) => w.icon)).toEqual(['lucide:house', 'lucide:clapperboard', 'lucide:wrench', 'lucide:globe', 'lucide:layout-grid', 'lucide:layout-grid'])
    await page.getByTestId('modal-close').click()
    // the UI renders them as Lucide svgs (current workspace button + every dropdown row)
    await expect(page.getByTestId('ws-menu-btn').locator('svg[data-icon="house"]')).toHaveCount(1)
    await openWsMenu(page)
    const iconsInMenu = await page.locator('[data-testid^="ws-item-"] svg[data-icon]').evaluateAll((els) => els.map((e) => e.getAttribute('data-icon')).filter((n) => n !== 'check'))
    expect(iconsInMenu).toEqual(['house', 'clapperboard', 'wrench', 'globe', 'layout-grid', 'layout-grid'])
    await page.keyboard.press('Escape')
    // pin_sort backfilled from sort: P2 (sort 1) before P1 (sort 3) in custom mode
    const links = await api.links()
    expect(links.find((l) => l.id === 'l1')!.pin_sort).toBe(3)
    expect(links.find((l) => l.id === 'l2')!.pin_sort).toBe(1)
    expect(await wsNames(page)).toHaveLength(6)
  })

  test('REST import / merge of emoji icons also migrates; a bare lucide name in a file is handled', async ({ api }) => {
    await api.call('POST', '/api/import', {
      mode: 'merge',
      data: { schema: 1, settings: {}, workspaces: [{ id: 'z1', name: 'Gamer', icon: '🎮', wallpaper: 'preset:aurora', sort: 0 }, { id: 'z2', name: 'Books', icon: '📚', wallpaper: 'preset:aurora', sort: 1 }, { id: 'z3', name: 'Cloudy', icon: '☁️', wallpaper: 'preset:aurora', sort: 2 }], categories: [], links: [] },
    })
    const names = Object.fromEntries((await api.bootstrap()).workspaces.map((w) => [w.name, w.icon]))
    expect(names.Gamer).toBe('lucide:gamepad-2')
    expect(names.Books).toBe('lucide:book-open')
    expect(names.Cloudy).toBe('lucide:cloud')
    void origin
  })
})
