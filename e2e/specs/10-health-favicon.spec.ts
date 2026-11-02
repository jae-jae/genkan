import { expect, openApp, selfOrigin, test, toggleEdit } from '../helpers'

test.describe('health checks', () => {
  test('dots turn green for the container itself and red for an unreachable address', async ({ page, api, ws }) => {
    const up = await api.link({ workspace_id: ws, title: 'Self', url: `${selfOrigin()}/healthz`, health_check: true })
    const down = await api.link({ workspace_id: ws, title: 'Nowhere', url: 'http://127.0.0.1:1/', health_check: true })
    const off = await api.link({ workspace_id: ws, title: 'Unmonitored', url: `${selfOrigin()}/healthz` })
    await openApp(page)
    await expect(page.getByTestId(`health-${up.id}`)).toBeVisible()
    await expect(page.getByTestId(`health-${off.id}`)).toHaveCount(0) // no dot without health_check
    // first check happens within seconds, results pushed via SSE (no reload)
    await expect(page.getByTestId(`health-${up.id}`)).toHaveAttribute('data-status', 'up', { timeout: 15_000 })
    await expect(page.getByTestId(`health-${down.id}`)).toHaveAttribute('data-status', 'down', { timeout: 15_000 })
    await expect(page.getByTestId(`health-${up.id}`)).toHaveAttribute('title', /Online|在线|up/i)
    // REST view agrees
    const h = await api.call<Record<string, { status: string; latency_ms: number }>>('GET', '/api/health')
    expect(h[up.id].status).toBe('up')
    expect(h[down.id].status).toBe('down')
    expect(h[off.id]).toBeUndefined()
  })

  test('enabling the check in the editor starts monitoring quickly; fixing the URL flips down to up', async ({ page, api, ws }) => {
    const l = await api.link({ workspace_id: ws, title: 'Flaky', url: 'http://127.0.0.1:1/' })
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId(`link-edit-${l.id}`).click()
    await page.getByTestId('link-health').click()
    await expect(page.getByTestId('link-health')).toHaveAttribute('aria-checked', 'true')
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId(`health-${l.id}`)).toHaveAttribute('data-status', 'down', { timeout: 15_000 })
    // repoint at a live address -> next check goes green (health interval is shortened to 15s, the minimum)
    await api.settings({ health_interval: 15 })
    await api.call('PATCH', `/api/links/${l.id}`, { url: `${selfOrigin()}/healthz` })
    await expect(page.getByTestId(`health-${l.id}`)).toHaveAttribute('data-status', 'up', { timeout: 30_000 })
  })
})

test.describe('favicon auto-fetch', () => {
  test('creating a link to the container origin fetches its favicon.svg asynchronously and the UI shows it', async ({ page, api, ws }) => {
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId('uncat-add-link').click()
    await page.getByTestId('link-url').fill(`${selfOrigin()}/`)
    await page.getByTestId('link-title').fill('Genkan itself')
    await expect(page.getByTestId('icon-mode-auto')).toHaveAttribute('aria-pressed', 'true') // default: auto
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId('link-modal')).toHaveCount(0)
    const cap = page.locator('[data-testid^="link-"][data-link-id]', { hasText: 'Genkan itself' })
    // the create response has an empty icon; the fetched icon arrives later via the SSE change event
    const img = cap.locator('img.ico')
    await expect(img).toBeVisible({ timeout: 15_000 })
    await expect(img).toHaveAttribute('src', /^\/uploads\/icons\/[0-9a-f]{16}\.svg$/)
    await expect.poll(() => img.evaluate((e: HTMLImageElement) => e.complete && e.naturalWidth > 0)).toBe(true)
    const l = (await api.links())[0]
    expect(l.icon).toMatch(/^favicon:[0-9a-f]{16}\.svg$/)
    const r = await api.ctx.get(`/uploads/icons/${l.icon.slice('favicon:'.length)}`)
    expect(r.status()).toBe(200)
    expect(r.headers()['content-type']).toContain('svg')
    void ws
  })

  test('unreachable site keeps the letter badge; refresh-icon on a reachable one fills it in', async ({ page, api, ws }) => {
    const bad = await api.call<any>('POST', '/api/links', { workspace_id: ws, title: 'Ghost', url: 'http://127.0.0.1:1/', fetch_icon: true })
    const ok = await api.call<any>('POST', '/api/links', { workspace_id: ws, title: 'Manual', url: `${selfOrigin()}/`, fetch_icon: false })
    expect(ok.icon).toBe('')
    await openApp(page)
    await expect(page.getByTestId(`link-${bad.id}`).locator('.ico-letter')).toHaveText('G')
    await expect(page.getByTestId(`link-${ok.id}`).locator('.ico-letter')).toHaveText('M')
    const r = await api.call<any>('POST', `/api/links/${ok.id}/refresh-icon`)
    expect(r.icon).toMatch(/^favicon:/)
    await expect(page.getByTestId(`link-${ok.id}`).locator('img.ico')).toBeVisible()
    await expect(page.getByTestId(`link-${bad.id}`).locator('.ico-letter')).toBeVisible()
    expect((await api.links()).find((l) => l.id === bad.id)!.icon).toBe('')
  })
})
