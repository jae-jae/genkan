// picsum — random landscape photos from picsum.photos (Lorem Picsum).
//
// Hook:    wallpaper(ctx) -> WallpaperResult
// Network: picsum.photos (API) and fastly.picsum.photos (image CDN: picsum.photos
//          answers image requests with a redirect, and every redirect target
//          must be on the allow-list too).

export const manifest = {
  id: "picsum",
  name: "Picsum Photos",
  version: "1.0.0",
  description: "Random landscape photos from picsum.photos. 随机风景图。",
  author: "genkan",
  homepage: "https://picsum.photos",
  icon: "camera",
  permissions: { fetch: ["picsum.photos", "fastly.picsum.photos"] },
  config: [],
  wallpapers: [{ id: "random", name: "Random photo", refresh: 3600 }],
};

export async function wallpaper(ctx) {
  // Ask the catalogue for one random page of photos, keep the wide ones.
  const page = 1 + Math.floor(Math.random() * 30);
  const res = await fetch(`https://picsum.photos/v2/list?page=${page}&limit=30`);
  if (!res.ok) throw new Error(`Picsum returned HTTP ${res.status}`);
  const photos = (await res.json()).filter((p) => p.width >= p.height * 1.4 && p.width >= 1920);
  if (photos.length === 0) throw new Error("Picsum returned no suitable photo");

  const p = photos[Math.floor(Math.random() * photos.length)];
  return {
    // /id/<id>/<w>/<h>.jpg is a fixed, cropped 1920x1080 rendition.
    url: `https://picsum.photos/id/${p.id}/1920/1080.jpg`,
    title: "Photo #" + p.id,
    credit: p.author + " · Unsplash",
    link: p.url,
  };
}
