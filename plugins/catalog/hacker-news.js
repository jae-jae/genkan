// hacker-news — top stories from Hacker News as a list widget.
//
// Hook:    widget(ctx) -> WidgetView
// Network: hacker-news.firebaseio.com (public Firebase API, no key)
//
// Shows how to fan out requests: one call for the story ids, then all items
// in parallel (Promise.allSettled so a single failed item doesn't break it).

export const messages = {
  en: {
    name: "Hacker News",
    desc: "Top stories from Hacker News.",
    widget: "Top stories",
    list: "List",
    top: "Top",
    best: "Best",
    new: "New",
    count: "Stories",
    meta: "{score} pts · {host} · {by}",
    comments: "{n} comments",
    failed: "Could not load Hacker News: {msg}",
  },
  zh: {
    name: "Hacker News",
    desc: "Hacker News 热门文章。",
    widget: "热门文章",
    list: "榜单",
    top: "热门",
    best: "最佳",
    new: "最新",
    count: "文章数",
    meta: "{score} 分 · {host} · {by}",
    comments: "{n} 评论",
    failed: "无法加载 Hacker News：{msg}",
  },
};

export const manifest = {
  id: "hacker-news",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  homepage: "https://news.ycombinator.com",
  icon: "newspaper",
  permissions: { fetch: ["hacker-news.firebaseio.com"] },
  widgets: [
    {
      id: "top",
      name: t("widget"),
      size: "m",
      refresh: 600,
      config: [
        {
          key: "kind",
          type: "select",
          label: t("list"),
          default: "top",
          options: [
            { value: "top", label: t("top") },
            { value: "best", label: t("best") },
            { value: "new", label: t("new") },
          ],
        },
        { key: "count", type: "number", label: t("count"), default: 10, min: 1, max: 20 },
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
  const { t } = ctx;
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
          subtitle: t("meta", { score: it.score, host, by: it.by }),
          url: it.url || "https://news.ycombinator.com/item?id=" + it.id,
          right: t("comments", { n: it.descendants || 0 }),
        };
      }),
    });
  } catch (e) {
    view.error = t("failed", { msg: e && e.message ? e.message : e });
  }
  return view;
}
