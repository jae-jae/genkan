// rss — latest entries of any RSS 2.0 / Atom feed.
//
// Hook:    widget(ctx) -> WidgetView
// Network: whatever host the feed URL points to. Because the URL is entered
//          per widget instance, the `url` field has `allowFetch: true`: Genkan
//          adds that URL's host to the fetch allow-list automatically, so
//          permissions.fetch can stay empty.
//
// There is no DOM / DOMParser on the server, so this is a small, tolerant,
// regex-based parser. It is not a validating XML parser, but handles CDATA,
// entities, namespaces and both feed dialects.

export const messages = {
  en: {
    name: "RSS Reader",
    desc: "Latest items of an RSS or Atom feed.",
    widget: "Feed",
    url: "Feed URL",
    title: "Title",
    titleHelp: "Optional; defaults to the feed's own title.",
    count: "Items",
    setUrl: "Set the feed URL in the widget settings",
    noEntries: "no entries found (not RSS/Atom?)",
    untitled: "(untitled)",
    failed: "Could not load feed: {msg}",
  },
  zh: {
    name: "RSS 阅读器",
    desc: "RSS / Atom 订阅的最新条目。",
    widget: "订阅",
    url: "订阅地址",
    title: "标题",
    titleHelp: "可选；默认使用订阅源自己的标题。",
    count: "条目数",
    setUrl: "请先设置订阅地址",
    noEntries: "没有找到条目（不是 RSS/Atom？）",
    untitled: "（无标题）",
    failed: "订阅加载失败：{msg}",
  },
};

export const manifest = {
  id: "rss",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  icon: "rss",
  permissions: { fetch: [] },
  widgets: [
    {
      id: "feed",
      name: t("widget"),
      size: "m",
      refresh: 900,
      config: [
        { key: "url", type: "url", label: t("url"), required: true, allowFetch: true },
        { key: "title", type: "string", label: t("title"), help: t("titleHelp") },
        { key: "count", type: "number", label: t("count"), default: 8, min: 1, max: 20 },
      ],
    },
  ],
};

// ---- tiny XML helpers ------------------------------------------------------

const ENTITIES = { amp: "&", lt: "<", gt: ">", quot: '"', apos: "'", nbsp: " " };

function decode(s) {
  return s.replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (m, e) => {
    if (e[0] === "#") {
      const cp = e[1].toLowerCase() === "x" ? parseInt(e.slice(2), 16) : parseInt(e.slice(1), 10);
      return cp > 0 && cp <= 0x10ffff ? String.fromCodePoint(cp) : m;
    }
    const r = ENTITIES[e.toLowerCase()];
    return r === undefined ? m : r;
  });
}

const CDATA = /^\s*<!\[CDATA\[([\s\S]*?)\]\]>\s*$/;
const stripTags = (html) => html.replace(/<\/?[a-z][^>]*>/gi, " ");
const squash = (s) => s.replace(/\s+/g, " ").trim();

// Raw inner text of element `name` in `xml` ("" if absent).
function raw(xml, name) {
  const m = new RegExp(`<${name}(?:\\s[^>]*)?>([\\s\\S]*?)</${name}>`, "i").exec(xml);
  return m ? m[1] : "";
}

// Plain value (link, date): CDATA is taken literally, otherwise entities are decoded.
function value(xml, name) {
  const r = raw(xml, name);
  const c = CDATA.exec(r);
  return (c ? c[1] : decode(r)).trim();
}

// Plain-text title. Titles are text, so only CDATA content is treated as HTML.
function titleOf(xml, name) {
  const r = raw(xml, name);
  const c = CDATA.exec(r);
  return squash(c ? decode(stripTags(c[1])) : decode(r));
}

// Plain-text summary. Bodies are HTML, either inside CDATA or entity-escaped.
function bodyOf(xml, name) {
  const r = raw(xml, name);
  const c = CDATA.exec(r);
  return squash(decode(stripTags(c ? c[1] : decode(r))));
}

// Value of an attribute in a start tag string.
const attr = (t, name) => {
  const m = new RegExp(`\\s${name}\\s*=\\s*("([^"]*)"|'([^']*)')`, "i").exec(t);
  return m ? decode(m[2] !== undefined ? m[2] : m[3]) : "";
};

// Entry link: Atom uses <link href=... rel=alternate/>, RSS uses <link>url</link>.
function linkOf(block) {
  const tags = block.match(/<link\b[^>]*>/gi) || [];
  for (const t of tags) {
    const rel = attr(t, "rel");
    if (attr(t, "href") && (!rel || rel === "alternate")) return attr(t, "href");
  }
  const link = value(block, "link");
  if (link) return link;
  const guid = value(block, "guid"); // only a URL-looking guid is a link
  return /^https?:\/\//i.test(guid) ? guid : "";
}

function parseFeed(xml) {
  const firstItem = xml.search(/<(item|entry)[\s>]/i);
  const head = firstItem < 0 ? xml : xml.slice(0, firstItem);
  const blocks = xml.match(/<(item|entry)[\s>][\s\S]*?<\/\1>/gi) || [];
  return {
    title: titleOf(head, "title"),
    items: blocks.map((b) => ({
      title: titleOf(b, "title"),
      link: linkOf(b),
      date: value(b, "pubDate") || value(b, "published") || value(b, "updated") || value(b, "dc:date"),
      summary: bodyOf(b, "description") || bodyOf(b, "summary") || bodyOf(b, "content:encoded") || bodyOf(b, "content"),
    })),
  };
}

function shortDate(s, locale) {
  const ms = Date.parse(s);
  if (isNaN(ms)) return "";
  return new Date(ms).toLocaleDateString(locale, { month: "short", day: "numeric", timeZone: "UTC" });
}

// ---- widget ----------------------------------------------------------------

export async function widget(ctx) {
  const { t, locale } = ctx;
  const cfg = (ctx.instance && ctx.instance.config) || {};
  const title = cfg.title || "RSS";
  const fail = (error) => ({ title, icon: "rss", body: [], error });

  if (!cfg.url) return fail(t("setUrl"));
  const n = Math.min(20, Math.max(1, Math.floor(Number(cfg.count)) || 8));

  try {
    const res = await fetch(cfg.url, { headers: { Accept: "application/rss+xml, application/atom+xml, application/xml, text/xml, */*" } });
    if (!res.ok) throw new Error("HTTP " + res.status);
    const feed = parseFeed((await res.text()).slice(0, 2_000_000));
    if (feed.items.length === 0) throw new Error(t("noEntries"));

    return {
      title: cfg.title || feed.title || "RSS",
      icon: "rss",
      body: [
        {
          type: "list",
          items: feed.items.slice(0, n).map((it) => {
            let url;
            try {
              const u = new URL(it.link || "-", cfg.url); // resolves relative links
              if (!it.link) throw 0;
              if (u.protocol === "http:" || u.protocol === "https:") url = u.href;
            } catch (e) {}
            return {
              title: it.title || it.link || t("untitled"),
              subtitle: it.summary.slice(0, 120) || undefined,
              url,
              right: shortDate(it.date, locale) || undefined,
            };
          }),
        },
      ],
    };
  } catch (e) {
    return fail(t("failed", { msg: e && e.message ? e.message : e }));
  }
}
