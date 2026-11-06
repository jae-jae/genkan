// Dev-only mock of the plugin endpoints (docs/PLUGINS.md §5): a fake weather widget (every Block type), a fake wallpaper source
// (generated SVGs), a fake calculator search, plus a few more catalog entries. Imported by server.mjs; not shipped.
const id = () => Math.random().toString(36).slice(2, 14).padEnd(12, 'a')
const now = () => new Date().toISOString()

const F = (key, type, label, o = {}) => ({ key, type, label, ...o })
const DEFS = {
  'ssh-resources-demo': {
    name: { en: 'SSH resources (demo)', zh: 'SSH 资源（演示）' }, version: '1.0.0', icon: 'server',
    permissions: { fetch: [], ssh: true },
    config_schema: [F('privateKey', 'secret', { en: 'Private key', zh: '私钥' }, { multiline: true })],
    widgets: [{ id: 'resources', name: 'Resources', size: 'm', refresh: 30 }],
  },
  weather: {
    name: { en: 'Weather', zh: '天气' }, version: '1.0.0', icon: 'cloud-sun', description: { en: 'Current weather and a 7-day trend from Open-Meteo (no API key).', zh: 'Open-Meteo 实时天气和 7 天趋势（无需 API key）。' }, author: 'genkan', homepage: 'https://open-meteo.com',
    permissions: { fetch: ['api.open-meteo.com', 'geocoding-api.open-meteo.com'], links: '' },
    config_schema: [F('unit', 'select', 'Temperature unit', { default: 'c', options: [{ value: 'c', label: 'Celsius' }, { value: 'f', label: 'Fahrenheit' }] })],
    widgets: [{ id: 'current', name: 'Current weather', size: 'm', refresh: 600, config: [F('city', 'string', 'City', { required: true, default: 'Tokyo', help: 'Resolved with the geocoding API' }), F('days', 'number', 'Forecast days', { default: 7, min: 3, max: 14 })] }],
  },
  calculator: {
    name: 'Calculator', version: '1.0.0', icon: 'calculator', description: 'Type 2+2*3 in the search box and press Enter to copy the result.', author: 'genkan',
    permissions: { fetch: [], links: '' }, config_schema: [], search: { name: 'Calculator', prefix: '' },
  },
  picsum: {
    name: 'Picsum', version: '1.0.0', icon: 'image', description: 'Random landscape photos from picsum.photos.', author: 'genkan',
    permissions: { fetch: ['picsum.photos', 'fastly.picsum.photos'], links: '' }, config_schema: [], wallpapers: [{ id: 'random', name: 'Random landscape', refresh: 3600 }],
  },
  'bing-wallpaper': {
    name: 'Bing daily wallpaper', version: '1.0.0', icon: 'image', description: 'The Bing home page picture of the day.', author: 'genkan',
    permissions: { fetch: ['www.bing.com', '*.bing.net'], links: '' },
    config_schema: [F('mkt', 'select', 'Market', { default: 'en-US', options: [{ value: 'en-US', label: 'en-US' }, { value: 'zh-CN', label: 'zh-CN' }, { value: 'ja-JP', label: 'ja-JP' }] })],
    wallpapers: [{ id: 'daily', name: 'Daily picture', refresh: 3600 }],
  },
  'hacker-news': {
    name: 'Hacker News', version: '1.0.0', icon: 'newspaper', description: 'Top stories from Hacker News.', author: 'genkan',
    permissions: { fetch: ['hacker-news.firebaseio.com'], links: '' }, config_schema: [],
    widgets: [{ id: 'top', name: 'Top stories', size: 'm', refresh: 900, config: [F('count', 'number', 'Stories', { default: 5, min: 1, max: 15 })] }],
  },
  'world-clock': {
    name: { en: 'World clock', zh: '世界时钟' }, version: '1.0.0', icon: 'clock', description: { en: 'Time in several time zones. No network access.', zh: '多个时区的时间，无需联网。' }, author: 'genkan',
    permissions: { fetch: [], links: '' }, config_schema: [],
    widgets: [{ id: 'clock', name: { en: 'World clock', zh: '世界时钟' }, size: 's', refresh: 60, config: [F('zones', 'text', 'Time zones', { default: 'Asia/Tokyo\nEurope/Berlin\nAmerica/New_York', help: 'One IANA zone per line' })] }],
  },
  pihole: {
    name: 'Pi-hole', version: '1.0.0', icon: 'shield', description: 'Queries and blocked count from your Pi-hole, with a pause button.', author: 'genkan',
    permissions: { fetch: [], links: '' }, config_schema: [],
    widgets: [{ id: 'stats', name: 'Pi-hole stats', size: 's', refresh: 60, config: [F('url', 'url', 'Pi-hole address', { required: true, allowFetch: true, default: 'http://pihole.lan' }), F('token', 'secret', 'API token', { required: true }), F('verbose', 'boolean', 'Show details', { default: true })] }],
  },
  todo: {
    name: { en: 'To-do', zh: '待办' }, version: '1.0.0', icon: 'list-todo', description: { en: 'A checklist you can tick off, rename, reorder and clear. No network access.', zh: '可勾选、重命名、排序和清除的待办清单，无需联网。' }, author: 'genkan',
    permissions: { fetch: [], links: '' }, config_schema: [],
    widgets: [{ id: 'list', name: { en: 'To-do list', zh: '待办清单' }, size: 'm', refresh: 3600, config: [] }],
  },
  note: {
    name: { en: 'Sticky note', zh: '便签' }, version: '1.0.0', icon: 'sticky-note', description: { en: 'A Markdown note you edit in place; task lists are clickable. No network access.', zh: '就地编辑的 Markdown 便签，任务列表可直接点击，无需联网。' }, author: 'genkan',
    permissions: { fetch: [], links: '' }, config_schema: [],
    widgets: [{ id: 'note', name: { en: 'Sticky note', zh: '便签' }, size: 'm', refresh: 3600, config: [F('tone', 'select', { en: 'Color', zh: '颜色' }, { default: 'yellow', options: ['default', 'yellow', 'pink', 'blue', 'green', 'purple'].map((v) => ({ value: v, label: v })) })] }],
  },
  countdown: {
    name: { en: 'Countdown', zh: '倒计时' }, version: '1.0.0', icon: 'calendar', description: { en: 'Days until a date. No network access.', zh: '距离某一天还有几天，无需联网。' }, author: 'genkan',
    permissions: { fetch: [], links: '' }, config_schema: [],
    widgets: [{ id: 'days', name: { en: 'Countdown', zh: '倒计时' }, size: 's', refresh: 60, config: [F('title', 'string', 'Title', { default: 'New year' }), F('date', 'string', 'Date', { default: '2027-01-01' })] }],
  },
  rss: {
    name: 'RSS feed', version: '1.0.0', icon: 'rss', description: 'Latest entries of a feed.', author: 'genkan',
    permissions: { fetch: [], links: '' }, config_schema: [],
    widgets: [{ id: 'feed', name: 'RSS feed', size: 'm', refresh: 900, config: [F('url', 'url', 'Feed URL', { allowFetch: true, default: 'https://example.com/feed.xml' }), F('count', 'number', 'Entries', { default: 3, min: 1, max: 15 })] }],
  },
  'link-importer': {
    name: 'Link importer', version: '1.0.0', icon: 'download', description: 'Imports links from a JSON URL on a schedule.', author: 'genkan',
    permissions: { fetch: [], links: 'write' },
    config_schema: [F('feed', 'url', 'JSON URL', { required: true, allowFetch: true })], schedules: [{ id: 'sync', every: 3600 }],
  },
  'any-fetch': {
    name: 'Any fetch (demo)', version: '0.1.0', icon: 'globe', description: 'Demo of the "any host" warning.', author: 'someone',
    permissions: { fetch: ['*'], links: 'read' }, config_schema: [], search: { name: 'Demo', prefix: 'demo' },
  },
}
const CATALOG = ['ssh-resources-demo', 'todo', 'note', 'weather', 'calculator', 'picsum', 'bing-wallpaper', 'hacker-news', 'world-clock', 'pihole', 'countdown', 'rss', 'link-importer', 'any-fetch']

// widget layout fields (PLUGINS.md 11.1): w = 0..6 columns (0 = default by manifest size), h = 0 (auto) or 80..1200 px
const clampW = (v) => Math.min(6, Math.max(0, Math.round(Number(v) || 0)))
const clampH = (v) => { v = Math.round(Number(v) || 0); return v <= 0 ? 0 : Math.min(1200, Math.max(80, v)) }

// ---- generated sources (so "Source" / "Preview" show something realistic) ----
const en = (v) => (typeof v === 'string' ? v : v?.en ?? Object.values(v || {})[0] ?? '')
const srcOf = (id, d) => {
  const name = en(d.name), desc = en(d.description)
  const hooks = []
  if (d.widgets?.length) hooks.push(`export async function widget(ctx) {\n  const { t } = ctx // t(key, params) reads the \`messages\` table above in ctx.lang\n  // ctx.widget = "${d.widgets[0].id}", ctx.config = the instance settings\n  return { title: t("name"), body: [{ type: "text", text: t("hello") }] }\n}`)
  if (d.wallpapers?.length) hooks.push(`export async function wallpaper(ctx) {\n  const r = await genkan.fetch("https://${d.permissions?.fetch?.[0] || 'example.com'}/")\n  return { url: r.url, title: ctx.t("name") }\n}`)
  if (d.search) hooks.push(`export async function search(ctx) {\n  return [{ title: ctx.t("result", { q: ctx.query }), copy: ctx.query }]\n}`)
  if (d.schedules?.length) hooks.push(`export async function schedule(ctx) {\n  console.log("sync started")\n}`)
  const msgs = `export const messages = {\n  en: { name: ${JSON.stringify(name)}, desc: ${JSON.stringify(desc)}, hello: "Hello from ${id}", result: "Result for {q}" },\n  zh: { name: ${JSON.stringify(name)}, desc: ${JSON.stringify(desc)}, hello: "来自 ${id} 的问候", result: "{q} 的结果" },\n}`
  return `// ${name}: ${desc}\n${msgs}\n\nexport const manifest = {\n  id: "${id}",\n  name: t("name"), // t() in the manifest points at \`messages\`\n  version: "${d.version}",\n  description: t("desc"),\n  permissions: { fetch: ${JSON.stringify(d.permissions?.fetch || [])}, links: "${d.permissions?.links || ''}" },\n}\n\n${hooks.join('\n\n')}\n`
}

// ---- fake GitHub "third-party" market ----
const ago = (d) => new Date(Date.now() - d * 86400000).toISOString()
const REPOS = [
  ['alice/genkan-plugins', 'A small collection of useful widgets: crypto ticker, quote of the day and more.', 842, 2],
  ['bob/genkan-homelab', 'Widgets for homelab dashboards: Proxmox, TrueNAS, UniFi and friends. Needs read access to your network.', 531, 9],
  ['carol/genkan-rss-plus', 'RSS / Atom reader widget with unread counts and per-feed icons.', 377, 21],
  ['dave/genkan-quotes', 'Daily quotes for your start page.', 120, 64],
  ['erin/genkan-weather-extras', 'Air quality, pollen and UV index widgets.', 96, 5],
  ['frank/genkan-github-widgets', 'Your GitHub notifications and open pull requests as widgets and a search provider.', 88, 1],
  ['grace/genkan-link-sync', 'Sync your links with a JSON file on a schedule. Writes to your links, so read the source first.', 64, 130],
  ['heidi/genkan-pomodoro', 'Pomodoro timer widget. No network access at all.', 41, 300],
  ['ivan/genkan-flaky', 'A repository whose scan fails (demo of the error state).', 12, 12],
  ['judy/genkan-everything', 'Demo of a plugin that asks for everything. Do not install.', 3, 400],
  ['kai/genkan-wallpapers', 'Wallpaper sources: Unsplash, Wikimedia picture of the day.', 210, 40],
].map(([repo, description, stars, d], i) => ({ repo, description, stars, updated_at: ago(d), html_url: 'https://github.com/' + repo, owner_avatar: i === 3 ? '' : `/uploads/plugin-wallpapers/art-${i + 11}.svg`, default_branch: 'main' }))
const F2 = (key, type, label, o = {}) => ({ key, type, label, ...o })
const mkp = (id, name, version, description, o = {}) => ({ id, name, version, description, author: o.author || 'community', homepage: '', icon: o.icon || 'puzzle', permissions: { fetch: o.fetch || [], links: o.links || '' }, config_schema: o.config_schema || [], widgets: o.widgets, wallpapers: o.wallpapers, search: o.search, schedules: o.schedules })
const raw = (repo, file) => `https://raw.githubusercontent.com/${repo}/main/${file}`
const MARKET_PLUGINS = {}
const MARKET_SCAN = {}
const regMk = (repo, file, def, error) => {
  const url = raw(repo, file)
  MARKET_PLUGINS[url] = { def, error, source: error ? `export const manifest = {\n  id: "${def?.id || 'broken'}",\n  name: "Broken",\n  version: "0.1.0"\n}\nexport async function widget(ctx) {\n  return { title: "x", body: [ }\n}\n` : srcOf(def.id, def) }
  ;(MARKET_SCAN[repo] ||= []).push({ path: file, url, def, error })
}
regMk('alice/genkan-plugins', 'plugins/crypto-ticker.js', mkp('crypto-ticker', 'Crypto ticker', '1.2.0', 'Bitcoin and Ethereum prices from CoinGecko.', { icon: 'wallet', fetch: ['api.coingecko.com'], widgets: [{ id: 'prices', name: 'Prices', size: 's', refresh: 120, config: [F2('coins', 'string', 'Coins', { default: 'bitcoin,ethereum' })] }] }))
regMk('alice/genkan-plugins', 'plugins/quote-of-day.js', mkp('quote-of-day', 'Quote of the day', '1.0.1', 'A short quote every day. No network access.', { icon: 'sparkles', widgets: [{ id: 'quote', name: 'Quote', size: 's', refresh: 3600 }] }))
regMk('alice/genkan-plugins', 'plugins/broken.js', null, 'invalid plugin: compile error: plugins/broken.js:4:12: SyntaxError: Unexpected token \'}\'')
regMk('bob/genkan-homelab', 'proxmox.ts', mkp('proxmox', 'Proxmox status', '0.4.0', 'CPU, memory and running guests of a Proxmox node.', { icon: 'server', config_schema: [], widgets: [{ id: 'node', name: 'Node status', size: 'm', refresh: 60, config: [F2('url', 'url', 'Proxmox URL', { required: true, allowFetch: true }), F2('token', 'secret', 'API token', { required: true })] }] }))
regMk('bob/genkan-homelab', 'unifi.ts', mkp('unifi', 'UniFi clients', '0.2.3', 'Connected clients and bandwidth from a UniFi controller.', { icon: 'router', links: 'read', widgets: [{ id: 'clients', name: 'Clients', size: 's', refresh: 60, config: [F2('url', 'url', 'Controller URL', { required: true, allowFetch: true })] }] }))
regMk('carol/genkan-rss-plus', 'rss-plus.js', mkp('rss-plus', 'RSS Plus', '2.0.0', 'Multi-feed reader with unread counts.', { icon: 'rss', widgets: [{ id: 'feeds', name: 'Feeds', size: 'm', refresh: 600, config: [F2('feeds', 'text', 'Feed URLs', { required: true, allowFetch: true })] }] }))
regMk('dave/genkan-quotes', 'quotes.js', mkp('quotes', 'Quotes', '1.0.0', 'Daily quotes.', { widgets: [{ id: 'q', name: 'Quote', size: 's', refresh: 3600 }] }))
regMk('erin/genkan-weather-extras', 'uv-index.js', mkp('uv-index', 'UV index', '1.1.0', 'Current UV index from Open-Meteo.', { icon: 'sun', fetch: ['api.open-meteo.com'], widgets: [{ id: 'uv', name: 'UV', size: 's', refresh: 900, config: [F2('city', 'string', 'City', { default: 'Tokyo' })] }] }))
regMk('erin/genkan-weather-extras', 'air-quality.js', mkp('air-quality', 'Air quality', '1.0.0', 'AQI and PM2.5.', { icon: 'cloud', fetch: ['air-quality-api.open-meteo.com'], widgets: [{ id: 'aqi', name: 'AQI', size: 's', refresh: 900 }] }))
regMk('frank/genkan-github-widgets', 'gh-prs.js', mkp('gh-prs', 'GitHub PRs', '0.9.0', 'Your open pull requests.', { icon: 'code', fetch: ['api.github.com'], widgets: [{ id: 'prs', name: 'Open PRs', size: 'm', refresh: 300, config: [F2('token', 'secret', 'Token', { required: true })] }], search: { name: 'GitHub', prefix: 'gh' } }))
regMk('grace/genkan-link-sync', 'sync.js', mkp('link-sync', 'Link sync', '1.0.0', 'Imports links from a JSON file on a schedule.', { icon: 'download', links: 'write', schedules: [{ id: 'sync', every: 3600 }], config_schema: [F2('feed', 'url', 'JSON URL', { required: true, allowFetch: true })] }))
regMk('heidi/genkan-pomodoro', 'pomodoro.js', mkp('pomodoro', 'Pomodoro', '1.0.0', 'Focus timer. No network.', { icon: 'clock', widgets: [{ id: 'timer', name: 'Timer', size: 's', refresh: 30 }] }))
regMk('judy/genkan-everything', 'all.js', mkp('everything', 'Everything', '6.6.6', 'Asks for every permission there is.', { fetch: ['*'], links: 'write', widgets: [{ id: 'x', name: 'X', size: 'l', refresh: 60 }], search: { name: 'All', prefix: 'all' } }))
regMk('kai/genkan-wallpapers', 'unsplash.js', mkp('unsplash', 'Unsplash', '1.3.0', 'Random Unsplash photos.', { icon: 'image', fetch: ['source.unsplash.com', 'images.unsplash.com'], wallpapers: [{ id: 'random', name: 'Random', refresh: 3600 }] }))
regMk('kai/genkan-wallpapers', 'wikimedia.js', mkp('wikimedia', 'Wikimedia POTD', '1.0.0', 'Picture of the day from Wikimedia Commons.', { icon: 'image', fetch: ['commons.wikimedia.org', 'upload.wikimedia.org'], wallpapers: [{ id: 'potd', name: 'Picture of the day', refresh: 21600 }] }))

export function createPluginsMock({ send, workspaces, bump }) {
  const installed = []
  const insts = []
  const sources = {}
  const add = (pid, enabled, config = {}) => {
    const def = DEFS[pid]
    installed.push({ id: pid, ...def, enabled, config, secrets_set: [], source_url: '', last_error: '', installed_at: now(), updated_at: now(), source: srcOf(pid, def) })
    return installed[installed.length - 1]
  }
  const addWidget = (plugin, widget, workspace_id, config = {}, w = 0, h = 0) => insts.push({ id: id(), plugin, widget, workspace_id, config, sort: insts.length, w, h })
  add('todo', true)
  add('note', true)
  add('weather', true, { unit: 'c' })
  add('calculator', true)
  add('picsum', true)
  add('world-clock', true)
  const pi = add('pihole', true)
  pi.secrets_set = []
  add('hacker-news', true)
  add('countdown', true)
  add('rss', true)
  add('any-fetch', false)
  const logs = { weather: [['info', 'fetched forecast for Tokyo (212 ms)']], pihole: [['warn', 'token missing'], ['error', 'fetch http://pihole.lan failed: connection refused']] }
  pi.last_error = 'fetch http://pihole.lan failed: connection refused'
  const seedWidgets = () => {
    const ws = workspaces()
    addWidget('todo', 'list', ws[0].id, {})
    addWidget('note', 'note', ws[0].id, { tone: 'yellow' })
    addWidget('weather', 'current', '', { city: 'Tokyo', days: 7 })
    addWidget('world-clock', 'clock', ws[0].id, { zones: 'Asia/Tokyo\nEurope/Berlin\nAmerica/New_York' })
    addWidget('pihole', 'stats', ws[0].id, { url: 'http://pihole.lan', token: 'seeded', verbose: true })
    addWidget('countdown', 'days', '', { title: 'Double 11', date: '2026-11-11' })
    addWidget('rss', 'feed', '', { url: 'https://example.com/feed.xml', count: 3 })
    addWidget('hacker-news', 'top', '', { count: 12 })
    addWidget('countdown', 'days', '', { title: 'New year', date: '2027-01-01' })
  }
  seedWidgets()

  const pub = (p) => ({
    id: p.id, name: p.name, version: p.version, description: p.description || '', author: p.author || '', homepage: p.homepage || '', icon: p.icon || '', enabled: p.enabled,
    permissions: { fetch: p.permissions?.fetch || [], links: p.permissions?.links || '', ssh: !!p.permissions?.ssh }, config_schema: p.config_schema || [],
    wallpapers: p.wallpapers || [], widgets: p.widgets || [], search: p.search || null, schedules: p.schedules || [],
  })
  const adminInfo = (p) => ({ ...pub(p), config: Object.fromEntries(Object.entries(p.config || {}).map(([k, v]) => [k, (p.config_schema || []).find((f) => f.key === k)?.type === 'secret' ? '' : v])), secrets_set: p.secrets_set, source_url: p.source_url, last_error: p.last_error, installed_at: p.installed_at, updated_at: p.updated_at })
  const secretKeys = (pl, widget) => (widget ? pl.widgets.find((w) => w.id === widget)?.config || [] : pl.config_schema || []).filter((f) => f.type === 'secret').map((f) => f.key)
  const instPub = (w, admin) => {
    const pl = installed.find((p) => p.id === w.plugin)
    const sk = pl ? secretKeys(pl, w.widget) : []
    const config = { ...w.config }
    for (const k of sk) delete config[k]
    return { ...w, config: admin ? { ...config, ...Object.fromEntries(sk.filter((k) => w.config[k]).map((k) => [k, '••••'])) } : config }
  }

  const svg = (n) => {
    const h = (n * 47) % 360
    return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1600 900" preserveAspectRatio="xMidYMid slice"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="hsl(${h} 70% 55%)"/><stop offset="1" stop-color="hsl(${(h + 70) % 360} 70% 25%)"/></linearGradient></defs><rect width="1600" height="900" fill="url(#g)"/><circle cx="1180" cy="260" r="150" fill="hsl(${(h + 30) % 360} 90% 78%)" opacity=".85"/><path d="M0 900V620Q300 480 600 620T1200 600T1600 560V900Z" fill="hsl(${(h + 200) % 360} 45% 18%)" opacity=".8"/><path d="M0 900V740Q400 640 800 740T1600 700V900Z" fill="hsl(${(h + 220) % 360} 40% 10%)"/></svg>`
  }
  let wpN = 0

  const calc = (q) => {
    const e = q.replace(/\^/g, '**').replace(/(\d+(?:\.\d+)?)%/g, '($1/100)')
    if (!/^[\d+\-*/().\s*]+$/.test(e) || !/[+\-*/]/.test(e.replace(/^\s*-/, ''))) return null
    try { const v = Function('"use strict";return (' + e + ')')(); return Number.isFinite(v) ? Math.round(v * 1e10) / 1e10 : null } catch { return null }
  }

  // ---- interactive widgets (docs/PLUGINS.md 15): per-instance state of the simulated todo / note plugins ----
  const state = new Map()
  const st = (w) => {
    if (!state.has(w.id)) state.set(w.id, w.plugin === 'todo' ? { items: ['Buy milk', 'Water the plants', 'Renew the domain'].map((text, i) => ({ id: 'i' + i, text, done: i === 2, at: Date.now() })), seq: 3 } : { text: '# Shopping\n\n- [ ] eggs\n- [x] bread\n- [ ] coffee\n\n**Tip:** double-click to edit, `Ctrl+Enter` saves.\n\n| Item | Qty |\n|:--|--:|\n| eggs | 12 |\n| coffee | 1 |\n' })
    return state.get(w.id)
  }
  const todoView = (w, zh) => {
    const s = st(w)
    const open = s.items.filter((i) => !i.done)
    const done = s.items.filter((i) => i.done)
    return {
      title: (zh ? '待办' : 'To-do') + (open.length ? ` · ${open.length}` : ''), icon: 'list-todo',
      body: [
        { type: 'input', action: 'add', placeholder: zh ? '添加待办' : 'Add a task', submit: zh ? '添加' : 'Add', icon: 'plus', maxLength: 500 },
        { type: 'list', sortAction: 'reorder', empty: zh ? '没有待办' : 'Nothing to do', items: [...open, ...done].map((i) => ({ id: i.id, title: i.text, check: { checked: i.done, action: 'toggle', params: { id: i.id } }, edit: { action: 'rename', params: { id: i.id } }, actions: [{ icon: 'trash-2', label: zh ? '删除' : 'Delete', action: 'remove', params: { id: i.id } }] })) },
        ...(done.length ? [{ type: 'button', action: 'clear', icon: 'eraser', label: (zh ? '清除已完成' : 'Clear done') + ` (${done.length})` }] : []),
      ],
    }
  }
  const noteView = (w, zh) => {
    const tone = w.config.tone ?? 'yellow'
    return { ...(tone !== 'default' ? { tone } : {}), body: [{ type: 'markdown', text: st(w).text, editAction: 'save', placeholder: zh ? '双击编辑…' : 'Double-click to edit…' }] }
  }
  /** runs an action of the simulated todo / note plugin; returns [status, body] */
  const act = (w, body, lang) => {
    const zh = (lang || '').startsWith('zh')
    const s = st(w)
    const pr = body.params || {}
    const val = typeof pr.value === 'string' ? pr.value : ''
    const text = val.trim().slice(0, 500)
    const find = () => s.items.find((i) => i.id === pr.id)
    if (w.plugin === 'note') {
      if (body.action !== 'save') return [400, { error: 'unknown action: ' + body.action }]
      if (typeof pr.value !== 'string') return [400, { error: 'value is required' }]
      s.text = val.slice(0, 20000)
    } else {
      if (body.action === 'add') { if (!text) return [400, { error: 'text is required' }]; if (s.items.length >= 200) return [400, { error: 'too many items' }]; s.items.push({ id: 'i' + ++s.seq, text, done: false, at: Date.now() }) }
      else if (body.action === 'toggle') { const i = find(); if (!i) return [400, { error: 'unknown item' }]; i.done = pr.checked === undefined ? !i.done : !!pr.checked }
      else if (body.action === 'rename') { const i = find(); if (!i) return [400, { error: 'unknown item' }]; if (!text) return [400, { error: 'text is required' }]; i.text = text }
      else if (body.action === 'remove') { if (!find()) return [400, { error: 'unknown item' }]; s.items = s.items.filter((i) => i.id !== pr.id) }
      else if (body.action === 'clear') s.items = s.items.filter((i) => !i.done)
      else if (body.action === 'reorder') {
        if (!Array.isArray(pr.ids)) return [400, { error: 'ids is required' }]
        const by = new Map(s.items.map((i) => [i.id, i]))
        const first = pr.ids.map((k) => by.get(k)).filter(Boolean)
        s.items = [...new Set([...first, ...s.items])]
      } else return [400, { error: 'unknown action: ' + body.action }]
    }
    return [200, { view: { view: view(w, lang), updated_at: now(), next_at: new Date(Date.now() + 3600000).toISOString() } }]
  }

  const spark = (n, base, amp) => Array.from({ length: n }, (_, i) => Math.round((base + Math.sin(i * 0.9 + 1) * amp + (Math.random() - 0.5) * 2) * 10) / 10)
  const view = (w, lang) => {
    const zh = (lang || '').startsWith('zh')
    if (w.plugin === 'todo') return todoView(w, zh)
    if (w.plugin === 'note') return noteView(w, zh)
    if (w.plugin === 'weather') {
      const f = (installed.find((p) => p.id === 'weather').config.unit || 'c') === 'f'
      const T = (c) => (f ? Math.round(c * 1.8 + 32) : c)
      const vals = spark(Math.min(14, w.config.days || 7), 18, 5).map(T)
      return {
        title: `${w.config.city || 'Tokyo'}`, icon: 'cloud-sun', link: 'https://open-meteo.com',
        body: [
          { type: 'row', children: [
            { type: 'text', text: `${T(21)}°${f ? 'F' : 'C'}`, size: 'xl' },
            { type: 'stats', items: [{ label: zh ? '湿度' : 'Humidity', value: 64, unit: '%', icon: 'cloud', trend: 'up' }, { label: zh ? '风速' : 'Wind', value: 12, unit: 'km/h', trend: 'down' }, { label: zh ? '气压' : 'Pressure', value: 1012, unit: 'hPa', trend: 'flat' }] },
          ] },
          { type: 'text', text: zh ? '多云转晴，体感舒适' : 'Partly cloudy, comfortable', muted: true, size: 's' },
          { type: 'chart', kind: 'spark', values: vals, labels: vals.map((_, i) => (i === 0 ? (zh ? '今天' : 'Today') : i === vals.length - 1 ? `+${i}d` : '')) },
          { type: 'divider' },
          { type: 'chart', kind: 'bar', values: [2, 0, 5, 12, 3, 0, 1], labels: ['M', 'T', 'W', 'T', 'F', 'S', 'S'] },
          { type: 'progress', label: zh ? '降水概率' : 'Chance of rain', value: 0.35, text: '35%' },
          { type: 'kv', items: [{ k: zh ? '日出' : 'Sunrise', v: '05:12' }, { k: zh ? '日落' : 'Sunset', v: '18:47' }, { k: 'UV', v: '5 (moderate)' }] },
          { type: 'list', items: [{ title: zh ? '明天' : 'Tomorrow', subtitle: zh ? '小雨' : 'Light rain', icon: 'cloud', right: `${T(19)}° / ${T(14)}°`, badge: '60%' }, { title: zh ? '后天' : 'Thu', subtitle: zh ? '晴' : 'Sunny', icon: 'cloud-sun', right: `${T(24)}° / ${T(15)}°`, url: 'https://open-meteo.com' }, { title: 'javascript: link (blocked)', url: 'javascript:alert(1)', right: 'x' }] },
          { type: 'image', alt: 'radar', ratio: 3, src: 'data:image/svg+xml;utf8,' + encodeURIComponent(svg(5)) },
          { type: 'row', children: [{ type: 'button', label: zh ? '刷新数据' : 'Refresh now', action: 'refresh', icon: 'refresh-cw' }, { type: 'button', label: zh ? '失败动作' : 'Failing action', action: 'fail' }] },
        ],
      }
    }
    if (w.plugin === 'world-clock') {
      const zones = String(w.config.zones || '').split('\n').map((s) => s.trim()).filter(Boolean).slice(0, 6)
      return { title: zh ? '世界时钟' : 'World clock', icon: 'clock', body: [{ type: 'kv', items: zones.map((z) => { let v = '?'; try { v = new Date().toLocaleTimeString('en-GB', { timeZone: z, hour: '2-digit', minute: '2-digit' }) } catch {} return { k: z.split('/').pop().replace(/_/g, ' '), v } }) }] }
    }
    if (w.plugin === 'pihole') {
      return { title: 'Pi-hole', icon: 'shield', body: [
        { type: 'stats', items: [{ label: zh ? '今日查询' : 'Queries', value: 18342 }, { label: zh ? '已拦截' : 'Blocked', value: 4210, trend: 'up' }] },
        { type: 'progress', label: zh ? '拦截率' : 'Block rate', value: 0.23, text: '23%' },
        { type: 'button', label: zh ? '暂停拦截 5 分钟' : 'Pause blocking 5 min', action: 'pause', params: { minutes: 5 } },
      ] }
    }
    if (w.plugin === 'hacker-news') {
      const n = w.config.count || 5
      return { title: 'Hacker News', icon: 'newspaper', link: 'https://news.ycombinator.com', body: [{ type: 'list', items: Array.from({ length: n }, (_, i) => ({ title: `Show HN: a tiny start page #${i + 1}`, subtitle: 'example.com', url: 'https://news.ycombinator.com/', right: String(300 - i * 31), badge: i === 0 ? 'new' : undefined })) }] }
    }
    if (w.plugin === 'countdown') {
      const days = Math.max(0, Math.ceil((new Date(w.config.date || '2027-01-01') - Date.now()) / 864e5))
      return { title: w.config.title || 'Countdown', icon: 'calendar', body: [{ type: 'stat', label: zh ? '还剩' : 'Remaining', value: days, unit: zh ? '天' : 'days', icon: 'calendar', trend: 'down' }, { type: 'text', text: String(w.config.date), muted: true, size: 's', align: 'center' }] }
    }
    if (w.plugin === 'rss') {
      const n = w.config.count || 3
      return { title: zh ? '示例博客' : 'Example blog', icon: 'rss', link: 'https://example.com', body: [{ type: 'list', items: Array.from({ length: n }, (_, i) => ({ title: `Weekly digest #${417 - i}: something worth reading`, subtitle: 'A short summary of the post goes here...', url: 'https://example.com/', right: `Oct ${30 - i * 7}` })) }] }
    }
    return { title: w.widget, body: [{ type: 'text', text: 'mock' }] }
  }

  const parseManifest = (src) => {
    const m = (re) => src.match(re)?.[1]
    const pid = m(/\bid:\s*["']([a-z0-9][a-z0-9-]+)["']/)
    if (!pid) return null
    // name: t("key") / description: t("key") -> the English text of messages.en.key (the mock does not run JS)
    const tkey = (f) => { const k = m(new RegExp('\\b' + f + ':\\s*t\\(\\s*["\']([^"\']+)["\']')); return k ? src.match(new RegExp('\\b' + k + ':\\s*["\']([^"\']+)["\']'))?.[1] : undefined }
    const fetchList = (m(/\bfetch:\s*\[([^\]]*)\]/) || '').split(',').map((s) => s.trim().replace(/^["']|["']$/g, '')).filter(Boolean)
    return {
      id: pid, name: m(/\bname:\s*["']([^"']+)["']/) || tkey('name') || pid, version: m(/\bversion:\s*["']([^"']+)["']/) || '0.0.1', description: m(/\bdescription:\s*["']([^"']+)["']/) || tkey('description') || '', author: '', icon: '',
      permissions: { fetch: fetchList, links: m(/\blinks:\s*["'](read|write)["']/) || '' }, config_schema: [],
      search: /export\s+(async\s+)?function\s+search/.test(src) ? { name: m(/\bname:\s*["']([^"']+)["']/) || pid, prefix: '' } : null,
    }
  }

  /** crude stand-in for the compiler: unbalanced braces -> "invalid plugin: compile error: plugin.js:L:C: SyntaxError: ..." */
  const syntaxError = (src) => {
    let depth = 0
    const lines = String(src).split('\n')
    for (let i = 0; i < lines.length; i++) for (let j = 0; j < lines[i].length; j++) {
      const c = lines[i][j]
      if (c === '{') depth++
      else if (c === '}' && --depth < 0) return `invalid plugin: compile error: plugin.js:${i + 1}:${j + 1}: SyntaxError: Unexpected token '}'`
    }
    return depth > 0 ? `invalid plugin: compile error: plugin.js:${lines.length}:${lines[lines.length - 1].length + 1}: SyntaxError: Unexpected end of input` : ''
  }

  const handle = async function (req, res, url, body, A, R) {
    const p = url.pathname
    const m = req.method
    let mm
    if (!p.startsWith('/api/plugins') && !p.startsWith('/api/widgets')) return false
    if (p === '/api/plugins/search' && m === 'GET') {
      if (!R()) return true
      const q = (url.searchParams.get('q') || '').trim()
      const groups = []
      if (installed.find((x) => x.id === 'calculator')?.enabled && q.length >= 2) {
        const v = calc(q)
        if (v !== null) groups.push({ plugin: 'calculator', name: 'Calculator', icon: 'calculator', results: [{ title: '= ' + v, subtitle: q, icon: 'calculator', copy: String(v) }, { title: 'Open in calculator.net', subtitle: 'External link', url: 'https://www.calculator.net/', icon: 'external-link' }] })
      }
      const dm = installed.find((x) => x.id === 'any-fetch')
      if (dm?.enabled && q.startsWith('demo ')) groups.push({ plugin: 'any-fetch', name: 'Demo', icon: 'globe', results: [{ title: 'Demo result for ' + q.slice(5), subtitle: 'https://example.com', url: 'https://example.com' }] })
      return send(res, 200, { groups }), true
    }
    if (p === '/api/plugins/preview' && m === 'POST') {
      if (!A()) return true
      const n = ['catalog', 'url', 'source'].filter((k) => body[k]).length
      if (n !== 1) return send(res, 400, { error: 'exactly one of source, url, catalog is required' }), true
      const lang = body.lang === 'ts' ? 'ts' : 'js'
      const out = (def, source) => {
        const rec = installed.find((x) => x.id === def.id)
        return send(res, 200, { plugin: { ...pub({ ...def, enabled: false }), enabled: false }, source, lang, installed: !!rec, ...(rec ? { installed_version: rec.version } : {}) })
      }
      if (body.catalog) {
        if (!DEFS[body.catalog]) return send(res, 404, { error: 'unknown catalog entry' }), true
        return out({ id: body.catalog, ...DEFS[body.catalog] }, srcOf(body.catalog, DEFS[body.catalog])), true
      }
      if (body.url) {
        if (!/^https?:\/\//.test(body.url)) return send(res, 400, { error: 'url must be http(s)' }), true
        const mk = MARKET_PLUGINS[body.url]
        if (mk) return mk.error ? (send(res, 200, { plugin: null, source: mk.source, lang, installed: false, error: mk.error }), true) : (out(mk.def, mk.source), true)
        let text
        try { const r = await fetch(body.url); if (!r.ok) throw new Error('HTTP ' + r.status); text = await r.text() } catch (e) { return send(res, 200, { plugin: null, source: '', lang, installed: false, error: 'download failed: ' + (e.message || 'error') }), true }
        const d = parseManifest(text)
        return d ? (out(d, text), true) : (send(res, 200, { plugin: null, source: text, lang, installed: false, error: 'invalid plugin: manifest: id is required' }), true)
      }
      const d = parseManifest(body.source)
      const bad = syntaxError(body.source)
      if (bad || !d) return send(res, 200, { plugin: null, source: body.source, lang, installed: false, error: bad || 'invalid plugin: manifest: id is required' }), true
      return out(d, body.source), true
    }
    if (p === '/api/plugins/market' && m === 'GET') {
      if (!A()) return true
      const sort = url.searchParams.get('sort') || 'stars'
      if (!['stars', 'updated'].includes(sort)) return send(res, 400, { error: 'sort must be stars or updated' }), true
      const q = (url.searchParams.get('q') || '').trim().toLowerCase()
      const pg = Math.min(30, Math.max(1, Number(url.searchParams.get('page')) || 1))
      await new Promise((r) => setTimeout(r, 350))
      if (q === 'ratelimit') return send(res, 200, { items: [], total: 0, cached_at: Date.now(), error: 'GitHub API rate limit exceeded. Try again in a few minutes, or set GENKAN_GITHUB_TOKEN on the server.' }), true
      let items = REPOS.filter((r) => !q || (r.repo + ' ' + r.description).toLowerCase().includes(q))
      items = items.slice().sort((a, b) => (sort === 'stars' ? b.stars - a.stars : new Date(b.updated_at) - new Date(a.updated_at)))
      const SIZE = 6 // the real server returns 20 per page; small here so "load more" can be tried
      return send(res, 200, { items: items.slice((pg - 1) * SIZE, pg * SIZE), total: items.length, cached_at: Date.now() }), true
    }
    if (p === '/api/plugins/market/cache/clear' && m === 'POST') {
      if (!A()) return true
      await new Promise((r) => setTimeout(r, 120))
      return send(res, 200, { ok: true }), true
    }
    if (p === '/api/plugins/market/repo' && m === 'GET') {
      if (!A()) return true
      const repo = url.searchParams.get('repo') || ''
      if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repo)) return send(res, 400, { error: 'invalid repo' }), true
      await new Promise((r) => setTimeout(r, 450))
      if (repo === 'ivan/genkan-flaky') return send(res, 200, { repo, default_branch: 'main', cached_at: Date.now(), plugins: [], error: 'GitHub API rate limit exceeded. Try again in a few minutes, or set GENKAN_GITHUB_TOKEN on the server.' }), true
      const plugins = (MARKET_SCAN[repo] || []).map((x) => {
        const rec = x.def && installed.find((i) => i.id === x.def.id)
        return { path: x.path, url: x.url, plugin: x.def ? { ...pub({ ...x.def, enabled: false }), enabled: false } : null, ...(x.error ? { error: x.error } : {}), installed: !!rec, ...(rec ? { installed_version: rec.version } : {}) }
      })
      return send(res, 200, { repo, default_branch: 'main', cached_at: Date.now(), plugins }), true
    }
    if (p === '/api/plugins/catalog' && m === 'GET') {
      if (!A()) return true
      const lg = (req.headers['accept-language'] || '').toLowerCase().startsWith('zh') ? 'zh' : 'en'
      const L = (v) => (typeof v === 'string' ? v : v?.[lg] ?? v?.en ?? '')
      return send(res, 200, CATALOG.map((c) => ({ id: c, name: L(DEFS[c].name), description: L(DEFS[c].description), icon: DEFS[c].icon, version: DEFS[c].version, installed: installed.some((x) => x.id === c), widgets: (DEFS[c].widgets || []).map((w) => ({ id: w.id, name: L(w.name), size: w.size })) }))), true
    }
    if ((mm = p.match(/^\/api\/plugins\/wallpaper\/([\w-]+)\/([\w-]+)$/)) && m === 'GET') {
      if (!R()) return true
      const pl = installed.find((x) => x.id === mm[1])
      if (!pl?.enabled || !pl.wallpapers?.some((w) => w.id === mm[2])) return send(res, 404, { error: 'unknown wallpaper source' }), true
      wpN++
      return send(res, 200, { url: `/uploads/plugin-wallpapers/art-${wpN}.svg`, title: 'Mock art #' + wpN, credit: `Mock art #${wpN} · ${en(pl.name)}`, link: 'https://example.com/art', updated_at: now() }), true
    }
    if (p === '/api/plugins' && m === 'GET') { if (!A()) return true; return send(res, 200, installed.map(adminInfo)), true }
    if (p === '/api/plugins' && m === 'POST') {
      if (!A()) return true
      let def
      let source = body.source
      if (body.catalog) {
        if (!DEFS[body.catalog]) return send(res, 404, { error: 'unknown catalog entry' }), true
        def = { id: body.catalog, ...DEFS[body.catalog] }
      } else if (body.url) {
        const mk = MARKET_PLUGINS[body.url]
        if (mk) {
          if (mk.error) return send(res, 400, { error: mk.error }), true
          def = { ...mk.def }; source = mk.source
        } else try { source = await (await fetch(body.url)).text() } catch { return send(res, 502, { error: 'mock: could not download ' + body.url }), true }
      }
      if (!def) {
        def = source && parseManifest(source)
        if (!def) return send(res, 400, { error: 'manifest: id is required' }), true
      }
      let rec = installed.find((x) => x.id === def.id)
      if (rec) Object.assign(rec, def, { config: rec.config, enabled: body.enable ?? rec.enabled, updated_at: now(), source_url: body.url || rec.source_url })
      else installed.push((rec = { ...def, enabled: body.enable ?? true, config: {}, secrets_set: [], source_url: body.url || '', last_error: '', installed_at: now(), updated_at: now(), source: source || srcOf(def.id, def) }))
      bump()
      return send(res, 200, adminInfo(rec)), true
    }
    if ((mm = p.match(/^\/api\/plugins\/([\w-]+)(?:\/(source|logs|test))?$/))) {
      if (!A()) return true
      const pl = installed.find((x) => x.id === mm[1])
      if (!pl) return send(res, 404, { error: 'plugin not found' }), true
      if (mm[2] === 'logs' && m === 'GET') return send(res, 200, { logs: (logs[pl.id] || []).map(([level, msg], i) => ({ ts: Date.now() - (i + 1) * 60000, level, msg })) }), true
      if (mm[2] === 'source' && m === 'GET') return send(res, 200, { source: pl.source, lang: pl.lang || 'js' }), true
      if (mm[2] === 'source' && m === 'PUT') {
        const src = String(body.source || '')
        const bad = syntaxError(src)
        if (bad) return send(res, 400, { error: bad }), true
        const d = parseManifest(src)
        if (!d) return send(res, 400, { error: 'invalid plugin: manifest: id is required' }), true
        if (d.id !== pl.id) return send(res, 400, { error: `invalid plugin: manifest.id "${d.id}" does not match "${pl.id}"` }), true
        Object.assign(pl, { name: d.name, version: d.version, description: d.description, permissions: d.permissions, source: src, lang: body.lang === 'ts' ? 'ts' : 'js', updated_at: now(), last_error: '' })
        bump()
        return send(res, 200, adminInfo(pl)), true
      }
      if (mm[2] === 'test' && m === 'POST') {
        const t0 = Date.now()
        await new Promise((r) => setTimeout(r, 250 + Math.random() * 300))
        const h = body.hook
        if (!['wallpaper', 'widget', 'search', 'schedule', 'action'].includes(h)) return send(res, 400, { error: 'unknown hook' }), true
        const logs = [{ ts: Date.now() - 40, level: 'info', msg: `test ${h}${body.target ? ' ' + body.target : ''} started` }]
        let result, error
        if (h === 'widget' || h === 'action') {
          const w = body.instance ? insts.find((x) => x.id === body.instance) : null
          const cfg = { ...(w?.config || {}), ...(body.config || {}) }
          if (!pl.widgets?.some((x) => x.id === body.target)) return send(res, 400, { error: 'unknown widget' }), true
          if (h === 'action') {
            if (!body.action) return send(res, 400, { error: 'action is required' }), true
            if (body.action === 'fail') { error = 'action failed: upstream returned 502'; logs.push({ ts: Date.now(), level: 'error', msg: error }) } else result = { toast: `Ran ${body.action}`, refresh: true }
          } else if (pl.id === 'pihole' && !cfg.token) { error = 'API token is not configured'; logs.push({ ts: Date.now(), level: 'warn', msg: 'token missing' }) }
          else { result = view({ plugin: pl.id, widget: body.target, config: cfg }, body.lang); logs.push({ ts: Date.now(), level: 'info', msg: `rendered ${result.body?.length || 0} blocks` }) }
        } else if (h === 'wallpaper') result = { url: 'https://picsum.photos/id/10/1600/900', title: 'Mock photo', credit: 'picsum.photos' }
        else if (h === 'search') { const v = pl.id === 'calculator' ? calc(body.query || '') : null; result = v !== null ? [{ title: '= ' + v, subtitle: body.query, copy: String(v) }] : [{ title: 'Result for ' + (body.query || ''), url: 'https://example.com' }] }
        else result = { imported: 3, skipped: 1 }
        return send(res, 200, { ok: !error, ...(error ? { error } : { result }), logs, ms: Date.now() - t0 }), true
      }
      if (m === 'PATCH') {
        if (typeof body.enabled === 'boolean') pl.enabled = body.enabled
        if (body.config && typeof body.config === 'object') {
          const sk = secretKeys(pl)
          for (const [k, v] of Object.entries(body.config)) {
            if (sk.includes(k)) {
              if (v === null) { delete pl.config[k]; pl.secrets_set = pl.secrets_set.filter((x) => x !== k) } else if (v !== '') { pl.config[k] = v; if (!pl.secrets_set.includes(k)) pl.secrets_set.push(k) }
            } else pl.config[k] = v
          }
        }
        pl.updated_at = now(); bump()
        return send(res, 200, adminInfo(pl)), true
      }
      if (m === 'DELETE') {
        installed.splice(installed.indexOf(pl), 1)
        for (let i = insts.length - 1; i >= 0; i--) if (insts[i].plugin === pl.id) insts.splice(i, 1)
        for (const w of workspaces()) if (w.wallpaper.startsWith(`plugin:${pl.id}/`)) w.wallpaper = 'preset:dusk'
        bump()
        return send(res, 200, { ok: true }), true
      }
    }
    if (p === '/api/widgets' && m === 'POST') {
      if (!A()) return true
      const pl = installed.find((x) => x.id === body.plugin)
      if (!pl?.widgets?.some((w) => w.id === body.widget)) return send(res, 400, { error: 'unknown widget' }), true
      const w = { id: id(), plugin: body.plugin, widget: body.widget, workspace_id: body.workspace_id || '', config: body.config || {}, sort: Math.max(-1, ...insts.map((x) => x.sort)) + 1, w: clampW(body.w), h: clampH(body.h) }
      for (const k of secretKeys(pl, w.widget)) if (w.config[k] === null) delete w.config[k]
      insts.push(w); bump()
      return send(res, 200, instPub(w, true)), true
    }
    if (p === '/api/widgets/reorder' && m === 'POST') { if (!A()) return true; body.ids.forEach((i, k) => { const w = insts.find((x) => x.id === i); if (w) w.sort = k }); bump(); return send(res, 200, { ok: true }), true }
    if ((mm = p.match(/^\/api\/widgets\/(\w+)(?:\/(data|action))?$/))) {
      const w = insts.find((x) => x.id === mm[1])
      if (mm[2] === 'data' && m === 'GET') {
        if (!R()) return true
        if (!w) return send(res, 404, { error: 'widget not found' }), true
        if (installed.find((x) => x.id === w.plugin)?.enabled === false) return send(res, 409, { error: 'plugin disabled' }), true
        if (w.plugin === 'pihole' && !w.config.token) return send(res, 200, { view: { title: 'Pi-hole', icon: 'shield', error: 'API token is not configured', body: [] }, updated_at: now(), next_at: now() }), true
        return send(res, 200, { view: view(w, req.headers['accept-language']), updated_at: now(), next_at: new Date(Date.now() + 60000).toISOString() }), true
      }
      if (!A()) return true
      if (!w) return send(res, 404, { error: 'widget not found' }), true
      if (mm[2] === 'action' && m === 'POST') {
        if (w.plugin === 'todo' || w.plugin === 'note') { await new Promise((r) => setTimeout(r, 60)); const [code, out] = act(w, body, req.headers['accept-language']); return send(res, code, out), true }
        if (body.action === 'fail') return send(res, 502, { error: 'action failed: upstream returned 502' }), true
        return send(res, 200, { toast: body.action === 'pause' ? 'Blocking paused for 5 minutes' : 'Refreshed', refresh: body.action !== 'pause' }), true
      }
      if (m === 'PATCH') {
        const pl = installed.find((x) => x.id === w.plugin)
        if (body.workspace_id !== undefined) w.workspace_id = body.workspace_id
        if (body.sort !== undefined) w.sort = body.sort
        if (body.w !== undefined) w.w = clampW(body.w)
        if (body.h !== undefined) w.h = clampH(body.h)
        if (body.config) {
          const sk = pl ? secretKeys(pl, w.widget) : []
          for (const [k, v] of Object.entries(body.config)) { if (sk.includes(k)) { if (v === null) delete w.config[k]; else if (v !== '') w.config[k] = v } else w.config[k] = v }
        }
        bump(); return send(res, 200, instPub(w, true)), true
      }
      if (m === 'DELETE') { insts.splice(insts.indexOf(w), 1); bump(); return send(res, 200, { ok: true }), true }
    }
    return false
  }
  return {
    handle,
    /** the `plugins` / `widgets` part of /api/bootstrap (guests see no config) */
    bootstrap: (r) => (r === 'none' ? { plugins: [], widgets: [] } : { plugins: installed.map(r === 'admin' ? adminInfo : pub), widgets: insts.map((w) => instPub(w, r === 'admin')) }),
    art: (n) => svg(Number(n)),
  }
}
