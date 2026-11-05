import type { Page } from '@playwright/test'
import { Api, CSRF, expect, openApp, openSettings, origin, test } from '../helpers'
import { request as pwRequest } from '@playwright/test'

// Plugin manager v2 (docs/PLUGINS.md 11.2 - 11.5): source editor (syntax highlighting) + always-visible test panel, catalog source dialog, preview before install, "Develop with AI",
// third-party (GitHub) tab. Only offline catalog plugins (calculator, world-clock, countdown) plus tiny inline plugins.
// The third-party tab talks to the real GitHub from the server and the server cannot be pointed elsewhere without code
// changes, so those tests only check that the tab renders and ends in a clean list / empty / error state.

interface P { id: string; name: string; version: string; enabled: boolean; last_error?: string }
const pluginsOf = async (api: Api): Promise<P[]> => (await api.call('GET', '/api/plugins')) || []
const install = (api: Api, id: string, enable = true) => api.call('POST', '/api/plugins', { catalog: id, enable })
const row = (page: Page, id: string) => page.locator(`[data-testid="plugin-row"][data-plugin-id="${id}"]`)
const panelGeometry = (page: Page) =>
  page.evaluate(() => {
    const p = document.querySelector('[data-testid="plugin-test-panel"]')!.getBoundingClientRect()
    const e = document.querySelector('[data-testid="plugin-source-editor-wrap"]')!.getBoundingClientRect()
    return { panelBottom: p.bottom, panelH: p.height, editorTop: e.top, editorH: e.height }
  })
const srcOf = async (api: Api, id: string) => (await api.call<{ source: string; lang: string }>('GET', `/api/plugins/${id}/source`)).source

const LOGGER = `export const manifest = {
  id: "e2e-logger", name: "E2E Logger", version: "1.0.0",
  search: { name: "E2E Logger", prefix: "log" },
}
export async function search(ctx) {
  console.log("hello from the logger", ctx.query)
  console.warn("careful now")
  return [{ title: "echo:" + ctx.query }]
}
`

test.describe('plugin manager: source editor, test panel, preview, Develop with AI, third-party', () => {
  test.afterAll(async ({ admin }) => {
    await new Api(admin).reset()
  })

  test('source editor: opens with the installed source; a syntax error shows a line marker and changes nothing; a valid edit updates the row', async ({ page, api }) => {
    await install(api, 'calculator')
    const original = await srcOf(api, 'calculator')
    await openApp(page)
    await openSettings(page, 'plugins')
    await expect(row(page, 'calculator').locator('small')).toHaveText('v1.0.0')
    await row(page, 'calculator').getByTestId('plugin-source-btn').click()

    const modal = page.getByTestId('plugin-source-modal')
    await expect(modal).toBeVisible()
    const ed = page.getByTestId('plugin-source-editor')
    await expect(ed).toHaveValue(original)
    await expect(ed).toBeFocused()
    await expect(ed).toHaveValue(/export const manifest/)
    await expect(modal.locator('.ce-gut')).toContainText('1') // line numbers
    await expect(page.getByTestId('plugin-source-state')).toHaveAttribute('data-dirty', '0')
    await expect(page.getByTestId('plugin-source-save')).toBeDisabled()
    await expect(page.getByTestId('plugin-source-lang')).toHaveValue('js')
    await expect(page.getByTestId('plugin-source-error')).toHaveCount(0)
    await expect(page.getByTestId('plugin-source-editor-errline')).toHaveCount(0)

    // ---- a syntax error: server says plugin.js:<line>:<col>, the editor marks the line ----
    const lines = original.split('\n').length
    const broken = original + '\nconst = ;\n'
    await ed.fill(broken)
    await expect(page.getByTestId('plugin-source-state')).toHaveAttribute('data-dirty', '1')
    await page.getByTestId('plugin-source-save').click()
    const err = page.getByTestId('plugin-source-error')
    await expect(err).toBeVisible()
    await expect(err).toContainText('SyntaxError')
    const m = /plugin\.js:(\d+):(\d+)/.exec((await err.textContent())!)
    expect(m, 'server error carries :line:col').not.toBeNull()
    expect(Number(m![1])).toBe(lines + 1) // the broken line we appended (the original ends with a newline)
    await expect(page.getByTestId('plugin-source-goto')).toBeVisible()
    await expect(page.getByTestId('plugin-source-goto')).toContainText(m![1])
    await expect(page.getByTestId('plugin-source-editor-errline')).toBeVisible()
    // nothing changed on the server, the row is untouched, the editor stays dirty
    expect(await srcOf(api, 'calculator')).toBe(original)
    expect((await pluginsOf(api)).find((p) => p.id === 'calculator')!.version).toBe('1.0.0')
    await expect(page.getByTestId('plugin-source-state')).toHaveAttribute('data-dirty', '1')
    // editing clears the marker
    await ed.press('End')
    await ed.press('x')
    await expect(page.getByTestId('plugin-source-editor-errline')).toHaveCount(0)

    // ---- manifest.id must not change ----
    await ed.fill(original.replace(/id:\s*(['"])calculator\1/, 'id: "other"'))
    await page.getByTestId('plugin-source-save').click()
    await expect(err).toBeVisible()
    await expect(err).toContainText(/id/i)
    expect((await pluginsOf(api)).map((p) => p.id)).toEqual(['calculator'])

    // ---- closing with unsaved changes asks; "keep editing" stays ----
    await page.getByTestId('plugin-source-close').click()
    await expect(page.getByTestId('plugin-source-confirm')).toBeVisible()
    await page.getByTestId('plugin-source-keep').click()
    await expect(page.getByTestId('plugin-source-confirm')).toHaveCount(0)
    await expect(modal).toBeVisible()

    // ---- a valid edit (version bump) saves with Ctrl/Cmd+S and updates the plugin row ----
    const bumped = original.replace(/version:\s*(['"])1\.0\.0\1/, 'version: "1.0.7"')
    expect(bumped).not.toBe(original)
    await ed.fill(bumped)
    await ed.press(process.platform === 'darwin' ? 'Meta+s' : 'Control+s')
    await expect(page.getByTestId('toast').filter({ hasText: /saved/i }).last()).toBeVisible()
    await expect(page.getByTestId('plugin-source-state')).toHaveAttribute('data-dirty', '0')
    await expect(err).toHaveCount(0)
    expect(await srcOf(api, 'calculator')).toBe(bumped)
    await expect.poll(async () => (await pluginsOf(api)).find((p) => p.id === 'calculator')!.version).toBe('1.0.7')
    // still enabled; the list behind the dialog shows the new version
    expect((await pluginsOf(api)).find((p) => p.id === 'calculator')!.enabled).toBe(true)
    await page.getByTestId('plugin-source-close').click()
    await expect(modal).toHaveCount(0)
    await expect(page.getByTestId('settings-modal')).toBeVisible()
    await expect(row(page, 'calculator').locator('small')).toHaveText('v1.0.7')
    await expect(row(page, 'calculator')).toHaveAttribute('data-enabled', '1')

    // ---- discard: unsaved changes are dropped ----
    await row(page, 'calculator').getByTestId('plugin-source-btn').click()
    await page.getByTestId('plugin-source-editor').fill(bumped + '\n// scratch\n')
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('plugin-source-confirm')).toBeVisible()
    await page.getByTestId('plugin-source-discard').click()
    await expect(modal).toHaveCount(0)
    expect(await srcOf(api, 'calculator')).toBe(bumped)
  })

  test('test panel: search "1+2*3" on calculator shows "= 7" (also while the plugin is disabled)', async ({ page, api }) => {
    await install(api, 'calculator', false) // /test works on disabled plugins
    await openApp(page)
    await openSettings(page, 'plugins')
    await expect(row(page, 'calculator')).toHaveAttribute('data-enabled', '0')
    await row(page, 'calculator').getByTestId('plugin-source-btn').click()
    await expect(page.getByTestId('plugin-source-editor')).toHaveValue(/export const manifest/)
    // the test panel is always there (no toggle), and it sits ABOVE the source
    await expect(page.getByTestId('plugin-test-panel')).toBeVisible()
    await expect(page.getByTestId('plugin-source-test-toggle')).toHaveCount(0)
    const g = await panelGeometry(page) // both rects read in one go (the dialog animates in)
    expect(g.panelBottom).toBeLessThanOrEqual(g.editorTop + 1)
    expect(g.editorH, 'the editor keeps most of the height').toBeGreaterThan(g.panelH)
    await expect(page.getByTestId('plugin-test-hook')).toHaveValue('search')
    await expect(page.getByTestId('plugin-test-result')).toHaveCount(0)
    await page.getByTestId('plugin-test-query').fill('1+2*3')
    await page.getByTestId('plugin-test-run').click()
    const res = page.getByTestId('plugin-test-result')
    await expect(res).toBeVisible()
    await expect(res).toHaveAttribute('data-ok', '1')
    await expect(page.getByTestId('plugin-test-json')).toContainText('= 7')
    await expect(page.getByTestId('plugin-test-json')).toContainText('1+2*3')
    await expect(page.getByTestId('plugin-test-ms')).toHaveText(/^\d+ ms$/)
    await expect(page.getByTestId('plugin-test-error')).toHaveCount(0)
    await expect(page.getByTestId('plugin-test-logs')).toBeVisible()

    // not math: still a successful run, with an empty result
    await page.getByTestId('plugin-test-query').fill('hello')
    await page.getByTestId('plugin-test-run').click()
    await expect(page.getByTestId('plugin-test-result')).toHaveAttribute('data-ok', '1')
    await expect(page.getByTestId('plugin-test-json')).not.toContainText('=')
    // the run did not enable anything
    expect((await pluginsOf(api)).find((p) => p.id === 'calculator')!.enabled).toBe(false)
  })

  test('test panel: logs ({ts,level,msg}) and results of an edited-but-unsaved plugin ("Save & run"), widget hook with an instance', async ({ page, api }) => {
    await api.call('POST', '/api/plugins', { source: LOGGER, enable: false })
    // the REST contract the panel relies on
    const r = await api.call<{ ok: boolean; logs: { ts: number; level: string; msg: string }[]; result: unknown[]; ms: number }>('POST', '/api/plugins/e2e-logger/test', { hook: 'search', query: 'log abc' })
    expect(r.ok).toBe(true)
    expect(r.logs.map((l) => l.level)).toEqual(['info', 'warn'])
    expect(r.logs[0]).toEqual({ ts: expect.any(Number), level: 'info', msg: 'hello from the logger log abc' })
    expect(r.logs[0].ts).toBeGreaterThan(1.5e12) // Unix milliseconds

    await openApp(page)
    await openSettings(page, 'plugins')
    await row(page, 'e2e-logger').getByTestId('plugin-source-btn').click()
    await page.getByTestId('plugin-test-query').fill('zzz')
    await page.getByTestId('plugin-test-run').click()
    await expect(page.getByTestId('plugin-test-result')).toHaveAttribute('data-ok', '1')
    await expect(page.getByTestId('plugin-test-json')).toContainText('echo:zzz')
    const logs = page.getByTestId('plugin-test-logs')
    await expect(logs).toContainText('INFO')
    await expect(logs).toContainText('hello from the logger zzz')
    await expect(logs).toContainText('WARN')
    await expect(logs).toContainText('careful now')
    await expect(logs).not.toContainText('Invalid Date')
    await expect(logs).not.toContainText('NaN')

    // edit without saving: the button says "Save & run", the run saves first and then tests the new code
    const ed = page.getByTestId('plugin-source-editor')
    await ed.fill(LOGGER.replace('"echo:"', '"echo2:"'))
    await expect(page.getByTestId('plugin-source-state')).toHaveAttribute('data-dirty', '1')
    await expect(page.getByTestId('plugin-test-run')).toContainText(/save/i)
    await page.getByTestId('plugin-test-run').click()
    await expect(page.getByTestId('plugin-test-json')).toContainText('echo2:zzz')
    await expect(page.getByTestId('plugin-source-state')).toHaveAttribute('data-dirty', '0')
    expect(await srcOf(api, 'e2e-logger')).toContain('echo2:')

    // a hook that throws: the run is reported as failed (not as a broken panel)
    await ed.fill(LOGGER.replace('return [{ title: "echo:" + ctx.query }]', 'throw new Error("boom")'))
    await page.getByTestId('plugin-test-run').click()
    await expect(page.getByTestId('plugin-test-result')).toHaveAttribute('data-ok', '0')
    await expect(page.getByTestId('plugin-test-error')).toContainText('boom')
    // the logs written before the failure are still shown
    await expect(page.getByTestId('plugin-test-logs')).toContainText('hello from the logger')
  })

  test('test panel on a widget plugin: pick the saved instance, run the widget hook', async ({ page, api, ws }) => {
    await install(api, 'countdown')
    await api.call('POST', '/api/widgets', { plugin: 'countdown', widget: 'timer', workspace_id: ws, config: { label: 'Panel', date: '2099-01-01' } })
    await openApp(page)
    await openSettings(page, 'plugins')
    await row(page, 'countdown').getByTestId('plugin-source-btn').click()
    await expect(page.getByTestId('plugin-test-hook')).toHaveValue('widget')
    await expect(page.getByTestId('plugin-test-target')).toHaveValue('timer')
    await expect(page.getByTestId('plugin-test-instance').locator('option')).toHaveCount(2) // "none" + the instance
    await page.getByTestId('plugin-test-instance').selectOption({ index: 1 })
    await page.getByTestId('plugin-test-run').click()
    await expect(page.getByTestId('plugin-test-result')).toHaveAttribute('data-ok', '1')
    await expect(page.getByTestId('plugin-test-json')).toContainText('Panel')
    await expect(page.getByTestId('plugin-test-json')).toContainText('"type": "stat"')
    // bad JSON in the config box is a local error, nothing is sent
    await page.getByTestId('plugin-test-config').fill('{nope')
    await page.getByTestId('plugin-test-run').click()
    await expect(page.getByTestId('plugin-test-localerror')).toBeVisible()
  })

  test('catalog items have a source icon after the version: the dialog shows info + highlighted read-only source; nothing is installed until Install; the paste/URL previews still work', async ({ page, api }) => {
    await openApp(page)
    await openSettings(page, 'plugins')
    await page.getByTestId('plugin-empty-add').click()
    const item = page.locator('[data-testid="plugin-catalog-item"][data-plugin-id="countdown"]')
    await expect(item).toBeVisible()
    await expect(item.getByTestId('plugin-catalog-preview')).toHaveCount(0) // replaced by the icon
    const icon = item.getByTestId('plugin-catalog-source')
    await expect(icon).toBeVisible()
    await expect(icon.locator('svg')).toHaveCount(1)
    // it sits in the title row, right after the version number
    const order = await item.locator('.pl-name').evaluate((el) => Array.from(el.children).map((c) => c.tagName + ':' + (c.getAttribute('data-testid') || '')))
    expect(order).toEqual(['B:', 'SMALL:', 'BUTTON:plugin-catalog-source'])
    await expect(item.locator('.pl-name small')).toHaveText('v1.0.0')

    await icon.click()
    const dlg = page.getByTestId('plugin-catalog-source-modal')
    await expect(dlg).toBeVisible()
    await expect(dlg.getByTestId('plugin-catalog-source-info-name')).toHaveText('Countdown')
    await expect(dlg.getByTestId('plugin-catalog-source-info-version')).toHaveText('v1.0.0')
    await expect(dlg.getByTestId('plugin-catalog-source-info-desc')).not.toBeEmpty()
    await expect(dlg.getByTestId('plugin-catalog-source-info-author')).toBeVisible()
    await expect(dlg.getByTestId('plugin-catalog-source-info-caps').getByTestId('plugin-badge')).not.toHaveCount(0) // capabilities
    await expect(dlg.getByTestId('plugin-perms')).toBeVisible()
    await expect(dlg.getByTestId('plugin-perm-none')).toBeVisible() // countdown asks for nothing
    await expect(dlg.getByTestId('plugin-catalog-source-info-config-field').first()).toBeVisible() // its settings
    const code = dlg.getByTestId('plugin-catalog-source-code')
    await expect(code).toHaveValue(/export const manifest/)
    await expect(code).toHaveJSProperty('readOnly', true)
    // the source is highlighted
    await expect(dlg.locator('.ce-hl .h-k').first()).toHaveText('export')
    await expect(dlg.locator('.ce-hl .h-s').first()).toBeVisible()
    await expect(dlg.getByTestId('plugin-catalog-source-error')).toHaveCount(0)
    await expect(dlg.getByTestId('plugin-catalog-source-install')).toBeEnabled()
    expect(await pluginsOf(api)).toEqual([]) // looking is not installing
    // Escape closes the dialog and returns to the list (still on the Add view)
    await page.keyboard.press('Escape')
    await expect(dlg).toHaveCount(0)
    await expect(item).toBeVisible()
    await expect(item).toHaveAttribute('data-installed', '0')
    expect(await pluginsOf(api)).toEqual([])

    // Install from the dialog
    await icon.click()
    await dlg.getByTestId('plugin-catalog-source-install').click()
    await expect(dlg).toHaveCount(0)
    await expect.poll(async () => (await pluginsOf(api)).map((p) => `${p.id}:${p.enabled}`)).toEqual(['countdown:true'])
    await api.reset()
    await openApp(page)
    await openSettings(page, 'plugins')
    await page.getByTestId('plugin-empty-add').click()

    // the paste tab: valid source with permissions
    await page.getByTestId('plugin-add-tab-source').click()
    const ok = `export const manifest = { id: "e2e-prev", name: "E2E Preview", version: "2.0.0", permissions: { fetch: ["example.com"] }, search: { name: "P", prefix: "p" } }
export async function search(ctx) { return [] }
`
    await page.getByTestId('plugin-source-input').fill(ok)
    await page.getByTestId('plugin-source-preview').click()
    await expect(page.getByTestId('plugin-preview-name')).toHaveText('E2E Preview')
    await expect(page.getByTestId('plugin-perm-host')).toHaveText(/example\.com/)
    await expect(page.getByTestId('plugin-preview-code')).toHaveValue(ok)
    expect(await pluginsOf(api)).toEqual([])

    // invalid source: a clean error with line:col, still no install
    await page.getByTestId('plugin-source-input').fill('export const manifest = {\n  id: "bad",\n  name: = 1\n}\n')
    await expect(page.getByTestId('plugin-preview')).toHaveCount(0) // editing closes the stale preview
    await page.getByTestId('plugin-source-preview').click()
    const e = page.getByTestId('plugin-preview-error')
    await expect(e).toBeVisible()
    await expect(e).toContainText(/plugin\.js:3:\d+/)
    await expect(page.getByTestId('plugin-preview-name')).toHaveCount(0)
    expect(await pluginsOf(api)).toEqual([])

    // URL tab: an unreachable URL is reported as an error in the panel, not as a failed page
    await page.getByTestId('plugin-add-tab-url').click()
    await page.getByTestId('plugin-url-input').fill('http://127.0.0.1:1/nope.js')
    await page.getByTestId('plugin-url-preview').click()
    await expect(page.getByTestId('plugin-preview-error')).toBeVisible()
    await expect(page.getByTestId('plugin-preview-code')).toHaveCount(0)
    expect(await pluginsOf(api)).toEqual([])
  })

  test('the permission dialog has a "view source" button; the preview REST contract (200 + error, 404, 400)', async ({ page, api }) => {
    const source = `export const manifest = { id: "e2e-perm2", name: "E2E Perm Two", version: "1.0.0", permissions: { fetch: ["example.com"] }, search: { name: "P2", prefix: "pp" } }
export async function search(ctx) { return [] }
`
    // REST contract first
    const good = await api.call<any>('POST', '/api/plugins/preview', { source })
    expect(good).toMatchObject({ source, lang: 'js', installed: false })
    expect(good.plugin).toMatchObject({ id: 'e2e-perm2', enabled: false, permissions: { fetch: ['example.com'] } })
    const bad = await api.call<any>('POST', '/api/plugins/preview', { source: 'const = 1' })
    expect(bad.plugin).toBeNull()
    expect(bad.error).toMatch(/plugin\.js:1:\d+/)
    expect(bad.source).toBe('const = 1')
    const cat = await api.call<any>('POST', '/api/plugins/preview', { catalog: 'calculator' })
    expect(cat.plugin.id).toBe('calculator')
    await expect(api.call('POST', '/api/plugins/preview', { catalog: 'nope' })).rejects.toThrow(/404/)
    await expect(api.call('POST', '/api/plugins/preview', {})).rejects.toThrow(/400/)
    await expect(api.call('POST', '/api/plugins/preview', { source, catalog: 'calculator' })).rejects.toThrow(/400/)
    expect(await pluginsOf(api)).toEqual([]) // previews never install

    await openApp(page)
    await openSettings(page, 'plugins')
    await page.getByTestId('plugin-empty-add').click()
    await page.getByTestId('plugin-add-tab-source').click()
    await page.getByTestId('plugin-source-input').fill(source)
    await page.getByTestId('plugin-source-install').click()
    const modal = page.getByTestId('plugin-perm-modal')
    await expect(modal).toBeVisible()
    await expect(modal.getByTestId('plugin-perm-source')).toHaveCount(0)
    await modal.getByTestId('plugin-perm-source-btn').click()
    await expect(modal.getByTestId('plugin-perm-source')).toBeVisible()
    await expect(modal.getByTestId('plugin-perm-source').locator('textarea')).toHaveValue(source)
    // installed disabled until allowed (unchanged v1 behaviour)
    expect((await pluginsOf(api)).map((p) => `${p.id}:${p.enabled}`)).toEqual(['e2e-perm2:false'])
    await modal.getByTestId('plugin-perm-cancel').click()
  })

  test('REST: PUT source keeps state, rejects an id change (400) and a missing plugin (404); anonymous / guests are refused', async ({ api, browser }) => {
    await install(api, 'countdown')
    const src = await srcOf(api, 'countdown')
    const bumped = src.replace(/version:\s*(['"])1\.0\.0\1/, 'version: "1.2.3"')
    const info = await api.call<P>('PUT', '/api/plugins/countdown/source', { source: bumped })
    expect(info).toMatchObject({ id: 'countdown', version: '1.2.3', enabled: true })
    await expect(api.call('PUT', '/api/plugins/countdown/source', { source: src.replace(/id:\s*(['"])countdown\1/, 'id: "else"') })).rejects.toThrow(/400/)
    await expect(api.call('PUT', '/api/plugins/nothere/source', { source: bumped })).rejects.toThrow(/404/)
    await expect(api.call('PUT', '/api/plugins/countdown/source', { source: 'const = 1' })).rejects.toThrow(/plugin\.js:1:\d+/)
    expect(await srcOf(api, 'countdown')).toBe(bumped)

    await api.settings({ guest_access: true })
    const anon = await pwRequest.newContext({ baseURL: origin(), extraHTTPHeaders: CSRF })
    for (const [m, p, body] of [
      ['PUT', '/api/plugins/countdown/source', { source: bumped }],
      ['POST', '/api/plugins/preview', { catalog: 'countdown' }],
      ['POST', '/api/plugins/countdown/test', { hook: 'widget', target: 'timer' }],
      ['GET', '/api/plugins/market', undefined],
      ['GET', '/api/plugins/market/repo?repo=a/b', undefined],
      ['POST', '/api/plugins/market/cache/clear', {}],
    ] as const) {
      const r = await anon.fetch(p, { method: m, data: body as any })
      expect([401, 403], `${m} ${p}`).toContain(r.status())
    }
    await anon.dispose()
  })

  test('Develop with AI: the dialog shows a short Skill prompt (no guide link), copy works, and step 1 jumps to Settings > AI', async ({ page, api }) => {
    void api
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: origin() })
    await openApp(page)
    await openSettings(page, 'plugins')
    await page.getByTestId('plugin-dev-ai-btn').click()
    const dlg = page.getByTestId('plugin-dev-modal')
    await expect(dlg).toBeVisible()
    for (const s of ['dev-step-1', 'dev-step-2']) await expect(page.getByTestId(s)).toBeVisible()
    await expect(page.getByTestId('dev-step-3')).toHaveCount(0)
    const prompt = page.getByTestId('dev-prompt')
    // the Skill carries the guide and the workflow: one short sentence, no guide URL, no long steps
    await expect(prompt).toContainText('Use the Genkan Skill to develop a Genkan plugin for me:')
    expect(await prompt.textContent()).not.toMatch(/\.md|githubusercontent|enable:false|\/api\/plugins/)
    await expect(page.getByTestId('dev-placeholder')).toBeVisible() // the requirement goes here
    await expect(dlg.locator('a')).toHaveCount(0)

    const full = (await prompt.textContent())!
    await page.getByTestId('dev-copy-btn').click()
    await expect(page.getByTestId('toast').filter({ hasText: /copied/i }).last()).toBeVisible()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(full)
    await expect(page.getByTestId('dev-copy').locator('svg[data-icon="check"]')).toBeVisible()
    await page.evaluate(() => navigator.clipboard.writeText(''))
    await page.getByTestId('dev-copy').click() // the small icon button copies too
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(full)

    // step 1
    await page.getByTestId('dev-open-ai').click()
    await expect(dlg).toHaveCount(0)
    await expect(page.getByTestId('settings-tab-ai')).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('settings-panel-ai')).toBeVisible()

    // Esc / close button
    await page.getByTestId('settings-tab-plugins').click()
    await page.getByTestId('plugin-dev-ai-btn').click()
    await expect(dlg).toBeVisible()
    await page.getByTestId('plugin-dev-close').click()
    await expect(dlg).toHaveCount(0)
    await expect(page.getByTestId('settings-modal')).toBeVisible()
  })

  test('Develop with AI follows the UI language (the prompt is the short Chinese sentence)', async ({ page }) => {
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: origin() })
    await openApp(page)
    await page.evaluate(() => localStorage.setItem('genkan.lang', 'zh'))
    await page.reload()
    await expect(page.getByTestId('ws-menu-btn')).toBeVisible()
    await openSettings(page, 'plugins')
    await page.getByTestId('plugin-dev-ai-btn').click()
    const prompt = page.getByTestId('dev-prompt')
    await expect(prompt).toContainText('用 Genkan Skill 帮我开发一个 Genkan 插件：')
    await expect(page.getByTestId('dev-placeholder')).toContainText('在这里写下你的需求')
    await page.getByTestId('dev-copy-btn').click()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('用 Genkan Skill 帮我开发一个 Genkan 插件：【在这里写下你的需求…】')
  })

  test('Third-party tab renders (warning, search, sort) and ends in a list, an empty state or a clean error - never a broken page', async ({ page, api }) => {
    void api
    await openApp(page)
    await openSettings(page, 'plugins')
    await page.getByTestId('plugin-add-btn').click()
    await page.getByTestId('plugin-add-tab-market').click()
    await expect(page.getByTestId('plugin-add-tab-market')).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByTestId('plugin-market')).toBeVisible()
    await expect(page.getByTestId('market-warning')).toBeVisible()
    await expect(page.getByTestId('market-search')).toBeVisible()
    await expect(page.getByTestId('market-sort-stars')).toHaveAttribute('aria-pressed', 'true')

    // settle: loading goes away and one of the three states is shown (GitHub may be reachable, empty, rate limited or unreachable)
    const settle = async () => {
      await expect(page.getByTestId('market-loading')).toHaveCount(0, { timeout: 20_000 })
      await expect
        .poll(async () => (await page.getByTestId('market-repo').count()) + (await page.getByTestId('market-empty').count()) + (await page.getByTestId('market-error').count()), { timeout: 20_000 })
        .toBeGreaterThan(0)
    }
    await settle()
    const nRepos = await page.getByTestId('market-repo').count()
    const nEmpty = await page.getByTestId('market-empty').count()
    const nErr = await page.getByTestId('market-error').count()
    if (nRepos > 0) {
      // a list: never together with the empty state; every card is well-formed (no values asserted)
      expect(nEmpty).toBe(0)
      await expect(page.getByTestId('market-repo-name').first()).toHaveText(/^[\w.-]+\/[\w.-]+$/)
      await expect(page.getByTestId('market-repo-toggle').first()).toBeVisible()
    } else if (nErr > 0) {
      await expect(page.getByTestId('market-error')).not.toBeEmpty()
      await expect(page.getByTestId('market-error')).not.toContainText(/undefined|\[object|NaN/)
      await expect(page.getByTestId('market-retry')).toBeVisible()
    } else {
      await expect(page.getByTestId('market-empty')).toBeVisible()
    }
    // no raw 5xx / HTML page leaked into the tab
    await expect(page.getByTestId('plugin-market')).not.toContainText(/<html|Internal Server Error|Bad Gateway/i)

    // switching sort re-queries and settles again
    await page.getByTestId('market-sort-updated').click()
    await expect(page.getByTestId('market-sort-updated')).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByTestId('market-sort-stars')).toHaveAttribute('aria-pressed', 'false')
    await settle()
    // a keyword that matches nothing is an empty state or an error, not a crash
    await page.getByTestId('market-search').fill('zz-e2e-no-such-plugin-xyz')
    await page.getByTestId('market-search').press('Enter')
    await settle()
    await expect(page.getByTestId('market-repo')).toHaveCount(0)
    // the rest of the dialog still works
    await page.getByTestId('plugin-add-tab-catalog').click()
    await expect(page.getByTestId('plugin-catalog')).toBeVisible()
    expect(await pluginsOf(api)).toEqual([])
  })

  test('Third-party tab against a stubbed GitHub (browser-level routes): list, scan, preview, batch install, error + retry', async ({ page, api }) => {
    // The UI is exercised with canned /api/plugins/market* answers; installs of the "raw URL" are redirected to catalog installs.
    const info = (id: string, name: string, version = '1.0.0') => ({ id, name, version, enabled: false, permissions: { fetch: [], links: '' }, config_schema: [], widgets: [], wallpapers: [], schedules: [], search: { name } })
    const repo = (n: string, stars: number) => ({ repo: `acme/${n}`, description: `${n} plugins`, stars, updated_at: '2026-10-01T12:00:00Z', html_url: `https://github.com/acme/${n}`, owner_avatar: '', default_branch: 'main' })
    const state = { fail: true }
    await page.route('**/api/plugins/market?**', (r) => {
      if (state.fail) return r.fulfill({ json: { items: [], total: 0, cached_at: Date.now(), error: 'GitHub API rate limit exceeded; set GENKAN_GITHUB_TOKEN' } })
      const u = new URL(r.request().url())
      const items = u.searchParams.get('q') === 'zzz' ? [] : [repo('alpha', 1234), repo('beta', 7)]
      return r.fulfill({ json: { items, total: items.length, cached_at: Date.now() } })
    })
    await page.route('**/api/plugins/market/repo?**', (r) => {
      const name = new URL(r.request().url()).searchParams.get('repo')
      if (name === 'acme/beta') return r.fulfill({ json: { repo: name, default_branch: 'main', plugins: [], cached_at: Date.now() } })
      return r.fulfill({
        json: {
          repo: name,
          default_branch: 'main',
          cached_at: Date.now(),
          plugins: [
            { path: 'plugins/countdown.js', url: 'https://raw.githubusercontent.com/acme/alpha/main/plugins/countdown.js', plugin: info('countdown', 'Countdown'), installed: false },
            { path: 'plugins/clock.js', url: 'https://raw.githubusercontent.com/acme/alpha/main/plugins/clock.js', plugin: info('world-clock', 'World Clock'), installed: false },
            { path: 'plugins/broken.js', url: 'https://raw.githubusercontent.com/acme/alpha/main/plugins/broken.js', plugin: null, error: 'invalid plugin: compile error: plugin.js:3:8: SyntaxError', installed: false },
          ],
        },
      })
    })
    // a source preview of a raw URL -> the catalog's countdown preview
    await page.route('**/api/plugins/preview', async (r) => {
      const real = await api.call<any>('POST', '/api/plugins/preview', { catalog: 'countdown' })
      return r.fulfill({ json: real })
    })
    // installing a raw URL -> install the matching built-in instead (the server is not asked to reach GitHub)
    await page.route('**/api/plugins', async (r) => {
      const req = r.request()
      if (req.method() !== 'POST') return r.fallback()
      const body = req.postDataJSON()
      if (!body?.url) return r.fallback()
      const id = String(body.url).includes('countdown') ? 'countdown' : 'world-clock'
      return r.continue({ postData: JSON.stringify({ catalog: id, enable: false }) })
    })

    await openApp(page)
    await openSettings(page, 'plugins')
    await page.getByTestId('plugin-add-btn').click()
    await page.getByTestId('plugin-add-tab-market').click()

    // error first (rate limited): readable text + Retry, no repo cards, no "empty" state on top of it
    await expect(page.getByTestId('market-error')).toContainText('rate limit')
    await expect(page.getByTestId('market-repo')).toHaveCount(0)
    await expect(page.getByTestId('market-empty')).toHaveCount(0)
    state.fail = false
    await page.getByTestId('market-retry').click()
    await expect(page.getByTestId('market-error')).toHaveCount(0)
    await expect(page.getByTestId('market-repo')).toHaveCount(2)
    const alpha = page.locator('[data-testid="market-repo"][data-repo="acme/alpha"]')
    await expect(alpha.getByTestId('market-repo-name')).toHaveText('acme/alpha')
    await expect(alpha.getByTestId('market-repo-stars')).toContainText('1.2k')
    await expect(alpha.getByTestId('market-repo-link')).toHaveAttribute('href', 'https://github.com/acme/alpha')

    // a repo without plugins
    const beta = page.locator('[data-testid="market-repo"][data-repo="acme/beta"]')
    await beta.getByTestId('market-repo-toggle').click()
    await expect(beta.getByTestId('market-scan-empty')).toBeVisible()
    await beta.getByTestId('market-repo-toggle').click()

    // scan: valid, valid, invalid (shown with its error, not selectable)
    await alpha.getByTestId('market-repo-toggle').click()
    await expect(alpha).toHaveAttribute('data-open', '1')
    await expect(alpha.getByTestId('market-plugin')).toHaveCount(3)
    const bad = alpha.locator('[data-testid="market-plugin"][data-path="plugins/broken.js"]')
    await expect(bad).toHaveAttribute('data-valid', '0')
    await expect(bad.getByTestId('market-plugin-error')).toContainText('plugin.js:3:8')
    await expect(bad.getByTestId('market-plugin-check')).toBeDisabled()
    const good = alpha.locator('[data-testid="market-plugin"][data-path="plugins/countdown.js"]')
    await expect(good).toHaveAttribute('data-valid', '1')
    await expect(good.getByTestId('market-plugin-name')).toHaveText('Countdown')
    await expect(alpha.getByTestId('market-install-selected')).toBeDisabled()
    await expect(alpha.getByTestId('market-install-all')).toContainText('2')

    // preview the source of one entry (nothing installed)
    await good.getByTestId('market-plugin-preview').click()
    await expect(good.getByTestId('market-preview-name')).toHaveText('Countdown')
    await expect(good.getByTestId('market-preview-code')).toHaveValue(/export const manifest/)
    expect(await pluginsOf(api)).toEqual([])

    // a search with no match: empty state
    await page.getByTestId('market-search').fill('zzz')
    await expect(page.getByTestId('market-empty')).toBeVisible()
    await expect(page.getByTestId('market-repo')).toHaveCount(0)
    await page.getByTestId('market-search').fill('')
    await expect(page.getByTestId('market-repo')).toHaveCount(2)
    await expect(alpha).toHaveAttribute('data-open', '1') // the expanded scan survived the re-query
    await expect(alpha.getByTestId('market-plugin')).toHaveCount(3)

    // install all valid ones: sequential POSTs (enable:false), then one batch permission dialog
    await alpha.getByTestId('market-install-all').click()
    const batch = page.getByTestId('plugin-batch-modal')
    await expect(batch).toBeVisible()
    await expect(batch.getByTestId('plugin-perm-item')).toHaveCount(2)
    expect((await pluginsOf(api)).map((p) => `${p.id}:${p.enabled}`).sort()).toEqual(['countdown:false', 'world-clock:false'])
    await batch.getByTestId('plugin-batch-allow').click()
    await expect(batch).toHaveCount(0)
    await expect.poll(async () => (await pluginsOf(api)).map((p) => `${p.id}:${p.enabled}`).sort()).toEqual(['countdown:true', 'world-clock:true'])
  })

  test('Add plugin tabs are ordered: built-in, third-party, URL, paste', async ({ page }) => {
    await openApp(page)
    await openSettings(page, 'plugins')
    await page.getByTestId('plugin-add-btn').click()
    const ids = await page.getByTestId('plugin-add-tabs').locator('button').evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')))
    expect(ids).toEqual(['plugin-add-tab-catalog', 'plugin-add-tab-market', 'plugin-add-tab-url', 'plugin-add-tab-source'])
    await expect(page.getByTestId('plugin-add-tab-catalog')).toHaveAttribute('aria-pressed', 'true') // still the default
  })

  test('Third-party "Clear cache": POSTs the clear first, then refetches the search and the expanded repos, then toasts; a failed clear refetches nothing', async ({ page }) => {
    const log: string[] = []
    const failClear = { on: false }
    const repo = (n: string) => ({ repo: `acme/${n}`, description: `${n} plugins`, stars: 5, updated_at: '2026-10-01T12:00:00Z', html_url: `https://github.com/acme/${n}`, owner_avatar: '', default_branch: 'main' })
    await page.route('**/api/plugins/market?**', (r) => {
      log.push('search')
      return r.fulfill({ json: { items: [repo('alpha'), repo('beta')], total: 2, cached_at: Date.now() } })
    })
    await page.route('**/api/plugins/market/repo?**', (r) => {
      const name = new URL(r.request().url()).searchParams.get('repo')
      log.push('scan:' + name)
      return r.fulfill({ json: { repo: name, default_branch: 'main', cached_at: Date.now(), plugins: [] } })
    })
    await page.route('**/api/plugins/market/cache/clear', (r) => {
      log.push('clear')
      if (r.request().method() !== 'POST') return r.fallback()
      return failClear.on ? r.fulfill({ status: 500, json: { error: 'boom' } }) : r.fulfill({ json: { ok: true } })
    })
    await openApp(page)
    await openSettings(page, 'plugins')
    await page.getByTestId('plugin-add-btn').click()
    await page.getByTestId('plugin-add-tab-market').click()
    await expect(page.getByTestId('market-repo')).toHaveCount(2)
    const btn = page.getByTestId('market-clear-cache')
    await expect(btn).toBeVisible()
    await expect(btn).toHaveText(/clear cache/i)
    const alpha = page.locator('[data-testid="market-repo"][data-repo="acme/alpha"]')
    await alpha.getByTestId('market-repo-toggle').click() // only alpha is expanded
    await expect(alpha.getByTestId('market-scan-empty')).toBeVisible()
    expect(log).toEqual(['search', 'scan:acme/alpha'])

    log.length = 0
    await btn.click()
    await expect(page.getByTestId('toast').filter({ hasText: /cache cleared/i }).last()).toBeVisible()
    expect(log).toEqual(['clear', 'search', 'scan:acme/alpha']) // beta was not expanded: not rescanned
    await expect(page.getByTestId('market-repo')).toHaveCount(2)
    await expect(alpha).toHaveAttribute('data-open', '1')
    await expect(btn).toBeEnabled()

    // the server refuses: an error toast, and the lists are not refetched
    log.length = 0
    failClear.on = true
    await btn.click()
    await expect(page.getByTestId('toast').filter({ hasText: /boom/i }).last()).toBeVisible()
    expect(log).toEqual(['clear'])
    await expect(btn).toBeEnabled()
  })

  test('syntax highlighting: tokens are coloured, plugin source is never injected as HTML, and the overlay stays aligned with the textarea', async ({ page, api }) => {
    await api.call('POST', '/api/plugins', { source: LOGGER, enable: false })
    await openApp(page)
    await openSettings(page, 'plugins')
    await row(page, 'e2e-logger').getByTestId('plugin-source-btn').click()
    const ed = page.getByTestId('plugin-source-editor')
    const hl = page.locator('[data-testid="plugin-source-editor-wrap"] .ce-hl')
    await expect(ed).toHaveValue(/export const manifest/)
    await expect(hl.locator('.h-k').first()).toHaveText('export')
    await expect(hl.locator('.h-s').first()).toBeVisible()

    const TRICKY = `export const manifest = { id: "e2e-logger", name: "E2E Logger", version: "1.0.0", search: { name: "L", prefix: "l" } }
// line comment <b id="pwn-comment">x</b>
/* block <i>x</i> */
const re = /<img src=x onerror="window.__pwn=1">/gi
const s = "<img src=x onerror=window.__pwn=2>"
const t = \`a \${1 + 2} <u>b</u> \${ \`n\${3}\` }\`
const n = 0x1F + 1_000.5 + (true ? null : undefined) / 2
export async function search(ctx) { return [] }
`
    await ed.fill(TRICKY)
    const cls = async (c: string) => (await hl.locator('.h-' + c).allTextContents()).join('|')
    expect(await cls('c')).toContain('// line comment <b id="pwn-comment">x</b>')
    expect(await cls('c')).toContain('/* block <i>x</i> */')
    expect(await cls('r')).toBe('/<img src=x onerror="window.__pwn=1">/gi')
    expect(await cls('s')).toContain('"<img src=x onerror=window.__pwn=2>"')
    expect(await cls('s')).toContain('`a ') // template text is a string ...
    expect((await cls('e')).split('|').join('')).toContain('${') // ... and `${ }` are their own token
    expect(await cls('n')).toContain('0x1F')
    expect(await cls('n')).toContain('1_000.5')
    expect(await cls('l')).toBe('true|null|undefined')
    expect(await cls('f')).toContain('search')
    expect(await cls('p')).toContain('id') // object keys
    // text only: no element was created from the plugin's HTML-looking content
    await expect(hl.locator('img, b, i, u')).toHaveCount(0)
    expect(await page.evaluate(() => (window as any).__pwn)).toBeUndefined()
    // the overlay shows exactly the textarea's text
    expect(await hl.evaluate((el) => el.textContent)).toBe(TRICKY)
    // dividing is not a regex: `/ 2` stays punctuation
    expect(await cls('r')).not.toContain('2')

    // typing, Tab and Enter auto-indent keep the two in step
    await ed.press('Control+End')
    await ed.pressSequentially('if (x) {')
    await ed.press('Enter')
    await ed.press('Tab')
    await ed.pressSequentially('y = 1')
    const v = await ed.inputValue()
    expect(v.endsWith('if (x) {\n  y = 1')).toBe(true) // Enter kept the (empty) indent, Tab added two spaces
    await expect.poll(() => hl.evaluate((el) => el.textContent)).toBe(v)

    // alignment: a tall file with one very long line, scrolled both ways
    const long = Array.from({ length: 160 }, (_, i) => (i === 100 ? 'const long = "' + 'abcdefghij'.repeat(80) + '"' : `const line${i} = ${i} // c`)).join('\n')
    await ed.fill(long)
    await expect.poll(() => hl.evaluate((el) => el.textContent)).toBe(long)
    const probe = () =>
      page.evaluate(() => {
        const ta = document.querySelector<HTMLTextAreaElement>('[data-testid="plugin-source-editor"]')!
        const pre = document.querySelector<HTMLElement>('[data-testid="plugin-source-editor-wrap"] .ce-hl')!
        const r = ta.getBoundingClientRect()
        const p = pre.getBoundingClientRect()
        const first = pre.querySelector('code')!.firstChild as HTMLElement
        const rg = document.createRange()
        rg.setStart(first.firstChild!, 0)
        rg.setEnd(first.firstChild!, 1)
        return { dx: p.left - (r.left - ta.scrollLeft), dy: p.top - (r.top - ta.scrollTop), gx: rg.getBoundingClientRect().left - (r.left + 12 - ta.scrollLeft), sl: ta.scrollLeft, st: ta.scrollTop, dw: pre.offsetWidth - ta.scrollWidth, dh: pre.offsetHeight - ta.scrollHeight }
      })
    // the layer follows the textarea's scroll offsets on the scroll event (next frame): poll until it has caught up
    const aligned = async () => {
      await expect.poll(async () => { const o = await probe(); return Math.max(Math.abs(o.dx), Math.abs(o.dy), Math.abs(o.gx)) }).toBeLessThan(0.6)
      return probe()
    }
    await aligned()
    await ed.evaluate((el: HTMLTextAreaElement) => ((el.scrollTop = 1700), (el.scrollLeft = 900)))
    const sc = await aligned()
    expect(sc.st).toBeGreaterThan(1000)
    expect(sc.sl).toBeGreaterThan(300)
    // same text extent in both layers, i.e. the same glyph advance and line height
    // (the pre box includes its right / bottom padding, which the textarea's scroll extent leaves out: 14px / 10px or 0)
    expect(Math.min(Math.abs(sc.dw), Math.abs(sc.dw - 14))).toBeLessThanOrEqual(1)
    expect(Math.min(Math.abs(sc.dh), Math.abs(sc.dh - 10))).toBeLessThanOrEqual(1)
    // the gutter follows too
    await expect.poll(() => page.locator('[data-testid="plugin-source-editor-wrap"] .ce-gut').evaluate((el) => el.scrollTop)).toBeGreaterThan(1000)
  })

  test('REST market endpoints: validation, and a well-formed answer for any GitHub outcome', async ({ api }) => {
    await expect(api.call('GET', '/api/plugins/market?sort=bogus')).rejects.toThrow(/400/)
    await expect(api.call('GET', '/api/plugins/market?page=abc')).rejects.toThrow(/400/)
    await expect(api.call('GET', '/api/plugins/market/repo?repo=not-a-repo')).rejects.toThrow(/400/)
    await expect(api.call('GET', '/api/plugins/market/repo?repo=../etc')).rejects.toThrow(/400/)
    await expect(api.call('GET', '/api/plugins/market/repo')).rejects.toThrow(/400/)
    // GitHub failures are 200 + error (not 5xx); successes have the documented shape. No values are asserted.
    const list = await api.call<{ items: unknown[]; total: number; cached_at: number; error?: string }>('GET', '/api/plugins/market?sort=updated&q=%20%20e2e%20')
    expect(Array.isArray(list.items)).toBe(true)
    expect(typeof list.total).toBe('number')
    expect(typeof list.cached_at).toBe('number')
    expect(list.cached_at).toBeGreaterThan(1.5e12) // Unix milliseconds
    if (list.error) {
      expect(list.items).toEqual([])
      expect(list.total).toBe(0)
    }
    const scan = await api.call<{ repo: string; plugins: unknown[]; error?: string }>('GET', '/api/plugins/market/repo?repo=genkan-e2e/does-not-exist-9f3a1c')
    expect(scan.repo).toBe('genkan-e2e/does-not-exist-9f3a1c')
    expect(Array.isArray(scan.plugins)).toBe(true)
    // an unknown repo cannot have plugins; if GitHub answered at all, it says why
    expect(scan.plugins).toEqual([])
    expect(typeof (scan.error ?? '')).toBe('string')
  })

  test('REST: POST /api/plugins/market/cache/clear answers {ok:true} (and is idempotent)', async ({ api }) => {
    expect(await api.call('POST', '/api/plugins/market/cache/clear')).toEqual({ ok: true })
    expect(await api.call('POST', '/api/plugins/market/cache/clear')).toEqual({ ok: true })
  })
})

// phone width: the test panel stays on top and compact, the editor keeps a usable height, nothing overflows sideways
test.describe('plugin manager on a phone (390px)', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 })
  test.afterAll(async ({ admin }) => {
    await new Api(admin).reset()
  })

  test('source editor: the always-visible test panel is above the source; optional fields fold; the editor is still tall', async ({ page, api, ws }) => {
    void ws
    await install(api, 'countdown')
    await openApp(page)
    await openSettings(page, 'plugins')
    await row(page, 'countdown').getByTestId('plugin-source-btn').click()
    const modal = page.getByTestId('plugin-source-modal')
    await expect(page.getByTestId('plugin-source-editor')).toHaveValue(/export const manifest/)
    const panel = page.getByTestId('plugin-test-panel')
    await expect(panel).toBeVisible()
    await page.waitForTimeout(500) // the sheet slides in
    const g = await panelGeometry(page)
    expect(g.panelBottom).toBeLessThanOrEqual(g.editorTop + 1)
    expect(g.panelH, 'compact').toBeLessThan(190)
    expect(g.editorH).toBeGreaterThanOrEqual(230)
    // widget plugin: the instance / settings fields are folded away at phone width, one tap opens them
    await expect(page.getByTestId('plugin-test-hook')).toBeVisible()
    await expect(page.getByTestId('plugin-test-config')).toBeHidden()
    await panel.locator('summary').click()
    await expect(page.getByTestId('plugin-test-config')).toBeVisible()
    // run works from here too
    await page.getByTestId('plugin-test-run').click()
    await expect(page.getByTestId('plugin-test-result')).toHaveAttribute('data-ok', '1')
    // no sideways overflow in the dialog or the page
    const over = await page.evaluate(() => {
      const m = document.querySelector('[data-testid="plugin-source-modal"] .modal-body')!
      return { body: m.scrollWidth - m.clientWidth, doc: document.documentElement.scrollWidth - window.innerWidth }
    })
    expect(over.body).toBeLessThanOrEqual(0)
    expect(over.doc).toBeLessThanOrEqual(0)
    await expect(modal).toBeVisible()
  })

  test('catalog source dialog fits the phone', async ({ page }) => {
    await openApp(page)
    await openSettings(page, 'plugins')
    await page.getByTestId('plugin-empty-add').click()
    const item = page.locator('[data-testid="plugin-catalog-item"][data-plugin-id="countdown"]')
    await item.getByTestId('plugin-catalog-source').click()
    const dlg = page.getByTestId('plugin-catalog-source-modal')
    await expect(dlg.getByTestId('plugin-catalog-source-code')).toHaveValue(/export const manifest/)
    const over = await page.evaluate(() => {
      const m = document.querySelector('[data-testid="plugin-catalog-source-modal"] .modal-body')!
      return { body: m.scrollWidth - m.clientWidth, doc: document.documentElement.scrollWidth - window.innerWidth }
    })
    expect(over.body).toBeLessThanOrEqual(0)
    expect(over.doc).toBeLessThanOrEqual(0)
  })
})
