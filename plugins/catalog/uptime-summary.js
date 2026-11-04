// uptime-summary — how many of your links have health checks enabled.
//
// Hook:        widget(ctx) -> WidgetView
// Permissions: links: "read" unlocks genkan.links.list(), genkan.workspaces.list()
//              and genkan.categories.list() (all synchronous, and returning
//              the same objects as the REST API).
//
// The host API doesn't expose live up/down status, only the `health_check`
// flag of each link, so this widget honestly reports *monitoring coverage*:
// totals plus a per-workspace breakdown.

export const messages = {
  en: {
    name: "Uptime Summary",
    desc: "Links with health checks enabled, per workspace.",
    widget: "Health check coverage",
    title: "Monitoring",
    links: "Links",
    monitored: "Monitored",
    coverage: "Coverage",
    failed: "Could not read links: {msg}",
  },
  zh: {
    name: "健康检查概览",
    desc: "按工作空间统计已启用健康检查的链接。",
    widget: "健康检查覆盖率",
    title: "监控概览",
    links: "链接",
    monitored: "已监控",
    coverage: "监控覆盖率",
    failed: "读取链接失败：{msg}",
  },
};

export const manifest = {
  id: "uptime-summary",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  icon: "activity",
  permissions: { links: "read" },
  widgets: [{ id: "summary", name: t("widget"), size: "s", refresh: 300, config: [] }],
};

export async function widget(ctx) {
  const { t } = ctx;
  const title = t("title");
  try {
    const links = genkan.links.list();
    const monitored = links.filter((l) => l.health_check);
    const names = new Map(genkan.workspaces.list().map((w) => [w.id, w.name]));

    // Per-workspace "monitored / total", busiest first, at most 8 rows.
    const per = new Map();
    for (const l of links) {
      const row = per.get(l.workspace_id) || { total: 0, on: 0 };
      row.total++;
      if (l.health_check) row.on++;
      per.set(l.workspace_id, row);
    }
    const rows = [...per.entries()].sort((a, b) => b[1].on - a[1].on || b[1].total - a[1].total).slice(0, 8);

    const body = [
      {
        type: "stats",
        items: [
          { label: t("links"), value: links.length },
          { label: t("monitored"), value: monitored.length, icon: "activity" },
        ],
      },
      {
        type: "progress",
        label: t("coverage"),
        value: links.length ? monitored.length / links.length : 0,
        text: links.length ? Math.round((monitored.length / links.length) * 100) + "%" : "-",
      },
    ];
    if (rows.length > 1) {
      body.push({ type: "kv", items: rows.map(([id, r]) => ({ k: names.get(id) || id, v: `${r.on} / ${r.total}` })) });
    }
    return { title, icon: "activity", body };
  } catch (e) {
    return { title, icon: "activity", body: [], error: t("failed", { msg: e && e.message ? e.message : e }) };
  }
}
