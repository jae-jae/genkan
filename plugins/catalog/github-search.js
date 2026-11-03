// github-search — search GitHub repositories from the search box: "gh genkan".
//
// Hook:    search(ctx) -> SearchResult[]
// Network: api.github.com
//
// `prefix: "gh"` means the hook is only called for queries starting with
// "gh " and ctx.query is the text after the prefix. Search hooks have a
// 2 second budget, so the request is aborted a little earlier than that.
// Unauthenticated GitHub search allows ~10 requests/minute; an optional
// personal access token (no scopes needed) raises that.

export const messages = {
  en: {
    name: "GitHub Search",
    desc: "Search GitHub repositories with the “gh” prefix.",
    group: "GitHub",
    token: "GitHub token",
    tokenHelp: "Optional. Raises the API rate limit; no scopes needed.",
    web: "Search GitHub for {q}",
  },
  zh: {
    name: "GitHub 搜索",
    desc: "用 gh 前缀搜索 GitHub 仓库。",
    group: "GitHub",
    token: "GitHub 令牌",
    tokenHelp: "可选。提高 API 速率限制，无需任何权限范围。",
    web: "在 GitHub 上搜索 {q}",
  },
};

export const manifest = {
  id: "github-search",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  homepage: "https://github.com",
  icon: "code",
  permissions: { fetch: ["api.github.com"] },
  config: [
    { key: "token", type: "secret", label: t("token"), help: t("tokenHelp") },
  ],
  search: { name: t("group"), prefix: "gh" },
};

const compact = (n) => (n >= 1000 ? (n / 1000).toFixed(n >= 10000 ? 0 : 1).replace(/\.0$/, "") + "k" : String(n));

export async function search(ctx) {
  const q = String(ctx.query || "").trim();
  if (q.length < 2) return [];
  const { t } = ctx;
  const web = {
    title: t("web", { q }),
    url: "https://github.com/search?type=repositories&q=" + encodeURIComponent(q),
    icon: "search",
  };

  const headers = { Accept: "application/vnd.github+json" };
  if (ctx.config.token) headers.Authorization = "Bearer " + ctx.config.token;

  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), 1700);
  try {
    const url = "https://api.github.com/search/repositories?per_page=7&sort=stars&q=" + encodeURIComponent(q);
    const res = await fetch(url, { headers, signal: ac.signal });
    if (!res.ok) throw new Error("HTTP " + res.status);
    const data = await res.json();
    const repos = (data.items || []).slice(0, 7).map((r) => ({
      title: r.full_name,
      subtitle: [`★ ${compact(r.stargazers_count)}`, r.language, r.description].filter(Boolean).join(" · "),
      url: r.html_url,
      icon: "code",
    }));
    return [...repos, web].slice(0, 8);
  } catch (e) {
    // Rate limited / offline / timed out: still offer the web search.
    return [{ ...web, subtitle: String(e && e.message ? e.message : e) }];
  } finally {
    clearTimeout(timer);
  }
}
