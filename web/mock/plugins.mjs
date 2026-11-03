// Dev-only mock of the plugin endpoints (docs/PLUGINS.md §5): a fake weather widget (every Block type), a fake wallpaper source
// (generated SVGs), a fake calculator search, plus a few more catalog entries. Imported by server.mjs; not shipped.
const id = () => Math.random().toString(36).slice(2, 14).padEnd(12, 'a')
const now = () => new Date().toISOString()

const F = (key, type, label, o = {}) => ({ key, type, label, ...o })
const DEFS = {
  weather: {
    name: 'Weather', version: '1.0.0', icon: 'cloud-sun', description: 'Current weather and a 7-day trend from Open-Meteo (no API key).', author: 'genkan', homepage: 'https://open-meteo.com',
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
    name: 'World clock', version: '1.0.0', icon: 'clock', description: 'Time in several time zones. No network access.', author: 'genkan',
    permissions: { fetch: [], links: '' }, config_schema: [],
    widgets: [{ id: 'clock', name: 'World clock', size: 's', refresh: 60, config: [F('zones', 'text', 'Time zones', { default: 'Asia/Tokyo\nEurope/Berlin\nAmerica/New_York', help: 'One IANA zone per line' })] }],
  },
  pihole: {
    name: 'Pi-hole', version: '1.0.0', icon: 'shield', description: 'Queries and blocked count from your Pi-hole, with a pause button.', author: 'genkan',
    permissions: { fetch: [], links: '' }, config_schema: [],
    widgets: [{ id: 'stats', name: 'Pi-hole stats', size: 's', refresh: 60, config: [F('url', 'url', 'Pi-hole address', { required: true, allowFetch: true, default: 'http://pihole.lan' }), F('token', 'secret', 'API token', { required: true }), F('verbose', 'boolean', 'Show details', { default: true })] }],
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
const CATALOG = ['weather', 'calculator', 'picsum', 'bing-wallpaper', 'hacker-news', 'world-clock', 'pihole', 'link-importer', 'any-fetch']

export function createPluginsMock({ send, workspaces, bump }) {
  const installed = []
  const insts = []
  const sources = {}
  const add = (pid, enabled, config = {}) => {
    const def = DEFS[pid]
    installed.push({ id: pid, ...def, enabled, config, secrets_set: [], source_url: '', last_error: '', installed_at: now(), updated_at: now(), source: `// mock source for ${pid}\nexport const manifest = ${JSON.stringify({ id: pid, name: def.name })}\n` })
    return installed[installed.length - 1]
  }
  const addWidget = (plugin, widget, workspace_id, config = {}) => insts.push({ id: id(), plugin, widget, workspace_id, config, sort: insts.length })
  add('weather', true, { unit: 'c' })
  add('calculator', true)
  add('picsum', true)
  add('world-clock', true)
  const pi = add('pihole', true)
  pi.secrets_set = []
  add('any-fetch', false)
  const logs = { weather: [['info', 'fetched forecast for Tokyo (212 ms)']], pihole: [['warn', 'token missing'], ['error', 'fetch http://pihole.lan failed: connection refused']] }
  pi.last_error = 'fetch http://pihole.lan failed: connection refused'
  const seedWidgets = () => {
    const ws = workspaces()
    addWidget('weather', 'current', '', { city: 'Tokyo', days: 7 })
    addWidget('world-clock', 'clock', ws[0].id, { zones: 'Asia/Tokyo\nEurope/Berlin\nAmerica/New_York' })
    addWidget('pihole', 'stats', ws[0].id, { url: 'http://pihole.lan', token: '', verbose: true })
  }
  seedWidgets()

  const pub = (p) => ({
    id: p.id, name: p.name, version: p.version, description: p.description || '', author: p.author || '', homepage: p.homepage || '', icon: p.icon || '', enabled: p.enabled,
    permissions: { fetch: p.permissions?.fetch || [], links: p.permissions?.links || '' }, config_schema: p.config_schema || [],
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

  const spark = (n, base, amp) => Array.from({ length: n }, (_, i) => Math.round((base + Math.sin(i * 0.9 + 1) * amp + (Math.random() - 0.5) * 2) * 10) / 10)
  const view = (w, lang) => {
    const zh = (lang || '').startsWith('zh')
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
    return { title: w.widget, body: [{ type: 'text', text: 'mock' }] }
  }

  const parseManifest = (src) => {
    const m = (re) => src.match(re)?.[1]
    const pid = m(/\bid:\s*["']([a-z0-9][a-z0-9-]+)["']/)
    if (!pid) return null
    const fetchList = (m(/\bfetch:\s*\[([^\]]*)\]/) || '').split(',').map((s) => s.trim().replace(/^["']|["']$/g, '')).filter(Boolean)
    return {
      id: pid, name: m(/\bname:\s*["']([^"']+)["']/) || pid, version: m(/\bversion:\s*["']([^"']+)["']/) || '0.0.1', description: m(/\bdescription:\s*["']([^"']+)["']/) || '', author: '', icon: '',
      permissions: { fetch: fetchList, links: m(/\blinks:\s*["'](read|write)["']/) || '' }, config_schema: [],
      search: /export\s+(async\s+)?function\s+search/.test(src) ? { name: m(/\bname:\s*["']([^"']+)["']/) || pid, prefix: '' } : null,
    }
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
    if (p === '/api/plugins/catalog' && m === 'GET') {
      if (!A()) return true
      return send(res, 200, CATALOG.map((c) => ({ id: c, name: DEFS[c].name, description: DEFS[c].description, icon: DEFS[c].icon, version: DEFS[c].version, installed: installed.some((x) => x.id === c) }))), true
    }
    if ((mm = p.match(/^\/api\/plugins\/wallpaper\/([\w-]+)\/([\w-]+)$/)) && m === 'GET') {
      if (!R()) return true
      const pl = installed.find((x) => x.id === mm[1])
      if (!pl?.enabled || !pl.wallpapers?.some((w) => w.id === mm[2])) return send(res, 404, { error: 'unknown wallpaper source' }), true
      wpN++
      return send(res, 200, { url: `/uploads/plugin-wallpapers/art-${wpN}.svg`, title: 'Mock art #' + wpN, credit: `Mock art #${wpN} · ${pl.name}`, link: 'https://example.com/art', updated_at: now() }), true
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
        try { source = await (await fetch(body.url)).text() } catch { return send(res, 502, { error: 'mock: could not download ' + body.url }), true }
      }
      if (!def) {
        def = source && parseManifest(source)
        if (!def) return send(res, 400, { error: 'manifest: id is required' }), true
      }
      let rec = installed.find((x) => x.id === def.id)
      if (rec) Object.assign(rec, def, { config: rec.config, enabled: body.enable ?? rec.enabled, updated_at: now(), source_url: body.url || rec.source_url })
      else installed.push((rec = { ...def, enabled: body.enable ?? true, config: {}, secrets_set: [], source_url: body.url || '', last_error: '', installed_at: now(), updated_at: now(), source: source || `// mock source for ${def.id}\n` }))
      bump()
      return send(res, 200, adminInfo(rec)), true
    }
    if ((mm = p.match(/^\/api\/plugins\/([\w-]+)(?:\/(source|logs|test))?$/))) {
      if (!A()) return true
      const pl = installed.find((x) => x.id === mm[1])
      if (!pl) return send(res, 404, { error: 'plugin not found' }), true
      if (mm[2] === 'logs' && m === 'GET') return send(res, 200, { logs: (logs[pl.id] || []).map(([level, msg], i) => ({ ts: Date.now() - (i + 1) * 60000, level, msg })) }), true
      if (mm[2] === 'source' && m === 'GET') return send(res, 200, { source: pl.source, lang: 'js' }), true
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
      const w = { id: id(), plugin: body.plugin, widget: body.widget, workspace_id: body.workspace_id || '', config: body.config || {}, sort: Math.max(-1, ...insts.map((x) => x.sort)) + 1 }
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
        if (body.action === 'fail') return send(res, 502, { error: 'action failed: upstream returned 502' }), true
        return send(res, 200, { toast: body.action === 'pause' ? 'Blocking paused for 5 minutes' : 'Refreshed', refresh: body.action !== 'pause' }), true
      }
      if (m === 'PATCH') {
        const pl = installed.find((x) => x.id === w.plugin)
        if (body.workspace_id !== undefined) w.workspace_id = body.workspace_id
        if (body.sort !== undefined) w.sort = body.sort
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
