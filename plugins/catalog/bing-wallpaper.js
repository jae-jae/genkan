// bing-wallpaper — Bing daily image as a wallpaper source (Bing 每日壁纸).
//
// Hook:    wallpaper(ctx) -> WallpaperResult | WallpaperResult[]
// Network: www.bing.com only (both the JSON endpoint and the image host).
//
// A wallpaper hook only describes the image; the Genkan server downloads it
// (through this plugin's fetch allow-list), validates it and serves it to
// browsers from its own /uploads, so the browser never talks to Bing.

export const manifest = {
  id: "bing-wallpaper",
  name: "Bing Wallpaper",
  version: "1.0.0",
  description: "Bing's daily image of the day. Bing 每日图片。",
  author: "genkan",
  homepage: "https://www.bing.com",
  icon: "image",
  permissions: { fetch: ["www.bing.com"] },
  config: [
    {
      key: "mkt",
      type: "select",
      label: "Market",
      help: "Bing market (region) the daily image is taken from.",
      default: "en-US",
      options: [
        { value: "en-US", label: "United States (en-US)" },
        { value: "en-GB", label: "United Kingdom (en-GB)" },
        { value: "zh-CN", label: "中国 (zh-CN)" },
        { value: "ja-JP", label: "日本 (ja-JP)" },
        { value: "de-DE", label: "Deutschland (de-DE)" },
        { value: "fr-FR", label: "France (fr-FR)" },
        { value: "es-ES", label: "España (es-ES)" },
        { value: "it-IT", label: "Italia (it-IT)" },
        { value: "pt-BR", label: "Brasil (pt-BR)" },
        { value: "en-CA", label: "Canada (en-CA)" },
        { value: "en-AU", label: "Australia (en-AU)" },
        { value: "en-IN", label: "India (en-IN)" },
      ],
    },
    {
      key: "random",
      type: "boolean",
      label: "Random from the last 8 days",
      help: "Return several recent images; Genkan picks one at random.",
      default: false,
    },
  ],
  wallpapers: [{ id: "daily", name: "Daily image", refresh: 3600 }],
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
