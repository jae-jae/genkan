import { request } from '@playwright/test'
import { expect, expectSort, openApp, openSettings, origin, readZipEntry, switchWs, test, wsNames, zipEntries } from '../helpers'

let rpcId = 0
async function rpc(ctx: Awaited<ReturnType<typeof request.newContext>>, method: string, params?: unknown) {
  const r = await ctx.post('/mcp', { data: { jsonrpc: '2.0', id: ++rpcId, method, params } })
  return { status: r.status(), body: await r.json() }
}
async function tool(ctx: Awaited<ReturnType<typeof request.newContext>>, name: string, args: unknown) {
  const { status, body } = await rpc(ctx, 'tools/call', { name, arguments: args })
  expect(status).toBe(200)
  expect(body.error, JSON.stringify(body)).toBeUndefined()
  return body.result
}

test.describe('single API token + Skill + MCP', () => {
  test('token exists by default and is masked; reveal, copy, regenerate (old token dies), Skill download; MCP drives the open page live via SSE', async ({ page, api, ws }) => {
    await api.link({ workspace_id: ws, title: 'Existing', url: `${origin()}/healthz?l=existing` })
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: origin() })
    await openApp(page)
    await page.evaluate(() => ((window as any).__noReload = 'still-here')) // proves the page is never reloaded

    // ---- a token exists from the start, masked by default; snippets are masked too ----
    const first = await api.token()
    expect(first).toMatch(/^gk_[a-z2-7]{32}$/)
    await openSettings(page, 'ai')
    await expect(page.getByTestId('settings-panel-ai')).toBeVisible()
    await expect(page.getByTestId('mcp-warning')).toContainText(/admin access/i)
    // default pane is Skill: only that pane is rendered
    await expect(page.getByTestId('pane-skill')).toBeVisible()
    await expect(page.getByTestId('mcp-command')).toHaveCount(0)
    await expect(page.getByTestId('mcp-json')).toHaveCount(0)
    const val = page.getByTestId('token-value')
    await expect(val).toHaveAttribute('data-masked', '1')
    await expect(val).toHaveText(`gk_\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2026${first.slice(-4)}`)
    await page.getByTestId('connect-claude').click()
    await expect(page.getByTestId('connect-claude')).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('pane-skill')).toHaveCount(0)
    await expect(page.getByTestId('mcp-json')).toHaveCount(0)
    await expect(page.getByTestId('mcp-command')).toContainText(`${origin()}/mcp`)
    await expect(page.getByTestId('mcp-command')).toContainText(`Bearer gk_\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2026${first.slice(-4)}`)
    await expect(page.getByTestId('mcp-command')).not.toContainText(first)
    expect(await page.locator('body').innerHTML()).not.toContain(first) // nothing in the DOM while masked (not even hidden)
    // code blocks never wrap (horizontal scroll instead)
    expect(await page.getByTestId('mcp-command').evaluate((e) => getComputedStyle(e).whiteSpace)).toBe('pre')
    // the copy buttons always copy the real value, even while masked; the icon flips to a check for a moment
    await page.getByTestId('copy-mcp-command').click()
    await expect(page.getByTestId('copy-mcp-command').locator('svg[data-icon="check"]')).toBeVisible()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`claude mcp add --transport http genkan ${origin()}/mcp --header "Authorization: Bearer ${first}"`)
    await expect(page.getByTestId('copy-mcp-command').locator('svg[data-icon="copy"]')).toBeVisible()
    await page.getByTestId('connect-mcp').click()
    await expect(page.getByTestId('mcp-command')).toHaveCount(0)
    await expect(page.getByTestId('mcp-json')).not.toContainText(first)
    await expect(page.getByTestId('mcp-json')).toContainText('"mcpServers"')
    expect(await page.locator('body').innerHTML()).not.toContain(first)
    await page.getByTestId('copy-mcp-json').click()
    expect(JSON.parse(await page.evaluate(() => navigator.clipboard.readText()))).toEqual({ mcpServers: { genkan: { type: 'http', url: `${origin()}/mcp`, headers: { Authorization: `Bearer ${first}` } } } })
    await page.getByTestId('token-copy').click()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(first)
    // reveal / hide
    await page.getByTestId('token-toggle').click()
    await expect(val).toHaveAttribute('data-masked', '0')
    await expect(val).toHaveText(first)
    await expect(page.getByTestId('mcp-json')).toContainText(`"Authorization": "Bearer ${first}"`)
    await page.getByTestId('connect-claude').click()
    await expect(page.getByTestId('mcp-command')).toContainText(`Bearer ${first}`)
    await page.getByTestId('token-toggle').click()
    await expect(val).toHaveAttribute('data-masked', '1')

    // ---- Skill download carries the token and this instance's URL ----
    await page.getByTestId('connect-skill').click()
    const [dl] = await Promise.all([page.waitForEvent('download'), page.getByTestId('skill-download').click()])
    expect(dl.suggestedFilename()).toBe('genkan-skill.zip')
    const zipPath = test.info().outputPath('genkan-skill.zip')
    await dl.saveAs(zipPath)
    const { readFileSync } = await import('node:fs')
    const zip = readFileSync(zipPath)
    expect(zipEntries(zip)).toEqual(['genkan/SKILL.md'])
    const skill = readZipEntry(zip, 'genkan/SKILL.md')
    expect(skill).toContain(`claude mcp add --transport http genkan ${origin()}/mcp --header "Authorization: Bearer ${first}"`)
    expect(skill).not.toMatch(/\{\{GENKAN_/)
    expect(skill.indexOf('WARNING')).toBeGreaterThan(skill.indexOf('---', 4)) // right after the frontmatter
    expect(skill.indexOf('WARNING')).toBeLessThan(skill.indexOf('# Genkan'))

    // ---- regenerate: inline confirmation, cancel keeps the token ----
    const oldCtx = await request.newContext({ baseURL: origin(), extraHTTPHeaders: { Authorization: `Bearer ${first}`, 'Content-Type': 'application/json' } })
    expect((await oldCtx.post('/mcp', { data: { jsonrpc: '2.0', id: 1, method: 'ping' } })).status()).toBe(200)
    await page.getByTestId('token-regenerate').click()
    await expect(page.getByTestId('token-confirm')).toContainText(/stops working immediately/i)
    await page.getByTestId('token-regenerate-cancel').click()
    await expect(page.getByTestId('token-confirm')).toHaveCount(0)
    expect(await api.token()).toBe(first)
    await page.getByTestId('token-regenerate').click()
    await page.getByTestId('token-regenerate-confirm').click()
    await expect(page.getByTestId('token-confirm')).toHaveCount(0)
    await expect(page.getByTestId('toast').filter({ hasText: /regenerated/i })).toBeVisible()
    const token = await api.token()
    expect(token).toMatch(/^gk_[a-z2-7]{32}$/)
    expect(token).not.toBe(first)
    // every snippet on the page follows the new token (still masked)
    await expect(val).toHaveText(`gk_\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2026${token.slice(-4)}`)
    await page.getByTestId('connect-claude').click()
    await expect(page.getByTestId('mcp-command')).toContainText(token.slice(-4))
    await page.getByTestId('copy-mcp-command').click()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toContain(`Bearer ${token}"`)
    // the old token is dead immediately, for MCP and REST
    expect((await oldCtx.post('/mcp', { data: { jsonrpc: '2.0', id: 2, method: 'ping' } })).status()).toBe(401)
    expect((await oldCtx.get('/api/export')).status()).toBe(401)
    await oldCtx.dispose()
    await page.getByTestId('modal-close').click()

    // ---- raw MCP over HTTP with the Bearer token ----
    const mcp = await request.newContext({ baseURL: origin(), extraHTTPHeaders: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' } })

    const init = await rpc(mcp, 'initialize', { protocolVersion: '2025-03-26', capabilities: {}, clientInfo: { name: 'e2e', version: '1' } })
    expect(init.status).toBe(200)
    expect(init.body.result).toMatchObject({ protocolVersion: '2025-03-26', serverInfo: { name: 'genkan' }, capabilities: { tools: {} } })
    expect(init.body.result.instructions).toBeTruthy()
    const note = await mcp.post('/mcp', { data: { jsonrpc: '2.0', method: 'notifications/initialized' } })
    expect(note.status()).toBe(202)
    expect((await rpc(mcp, 'ping')).body.result).toBeDefined()

    const list = await rpc(mcp, 'tools/list')
    const names = list.body.result.tools.map((t: any) => t.name)
    expect(names).toEqual(['get_overview', 'search_links', 'create_workspace', 'update_workspace', 'delete_workspace', 'create_category', 'update_category', 'delete_category', 'create_links', 'update_link', 'delete_links', 'reorder', 'update_settings'])
    for (const t of list.body.result.tools) expect(t.inputSchema.type).toBe('object')

    const ov = await tool(mcp, 'get_overview', {})
    expect(JSON.stringify(ov.structuredContent)).toContain('Existing')
    expect(JSON.parse(ov.content[0].text)).toBeTruthy()

    // create_links -> appears live
    const cat = await tool(mcp, 'create_category', { workspace_id: ws, name: 'From AI' })
    const catId = cat.structuredContent.category.id
    await expect(page.getByTestId(`category-${catId}-title`)).toHaveCount(0) // empty categories are hidden outside edit mode
    const created = await tool(mcp, 'create_links', {
      links: [
        { workspace_id: ws, category_id: catId, title: 'AI Alpha', url: `${origin()}/healthz?l=ai-a`, description: 'made by an agent' },
        { workspace_id: ws, category_id: catId, title: 'AI Beta', url: `${origin()}/healthz?l=ai-b`, pinned: true },
      ],
    })
    expect(created.structuredContent.created_count).toBe(2)
    expect(created.isError).toBeFalsy()
    await expect(page.getByTestId(`category-${catId}-title`)).toHaveText('From AI')
    await expect(page.getByRole('link', { name: 'AI Alpha' })).toBeVisible()
    await expect(page.getByTestId('section-pinned')).toContainText('AI Beta')
    const ids: string[] = created.structuredContent.created.map((l: any) => l.id)

    // update_link -> rename shows up live
    const up = await tool(mcp, 'update_link', { id: ids[0], title: 'AI Alpha Renamed', description: 'edited' })
    expect(up.structuredContent.link.title).toBe('AI Alpha Renamed')
    await expect(page.getByRole('link', { name: 'AI Alpha Renamed' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'AI Alpha', exact: true })).toHaveCount(0)

    // update_link -> move to a new workspace; it shows up in the dropdown, link leaves this one
    const w2 = (await tool(mcp, 'create_workspace', { name: 'Robot WS', icon: 'lucide:bot' })).structuredContent.workspace.id
    await expect.poll(() => wsNames(page)).toContain('Robot WS')
    await tool(mcp, 'update_link', { id: ids[0], workspace_id: w2, category_id: '' })
    await expect(page.getByRole('link', { name: 'AI Alpha Renamed' })).toHaveCount(0)
    await switchWs(page, 'Robot WS')
    await expect(page.getByRole('link', { name: 'AI Alpha Renamed' })).toBeVisible()
    await switchWs(page, ws)

    // update_settings -> the sort mode in the main menu follows
    await tool(mcp, 'update_settings', { sort_mode: 'recent' })
    await expect.poll(async () => (await api.bootstrap()).settings.sort_mode).toBe('recent')
    await expectSort(page, 'recent')

    // delete_links -> disappear live
    const del = await tool(mcp, 'delete_links', { ids: [ids[1], 'doesnotexist'] })
    expect(del.structuredContent.deleted).toBe(1)
    await expect(page.getByRole('link', { name: 'AI Beta' })).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'Existing' })).toBeVisible()

    // business errors are isError results, not transport errors
    const bad = await rpc(mcp, 'tools/call', { name: 'update_link', arguments: { id: 'nope', title: 'x' } })
    expect(bad.body.result.isError).toBe(true)
    const unknown = await rpc(mcp, 'tools/call', { name: 'no_such_tool', arguments: {} })
    expect(unknown.body.error).toBeTruthy()
    expect((await rpc(mcp, 'no/such/method')).body.error.code).toBe(-32601)

    // the page was never reloaded and never lost its marker
    expect(await page.evaluate(() => (window as any).__noReload)).toBe('still-here')
    await mcp.dispose()
  })

  test('MCP auth edge cases: no token, bad token, GET, cookie auth is not enough', async ({ api }) => {
    const none = await request.newContext({ baseURL: origin() })
    const noTok = await none.post('/mcp', { data: { jsonrpc: '2.0', id: 1, method: 'ping' } })
    expect(noTok.status()).toBe(401)
    const badTok = await none.post('/mcp', { headers: { Authorization: 'Bearer gk_00000000000000000000000000000000' }, data: { jsonrpc: '2.0', id: 1, method: 'ping' } })
    expect(badTok.status()).toBe(401)
    const cookie = (await api.ctx.storageState()).cookies.map((c) => `${c.name}=${c.value}`).join('; ')
    const viaCookie = await none.post('/mcp', { headers: { Cookie: cookie, 'X-Requested-With': 'genkan' }, data: { jsonrpc: '2.0', id: 1, method: 'ping' } })
    expect(viaCookie.status()).toBe(401)
    const tok = await api.token()
    const get = await none.get('/mcp', { headers: { Authorization: `Bearer ${tok}` } })
    expect(get.status()).toBe(405)
    // batch requests and bearer access to the REST API (no CSRF header needed for tokens)
    const batch = await none.post('/mcp', { headers: { Authorization: `Bearer ${tok}` }, data: [{ jsonrpc: '2.0', id: 1, method: 'ping' }, { jsonrpc: '2.0', id: 2, method: 'tools/list' }] })
    expect(batch.status()).toBe(200)
    expect(await batch.json()).toHaveLength(2)
    const rest = await none.post('/api/workspaces', { headers: { Authorization: `Bearer ${tok}` }, data: { name: 'via token' } })
    expect(rest.status()).toBe(200)
    await none.dispose()
  })

  test('token API: admin-only, CSRF-protected regenerate, Skill zip reflects proxy headers and the current token', async ({ api }) => {
    const tok = await api.token()
    const none = await request.newContext({ baseURL: origin() })
    // anonymous / cookie without CSRF header
    expect((await none.get('/api/token')).status()).toBe(401)
    expect((await none.get('/api/skill')).status()).toBe(401)
    expect((await none.post('/api/token/regenerate', { headers: { 'X-Requested-With': 'genkan' } })).status()).toBe(401)
    const cookie = (await api.ctx.storageState()).cookies.map((c) => `${c.name}=${c.value}`).join('; ')
    expect((await none.post('/api/token/regenerate', { headers: { Cookie: cookie } })).status()).toBe(403)
    expect(await api.token()).toBe(tok) // nothing changed
    // the removed multi-token endpoints are gone
    for (const [m, p] of [['GET', '/api/tokens'], ['POST', '/api/tokens'], ['DELETE', '/api/tokens/abc']] as const) {
      expect([404, 405], `${m} ${p}`).toContain((await api.ctx.fetch(p, { method: m, data: m === 'GET' ? undefined : {} })).status())
    }
    // the token never leaks via export / bootstrap
    expect(JSON.stringify(await (await api.ctx.get('/api/export')).json())).not.toContain(tok)
    expect(JSON.stringify(await api.bootstrap())).not.toContain(tok)
    // a bearer token works for the whole admin API, including reading itself
    const bearer = { Authorization: `Bearer ${tok}` }
    expect(((await (await none.get('/api/token', { headers: bearer })).json()) as { token: string }).token).toBe(tok)
    // Skill zip: instance URL from the request, derived from proxy headers when present
    const get = async (headers: Record<string, string> = {}) => {
      const r = await api.ctx.get('/api/skill', { headers })
      expect(r.status()).toBe(200)
      expect(r.headers()['content-type']).toBe('application/zip')
      expect(r.headers()['content-disposition']).toContain('genkan-skill.zip')
      const zip = await r.body()
      expect(zipEntries(zip)).toEqual(['genkan/SKILL.md'])
      return readZipEntry(zip, 'genkan/SKILL.md')
    }
    expect(await get()).toContain(`genkan ${origin()}/mcp --header "Authorization: Bearer ${tok}"`)
    const proxied = await get({ 'X-Forwarded-Proto': 'https', 'X-Forwarded-Host': 'start.example.org' })
    expect(proxied).toContain('claude mcp add --transport http genkan https://start.example.org/mcp')
    expect(proxied).toContain(`curl -s -H "Authorization: Bearer ${tok}" "https://start.example.org/api/bootstrap"`)
    expect(proxied).not.toMatch(/\{\{GENKAN_|\$GENKAN_/)
    // regenerate through the API: the zip follows, the old token is dead
    const nt = (await api.call<{ token: string }>('POST', '/api/token/regenerate')).token
    expect(nt).not.toBe(tok)
    expect(await get()).toContain(nt)
    expect((await none.get('/api/token', { headers: bearer })).status()).toBe(401)
    expect((await none.get('/api/token', { headers: { Authorization: `Bearer ${nt}` } })).status()).toBe(200)
    await none.dispose()
  })

  test('SSE stream: change events carry the version and arrive for REST writes too', async ({ page, api, ws }) => {
    await openApp(page)
    await page.evaluate(() => ((window as any).__noReload = 1))
    await api.link({ workspace_id: ws, title: 'Pushed by REST', url: 'http://rest.lan' })
    await expect(page.getByRole('link', { name: 'Pushed by REST' })).toBeVisible()
    expect(await page.evaluate(() => (window as any).__noReload)).toBe(1)
    // raw SSE client: first byte stream contains an event after a write
    const sse = await page.evaluate(async () => {
      const es = new EventSource('/api/events')
      const got = new Promise<string>((res) => es.addEventListener('change', (e) => res((e as MessageEvent).data)))
      await new Promise((r) => (es.onopen = r))
      await fetch('/api/settings', { method: 'PATCH', headers: { 'X-Requested-With': 'genkan', 'Content-Type': 'application/json' }, body: JSON.stringify({ show_clock: false }) })
      const data = await got
      es.close()
      return data
    })
    expect(JSON.parse(sse).version).toBeGreaterThan(0)
    await expect(page.getByTestId('clock')).toHaveCount(0) // live setting change hides the clock
  })
})
