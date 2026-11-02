import { expect, openApp, openSettings, origin, test, toggleEdit } from '../helpers'

test.describe('AI tab and top-right AI button', () => {
  test('the AI button (between the edit toggle and the menu) opens Settings straight on the AI tab', async ({ page }) => {
    await openApp(page)
    const ai = page.getByTestId('ai-btn')
    await expect(ai).toBeVisible()
    await expect(ai).toHaveAttribute('aria-label', 'AI')
    await expect(ai.locator('svg[data-icon="bot"]')).toHaveCount(1)
    // order in the top-right group: edit toggle, AI, main menu
    const x = async (id: string) => (await page.getByTestId(id).boundingBox())!.x
    expect(await x('edit-toggle')).toBeLessThan(await x('ai-btn'))
    expect(await x('ai-btn')).toBeLessThan(await x('menu-btn'))

    await ai.click()
    await expect(page.getByTestId('settings-modal')).toBeVisible()
    await expect(page.getByTestId('settings-tab-ai')).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('settings-panel-ai')).toBeVisible()
    await expect(page.getByTestId('token-card')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('settings-modal')).toHaveCount(0)
    // works while editing too
    await toggleEdit(page)
    await ai.click()
    await expect(page.getByTestId('settings-panel-ai')).toBeVisible()
  })

  test('the tab is called "AI" (no "AI / MCP" anywhere), in the documented order, in both languages', async ({ page }) => {
    await openApp(page)
    await openSettings(page)
    const ids = await page.locator('[data-testid^="settings-tab-"]').evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')!.replace('settings-tab-', '')))
    expect(ids).toEqual(['general', 'appearance', 'content', 'ai', 'security', 'data', 'about'])
    await expect(page.getByTestId('settings-tab-mcp')).toHaveCount(0)
    await expect(page.getByTestId('settings-tab-ai')).toHaveText('AI')
    await page.getByTestId('settings-tab-ai').click()
    await expect(page.getByTestId('settings-modal')).not.toContainText('AI / MCP')
    await expect(page.getByTestId('settings-modal')).not.toContainText('AI/MCP')
    await page.getByTestId('modal-close').click()

    await page.evaluate(() => localStorage.setItem('genkan.lang', 'zh'))
    await page.reload()
    await expect(page.getByTestId('ai-btn')).toBeVisible()
    await openSettings(page)
    await expect(page.getByTestId('settings-tab-ai')).toHaveText('AI')
    await expect(page.getByTestId('settings-tab-content')).toHaveText('内容')
    await page.getByTestId('settings-tab-ai').click()
    await expect(page.getByTestId('mcp-warning')).toContainText('Token 等同管理员权限')
    await expect(page.getByTestId('connect-skill')).toContainText('Skill（推荐）')
    await expect(page.getByTestId('connect-mcp')).toContainText('其他 MCP 客户端')
    await expect(page.getByTestId('settings-modal')).not.toContainText('AI / MCP')
  })

  test('Connect panes: only the selected one is rendered; code blocks do not wrap; copy buttons give feedback', async ({ page }) => {
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: origin() })
    await openApp(page)
    await openSettings(page, 'ai')
    const tabs = page.getByTestId('connect-tabs')
    await expect(tabs).toHaveAttribute('role', 'tablist')
    await expect(page.getByTestId('connect-skill')).toHaveAttribute('aria-selected', 'true')

    // Skill (default): steps + a prominent download button
    await expect(page.getByTestId('pane-skill')).toBeVisible()
    await expect(page.getByTestId('pane-skill').locator('ol > li')).toHaveCount(2)
    await expect(page.getByTestId('pane-skill')).toContainText('~/.claude/skills/')
    await expect(page.getByTestId('skill-download')).toHaveAttribute('href', '/api/skill')
    await expect(page.getByTestId('pane-claude')).toHaveCount(0)
    await expect(page.getByTestId('pane-mcp')).toHaveCount(0)

    // Claude Code
    await page.getByTestId('connect-claude').click()
    await expect(page.getByTestId('pane-claude')).toBeVisible()
    await expect(page.getByTestId('pane-skill')).toHaveCount(0)
    const cmd = page.getByTestId('mcp-command')
    await expect(cmd).toContainText('claude mcp add --transport http genkan')
    expect(await cmd.evaluate((e) => getComputedStyle(e).whiteSpace)).toBe('pre')
    // the copy button is overlaid in the top-right corner of the code block
    const box = (await cmd.locator('xpath=..').boundingBox())!
    const cb = (await page.getByTestId('copy-mcp-command').boundingBox())!
    expect(cb.x + cb.width).toBeGreaterThan(box.x + box.width - 40)
    expect(cb.y).toBeLessThan(box.y + 40)
    await page.getByTestId('copy-mcp-command').click()
    await expect(page.getByTestId('copy-mcp-command').locator('svg[data-icon="check"]')).toBeVisible()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toContain('claude mcp add --transport http genkan')
    await expect(page.getByTestId('copy-mcp-command').locator('svg[data-icon="copy"]')).toBeVisible() // back after ~1.5 s

    // Other MCP clients
    await page.getByTestId('connect-mcp').click()
    await expect(page.getByTestId('pane-mcp')).toBeVisible()
    await expect(page.getByTestId('mcp-command')).toHaveCount(0)
    await expect(page.getByTestId('mcp-json')).toContainText('"mcpServers"')
    await page.getByTestId('copy-mcp-json').click()
    expect(JSON.parse(await page.evaluate(() => navigator.clipboard.readText())).mcpServers.genkan.url).toBe(`${origin()}/mcp`)

    // keyboard: arrow keys move between the segmented tabs
    await page.getByTestId('connect-mcp').focus()
    await page.keyboard.press('ArrowRight')
    await expect(page.getByTestId('connect-skill')).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('pane-skill')).toBeVisible()
  })

  test('About shows the server build version from the API', async ({ page, api }) => {
    const bs = await api.bootstrap()
    expect(bs.server_version).toBeTruthy()
    await openApp(page)
    await openSettings(page)
    await page.getByTestId('settings-tab-about').click()
    await expect(page.getByTestId('about-version')).toHaveText(bs.server_version)
  })

  test('exactly one warning line about the token, no big banner', async ({ page }) => {
    await openApp(page)
    await openSettings(page, 'ai')
    await expect(page.getByTestId('mcp-warning')).toHaveCount(1)
    const h = (await page.getByTestId('mcp-warning').boundingBox())!.height
    expect(h).toBeLessThan(50) // a compact line, not a banner
    // the token is masked on first paint and the reveal toggle is an icon-only button
    await expect(page.getByTestId('token-value')).toHaveAttribute('data-masked', '1')
    await expect(page.getByTestId('token-toggle')).toHaveText('')
  })
})
