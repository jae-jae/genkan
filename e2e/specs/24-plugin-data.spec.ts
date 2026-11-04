import { readFileSync } from 'node:fs'
import type { Page } from '@playwright/test'
import { Api, expect, openApp, openSettings, test } from '../helpers'

// Plugin storage (docs/PLUGINS.md 13): the "Data" dialog, the optional plugin data in exports, and the line chart block.
// One tiny inline plugin writes plugin-level and instance-level storage and returns a `line` chart, so nothing here needs the network.

const PLUGIN = `export const manifest = {
  id: "e2e-data", name: "E2E Data", version: "1.0.0",
  widgets: [{ id: "w", name: "Writer", size: "m", refresh: 60 }, { id: "c", name: "Chart", size: "m", refresh: 60 }],
}
export async function widget(ctx) {
  if (ctx.widget === "c") {
    const pts = []
    for (let i = 30; i >= 0; i--) pts.push([ctx.now - i * 86400000, 1000 + (30 - i) * 37 + (i % 5) * 11])
    pts.reverse() // out of order: the server sorts them
    pts.push(["x", 2], [0, 5], [null, 1]) // invalid ones are dropped
    return { title: "History", body: [{ type: "chart", kind: "line", points: pts, format: "int", color: "green" }] }
  }
  const s = ctx.instance.storage
  s.set("count", (s.get("count") || 0) + 1)
  s.set("profile", { name: "demo", tags: ["a", "b"] })
  genkan.storage.set("cache", { token: "cached-value", n: 42 })
  return { title: "Writer", body: [{ type: "text", text: "count=" + s.get("count") }] }
}
`

interface Entry { key: string; size: number; value: unknown; instance?: string }
interface Storage { size: number; limit: number; entries: Entry[] }
const storageOf = (api: Api) => api.call<Storage>('GET', '/api/plugins/e2e-data/storage')
const row = (page: Page, id: string) => page.locator(`[data-testid="plugin-row"][data-plugin-id="${id}"]`)

async function setup(api: Api, ws: string) {
  await api.call('POST', '/api/plugins', { source: PLUGIN, enable: true })
  const w = await api.call<{ id: string }>('POST', '/api/widgets', { plugin: 'e2e-data', widget: 'w', workspace_id: ws })
  const c = await api.call<{ id: string }>('POST', '/api/widgets', { plugin: 'e2e-data', widget: 'c', workspace_id: ws })
  await api.call('GET', `/api/widgets/${w.id}/data`) // runs the hook once: writes the storage
  return { w: w.id, c: c.id }
}

test.describe('plugin data: storage dialog, export, line chart', () => {
  test.afterAll(async ({ admin }) => {
    await new Api(admin).reset()
  })

  test('storage dialog: usage, keys, values, delete a key, clear all with confirm', async ({ page, api, ws }) => {
    const { w } = await setup(api, ws)
    const before = await storageOf(api)
    expect(before.limit).toBe(262144)
    expect(before.entries.map((e) => [e.key, e.instance ?? ''])).toEqual([['cache', ''], ['count', w], ['profile', w]])

    await openApp(page)
    await openSettings(page, 'plugins')
    await row(page, 'e2e-data').getByTestId('plugin-data-btn').click()
    const modal = page.getByTestId('plugin-data-modal')
    await expect(modal).toBeVisible()
    const usage = page.getByTestId('plugin-data-usage')
    await expect(usage).toBeVisible()
    expect(Number(await usage.getAttribute('data-size'))).toBe(before.size)
    expect(Number(await usage.getAttribute('data-limit'))).toBe(262144)
    await expect(usage).toContainText('256 KB')
    const bar = usage.getByRole('progressbar')
    expect(Number(await bar.getAttribute('aria-valuenow'))).toBe(before.size)

    const rows = page.getByTestId('plugin-data-row')
    await expect(rows).toHaveCount(3)
    await expect(rows.nth(0)).toHaveAttribute('data-key', 'cache')
    await expect(rows.nth(0).getByTestId('plugin-data-instance')).toHaveCount(0) // plugin level
    await expect(rows.nth(1).getByTestId('plugin-data-instance')).toHaveAttribute('title', w)
    await expect(rows.nth(1).getByTestId('plugin-data-size')).toContainText(/\d+ B/)
    // values are collapsed until expanded, then pretty JSON
    await expect(page.getByTestId('plugin-data-value')).toHaveCount(0)
    await rows.nth(2).getByTestId('plugin-data-toggle').click()
    const val = rows.nth(2).getByTestId('plugin-data-value')
    await expect(val).toContainText('"tags": [')
    await expect(val).toContainText('"name": "demo"')
    await rows.nth(2).getByTestId('plugin-data-toggle').click()
    await expect(val).toHaveCount(0)

    // delete one key (instance scoped key goes with &instance=)
    await rows.nth(1).getByTestId('plugin-data-delete').click()
    await expect(rows).toHaveCount(2)
    expect((await storageOf(api)).entries.map((e) => e.key)).toEqual(['cache', 'profile'])

    // Clear all needs a second click; cancel keeps everything
    await page.getByTestId('plugin-data-clear').click()
    await expect(page.getByTestId('plugin-data-confirm')).toBeVisible()
    await page.getByTestId('plugin-data-clear-cancel').click()
    await expect(page.getByTestId('plugin-data-confirm')).toHaveCount(0)
    expect((await storageOf(api)).entries).toHaveLength(2)
    await page.getByTestId('plugin-data-clear').click()
    await page.getByTestId('plugin-data-clear-yes').click()
    await expect(page.getByTestId('plugin-data-empty')).toBeVisible()
    await expect(page.getByTestId('plugin-data-clear')).toBeDisabled()
    expect(await storageOf(api)).toMatchObject({ size: 0, entries: [] })

    // the dialog stacks over Settings: closing returns to it
    await page.getByTestId('plugin-data-close').click()
    await expect(modal).toHaveCount(0)
    await expect(page.getByTestId('settings-modal')).toBeVisible()
  })

  test('deleting a widget clears its instance storage (REST)', async ({ api, ws }) => {
    const { w } = await setup(api, ws)
    expect((await storageOf(api)).entries.some((e) => e.instance === w)).toBe(true)
    await api.call('DELETE', `/api/widgets/${w}`)
    expect((await storageOf(api)).entries.map((e) => e.key)).toEqual(['cache'])
  })

  test('export: plugin data is opt-in (unchecked by default, with a hint) and round-trips through import', async ({ page, api, ws }) => {
    const { w } = await setup(api, ws)
    await openApp(page)
    await openSettings(page, 'data')
    const chk = page.getByTestId('export-plugin-data-chk')
    await expect(chk).not.toBeChecked()
    await expect(page.getByTestId('export-plugin-data-hint')).toContainText(/sensitive/i)
    const exportJson = async (name: string) => {
      const [dl] = await Promise.all([page.waitForEvent('download'), page.getByTestId('export-btn').click()])
      const path = test.info().outputPath(name)
      await dl.saveAs(path)
      return { path, json: JSON.parse(readFileSync(path, 'utf8')) }
    }
    const plain = await exportJson('plain.json')
    expect(Object.keys(plain.json).sort()).toEqual(['categories', 'links', 'plugins', 'schema', 'settings', 'widgets', 'workspaces'])

    await chk.check()
    await expect(page.getByTestId('export-btn')).toHaveAttribute('href', /plugin_data=1/)
    const full = await exportJson('full.json')
    expect(Object.keys(full.json).sort()).toEqual(['categories', 'links', 'plugin_data', 'plugins', 'schema', 'settings', 'widgets', 'workspaces'])
    const kv = full.json.plugin_data['e2e-data']
    expect(Object.keys(kv).sort()).toEqual(['@i/' + w + '/count', '@i/' + w + '/profile', 'cache'])
    expect(kv.cache).toEqual({ token: 'cached-value', n: 42 })
    await chk.uncheck()
    await expect(page.getByTestId('export-btn')).toHaveAttribute('href', '/api/export')

    // wipe the storage, import the full export (replace): everything is back, bound to the same widget
    await api.call('DELETE', '/api/plugins/e2e-data/storage')
    expect((await storageOf(api)).entries).toHaveLength(0)
    await page.getByTestId('import-mode-replace').click()
    await page.getByTestId('import-file').setInputFiles(full.path)
    await page.getByTestId('import-btn').click()
    await expect.poll(async () => (await storageOf(api)).entries.length).toBe(3)
    const back = await storageOf(api)
    expect(back.entries.find((e) => e.key === 'count')).toMatchObject({ instance: w, value: 1 })
    expect(back.entries.find((e) => e.key === 'cache')!.value).toEqual({ token: 'cached-value', n: 42 })

    // importing the plain export leaves the storage alone
    await page.getByTestId('import-file').setInputFiles(plain.path)
    await page.getByTestId('import-btn').click()
    await expect.poll(async () => ((await api.bootstrap()) as any).widgets.length).toBe(2)
    expect((await storageOf(api)).entries).toHaveLength(3)
  })

  test('line chart: curve, area, y/x ticks, hover tooltip; fits at phone width', async ({ page, api, ws }) => {
    const { c } = await setup(api, ws)
    // the server sorted the points and dropped the invalid ones
    const d = await api.call<any>('GET', `/api/widgets/${c}/data`)
    const blk = d.view.body[0]
    expect(blk).toMatchObject({ type: 'chart', kind: 'line', format: 'int', color: 'green' })
    expect(blk.points).toHaveLength(31)
    expect(blk.points.map((p: number[]) => p[0])).toEqual([...blk.points.map((p: number[]) => p[0])].sort((a, b) => a - b))

    await openApp(page)
    const card = page.locator(`[data-testid="widget"][data-widget-id="${c}"]`)
    const chart = card.getByTestId('widget-chart')
    await expect(chart).toBeVisible()
    await expect(chart).toHaveAttribute('data-kind', 'line')
    const path = card.getByTestId('widget-line-path')
    expect((await path.getAttribute('d'))!.startsWith('M')).toBe(true)
    expect(await path.getAttribute('d')).toContain('C') // smooth curve
    expect(await path.getAttribute('stroke')).toBe('#3ddc97') // color: green
    await expect(card.locator('linearGradient')).toHaveCount(1) // area fill
    // y axis: 3-4 compact ticks; x axis: dates
    const ticks = await card.locator('svg text.wg-lc-t').allTextContents()
    const yTicks = await card.locator('svg text.wg-lc-t[text-anchor="end"]').allTextContents()
    expect(yTicks.length).toBeGreaterThanOrEqual(3)
    expect(yTicks.length).toBeLessThanOrEqual(4)
    expect(yTicks.every((s) => /^[\d.,]+\s?[kK万]?$/.test(s))).toBe(true)
    const xTicks = ticks.filter((s) => !yTicks.includes(s))
    expect(xTicks.length).toBeGreaterThanOrEqual(2)
    expect(xTicks.every((s) => /^[A-Z][a-z]{2} \d{1,2}$/.test(s))).toBe(true)

    // hover: crosshair + tooltip with date and value, gone when the mouse leaves
    const svg = card.getByTestId('widget-line')
    const box = (await svg.boundingBox())!
    await expect(card.getByTestId('widget-chart-tip')).toHaveCount(0)
    await page.mouse.move(box.x + box.width * 0.6, box.y + box.height / 2)
    const tip = card.getByTestId('widget-chart-tip')
    await expect(tip).toBeVisible()
    await expect(tip).toContainText(/[A-Z][a-z]{2} \d{1,2}/)
    await expect(tip).toContainText(/\d,\d{3}/)
    expect(await card.locator('svg .wg-lc-cross').count()).toBe(1)
    const tb = (await tip.boundingBox())!
    expect(tb.x).toBeGreaterThanOrEqual(box.x - 1)
    expect(tb.x + tb.width).toBeLessThanOrEqual(box.x + box.width + 1)
    await page.mouse.move(box.x + box.width * 0.2, box.y + box.height / 2)
    const first = await tip.textContent()
    await page.mouse.move(box.x + box.width * 0.9, box.y + box.height / 2)
    expect(await tip.textContent()).not.toBe(first)
    await page.mouse.move(5, 5)
    await expect(tip).toHaveCount(0)

    // the last point is the highest here: hovering the far right shows the maximum
    await page.mouse.move(box.x + box.width - 2, box.y + box.height / 2)
    await expect(tip).toContainText('2,1')

    // phone width: nothing overflows, the chart is responsive (ResizeObserver) and still readable
    const wDesktop = box.width
    await page.setViewportSize({ width: 390, height: 800 })
    await expect(async () => {
      const b = (await svg.boundingBox())!
      expect(b.width).toBeLessThan(wDesktop)
      expect(b.width).toBeGreaterThan(200)
    }).toPass()
    const m = (await svg.boundingBox())!
    await expect
      .poll(() => page.evaluate(() => [...document.querySelectorAll('body *')].filter((e) => e.getBoundingClientRect().right > window.innerWidth + 1).map((e) => e.tagName + '.' + (e as HTMLElement).className)))
      .toEqual([])
    const cb = (await card.boundingBox())!
    expect(m.x).toBeGreaterThanOrEqual(cb.x)
    expect(m.x + m.width).toBeLessThanOrEqual(cb.x + cb.width + 1)
    expect((await card.locator('svg text.wg-lc-t[text-anchor="end"]').count())).toBeGreaterThanOrEqual(3)
    await page.mouse.move(m.x + m.width / 2, m.y + m.height / 2)
    await expect(card.getByTestId('widget-chart-tip')).toBeVisible()
    await page.screenshot({ path: test.info().outputPath('line-chart-390.png') })
  })
})
