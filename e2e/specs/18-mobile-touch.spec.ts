import type { Page } from '@playwright/test'
import { expect, linkCap, openApp, origin, test, toggleEdit, touchGesture as touch } from '../helpers'

const L = (id: string, title: string, extra: Record<string, unknown> = {}) => ({ id, workspace_id: 'ws1', title, url: `${origin()}/healthz?l=${id}`, ...extra })

async function center(l: ReturnType<typeof linkCap>) {
  const b = (await l.boundingBox())!
  return { x: b.x + b.width / 2, y: b.y + b.height / 2 }
}

test.describe('mobile: touch interactions (390x844)', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 })

  test.beforeEach(async ({ api }) => {
    await api.seed({
      categories: [{ id: 'c1', workspace_id: 'ws1', name: 'Apps' }],
      links: [
        L('a', 'Alpha', { category_id: 'c1', description: 'Photo backup and gallery for the whole family' }),
        L('b', 'Beta', { category_id: 'c1', description: 'Second' }),
        L('c', 'Gamma', { category_id: 'c1' }),
      ],
    })
  })

  test('long-press (~500 ms) on a capsule opens the bottom sheet and does NOT navigate; Esc/backdrop closes', async ({ page, api, context }) => {
    await openApp(page)
    const { x, y } = await center(linkCap(page, 'a'))
    let popups = 0
    context.on('page', () => popups++)
    await touch(page, x, y, { hold: 700 })
    const menu = page.getByTestId('ctx-menu-link')
    await expect(menu).toBeVisible()
    await expect(menu).toHaveClass(/ctx-sheet/)
    // a bottom sheet: full width, docked to the bottom edge
    await expect
      .poll(async () => {
        const b = (await menu.boundingBox())!
        return Math.round(b.x) === 0 && Math.round(b.width) === 390 && Math.abs(b.y + b.height - 844) < 2
      })
      .toBe(true)
    await expect(page.locator('[data-testid="ctx-menu-link"] [role^="menuitem"]')).toHaveCount(6)
    await expect(menu).toContainText('Alpha') // title of the capsule
    // releasing the finger neither navigated nor counted a click, nor closed the sheet
    await page.waitForTimeout(900) // (negative check: give a wrongly fired click / popup time to appear)
    expect(popups).toBe(0)
    expect(page.url()).toBe(`${origin()}/`)
    await expect(menu).toBeVisible()
    expect((await api.links()).find((l) => l.id === 'a')!.clicks).toBe(0)
    // tapping the dimmed backdrop closes
    await page.touchscreen.tap(190, 80)
    await expect(menu).toHaveCount(0)

    // second time: pick an item with a tap
    await touch(page, x, y, { hold: 700 })
    await expect(menu).toBeVisible()
    await page.getByTestId('ctx-pin').tap()
    await expect(menu).toHaveCount(0)
    await expect(page.getByTestId('pin-link-a')).toBeVisible()
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'a')!.pinned).toBe(true)
  })

  test('a normal tap still opens the link; moving the finger (>8 px) before the timer does not open the sheet', async ({ page, api, context }) => {
    await openApp(page)
    const { x, y } = await center(linkCap(page, 'c'))
    // moved far before the timer: cancelled
    await touch(page, x, y, { hold: 700, moveBy: 30 })
    await expect(page.getByTestId('ctx-menu-link')).toHaveCount(0)
    // tap navigates (new tab by default) and counts
    const popup = context.waitForEvent('page')
    await linkCap(page, 'b').tap()
    const tab = await popup
    await expect.poll(() => tab.url()).toContain('/healthz?l=b')
    await tab.close()
    await expect.poll(async () => (await api.links()).find((l) => l.id === 'b')!.clicks).toBeGreaterThanOrEqual(1)
  })

  test('in edit mode long-press is for dragging: no menu', async ({ page }) => {
    await openApp(page)
    await toggleEdit(page)
    const { x, y } = await center(linkCap(page, 'c'))
    await touch(page, x, y, { hold: 800 })
    await expect(page.getByTestId('ctx-menu-link')).toHaveCount(0)
    await expect(page.getByTestId('link-modal')).toHaveCount(0)
  })

  test('descriptions are shown (truncated) in the two-column grid; topbar incl. the AI button fits', async ({ page }) => {
    await openApp(page)
    await expect(linkCap(page, 'a').locator('.cap-desc')).toHaveText('Photo backup and gallery for the whole family')
    const a = (await linkCap(page, 'a').boundingBox())!
    const b = (await linkCap(page, 'b').boundingBox())!
    expect(Math.abs(a.y - b.y)).toBeLessThan(2) // same row: two columns
    expect(a.x + a.width).toBeLessThanOrEqual(390.5)
    expect(b.x + b.width).toBeLessThanOrEqual(390.5)
    // truncated with an ellipsis, not wrapped / overflowing
    const d = linkCap(page, 'a').locator('.cap-desc')
    expect(await d.evaluate((e) => e.scrollWidth > e.clientWidth)).toBe(true)
    expect(a.height).toBeLessThan(90)
    for (const id of ['ws-menu-btn', 'ai-btn', 'edit-toggle', 'menu-btn']) {
      const bb = (await page.getByTestId(id).boundingBox())!
      expect(bb.x, id).toBeGreaterThanOrEqual(0)
      expect(bb.x + bb.width, id).toBeLessThanOrEqual(390)
    }
    // AI button opens the AI tab on mobile too
    await page.getByTestId('ai-btn').tap()
    await expect(page.getByTestId('settings-panel-ai')).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
  })

  test('Settings tabs, AI tab and gallery fit the phone screen', async ({ page }) => {
    await openApp(page)
    await page.getByTestId('ai-btn').tap()
    for (const tab of ['ai', 'appearance', 'content']) {
      await page.getByTestId(`settings-tab-${tab}`).tap()
      await expect(page.getByTestId(`settings-panel-${tab}`)).toBeVisible()
      const m = (await page.getByTestId('settings-modal').boundingBox())!
      expect(m.x, tab).toBeGreaterThanOrEqual(0)
      expect(m.x + m.width, tab).toBeLessThanOrEqual(390.5)
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth), tab).toBeLessThanOrEqual(0)
    }
  })
})
