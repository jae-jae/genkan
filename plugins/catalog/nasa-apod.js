// nasa-apod — NASA Astronomy Picture of the Day as a wallpaper source.
//
// Hook:    wallpaper(ctx) -> WallpaperResult
// Network: api.nasa.gov (JSON); images come from apod.nasa.gov (classic host)
//          or assets.science.nasa.gov (where NASA's APOD moved to in 2026).
//
// APOD is sometimes a video, a hotlinked image from another site or (while
// NASA's backend is degraded) a generic site-logo placeholder. Those days are
// skipped by walking back one day at a time (max 6 tries).
// The shared DEMO_KEY is rate limited; get a free key at https://api.nasa.gov.

export const messages = {
  en: {
    name: "NASA APOD",
    desc: "NASA Astronomy Picture of the Day.",
    key: "NASA API key",
    keyHelp: "Optional. Leave empty to use the rate-limited DEMO_KEY.",
    source: "Astronomy Picture of the Day",
  },
  zh: {
    name: "NASA 每日天文图",
    desc: "NASA 每日天文图（APOD）。",
    key: "NASA API 密钥",
    keyHelp: "可选。留空则使用有速率限制的 DEMO_KEY。",
    source: "每日天文图",
  },
};

export const manifest = {
  id: "nasa-apod",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  homepage: "https://apod.nasa.gov",
  icon: "rocket",
  permissions: { fetch: ["api.nasa.gov", "apod.nasa.gov", "assets.science.nasa.gov"] },
  config: [
    {
      key: "api_key",
      type: "secret",
      label: t("key"),
      help: t("keyHelp"),
    },
  ],
  wallpapers: [{ id: "apod", name: t("source"), refresh: 3600 }],
};

const DAY = 86400000;
const ymd = (ms) => new Date(ms).toISOString().slice(0, 10);

export async function wallpaper(ctx) {
  const key = ctx.config.api_key || "DEMO_KEY";
  let date = ""; // empty = "today" as NASA sees it (US Eastern time)

  for (let i = 0; i < 6; i++) {
    const q = `api_key=${encodeURIComponent(key)}` + (date ? `&date=${date}` : "");
    // The API often answers 500/503 for a moment: try up to 3 times.
    let res;
    for (let attempt = 0; attempt < 3; attempt++) {
      res = await fetch("https://api.nasa.gov/planetary/apod?" + q);
      if (res.status < 500) break;
      await new Promise((r) => setTimeout(r, 700));
    }
    if (res.status === 429) throw new Error("NASA API rate limit hit; set your own api_key in the plugin settings");
    if (!res.ok) throw new Error(`NASA APOD returned HTTP ${res.status}`);
    const d = await res.json();

    // Only real still images on a host we may fetch from, in a format the
    // server accepts (no gif), and not the placeholder logo.
    const ok =
      d.media_type === "image" &&
      typeof d.url === "string" &&
      /^https:\/\/(apod|assets\.science)\.nasa\.gov\/.+\.(jpe?g|png|webp)$/i.test(d.url) &&
      !/\/wp-content\/|logo/i.test(d.url);
    if (ok) {
      return {
        // `hdurl` can exceed the 15 MiB download limit, so use the normal one.
        url: d.url,
        title: d.title,
        credit: "NASA APOD" + (d.copyright ? " · " + String(d.copyright).replace(/\s+/g, " ").trim() : ""),
        link: "https://science.nasa.gov/apod/", // NASA moved the per-day pages; the index is stable
      };
    }
    // Not usable today: continue with the previous day.
    if (!/^\d{4}-\d{2}-\d{2}$/.test(d.date || "")) throw new Error("Unexpected response from the NASA APOD API");
    date = ymd(Date.parse(d.date + "T00:00:00Z") - DAY);
  }
  throw new Error("No still image found in the last few APOD days (the NASA API may be degraded)");
}
