// Composes the README images from the raw captures of `npm run screenshots` (.tmp/screenshots) into
// docs/images/: a hero (desktop browser window + phone) and a few optimized gallery shots (JPEG).
// Usage: npm run docs-images   (needs `npm run screenshots` first; no extra dependencies)
import { chromium } from '@playwright/test'
import { mkdirSync, statSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const SRC = resolve(root, '.tmp/screenshots')
const OUT = resolve(root, 'docs/images')
mkdirSync(OUT, { recursive: true })
const url = (n) => pathToFileURL(resolve(SRC, `${n}.png`)).href

// name -> raw capture
const GALLERY = {
  'search': 'desktop-search',
  'context-menu': 'desktop-context-menu-link',
  'ai': 'desktop-settings-ai',
  'content': 'desktop-settings-content',
}

const heroHtml = `<!doctype html><meta charset="utf-8"><style>
*{box-sizing:border-box;margin:0}
html,body{width:1200px;height:860px;overflow:hidden;font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text","Inter","Segoe UI",sans-serif}
.bg{position:absolute;inset:-120px;background:url(${url('desktop-home')}) center/cover;filter:blur(70px) saturate(1.25) brightness(.58)}
.veil{position:absolute;inset:0;background:radial-gradient(120% 90% at 20% 0%,rgba(255,255,255,.16),transparent 60%),linear-gradient(180deg,rgba(10,8,30,.10),rgba(10,8,30,.34))}
.win{position:absolute;left:64px;top:66px;width:960px;border-radius:14px;background:#1b1b22;overflow:hidden;
 box-shadow:0 0 0 1px rgba(255,255,255,.14),0 2px 6px rgba(0,0,0,.25),0 30px 60px -10px rgba(5,3,25,.5),0 60px 90px -30px rgba(5,3,25,.45)}
.bar{height:38px;display:flex;align-items:center;padding:0 14px;background:linear-gradient(#2d2d36,#25252d);border-bottom:1px solid rgba(0,0,0,.5);position:relative}
.dots{display:flex;gap:8px}.dots i{width:12px;height:12px;border-radius:50%;display:block;box-shadow:inset 0 0 0 .5px rgba(0,0,0,.25)}
.dots i:nth-child(1){background:#ff5f57}.dots i:nth-child(2){background:#febc2e}.dots i:nth-child(3){background:#28c840}
.url{position:absolute;left:50%;top:7px;transform:translateX(-50%);width:300px;height:24px;border-radius:7px;background:rgba(255,255,255,.08);color:rgba(255,255,255,.62);font-size:12px;display:flex;align-items:center;justify-content:center;gap:6px;letter-spacing:.1px}
.url svg{width:10px;height:10px;opacity:.7}
.win img{display:block;width:960px;height:auto}
.phone{position:absolute;left:882px;top:286px;width:252px;padding:8px;border-radius:44px;background:linear-gradient(145deg,#3a3a44,#101015 45%,#1c1c23);
 box-shadow:0 0 0 1.5px rgba(255,255,255,.22),0 0 0 3px rgba(0,0,0,.55),0 30px 60px -8px rgba(5,3,25,.6),0 60px 90px -30px rgba(5,3,25,.5)}
.phone .scr{position:relative;border-radius:36px;overflow:hidden;background:#000}
.phone img{display:block;width:100%;height:auto}
</style>
<div class="bg"></div><div class="veil"></div>
<div class="win"><div class="bar"><div class="dots"><i></i><i></i><i></i></div>
<div class="url"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/></svg>genkan.lan</div></div>
<img src="${url('desktop-home')}"></div>
<div class="phone"><div class="scr"><img src="${url('mobile-home')}"></div></div>`

const browser = await chromium.launch()
const size = (f) => (statSync(f).size / 1024).toFixed(0) + ' KB'
const tmp = resolve(root, '.tmp/hero.html')
mkdirSync(dirname(tmp), { recursive: true })

// hero: 1200x860 css px at 2x = 2400x1720
{
  writeFileSync(tmp, heroHtml)
  const ctx = await browser.newContext({ viewport: { width: 1200, height: 860 }, deviceScaleFactor: 2 })
  const page = await ctx.newPage()
  await page.goto(pathToFileURL(tmp).href)
  await page.evaluate(() => Promise.all([...document.images].map((i) => i.decode())))
  const f = resolve(OUT, 'hero.jpg')
  await page.screenshot({ path: f, type: 'jpeg', quality: 88 })
  console.log('hero.jpg', size(f))
  await ctx.close()
}

// gallery: 1600px wide JPEG
for (const [name, raw] of Object.entries(GALLERY)) {
  const ctx = await browser.newContext({ viewport: { width: 1600, height: 1111 }, deviceScaleFactor: 1 })
  const page = await ctx.newPage()
  writeFileSync(tmp, `<!doctype html><style>*{margin:0}body{background:#000}img{display:block;width:1600px;height:auto}</style><img src="${url(raw)}">`)
  await page.goto(pathToFileURL(tmp).href)
  await page.evaluate(() => Promise.all([...document.images].map((i) => i.decode())))
  const h = await page.evaluate(() => document.images[0].getBoundingClientRect().height)
  const f = resolve(OUT, `${name}.jpg`)
  await page.screenshot({ path: f, type: 'jpeg', quality: 82, clip: { x: 0, y: 0, width: 1600, height: Math.round(h) } })
  console.log(`${name}.jpg`, size(f))
  await ctx.close()
}
await browser.close()
