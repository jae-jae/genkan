import { expect, openApp, origin, pinCap, test, toggleEdit, linkCap, sectionTitles } from '../helpers'

const L = (id: string, title: string, extra: Record<string, unknown> = {}) => ({ id, workspace_id: 'ws1', title, url: `${origin()}/healthz?l=${id}`, ...extra })

test.describe('board layout: one category per row, capsule descriptions, pinned row', () => {
  test('every category is its own full-width row, stacked vertically (desktop)', async ({ page, api }) => {
    await api.seed({
      categories: ['c1', 'c2', 'c3', 'c4'].map((id, i) => ({ id, workspace_id: 'ws1', name: 'Category ' + (i + 1) })),
      links: ['c1', 'c2', 'c3', 'c4'].flatMap((c) => ['a', 'b'].map((x) => L(c + x, `Link ${c}${x}`, { category_id: c }))),
    })
    await openApp(page)
    const board = (await page.getByTestId('board').boundingBox())!
    await expect(page.getByTestId('board')).not.toHaveAttribute('data-cols', /.*/)
    const boxes = []
    for (const c of ['c1', 'c2', 'c3', 'c4']) boxes.push((await page.getByTestId(`category-${c}`).boundingBox())!)
    for (let i = 0; i < boxes.length; i++) {
      // all sections share the same left edge and the same (full) width ...
      expect(Math.abs(boxes[i].x - boxes[0].x)).toBeLessThan(1)
      expect(Math.abs(boxes[i].width - boxes[0].width)).toBeLessThan(1)
      // ... which spans the board's content width (minus its padding)
      expect(boxes[i].width).toBeGreaterThan(board.width * 0.85)
      // and each sits strictly below the previous one: no two categories on one line
      if (i) expect(boxes[i].y).toBeGreaterThanOrEqual(boxes[i - 1].y + boxes[i - 1].height - 1)
    }
    // capsules wrap inside the row (two capsules of one category share a line)
    const a = (await linkCap(page, 'c1a').boundingBox())!
    const b = (await linkCap(page, 'c1b').boundingBox())!
    expect(Math.abs(a.y - b.y)).toBeLessThan(2)
    expect(b.x).toBeGreaterThan(a.x)
    // the same on a wide screen
    await page.setViewportSize({ width: 1920, height: 1080 })
    const wide = []
    for (const c of ['c1', 'c2', 'c3']) wide.push((await page.getByTestId(`category-${c}`).boundingBox())!)
    expect(wide[1].y).toBeGreaterThan(wide[0].y + wide[0].height - 1)
    expect(wide[2].y).toBeGreaterThan(wide[1].y + wide[1].height - 1)
    expect(Math.abs(wide[1].width - wide[0].width)).toBeLessThan(1)
  })

  test('capsule shows its description as a second line; no description keeps it single-line', async ({ page, api }) => {
    await api.seed({
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: [
        L('d', 'With desc', { category_id: 'c1', description: 'Photo backup and gallery' }),
        L('n', 'No desc', { category_id: 'c1' }),
        L('long', 'Long desc', { category_id: 'c1', description: 'x'.repeat(300) }),
      ],
    })
    await openApp(page)
    const d = linkCap(page, 'd')
    await expect(d.locator('.cap-title')).toHaveText('With desc')
    await expect(d.locator('.cap-desc')).toHaveText('Photo backup and gallery')
    await expect(linkCap(page, 'n').locator('.cap-desc')).toHaveCount(0)
    // description sits below the title, is smaller and dimmer
    const t = (await d.locator('.cap-title').boundingBox())!
    const ds = (await d.locator('.cap-desc').boundingBox())!
    expect(ds.y).toBeGreaterThanOrEqual(t.y + t.height - 2)
    const st = await d.locator('.cap-desc').evaluate((e) => ({ fs: parseFloat(getComputedStyle(e).fontSize), op: getComputedStyle(e).opacity, ws: getComputedStyle(e).whiteSpace, to: getComputedStyle(e).textOverflow }))
    expect(st.fs).toBeLessThanOrEqual(12.5)
    expect(st.fs).toBeGreaterThanOrEqual(11)
    expect(st.ws).toBe('nowrap')
    expect(st.to).toBe('ellipsis')
    expect(parseFloat(st.op)).toBeLessThan(1)
    // a 300-char description does not blow the capsule up; it stays on one line
    const lb = (await linkCap(page, 'long').boundingBox())!
    expect(lb.width).toBeLessThan(420)
    expect(lb.height).toBeLessThan(80)
    const dh = (await d.boundingBox())!.height
    const nh = (await linkCap(page, 'n').boundingBox())!.height
    expect(dh).toBeGreaterThanOrEqual(nh) // two-line capsule is at least as tall
    // the tooltip still carries description + url
    await expect(d).toHaveAttribute('title', new RegExp('Photo backup and gallery'))
  })

  test('no pinned heading; pinned link shows in the row AND in its category; unpinning removes only the row copy', async ({ page, api }) => {
    await api.seed({
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: [L('a', 'Alpha', { category_id: 'c1' }), L('p', 'Top One', { category_id: 'c1', pinned: true }), L('u', 'Loose one', { pinned: true })],
    })
    await openApp(page)
    const row = page.getByTestId('section-pinned')
    await expect(row).toBeVisible()
    await expect(page.getByTestId('section-pinned-title')).toHaveCount(0)
    await expect(row.locator('h1, h2, h3, h4, .sec-title, .sec-head')).toHaveCount(0)
    await expect(row).not.toContainText(/收藏|pinned|favou?rites?/i)
    // the pinned row is above all categories
    const rb = (await row.boundingBox())!
    const cb = (await page.getByTestId('category-c1').boundingBox())!
    expect(rb.y + rb.height).toBeLessThanOrEqual(cb.y + 1)
    // both copies are present, with distinct test ids and the same link
    await expect(pinCap(page, 'p')).toBeVisible()
    await expect(page.getByTestId('category-c1').getByTestId('link-p')).toBeVisible()
    await expect(pinCap(page, 'p')).toHaveAttribute('href', `${origin()}/healthz?l=p`)
    await expect(linkCap(page, 'p')).toHaveAttribute('href', `${origin()}/healthz?l=p`)
    // an uncategorised pinned link also lives in the uncategorised section
    await expect(page.getByTestId('category-uncategorized').getByTestId('link-u')).toBeVisible()
    await expect(pinCap(page, 'u')).toBeVisible()
    // no "收藏" / "Pinned" word anywhere in the board (en and zh)
    await expect(page.getByTestId('board')).not.toContainText(/收藏|Pinned/)
    await page.evaluate(() => localStorage.setItem('genkan.lang', 'zh'))
    await page.reload()
    await expect(page.getByTestId('board')).toBeVisible()
    await expect(page.getByTestId('board')).not.toContainText('收藏')

    // unpin: gone from the row, still in the category
    await api.call('PATCH', '/api/links/p', { pinned: false })
    await expect(pinCap(page, 'p')).toHaveCount(0)
    await expect(page.getByTestId('category-c1').getByTestId('link-p')).toBeVisible()
  })

  test('pinned row order (custom mode) is independent of the category order', async ({ page, api }) => {
    await api.seed({
      settings: { sort_mode: 'custom' },
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: [
        L('a', 'A', { category_id: 'c1', pinned: true, pin_sort: 2 }),
        L('b', 'B', { category_id: 'c1', pinned: true, pin_sort: 0 }),
        L('c', 'C', { category_id: 'c1', pinned: true, pin_sort: 1 }),
        L('d', 'D', { category_id: 'c1' }),
      ],
    })
    await openApp(page)
    expect(await sectionTitles(page.getByTestId('section-pinned'))).toEqual(['B', 'C', 'A'])
    expect(await sectionTitles(page.getByTestId('category-c1'))).toEqual(['A', 'B', 'C', 'D'])
    // pinning a new link appends it to the end of the pinned row
    await api.call('PATCH', '/api/links/d', { pinned: true })
    await expect.poll(() => sectionTitles(page.getByTestId('section-pinned'))).toEqual(['B', 'C', 'A', 'D'])
    expect(await sectionTitles(page.getByTestId('category-c1'))).toEqual(['A', 'B', 'C', 'D'])
    const d = (await api.links()).find((l) => l.id === 'd')!
    expect(d.pin_sort).toBeGreaterThan(2)
    // REST reorder of the pinned row only changes the pinned row
    await api.call('POST', '/api/links/reorder', { workspace_id: 'ws1', category_id: '__pinned', ids: ['d', 'a', 'c', 'b'] })
    await expect.poll(() => sectionTitles(page.getByTestId('section-pinned'))).toEqual(['D', 'A', 'C', 'B'])
    expect(await sectionTitles(page.getByTestId('category-c1'))).toEqual(['A', 'B', 'C', 'D'])
  })

  test('edit mode: pinned-row capsules have their own edit / delete buttons; edit mode shows the empty pinned row', async ({ page, api }) => {
    await api.seed({ categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }], links: [L('p', 'Top One', { category_id: 'c1', pinned: true })] })
    await openApp(page)
    await toggleEdit(page)
    await expect(page.getByTestId('pin-link-edit-p')).toBeVisible()
    await expect(page.getByTestId('link-edit-p')).toBeVisible()
    await page.getByTestId('pin-link-delete-p').click()
    await page.getByTestId('confirm-ok').click()
    await expect(page.getByTestId('pin-link-p')).toHaveCount(0)
    await expect(page.getByTestId('link-p')).toHaveCount(0) // the link itself is gone from both places
    await expect(page.getByTestId('section-pinned')).toBeVisible() // edit mode keeps the (empty) row as a drop target
  })
})
