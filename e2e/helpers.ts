import { deflateSync, inflateRawSync } from 'node:zlib'
import { test as base, expect, request as pwRequest, type APIRequestContext, type Locator, type Page } from '@playwright/test'

export { expect }

export const PASSWORD = 'e2e-password-1'
export const CSRF = { 'X-Requested-With': 'genkan' }

/** Origin the browser / test runner talks to. */
export const origin = () => process.env.GENKAN_E2E_URL!
/** Origin the *server* can use to reach itself (favicon fetch, health checks). */
export const selfOrigin = () => process.env.GENKAN_E2E_SELF!

export interface Link {
  id: string
  workspace_id: string
  category_id: string
  title: string
  url: string
  description: string
  icon: string
  pinned: boolean
  sort: number
  pin_sort: number
  clicks: number
  last_used_at: string | null
  health_check: boolean
}
export interface Workspace { id: string; name: string; icon: string; wallpaper: string; sort: number }
export interface Category { id: string; workspace_id: string; name: string; sort: number }
export interface Boot {
  auth: { setup_required: boolean; role: string }
  settings: Record<string, any>
  workspaces: Workspace[]
  categories: Category[]
  links: Link[]
  health: Record<string, { status: string }>
  version: number
  server_version?: string
}

/** Thin wrapper around an authenticated (cookie + CSRF header) request context. */
export class Api {
  constructor(public ctx: APIRequestContext) {}

  async call<T = any>(method: string, path: string, data?: unknown): Promise<T> {
    const r = await this.ctx.fetch(path, { method, data: data as any })
    const body = await r.text()
    if (!r.ok()) throw new Error(`${method} ${path} -> ${r.status()} ${body}`)
    return (body ? JSON.parse(body) : undefined) as T
  }
  bootstrap() { return this.call<Boot>('GET', '/api/bootstrap') }
  /** The instance's single API token. */
  async token() { return (await this.call<{ token: string }>('GET', '/api/token')).token }
  settings(patch: Record<string, unknown>) { return this.call('PATCH', '/api/settings', patch) }
  workspace(b: { name: string; icon?: string; wallpaper?: string }) { return this.call<Workspace>('POST', '/api/workspaces', b) }
  category(workspace_id: string, name: string) { return this.call<Category>('POST', '/api/categories', { workspace_id, name }) }
  link(b: { workspace_id: string; title: string; url: string; category_id?: string; description?: string; icon?: string; pinned?: boolean; health_check?: boolean; fetch_icon?: boolean }) {
    return this.call<Link>('POST', '/api/links', { fetch_icon: false, ...b })
  }
  async click(id: string, n = 1) {
    for (let i = 0; i < n; i++) await this.ctx.post(`/api/links/${id}/click`)
  }
  async links() { return (await this.bootstrap()).links }

  /**
   * Replace all data with a deterministic dataset (ids are the given ones). Links are listed per category in
   * the given order; `sort` follows list order.
   */
  async seed(d: {
    settings?: Record<string, unknown>
    workspaces?: { id: string; name: string; icon?: string; wallpaper?: string }[]
    categories?: { id: string; workspace_id: string; name: string }[]
    links?: any[]
  }) {
    const workspaces = d.workspaces ?? [{ id: 'ws1', name: 'Home', icon: 'lucide:house' }]
    await this.call('POST', '/api/import', {
      mode: 'replace',
      data: {
        schema: 1,
        settings: d.settings ?? {},
        workspaces: workspaces.map((w, i) => ({ icon: '', wallpaper: 'preset:dusk', ...w, sort: i })),
        categories: (d.categories ?? []).map((c, i) => ({ ...c, sort: i })),
        links: (d.links ?? []).map((l: any, i: number) => ({ category_id: '', description: '', icon: '', pinned: false, clicks: 0, last_used_at: null, health_check: false, ...l, sort: l.sort ?? i, pin_sort: l.pin_sort ?? i })),
      },
    })
    return this.bootstrap()
  }

  /** Replace all data with a single empty "Home" workspace and default settings (the API token is kept). */
  async reset(): Promise<{ ws: string }> {
    await this.call('POST', '/api/import', {
      mode: 'replace',
      data: { schema: 1, settings: {}, workspaces: [{ id: '', name: 'Home', icon: 'lucide:house', wallpaper: 'preset:dusk', sort: 0 }], categories: [], links: [] },
    })
    // the wallpaper gallery is part of the baseline too
    for (const f of await this.call<{ value: string }[]>('GET', '/api/wallpapers')) await this.call('DELETE', `/api/wallpapers/${f.value.slice('upload:'.length)}`)
    const b = await this.bootstrap()
    return { ws: b.workspaces[0].id }
  }
}

/** Log in (or perform first-run setup) and return an authenticated API context. */
export async function adminContext(): Promise<APIRequestContext> {
  const anon = await pwRequest.newContext({ baseURL: origin(), extraHTTPHeaders: CSRF })
  const boot = await (await anon.get('/api/bootstrap')).json()
  const r = await anon.post(boot.auth.setup_required ? '/api/auth/setup' : '/api/auth/login', { data: { password: PASSWORD } })
  if (!r.ok()) throw new Error(`admin login failed: ${r.status()} ${await r.text()}`)
  return anon
}

type Fixtures = { api: Api; ws: string }
type WorkerFixtures = { admin: APIRequestContext }

export const test = base.extend<Fixtures, WorkerFixtures>({
  baseURL: async ({}, use) => use(origin()),
  admin: [
    async ({}, use) => {
      const ctx = await adminContext()
      await use(ctx)
      await ctx.dispose()
    },
    { scope: 'worker' },
  ],
  // Fresh baseline for every test.
  api: async ({ admin }, use) => {
    const api = new Api(admin)
    await api.reset()
    await use(api)
  },
  ws: async ({ api }, use) => use((await api.bootstrap()).workspaces[0].id),
  // Every test page starts logged in as admin (tests that need anonymous browsers create their own context).
  context: async ({ context, admin, api }, use) => {
    void api
    const st = await admin.storageState()
    await context.addCookies(st.cookies)
    await use(context)
  },
})

/** Open the app and wait until the board / shell is rendered. */
export async function openApp(page: Page, path = '/') {
  await page.goto(path)
  await expect(page.getByTestId('ws-menu-btn')).toBeVisible()
}

export async function toggleEdit(page: Page, on = true) {
  const b = page.getByTestId('edit-toggle')
  if ((await b.getAttribute('aria-pressed')) !== String(on)) await b.click()
  await expect(b).toHaveAttribute('aria-pressed', String(on))
}

export const linkCap = (page: Page, id: string) => page.getByTestId(`link-${id}`)
/** The pinned-row copy of a pinned link. */
export const pinCap = (page: Page, id: string) => page.getByTestId(`pin-link-${id}`)

/** Titles of the link capsules inside a section, in DOM order. */
export async function sectionTitles(sec: Locator): Promise<string[]> {
  return sec.locator('.cap:not(.cap-add) .cap-title').allTextContents()
}

/**
 * Drag one element onto another with real pointer events.
 * `where` picks the drop point inside the target box.
 */
export async function drag(page: Page, from: Locator, to: Locator, where: 'before' | 'after' | 'center' = 'center') {
  const a = (await from.boundingBox())!
  const b = (await to.boundingBox())!
  const sx = a.x + a.width / 2
  const sy = a.y + a.height / 2
  const tx = where === 'before' ? b.x + 4 : where === 'after' ? b.x + b.width - 4 : b.x + b.width / 2
  const ty = b.y + b.height / 2
  await page.mouse.move(sx, sy)
  await page.mouse.down()
  await page.mouse.move(sx + 12, sy + 6, { steps: 3 })
  await page.mouse.move(tx, ty, { steps: 14 })
  await page.mouse.up()
}

/** Wait for the `change` SSE round-trip to settle: the board reflects server state. */
export async function waitServerVersion(api: Api, atLeast: number) {
  await expect.poll(async () => (await api.bootstrap()).version).toBeGreaterThanOrEqual(atLeast)
}

// ---- header menus (workspace dropdown top-left, main menu top-right) ----

/** Open a dropdown by its trigger button (no-op when already open). */
async function openMenu(page: Page, btn: string, menu: string) {
  const b = page.getByTestId(btn)
  // retried: a click right after a drag can be swallowed by the drag-end click guard
  await expect(async () => {
    if ((await b.getAttribute('aria-expanded')) !== 'true') await b.click()
    await expect(page.getByTestId(menu)).toBeVisible({ timeout: 1500 })
  }).toPass({ timeout: 7000 })
}
export const openWsMenu = (page: Page) => openMenu(page, 'ws-menu-btn', 'ws-menu')
export const openMainMenu = (page: Page) => openMenu(page, 'menu-btn', 'main-menu')

export async function closeMenus(page: Page) {
  for (const m of ['ws-menu', 'main-menu']) {
    if (await page.getByTestId(m).isVisible().catch(() => false)) await page.keyboard.press('Escape')
  }
}

/** Open the main menu and click one of its items (`settings-btn`, `logout-btn`, `login-btn`, `sort-recent`, ...). */
export async function mainMenuItem(page: Page, testid: string) {
  await openMainMenu(page)
  await page.getByTestId(testid).click()
}
export async function openSettings(page: Page, tab?: string) {
  await mainMenuItem(page, 'settings-btn')
  await expect(page.getByTestId('settings-modal')).toBeVisible()
  if (tab) await page.getByTestId(`settings-tab-${tab}`).click()
}
/** Assert the checked sort mode in the main menu (opens and closes it). */
export async function expectSort(page: Page, mode: 'frequency' | 'recent' | 'custom') {
  await openMainMenu(page)
  for (const m of ['frequency', 'recent', 'custom'])
    await expect(page.getByTestId(`sort-${m}`)).toHaveAttribute('aria-checked', String(m === mode))
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('main-menu')).toHaveCount(0)
}
export const setSort = (page: Page, mode: 'frequency' | 'recent' | 'custom') => mainMenuItem(page, `sort-${mode}`)

/** Locator for a workspace menu item by id or by name (the menu must be open). */
export const wsItem = (page: Page, idOrName: string) =>
  page.locator(`[data-testid="ws-item-${idOrName}"], [data-testid^="ws-item-"][data-ws-name="${idOrName}"]`).first()

/** Switch workspace through the dropdown. */
export async function switchWs(page: Page, idOrName: string) {
  await openWsMenu(page)
  await wsItem(page, idOrName).click()
  await expect(page.getByTestId('ws-menu')).toHaveCount(0)
}

/** Name of the workspace shown on the dropdown button. */
export const currentWsName = (page: Page) => page.getByTestId('ws-menu-btn').getAttribute('data-ws-name')

/** Names of all workspaces in menu order (opens and closes the menu). */
export async function wsNames(page: Page): Promise<string[]> {
  await openWsMenu(page)
  const names = await page.locator('[data-testid^="ws-item-"]').evaluateAll((els) => els.map((e) => e.getAttribute('data-ws-name')!))
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('ws-menu')).toHaveCount(0)
  return names
}

/** Names of all entries of a (non-zip64) zip archive. */
export function zipEntries(buf: Buffer): string[] {
  return centralDirectory(buf).map((e) => e.name)
}

/** Read one entry of a zip archive (stored or deflated) without extra dependencies. */
export function readZipEntry(buf: Buffer, name: string): string {
  const e = centralDirectory(buf).find((x) => x.name === name)
  if (!e) throw new Error(`zip entry ${name} not found`)
  const nameLen = buf.readUInt16LE(e.offset + 26)
  const extraLen = buf.readUInt16LE(e.offset + 28)
  const data = buf.subarray(e.offset + 30 + nameLen + extraLen, e.offset + 30 + nameLen + extraLen + e.csize)
  return (e.method === 8 ? inflateRawSync(data) : data).toString('utf8')
}

function centralDirectory(buf: Buffer) {
  let eocd = buf.length - 22
  while (eocd >= 0 && buf.readUInt32LE(eocd) !== 0x06054b50) eocd--
  if (eocd < 0) throw new Error('not a zip file')
  const count = buf.readUInt16LE(eocd + 10)
  let p = buf.readUInt32LE(eocd + 16)
  const out: { name: string; method: number; csize: number; offset: number }[] = []
  for (let i = 0; i < count; i++) {
    if (buf.readUInt32LE(p) !== 0x02014b50) throw new Error('bad central directory')
    const nl = buf.readUInt16LE(p + 28)
    out.push({
      method: buf.readUInt16LE(p + 10),
      csize: buf.readUInt32LE(p + 20),
      offset: buf.readUInt32LE(p + 42),
      name: buf.subarray(p + 46, p + 46 + nl).toString('utf8'),
    })
    p += 46 + nl + buf.readUInt16LE(p + 30) + buf.readUInt16LE(p + 32)
  }
  return out
}

// ---- touch ----

/** One real touch gesture through CDP (long-press = `hold` ms, `moveBy` px drags sideways first). */
export async function touchGesture(page: Page, x: number, y: number, o: { hold?: number; moveBy?: number } = {}) {
  const cdp = await page.context().newCDPSession(page)
  const pt = (px: number, py: number) => [{ x: px, y: py, id: 1 }]
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: pt(x, y) })
  if (o.moveBy) for (let i = 1; i <= 4; i++) await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: pt(x + (o.moveBy * i) / 4, y) })
  if (o.hold) await new Promise((r) => setTimeout(r, o.hold))
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  await cdp.detach()
}

// ---- generated test images ----

const crcTable = (() => {
  const t = new Uint32Array(256)
  for (let n = 0; n < 256; n++) {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    t[n] = c >>> 0
  }
  return t
})()
function crc32(b: Buffer) {
  let c = 0xffffffff
  for (const x of b) c = crcTable[(c ^ x) & 0xff] ^ (c >>> 8)
  return (c ^ 0xffffffff) >>> 0
}
function pngChunk(type: string, data: Buffer) {
  const len = Buffer.alloc(4)
  len.writeUInt32BE(data.length)
  const td = Buffer.concat([Buffer.from(type, 'ascii'), data])
  const crc = Buffer.alloc(4)
  crc.writeUInt32BE(crc32(td))
  return Buffer.concat([len, td, crc])
}
/** A valid solid-colour PNG; different colours give different bytes (and therefore different wallpaper file names). */
export function makePng(rgb: [number, number, number], w = 16, h = 9): Buffer {
  const row = Buffer.alloc(1 + w * 3)
  for (let x = 0; x < w; x++) row.set(rgb, 1 + x * 3)
  const raw = Buffer.concat(Array.from({ length: h }, () => row))
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(w, 0)
  ihdr.writeUInt32BE(h, 4)
  ihdr[8] = 8
  ihdr[9] = 2
  return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), pngChunk('IHDR', ihdr), pngChunk('IDAT', deflateSync(raw)), pngChunk('IEND', Buffer.alloc(0))])
}
