// Dev-only mock of the Genkan REST contract (docs/SPEC.md §6), for working on the frontend without the Go backend. Not shipped.
//   npm run mock (in web/)           -> http://localhost:8080 (serves web/dist + /api; admin password: secret123)
//   MOCK_OPEN=1  every request is admin;  MOCK_SETUP=1  start in first-run setup state
import http from 'node:http'
import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'
import os from 'node:os'
import { fileURLToPath } from 'node:url'
import { createPluginsMock } from './plugins.mjs'

const dist = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'dist')
const PORT = Number(process.env.PORT || 8080)
const upDir = fs.mkdtempSync(path.join(os.tmpdir(), 'genkan-mock-'))
fs.mkdirSync(path.join(upDir, 'wallpapers'), { recursive: true })
const ICONS = 'house layout-grid folder briefcase code terminal server database hard-drive cloud network router wifi globe shield lock key-round film clapperboard tv music headphones image camera gamepad-2 book-open graduation-cap newspaper shopping-cart wallet chart-line activity gauge cpu wrench settings bot sparkles heart star bookmark users message-circle mail calendar download flask-conical leaf coffee plane lightbulb rocket'.split(' ')
const okIcon = (v) => v === '' || v === undefined || (v.startsWith('lucide:') && ICONS.includes(v.slice(7)))
let password = process.env.MOCK_SETUP ? '' : 'secret123'
let guest = !!process.env.MOCK_GUEST
let version = 1
const sessions = new Set()
const id = () => Math.random().toString(36).slice(2, 14).padEnd(12, 'a')
const now = () => new Date().toISOString()
const ago = (m) => new Date(Date.now() - m * 60000).toISOString()

const settings = { title: 'Genkan', sort_mode: 'frequency', guest_access: false, search_engine: 'https://www.google.com/search?q=%s', open_in_new_tab: true, health_interval: 60, show_clock: true }
let workspaces = [], categories = [], links = []
const newToken = () => 'gk_' + Array.from({ length: 32 }, () => 'abcdefghijklmnopqrstuvwxyz234567'[Math.floor(Math.random() * 32)]).join('')
let apiToken = newToken()
const health = {}

function seed() {
  const W = (name, icon, wallpaper, sort) => ({ id: id(), name, icon, wallpaper, sort })
  const home = W('Home', 'lucide:house', 'preset:dusk', 0)
  const media = W('Media', 'lucide:clapperboard', 'preset:aurora', 1)
  const dev = W('Dev', 'lucide:code', 'preset:ocean', 2)
  const net = W('Network', 'lucide:network', 'preset:forest', 3)
  workspaces = [home, media, dev, net]
  const C = (ws, name, sort) => ({ id: id(), workspace_id: ws.id, name, sort })
  const cInfra = C(home, 'Infrastructure', 0), cApps = C(home, 'Apps', 1), cStream = C(media, 'Streaming', 0), cArr = C(media, 'Automation', 1)
  const cCode = C(dev, 'Code', 0), cOps = C(net, 'Gateway', 0)
  categories = [cInfra, cApps, cStream, cArr, cCode, cOps]
  let n = 0
  const L = (ws, cat, title, url, o = {}) => ({ id: id(), workspace_id: ws.id, category_id: cat ? cat.id : '', title, url, description: o.description || '', icon: o.icon || '', pinned: !!o.pinned, sort: n++, pin_sort: 0, clicks: o.clicks ?? 0, last_used_at: o.recent ? ago(o.recent) : null, health_check: !!o.health, created_at: now(), updated_at: now() })
  links = [
    L(home, cInfra, 'Proxmox', 'https://pve.lan:8006', { clicks: 42, recent: 30, health: true, pinned: true, description: 'Hypervisor' }),
    L(home, cInfra, 'TrueNAS', 'http://nas.lan', { clicks: 31, description: 'Storage & backups', recent: 200, health: true, pinned: true }),
    L(home, cInfra, 'Portainer', 'http://nas.lan:9000', { clicks: 12, description: 'Docker management', recent: 800, health: true }),
    L(home, cInfra, 'Pi-hole', 'http://pihole.lan/admin', { clicks: 22, recent: 90, health: true, icon: 'emoji:🕳️', pinned: true }),
    L(home, cInfra, 'Grafana', 'http://nas.lan:3000', { clicks: 18, description: 'Metrics & dashboards', recent: 400, health: true }),
    L(home, cApps, 'Nextcloud', 'https://cloud.example.lan', { clicks: 27, recent: 60, health: true, pinned: true, description: 'Files & calendar' }),
    L(home, cApps, 'Vaultwarden', 'https://vault.lan', { clicks: 40, description: 'Passwords', recent: 15, health: true }),
    L(home, cApps, 'Home Assistant', 'http://ha.lan:8123', { clicks: 55, description: 'Smart home', recent: 5, health: true, pinned: true }),
    L(home, cApps, 'Paperless-ngx', 'http://nas.lan:8000', { clicks: 9, recent: 3000 }),
    L(home, cApps, 'Immich', 'http://nas.lan:2283', { clicks: 33, description: 'Photos', recent: 120, health: true }),
    L(home, cApps, 'Mealie', 'http://nas.lan:9925', { clicks: 4 }),
    L(home, null, 'GitHub', 'https://github.com', { clicks: 7 }),
    L(media, cStream, 'Jellyfin', 'http://nas.lan:8096', { clicks: 60, description: 'Movies & shows', recent: 10, health: true, pinned: true }),
    L(media, cStream, 'Plex', 'http://nas.lan:32400/web', { clicks: 14, health: true }),
    L(media, cStream, 'Navidrome', 'http://nas.lan:4533', { clicks: 20, recent: 55, health: true }),
    L(media, cArr, 'Sonarr', 'http://nas.lan:8989', { clicks: 11, description: 'TV automation', health: true }),
    L(media, cArr, 'Radarr', 'http://nas.lan:7878', { clicks: 10, health: true }),
    L(media, cArr, 'qBittorrent', 'http://nas.lan:8080', { clicks: 16, health: true }),
    L(dev, cCode, 'Gitea', 'http://git.lan:3000', { clicks: 30, health: true }),
    L(dev, cCode, 'Drone CI', 'http://ci.lan', { clicks: 6 }),
    L(net, cOps, 'OPNsense', 'https://router.lan', { clicks: 25, health: true }),
    L(net, cOps, 'UniFi', 'https://unifi.lan:8443', { clicks: 8, health: true }),
  ]
  for (const ws of workspaces) links.filter((l) => l.workspace_id === ws.id && l.pinned).forEach((l, i) => (l.pin_sort = i))
  for (const l of links) if (l.health_check) health[l.id] = { status: Math.random() > 0.12 ? 'up' : 'down', latency_ms: Math.round(5 + Math.random() * 60), checked_at: now(), code: 200 }
}
seed()

const sse = new Set()
const bump = () => { version++; for (const r of sse) r.write(`event: change\ndata: ${JSON.stringify({ version })}\n\n`) }
setInterval(() => { for (const r of sse) r.write(': ping\n\n') }, 25000)

const cookieOf = (req) => (req.headers.cookie || '').match(/genkan_session=([^;]+)/)?.[1]
const roleOf = (req) => {
  if (process.env.MOCK_OPEN) return 'admin'
  const b = (req.headers.authorization || '').replace('Bearer ', '')
  if (b && b === apiToken) return 'admin'
  if (sessions.has(cookieOf(req))) return 'admin'
  return guest || settings.guest_access ? 'guest' : 'none'
}
const readBody = (req) => new Promise((res) => { const c = []; req.on('data', (d) => c.push(d)); req.on('end', () => res(Buffer.concat(c))) })
const send = (res, code, obj, headers = {}) => { res.writeHead(code, { 'Content-Type': 'application/json', ...headers }); res.end(obj === undefined ? '' : JSON.stringify(obj)) }
const nextPin = (wsid) => Math.max(-1, ...links.filter((l) => l.workspace_id === wsid && l.pinned).map((l) => l.pin_sort)) + 1
const sortBy = (a) => a.sort((x, y) => x.sort - y.sort)

const plug = createPluginsMock({ send, workspaces: () => workspaces, bump: () => bump() })
function bootstrap(role) {
  return {
    ...plug.bootstrap(role),
    auth: { setup_required: password === '', role },
    settings: role === 'none' ? { title: settings.title } : settings,
    workspaces: role === 'none' ? [] : workspaces, categories: role === 'none' ? [] : categories, links: role === 'none' ? [] : links,
    health: role === 'none' ? {} : health, version, ...(role === 'none' ? {} : { server_version: 'dev-mock' }),
  }
}

async function api(req, res, url) {
  const p = url.pathname
  const m = req.method
  const role = roleOf(req)
  const isBearer = (req.headers.authorization || '').startsWith('Bearer ')
  if (m !== 'GET' && !isBearer && !/\/click$/.test(p) && req.headers['x-requested-with'] !== 'genkan') return send(res, 403, { error: 'csrf' })
  const raw = await readBody(req)
  let body = {}
  if (raw.length && (req.headers['content-type'] || '').includes('json')) try { body = JSON.parse(raw) } catch { return send(res, 400, { error: 'bad json' }) }
  const A = () => role === 'admin' || (send(res, role === 'none' ? 401 : 403, { error: 'admin only' }), false)
  const R = () => role !== 'none' || (send(res, 401, { error: 'unauthorized' }), false)
  let mm
  if (await plug.handle(req, res, url, body, A, R)) return
  if (p === '/api/bootstrap' && m === 'GET') return send(res, 200, bootstrap(role))
  if (p === '/api/auth/setup' && m === 'POST') {
    if (password) return send(res, 409, { error: 'already set up' })
    if ((body.password || '').length < 6) return send(res, 400, { error: 'password too short' })
    password = body.password; const s = id(); sessions.add(s)
    return send(res, 200, { ok: true }, { 'Set-Cookie': `genkan_session=${s}; Path=/; HttpOnly; SameSite=Lax` })
  }
  if (p === '/api/auth/login' && m === 'POST') {
    if (body.password !== password) return send(res, 401, { error: 'invalid password' })
    const s = id(); sessions.add(s)
    return send(res, 200, { ok: true }, { 'Set-Cookie': `genkan_session=${s}; Path=/; HttpOnly; SameSite=Lax` })
  }
  if (p === '/api/auth/logout' && m === 'POST') { sessions.delete(cookieOf(req)); return send(res, 200, { ok: true }, { 'Set-Cookie': 'genkan_session=; Path=/; Max-Age=0' }) }
  if (p === '/api/auth/password' && m === 'POST') {
    if (!A()) return
    if (body.old_password !== password) return send(res, 401, { error: 'wrong password' })
    if ((body.new_password || '').length < 6) return send(res, 400, { error: 'password too short' })
    password = body.new_password; return send(res, 200, { ok: true })
  }
  if (p === '/api/events' && m === 'GET') {
    if (!R()) return
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' })
    res.write(': hi\n\n'); sse.add(res); req.on('close', () => sse.delete(res)); return
  }
  if (p === '/api/health' && m === 'GET') return R() && send(res, 200, health)
  if (p === '/api/settings' && m === 'PATCH') { if (!A()) return; Object.assign(settings, body); bump(); return send(res, 200, settings) }
  if (p === '/api/workspaces' && m === 'POST') {
    if (!A()) return
    if (!okIcon(body.icon)) return send(res, 400, { error: 'unknown icon' })
    const w = { id: id(), name: body.name || 'Workspace', icon: body.icon || 'lucide:layout-grid', wallpaper: body.wallpaper || 'preset:dusk', sort: Math.max(-1, ...workspaces.map((x) => x.sort)) + 1 }
    workspaces.push(w); bump(); return send(res, 200, w)
  }
  if (p === '/api/workspaces/reorder' && m === 'POST') { if (!A()) return; body.ids.forEach((i, k) => { const w = workspaces.find((x) => x.id === i); if (w) w.sort = k }); bump(); return send(res, 200, { ok: true }) }
  if ((mm = p.match(/^\/api\/workspaces\/(\w+)$/))) {
    if (!A()) return
    const w = workspaces.find((x) => x.id === mm[1]); if (!w) return send(res, 404, { error: 'not found' })
    if (m === 'PATCH') { if (!okIcon(body.icon)) return send(res, 400, { error: 'unknown icon' }); Object.assign(w, body, { id: w.id }); bump(); return send(res, 200, w) }
    if (m === 'DELETE') {
      if (workspaces.length < 2) return send(res, 409, { error: 'cannot delete the last workspace' })
      workspaces = workspaces.filter((x) => x !== w); categories = categories.filter((c) => c.workspace_id !== w.id); links = links.filter((l) => l.workspace_id !== w.id); bump(); return send(res, 200, { ok: true })
    }
  }
  if (p === '/api/categories' && m === 'POST') {
    if (!A()) return
    const c = { id: id(), workspace_id: body.workspace_id, name: body.name, sort: Math.max(-1, ...categories.filter((x) => x.workspace_id === body.workspace_id).map((x) => x.sort)) + 1 }
    categories.push(c); bump(); return send(res, 200, c)
  }
  if (p === '/api/categories/reorder' && m === 'POST') { if (!A()) return; body.ids.forEach((i, k) => { const c = categories.find((x) => x.id === i); if (c) c.sort = k }); bump(); return send(res, 200, { ok: true }) }
  if ((mm = p.match(/^\/api\/categories\/(\w+)$/))) {
    if (!A()) return
    const c = categories.find((x) => x.id === mm[1]); if (!c) return send(res, 404, { error: 'not found' })
    if (m === 'PATCH') { if (body.name) c.name = body.name; bump(); return send(res, 200, c) }
    if (m === 'DELETE') {
      if (url.searchParams.get('delete_links')) links = links.filter((l) => l.category_id !== c.id); else links.forEach((l) => { if (l.category_id === c.id) l.category_id = '' })
      categories = categories.filter((x) => x !== c); bump(); return send(res, 200, { ok: true })
    }
  }
  if (p === '/api/links' && m === 'POST') {
    if (!A()) return
    if (!body.url || !body.title) return send(res, 400, { error: 'title and url required' })
    const l = { id: id(), workspace_id: body.workspace_id, category_id: body.category_id || '', title: body.title, url: body.url, description: body.description || '', icon: body.icon || '', pinned: !!body.pinned, pin_sort: nextPin(body.workspace_id), sort: Math.max(-1, ...links.map((x) => x.sort)) + 1, clicks: 0, last_used_at: null, health_check: !!body.health_check, created_at: now(), updated_at: now() }
    links.push(l); bump(); return send(res, 200, l)
  }
  if (p === '/api/links/reorder' && m === 'POST') {
    if (!A()) return
    body.ids.forEach((i, k) => { const l = links.find((x) => x.id === i); if (!l) return; if (body.category_id === '__pinned') { l.pin_sort = k; l.pinned = true } else { l.sort = k; l.category_id = body.category_id || '' } })
    bump(); return send(res, 200, { ok: true })
  }
  if ((mm = p.match(/^\/api\/links\/(\w+)\/click$/)) && m === 'POST') { if (role === 'admin') { const l = links.find((x) => x.id === mm[1]); if (l) { l.clicks++; l.last_used_at = now() } } res.writeHead(204); return res.end() }
  if ((mm = p.match(/^\/api\/links\/(\w+)\/refresh-icon$/)) && m === 'POST') { if (!A()) return; const l = links.find((x) => x.id === mm[1]); if (!l) return send(res, 404, { error: 'not found' }); return send(res, 502, { error: 'no icon found' }) }
  if ((mm = p.match(/^\/api\/links\/(\w+)$/))) {
    if (!A()) return
    const l = links.find((x) => x.id === mm[1]); if (!l) return send(res, 404, { error: 'not found' })
    if (m === 'PATCH') { const was = l.pinned, np = nextPin(l.workspace_id); Object.assign(l, body, { id: l.id, updated_at: now() }); if (l.pinned && !was) l.pin_sort = np; bump(); return send(res, 200, l) }
    if (m === 'DELETE') { links = links.filter((x) => x !== l); bump(); return send(res, 200, { ok: true }) }
  }
  if (p === '/api/uploads/wallpaper' && m === 'POST') {
    if (!A()) return
    const ct = req.headers['content-type'] || ''
    const bd = ct.match(/boundary=(.+)$/)?.[1]
    if (!bd) return send(res, 400, { error: 'multipart expected' })
    const start = raw.indexOf('\r\n\r\n') + 4
    const end = raw.lastIndexOf('\r\n--' + bd)
    const buf = raw.subarray(start, end)
    const head = raw.subarray(0, start).toString('latin1')
    const ext = (head.match(/filename="[^"]*\.(\w+)"/)?.[1] || 'jpg').toLowerCase()
    if (!['jpg', 'jpeg', 'png', 'webp', 'avif'].includes(ext)) return send(res, 400, { error: 'unsupported image type' })
    const file = crypto.createHash('sha256').update(buf).digest('hex').slice(0, 16) + '.' + ext
    fs.writeFileSync(path.join(upDir, 'wallpapers', file), buf)
    return send(res, 200, { value: 'upload:' + file, url: '/uploads/wallpapers/' + file })
  }
  if (p === '/api/wallpapers' && m === 'GET') {
    if (!A()) return
    const dir = path.join(upDir, 'wallpapers')
    return send(res, 200, fs.readdirSync(dir).map((f) => ({ f, st: fs.statSync(path.join(dir, f)) })).sort((x, y) => y.st.mtimeMs - x.st.mtimeMs).map(({ f, st }) => ({ value: 'upload:' + f, url: '/uploads/wallpapers/' + f, size: st.size, created_at: st.mtime.toISOString() })))
  }
  if ((mm = p.match(/^\/api\/wallpapers\/([\w.-]+)$/)) && m === 'DELETE') {
    if (!A()) return
    const f = path.join(upDir, 'wallpapers', mm[1])
    if (!fs.existsSync(f)) return send(res, 404, { error: 'not found' })
    fs.unlinkSync(f)
    for (const w of workspaces) if (w.wallpaper === 'upload:' + mm[1]) w.wallpaper = 'preset:dusk'
    bump(); return send(res, 200, { ok: true })
  }
  if (p === '/api/uploads/icon' && m === 'POST') { if (!A()) return; return send(res, 200, { value: 'upload:mock.png', url: '/uploads/icons/mock.png' }) }
  if (p === '/api/export' && m === 'GET') { if (!A()) return; return send(res, 200, { schema: 1, settings, workspaces, categories, links }, { 'Content-Disposition': 'attachment; filename="genkan-export.json"' }) }
  if (p === '/api/import' && m === 'POST') {
    if (!A()) return
    const d = body.data; if (!d || !Array.isArray(d.workspaces)) return send(res, 400, { error: 'invalid export' })
    if (body.mode === 'replace') { workspaces = d.workspaces; categories = d.categories || []; links = d.links || [] } else { workspaces.push(...d.workspaces); categories.push(...(d.categories || [])); links.push(...(d.links || [])) }
    bump(); return send(res, 200, { ok: true, workspaces: d.workspaces.length, links: (d.links || []).length })
  }
  if (p === '/api/token' && m === 'GET') { if (!A()) return; return send(res, 200, { token: apiToken }) }
  if (p === '/api/token/regenerate' && m === 'POST') { if (!A()) return; apiToken = newToken(); return send(res, 200, { token: apiToken }) }
  if (p === '/api/skill' && m === 'GET') { if (!A()) return; res.writeHead(200, { 'Content-Type': 'text/plain', 'Content-Disposition': 'attachment; filename="genkan-skill.zip"' }); return res.end('mock: the real server returns a zip with genkan/SKILL.md, reference.md and .env\n') }
  return send(res, 404, { error: 'not found' })
}

const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.webp': 'image/webp', '.json': 'application/json', '.webmanifest': 'application/manifest+json' }
http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://x')
  try {
    if (url.pathname.startsWith('/api/')) return await api(req, res, url)
    if (url.pathname === '/healthz') return res.end('ok')
    const art = url.pathname.match(/^\/uploads\/plugin-wallpapers\/art-(\d+)\.svg$/)
    if (art) { res.writeHead(200, { 'Content-Type': 'image/svg+xml' }); return res.end(plug.art(art[1])) }
    if (url.pathname.startsWith('/uploads/')) {
      const f = path.join(upDir, url.pathname.slice(9))
      if (!f.startsWith(upDir) || !fs.existsSync(f) || fs.statSync(f).isDirectory()) { res.writeHead(404); return res.end() }
      res.writeHead(200, { 'Content-Type': mime[path.extname(f)] || 'application/octet-stream' }); return fs.createReadStream(f).pipe(res)
    }
    let f = path.join(dist, url.pathname === '/' ? 'index.html' : url.pathname)
    if (!f.startsWith(dist) || !fs.existsSync(f) || fs.statSync(f).isDirectory()) f = path.join(dist, 'index.html')
    res.writeHead(200, { 'Content-Type': mime[path.extname(f)] || 'application/octet-stream' })
    fs.createReadStream(f).pipe(res)
  } catch (e) { console.error(e); res.writeHead(500); res.end() }
}).listen(PORT, () => console.log('mock on :' + PORT))

setInterval(() => {
  for (const l of links) if (l.health_check && Math.random() < 0.05) health[l.id] = { status: health[l.id]?.status === 'up' ? 'down' : 'up', latency_ms: 20, checked_at: now(), code: 200 }
  for (const r of sse) r.write(`event: health\ndata: ${JSON.stringify(health)}\n\n`)
}, 15000)
