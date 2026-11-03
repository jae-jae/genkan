// pihole — Pi-hole statistics plus a "pause blocking" button.
//
// Hooks:   widget(ctx) -> WidgetView, action(ctx) -> { toast, refresh }
// Network: the Pi-hole address you type in the widget settings. It is an
//          `allowFetch` url field, so Genkan whitelists exactly that host:port
//          (permissions.fetch stays empty).
//
// Works with both generations of Pi-hole:
//   v6  POST /api/auth {password} -> session id, then GET /api/stats/summary
//       and /api/dns/blocking with a `sid` header; the session is closed again
//       (DELETE /api/auth) because v6 only allows a limited number of seats.
//       Use the web password or an "app password".
//   v5  GET /admin/api.php?summaryRaw&auth=TOKEN  (token = Settings > API).
// The one secret field serves both: password for v6, API token for v5. The
// version is detected by probing /api/auth, which only v6 answers with JSON.

export const messages = {
  en: {
    name: "Pi-hole",
    desc: "Pi-hole queries, blocked count and pause button.",
    widget: "Pi-hole stats",
    url: "Pi-hole URL",
    urlHelp: "e.g. http://192.168.1.2 or http://pi.hole",
    password: "Password / API token",
    passwordHelp: "v6: web password or app password. v5: API token. May be empty if none is set.",
    setUrl: "Set the Pi-hole URL in the widget settings",
    queries: "Queries",
    blocked: "Blocked",
    rate: "Block rate",
    status: "Status",
    blocking: "Blocking",
    paused: "Paused",
    blocklist: "Blocklist",
    clients: "Clients",
    pause: "Pause blocking for 5 min",
    resume: "Resume blocking",
    unreachable: "Cannot reach Pi-hole: {msg}",
    pausedToast: "Blocking paused for 5 minutes",
    resumedToast: "Blocking resumed",
  },
  zh: {
    name: "Pi-hole",
    desc: "Pi-hole 查询与拦截统计，带暂停按钮。",
    widget: "Pi-hole 统计",
    url: "Pi-hole 地址",
    urlHelp: "例如 http://192.168.1.2 或 http://pi.hole",
    password: "密码 / API 令牌",
    passwordHelp: "v6：网页密码或应用密码。v5：API 令牌。未设置时可留空。",
    setUrl: "请先设置 Pi-hole 地址",
    queries: "今日查询",
    blocked: "已拦截",
    rate: "拦截率",
    status: "状态",
    blocking: "拦截中",
    paused: "已暂停",
    blocklist: "规则域名",
    clients: "活跃客户端",
    pause: "暂停拦截 5 分钟",
    resume: "恢复拦截",
    unreachable: "无法连接 Pi-hole：{msg}",
    pausedToast: "已暂停拦截 5 分钟",
    resumedToast: "已恢复拦截",
  },
};

export const manifest = {
  id: "pihole",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  homepage: "https://pi-hole.net",
  icon: "shield",
  permissions: { fetch: [] },
  widgets: [
    {
      id: "stats",
      name: t("widget"),
      size: "m",
      refresh: 60,
      config: [
        { key: "url", type: "url", label: t("url"), help: t("urlHelp"), required: true, allowFetch: true },
        { key: "password", type: "secret", label: t("password"), help: t("passwordHelp") },
      ],
    },
  ],
};

const PAUSE_SECONDS = 300;

async function call(url, init = {}) {
  const res = await fetch(url, init);
  let body = null;
  try {
    body = await res.json();
  } catch (e) {
    // not JSON (e.g. an HTML 404 page from a v5 box)
  }
  return { status: res.status, body };
}

const JSON_HEADERS = { "Content-Type": "application/json" };

// Log in and return a small client: { version, summary(), setBlocking(), close() }.
async function connect(rawUrl, secret) {
  const base = String(rawUrl).replace(/\/+$/, "").replace(/\/admin$/, "");

  // --- v6 -----------------------------------------------------------------
  const auth = await call(base + "/api/auth", { method: "POST", headers: JSON_HEADERS, body: JSON.stringify({ password: secret }) });
  const session = auth.body && auth.body.session;
  if (session) {
    if (!session.valid) throw new Error(session.message || "authentication failed (check the password)");
    const h = session.sid ? { sid: session.sid } : {}; // no password set -> no sid
    return {
      version: 6,
      async summary() {
        const [s, b] = await Promise.all([call(base + "/api/stats/summary", { headers: h }), call(base + "/api/dns/blocking", { headers: h })]);
        if (!s.body || !s.body.queries) throw new Error("unexpected response (HTTP " + s.status + ")");
        return {
          total: s.body.queries.total,
          blocked: s.body.queries.blocked,
          percent: s.body.queries.percent_blocked,
          domains: s.body.gravity && s.body.gravity.domains_being_blocked,
          clients: s.body.clients && s.body.clients.active,
          enabled: !b.body || b.body.blocking !== "disabled",
        };
      },
      async setBlocking(enabled, seconds) {
        const r = await call(base + "/api/dns/blocking", {
          method: "POST",
          headers: { ...JSON_HEADERS, ...h },
          body: JSON.stringify({ blocking: enabled, timer: enabled ? null : seconds }),
        });
        if (!r.body || r.body.blocking === undefined) throw new Error("Pi-hole refused (HTTP " + r.status + ")");
      },
      async close() {
        if (session.sid) await call(base + "/api/auth", { method: "DELETE", headers: h }).catch(() => {});
      },
    };
  }

  // --- v5 -----------------------------------------------------------------
  const api = (query) => `${base}/admin/api.php?${query}&auth=${encodeURIComponent(secret)}`;
  return {
    version: 5,
    async summary() {
      const s = await call(api("summaryRaw"));
      if (!s.body || s.body.dns_queries_today === undefined) throw new Error("not a Pi-hole at that address (HTTP " + s.status + ")");
      return {
        total: s.body.dns_queries_today,
        blocked: s.body.ads_blocked_today,
        percent: s.body.ads_percentage_today,
        domains: s.body.domains_being_blocked,
        clients: s.body.unique_clients,
        enabled: s.body.status === "enabled",
      };
    },
    async setBlocking(enabled, seconds) {
      const r = await call(api(enabled ? "enable" : "disable=" + seconds));
      if (!r.body || !r.body.status) throw new Error("Pi-hole refused (check the API token)");
    },
    async close() {},
  };
}

const errText = (e) => (e && e.message ? e.message : String(e));

export async function widget(ctx) {
  const { t, locale } = ctx;
  const cfg = (ctx.instance && ctx.instance.config) || {};
  const view = { title: "Pi-hole", icon: "shield", body: [] };
  if (!cfg.url) return { ...view, error: t("setUrl") };
  view.link = String(cfg.url).replace(/\/+$/, "").replace(/\/admin$/, "") + "/admin";

  let client;
  try {
    client = await connect(cfg.url, cfg.password || "");
    const s = await client.summary();
    const n = (x) => (typeof x === "number" ? x.toLocaleString(locale) : String(x ?? "-"));
    const pct = Number(s.percent) || 0;

    view.body = [
      {
        type: "stats",
        items: [
          { label: t("queries"), value: n(s.total) },
          { label: t("blocked"), value: n(s.blocked) },
          { label: t("rate"), value: pct.toFixed(1), unit: "%" },
        ],
      },
      { type: "progress", value: Math.min(1, Math.max(0, pct / 100)), text: pct.toFixed(1) + "%" },
      {
        type: "kv",
        items: [
          { k: t("status"), v: s.enabled ? t("blocking") : t("paused") },
          { k: t("blocklist"), v: n(s.domains) },
          { k: t("clients"), v: n(s.clients) },
          { k: "Pi-hole", v: "v" + client.version },
        ],
      },
      s.enabled
        ? { type: "button", label: t("pause"), action: "pause", icon: "pin-off" }
        : { type: "button", label: t("resume"), action: "resume", icon: "check" },
    ];
  } catch (e) {
    view.error = t("unreachable", { msg: errText(e) });
  } finally {
    if (client) await client.close();
  }
  return view;
}

// Button handler: ctx.action is "pause" or "resume".
export async function action(ctx) {
  const { t } = ctx;
  const cfg = (ctx.instance && ctx.instance.config) || {};
  if (!cfg.url) throw new Error("Pi-hole URL is not configured");
  if (ctx.action !== "pause" && ctx.action !== "resume") throw new Error("unknown action: " + ctx.action);

  const client = await connect(cfg.url, cfg.password || "");
  try {
    await client.setBlocking(ctx.action === "resume", PAUSE_SECONDS);
  } finally {
    await client.close();
  }
  genkan.log("pihole:", ctx.action);
  return {
    toast: t(ctx.action === "pause" ? "pausedToast" : "resumedToast"),
    refresh: true,
  };
}
