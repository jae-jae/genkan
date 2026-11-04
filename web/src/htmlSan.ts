// Client-side sanitizer for the `html` widget block (docs/PLUGINS.md §14). The server already cleaned the markup;
// this is defence in depth: the text is parsed by DOMParser (an inert document: no script runs, nothing loads) and
// ONLY allow-listed elements and attributes are rebuilt into a fresh fragment. Raw plugin HTML is never inserted.

const ALLOWED = new Set(
  'p div span br hr h1 h2 h3 h4 h5 h6 strong b em i u s small sub sup code pre blockquote ul ol li dl dt dd table thead tbody tr th td a img figure figcaption time'.split(' '),
)
/** Removed together with their content; every other unknown element is unwrapped (its children stay). */
const DROPPED = new Set(
  'script style iframe frame frameset object embed applet svg math template noscript noembed noframes head title meta link base audio video source track canvas map area dialog plaintext xmp textarea select option optgroup datalist button input slot portal'.split(
    ' ',
  ),
)
const VOID = new Set(['br', 'hr', 'img'])
const XHTML = 'http://www.w3.org/1999/xhtml'
const MAX_IN = 192 * 1024
const MAX_DEPTH = 20
const MAX_NODES = 6000

/** An absolute http(s) URL or null. Relative URLs are refused (the server resolved them); control characters are refused (they hide schemes). */
export function absHttp(v: unknown): string | null {
  if (typeof v !== 'string') return null
  const s = v.trim()
  // eslint-disable-next-line no-control-regex
  if (!s || s.length > 2048 || /[\u0000-\u001f\u007f]/.test(s)) return null
  try {
    const u = new URL(s)
    return u.protocol === 'http:' || u.protocol === 'https:' ? u.href : null
  } catch {
    return null
  }
}

const digits = (v: string | null, max: number): string | null => {
  const s = (v || '').trim()
  return /^\d{1,4}$/.test(s) && +s >= 1 && +s <= max ? String(+s) : null
}

export function sanitizeToFragment(html: unknown): DocumentFragment {
  const out = document.createDocumentFragment()
  if (typeof html !== 'string' || !html) return out
  const doc = new DOMParser().parseFromString(html.length > MAX_IN ? html.slice(0, MAX_IN) : html, 'text/html')
  let nodes = 0

  const text = (el: Node, sb: string[]) => {
    if (el.nodeType === 3) sb.push(el.nodeValue || '')
    else if (el.nodeType === 1 && (el as Element).namespaceURI === XHTML && !DROPPED.has((el as Element).localName)) el.childNodes.forEach((c) => text(c, sb))
  }

  const walk = (src: Node, dst: Node, depth: number) => {
    for (let c = src.firstChild; c && nodes < MAX_NODES; c = c.nextSibling) {
      if (c.nodeType === 3) {
        nodes++
        dst.appendChild(document.createTextNode(c.nodeValue || ''))
        continue
      }
      if (c.nodeType !== 1) continue
      const el = c as Element
      if (el.namespaceURI !== XHTML) continue // svg / math
      const tag = el.localName
      if (DROPPED.has(tag)) continue
      if (!ALLOWED.has(tag)) {
        walk(el, dst, depth)
        continue
      }
      if (depth >= MAX_DEPTH) {
        const sb: string[] = []
        text(el, sb)
        nodes++
        dst.appendChild(document.createTextNode(sb.join('')))
        continue
      }
      const n = document.createElement(tag)
      let ok = true
      if (tag === 'a') {
        const href = absHttp(el.getAttribute('href'))
        if (!href) {
          walk(el, dst, depth) // no usable link: plain content
          continue
        }
        n.setAttribute('href', href)
        n.setAttribute('target', '_blank')
        n.setAttribute('rel', 'noopener noreferrer')
      } else if (tag === 'img') {
        const src2 = absHttp(el.getAttribute('src'))
        ok = !!src2
        if (src2) {
          n.setAttribute('src', src2)
          n.setAttribute('loading', 'lazy')
          n.setAttribute('decoding', 'async')
          n.setAttribute('referrerpolicy', 'no-referrer')
          n.setAttribute('draggable', 'false')
          n.setAttribute('alt', (el.getAttribute('alt') || '').slice(0, 300))
          for (const k of ['width', 'height']) {
            const d = digits(el.getAttribute(k), 4000)
            if (d) n.setAttribute(k, d)
          }
        }
      } else if (tag === 'td' || tag === 'th') {
        for (const k of ['colspan', 'rowspan']) {
          const d = digits(el.getAttribute(k), 100)
          if (d) n.setAttribute(k, d)
        }
      } else if (tag === 'time') {
        const dt = el.getAttribute('datetime')
        if (dt) n.setAttribute('datetime', dt.slice(0, 64))
      }
      if (!ok) continue
      const title = el.getAttribute('title')
      if (title) n.setAttribute('title', title.slice(0, 300))
      nodes++
      dst.appendChild(n)
      if (!VOID.has(tag)) walk(el, n, depth + 1)
    }
  }
  walk(doc.body, out, 0)
  return out
}
