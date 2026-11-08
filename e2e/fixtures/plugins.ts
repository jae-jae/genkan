// Deterministic plugin & widget seeding for screenshots.spec.ts.
// Built-in plugins come from plugins/catalog/; third-party showcase plugins come from a sibling
// genkan-plugins checkout (or GENKAN_PLUGINS_DIR, falling back to GitHub raw if absent). Network
// and SSH calls are shimmed at module scope so every chart, stat and timestamp renders offline.
import { createHash } from 'node:crypto'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Api } from '../helpers'

const here = dirname(fileURLToPath(import.meta.url))
const CATALOG_DIR = resolve(here, '../../plugins/catalog')
const THIRD_PARTY_DIR = process.env.GENKAN_PLUGINS_DIR || resolve(here, '../../../genkan-plugins/plugins')
const THIRD_PARTY_RAW = 'https://raw.githubusercontent.com/jae-jae/genkan-plugins/main/plugins'

export const SHOTS_NOW_MS = Date.parse('2026-11-04T01:41:00Z') // 09:41 Asia/Shanghai, Nov 4 UTC

function wrapPlugin(src: string, prelude = ''): string {
  let out = src
  for (const fn of ['widget', 'action', 'schedule', 'search', 'wallpaper']) {
    out = out
      .replace(new RegExp(`export\\s+async\\s+function\\s+${fn}\\s*\\(`), `async function __orig_${fn}(`)
      .replace(new RegExp(`export\\s+function\\s+${fn}\\s*\\(`), `function __orig_${fn}(`)
  }
  const exports = ['widget', 'action', 'schedule', 'search', 'wallpaper']
    .filter((fn) => out.includes(`function __orig_${fn}(`))
    .map(
      (fn) => `export async function ${fn}(ctx) {
  const c = ctx ? Object.create(ctx) : {};
  Object.defineProperty(c, "now", { value: ${SHOTS_NOW_MS}, enumerable: true, configurable: true });
  return __orig_${fn}(c);
}`,
    )
    .join('\n')
  return `${prelude}\n${out}\n${exports}\n`
}

async function readThirdParty(file: string): Promise<string | null> {
  const local = resolve(THIRD_PARTY_DIR, file)
  if (existsSync(local)) return readFileSync(local, 'utf8')
  try {
    const r = await fetch(`${THIRD_PARTY_RAW}/${file}`, { signal: AbortSignal.timeout(5000) })
    if (r.ok) return await r.text()
  } catch {
    /* offline without sibling checkout: skip third-party plugin */
  }
  return null
}

export async function seedPluginsAndWidgets(api: Api, wsKey: (k: string) => string, zh: boolean): Promise<{ hasThirdParty: boolean }> {
  for (const id of ['todo', 'note', 'calculator', 'uptime-summary']) {
    await api.call('POST', '/api/plugins', { catalog: id, enable: true })
  }
  for (const id of ['world-clock', 'countdown']) {
    const raw = readFileSync(resolve(CATALOG_DIR, `${id}.js`), 'utf8')
    await api.call('POST', '/api/plugins', { source: wrapPlugin(raw), enable: true })
  }

  // Shimmed weather (Open-Meteo)
  {
    const raw = readFileSync(resolve(CATALOG_DIR, 'weather.js'), 'utf8')
    const cityName = zh ? '东京' : 'Tokyo'
    const countryName = zh ? '日本' : 'Japan'
    const prelude = `
const fetch = async (url) => {
  if (url.includes("geocoding-api.open-meteo.com")) {
    return { ok: true, status: 200, json: async () => ({ results: [{ latitude: 35.68, longitude: 139.76, name: ${JSON.stringify(cityName)}, country: ${JSON.stringify(countryName)} }] }) };
  }
  return {
    ok: true, status: 200,
    json: async () => ({
      current: { temperature_2m: 18.4, apparent_temperature: 17.8, relative_humidity_2m: 52, weather_code: 1, wind_speed_10m: 11.2, is_day: 1 },
      daily: {
        time: ["2026-11-04", "2026-11-05", "2026-11-06", "2026-11-07", "2026-11-08", "2026-11-09", "2026-11-10"],
        temperature_2m_max: [19, 21, 20, 17, 18, 22, 21],
        temperature_2m_min: [11, 12, 13, 10, 11, 13, 12],
        weather_code: [1, 2, 0, 3, 1, 0, 1]
      }
    })
  };
};`
    await api.call('POST', '/api/plugins', { source: wrapPlugin(raw, prelude), enable: true })
  }

  // Shimmed Pi-hole v6
  {
    const raw = readFileSync(resolve(CATALOG_DIR, 'pihole.js'), 'utf8')
    const prelude = `
const fetch = async (url) => {
  const j = (body) => ({ ok: true, status: 200, json: async () => body, text: async () => JSON.stringify(body) });
  if (url.endsWith("/api/auth")) return j({ session: { valid: true, sid: "demo-sid" } });
  if (url.endsWith("/api/stats/summary")) return j({ queries: { total: 48210, blocked: 12480, percent_blocked: 25.9 }, gravity: { domains_being_blocked: 284512 }, clients: { active: 24 } });
  if (url.endsWith("/api/dns/blocking")) return j({ blocking: "enabled" });
  return j({});
};`
    await api.call('POST', '/api/plugins', { source: wrapPlugin(raw, prelude), enable: true })
  }

  // ---- Optional third-party showcase plugins (jae-jae/genkan-plugins) ----
  let hasThirdParty = true
  const sshRaw = await readThirdParty('ssh-server.js')
  if (sshRaw) {
    const pts = [22.4, 24.1, 23.0, 26.8, 31.2, 29.5, 27.1, 25.4, 24.8, 28.9, 34.2, 32.0, 29.8, 26.3, 25.1, 27.4, 30.8, 28.1, 26.9, 25.7, 29.2, 31.6, 28.7, 27.9]
    const hist = pts.map((v, i) => [SHOTS_NOW_MS - (pts.length - i) * 60_000, v])
    const prelude = `
const __realGenkan = globalThis.genkan;
const genkan = Object.assign(Object.create(__realGenkan), {
  ssh: {
    stats: async () => ({
      hostname: "pve.lan", kernel: "6.8.12-4-pve", arch: "x86_64",
      collectedAt: ${SHOTS_NOW_MS}, cores: 8, cpuPercent: 28.4,
      uptimeSeconds: 42 * 86400 + 11 * 3600 + 18 * 60,
      load: [1.14, 0.98, 0.85],
      memory: { percent: 58.1, usedBytes: Math.round(18.6 * 1073741824), totalBytes: 32 * 1073741824 },
      disk: { percent: 41.2, usedBytes: 412 * 1073741824, totalBytes: 1000 * 1073741824 },
      network: { rxBytesPerSecond: Math.round(14.2 * 1048576), txBytesPerSecond: Math.round(6.8 * 1048576) },
    })
  }
});`
    const wrapped = wrapPlugin(sshRaw, prelude).replace(
      'return __orig_widget(c);',
      `if (c.instance && c.instance.storage && !c.instance.storage.get("snapshot")) {
    const sc = Object.create(c.instance);
    const origGet = c.instance.storage.get.bind(c.instance.storage);
    sc.storage = Object.assign(Object.create(c.instance.storage), {
      get: (k) => k === "snapshot" ? (origGet(k) || { identity: JSON.stringify(["pve.lan", 22, "monitor"]), history: ${JSON.stringify(hist)} }) : origGet(k)
    });
    Object.defineProperty(c, "instance", { value: sc, enumerable: true, configurable: true });
  }
  return __orig_widget(c);`,
    )
    await api.call('POST', '/api/plugins', { source: wrapped, enable: true })
  } else {
    hasThirdParty = false
  }

  const subRaw = await readThirdParty('airport-subscription.js')
  if (subRaw) {
    const subUrl = 'https://sub.example.com/demo'
    const subSrc = createHash('sha256').update(subUrl + '\nAsia/Shanghai').digest('hex')
    const nowSec = Math.floor(SHOTS_NOW_MS / 1000)
    const GB = 1073741824
    const dailyGB = [11.2, 8.4, 14.6, 9.1, 16.3, 12.8, 6.4]
    const days = dailyGB.map((gb, i) => {
      const dt = new Date(SHOTS_NOW_MS - (6 - i) * 86400_000)
      return [dt.toISOString().slice(0, 10), Math.round(gb * GB), nowSec - (6 - i) * 86400]
    })
    const pts = days.map((d, i) => [d[2], Math.round((510 + i * 12) * GB)])
    const fakeState = {
      v: 1, src: subSrc, at: nowSec, ok: nowSec, view: '7d', err: null,
      snap: { u: Math.round(142 * GB), d: Math.round(446 * GB), t: 1000 * GB, e: nowSec + 185 * 86400, n: 'Cloud Relay', r: 16 },
      pts, days,
    }
    const prelude = `
const __realGenkan = globalThis.genkan;
const __subFallback = ${JSON.stringify(fakeState)};
const genkan = Object.assign(Object.create(__realGenkan), {
  storage: Object.assign(Object.create(__realGenkan.storage), {
    get: (k) => { const v = __realGenkan.storage.get(k); return v !== undefined ? v : (k.startsWith("sub:") ? JSON.parse(JSON.stringify(__subFallback)) : undefined); }
  })
});
const fetch = async () => ({
  ok: true, status: 200,
  headers: { get: (h) => ({ "subscription-userinfo": "upload=${Math.round(142 * GB)}; download=${Math.round(446 * GB)}; total=${1000 * GB}; expire=${nowSec + 185 * 86400}", "profile-title": "Cloud Relay", "profile-reset-day": "16" }[h.toLowerCase()] || null) }
});`
    await api.call('POST', '/api/plugins', { source: wrapPlugin(subRaw, prelude), enable: true })
  } else {
    hasThirdParty = false
  }

  const stockRaw = await readThirdParty('stock-trend.js')
  if (stockRaw) {
    const closes = [18412, 18480, 18390, 18520, 18610, 18540, 18690, 18745, 18680, 18810, 18890, 18830, 18940, 19015, 18970, 19085, 19140, 19090, 19195, 19240, 19286.78]
    const nowSec = Math.floor(SHOTS_NOW_MS / 1000)
    const times = closes.map((_, i) => nowSec - (closes.length - 1 - i) * 86400)
    const prelude = `
const fetch = async () => ({
  ok: true, status: 200,
  json: async () => ({
    chart: {
      result: [{
        meta: { regularMarketPrice: 19286.78, regularMarketTime: ${nowSec}, previousClose: 19016.42, currency: "USD", instrumentType: "INDEX" },
        timestamp: ${JSON.stringify(times)},
        indicators: { quote: [{ close: ${JSON.stringify(closes)} }] }
      }]
    }
  })
});`
    await api.call('POST', '/api/plugins', { source: wrapPlugin(stockRaw, prelude), enable: true })
  } else {
    hasThirdParty = false
  }

  const codexRaw = await readThirdParty('codex-usage.js')
  if (codexRaw) {
    const nowSec = Math.floor(SHOTS_NOW_MS / 1000)
    const prelude = `
const fetch = async () => ({
  ok: true, status: 200,
  json: async () => ({
    plan_type: "pro",
    rate_limit: {
      allowed: true,
      primary_window: { used_percent: 8, limit_window_seconds: 18000, reset_at: ${nowSec + 3 * 3600 + 40 * 60} },
      secondary_window: { used_percent: 24, limit_window_seconds: 604800, reset_at: ${nowSec + 4 * 86400 + 7 * 3600} }
    },
    code_review_rate_limit: {
      allowed: true,
      primary_window: { used_percent: 4, limit_window_seconds: 604800, reset_at: ${nowSec + 5 * 86400 + 2 * 3600} }
    },
    credits: { has_credits: true, unlimited: false, balance: "42.50" }
  })
});`
    await api.call('POST', '/api/plugins', { source: wrapPlugin(codexRaw, prelude), enable: true })
    await api.call('PATCH', '/api/plugins/codex-usage', { config: { access_token: 'eyJhbGciOiJub25lIn0.eyJleHAiOjE4OTM0NTYwMDB9.demo' } })
  } else {
    hasThirdParty = false
  }

  const btcRaw = await readThirdParty('bitcoin-price.js')
  if (btcRaw) {
    const prelude = `
const fetch = async () => ({
  ok: true, status: 200,
  json: async () => ({
    last_updated: "${new Date(SHOTS_NOW_MS - 120_000).toISOString()}",
    quotes: { USD: { price: 76420.5, percent_change_24h: 2.84 }, CNY: { price: 546180.0, percent_change_24h: 2.84 } }
  })
});`
    await api.call('POST', '/api/plugins', { source: wrapPlugin(btcRaw, prelude), enable: true })
  } else {
    hasThirdParty = false
  }

  // ---- Home workspace: 1 clean row of 3 equal-height widgets ----
  await api.call('POST', '/api/widgets', { plugin: 'weather', widget: 'current', workspace_id: wsKey('home'), w: 2, h: 328, config: { city: 'Tokyo', unit: 'c' } })
  const wTodo = await api.call<{ id: string }>('POST', '/api/widgets', { plugin: 'todo', widget: 'list', workspace_id: wsKey('home'), w: 2, h: 328, config: {} })
  const wNote = await api.call<{ id: string }>('POST', '/api/widgets', { plugin: 'note', widget: 'note', workspace_id: wsKey('home'), w: 2, h: 328, config: { tone: 'purple' } })

  const tasks = zh
    ? ['升级 Proxmox VE 内核', '续签 *.lan 内网证书', '检查 Frigate 录像盘空间', '验证异地 restic 备份']
    : ['Update Proxmox VE kernel', 'Renew *.lan TLS certificate', 'Check Frigate NVR storage', 'Verify offsite restic backup']
  for (const task of tasks) {
    await api.call('POST', `/api/widgets/${wTodo.id}/action`, { action: 'add', params: { value: task } })
  }
  const todoData = await api.call<{ view: { body: { type: string; items?: { id: string }[] }[] } }>('GET', `/api/widgets/${wTodo.id}/data`)
  const todoItems = todoData.view.body.find((b) => b.type === 'list')?.items || []
  if (todoItems[3]) {
    await api.call('POST', `/api/widgets/${wTodo.id}/action`, { action: 'toggle', params: { id: todoItems[3].id, checked: true } })
  }
  await api.call('POST', `/api/widgets/${wNote.id}/action`, {
    action: 'save',
    params: {
      value: zh
        ? '### Homelab 备忘\n- `pve.lan` → `10.0.10.2` (Proxmox)\n- `nas.lan` → `10.0.10.5` (TrueNAS)\n- `dns.lan` → Pi-hole + Unbound\n- `ha.lan` → Zigbee 协调器已就绪\n- 每日 `03:00` 自动执行 restic 备份\n- [x] 同步 Immich 家庭照片库\n- [x] 验证 UPS 断电自动关机\n- [ ] 续签 `*.lan` 内网 TLS 证书\n- [ ] 演练 restic 异地灾备恢复'
        : '### Homelab Notes\n- `pve.lan` → `10.0.10.2` (Proxmox)\n- `nas.lan` → `10.0.10.5` (TrueNAS)\n- `dns.lan` → Pi-hole + Unbound\n- `ha.lan` → Zigbee coordinator online\n- Nightly restic snapshot at `03:00`\n- [x] Sync Immich photo library\n- [x] Verify UPS graceful shutdown\n- [ ] Renew `*.lan` TLS certificate\n- [ ] Test offsite restic restore',
    },
  })

  // ---- Dev workspace: rich widget showcase (columns bottom-aligned) ----
  if (sshRaw) {
    await api.call('POST', '/api/widgets', {
      plugin: 'ssh-server', widget: 'resources', workspace_id: wsKey('dev'), w: 2,
      config: { name: 'Proxmox VE · pve.lan', host: 'pve.lan', port: 22, username: 'monitor', password: 'demo-password' },
    })
  }
  if (subRaw) {
    await api.call('POST', '/api/widgets', {
      plugin: 'airport-subscription', widget: 'sub', workspace_id: wsKey('dev'), w: 2,
      config: { url: 'https://sub.example.com/demo', name: zh ? '云中继节点' : 'Cloud Relay', resetDay: 16, view: '7d', zone: 'Asia/Shanghai' },
    })
  }
  if (stockRaw) {
    await api.call('POST', '/api/widgets', {
      plugin: 'stock-trend', widget: 'market', workspace_id: wsKey('dev'), w: 2, h: 404,
      config: { symbol: '^IXIC', period: '1mo' },
    })
  }
  if (codexRaw) {
    await api.call('POST', '/api/widgets', {
      plugin: 'codex-usage', widget: 'usage', workspace_id: wsKey('dev'), w: 2, h: 356,
      config: { timezone: 'Asia/Shanghai', mode: 'remaining' },
    })
  }
  await api.call('POST', '/api/widgets', {
    plugin: 'pihole', widget: 'stats', workspace_id: wsKey('dev'), w: 2, h: 316,
    config: { url: 'http://pihole.lan', password: 'demo' },
  })
  if (btcRaw) {
    await api.call('POST', '/api/widgets', {
      plugin: 'bitcoin-price', widget: 'price', workspace_id: wsKey('dev'), w: 2, h: 224,
      config: { currency: 'usd' },
    })
  } else {
    await api.call('POST', '/api/widgets', { plugin: 'uptime-summary', widget: 'summary', workspace_id: wsKey('dev'), w: 2, config: {} })
    await api.call('POST', '/api/widgets', { plugin: 'world-clock', widget: 'clock', workspace_id: wsKey('dev'), w: 2, config: {} })
  }

  return { hasThirdParty }
}
