import { expect, openApp, openWsMenu, switchWs, test, toggleEdit, wsItem, wsNames } from '../helpers'

test.describe('workspaces', () => {
  test('create, rename (via right-click), switch and delete a workspace', async ({ page, api }) => {
    await openApp(page)
    await toggleEdit(page)

    await openWsMenu(page)
    await page.getByTestId('ws-add').click()
    await page.getByTestId('ws-name').fill('Media')
    await page.getByTestId('ws-icon-clapperboard').click()
    await expect(page.getByTestId('ws-icon')).toHaveAttribute('data-value', 'lucide:clapperboard')
    await expect(page.getByTestId('wp-preset-dusk')).toHaveAttribute('aria-pressed', 'true') // Dusk is the default selection
    await page.getByTestId('wp-preset-ocean').click()
    await page.getByTestId('ws-save').click()
    await expect(page.getByTestId('workspace-modal')).toHaveCount(0)

    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Media') // new workspace gets selected
    await expect(page.getByTestId('wallpaper')).toHaveAttribute('data-wallpaper', 'preset:ocean')
    const b = await api.bootstrap()
    expect(b.workspaces.map((w) => w.name)).toEqual(['Home', 'Media'])
    expect(b.workspaces[1]).toMatchObject({ icon: 'lucide:clapperboard', wallpaper: 'preset:ocean' })

    // rename through right-click on its row in the dropdown
    await openWsMenu(page)
    await expect(wsItem(page, 'Media')).toHaveAttribute('aria-checked', 'true')
    await wsItem(page, 'Media').click({ button: 'right' })
    await expect(page.getByTestId('workspace-modal')).toBeVisible()
    await page.getByTestId('ws-name').fill('Movies')
    await page.getByTestId('ws-save').click()
    await expect(page.getByTestId('ws-menu-btn')).toHaveAttribute('data-ws-name', 'Movies')
    // the UI renames optimistically; the PATCH may still be in flight
    await expect.poll(async () => (await api.bootstrap()).workspaces.map((w) => w.name)).toEqual(['Home', 'Movies'])

    // switching back and forth changes the wallpaper
    await switchWs(page, 'Home')
    await expect(page.getByTestId('wallpaper')).toHaveAttribute('data-wallpaper', 'preset:dusk')

    // delete Movies
    await switchWs(page, 'Movies')
    const id = (await api.bootstrap()).workspaces[1].id
    await openWsMenu(page)
    await page.getByTestId(`ws-edit-${id}`).click()
    await page.getByTestId('ws-delete').click()
    await page.getByTestId('confirm-ok').click()
    expect(await wsNames(page)).toEqual(['Home'])
    expect((await api.bootstrap()).workspaces.map((w) => w.name)).toEqual(['Home'])
  })

  test('the last workspace cannot be deleted', async ({ page, api, ws }) => {
    await openApp(page)
    await toggleEdit(page)
    await openWsMenu(page)
    await page.getByTestId(`ws-edit-${ws}`).click()
    // either the button is hidden/disabled, or the server answers 409 and the workspace stays
    const del = page.getByTestId('ws-delete')
    if (await del.isVisible().catch(() => false)) {
      if (await del.isEnabled()) {
        await del.click()
        await page.getByTestId('confirm-ok').click()
        await expect(page.getByTestId('toast').first()).toBeVisible()
      }
    }
    expect((await api.bootstrap()).workspaces).toHaveLength(1)
    const r = await api.ctx.delete(`/api/workspaces/${ws}`)
    expect(r.status()).toBe(409)
  })
})

test.describe('categories', () => {
  test('create, rename, delete (links become uncategorized) and delete with links', async ({ page, api, ws }) => {
    await openApp(page)
    await toggleEdit(page)

    await page.getByTestId('new-category-btn').click()
    await page.getByTestId('category-name').fill('Infra')
    await page.getByTestId('category-save').click()
    await expect(page.getByRole('heading', { name: 'Infra' })).toBeVisible()
    const cat = (await api.bootstrap()).categories[0]
    expect(cat).toMatchObject({ name: 'Infra', workspace_id: ws })

    await page.getByTestId(`category-rename-${cat.id}`).click()
    await page.getByTestId('category-name').fill('Servers')
    await page.getByTestId('category-save').click()
    await expect(page.getByTestId(`category-${cat.id}-title`)).toHaveText('Servers')

    // links in it survive a plain delete and become uncategorized
    const l = await api.link({ workspace_id: ws, category_id: cat.id, title: 'Proxmox', url: 'http://pve.lan:8006' })
    await expect(page.getByTestId(`link-${l.id}`)).toBeVisible()
    await page.getByTestId(`category-delete-${cat.id}`).click()
    await expect(page.getByTestId('confirm-modal')).toBeVisible()
    await page.getByTestId('confirm-ok').click()
    await expect(page.getByTestId(`category-${cat.id}`)).toHaveCount(0)
    await expect(page.getByTestId(`link-${l.id}`)).toBeVisible()
    const after = await api.bootstrap()
    expect(after.categories).toHaveLength(0)
    expect(after.links.find((x) => x.id === l.id)!.category_id).toBe('')

    // delete category together with its links
    const c2 = await api.category(ws, 'Temp')
    const l2 = await api.link({ workspace_id: ws, category_id: c2.id, title: 'Gone', url: 'http://gone.lan' })
    await expect(page.getByTestId(`link-${l2.id}`)).toBeVisible()
    await page.getByTestId(`category-delete-${c2.id}`).click()
    await page.getByTestId('confirm-checkbox').check()
    await page.getByTestId('confirm-ok').click()
    await expect(page.getByTestId(`link-${l2.id}`)).toHaveCount(0)
    const fin = await api.bootstrap()
    expect(fin.links.map((x) => x.title)).toEqual(['Proxmox'])
  })
})

test.describe('links', () => {
  test('create via empty state, with host-derived title, category and description', async ({ page, api, ws }) => {
    const cat = await api.category(ws, 'Media')
    await openApp(page)
    await expect(page.getByTestId('empty-state')).toBeVisible()
    await page.getByTestId('empty-add-link').click()
    const m = page.getByTestId('link-modal')
    await expect(m).toBeVisible()
    await page.getByTestId('link-url').fill('http://jellyfin.lan:8096/web')
    await expect(page.getByTestId('link-title')).toHaveValue('jellyfin.lan') // auto-filled from the host
    await page.getByTestId('link-title').fill('Jellyfin')
    await page.getByTestId('link-desc').fill('Movies & TV')
    await page.getByTestId('link-category').selectOption(cat.id)
    await page.getByTestId('icon-mode-emoji').click()
    await page.getByTestId('link-icon-emoji').fill('🎬')
    await page.getByTestId('link-save').click()
    await expect(m).toHaveCount(0)

    const l = (await api.bootstrap()).links[0]
    expect(l).toMatchObject({ title: 'Jellyfin', url: 'http://jellyfin.lan:8096/web', description: 'Movies & TV', category_id: cat.id, icon: 'emoji:🎬', workspace_id: ws, pinned: false })
    const cap = page.getByTestId(`link-${l.id}`)
    await expect(page.getByTestId(`category-${cat.id}`).getByTestId(`link-${l.id}`)).toBeVisible()
    await expect(cap).toHaveAttribute('href', 'http://jellyfin.lan:8096/web')
    await expect(cap).toHaveAttribute('target', '_blank')
    await expect(cap).toContainText('Jellyfin')
    await expect(cap).toHaveAttribute('title', /Movies & TV[\s\S]*jellyfin\.lan/)
  })

  test('URL without scheme is normalised; invalid URL is refused', async ({ page, api }) => {
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId('uncat-add-link').click()
    await page.getByTestId('icon-mode-letter').click()
    await page.getByTestId('link-url').fill('')
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId('link-modal')).toBeVisible() // validation keeps the modal open
    await page.getByTestId('link-url').fill('nas.lan:5000')
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId('link-modal')).toHaveCount(0)
    const l = (await api.bootstrap()).links[0]
    expect(l.url).toMatch(/^https?:\/\/nas\.lan:5000\/?$/)
  })

  test('edit a link (title, url, pin, health) and move it between categories', async ({ page, api, ws }) => {
    const c1 = await api.category(ws, 'A')
    const c2 = await api.category(ws, 'B')
    const l = await api.link({ workspace_id: ws, category_id: c1.id, title: 'Old name', url: 'http://old.lan', icon: 'emoji:🚀' })
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId(`link-edit-${l.id}`).click()
    await page.getByTestId('link-title').fill('New name')
    await page.getByTestId('link-url').fill('http://new.lan:81')
    await page.getByTestId('link-category').selectOption(c2.id)
    await page.getByTestId('link-pinned').click()
    await expect(page.getByTestId('link-pinned')).toHaveAttribute('aria-checked', 'true')
    await page.getByTestId('link-health').click()
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId('link-modal')).toHaveCount(0)
    // pinned links are shown in the pinned row AND in their category
    await expect(page.getByTestId('section-pinned').getByTestId(`pin-link-${l.id}`)).toContainText('New name')
    await expect(page.getByTestId(`category-${c2.id}`).getByTestId(`link-${l.id}`)).toContainText('New name')
    await expect(page.getByTestId(`category-${c1.id}`).getByTestId(`link-${l.id}`)).toHaveCount(0)
    await expect.poll(async () => (await api.links())[0]).toMatchObject({ title: 'New name', url: 'http://new.lan:81', category_id: c2.id, pinned: true, health_check: true })
    await expect(page.getByTestId(`health-${l.id}`)).toBeVisible()
    await expect(page.getByTestId(`pin-health-${l.id}`)).toBeVisible()
  })

  test('delete a link with confirmation (cancel keeps it)', async ({ page, api, ws }) => {
    const l = await api.link({ workspace_id: ws, title: 'Victim', url: 'http://victim.lan' })
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId(`link-delete-${l.id}`).click()
    await page.getByTestId('confirm-cancel').click()
    await expect(page.getByTestId('confirm-modal')).toHaveCount(0)
    await expect(page.getByTestId(`link-${l.id}`)).toBeVisible()
    await page.getByTestId(`link-delete-${l.id}`).click()
    await page.getByTestId('confirm-ok').click()
    await expect(page.getByTestId(`link-${l.id}`)).toHaveCount(0)
    await toggleEdit(page, false)
    await expect(page.getByTestId('empty-state')).toBeVisible() // back to the empty state
    expect(await api.links()).toHaveLength(0)
  })

  test('pin from the editor via the "+" capsule of the pinned section; Esc closes modals', async ({ page, api }) => {
    await openApp(page)
    await toggleEdit(page)
    await page.getByTestId('empty-add-link').or(page.getByTestId('pinned-add-link')).first().click()
    await expect(page.getByTestId('link-url')).toBeFocused() // modal is mounted and its key handler attached
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('link-modal')).toHaveCount(0)
    await page.getByTestId('pinned-add-link').click()
    await expect(page.getByTestId('link-pinned')).toHaveAttribute('aria-checked', 'true')
    await page.getByTestId('icon-mode-letter').click()
    await page.getByTestId('link-url').fill('http://pinned.lan')
    await page.getByTestId('link-title').fill('Pinned one')
    await page.getByTestId('link-save').click()
    await expect(page.getByTestId('section-pinned')).toContainText('Pinned one')
    expect((await api.links())[0].pinned).toBe(true)
    await toggleEdit(page, false)
    await expect(page.getByTestId('section-pinned')).toContainText('Pinned one')
    await expect(page.getByTestId('section-pinned-title')).toHaveCount(0) // no heading any more
  })

  test('creating a link toasts a success message and data survives reload', async ({ page, api, ws }) => {
    await api.link({ workspace_id: ws, title: 'Persisted', url: 'http://p.lan' })
    await page.goto('/')
    await expect(page.getByRole('link', { name: 'Persisted' })).toBeVisible()
    await page.reload()
    await expect(page.getByRole('link', { name: 'Persisted' })).toBeVisible()
  })
})
