import { readFileSync } from 'node:fs'
import { expect, openApp, openSettings, origin, test } from '../helpers'

const L = (id: string, title: string, extra: Record<string, unknown> = {}) => ({ id, workspace_id: 'ws1', title, url: `${origin()}/healthz?l=${id}`, ...extra })

test.describe('export / import', () => {
  test('export downloads JSON without auth/API token; replace and merge import via Settings > Data', async ({ page, api }) => {
    await api.seed({
      settings: { title: 'Lab Home', sort_mode: 'custom' },
      workspaces: [
        { id: 'ws1', name: 'Home', icon: 'lucide:house', wallpaper: 'preset:dusk' },
        { id: 'ws2', name: 'Media', icon: 'lucide:clapperboard' },
      ],
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: [L('a', 'Alpha', { category_id: 'c1', clicks: 3, pinned: true }), L('b', 'Beta', { workspace_id: 'ws2' })],
    })
    const apiToken = await api.token()
    await openApp(page)
    await openSettings(page)
    await page.getByTestId('settings-tab-data').click()

    // ---- export ----
    const [dl] = await Promise.all([page.waitForEvent('download'), page.getByTestId('export-btn').click()])
    expect(dl.suggestedFilename()).toMatch(/\.json$/)
    const path = test.info().outputPath('export.json')
    await dl.saveAs(path)
    const raw = readFileSync(path, 'utf8')
    const exp = JSON.parse(raw)
    expect(Object.keys(exp).sort()).toEqual(['categories', 'links', 'schema', 'settings', 'workspaces'])
    expect(exp.workspaces.map((w: any) => w.name)).toEqual(['Home', 'Media'])
    expect(exp.links.map((l: any) => l.title).sort()).toEqual(['Alpha', 'Beta'])
    expect(exp.settings.title).toBe('Lab Home')
    expect(raw).not.toContain('password_hash')
    expect(raw).not.toContain('session_secret')
    expect(raw).not.toContain(apiToken)
    expect(raw).not.toContain('api_token')

    // ---- mutate, then import (replace) restores the exported state ----
    await api.call('DELETE', '/api/workspaces/ws2')
    await api.settings({ title: 'Changed' })
    await api.link({ workspace_id: 'ws1', title: 'Extra', url: 'http://extra.lan' })
    await page.reload()
    await openSettings(page)
    await page.getByTestId('settings-tab-data').click()
    await page.getByTestId('import-mode-replace').click()
    await expect(page.getByTestId('import-mode-replace')).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByTestId('import-btn')).toBeDisabled()
    await page.getByTestId('import-file').setInputFiles(path)
    await page.getByTestId('import-btn').click()
    await expect(page.getByTestId('toast').filter({ hasText: /2/ }).first()).toBeVisible()
    await expect.poll(async () => (await api.bootstrap()).workspaces.map((w) => w.name)).toEqual(['Home', 'Media'])
    let b = await api.bootstrap()
    expect(b.links.map((l) => l.title).sort()).toEqual(['Alpha', 'Beta'])
    expect(b.settings.title).toBe('Lab Home')
    expect(b.links.find((l) => l.title === 'Alpha')).toMatchObject({ clicks: 3, pinned: true })
    // the API token survives an import
    expect(await api.token()).toBe(apiToken)
    await page.getByTestId('modal-close').click()
    await expect(page.getByTestId('section-pinned')).toContainText('Alpha')

    // ---- merge appends under new ids and does not overwrite ----
    await openSettings(page)
    await page.getByTestId('settings-tab-data').click()
    await expect(page.getByTestId('import-mode-merge')).toHaveAttribute('aria-pressed', 'true') // default
    await page.getByTestId('import-file').setInputFiles(path)
    await page.getByTestId('import-btn').click()
    await expect.poll(async () => (await api.bootstrap()).workspaces.length).toBe(4)
    b = await api.bootstrap()
    expect(b.workspaces.map((w) => w.name)).toEqual(['Home', 'Media', 'Home', 'Media'])
    expect(b.links).toHaveLength(4)
    expect(new Set(b.workspaces.map((w) => w.id)).size).toBe(4)
    expect(new Set(b.links.map((l) => l.id)).size).toBe(4)
    expect(b.settings.title).toBe('Lab Home')
    // every link belongs to an existing workspace and category ids were remapped consistently
    for (const l of b.links) {
      expect(b.workspaces.some((w) => w.id === l.workspace_id)).toBe(true)
      if (l.category_id) expect(b.categories.find((c) => c.id === l.category_id)!.workspace_id).toBe(l.workspace_id)
    }
    expect(b.categories).toHaveLength(2)
  })

  test('invalid import file shows an error and changes nothing', async ({ page, api, ws }) => {
    await api.link({ workspace_id: ws, title: 'Keep', url: 'http://keep.lan' })
    await openApp(page)
    await openSettings(page)
    await page.getByTestId('settings-tab-data').click()
    await page.getByTestId('import-mode-replace').click()
    await page.getByTestId('import-file').setInputFiles({ name: 'bad.json', mimeType: 'application/json', buffer: Buffer.from('{not json') })
    await page.getByTestId('import-btn').click()
    await expect(page.getByTestId('toast').first()).toHaveAttribute('data-kind', 'error')
    await page.getByTestId('import-file').setInputFiles({ name: 'empty.json', mimeType: 'application/json', buffer: Buffer.from('{"schema":1,"workspaces":[],"categories":[],"links":[]}') })
    await page.getByTestId('import-btn').click()
    await expect(page.getByTestId('toast').nth(0)).toBeVisible()
    expect((await api.links()).map((l) => l.title)).toEqual(['Keep'])
  })

  test('REST export is an attachment', async ({ api }) => {
    const r = await api.ctx.get('/api/export')
    expect(r.status()).toBe(200)
    expect(r.headers()['content-disposition']).toContain('attachment')
  })
})
