// github-stars — star history of GitHub repositories as a line chart.
//
// Hooks:   widget(ctx) -> WidgetView, schedule(ctx) "sample" (every hour)
// Network: api.github.com (optional token in the plugin settings: 60 -> 5000 requests/hour)
// Output:  title = the repository (links to GitHub), then
//            1. a `stat`   the current stars, trend = the last 7 days
//            2. a `stats`  "+N" for the last 7 and 30 days
//            3. a `chart`  kind "line" for the configured range
//            4. muted `text` notes (collecting data / partial history / refresh failed)
//
// GitHub has no star history API, so the history is a hybrid:
//   * Own sampling: the schedule (and the widget, when its newest point is
//     older than an hour) reads `stargazers_count` and appends [now, count].
//   * One-time backfill (needs the token: GitHub answers 401 to unauthenticated
//     stargazer requests, so without a token the chart builds up from now on):
//     the first time a repository is seen, ~10 pages of
//     GET /repos/{o}/{r}/stargazers (Accept: ...star+json) are sampled evenly;
//     page p gives the point (starred_at of its first item, (p-1)*100+1).
//     GitHub only serves the first 400 pages (40,000 stars); larger repositories
//     keep an approximate history until their own sampling takes over.
//     A 403/429 stops the backfill; it resumes on a later run.
//
// Storage (plugin level, 256 KiB shared by all repositories):
//   series:{owner/name}  { t, c, d } delta coded points: first point = minute `t`
//                        (Unix minutes) with count `c`, then d = [[+minutes, +count], ...].
//                        Compaction: every point for 7 days, then one per day,
//                        after one year one per week (~600 points = ~5 KiB for 2+ years of history).
//   meta:{owner/name}    { bf: 0 | 1 | 2 backfill not started / running / done, todo: [pages left],
//                          at: start of the running backfill (s), seen: last time an instance used it (s),
//                          big: more than 40k stars }
// Series of repositories that no widget instance uses are deleted after 30 days.
// Keys are lower case. Read-modify-write is done without awaiting in between, so
// concurrent hooks lose at worst one point.

export const messages = {
  en: {
    name: "GitHub Stars",
    desc: "Star history of GitHub repositories as a line chart.",
    token: "GitHub token",
    tokenHelp: "Needed to backfill the history before the first sample (GitHub lists stargazers only to signed-in requests). Also raises the rate limit to 5,000/hour. No scopes needed.",
    wname: "Star history",
    repo: "Repository",
    repoHelp: "owner/name, e.g. preactjs/preact",
    range: "Range",
    r30: "30 days",
    r90: "90 days",
    r1y: "1 year",
    rall: "All time",
    stars: "Stars",
    d7: "Last 7 days",
    d30: "Last 30 days",
    collecting: "Collecting data…",
    needToken: "Add a GitHub token in the plugin settings to backfill the earlier history. Until then the chart builds up from now on.",
    partial: "GitHub only lists the first 40,000 stargazers, so the history before this widget started sampling is approximate.",
    stale: "Could not refresh: {reason}",
    noRepo: "Set a repository (owner/name) in the widget settings.",
    badRepo: "Invalid repository “{repo}”: use owner/name.",
    errNotFound: "Repository {repo} was not found.",
    errRate: "GitHub rate limit reached. Add a token in the plugin settings or wait a while.",
    errAuth: "GitHub rejected the token.",
    errHttp: "GitHub returned HTTP {status}.",
    errNet: "Could not reach GitHub.",
    errFull: "Plugin storage is full: too many repositories.",
  },
  zh: {
    name: "GitHub 星标",
    desc: "以折线图展示 GitHub 仓库的星标增长历史。",
    token: "GitHub 令牌",
    tokenHelp: "回填首次采样之前的历史需要它（GitHub 只向已登录请求提供 stargazer 列表），并把速率限制提高到每小时 5000 次。无需任何权限范围。",
    wname: "星标趋势",
    repo: "仓库",
    repoHelp: "owner/name，例如 preactjs/preact",
    range: "时间范围",
    r30: "30 天",
    r90: "90 天",
    r1y: "1 年",
    rall: "全部",
    stars: "星标",
    d7: "近 7 天",
    d30: "近 30 天",
    collecting: "正在收集数据…",
    needToken: "在插件设置中添加 GitHub 令牌即可回填更早的历史；在此之前，图表从现在开始积累。",
    partial: "GitHub 只提供前 40,000 位 stargazer，因此本组件开始采样之前的历史是近似值。",
    stale: "刷新失败：{reason}",
    noRepo: "请在小组件设置中填写仓库（owner/name）。",
    badRepo: "仓库“{repo}”无效：请使用 owner/name 格式。",
    errNotFound: "找不到仓库 {repo}。",
    errRate: "已达到 GitHub 速率限制。请在插件设置中添加令牌，或稍后再试。",
    errAuth: "GitHub 拒绝了该令牌。",
    errHttp: "GitHub 返回 HTTP {status}。",
    errNet: "无法连接 GitHub。",
    errFull: "插件存储已满：仓库过多。",
  },
};

export const manifest = {
  id: "github-stars",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  homepage: "https://github.com",
  icon: "star",
  permissions: { fetch: ["api.github.com"] },
  config: [{ key: "token", type: "secret", label: t("token"), help: t("tokenHelp") }],
  widgets: [
    {
      id: "trend",
      name: t("wname"),
      size: "m",
      refresh: 600,
      config: [
        { key: "repo", type: "string", label: t("repo"), help: t("repoHelp"), required: true },
        {
          key: "range",
          type: "select",
          label: t("range"),
          default: "90d",
          options: [
            { value: "30d", label: t("r30") },
            { value: "90d", label: t("r90") },
            { value: "1y", label: t("r1y") },
            { value: "all", label: t("rall") },
          ],
        },
      ],
    },
  ],
  schedules: [{ id: "sample", every: 3600 }],
};

const API = "https://api.github.com";
const DAY = 86400;
const PAGES = 10; // pages sampled by the backfill
const MAX_PAGE = 400; // GitHub serves at most 400 pages of 100 stargazers
const RANGE = { "30d": 30 * DAY, "90d": 90 * DAY, "1y": 365 * DAY, all: 0 };

const nowSec = () => Math.floor(Date.now() / 1000);
const fail = (kind, extra) => Object.assign(new Error(kind), { kind }, extra);

// "owner/name", "github.com/o/r", "https://github.com/o/r.git" -> "o/r" or ""
function parseRepo(v) {
  let s = String(v || "").trim().replace(/^(?:https?:\/\/)?(?:www\.)?github\.com\//i, "");
  const [o, r] = s.split("/");
  const name = (r || "").replace(/\.git$/i, "");
  if (!/^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$/.test(o || "") || !/^[A-Za-z0-9._-]{1,100}$/.test(name) || name === "." || name === "..") return "";
  return o + "/" + name;
}

// ---- storage ---------------------------------------------------------------------------------

const sKey = (repo) => "series:" + repo.toLowerCase();
const mKey = (repo) => "meta:" + repo.toLowerCase();

function enc(pts) {
  if (!pts.length) return { t: 0, c: 0, d: [] };
  const m = pts.map((p) => [Math.round(p[0] / 60), p[1]]);
  const d = [];
  for (let i = 1; i < m.length; i++) d.push([m[i][0] - m[i - 1][0], m[i][1] - m[i - 1][1]]);
  return { t: m[0][0], c: m[0][1], d };
}

function dec(v) {
  if (!v || !Array.isArray(v.d)) return [];
  let tm = Number(v.t) || 0;
  let c = Number(v.c) || 0;
  const out = [[tm * 60, c]];
  for (const [dt, dc] of v.d) {
    tm += dt;
    c += dc;
    out.push([tm * 60, c]);
  }
  return out;
}

// Every point for 7 days, then one per day, after a year one per week (the last point of a bucket wins).
function compact(pts, now) {
  pts.sort((a, b) => a[0] - b[0]);
  const out = [];
  let last = null;
  for (const p of pts) {
    const age = now - p[0];
    const bucket = age > 365 * DAY ? "w" + Math.floor(p[0] / (7 * DAY)) : age > 7 * DAY ? "d" + Math.floor(p[0] / DAY) : null;
    if ((bucket !== null && bucket === last) || (out.length && out[out.length - 1][0] === p[0])) out[out.length - 1] = p;
    else out.push(p);
    last = bucket;
  }
  return out;
}

const loadSeries = (repo) => dec(genkan.storage.get(sKey(repo)));
const loadMeta = (repo) => Object.assign({ bf: 0, todo: [], at: 0, seen: 0, big: false }, genkan.storage.get(mKey(repo)));

function put(key, value) {
  try {
    genkan.storage.set(key, value);
  } catch (e) {
    throw fail("full");
  }
}

// Adds points to a series (read-modify-write without awaiting in between).
function addPoints(repo, add) {
  const now = nowSec();
  put(sKey(repo), enc(compact(loadSeries(repo).concat(add), now)));
}

// Appends the current total; a point younger than 20 minutes is replaced instead of duplicated.
function record(repo, count) {
  const now = nowSec();
  const pts = loadSeries(repo);
  if (pts.length && now - pts[pts.length - 1][0] < 1200) pts.pop();
  pts.push([now, count]);
  put(sKey(repo), enc(compact(pts, now)));
}

// ---- GitHub ----------------------------------------------------------------------------------

async function gh(path, token, accept) {
  const headers = { Accept: accept || "application/vnd.github+json" };
  if (token) headers.Authorization = "Bearer " + token;
  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), 3500);
  try {
    const res = await fetch(API + path, { headers, signal: ac.signal });
    if (res.status === 404) throw fail("notfound");
    if (res.status === 401) throw fail("auth");
    if (res.status === 403 || res.status === 429) throw fail("rate");
    if (!res.ok) throw fail("http", { status: res.status });
    const remaining = Number(res.headers.get("x-ratelimit-remaining"));
    return { data: await res.json(), low: Number.isFinite(remaining) && remaining <= 2 };
  } catch (e) {
    if (e && e.kind) throw e;
    throw fail("net");
  } finally {
    clearTimeout(timer);
  }
}

async function fetchCount(repo, token) {
  const { data } = await gh("/repos/" + repo, token);
  const n = Number(data && data.stargazers_count);
  if (!Number.isFinite(n)) throw fail("http", { status: 0 });
  return n;
}

// ~PAGES pages spread evenly over 1..last
function pickPages(count) {
  const last = Math.min(MAX_PAGE, Math.max(1, Math.ceil(count / 100)));
  const k = Math.min(PAGES, last);
  const pages = [];
  for (let i = 0; i < k; i++) {
    const p = k === 1 ? 1 : 1 + Math.round((i * (last - 1)) / (k - 1));
    if (!pages.includes(p)) pages.push(p);
  }
  return pages;
}

// One-time history backfill; stops at the deadline, on a rate limit or when the quota is nearly used up.
// Progress is stored after every page, so the next run continues where this one stopped.
async function backfill(repo, token, until) {
  if (!token) return; // GitHub lists stargazers only to authenticated requests
  let meta = loadMeta(repo);
  const now = nowSec();
  if (meta.bf === 2 || now - meta.at < 90) return; // done, or another hook is working on it
  meta.at = now;
  put(mKey(repo), meta);
  try {
    if (meta.bf === 0) {
      const count = await fetchCount(repo, token);
      record(repo, count);
      meta = loadMeta(repo);
      meta.big = count > MAX_PAGE * 100;
      meta.todo = count > 0 ? pickPages(count) : [];
      meta.bf = 1;
      put(mKey(repo), meta);
    }
    while (meta.todo.length && Date.now() < until) {
      const page = meta.todo[0];
      const { data, low } = await gh("/repos/" + repo + "/stargazers?per_page=100&page=" + page, token, "application/vnd.github.star+json");
      const at = Array.isArray(data) && data.length ? Date.parse(data[0].starred_at) : NaN;
      if (Number.isFinite(at)) addPoints(repo, [[Math.floor(at / 1000), (page - 1) * 100 + 1]]);
      meta = loadMeta(repo);
      meta.todo = meta.todo.filter((p) => p !== page);
      put(mKey(repo), meta);
      if (low) break;
    }
  } finally {
    meta = loadMeta(repo);
    meta.at = 0;
    if (meta.bf === 1 && !meta.todo.length) meta.bf = 2;
    put(mKey(repo), meta);
  }
}

// ---- numbers ---------------------------------------------------------------------------------

// The count at time `at` (linear between samples); null when the history does not reach back that far.
function valueAt(pts, at) {
  if (!pts.length || at > pts[pts.length - 1][0]) return null;
  if (at <= pts[0][0]) return pts[0][1] === 1 ? 0 : null; // before the very first star there were none
  let i = 1;
  while (pts[i][0] < at) i++;
  const [t0, v0] = pts[i - 1];
  const [t1, v1] = pts[i];
  return v0 + ((v1 - v0) * (at - t0)) / (t1 - t0 || 1);
}

const group = (n) => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
const signed = (n) => (n > 0 ? "+" : n < 0 ? "−" : "") + group(Math.abs(n));
const trendOf = (n) => (n === null ? undefined : n > 0 ? "up" : n < 0 ? "down" : "flat");

function chartPoints(pts, now, range) {
  let a = pts;
  if (range) {
    const cut = now - range;
    const i = pts.findIndex((p) => p[0] >= cut);
    if (i < 0) a = pts.slice(-1);
    else {
      a = pts.slice(i);
      const v = i > 0 ? valueAt(pts, cut) : null;
      if (v !== null && a[0][0] > cut) a.unshift([cut, Math.round(v)]);
    }
  }
  if (a.length > 480) {
    const step = (a.length - 1) / 479;
    a = Array.from({ length: 480 }, (_, i) => a[Math.round(i * step)]);
  }
  return a.map((p) => [p[0] * 1000, p[1]]);
}

function errText(t, e, repo) {
  switch (e && e.kind) {
    case "notfound":
      return t("errNotFound", { repo });
    case "rate":
      return t("errRate");
    case "auth":
      return t("errAuth");
    case "http":
      return t("errHttp", { status: e.status });
    case "full":
      return t("errFull");
    default:
      return t("errNet");
  }
}

// ---- hooks -----------------------------------------------------------------------------------

export async function widget(ctx) {
  const { t } = ctx;
  const cfg = (ctx.instance && ctx.instance.config) || {};
  const raw = String(cfg.repo || "").trim();
  if (!raw) return { title: t("name"), icon: "star", body: [], error: t("noRepo") };
  const repo = parseRepo(raw);
  if (!repo) return { title: t("name"), icon: "star", body: [], error: t("badRepo", { repo: raw }) };
  const base = { title: repo, icon: "star", link: "https://github.com/" + repo };
  const token = ctx.config.token;
  const started = Date.now();
  let warn = "";

  const meta = loadMeta(repo);
  if (nowSec() - meta.seen > DAY) {
    meta.seen = nowSec();
    put(mKey(repo), meta);
  }
  let pts = loadSeries(repo);
  try {
    // (a repository that is about to be backfilled gets its count from the backfill: one request less)
    if ((meta.bf !== 0 || !token) && (!pts.length || nowSec() - pts[pts.length - 1][0] > 3600)) record(repo, await fetchCount(repo, token));
    if (meta.bf !== 2) await backfill(repo, token, started + 5000);
  } catch (e) {
    if (e && e.kind === "notfound") return { ...base, body: [], error: errText(t, e, repo) };
    genkan.log("github-stars", repo, e && e.message);
    warn = errText(t, e, repo);
  }
  pts = loadSeries(repo);
  if (!pts.length) return warn ? { ...base, body: [], error: warn } : { ...base, body: [{ type: "text", text: t("collecting"), muted: true, align: "center" }] };

  const now = nowSec();
  const cur = pts[pts.length - 1][1];
  const v7 = valueAt(pts, now - 7 * DAY);
  const v30 = valueAt(pts, now - 30 * DAY);
  const d7 = v7 === null ? null : Math.round(cur - v7);
  const d30 = v30 === null ? null : Math.round(cur - v30);
  const delta = (label, d) => ({ label, value: d === null ? "—" : signed(d), trend: trendOf(d) });
  const body = [
    { type: "stat", label: t("stars"), value: group(cur), icon: "star", trend: trendOf(d7) },
    { type: "stats", items: [delta(t("d7"), d7), delta(t("d30"), d30)] },
  ];
  if (pts.length >= 2) body.push({ type: "chart", kind: "line", points: chartPoints(pts, now, RANGE[cfg.range] ?? RANGE["90d"]), format: "int", color: "accent" });
  else body.push({ type: "text", text: t("collecting"), muted: true, size: "s", align: "center" });
  if (!token && loadMeta(repo).bf !== 2) body.push({ type: "text", text: t("needToken"), muted: true, size: "s" });
  if (loadMeta(repo).big) body.push({ type: "text", text: t("partial"), muted: true, size: "s" });
  if (warn) body.push({ type: "text", text: t("stale", { reason: warn }), muted: true, size: "s" });
  return { ...base, body };
}

export async function schedule(ctx) {
  const until = Date.now() + 45000;
  const token = ctx.config.token;
  const repos = new Map(); // lower case -> as typed
  for (const w of genkan.widgets.list()) {
    const repo = w.widget === "trend" ? parseRepo(w.config && w.config.repo) : "";
    if (repo) repos.set(repo.toLowerCase(), repo);
  }
  const now = nowSec();
  for (const repo of repos.values()) {
    const meta = loadMeta(repo);
    if (now - meta.seen > 3600) {
      meta.seen = now;
      put(mKey(repo), meta);
    }
  }

  let blocked = false;
  const stop = (repo, e) => {
    genkan.log("github-stars", repo, e && e.kind ? e.kind : e && e.message);
    if (e && (e.kind === "rate" || e.kind === "net" || e.kind === "auth")) blocked = true;
  };
  // 1. own sampling
  for (const repo of repos.values()) {
    if (blocked || Date.now() > until) break;
    const pts = loadSeries(repo);
    if ((token && loadMeta(repo).bf === 0) || (pts.length && nowSec() - pts[pts.length - 1][0] < 1800)) continue; // (the backfill reads the count itself)
    try {
      record(repo, await fetchCount(repo, token));
    } catch (e) {
      stop(repo, e);
    }
  }
  // 2. pending backfills
  for (const repo of repos.values()) {
    if (blocked || Date.now() > until) break;
    try {
      await backfill(repo, token, until);
    } catch (e) {
      stop(repo, e);
    }
  }
  // 3. forget repositories nobody uses any more (30 days after they were last seen)
  const names = new Set();
  for (const key of genkan.storage.keys()) {
    if (key.startsWith("meta:") || key.startsWith("series:")) names.add(key.slice(key.indexOf(":") + 1));
  }
  for (const name of names) {
    if (repos.has(name)) continue;
    const meta = genkan.storage.get("meta:" + name);
    if (!meta || !meta.seen) {
      genkan.storage.set("meta:" + name, { ...(meta || { bf: 0, todo: [], at: 0, big: false }), seen: now }); // start the clock
    } else if (now - meta.seen > 30 * DAY) {
      genkan.storage.delete("meta:" + name);
      genkan.storage.delete("series:" + name);
    }
  }
  return { repos: repos.size };
}
