import { expect, openApp, origin, test } from '../helpers'

const L = (id: string, title: string, extra: Record<string, unknown> = {}) => ({
  id,
  workspace_id: 'ws1',
  title,
  url: `${origin()}/healthz?l=${id}`,
  ...extra,
})

test.describe('search', () => {
  test.beforeEach(async ({ api }) => {
    await api.seed({
      workspaces: [
        { id: 'ws1', name: 'Home', icon: 'lucide:house' },
        { id: 'ws2', name: 'Lab', icon: 'lucide:flask-conical' },
      ],
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Media' }],
      links: [
        L('jelly', 'Jellyfin', { category_id: 'c1', description: 'Movies and TV' }),
        L('plex', 'Plex', { category_id: 'c1' }),
        L('ha', 'Home Assistant', { description: 'smart home automation' }),
        L('pve', 'Proxmox', { workspace_id: 'ws2' }), // other workspace: search spans all workspaces
      ],
    })
  })

  test('"/" focuses the search box, typing shows ranked results, Esc closes', async ({ page }) => {
    await openApp(page)
    await expect(page.getByTestId('search-results')).toHaveCount(0)
    await page.keyboard.press('/')
    await expect(page.getByTestId('search-input')).toBeFocused()
    await page.keyboard.type('jel')
    await expect(page.getByTestId('search-results')).toBeVisible()
    await expect(page.getByTestId('search-result')).toHaveCount(1)
    await expect(page.getByTestId('search-result').first()).toContainText('Jellyfin')
    await expect(page.getByTestId('search-result').first()).toHaveAttribute('data-link-id', 'jelly')
    await expect(page.getByTestId('search-web')).toBeVisible()
    await page.keyboard.press('Escape') // clears the query
    await expect(page.getByTestId('search-input')).toHaveValue('')
    await expect(page.getByTestId('search-results')).toHaveCount(0)
  })

  test('Ctrl/Cmd+K focuses the search box', async ({ page }) => {
    await openApp(page)
    await page.keyboard.press('ControlOrMeta+k')
    await expect(page.getByTestId('search-input')).toBeFocused()
  })

  test('matches description, category name, url and crosses workspaces; fuzzy subsequence', async ({ page }) => {
    await openApp(page)
    const input = page.getByTestId('search-input')
    const hits = page.getByTestId('search-result')
    await input.fill('automation')
    await expect(hits).toHaveCount(1)
    await expect(hits.first()).toContainText('Home Assistant')
    await input.fill('media') // category name
    await expect(hits).toHaveCount(2)
    await input.fill('prox') // lives in the other workspace
    await expect(hits).toHaveCount(1)
    await expect(hits.first()).toContainText('Proxmox')
    await expect(hits.first()).toContainText('Lab') // result shows where it lives
    await input.fill('l=plex') // url
    await expect(hits).toHaveCount(1)
    await input.fill('jlf') // subsequence
    await expect(hits.first()).toContainText('Jellyfin')
  })

  test('arrow keys move the selection, wrap around; Enter opens the selected link and counts the click', async ({ page, api, context }) => {
    await openApp(page)
    await page.getByTestId('search-input').fill('home') // "Home Assistant" + (fuzzy) others
    const rows = page.getByTestId('search-result')
    await expect(rows.first()).toHaveAttribute('aria-selected', 'true')
    const n = await rows.count()
    expect(n).toBeGreaterThanOrEqual(1)
    // go down to "web search" row and wrap to the first again
    for (let i = 0; i < n; i++) await page.keyboard.press('ArrowDown')
    await expect(page.getByTestId('search-web')).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('ArrowDown')
    await expect(rows.first()).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('ArrowUp')
    await expect(page.getByTestId('search-web')).toHaveAttribute('aria-selected', 'true')

    await page.getByTestId('search-input').fill('plex')
    await expect(rows.first()).toHaveAttribute('aria-selected', 'true')
    const popup = context.waitForEvent('page')
    await page.keyboard.press('Enter')
    const tab = await popup
    await expect.poll(() => tab.url()).toContain('/healthz?l=plex')
    await tab.close()
    await page.bringToFront()
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'plex')!.clicks).toBe(1)
    await expect(page.getByTestId('search-input')).toHaveValue('') // search resets after opening
  })

  test('clicking a result opens it', async ({ page, context }) => {
    await openApp(page)
    await page.getByTestId('search-input').fill('proxmox')
    const popup = context.waitForEvent('page')
    await page.getByTestId('search-result').first().click()
    const tab = await popup
    await expect.poll(() => tab.url()).toContain('/healthz?l=pve')
    await tab.close()
  })

  test('no match: only the web-search row, Enter searches with the configured engine', async ({ page, api, context }) => {
    await api.settings({ search_engine: `${origin()}/healthz?engine=1&q=%s` })
    await openApp(page)
    await page.getByTestId('search-input').fill('zzqx unknown thing')
    await expect(page.getByTestId('search-result')).toHaveCount(0)
    await expect(page.getByTestId('search-web')).toBeVisible()
    await expect(page.getByTestId('search-web')).toContainText('zzqx unknown thing')
    const popup = context.waitForEvent('page')
    await page.keyboard.press('Enter')
    const tab = await popup
    await expect.poll(() => tab.url()).toContain('engine=1&q=zzqx%20unknown%20thing')
    await tab.close()
    await page.bringToFront()
  })

  test('results are capped at 8', async ({ page, api }) => {
    await api.seed({ links: Array.from({ length: 12 }, (_, i) => L('n' + i, 'Node ' + i)) })
    await openApp(page)
    await page.getByTestId('search-input').fill('node')
    await expect(page.getByTestId('search-result')).toHaveCount(8)
  })

  test('digit keys switch workspace when the search box is not focused', async ({ page }) => {
    await openApp(page)
    const btn = page.getByTestId('ws-menu-btn')
    await expect(btn).toHaveAttribute('data-ws-name', 'Home')
    await page.keyboard.press('2')
    await expect(btn).toHaveAttribute('data-ws-name', 'Lab')
    await expect(page.getByRole('link', { name: 'Proxmox' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Plex' })).toHaveCount(0)
    await page.keyboard.press('1')
    await expect(btn).toHaveAttribute('data-ws-name', 'Home')
    // typing digits in the search box must not switch workspaces
    await page.keyboard.press('/')
    await page.keyboard.type('2')
    await expect(btn).toHaveAttribute('data-ws-name', 'Home')
    await expect(page.getByTestId('search-input')).toHaveValue('2')
  })
})
