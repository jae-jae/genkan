import type { Locator, Page } from '@playwright/test'
import { Api, expect, openApp, origin, test, toggleEdit } from '../helpers'

// Interactive widgets and the widget picker (docs/PLUGINS.md 15): the built-in `todo` and `note` plugins (input, checkboxes, inline edit,
// drag / keyboard sort, item buttons, Markdown block with in-place editing, tone), the searchable grid picker with install-on-pick,
// the full-row "add widget" tile, and the read-only view for visitors. Offline plugins only.
// Every test starts from `api.reset()` (the `api` fixture), which drops all plugins and widgets, so each test installs what it needs.

interface W { id: string; plugin: string; widget: string }
const install = (api: Api, id: string) => api.call('POST', '/api/plugins', { catalog: id, enable: true })
const addWidget = (api: Api, plugin: string, widget: string, workspace_id: string, config: Record<string, unknown> = {}) =>
  api.call<W>('POST', '/api/widgets', { plugin, widget, workspace_id, config })
const act = (api: Api, id: string, action: string, params: Record<string, unknown> = {}) => api.call<{ view: any }>('POST', `/api/widgets/${id}/action`, { action, params })
const viewOf = async (api: Api, id: string) => (await api.call<{ view: any }>('GET', `/api/widgets/${id}/data`)).view
const listOf = async (api: Api, id: string): Promise<{ id: string; title: string; check?: { checked: boolean } }[]> =>
  (await viewOf(api, id)).body.find((b: any) => b.type === 'list').items
const noteText = async (api: Api, id: string): Promise<string> => (await viewOf(api, id)).body[0].text

const card = (page: Page, plugin: string) => page.locator(`[data-testid="widget"][data-plugin="${plugin}"]`)
const rows = (c: Locator) => c.getByTestId('widget-list-item')
const titles = (c: Locator) => rows(c).getByTestId('widget-item-title').allTextContents()
const box = async (l: Locator) => (await l.boundingBox())!

async function addTask(page: Page, c: Locator, text: string) {
  const input = c.getByTestId('widget-input')
  await input.fill(text)
  await input.press('Enter')
  await expect(c.getByTestId('widget-item-title').filter({ hasText: text })).toHaveCount(1)
}

/** Drag a list row with the mouse to just above / below the middle of another row. */
async function dragRow(page: Page, from: Locator, to: Locator, below: boolean) {
  const a = await box(from)
  const b = await box(to)
  const sx = a.x + a.width / 2
  const sy = a.y + a.height / 2
  await page.mouse.move(sx, sy)
  await page.mouse.down()
  await page.mouse.move(sx + 2, sy + 8, { steps: 2 })
  await page.mouse.move(sx, b.y + b.height * (below ? 0.85 : 0.15), { steps: 10 })
  await page.mouse.up()
}

test.describe('widget picker', () => {
  test('grid, groups, badges, search and the empty state; the list scrolls, the modal does not', async ({ page, api }) => {
    await install(api, 'world-clock')
    await install(api, 'countdown')
    await api.call('PATCH', '/api/plugins/countdown', { enabled: false }) // installed but disabled
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId('widget-add').click()
    const picker = page.getByTestId('widget-picker')
    await expect(picker).toBeVisible()
    await expect(page.getByTestId('widget-search')).toBeFocused()

    // groups: enabled plugins, then disabled ones, then the built-in plugins that are not installed yet
    await expect(page.getByTestId('widget-pick-world-clock-clock')).toBeVisible()
    const groups = () => picker.locator('[data-testid^="widget-group-"]').evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')!.slice('widget-group-'.length) + ':' + e.getAttribute('data-state')))
    await expect.poll(groups).toEqual(['installed:enabled', 'disabled:disabled', 'new:available'])
    // badges: none on enabled cards, "Disabled" and "Not installed" on the others
    await expect(page.getByTestId('widget-badge-world-clock-clock')).toHaveCount(0)
    await expect(page.getByTestId('widget-badge-countdown-timer')).toHaveText('Disabled')
    await expect(page.getByTestId('widget-badge-todo-list')).toHaveText('Not installed')
    await expect(page.getByTestId('widget-pick-todo-list')).toHaveAttribute('data-state', 'available')
    // a card carries the name, the plugin name and the default size
    await expect(page.getByTestId('widget-pick-world-clock-clock')).toContainText('World Clock')
    await expect(page.getByTestId('widget-pick-world-clock-clock')).toContainText('Medium')

    // 3 columns on a wide screen
    const firstRow = async () => {
      const cards = page.locator('[data-testid="widget-group-new"] .wg-pick')
      const n = await cards.count()
      const ys: number[] = []
      for (let i = 0; i < Math.min(n, 6); i++) ys.push(Math.round((await box(cards.nth(i))).y))
      return ys.filter((y) => y === ys[0]).length
    }
    await expect.poll(firstRow).toBe(3)

    // the modal has a fixed height: only the list scrolls, the search box stays where it is
    const modal = page.getByTestId('widget-modal')
    await page.waitForTimeout(400) // the pop-in animation scales the modal
    const mh = (await box(modal)).height
    const list = page.getByTestId('widget-pick-list')
    expect(await list.evaluate((e) => e.scrollHeight > e.clientHeight)).toBe(true)
    expect(await page.locator('.modal-wgp .modal-body').evaluate((e) => e.scrollHeight <= e.clientHeight + 1)).toBe(true)
    const sy = (await box(page.getByTestId('widget-search'))).y
    await list.evaluate((e) => (e.scrollTop = e.scrollHeight))
    expect(Math.abs((await box(page.getByTestId('widget-search'))).y - sy)).toBeLessThan(1)
    await list.evaluate((e) => (e.scrollTop = 0))

    // search: widget name, case-insensitive, plugin id, both languages, description
    const search = page.getByTestId('widget-search')
    const cards = () => picker.locator('.wg-pick').evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')!))
    await search.fill('WORLD')
    await expect.poll(cards).toEqual(['widget-pick-world-clock-clock'])
    await expect.poll(groups).toEqual(['installed:enabled'])
    await search.fill('world-clock') // the id
    await expect.poll(cards).toEqual(['widget-pick-world-clock-clock'])
    await search.fill('倒计时') // the Chinese name finds the English UI's card
    await expect.poll(cards).toEqual(['widget-pick-countdown-timer'])
    await search.fill('countdown')
    await expect.poll(cards).toContain('widget-pick-countdown-timer')
    const desc: string = ((await api.call<any[]>('GET', '/api/plugins')).find((p) => p.id === 'world-clock').description as any)?.en ?? ''
    const word = desc.split(/\s+/).find((w) => /^[a-z]{6,}$/i.test(w))
    if (word) {
      await search.fill(word.toUpperCase()) // the description
      await expect.poll(cards).toContain('widget-pick-world-clock-clock')
    }
    await search.fill('todo')
    await expect.poll(cards).toEqual(['widget-pick-todo-list'])
    expect(Math.abs((await box(modal)).height - mh)).toBeLessThan(2) // fixed height, also with one result

    // nothing matches
    await expect(page.getByTestId('widget-search-empty')).toHaveCount(0)
    await search.fill('zzz-no-such-widget')
    await expect(page.getByTestId('widget-search-empty')).toBeVisible()
    await expect(page.getByTestId('widget-search-empty')).toContainText('zzz-no-such-widget')
    await expect(picker.locator('.wg-pick')).toHaveCount(0)
    expect(Math.abs((await box(modal)).height - mh)).toBeLessThan(2)
    await search.fill('')
    await expect(page.getByTestId('widget-search-empty')).toHaveCount(0)
    await expect(page.getByTestId('widget-pick-world-clock-clock')).toBeVisible()

    // two columns on a narrow screen
    await page.setViewportSize({ width: 500, height: 800 })
    await expect.poll(firstRow).toBe(2)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  })

  test('picking an installed widget opens the form; a disabled plugin is enabled first', async ({ page, api, ws }) => {
    await install(api, 'world-clock')
    await install(api, 'countdown')
    await api.call('PATCH', '/api/plugins/countdown', { enabled: false })
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId('widget-add').click()
    await page.getByTestId('widget-pick-countdown-timer').click()
    await expect(page.getByTestId('widget-form')).toBeVisible()
    await expect(page.getByTestId('widget-workspace')).toHaveValue(ws)
    expect(((await api.call<any[]>('GET', '/api/plugins')).find((p) => p.id === 'countdown')).enabled).toBe(true)
    await page.getByTestId('widget-cfg-label').fill('Party')
    await page.getByTestId('widget-cfg-date').fill('2099-01-01')
    await page.getByTestId('widget-save').click()
    await expect(page.getByTestId('widget-modal')).toHaveCount(0)
    await expect(card(page, 'countdown').getByTestId('widget-title')).toHaveText('Party')
  })

  test('picking a widget of a plugin that is not installed installs and enables it, then opens the form', async ({ page, api, ws }) => {
    expect(await api.call<any[]>('GET', '/api/plugins')).toEqual([])
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId('widget-add').click()
    await expect(page.getByTestId('widget-picker')).toBeVisible()
    const pick = page.getByTestId('widget-pick-todo-list')
    await expect(pick).toHaveAttribute('data-state', 'available')
    await pick.click()
    await expect(page.getByTestId('widget-form')).toBeVisible()
    await expect(page.getByTestId('widget-form')).toContainText('To-do')
    await expect(page.getByTestId('widget-workspace')).toHaveValue(ws)
    const todo = (await api.call<any[]>('GET', '/api/plugins')).find((p) => p.id === 'todo')
    expect(todo?.enabled).toBe(true)
    await page.getByTestId('widget-save').click()
    await expect(page.getByTestId('widget-modal')).toHaveCount(0)
    const c = card(page, 'todo')
    await expect(c).toHaveCount(1)
    await expect(c.getByTestId('widget-input')).toBeVisible()
    // back from the form: the plugin now sits in the installed group
    await page.getByTestId('widget-add').click()
    await expect(page.getByTestId('widget-group-installed').getByTestId('widget-pick-todo-list')).toBeVisible()
    await expect(page.getByTestId('widget-pick-todo-list')).toHaveAttribute('data-state', 'enabled')
  })

  test('a failing install shows a toast and keeps the picker open; a slow one shows a loading state', async ({ page, api }) => {
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId('widget-add').click()
    await expect(page.getByTestId('widget-pick-todo-list')).toBeVisible()
    let fail = true
    await page.route('**/api/plugins', async (route) => {
      if (route.request().method() !== 'POST') return route.continue()
      await new Promise((r) => setTimeout(r, 500))
      if (fail) return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'install exploded' }) })
      return route.continue()
    })
    await page.getByTestId('widget-pick-todo-list').click()
    await expect(page.getByTestId('widget-pick-todo-list')).toHaveAttribute('aria-busy', 'true')
    await expect(page.getByTestId('widget-badge-todo-list')).toHaveText('Installing…')
    await expect(page.getByTestId('toast').filter({ hasText: 'install exploded' })).toBeVisible()
    await expect(page.getByTestId('widget-picker')).toBeVisible()
    await expect(page.getByTestId('widget-pick-todo-list')).toHaveAttribute('aria-busy', 'false')
    expect((await api.call<any[]>('GET', '/api/plugins')).some((p) => p.id === 'todo')).toBe(false)
    fail = false
    await page.getByTestId('widget-pick-todo-list').click()
    await expect(page.getByTestId('widget-form')).toBeVisible()
  })

  test('no widgets at all (catalog unreachable, nothing installed): the empty state offers the plugin manager', async ({ page }) => {
    await page.route('**/api/plugins/catalog', (route) => route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'down' }) }))
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId('widget-add').click()
    await expect(page.getByTestId('widget-none')).toBeVisible()
    await expect(page.getByTestId('widget-search-empty')).toHaveCount(0)
    await page.getByTestId('widget-open-plugins').click()
    await expect(page.getByTestId('widget-modal')).toHaveCount(0)
    await expect(page.getByTestId('settings-modal')).toBeVisible()
  })

  test('with widgets available the empty state never shows; Enter picks the first match', async ({ page, api }) => {
    await install(api, 'world-clock')
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId('widget-add').click()
    await expect(page.getByTestId('widget-pick-world-clock-clock')).toBeVisible()
    await expect(page.getByTestId('widget-none')).toHaveCount(0)
    await page.getByTestId('widget-search').fill('clock')
    await page.getByTestId('widget-search').press('Enter')
    await expect(page.getByTestId('widget-form')).toBeVisible()
  })

  test.describe('Chinese UI', () => {
    test.use({ locale: 'zh-CN' })
    test('group and badge labels are localized; names match in both languages', async ({ page, api }) => {
      await install(api, 'world-clock')
      await openApp(page)
      await toggleEdit(page)
      await page.getByTestId('widget-add').click()
      await expect(page.getByTestId('widget-search')).toHaveAttribute('placeholder', '搜索小组件')
      await expect(page.getByTestId('widget-badge-todo-list')).toHaveText('未安装')
      await expect(page.getByTestId('widget-group-new')).toContainText('可安装')
      await expect(page.getByTestId('widget-pick-todo-list')).toContainText('待办')
      await page.getByTestId('widget-search').fill('world clock') // the English name still matches
      await expect(page.getByTestId('widget-pick-world-clock-clock')).toBeVisible()
      await page.getByTestId('widget-search').fill('nope-nope')
      await expect(page.getByTestId('widget-search-empty')).toContainText('nope-nope')
    })
  })
})

test.describe('add widget tile', () => {
  test('is the first card and spans the whole row at every grid density', async ({ page, api, ws }) => {
    await install(api, 'countdown')
    for (const label of ['One', 'Two']) await addWidget(api, 'countdown', 'timer', ws, { label, date: '2099-01-01' })
    await openApp(page)
    await toggleEdit(page)
    await expect(page.getByTestId('widget')).toHaveCount(2)
    for (const [width, cols] of [[1280, 6], [900, 4], [600, 2], [400, 1]] as const) {
      await page.setViewportSize({ width, height: 800 })
      const area = page.getByTestId('widget-area')
      await expect.poll(async () => (await area.evaluate((el) => getComputedStyle(el).gridTemplateColumns.split(' ').length)), `columns at ${width}`).toBe(cols)
      const a = await box(area)
      const t = await box(page.getByTestId('widget-add'))
      expect(Math.abs(t.width - a.width), `add tile width at ${width}`).toBeLessThanOrEqual(2)
      expect(Math.abs(t.x - a.x)).toBeLessThanOrEqual(1)
      expect(t.height).toBeGreaterThan(44)
      expect(t.height).toBeLessThan(60)
      // nothing sits beside it, and the cards come after it
      for (const w of await page.getByTestId('widget').all()) expect((await box(w)).y).toBeGreaterThan(t.y + t.height - 1)
    }
    expect(await page.getByTestId('widget-area').evaluate((el) => el.firstElementChild!.classList.contains('wgi-add'))).toBe(true)
  })
})

test.describe('todo widget', () => {
  test('add, toggle, rename, delete, reorder (mouse and keyboard) and clear finished', async ({ page, api, ws }) => {
    await install(api, 'todo')
    const w = await addWidget(api, 'todo', 'list', ws)
    await openApp(page)
    const c = card(page, 'todo')
    await expect(c.getByTestId('widget-input')).toBeVisible()
    await expect(c.getByTestId('widget-title')).toHaveText('To-do')
    await expect(c.getByTestId('widget-list-empty')).toBeVisible() // empty text, no items

    // add: Enter submits, the field empties and keeps the focus; blank input is ignored
    const input = c.getByTestId('widget-input')
    await input.fill('   ')
    await expect(c.getByTestId('widget-input-submit')).toBeDisabled()
    await input.press('Enter')
    await expect(rows(c)).toHaveCount(0)
    await addTask(page, c, 'Alpha')
    await expect(input).toHaveValue('')
    await expect(input).toBeFocused()
    await addTask(page, c, 'Bravo')
    await input.fill('Charlie')
    await c.getByTestId('widget-input-submit').click() // the button works as well
    await expect(rows(c)).toHaveCount(3)
    await expect(c.getByTestId('widget-title')).toHaveText('To-do · 3')
    await expect(c.getByTestId('widget-list-empty')).toHaveCount(0)
    expect(await titles(c)).toEqual(['Alpha', 'Bravo', 'Charlie'])
    expect((await listOf(api, w.id)).map((i) => i.title)).toEqual(['Alpha', 'Bravo', 'Charlie'])

    // toggle: finished items sink to the bottom, are struck through, and the count follows
    const bravo = rows(c).filter({ hasText: 'Bravo' })
    await bravo.getByTestId('widget-check').check()
    await expect(bravo).toHaveAttribute('data-checked', '1')
    await expect.poll(() => titles(c)).toEqual(['Alpha', 'Charlie', 'Bravo'])
    await expect(c.getByTestId('widget-title')).toHaveText('To-do · 2')
    expect(await bravo.getByTestId('widget-item-title').evaluate((e) => getComputedStyle(e).textDecorationLine)).toBe('line-through')
    await expect(c.getByTestId('widget-button')).toHaveText('Clear finished (1)')
    await bravo.getByTestId('widget-check').uncheck()
    await expect.poll(() => titles(c)).toEqual(['Alpha', 'Bravo', 'Charlie'])
    await expect(c.getByTestId('widget-button')).toHaveCount(0)

    // rename: double-click, Enter saves, Esc cancels, an empty value is not submitted
    const alpha = () => rows(c).filter({ hasText: /^Alpha|Alpha2/ })
    await rows(c).first().getByTestId('widget-item-title').dblclick()
    const edit = c.getByTestId('widget-item-edit')
    await expect(edit).toBeFocused()
    await expect(edit).toHaveValue('Alpha')
    await edit.fill('Alpha2')
    await edit.press('Enter')
    await expect(edit).toHaveCount(0)
    await expect.poll(() => titles(c)).toEqual(['Alpha2', 'Bravo', 'Charlie'])
    await alpha().getByTestId('widget-item-title').dblclick()
    await c.getByTestId('widget-item-edit').fill('discarded')
    await c.getByTestId('widget-item-edit').press('Escape')
    await expect(c.getByTestId('widget-item-edit')).toHaveCount(0)
    expect(await titles(c)).toEqual(['Alpha2', 'Bravo', 'Charlie'])
    await alpha().getByTestId('widget-item-title').dblclick()
    await c.getByTestId('widget-item-edit').fill('')
    await c.getByTestId('widget-item-edit').press('Enter')
    expect(await titles(c)).toEqual(['Alpha2', 'Bravo', 'Charlie'])
    expect((await listOf(api, w.id))[0].title).toBe('Alpha2')

    // delete with the item's button
    const del = rows(c).filter({ hasText: 'Charlie' }).getByTestId('widget-item-action')
    await expect(del).toHaveAttribute('aria-label', 'Delete')
    await expect(del).toHaveAttribute('title', 'Delete')
    await del.click()
    await expect.poll(() => titles(c)).toEqual(['Alpha2', 'Bravo'])
    await addTask(page, c, 'Delta')
    await addTask(page, c, 'Echo')
    expect(await titles(c)).toEqual(['Alpha2', 'Bravo', 'Delta', 'Echo'])

    // reorder with the mouse: Alpha2 goes below Echo; the order is saved on the server and survives a reload
    await dragRow(page, rows(c).nth(0), rows(c).nth(3), true)
    await expect.poll(() => titles(c)).toEqual(['Bravo', 'Delta', 'Echo', 'Alpha2'])
    await expect.poll(async () => (await listOf(api, w.id)).map((i) => i.title)).toEqual(['Bravo', 'Delta', 'Echo', 'Alpha2'])
    await page.reload()
    await expect(rows(c)).toHaveCount(4)
    expect(await titles(c)).toEqual(['Bravo', 'Delta', 'Echo', 'Alpha2'])

    // a plain click on the row does not start a drag, and Esc cancels a drag in progress
    const a = await box(rows(c).nth(0))
    await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2)
    await page.mouse.down()
    await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2 + 60, { steps: 6 })
    await page.keyboard.press('Escape')
    await page.mouse.up()
    expect(await titles(c)).toEqual(['Bravo', 'Delta', 'Echo', 'Alpha2'])
    expect((await listOf(api, w.id)).map((i) => i.title)).toEqual(['Bravo', 'Delta', 'Echo', 'Alpha2'])

    // keyboard: Alt+ArrowDown / Alt+ArrowUp move the focused row, the focus stays on it
    const delta = rows(c).filter({ hasText: 'Delta' })
    await delta.focus()
    await page.keyboard.press('Alt+ArrowDown')
    await expect.poll(() => titles(c)).toEqual(['Bravo', 'Echo', 'Delta', 'Alpha2'])
    await expect(rows(c).filter({ hasText: 'Delta' })).toBeFocused()
    await page.keyboard.press('Alt+ArrowUp')
    await expect.poll(() => titles(c)).toEqual(['Bravo', 'Delta', 'Echo', 'Alpha2'])
    await expect(rows(c).filter({ hasText: 'Delta' })).toBeFocused()
    await page.keyboard.press('Alt+ArrowUp')
    await expect.poll(() => titles(c)).toEqual(['Delta', 'Bravo', 'Echo', 'Alpha2'])
    await expect.poll(async () => (await listOf(api, w.id)).map((i) => i.title)).toEqual(['Delta', 'Bravo', 'Echo', 'Alpha2'])
    await page.keyboard.press('Alt+ArrowUp') // already first: nothing happens
    expect(await titles(c)).toEqual(['Delta', 'Bravo', 'Echo', 'Alpha2'])

    // clear finished
    await rows(c).filter({ hasText: 'Bravo' }).getByTestId('widget-check').check()
    await rows(c).filter({ hasText: 'Alpha2' }).getByTestId('widget-check').check()
    await expect(c.getByTestId('widget-button')).toHaveText('Clear finished (2)')
    await c.getByTestId('widget-button').click()
    await expect.poll(() => titles(c)).toEqual(['Delta', 'Echo'])
    await expect(c.getByTestId('widget-button')).toHaveCount(0)
    await expect(c.getByTestId('widget-title')).toHaveText('To-do · 2')
    for (const t of ['Delta', 'Echo']) await rows(c).filter({ hasText: t }).getByTestId('widget-item-action').click()
    await expect(rows(c)).toHaveCount(0)
    await expect(c.getByTestId('widget-list-empty')).toBeVisible()
    await expect(c.getByTestId('widget-title')).toHaveText('To-do')
  })

  test('check toggles optimistically and rolls back with a toast when the action fails; add keeps the text on error', async ({ page, api, ws }) => {
    await install(api, 'todo')
    const w = await addWidget(api, 'todo', 'list', ws)
    await act(api, w.id, 'add', { value: 'First' })
    await openApp(page)
    const c = card(page, 'todo')
    await expect(rows(c)).toHaveCount(1)
    await page.route('**/api/widgets/*/action', async (route) => {
      await new Promise((r) => setTimeout(r, 700))
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'storage is on fire' }) })
    })
    const check = c.getByTestId('widget-check')
    await check.click()
    await expect(check).toBeChecked({ timeout: 400 }) // optimistic: no wait for the server
    await expect(page.getByTestId('toast').filter({ hasText: 'storage is on fire' })).toBeVisible()
    await expect(check).not.toBeChecked() // rolled back
    expect((await listOf(api, w.id))[0].check?.checked).toBe(false)
    // the input empties at once, and gets its text back when the action fails
    const input = c.getByTestId('widget-input')
    await input.fill('Second')
    await input.press('Enter')
    await expect(input).toHaveValue('')
    await expect(input).toBeDisabled()
    await expect(input).toHaveValue('Second')
    await expect(input).toBeEnabled()
    await expect(input).toBeFocused()
    await expect(rows(c)).toHaveCount(1)
    await page.unroute('**/api/widgets/*/action')
    await input.press('Enter')
    await expect(rows(c)).toHaveCount(2)
    await expect(input).toHaveValue('')
  })

  test('the card is replaced by the view of the action response: no extra data request', async ({ page, api, ws }) => {
    await install(api, 'todo')
    await addWidget(api, 'todo', 'list', ws)
    await openApp(page)
    const c = card(page, 'todo')
    await expect(c.getByTestId('widget-input')).toBeVisible()
    let dataCalls = 0
    page.on('request', (r) => /\/api\/widgets\/\w+\/data/.test(r.url()) && dataCalls++)
    await addTask(page, c, 'One')
    await addTask(page, c, 'Two')
    await rows(c).first().getByTestId('widget-check').check()
    await expect.poll(() => titles(c)).toEqual(['Two', 'One'])
    expect(dataCalls).toBe(0)
  })

  test('long-press then drag reorders on a touch screen', async ({ browser, api, ws }) => {
    await install(api, 'todo')
    const w = await addWidget(api, 'todo', 'list', ws)
    for (const v of ['One', 'Two', 'Three']) await act(api, w.id, 'add', { value: v })
    const ctx = await browser.newContext({ baseURL: origin(), viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, locale: 'en-US', storageState: await (await import('../helpers')).adminContext().then(async (a) => { const s = await a.storageState(); await a.dispose(); return s }) })
    const p = await ctx.newPage()
    await p.goto('/')
    const c = card(p, 'todo')
    await expect(rows(c)).toHaveCount(3)
    const a = await box(rows(c).nth(0))
    const z = await box(rows(c).nth(2))
    const x = a.x + a.width / 2
    const cdp = await ctx.newCDPSession(p)
    const pt = (y: number) => [{ x, y, id: 1 }]
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: pt(a.y + a.height / 2) })
    await new Promise((r) => setTimeout(r, 450)) // the long press
    for (let i = 1; i <= 8; i++) await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: pt(a.y + a.height / 2 + ((z.y + z.height * 0.9 - a.y - a.height / 2) * i) / 8) })
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
    await expect.poll(() => titles(c)).toEqual(['Two', 'Three', 'One'])
    await expect.poll(async () => (await listOf(api, w.id)).map((i) => i.title)).toEqual(['Two', 'Three', 'One'])
    // the item button is always visible on a touch screen
    await expect(rows(c).first().getByTestId('widget-item-action')).toBeVisible()
    expect(await rows(c).first().locator('.wg-li-acts').evaluate((e) => getComputedStyle(e).opacity)).toBe('1')
    await ctx.close()
  })

  test('in layout edit mode the controls inside cards are inert', async ({ page, api, ws }) => {
    await install(api, 'todo')
    const w = await addWidget(api, 'todo', 'list', ws)
    await act(api, w.id, 'add', { value: 'Inert' })
    await openApp(page)
    await toggleEdit(page)
    const c = card(page, 'todo')
    await expect(rows(c)).toHaveCount(1)
    expect(await c.getByTestId('widget-check').evaluate((e) => getComputedStyle(e).pointerEvents)).toBe('none')
    expect(await c.getByTestId('widget-input').evaluate((e) => getComputedStyle(e).pointerEvents)).toBe('none')
    expect(await c.getByTestId('widget-item-action').evaluate((e) => getComputedStyle(e).pointerEvents)).toBe('none')
    await expect(rows(c).first()).not.toHaveAttribute('data-sort', '1')
    // ... except that a click on the title renames
    await c.getByTestId('widget-item-title').click()
    await expect(c.getByTestId('widget-item-edit')).toBeFocused()
    await c.getByTestId('widget-item-edit').fill('Renamed here')
    await c.getByTestId('widget-item-edit').press('Enter')
    await expect.poll(async () => (await listOf(api, w.id))[0].title).toBe('Renamed here')
  })

  test('visitors see a read-only list', async ({ browser, api, ws }) => {
    await install(api, 'todo')
    const w = await addWidget(api, 'todo', 'list', ws)
    for (const v of ['Visible one', 'Visible two']) await act(api, w.id, 'add', { value: v })
    await act(api, w.id, 'toggle', { id: (await listOf(api, w.id))[1].id, checked: true })
    await api.settings({ guest_access: true })
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const g = await ctx.newPage()
    await g.goto('/')
    await expect(g.getByTestId('ws-menu-btn')).toBeVisible()
    const c = card(g, 'todo')
    await expect(rows(c)).toHaveCount(2)
    expect(await titles(c)).toEqual(['Visible one', 'Visible two'])
    // checkboxes are shown (with their state) but cannot be clicked; nothing else is offered
    await expect(c.getByTestId('widget-check')).toHaveCount(2)
    for (const ck of await c.getByTestId('widget-check').all()) await expect(ck).toBeDisabled()
    await expect(c.getByTestId('widget-check').nth(1)).toBeChecked()
    for (const id of ['widget-input', 'widget-input-form', 'widget-item-action', 'widget-button', 'widget-item-edit', 'widget-refresh']) await expect(c.getByTestId(id), id).toHaveCount(0)
    await expect(rows(c).first()).not.toHaveAttribute('data-sort', '1')
    await expect(rows(c).first()).not.toHaveAttribute('tabindex', /.*/)
    await rows(c).first().getByTestId('widget-item-title').dblclick()
    await expect(c.getByTestId('widget-item-edit')).toHaveCount(0)
    await rows(c).first().getByTestId('widget-check').click({ force: true })
    await g.waitForTimeout(300)
    expect((await listOf(api, w.id)).map((i) => i.check?.checked)).toEqual([false, true])
    // the server refuses an action from a visitor as well
    const r = await g.request.post(`/api/widgets/${w.id}/action`, { headers: { 'X-Requested-With': 'genkan' }, data: { action: 'add', params: { value: 'sneaky' } } })
    expect([401, 403]).toContain(r.status())
    await ctx.close()
  })
})

const MD = [
  '# Title H1',
  '## Sub H2',
  'Some **bold**, *italic*, ~~gone~~ and `code` with a [link](https://example.com/a) and <https://example.org/auto> or https://bare.example/x.',
  'Second line',
  '',
  '> quote line',
  '',
  '---',
  '',
  '1. one',
  '2. two',
  '   - nested',
  '',
  '- [ ] open task',
  '- [x] done task',
  '',
  '```',
  'raw <b>not bold</b>',
  '```',
  '',
  '| A | B |',
  '|:--|--:|',
  '| 1 | 2 |',
  '',
  '![alt text](https://example.com/i.png) [bad](javascript:alert(1)) <img src=x onerror=alert(1)>',
].join('\n')

test.describe('note widget', () => {
  test('renders safe Markdown; no title bar; tone', async ({ page, api, ws }) => {
    await install(api, 'note')
    const w = await addWidget(api, 'note', 'note', ws, { tone: 'yellow' })
    await act(api, w.id, 'save', { value: MD })
    await openApp(page)
    const c = card(page, 'note')
    const md = c.getByTestId('widget-markdown')
    await expect(md).toBeVisible()
    // no title bar, soft tone
    await expect(c.getByTestId('widget-title')).toHaveCount(0)
    await expect(c).toHaveAttribute('data-tone', 'yellow')
    await expect(c).toHaveAttribute('data-bare', '1')
    const bg = (l: Locator) => l.evaluate((e) => getComputedStyle(e).backgroundImage)
    expect(await bg(c)).toContain('gradient')

    await expect(md.locator('h1')).toHaveText('Title H1')
    await expect(md.locator('h2')).toHaveText('Sub H2')
    await expect(md.locator('strong')).toHaveText('bold')
    await expect(md.locator('em')).toHaveText('italic')
    await expect(md.locator('del')).toHaveText('gone')
    await expect(md.locator('p code')).toHaveText('code')
    const link = md.locator('a[href="https://example.com/a"]')
    await expect(link).toHaveText('link')
    await expect(link).toHaveAttribute('target', '_blank')
    await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    await expect(md.locator('a[href="https://example.org/auto"]')).toHaveText('https://example.org/auto')
    await expect(md.locator('a[href="https://bare.example/x"]')).toHaveText('https://bare.example/x')
    await expect(md.locator('p').first().locator('br')).toHaveCount(1) // the line break is kept
    await expect(md.locator('blockquote')).toHaveText('quote line')
    await expect(md.locator('hr')).toHaveCount(1)
    await expect(md.locator('ol > li')).toHaveCount(2)
    await expect(md.locator('ol > li').nth(1).locator('ul > li')).toHaveText('nested')
    await expect(md.getByTestId('widget-md-check')).toHaveCount(2)
    await expect(md.getByTestId('widget-md-check').nth(0)).not.toBeChecked()
    await expect(md.getByTestId('widget-md-check').nth(1)).toBeChecked()
    await expect(md.locator('pre code')).toHaveText('raw <b>not bold</b>')
    await expect(md.locator('th')).toHaveText(['A', 'B'])
    await expect(md.locator('td').nth(1)).toHaveCSS('text-align', 'right')
    // an image is a link, nothing is loaded; unsafe links are text; raw HTML is text
    await expect(md.locator('img')).toHaveCount(0)
    await expect(md.locator('a[href="https://example.com/i.png"]')).toHaveText('alt text')
    await expect(md.locator('a[href^="javascript"]')).toHaveCount(0)
    await expect(md).toContainText('bad')
    await expect(md).toContainText('<img src=x onerror=alert(1)>')
    await expect(md.locator('b')).toHaveCount(0)

    // every tone is a tint; "default" has none
    for (const tone of ['pink', 'blue', 'green', 'purple']) {
      await api.call('PATCH', `/api/widgets/${w.id}`, { config: { tone } })
      await page.reload()
      await expect(card(page, 'note')).toHaveAttribute('data-tone', tone)
    }
    await api.call('PATCH', `/api/widgets/${w.id}`, { config: { tone: 'default' } })
    await page.reload()
    await expect(card(page, 'note').getByTestId('widget-markdown')).toBeVisible()
    await expect(card(page, 'note')).not.toHaveAttribute('data-tone', /.+/)
  })

  test('double-click edits: Ctrl+Enter and blur save, Esc cancels, the textarea grows with the text', async ({ page, api, ws }) => {
    await install(api, 'note')
    const w = await addWidget(api, 'note', 'note', ws, { tone: 'blue' })
    await openApp(page)
    const c = card(page, 'note')
    // an empty note shows the placeholder (with the hint for the admin)
    await expect(c.getByTestId('widget-markdown-placeholder')).toBeVisible()
    await c.getByTestId('widget-markdown').dblclick()
    const ta = c.getByTestId('widget-markdown-input')
    await expect(ta).toBeFocused()
    const h0 = (await box(ta)).height
    await ta.fill('first\nsecond\nthird\nfourth\nfifth\nsixth\nseventh\neighth')
    await expect.poll(async () => (await box(ta)).height).toBeGreaterThan(h0 + 40)
    await ta.fill('# Saved with ctrl\n\nbody')
    await ta.press('Control+Enter')
    await expect(ta).toHaveCount(0)
    await expect(c.getByTestId('widget-markdown').locator('h1')).toHaveText('Saved with ctrl')
    await expect.poll(() => noteText(api, w.id)).toBe('# Saved with ctrl\n\nbody')

    // Esc discards
    await c.getByTestId('widget-markdown').dblclick()
    await ta.fill('this is thrown away')
    await ta.press('Escape')
    await expect(ta).toHaveCount(0)
    await expect(c.getByTestId('widget-markdown').locator('h1')).toHaveText('Saved with ctrl')
    expect(await noteText(api, w.id)).toBe('# Saved with ctrl\n\nbody')

    // the edit button, and a blur saves
    await c.hover()
    await c.getByTestId('widget-markdown-edit').click()
    await expect(ta).toBeFocused()
    await ta.fill('saved on blur')
    await page.mouse.click(5, 5) // click elsewhere
    await expect(ta).toHaveCount(0)
    await expect(c.getByTestId('widget-markdown')).toHaveText('saved on blur')
    await expect.poll(() => noteText(api, w.id)).toBe('saved on blur')

    // unchanged text is not saved; an empty value clears the note
    await c.getByTestId('widget-markdown').dblclick()
    await ta.press('Control+Enter')
    await c.getByTestId('widget-markdown').dblclick()
    await ta.fill('')
    await ta.press('Control+Enter')
    await expect(c.getByTestId('widget-markdown-placeholder')).toBeVisible()
    await expect.poll(() => noteText(api, w.id)).toBe('')
    // saved text survives a reload (and the other tones keep their tint)
    await c.getByTestId('widget-markdown').dblclick()
    await ta.fill('persisted')
    await ta.press('Meta+Enter')
    await page.reload()
    await expect(card(page, 'note').getByTestId('widget-markdown')).toHaveText('persisted')
    await expect(card(page, 'note')).toHaveAttribute('data-tone', 'blue')
  })

  test('task checkboxes rewrite the line and save', async ({ page, api, ws }) => {
    await install(api, 'note')
    const w = await addWidget(api, 'note', 'note', ws)
    const text = 'Shopping\n\n- [ ] eggs\n- [x] bread\n  - [ ] nested task\n1. [ ] numbered\n\n> - [ ] quoted\n\nplain - [ ] not a task'
    await act(api, w.id, 'save', { value: text })
    await openApp(page)
    const c = card(page, 'note')
    const checks = c.getByTestId('widget-md-check')
    await expect(checks).toHaveCount(5)
    await checks.nth(0).check() // eggs
    await expect.poll(() => noteText(api, w.id)).toBe(text.replace('- [ ] eggs', '- [x] eggs'))
    await expect(checks.nth(0)).toBeChecked()
    await checks.nth(1).uncheck() // bread
    await checks.nth(2).check() // nested
    await checks.nth(3).check() // numbered
    await checks.nth(4).check() // quoted
    await expect
      .poll(() => noteText(api, w.id))
      .toBe('Shopping\n\n- [x] eggs\n- [ ] bread\n  - [x] nested task\n1. [x] numbered\n\n> - [x] quoted\n\nplain - [ ] not a task')
    await expect(c.getByTestId('widget-markdown-input')).toHaveCount(0) // ticking does not open the editor
    await page.reload()
    await expect(c.getByTestId('widget-md-check').nth(1)).not.toBeChecked()
    await expect(c.getByTestId('widget-md-check').nth(4)).toBeChecked()
  })

  test('a failed save keeps the text in the editor and toasts the error', async ({ page, api, ws }) => {
    await install(api, 'note')
    const w = await addWidget(api, 'note', 'note', ws)
    await act(api, w.id, 'save', { value: 'original' })
    await openApp(page)
    const c = card(page, 'note')
    await expect(c.getByTestId('widget-markdown')).toHaveText('original')
    await page.route('**/api/widgets/*/action', (route) => route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: 'disk full' }) }))
    await c.getByTestId('widget-markdown').dblclick()
    await c.getByTestId('widget-markdown-input').fill('new text that must not get lost')
    await c.getByTestId('widget-markdown-input').press('Control+Enter')
    await expect(page.getByTestId('toast').filter({ hasText: 'disk full' })).toBeVisible()
    await expect(c.getByTestId('widget-markdown-input')).toHaveValue('new text that must not get lost')
    await page.unroute('**/api/widgets/*/action')
    await c.getByTestId('widget-markdown-input').press('Control+Enter')
    await expect(c.getByTestId('widget-markdown')).toHaveText('new text that must not get lost')
    expect(await noteText(api, w.id)).toBe('new text that must not get lost')
  })

  test('visitors see the note read-only', async ({ browser, api, ws }) => {
    await install(api, 'note')
    const w = await addWidget(api, 'note', 'note', ws, { tone: 'green' })
    await act(api, w.id, 'save', { value: 'Hello visitor\n\n- [ ] a task' })
    await api.settings({ guest_access: true })
    const ctx = await browser.newContext({ baseURL: origin(), locale: 'en-US' })
    const g = await ctx.newPage()
    await g.goto('/')
    const c = card(g, 'note')
    await expect(c.getByTestId('widget-markdown')).toContainText('Hello visitor')
    await expect(c).toHaveAttribute('data-tone', 'green')
    await expect(c.getByTestId('widget-markdown-edit')).toHaveCount(0)
    await c.getByTestId('widget-markdown').dblclick()
    await expect(c.getByTestId('widget-markdown-input')).toHaveCount(0)
    await expect(c.getByTestId('widget-md-check')).toBeDisabled()
    await c.getByTestId('widget-md-check').click({ force: true })
    await g.waitForTimeout(300)
    expect(await noteText(api, w.id)).toBe('Hello visitor\n\n- [ ] a task')
    // an empty note: no "double-click to edit" hint for a visitor
    await act(api, w.id, 'save', { value: '' })
    await g.reload()
    await expect(c.getByTestId('widget-markdown')).toBeVisible()
    await expect(c.getByTestId('widget-markdown-placeholder')).toHaveText(/Double-click to write a note/) // the plugin's own placeholder is shown
    await ctx.close()
  })

  test('a note can be added from the picker and resized like any card', async ({ page, api }) => {
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId('widget-add').click()
    await page.getByTestId('widget-search').fill('note')
    await page.getByTestId('widget-pick-note-note').click() // installs the plugin
    await expect(page.getByTestId('widget-cfg-tone')).toHaveValue('yellow')
    await page.getByTestId('widget-cfg-tone').selectOption('pink')
    await page.getByTestId('widget-save').click()
    const c = card(page, 'note')
    await expect(c).toHaveAttribute('data-tone', 'pink')
    // in layout edit mode the title bar comes back (it carries the drag handle) and the note is inert
    await expect(c.getByTestId('widget-title')).toBeVisible()
    await c.getByTestId('widget-markdown').dblclick()
    await expect(c.getByTestId('widget-markdown-input')).toHaveCount(0)
    await toggleEdit(page, false)
    await expect(c.getByTestId('widget-title')).toHaveCount(0)
    void api
  })
})
