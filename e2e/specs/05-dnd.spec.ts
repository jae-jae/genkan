import { drag, expect, expectSort, openApp, openWsMenu, origin, sectionTitles, test, toggleEdit } from '../helpers'

const L = (id: string, title: string, extra: Record<string, unknown> = {}) => ({ id, workspace_id: 'ws1', title, url: `${origin()}/healthz?l=${id}`, ...extra })
const order = async (api: any, filter: (l: any) => boolean) =>
  (await api.links()).filter(filter).sort((a: any, b: any) => a.sort - b.sort).map((l: any) => l.id)

test.describe('drag & drop (edit mode, pointer events)', () => {
  // tall viewport: dragging near the bottom edge auto-scrolls the page
  test.use({ viewport: { width: 1280, height: 1100 } })

  test('reorder links inside a category (custom mode)', async ({ page, api }) => {
    await api.seed({
      settings: { sort_mode: 'custom' },
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: ['a', 'b', 'c', 'd'].map((id) => L(id, id.toUpperCase(), { category_id: 'c1' })),
    })
    await openApp(page)
    await toggleEdit(page)
    const sec = page.getByTestId('category-c1')
    expect(await sectionTitles(sec)).toEqual(['A', 'B', 'C', 'D'])
    await drag(page, page.getByTestId('link-a'), page.getByTestId('link-c'), 'after')
    await expect.poll(async () => sectionTitles(sec)).toEqual(['B', 'C', 'A', 'D'])
    await expect.poll(() => order(api, (l) => l.category_id === 'c1')).toEqual(['b', 'c', 'a', 'd'])
    // and back to front
    await drag(page, page.getByTestId('link-d'), page.getByTestId('link-b'), 'before')
    await expect.poll(async () => sectionTitles(sec)).toEqual(['D', 'B', 'C', 'A'])
    await expect.poll(() => order(api, (l) => l.category_id === 'c1')).toEqual(['d', 'b', 'c', 'a'])
    // persisted
    await page.reload()
    await expect(page.getByTestId('category-c1')).toBeVisible()
    expect(await sectionTitles(page.getByTestId('category-c1'))).toEqual(['D', 'B', 'C', 'A'])
  })

  test('dragging while in frequency mode switches to custom, keeps the visible order and shows a toast', async ({ page, api }) => {
    await api.seed({
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: [L('a', 'A', { category_id: 'c1', clicks: 1 }), L('b', 'B', { category_id: 'c1', clicks: 9 }), L('c', 'C', { category_id: 'c1', clicks: 5 })],
    })
    await openApp(page)
    await toggleEdit(page)
    const sec = page.getByTestId('category-c1')
    expect(await sectionTitles(sec)).toEqual(['B', 'C', 'A']) // frequency order
    await drag(page, page.getByTestId('link-a'), page.getByTestId('link-b'), 'before')
    await expect(page.getByTestId('toast').first()).toBeVisible()
    await expectSort(page, 'custom')
    await expect.poll(async () => sectionTitles(sec)).toEqual(['A', 'B', 'C'])
    await expect.poll(async () => (await api.bootstrap()).settings.sort_mode).toBe('custom')
    await expect.poll(() => order(api, (l) => l.category_id === 'c1')).toEqual(['a', 'b', 'c'])
  })

  test('move a link to another category (cross-category) and to an empty category', async ({ page, api }) => {
    await api.seed({
      settings: { sort_mode: 'custom' },
      categories: [
        { id: 'c1', workspace_id: 'ws1', name: 'One' },
        { id: 'c2', workspace_id: 'ws1', name: 'Two' },
        { id: 'c3', workspace_id: 'ws1', name: 'Three' },
      ],
      links: [L('a', 'A', { category_id: 'c1' }), L('b', 'B', { category_id: 'c1' }), L('x', 'X', { category_id: 'c2' }), L('y', 'Y', { category_id: 'c2' })],
    })
    await openApp(page)
    await toggleEdit(page)
    await drag(page, page.getByTestId('link-a'), page.getByTestId('link-x'), 'after')
    await expect(page.getByTestId('category-c2').getByTestId('link-a')).toBeVisible()
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'a')!.category_id).toBe('c2')
    expect(await sectionTitles(page.getByTestId('category-c2'))).toEqual(['X', 'A', 'Y'])
    await expect.poll(() => order(api, (l) => l.category_id === 'c2')).toEqual(['x', 'a', 'y'])
    expect(await sectionTitles(page.getByTestId('category-c1'))).toEqual(['B'])

    // into an empty category (drop on its "+" / empty flow area)
    await drag(page, page.getByTestId('link-b'), page.getByTestId('category-c3').locator('[data-dnd-flow]'))
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'b')!.category_id).toBe('c3')
    await expect(page.getByTestId('category-c3').getByTestId('link-b')).toBeVisible()
  })

  test('drag into the pinned row pins the link (it stays in its category); pinned row order is independent (pin_sort)', async ({ page, api }) => {
    await api.seed({
      settings: { sort_mode: 'custom' },
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: [L('a', 'A', { category_id: 'c1' }), L('b', 'B', { category_id: 'c1' }), L('p', 'P', { pinned: true })],
    })
    await openApp(page)
    await toggleEdit(page)
    const pinned = page.getByTestId('section-pinned')
    const before = await api.links()
    await drag(page, page.getByTestId('link-b'), page.getByTestId('pin-link-p'), 'before')
    await expect(pinned.getByTestId('pin-link-b')).toBeVisible()
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'b')!.pinned).toBe(true)
    await expect.poll(() => sectionTitles(pinned)).toEqual(['B', 'P'])
    const pinOrder = async () => (await api.links()).filter((l) => l.pinned).sort((x, y) => x.pin_sort - y.pin_sort).map((l) => l.id)
    await expect.poll(pinOrder).toEqual(['b', 'p'])
    // the link is still a member of its category, and category order/sort values are untouched
    await expect(page.getByTestId('category-c1').getByTestId('link-b')).toBeVisible()
    await expect.poll(() => sectionTitles(page.getByTestId('category-c1'))).toEqual(['A', 'B'])
    const after = await api.links()
    for (const l of after) {
      expect(l.sort, `sort of ${l.id}`).toBe(before.find((x) => x.id === l.id)!.sort)
      expect(l.category_id, `category of ${l.id}`).toBe(before.find((x) => x.id === l.id)!.category_id)
    }

    // reorder inside the pinned row: only pin_sort changes
    await drag(page, page.getByTestId('pin-link-p'), page.getByTestId('pin-link-b'), 'before')
    await expect.poll(pinOrder).toEqual(['p', 'b'])
    await expect.poll(() => sectionTitles(pinned)).toEqual(['P', 'B'])
    await expect.poll(() => sectionTitles(page.getByTestId('category-c1'))).toEqual(['A', 'B'])
    for (const l of await api.links()) expect(l.sort, `sort of ${l.id}`).toBe(before.find((x) => x.id === l.id)!.sort)

    // dragging a pinned-row capsule into a category is cancelled: nothing changes, nothing is unpinned
    const snapshot = JSON.stringify((await api.links()).map((l) => [l.id, l.pinned, l.category_id, l.sort, l.pin_sort]))
    await drag(page, page.getByTestId('pin-link-p'), page.getByTestId('link-a'), 'before')
    await expect(page.getByTestId('category-c1').getByTestId('link-p')).toHaveCount(0)
    await expect(pinned.getByTestId('pin-link-p')).toBeVisible()
    expect(JSON.stringify((await api.links()).map((l) => [l.id, l.pinned, l.category_id, l.sort, l.pin_sort]))).toBe(snapshot)
  })

  test('REST: reorder __pinned only rewrites pin_sort and pinned', async ({ api }) => {
    await api.seed({
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: [L('a', 'A', { category_id: 'c1', pinned: true, pin_sort: 0 }), L('b', 'B', { category_id: 'c1', pinned: true, pin_sort: 1 }), L('c', 'C', { category_id: 'c1' })],
    })
    const snap = (await api.links()).map((l) => ({ id: l.id, sort: l.sort, category_id: l.category_id }))
    await api.call('POST', '/api/links/reorder', { workspace_id: 'ws1', category_id: '__pinned', ids: ['b', 'c', 'a'] })
    const links = await api.links()
    expect(links.filter((l) => l.pinned).sort((x, y) => x.pin_sort - y.pin_sort).map((l) => l.id)).toEqual(['b', 'c', 'a'])
    expect(links.map((l) => ({ id: l.id, sort: l.sort, category_id: l.category_id }))).toEqual(snap)
  })

  test('reorder categories by their handle', async ({ page, api }) => {
    await api.seed({
      categories: ['c1', 'c2', 'c3'].map((id, i) => ({ id, workspace_id: 'ws1', name: 'Cat ' + (i + 1) })),
      links: ['c1', 'c2', 'c3'].map((c) => L('l' + c, 'L' + c, { category_id: c })),
    })
    await openApp(page)
    await toggleEdit(page)
    const titles = () => page.locator('[data-dnd-kind="category"] .sec-title').allTextContents()
    expect(await titles()).toEqual(['Cat 1', 'Cat 2', 'Cat 3'])
    await drag(page, page.getByTestId('category-handle-c1'), page.getByTestId('category-c3'), 'after')
    await expect.poll(titles).toEqual(['Cat 2', 'Cat 3', 'Cat 1'])
    await expect.poll(async () => (await api.bootstrap()).categories.sort((a, b) => a.sort - b.sort).map((c) => c.id)).toEqual(['c2', 'c3', 'c1'])
  })

  test('reorder workspaces with the up / down buttons in the dropdown (edit mode only)', async ({ page, api }) => {
    await api.seed({ workspaces: ['One', 'Two', 'Three'].map((n, i) => ({ id: 'w' + (i + 1), name: n })) })
    await openApp(page)
    const names = () => page.locator('[data-testid^="ws-item-"]').evaluateAll((els) => els.map((e) => e.getAttribute('data-ws-name')))
    // not in edit mode: no move buttons
    await openWsMenu(page)
    await expect(page.locator('[data-testid^="ws-up-"]')).toHaveCount(0)
    await page.keyboard.press('Escape')
    await toggleEdit(page)
    await openWsMenu(page)
    expect(await names()).toEqual(['One', 'Two', 'Three'])
    // edge buttons are inert
    await expect(page.getByTestId('ws-up-w1')).toHaveAttribute('aria-disabled', 'true')
    await expect(page.getByTestId('ws-down-w3')).toHaveAttribute('aria-disabled', 'true')
    await page.getByTestId('ws-down-w1').click()
    await expect.poll(names).toEqual(['Two', 'One', 'Three'])
    await page.getByTestId('ws-down-w1').click() // the menu stays open and keeps focus on the same control
    await expect.poll(names).toEqual(['Two', 'Three', 'One'])
    await expect(page.getByTestId('ws-down-w1')).toBeFocused()
    await page.getByTestId('ws-up-w3').click()
    await expect.poll(names).toEqual(['Three', 'Two', 'One'])
    await expect.poll(async () => (await api.bootstrap()).workspaces.map((w) => w.id)).toEqual(['w3', 'w2', 'w1'])
    // digit shortcuts follow the new order
    await page.keyboard.press('Escape')
    await page.keyboard.press('1')
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Three')
  })

  test('a click (no movement) on a capsule in edit mode opens the editor instead of dragging', async ({ page, api }) => {
    await api.seed({ links: [L('a', 'Alpha')] })
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId('link-a').click()
    await expect(page.getByTestId('link-modal')).toBeVisible()
    await expect(page.getByTestId('link-title')).toHaveValue('Alpha')
  })
})
