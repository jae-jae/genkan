// hacker-news — top stories from Hacker News as a list widget.
//
// Hook:    widget(ctx) -> WidgetView
// Network: hacker-news.firebaseio.com (public Firebase API, no key)
//
// Shows how to fan out requests: one call for the story ids, then all items
// in parallel (Promise.allSettled so a single failed item doesn't break it).

export const manifest = {
  id: "hacker-news",
  name: "Hacker News",
  version: "1.0.0",
  description: "Top stories from Hacker News. Hacker News 热门。",
  author: "genkan",
  homepage: "https://news.ycombinator.com",
  icon: "newspaper",
  permissions: { fetch: ["hacker-news.firebaseio.com"] },
  widgets: [
    {
      id: "top",
      name: "Top stories",
      size: "m",
      refresh: 600,
      config: [
        {
          key: "kind",
          type: "select",
          label: "List",
          default: "top",
          options: [
            { value: "top", label: "Top" },
            { value: "best", label: "Best" },
            { value: "new", label: "New" },
          ],
        },
        { key: "count", type: "number", label: "Stories", default: 10, min: 1, max: 20 },
      ],
    },
  ],
};

const API = "https://hacker-news.firebaseio.com/v0/";

async function getJSON(path) {
  const res = await fetch(API + path);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function widget(ctx) {
  const zh = ctx.lang === "zh";
  const cfg = (ctx.instance && ctx.instance.config) || {};
  const kind = ["top", "best", "new"].includes(cfg.kind) ? cfg.kind : "top";
  const n = Math.min(20, Math.max(1, Math.floor(Number(cfg.count)) || 10));
  const view = { title: "Hacker News", icon: "newspaper", link: "https://news.ycombinator.com", body: [] };

  try {
    const ids = (await getJSON(`${kind}stories.json`)).slice(0, n);
    const settled = await Promise.allSettled(ids.map((id) => getJSON(`item/${id}.json`)));
    const items = settled.map((r) => r.value).filter((it) => it && it.title && !it.dead && !it.deleted);
    if (items.length === 0) throw new Error("no stories");

    view.body.push({
      type: "list",
      items: items.map((it) => {
        const host = it.url ? new URL(it.url).hostname.replace(/^www\./, "") : "news.ycombinator.com";
        return {
          title: it.title,
          subtitle: `${it.score} ${zh ? "分" : "pts"} · ${host} · ${it.by}`,
          url: it.url || "https://news.ycombinator.com/item?id=" + it.id,
          right: (it.descendants || 0) + (zh ? " 评论" : " comments"),
        };
      }),
    });
  } catch (e) {
    view.error = (zh ? "无法加载 Hacker News：" : "Could not load Hacker News: ") + (e && e.message ? e.message : e);
  }
  return view;
}
