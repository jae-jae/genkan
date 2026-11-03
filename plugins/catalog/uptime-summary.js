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

export const manifest = {
  id: "uptime-summary",
  name: "Uptime Summary",
  version: "1.0.0",
  description: "Links with health checks enabled, per workspace. 健康检查链接汇总。",
  author: "genkan",
  icon: "activity",
  permissions: { links: "read" },
  widgets: [{ id: "summary", name: "Health check coverage", size: "s", refresh: 300, config: [] }],
};

export async function widget(ctx) {
  const zh = ctx.lang === "zh";
  const title = zh ? "监控概览" : "Monitoring";
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
          { label: zh ? "链接" : "Links", value: links.length },
          { label: zh ? "已监控" : "Monitored", value: monitored.length, icon: "activity" },
        ],
      },
      {
        type: "progress",
        label: zh ? "监控覆盖率" : "Coverage",
        value: links.length ? monitored.length / links.length : 0,
        text: links.length ? Math.round((monitored.length / links.length) * 100) + "%" : "-",
      },
    ];
    if (rows.length > 1) {
      body.push({ type: "kv", items: rows.map(([id, r]) => ({ k: names.get(id) || id, v: `${r.on} / ${r.total}` })) });
    }
    return { title, icon: "activity", body };
  } catch (e) {
    return { title, icon: "activity", body: [], error: (zh ? "读取链接失败：" : "Could not read links: ") + (e && e.message ? e.message : e) };
  }
}
