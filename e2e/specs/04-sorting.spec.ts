import { expect, expectSort, openApp, openSettings, origin, sectionTitles, setSort, test } from '../helpers'

const ago = (s: number) => new Date(Date.now() - s * 1000).toISOString().replace(/\.\d+Z$/, 'Z')
const L = (id: string, title: string, extra: Record<string, unknown> = {}) => ({ id, workspace_id: 'ws1', title, url: `${origin()}/healthz?l=${id}`, ...extra })

test.describe('click counting and sort modes', () => {
  test('clicking a capsule counts the click and (frequency mode) re-orders on next load', async ({ page, api, context }) => {
    await api.seed({ links: [L('a', 'Alpha'), L('b', 'Beta'), L('c', 'Gamma')] })
    await openApp(page)
    const sec = page.getByTestId('category-uncategorized')
    expect(await sectionTitles(sec)).toEqual(['Alpha', 'Beta', 'Gamma'])

    for (const [id, times] of [['c', 2], ['b', 1]] as const) {
      for (let i = 0; i < times; i++) {
        const popup = context.waitForEvent('page')
        await page.getByTestId(`link-${id}`).click()
        const tab = await popup
        await expect.poll(() => tab.url()).toContain(`/healthz?l=${id}`)
        await tab.close()
        await page.bringToFront()
      }
    }
    await expect.poll(async () => Object.fromEntries((await api.links()).map((l) => [l.id, l.clicks]))).toEqual({ a: 0, b: 1, c: 2 })
    const links = await api.links()
    expect(links.find((l) => l.id === 'c')!.last_used_at).toBeTruthy()
    expect(links.find((l) => l.id === 'a')!.last_used_at).toBeNull()
    await page.reload()
    await expect(page.getByTestId('category-uncategorized')).toBeVisible()
    expect(await sectionTitles(page.getByTestId('category-uncategorized'))).toEqual(['Gamma', 'Beta', 'Alpha'])
  })

  test('three sort modes order capsules (pinned + categories each use the rule)', async ({ page, api }) => {
    await api.seed({
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: [
        L('a', 'Alpha', { category_id: 'c1', clicks: 1, last_used_at: ago(10) }),
        L('b', 'Beta', { category_id: 'c1', clicks: 9, last_used_at: ago(3000) }),
        L('c', 'Gamma', { category_id: 'c1', clicks: 4, last_used_at: ago(100) }),
        L('d', 'Delta', { category_id: 'c1', clicks: 0 }),
        L('p1', 'PinLow', { category_id: 'c1', pinned: true, clicks: 2, last_used_at: ago(5), pin_sort: 1 }),
        L('p2', 'PinHigh', { category_id: 'c1', pinned: true, clicks: 8, last_used_at: ago(9000), pin_sort: 0 }),
      ],
    })
    await openApp(page)
    const apps = page.getByTestId('category-c1')
    const pinned = page.getByTestId('section-pinned')
    await expectSort(page, 'frequency')
    expect(await sectionTitles(apps)).toEqual(['Beta', 'PinHigh', 'Gamma', 'PinLow', 'Alpha', 'Delta'])
    expect(await sectionTitles(pinned)).toEqual(['PinHigh', 'PinLow'])
    // pinned links are shown in both places
    await expect(apps.getByTestId('link-p1')).toHaveCount(1)
    await expect(pinned.getByTestId('pin-link-p1')).toHaveCount(1)

    await setSort(page, 'recent')
    await expectSort(page, 'recent')
    expect(await sectionTitles(apps)).toEqual(['PinLow', 'Alpha', 'Gamma', 'Beta', 'PinHigh', 'Delta']) // null last
    expect(await sectionTitles(pinned)).toEqual(['PinLow', 'PinHigh'])
    await expect.poll(async () => (await api.bootstrap()).settings.sort_mode).toBe('recent')

    await setSort(page, 'custom')
    // category rows follow `sort` (list order), the pinned row follows `pin_sort` (independent)
    expect(await sectionTitles(apps)).toEqual(['Alpha', 'Beta', 'Gamma', 'Delta', 'PinLow', 'PinHigh'])
    expect(await sectionTitles(pinned)).toEqual(['PinHigh', 'PinLow'])
    await expect.poll(async () => (await api.bootstrap()).settings.sort_mode).toBe('custom')

    // persisted: survives reload
    await page.reload()
    await expectSort(page, 'custom')
    await setSort(page, 'frequency')
    expect(await sectionTitles(apps)).toEqual(['Beta', 'PinHigh', 'Gamma', 'PinLow', 'Alpha', 'Delta'])
  })

  test('recent mode: a freshly clicked link jumps to the front', async ({ page, api, context }) => {
    await api.seed({ settings: { sort_mode: 'recent' }, links: [L('a', 'Alpha', { last_used_at: ago(500) }), L('b', 'Beta', { last_used_at: ago(100) }), L('c', 'Gamma')] })
    await openApp(page)
    const sec = page.getByTestId('category-uncategorized')
    expect(await sectionTitles(sec)).toEqual(['Beta', 'Alpha', 'Gamma'])
    const popup = context.waitForEvent('page')
    await page.getByTestId('link-c').click()
    await (await popup).close()
    await page.bringToFront()
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'c')!.last_used_at).toBeTruthy()
    await page.reload()
    await expect(page.getByTestId('category-uncategorized')).toBeVisible()
    expect(await sectionTitles(page.getByTestId('category-uncategorized'))).toEqual(['Gamma', 'Beta', 'Alpha'])
  })

  test('sort mode can be changed in Settings > General too', async ({ page, api }) => {
    await api.seed({ links: [L('a', 'Alpha'), L('b', 'Beta', { clicks: 3 })] })
    await openApp(page)
    await openSettings(page)
    await page.getByTestId('set-sort').selectOption('custom')
    await expect.poll(async () => (await api.bootstrap()).settings.sort_mode).toBe('custom')
    await page.getByTestId('modal-close').click()
    await expectSort(page, 'custom')
    expect(await sectionTitles(page.getByTestId('category-uncategorized'))).toEqual(['Alpha', 'Beta'])
  })
})
