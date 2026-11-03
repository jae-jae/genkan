// media — show a web page, a scraped part of a page, images, video and audio.
//
// Widgets (every one needs a per-instance URL, so none has fixed fetch hosts):
//   page     Web page     -> one `iframe` block. The BROWSER loads the page, so no
//                            fetch permission is involved. Optional crop/zoom/refresh.
//   snippet  Web snippet  -> `html` blocks. The SERVER fetches the page (the `url`
//                            field has allowFetch: true) and cuts out the elements a
//                            CSS selector matches with genkan.html.select().
//                            Static HTML only: no JavaScript-rendered content, no
//                            pages behind a login.
//   image    Image        -> one `image` block: a slideshow of the URLs (one per line).
//   video    Video        -> YouTube / Bilibili links become an `iframe` block
//                            (youtube-nocookie.com / player.bilibili.com), anything
//                            else a `media` video block.
//   audio    Audio        -> a `media` audio block (files and radio streams).
//
// Nothing here is stored. All strings live in `messages` (t() in the manifest,
// ctx.t / ctx.locale in hooks).

export const messages = {
  en: {
    name: "Media",
    desc: "Embed a web page, scrape part of a page, show images, play video and audio.",
    page: "Web page",
    snippet: "Web snippet",
    image: "Image",
    video: "Video",
    audio: "Audio",
    url: "URL",
    pageUrlHelp: "The browser loads this page directly. Sites that forbid embedding (X-Frame-Options) show blank; a link below the frame opens it in a new tab.",
    height: "Height (px)",
    heightHelp: "120 to 1600.",
    cropX: "Crop: left (px)",
    cropY: "Crop: top (px)",
    cropW: "Crop: width (px)",
    cropH: "Crop: height (px)",
    cropHelp: "Optional. Show only this rectangle of the page. Needs width and height; the widget height then follows the region.",
    zoom: "Zoom",
    zoomHelp: "0.25 to 2. Below 1 shows more of the page.",
    pageRefresh: "Reload every (seconds)",
    pageRefreshHelp: "0 = never. At least 5.",
    snippetUrlHelp: "The server fetches this page. JavaScript-rendered content and pages behind a login are not supported.",
    title: "Title",
    titleHelp: "Optional. Defaults to the site name.",
    selector: "CSS selector",
    selectorHelp: "Which elements to show, e.g. .titleline > a or article p.",
    max: "Max items",
    urls: "Image URLs",
    urlsHelp: "One http(s) URL per line (up to 20). Several URLs make a slideshow.",
    interval: "Seconds per image",
    intervalHelp: "At least 3.",
    fit: "Fit",
    cover: "Fill (crop)",
    contain: "Show whole image",
    imgHeightHelp: "Optional. Empty = 16:9.",
    link: "Link",
    linkHelp: "Optional. Opens when an image is clicked.",
    poster: "Poster image URL",
    posterHelp: "Optional. Shown before a video starts.",
    autoplay: "Autoplay (muted)",
    autoplayHelp: "Browsers only allow autoplay without sound.",
    loop: "Loop",
    controls: "Show controls",
    videoUrlHelp: "A video file (mp4, webm), or a YouTube or Bilibili link.",
    audioUrlHelp: "An audio file or a radio stream URL.",
    // runtime
    setUrl: "Set the URL in the widget settings",
    badUrl: "Not a valid http(s) URL: {url}",
    setSelector: "Set a CSS selector in the widget settings",
    badSelector: "Invalid CSS selector: {msg}",
    noMatch: "Nothing on the page matches “{selector}”. The content may be rendered by JavaScript.",
    fetchFailed: "Could not load the page: {msg}",
    httpError: "The page answered HTTP {status}",
    notHtml: "Not an HTML page (content type {type})",
    noImages: "Add at least one image URL (http or https) in the widget settings",
  },
  zh: {
    name: "媒体",
    desc: "嵌入网页、截取网页片段、显示图片、播放视频和音频。",
    page: "网页",
    snippet: "网页片段",
    image: "图片",
    video: "视频",
    audio: "音频",
    url: "地址",
    pageUrlHelp: "由浏览器直接加载该网页。禁止嵌入（X-Frame-Options）的网站会显示空白；框下方的链接可在新标签页打开。",
    height: "高度（px）",
    heightHelp: "120 到 1600。",
    cropX: "裁剪：左边距（px）",
    cropY: "裁剪：上边距（px）",
    cropW: "裁剪：宽度（px）",
    cropH: "裁剪：高度（px）",
    cropHelp: "可选。只显示网页中的这块矩形区域，需同时填写宽度和高度；此时小组件高度由该区域决定。",
    zoom: "缩放",
    zoomHelp: "0.25 到 2。小于 1 可显示更多内容。",
    pageRefresh: "刷新间隔（秒）",
    pageRefreshHelp: "0 = 不刷新。至少 5。",
    snippetUrlHelp: "由服务器抓取该网页。不支持 JavaScript 渲染的内容，也不支持需要登录的页面。",
    title: "标题",
    titleHelp: "可选。默认使用网站名。",
    selector: "CSS 选择器",
    selectorHelp: "要显示哪些元素，例如 .titleline > a 或 article p。",
    max: "最多条数",
    urls: "图片地址",
    urlsHelp: "每行一个 http(s) 地址（最多 20 个）。多个地址会轮播。",
    interval: "每张停留（秒）",
    intervalHelp: "至少 3。",
    fit: "适应方式",
    cover: "填满（裁剪）",
    contain: "完整显示",
    imgHeightHelp: "可选。留空 = 16:9。",
    link: "链接",
    linkHelp: "可选。点击图片时打开。",
    poster: "封面图地址",
    posterHelp: "可选。视频开始播放前显示。",
    autoplay: "自动播放（静音）",
    autoplayHelp: "浏览器只允许静音自动播放。",
    loop: "循环播放",
    controls: "显示控制条",
    videoUrlHelp: "视频文件（mp4、webm），或 YouTube、哔哩哔哩链接。",
    audioUrlHelp: "音频文件或电台流地址。",
    setUrl: "请先在小组件设置中填写地址",
    badUrl: "不是有效的 http(s) 地址：{url}",
    setSelector: "请先在小组件设置中填写 CSS 选择器",
    badSelector: "CSS 选择器无效：{msg}",
    noMatch: "页面中没有匹配“{selector}”的内容。该内容可能由 JavaScript 渲染。",
    fetchFailed: "无法加载网页：{msg}",
    httpError: "网页返回 HTTP {status}",
    notHtml: "不是 HTML 页面（内容类型 {type}）",
    noImages: "请先在小组件设置中填写至少一个图片地址（http 或 https）",
  },
};

export const manifest = {
  id: "media",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  icon: "film",
  widgets: [
    {
      id: "page",
      name: t("page"),
      size: "l",
      refresh: 3600, // the block itself carries the reload interval
      config: [
        { key: "url", type: "url", label: t("url"), help: t("pageUrlHelp"), required: true },
        { key: "height", type: "number", label: t("height"), help: t("heightHelp"), default: 360, min: 120, max: 1600 },
        { key: "cropX", type: "number", label: t("cropX"), help: t("cropHelp"), min: 0, max: 20000 },
        { key: "cropY", type: "number", label: t("cropY"), min: 0, max: 20000 },
        { key: "cropW", type: "number", label: t("cropW"), min: 1, max: 20000 },
        { key: "cropH", type: "number", label: t("cropH"), min: 1, max: 20000 },
        { key: "zoom", type: "number", label: t("zoom"), help: t("zoomHelp"), default: 1, min: 0.25, max: 2 },
        { key: "refresh", type: "number", label: t("pageRefresh"), help: t("pageRefreshHelp"), default: 0, min: 0, max: 86400 },
      ],
    },
    {
      id: "snippet",
      name: t("snippet"),
      size: "m",
      refresh: 900,
      config: [
        { key: "url", type: "url", label: t("url"), help: t("snippetUrlHelp"), required: true, allowFetch: true },
        { key: "selector", type: "string", label: t("selector"), help: t("selectorHelp"), required: true },
        { key: "title", type: "string", label: t("title"), help: t("titleHelp") },
        { key: "max", type: "number", label: t("max"), default: 5, min: 1, max: 20 },
      ],
    },
    {
      id: "image",
      name: t("image"),
      size: "m",
      refresh: 3600,
      config: [
        { key: "urls", type: "text", label: t("urls"), help: t("urlsHelp"), required: true },
        { key: "interval", type: "number", label: t("interval"), help: t("intervalHelp"), default: 6, min: 3, max: 3600 },
        {
          key: "fit",
          type: "select",
          label: t("fit"),
          default: "cover",
          options: [
            { value: "cover", label: t("cover") },
            { value: "contain", label: t("contain") },
          ],
        },
        { key: "height", type: "number", label: t("height"), help: t("imgHeightHelp"), min: 60, max: 1000 },
        { key: "link", type: "url", label: t("link"), help: t("linkHelp") },
      ],
    },
    {
      id: "video",
      name: t("video"),
      size: "m",
      refresh: 3600,
      config: [
        { key: "url", type: "url", label: t("url"), help: t("videoUrlHelp"), required: true },
        { key: "poster", type: "url", label: t("poster"), help: t("posterHelp") },
        { key: "autoplay", type: "boolean", label: t("autoplay"), help: t("autoplayHelp"), default: false },
        { key: "loop", type: "boolean", label: t("loop"), default: false },
        { key: "controls", type: "boolean", label: t("controls"), default: true },
      ],
    },
    {
      id: "audio",
      name: t("audio"),
      size: "s",
      refresh: 3600,
      config: [
        { key: "url", type: "url", label: t("url"), help: t("audioUrlHelp"), required: true },
        { key: "title", type: "string", label: t("title") },
        { key: "loop", type: "boolean", label: t("loop"), default: false },
      ],
    },
  ],
};

const UA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36";

const isHttp = (u) => /^https?:\/\/[^\s/]+/i.test(u);
const hostOf = (u) => {
  const m = /^https?:\/\/(?:[^@/]*@)?([^/:?#]+)/i.exec(u);
  return m ? m[1].replace(/^www\./i, "") : "";
};
const num = (v) => (v === undefined || v === null || v === "" || !Number.isFinite(Number(v)) ? undefined : Number(v));

// ---- video link detection ---------------------------------------------------

const YT_ID = "([A-Za-z0-9_-]{11})(?![A-Za-z0-9_-])";

// "90", "1m30s", "1h2m3s" -> seconds (0 = none)
function seconds(s) {
  if (!s) return 0;
  if (/^\d+s?$/.test(s)) return parseInt(s, 10);
  const m = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/.exec(s);
  return m ? (+m[1] || 0) * 3600 + (+m[2] || 0) * 60 + (+m[3] || 0) : 0;
}

// YouTube watch / youtu.be / shorts / live / embed links -> video id (or "").
export function youtubeId(u) {
  let m = new RegExp("^https?://(?:www\\.|m\\.|music\\.)?youtube(?:-nocookie)?\\.com/(?:shorts|live|embed|v)/" + YT_ID, "i").exec(u);
  if (m) return m[1];
  m = new RegExp("^https?://(?:www\\.|m\\.|music\\.)?youtube\\.com/watch\\?(?:[^#]*&)?v=" + YT_ID, "i").exec(u);
  if (m) return m[1];
  m = new RegExp("^https?://youtu\\.be/" + YT_ID, "i").exec(u);
  return m ? m[1] : "";
}

export function bilibiliId(u) {
  const m = /^https?:\/\/(?:www\.|m\.)?bilibili\.com\/video\/(BV[0-9A-Za-z]{8,12})(?![0-9A-Za-z])/.exec(u);
  return m ? m[1] : "";
}

function embedFor(u, cfg) {
  const yt = youtubeId(u);
  if (yt) {
    const q = [];
    const start = seconds((/[?&#](?:t|start)=([0-9hms]+)/.exec(u) || [])[1]);
    if (start) q.push("start=" + start);
    if (cfg.autoplay) q.push("autoplay=1", "mute=1");
    if (cfg.loop) q.push("loop=1", "playlist=" + yt);
    if (cfg.controls === false) q.push("controls=0");
    return "https://www.youtube-nocookie.com/embed/" + yt + (q.length ? "?" + q.join("&") : "");
  }
  const bv = bilibiliId(u);
  if (bv) return "https://player.bilibili.com/player.html?bvid=" + bv + "&autoplay=" + (cfg.autoplay ? 1 : 0);
  return "";
}

// ---- widgets ----------------------------------------------------------------

const fail = (title, icon, error) => ({ title, icon, body: [], error });

function pageWidget(ctx, cfg) {
  const { t } = ctx;
  const url = String(cfg.url || "").trim();
  if (!url) return fail(t("page"), "globe", t("setUrl"));
  if (!isHttp(url)) return fail(t("page"), "globe", t("badUrl", { url }));
  const block = { type: "iframe", src: url, height: num(cfg.height) || 360 };
  const w = num(cfg.cropW);
  const h = num(cfg.cropH);
  if (w > 0 && h > 0) block.crop = { x: num(cfg.cropX) || 0, y: num(cfg.cropY) || 0, width: w, height: h };
  const zoom = num(cfg.zoom);
  if (zoom) block.zoom = zoom;
  const refresh = num(cfg.refresh);
  if (refresh > 0) block.refresh = refresh;
  return { title: hostOf(url) || t("page"), icon: "globe", link: url, body: [block] };
}

// Table rows / list items on their own are dropped by the HTML parser, so they get their container back.
function snippetBlocks(matches) {
  const htmls = matches.map((m) => m.html);
  const all = (re) => htmls.every((h) => re.test(h));
  if (all(/^<tr[\s>]/i)) return [`<table><tbody>${htmls.join("")}</tbody></table>`];
  if (all(/^<li[\s>]/i)) return [`<ul>${htmls.join("")}</ul>`];
  return htmls.map((h) => {
    if (/^<(td|th)[\s>]/i.test(h)) return `<table><tbody><tr>${h}</tr></tbody></table>`;
    if (/^<tr[\s>]/i.test(h)) return `<table><tbody>${h}</tbody></table>`;
    if (/^<(thead|tbody|tfoot)[\s>]/i.test(h)) return `<table>${h}</table>`;
    if (/^<li[\s>]/i.test(h)) return `<ul>${h}</ul>`;
    if (/^<(dt|dd)[\s>]/i.test(h)) return `<dl>${h}</dl>`;
    return h;
  });
}

async function snippetWidget(ctx, cfg) {
  const { t } = ctx;
  const url = String(cfg.url || "").trim();
  const selector = String(cfg.selector || "").trim();
  const title = String(cfg.title || "").trim() || hostOf(url) || t("snippet");
  const bad = (error) => fail(title, "newspaper", error);
  if (!url) return bad(t("setUrl"));
  if (!isHttp(url)) return bad(t("badUrl", { url }));
  if (!selector) return bad(t("setSelector"));
  const max = Math.min(20, Math.max(1, Math.floor(num(cfg.max)) || 5));

  let page, base;
  try {
    const res = await fetch(url, {
      headers: { "User-Agent": UA, Accept: "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5", "Accept-Language": ctx.locale || "en" },
    });
    if (!res.ok) return bad(t("httpError", { status: res.status }));
    const type = String((res.headers && res.headers.get && res.headers.get("content-type")) || "");
    if (type && !/html|xml/i.test(type)) return bad(t("notHtml", { type: type.split(";")[0] }));
    base = isHttp(res.url || "") ? res.url : url; // after redirects
    // genkan.html.select takes at most 2 MiB; a char can be 3 bytes
    page = (await res.text()).slice(0, 650000);
  } catch (e) {
    return bad(t("fetchFailed", { msg: e && e.message ? e.message : String(e) }));
  }

  let matches;
  try {
    matches = genkan.html.select(page, selector, { base, limit: max });
  } catch (e) {
    return bad(t("badSelector", { msg: String(e && e.message ? e.message : e).replace(/^genkan\.html\.select:\s*/, "") }));
  }
  if (matches.length === 0) return bad(t("noMatch", { selector }));
  return { title, icon: "newspaper", link: url, body: snippetBlocks(matches).map((html) => ({ type: "html", html })) };
}

function imageWidget(ctx, cfg) {
  const { t } = ctx;
  const link = String(cfg.link || "").trim();
  const urls = String(cfg.urls || "")
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(isHttp)
    .slice(0, 20);
  if (urls.length === 0) return fail(t("image"), "image", t("noImages"));
  const block = {
    type: "image",
    images: urls.map((src) => (isHttp(link) ? { src, href: link } : { src })),
    interval: num(cfg.interval) || 6,
    fit: cfg.fit === "contain" ? "contain" : "cover",
  };
  const height = num(cfg.height);
  if (height) block.height = height;
  return { title: t("image"), icon: "image", body: [block] };
}

function videoWidget(ctx, cfg) {
  const { t } = ctx;
  const url = String(cfg.url || "").trim();
  if (!url) return fail(t("video"), "film", t("setUrl"));
  if (!isHttp(url)) return fail(t("video"), "film", t("badUrl", { url }));
  const emb = embedFor(url, cfg);
  if (emb) return { title: t("video"), icon: "film", link: url, body: [{ type: "iframe", src: emb, ratio: 16 / 9 }] };
  const block = { type: "media", kind: "video", src: url, controls: cfg.controls !== false };
  if (isHttp(String(cfg.poster || ""))) block.poster = cfg.poster;
  if (cfg.autoplay) {
    block.autoplay = true;
    block.muted = true; // browsers only autoplay muted video
  }
  if (cfg.loop) block.loop = true;
  return { title: t("video"), icon: "film", body: [block] };
}

function audioWidget(ctx, cfg) {
  const { t } = ctx;
  const url = String(cfg.url || "").trim();
  const title = String(cfg.title || "").trim();
  if (!url) return fail(title || t("audio"), "headphones", t("setUrl"));
  if (!isHttp(url)) return fail(title || t("audio"), "headphones", t("badUrl", { url }));
  const block = { type: "media", kind: "audio", src: url, controls: true };
  if (title) block.title = title;
  if (cfg.loop) block.loop = true;
  return { title: title || t("audio"), icon: "headphones", body: [block] };
}

export async function widget(ctx) {
  const cfg = (ctx.instance && ctx.instance.config) || {};
  switch (ctx.widget) {
    case "snippet":
      return snippetWidget(ctx, cfg);
    case "image":
      return imageWidget(ctx, cfg);
    case "video":
      return videoWidget(ctx, cfg);
    case "audio":
      return audioWidget(ctx, cfg);
    default:
      return pageWidget(ctx, cfg);
  }
}
