// link-importer — periodically import links from a JSON URL.
//
// Hook:        schedule(ctx)  (runs every hour; the return value is ignored)
// Permissions: links: "write" unlocks genkan.links.create / update.
// Network:     the JSON URL you configure (an `allowFetch` url field).
//
// Expected JSON: an array (or { "links": [...] }) of
//   { "title": "Grafana", "url": "https://grafana.lan", "category": "Monitoring" }
// `title` and `category` are optional. Links whose URL already exists anywhere
// in Genkan are skipped, so the job is idempotent. `category` is matched by
// name (case-insensitive) against the target workspace's existing categories;
// unknown names are left uncategorised (the host API cannot create categories).
// The result of each run is kept in plugin storage under "lastSync" and
// written to the plugin log.

export const manifest = {
  id: "link-importer",
  name: "Link Importer",
  version: "1.0.0",
  description: "Import links from a JSON URL on a schedule. 定时从 JSON 导入链接。",
  author: "genkan",
  icon: "download",
  permissions: { fetch: [], links: "write" },
  config: [
    { key: "url", type: "url", label: "JSON URL", help: "Array of {title, url, category}", required: true, allowFetch: true },
    { key: "workspace", type: "string", label: "Workspace", help: "Name or id. Empty = the first workspace." },
  ],
  schedules: [{ id: "sync", every: 3600 }],
};

const MAX_PER_RUN = 100;

// Compare URLs loosely: case-insensitive host, no trailing slash, no fragment.
function key(u) {
  try {
    const x = new URL(u);
    x.hash = "";
    return (x.origin + x.pathname.replace(/\/+$/, "") + x.search).toLowerCase();
  } catch (e) {
    return String(u).trim().toLowerCase();
  }
}

export async function schedule(ctx) {
  const record = (r) => {
    genkan.storage.set("lastSync", { at: ctx.now, ...r });
    genkan.log("sync:", JSON.stringify(r));
    return r;
  };

  if (!ctx.config.url) return record({ error: "JSON URL is not configured" });

  try {
    const res = await fetch(ctx.config.url, { headers: { Accept: "application/json" } });
    if (!res.ok) throw new Error("HTTP " + res.status);
    const data = await res.json();
    const entries = Array.isArray(data) ? data : data && data.links;
    if (!Array.isArray(entries)) throw new Error("expected a JSON array of links");

    // Resolve the target workspace and its categories.
    const workspaces = genkan.workspaces.list();
    const want = String(ctx.config.workspace || "").trim().toLowerCase();
    const ws = want
      ? workspaces.find((w) => w.id === ctx.config.workspace || String(w.name).toLowerCase() === want)
      : workspaces[0];
    if (!ws) throw new Error(want ? "workspace not found: " + ctx.config.workspace : "no workspace exists");
    const cats = new Map(genkan.categories.list().filter((c) => c.workspace_id === ws.id).map((c) => [String(c.name).toLowerCase(), c.id]));

    const known = new Set(genkan.links.list().map((l) => key(l.url)));
    let added = 0;
    let skipped = 0;
    let invalid = 0;
    for (const e of entries) {
      if (added >= MAX_PER_RUN) break;
      const url = e && typeof e.url === "string" ? e.url.trim() : "";
      if (!/^https?:\/\/[^\s/]+/i.test(url)) {
        invalid++;
        continue;
      }
      if (known.has(key(url))) {
        skipped++;
        continue;
      }
      const input = { workspace_id: ws.id, title: String(e.title || new URL(url).hostname).slice(0, 100), url };
      const cat = cats.get(String(e.category || "").toLowerCase());
      if (cat) input.category_id = cat;
      genkan.links.create(input);
      known.add(key(url));
      added++;
    }
    return record({ added, skipped, invalid, workspace: ws.name });
  } catch (e) {
    return record({ error: e && e.message ? e.message : String(e) });
  }
}
