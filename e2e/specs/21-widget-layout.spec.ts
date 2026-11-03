import type { Locator, Page } from '@playwright/test'
import { Api, expect, openApp, origin, test, toggleEdit } from '../helpers'

// Widget layout v2 (docs/PLUGINS.md 11.1 / 11.5): masonry grid, `w` / `h`, drag to reorder, resize handle, widget context
// menu, "Add widget" in the page context menu. Only offline catalog plugins: world-clock (size m -> default w 4) and
// countdown (size s -> default w 2). Every test starts from `api.reset()` (the `api` fixture).

interface W { id: string; plugin: string; widget: string; workspace_id: string; sort: number; w: number; h: number }
const widgetsOf = async (api: Api): Promise<W[]> => ((await api.bootstrap()) as any).widgets as W[]
const widgetOf = async (api: Api, id: string) => (await widgetsOf(api)).find((w) => w.id === id)!
const install = (api: Api, id: string) => api.call('POST', '/api/plugins', { catalog: id, enable: true })
const add = (api: Api, plugin: string, widget: string, extra: Record<string, unknown> = {}) =>
  api.call<W>('POST', '/api/widgets', { plugin, widget, workspace_id: '', config: plugin === 'countdown' ? { label: 'Event', date: '2099-01-01' } : {}, ...extra })
const card = (page: Page, id: string) => page.locator(`[data-testid="widget"][data-widget-id="${id}"]`)
const domOrder = (page: Page) => page.getByTestId('widget').evaluateAll((els) => els.map((e) => e.getAttribute('data-widget-id')!))

/** Wait until every card has loaded its data and the layout has stopped moving (cards grow when their content arrives). */
async function settled(page: Page, n?: number) {
  const cards = page.getByTestId('widget')
  if (n !== undefined) await expect(cards).toHaveCount(n)
  await expect(page.locator('[data-testid="widget"][aria-busy="true"]')).toHaveCount(0)
  await expect(page.getByTestId('widget-skeleton')).toHaveCount(0)
  let prev = ''
  await expect
    .poll(async () => {
      const now = JSON.stringify(await page.locator('.wgi').evaluateAll((els) => els.map((e) => { const r = e.getBoundingClientRect(); return [r.x, r.y, r.width, r.height].map(Math.round) })))
      const same = now === prev
      prev = now
      return same
    })
    .toBe(true)
}

/** A slow, deterministic drag: the reorder preview re-evaluates at most every ~170 ms, so dwell on the way and at the end. */
async function slowDrag(page: Page, from: Locator, to: Locator, where: 'before' | 'after') {
  const a = (await from.boundingBox())!
  const b = (await to.boundingBox())!
  const sx = a.x + a.width / 2
  const sy = a.y + a.height / 2
  // top-left half of the target inserts before it, bottom-right half after it
  const tx = where === 'before' ? b.x + b.width * 0.15 : b.x + b.width * 0.85
  const ty = where === 'before' ? b.y + b.height * 0.15 : b.y + b.height * 0.85
  await page.mouse.move(sx, sy)
  await page.mouse.down()
  await page.mouse.move(sx + 10, sy + 10, { steps: 3 })
  for (let i = 1; i <= 8; i++) {
    await page.mouse.move(sx + ((tx - sx) * i) / 8, sy + ((ty - sy) * i) / 8)
    await page.waitForTimeout(40)
  }
  await page.waitForTimeout(260)
  await page.mouse.move(tx + 1, ty + 1)
  await page.waitForTimeout(260)
  await page.mouse.up()
}

/**
 * Right-click a card and pick a menu item. The menu is a snapshot taken when it opens and it closes on any layout shift, and a
 * bootstrap reload that raced the previous PATCH can briefly show the old value - so the whole open + (check) + click is retried.
 */
async function ctxPick(page: Page, c: Locator, item: string, checked?: Record<string, 'true' | 'false'>) {
  await expect(async () => {
    await c.click({ button: 'right', position: { x: 30, y: 20 }, timeout: 2000 })
    for (const [id, v] of Object.entries(checked ?? {})) await expect(page.getByTestId(id)).toHaveAttribute('aria-checked', v, { timeout: 1000 })
    await page.getByTestId(item).click({ timeout: 2000 })
  }).toPass({ timeout: 10_000 })
}

const box = async (l: Locator) => (await l.boundingBox())!
const intersects = (a: { x: number; y: number; width: number; height: number }, b: typeof a) =>
  a.x < b.x + b.width - 0.5 && b.x < a.x + a.width - 0.5 && a.y < b.y + b.height - 0.5 && b.y < a.y + a.height - 0.5

test.describe('widget layout (masonry, drag, resize, context menu)', () => {
  // The suite shares one container; leave it without plugins even when a test fails half way.
  test.afterAll(async ({ admin }) => {
    await new Api(admin).reset()
  })

  test('the add card comes first in edit mode (DOM and visually); default widths follow the manifest size', async ({ page, api }) => {
    await install(api, 'world-clock')
    await install(api, 'countdown')
    const clock = await add(api, 'world-clock', 'clock')
    const cd = await add(api, 'countdown', 'timer')
    expect([clock.w, clock.h, cd.w, cd.h]).toEqual([0, 0, 0, 0]) // always present in the JSON, 0 = default / auto
    await openApp(page)
    await settled(page, 2)
    // outside edit mode there is no add card
    await expect(page.getByTestId('widget-add')).toHaveCount(0)
    // defaults: m = 4 columns, s = 2 columns (of 6)
    await expect(card(page, clock.id)).toHaveAttribute('data-w', '4')
    await expect(card(page, cd.id)).toHaveAttribute('data-w', '2')
    await expect(card(page, clock.id)).toHaveAttribute('data-h', '0')
    const area = await box(page.getByTestId('widget-area'))
    expect((await box(card(page, clock.id))).width).toBeGreaterThan(area.width * 0.6)
    expect((await box(card(page, cd.id))).width).toBeLessThan(area.width * 0.4)

    await toggleEdit(page)
    await settled(page, 2)
    const add1 = page.getByTestId('widget-add')
    await expect(add1).toBeVisible()
    // first child of the area ...
    expect(await page.getByTestId('widget-area').evaluate((el) => el.firstElementChild!.classList.contains('wgi-add'))).toBe(true)
    // ... and the first thing the eye meets: top-left of every card
    const a = await box(add1)
    for (const id of [clock.id, cd.id]) {
      const c = await box(card(page, id))
      expect(a.y < c.y + 1 && (a.y < c.y - 1 || a.x < c.x), `add card before ${id}`).toBe(true)
    }
    // cards keep their DOM order after the add card
    expect(await domOrder(page)).toEqual([clock.id, cd.id])
    // the add card opens the widget picker
    await add1.click()
    await expect(page.getByTestId('widget-picker')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('widget-modal')).toHaveCount(0)
  })

  test('drag a card to a new place: the order persists on the server and after a reload', async ({ page, api }) => {
    await install(api, 'countdown')
    // four equal cards (2 columns each) so a drag behaves like a sortable list; wide cards would grow under the pointer
    const made: W[] = []
    for (const l of ['One', 'Two', 'Three', 'Four']) made.push(await add(api, 'countdown', 'timer', { config: { label: l, date: '2099-01-01' } }))
    const ids = made.map((w) => w.id)
    await openApp(page)
    await toggleEdit(page)
    await settled(page, 4)
    expect(await domOrder(page)).toEqual(ids)

    // first card -> after the last one
    await slowDrag(page, page.getByTestId(`widget-grip-${ids[0]}`), card(page, ids[3]), 'after')
    const want1 = [ids[1], ids[2], ids[3], ids[0]]
    await expect.poll(() => domOrder(page)).toEqual(want1)
    await expect.poll(async () => (await widgetsOf(api)).sort((x, y) => x.sort - y.sort).map((w) => w.id)).toEqual(want1)
    // the drop clears every drag artefact
    await expect(page.locator('.dnd-ghost')).toHaveCount(0)
    await expect(page.locator('.wgi[data-ph]')).toHaveCount(0)
    await expect(page.getByTestId('widget-area')).not.toHaveAttribute('data-dragging', '1')

    await page.reload()
    await toggleEdit(page)
    await settled(page, 4)
    expect(await domOrder(page)).toEqual(want1)

    // dragging by the card body (not the grip) works too: last -> before the first
    await slowDrag(page, card(page, ids[0]).locator('.wg-head'), card(page, ids[1]), 'before')
    const want2 = [ids[0], ids[1], ids[2], ids[3]]
    await expect.poll(() => domOrder(page)).toEqual(want2)
    await expect.poll(async () => (await widgetsOf(api)).sort((x, y) => x.sort - y.sort).map((w) => w.id)).toEqual(want2)
  })

  test('Esc during a drag cancels it; a drag that goes nowhere changes nothing', async ({ page, api }) => {
    await install(api, 'countdown')
    const a = await add(api, 'countdown', 'timer')
    const b = await add(api, 'countdown', 'timer', { config: { label: 'Two', date: '2099-02-02' } })
    await openApp(page)
    await toggleEdit(page)
    await settled(page, 2)
    const from = page.getByTestId(`widget-grip-${a.id}`)
    const fb = await box(from)
    const tb = await box(card(page, b.id))
    await page.mouse.move(fb.x + fb.width / 2, fb.y + fb.height / 2)
    await page.mouse.down()
    await page.mouse.move(fb.x + 14, fb.y + 14, { steps: 3 })
    await page.mouse.move(tb.x + tb.width * 0.9, tb.y + tb.height * 0.9, { steps: 10 })
    await page.waitForTimeout(300)
    await page.keyboard.press('Escape')
    await page.mouse.up()
    await expect(page.locator('.dnd-ghost')).toHaveCount(0)
    expect(await domOrder(page)).toEqual([a.id, b.id])
    expect((await widgetsOf(api)).sort((x, y) => x.sort - y.sort).map((w) => w.id)).toEqual([a.id, b.id])
  })

  test('resize handle: columns snap, height in 8px steps; the server gets {w,h}; double-click restores auto height', async ({ page, api }) => {
    await install(api, 'world-clock')
    const clock = await add(api, 'world-clock', 'clock') // 4 columns, auto height
    await openApp(page)
    await toggleEdit(page)
    await settled(page, 1)
    const handle = page.getByTestId(`widget-resize-${clock.id}`)
    await expect(handle).toBeVisible()
    const c0 = await box(card(page, clock.id))
    const area = await box(page.getByTestId('widget-area'))
    const colW = (area.width + 16) / 6 // ~one column (the exact gap does not matter for a snap)
    const hb = await box(handle)
    const hx = hb.x + hb.width / 2
    const hy = hb.y + hb.height / 2

    // narrower by one column and taller by ~100px
    await page.mouse.move(hx, hy)
    await page.mouse.down()
    await page.mouse.move(hx - colW * 0.7, hy + 40, { steps: 5 })
    await page.mouse.move(hx - colW, hy + 100, { steps: 5 })
    // live preview while dragging: the size tip shows "columns/total x height"
    await expect(page.locator('.wg-size-tip')).toBeVisible()
    await expect(page.locator('.wg-size-tip')).toContainText('×')
    await page.mouse.up()
    await expect(page.locator('.wg-size-tip')).toHaveCount(0)

    await expect.poll(async () => (await widgetOf(api, clock.id)).w).toBe(3)
    const saved = await widgetOf(api, clock.id)
    expect(saved.h).toBeGreaterThanOrEqual(80)
    expect(saved.h % 8).toBe(0) // vertical steps of 8px
    expect(Math.abs(saved.h - (c0.height + 100))).toBeLessThanOrEqual(16)
    await expect(card(page, clock.id)).toHaveAttribute('data-w', '3')
    await expect(card(page, clock.id)).toHaveAttribute('data-h', String(saved.h))
    await expect(card(page, clock.id)).toHaveAttribute('data-fixed', '1')
    expect(Math.round((await box(card(page, clock.id))).height)).toBe(saved.h)
    const w3 = (await box(card(page, clock.id))).width
    expect(w3).toBeLessThan(c0.width - colW * 0.5)

    // survives a reload (and /api/bootstrap agrees: this is what the board reads)
    await page.reload()
    await toggleEdit(page)
    await settled(page, 1)
    await expect(card(page, clock.id)).toHaveAttribute('data-w', '3')
    await expect(card(page, clock.id)).toHaveAttribute('data-h', String(saved.h))

    // double-click on the handle: h -> 0 (auto height); the width stays
    await page.getByTestId(`widget-resize-${clock.id}`).dblclick()
    await expect.poll(async () => (await widgetOf(api, clock.id)).h).toBe(0)
    expect((await widgetOf(api, clock.id)).w).toBe(3)
    await expect(card(page, clock.id)).toHaveAttribute('data-h', '0')
    await expect(card(page, clock.id)).not.toHaveAttribute('data-fixed', '1')
    expect((await box(card(page, clock.id))).height).toBeCloseTo(c0.height, -1) // back to the content height (+-5px)
  })

  test('resize handle by keyboard: arrows change columns / height, Home restores auto height', async ({ page, api }) => {
    await install(api, 'countdown')
    const cd = await add(api, 'countdown', 'timer')
    await openApp(page)
    await toggleEdit(page)
    await settled(page, 1)
    const handle = page.getByTestId(`widget-resize-${cd.id}`)
    await handle.focus()
    await page.keyboard.press('ArrowRight') // 2 -> 3 columns
    await expect.poll(async () => (await widgetOf(api, cd.id)).w).toBe(3)
    await page.keyboard.press('ArrowDown') // fixed height = current + 40
    await expect.poll(async () => (await widgetOf(api, cd.id)).h).toBeGreaterThanOrEqual(80)
    await page.keyboard.press('Home')
    await expect.poll(async () => (await widgetOf(api, cd.id)).h).toBe(0)
    await expect(handle).toBeFocused()
  })

  test('REST: w / h are validated and clamped; PATCH changes only what is sent; w:0 / h:0 mean defaults', async ({ api, ws }) => {
    await install(api, 'countdown')
    const cd = await add(api, 'countdown', 'timer', { w: 3, h: 200 })
    expect([cd.w, cd.h]).toEqual([3, 200])
    const p1 = await api.call<W>('PATCH', `/api/widgets/${cd.id}`, { h: 9999 })
    expect([p1.w, p1.h]).toEqual([3, 1200]) // w untouched, h clamped
    const p2 = await api.call<W>('PATCH', `/api/widgets/${cd.id}`, { w: 99, h: 5 })
    expect([p2.w, p2.h]).toEqual([6, 80])
    const p3 = await api.call<W>('PATCH', `/api/widgets/${cd.id}`, { w: 0, h: 0 })
    expect([p3.w, p3.h]).toEqual([0, 0])
    const p4 = await api.call<W>('PATCH', `/api/widgets/${cd.id}`, { workspace_id: ws })
    expect([p4.workspace_id, p4.w, p4.h]).toEqual([ws, 0, 0])
    const p5 = await api.call<W>('PATCH', `/api/widgets/${cd.id}`, { workspace_id: '' })
    expect(p5.workspace_id).toBe('')
    // the export / import round-trip keeps them
    await api.call('PATCH', `/api/widgets/${cd.id}`, { w: 5, h: 320 })
    const exp = await api.call<any>('GET', '/api/export')
    expect(exp.widgets.find((w: W) => w.id === cd.id)).toMatchObject({ w: 5, h: 320 })
  })

  test('widget context menu (admin): size preset, height, scope, delete; a guest only gets refresh', async ({ page, api, ws, browser }) => {
    await install(api, 'world-clock')
    await install(api, 'countdown')
    const second = await api.workspace({ name: 'Second' })
    const clock = await add(api, 'world-clock', 'clock')
    const cd = await add(api, 'countdown', 'timer', { h: 200 })
    await openApp(page)
    await settled(page, 2)

    // right-click opens the widget menu, not the page menu
    await card(page, cd.id).click({ button: 'right', position: { x: 30, y: 20 } })
    const menu = page.getByTestId('ctx-menu-widget')
    await expect(menu).toBeVisible()
    await expect(page.getByTestId('ctx-menu-page')).toHaveCount(0)
    const ids = await menu.locator('[role^="menuitem"]').evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')))
    expect(ids).toEqual(['ctx-widget-refresh', 'ctx-widget-config', 'ctx-widget-size-s', 'ctx-widget-size-m', 'ctx-widget-size-l', 'ctx-widget-size-full', 'ctx-widget-height-auto', 'ctx-widget-scope-ws', 'ctx-widget-scope-all', 'ctx-widget-delete'])
    // countdown is size s -> 2 columns -> "small" checked; h = 200 -> auto height not checked; scope: all workspaces
    await expect(page.getByTestId('ctx-widget-size-s')).toHaveAttribute('aria-checked', 'true')
    await expect(page.getByTestId('ctx-widget-height-auto')).toHaveAttribute('aria-checked', 'false')
    await expect(page.getByTestId('ctx-widget-scope-all')).toHaveAttribute('aria-checked', 'true')
    await expect(page.getByTestId('ctx-widget-scope-ws')).toHaveAttribute('aria-checked', 'false')

    // size preset: large = 4 columns
    await page.getByTestId('ctx-widget-size-l').click()
    await expect(menu).toHaveCount(0)
    await expect.poll(async () => (await widgetOf(api, cd.id)).w).toBe(4)
    await expect(card(page, cd.id)).toHaveAttribute('data-w', '4')

    // height: auto
    await ctxPick(page, card(page, cd.id), 'ctx-widget-height-auto', { 'ctx-widget-size-l': 'true' })
    await expect.poll(async () => (await widgetOf(api, cd.id)).h).toBe(0)
    await expect(card(page, cd.id)).not.toHaveAttribute('data-fixed', '1')

    // the world clock (manifest size m) defaults to 4 columns = "large"
    await ctxPick(page, card(page, clock.id), 'ctx-widget-size-full', { 'ctx-widget-size-l': 'true' })
    await expect.poll(async () => (await widgetOf(api, clock.id)).w).toBe(6)

    // scope: current workspace only, then all again
    await ctxPick(page, card(page, cd.id), 'ctx-widget-scope-ws', { 'ctx-widget-scope-all': 'true' })
    await expect.poll(async () => (await widgetOf(api, cd.id)).workspace_id).toBe(ws)
    await ctxPick(page, card(page, cd.id), 'ctx-widget-scope-all', { 'ctx-widget-scope-ws': 'true' })
    await expect.poll(async () => (await widgetOf(api, cd.id)).workspace_id).toBe('')

    // scoped to this workspace -> invisible in another one
    await ctxPick(page, card(page, cd.id), 'ctx-widget-scope-ws', { 'ctx-widget-scope-all': 'true' })
    await expect.poll(async () => (await widgetOf(api, cd.id)).workspace_id).toBe(ws)
    await page.evaluate((id) => localStorage.setItem('genkan.ws', id), second.id)
    await page.reload()
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Second')
    await expect(card(page, cd.id)).toHaveCount(0)
    await expect(card(page, clock.id)).toBeVisible()
    await page.evaluate((id) => localStorage.setItem('genkan.ws', id), ws)
    await page.reload()
    await settled(page, 2)

    // "Edit" opens the widget modal pre-filled
    await ctxPick(page, card(page, cd.id), 'ctx-widget-config')
    await expect(page.getByTestId('widget-modal')).toBeVisible()
    await expect(page.getByTestId('widget-cfg-label')).toHaveValue('Event')
    await page.getByTestId('widget-cancel').click()

    // delete asks first
    await ctxPick(page, card(page, cd.id), 'ctx-widget-delete')
    await expect(page.getByTestId('confirm-modal')).toBeVisible()
    await page.getByTestId('confirm-cancel').click()
    await expect(card(page, cd.id)).toBeVisible()
    await ctxPick(page, card(page, cd.id), 'ctx-widget-delete')
    await page.getByTestId('confirm-ok').click()
    await expect(card(page, cd.id)).toHaveCount(0)
    expect((await widgetsOf(api)).map((w) => w.id)).toEqual([clock.id])

    // ---- the same menu from the "..." button in edit mode ----
    await toggleEdit(page)
    await page.getByTestId(`widget-more-${clock.id}`).click()
    await expect(page.getByTestId('ctx-menu-widget')).toBeVisible()
    await expect(page.getByTestId('ctx-widget-size-full')).toHaveAttribute('aria-checked', 'true')
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('ctx-menu-widget')).toHaveCount(0)

    // ---- guest: refresh only (no link in these views) ----
    await api.settings({ guest_access: true })
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const g = await ctx.newPage()
    await g.goto('/')
    await expect(g.getByTestId('ws-menu-btn')).toBeVisible()
    await expect(card(g, clock.id)).toBeVisible()
    await card(g, clock.id).click({ button: 'right', position: { x: 30, y: 20 } })
    const gm = g.getByTestId('ctx-menu-widget')
    await expect(gm).toBeVisible()
    expect(await gm.locator('[role^="menuitem"]').evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')))).toEqual(['ctx-widget-refresh'])
    for (const id of ['ctx-widget-config', 'ctx-widget-size-s', 'ctx-widget-size-full', 'ctx-widget-height-auto', 'ctx-widget-scope-ws', 'ctx-widget-scope-all', 'ctx-widget-delete']) {
      await expect(g.getByTestId(id), id).toHaveCount(0)
    }
    await g.keyboard.press('Escape')
    await ctx.close()
  })

  test('page context menu: "Add widget" opens the widget modal and adds one', async ({ page, api, ws }) => {
    await install(api, 'countdown')
    await openApp(page)
    await page.mouse.click(1100, 650, { button: 'right' })
    await expect(page.getByTestId('ctx-menu-page')).toBeVisible()
    await page.getByTestId('ctx-add-widget').click()
    await expect(page.getByTestId('widget-modal')).toBeVisible()
    await expect(page.getByTestId('widget-picker')).toBeVisible()
    await page.getByTestId('widget-pick-countdown-timer').click()
    await expect(page.getByTestId('widget-workspace')).toHaveValue(ws)
    await page.getByTestId('widget-cfg-label').fill('From menu')
    await page.getByTestId('widget-cfg-date').fill('2099-01-01')
    await page.getByTestId('widget-save').click()
    await expect(page.getByTestId('widget-modal')).toHaveCount(0)
    await expect(page.getByTestId('widget')).toHaveCount(1)
    await expect(page.getByTestId('widget-title')).toHaveText('From menu')
  })

  test('mixed sizes: no overlaps, no holes - at 6, 4, 2 and 1 columns', async ({ page, api }) => {
    await install(api, 'world-clock')
    await install(api, 'countdown')
    const specs: [string, string, Record<string, unknown>][] = [
      ['world-clock', 'clock', {}],
      ['countdown', 'timer', {}],
      ['countdown', 'timer', { w: 3, h: 160 }],
      ['countdown', 'timer', { w: 1 }],
      ['world-clock', 'clock', { w: 2, h: 240 }],
      ['countdown', 'timer', { w: 5, h: 100 }],
      ['world-clock', 'clock', { w: 6 }],
      ['countdown', 'timer', { w: 4, h: 400 }],
      ['countdown', 'timer', { w: 2 }],
    ]
    for (const [p, w, extra] of specs) await add(api, p, w, extra)
    await openApp(page)
    await settled(page, specs.length)

    const check = async (label: string, cols: number) => {
      await settled(page, specs.length)
      const areaBox = await box(page.getByTestId('widget-area'))
      const cards = await page.getByTestId('widget').evaluateAll((els) => els.map((e) => { const r = e.getBoundingClientRect(); return { id: e.getAttribute('data-widget-id')!, x: r.x, y: r.y, width: r.width, height: r.height } }))
      // a cell occupies `--r` rows of 4px (its own box is only as tall as its content)
      const cells = await page.locator('.wgi').evaluateAll((els) => els.map((e) => { const r = e.getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: Number((e as HTMLElement).style.getPropertyValue('--r')) * 4 || r.height } }))
      expect(await page.evaluate(() => getComputedStyle(document.querySelector('[data-testid="widget-area"]')!).gridTemplateColumns.split(' ').length), label).toBe(cols)
      // 1. no two cards intersect, and every card lies inside the area
      for (let i = 0; i < cards.length; i++) {
        expect(cards[i].x, `${label}: card ${i} inside (left)`).toBeGreaterThanOrEqual(areaBox.x - 1)
        expect(cards[i].x + cards[i].width, `${label}: card ${i} inside (right)`).toBeLessThanOrEqual(areaBox.x + areaBox.width + 1)
        for (let j = i + 1; j < cards.length; j++) expect(intersects(cards[i], cards[j]), `${label}: cards ${i} and ${j} overlap`).toBe(false)
      }
      // 2. no holes: with dense packing every cell sits at the top of the area or right below another cell it overlaps horizontally
      const top = Math.min(...cells.map((c) => c.y))
      for (const [i, c] of cells.entries()) {
        if (Math.abs(c.y - top) < 1.5) continue
        const supported = cells.some((o, j) => j !== i && Math.abs(o.y + o.height - c.y) < 1.5 && o.x < c.x + c.width - 1 && c.x < o.x + o.width - 1)
        expect(supported, `${label}: cell ${i} (y=${Math.round(c.y)}) floats with a hole above it`).toBe(true)
      }
      // 3. the area is as tall as the lowest cell (no collapsed or overflowing rows)
      const bottom = Math.max(...cells.map((c) => c.y + c.height))
      expect(areaBox.y + areaBox.height, `${label}: area height`).toBeGreaterThanOrEqual(bottom - 2)
      expect(areaBox.y + areaBox.height, `${label}: area height`).toBeLessThanOrEqual(bottom + 8)
      // fixed-height cards are exactly that tall
      for (const f of await page.locator('[data-testid="widget"][data-fixed="1"]').all()) {
        expect(Math.round((await box(f)).height), `${label}: fixed height`).toBe(Number(await f.getAttribute('data-h')))
      }
    }

    await check('1280px / 6 cols', 6)
    // the 6-column geometry: w=6 is the whole row, w=3 half of it (the third card)
    const areaW = (await box(page.getByTestId('widget-area'))).width
    const full = page.locator('[data-testid="widget"][data-w="6"]')
    expect((await box(full)).width).toBeGreaterThan(areaW - 2)
    expect(Math.abs((await box(page.locator('[data-testid="widget"][data-w="3"]'))).width - (areaW - 16) / 2)).toBeLessThan(2) // half the row minus half the 16px gap

    await page.setViewportSize({ width: 1000, height: 800 })
    await check('1000px / 4 cols', 4)
    await page.setViewportSize({ width: 600, height: 800 })
    await check('600px / 2 cols', 2)
    await page.setViewportSize({ width: 400, height: 800 })
    await check('400px / 1 col', 1)
    // in a single column every card spans the full width
    const aw = (await box(page.getByTestId('widget-area'))).width
    for (const c of await page.getByTestId('widget').all()) expect((await box(c)).width).toBeGreaterThan(aw - 2)

    // edit mode too (the add card takes part in the layout)
    await page.setViewportSize({ width: 1280, height: 800 })
    await toggleEdit(page)
    await expect(page.getByTestId('widget-add')).toBeVisible()
    await check('edit mode / 6 cols', 6)
  })

  test('a fixed height clips the content and scrolls inside the card', async ({ page, api }) => {
    await install(api, 'world-clock')
    const clock = await add(api, 'world-clock', 'clock', { h: 80 })
    await openApp(page)
    await settled(page, 1)
    await expect(card(page, clock.id)).toHaveAttribute('data-fixed', '1')
    expect(Math.round((await box(card(page, clock.id))).height)).toBe(80)
    const body = card(page, clock.id).locator('.wg-body')
    expect(await body.evaluate((e) => e.scrollHeight > e.clientHeight && ['auto', 'scroll'].includes(getComputedStyle(e).overflowY))).toBe(true)
  })
})
