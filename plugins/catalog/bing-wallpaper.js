// bing-wallpaper — Bing daily image as a wallpaper source (Bing 每日壁纸).
//
// Hook:    wallpaper(ctx) -> WallpaperResult | WallpaperResult[]
// Network: www.bing.com only (both the JSON endpoint and the image host).
//
// A wallpaper hook only describes the image; the Genkan server downloads it
// (through this plugin's fetch allow-list), validates it and serves it to
// browsers from its own /uploads, so the browser never talks to Bing.

// Texts live in `messages` (en + zh); t("key") in the manifest points at them.
export const messages = {
  en: {
    name: "Bing Wallpaper",
    desc: "Bing's daily image of the day.",
    daily: "Daily image",
    market: "Market",
    marketHelp: "Bing market (region) the daily image is taken from.",
    random: "Random from the last 8 days",
    randomHelp: "Return several recent images; Genkan picks one at random.",
    "mkt.en-US": "United States (en-US)",
    "mkt.en-GB": "United Kingdom (en-GB)",
    "mkt.zh-CN": "China (zh-CN)",
    "mkt.ja-JP": "Japan (ja-JP)",
    "mkt.de-DE": "Germany (de-DE)",
    "mkt.fr-FR": "France (fr-FR)",
    "mkt.es-ES": "Spain (es-ES)",
    "mkt.it-IT": "Italy (it-IT)",
    "mkt.pt-BR": "Brazil (pt-BR)",
    "mkt.en-CA": "Canada (en-CA)",
    "mkt.en-AU": "Australia (en-AU)",
    "mkt.en-IN": "India (en-IN)",
  },
  zh: {
    name: "必应壁纸",
    desc: "必应每日图片。",
    daily: "每日图片",
    market: "地区",
    marketHelp: "每日图片所取自的必应地区。",
    random: "最近 8 天随机",
    randomHelp: "返回最近的多张图片，由 Genkan 随机选一张。",
    "mkt.en-US": "美国 (en-US)",
    "mkt.en-GB": "英国 (en-GB)",
    "mkt.zh-CN": "中国 (zh-CN)",
    "mkt.ja-JP": "日本 (ja-JP)",
    "mkt.de-DE": "德国 (de-DE)",
    "mkt.fr-FR": "法国 (fr-FR)",
    "mkt.es-ES": "西班牙 (es-ES)",
    "mkt.it-IT": "意大利 (it-IT)",
    "mkt.pt-BR": "巴西 (pt-BR)",
    "mkt.en-CA": "加拿大 (en-CA)",
    "mkt.en-AU": "澳大利亚 (en-AU)",
    "mkt.en-IN": "印度 (en-IN)",
  },
};

export const manifest = {
  id: "bing-wallpaper",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  homepage: "https://www.bing.com",
  icon: "image",
  permissions: { fetch: ["www.bing.com"] },
  config: [
    {
      key: "mkt",
      type: "select",
      label: t("market"),
      help: t("marketHelp"),
      default: "en-US",
      options: [
        { value: "en-US", label: t("mkt.en-US") },
        { value: "en-GB", label: t("mkt.en-GB") },
        { value: "zh-CN", label: t("mkt.zh-CN") },
        { value: "ja-JP", label: t("mkt.ja-JP") },
        { value: "de-DE", label: t("mkt.de-DE") },
        { value: "fr-FR", label: t("mkt.fr-FR") },
        { value: "es-ES", label: t("mkt.es-ES") },
        { value: "it-IT", label: t("mkt.it-IT") },
        { value: "pt-BR", label: t("mkt.pt-BR") },
        { value: "en-CA", label: t("mkt.en-CA") },
        { value: "en-AU", label: t("mkt.en-AU") },
        { value: "en-IN", label: t("mkt.en-IN") },
      ],
    },
    {
      key: "random",
      type: "boolean",
      label: t("random"),
      help: t("randomHelp"),
      default: false,
    },
  ],
  wallpapers: [{ id: "daily", name: t("daily"), refresh: 3600 }],
};

export async function wallpaper(ctx) {
  const mkt = ctx.config.mkt || "en-US";
  const n = ctx.config.random ? 8 : 1;
  const api = `https://www.bing.com/HPImageArchive.aspx?format=js&idx=0&n=${n}&mkt=${encodeURIComponent(mkt)}`;

  const res = await fetch(api);
  if (!res.ok) throw new Error(`Bing returned HTTP ${res.status}`);
  const data = await res.json();
  const images = (data && data.images) || [];
  if (images.length === 0) throw new Error("Bing returned no images for market " + mkt);

  // Wallpaper "errors" are thrown: the server logs them and falls back to
  // the default wallpaper.
  return images.map((img) => ({
    // `url` is a path on www.bing.com; `urlbase + "_UHD.jpg"` would be 4K,
    // but the plain url (1920x1080) keeps downloads small.
    url: new URL(img.url, "https://www.bing.com").href,
    title: img.title || "",
    credit: img.copyright || "Bing",
    link: img.copyrightlink && img.copyrightlink.startsWith("http") ? img.copyrightlink : undefined,
  }));
}
