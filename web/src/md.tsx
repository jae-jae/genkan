import type { ComponentChild } from 'preact'

/**
 * A small, safe markdown subset for release notes: headings, paragraphs, (nested) lists, fenced code, block quotes,
 * **bold**, *italic*, `code`, [links](https://…) and bare URLs.
 *
 * Output is VNodes only (never innerHTML): text is always a text node, so raw HTML in the notes is shown literally.
 * Only http(s) URLs become links (target=_blank, rel=noopener noreferrer); any other scheme (`javascript:`, `data:` …)
 * renders as plain text. GitHub `@user` / `#123` references stay plain text.
 */

const MAX = 20000 // characters rendered per release; the rest is on GitHub
const URL_RE = /https?:\/\/[^\s<>]+/
const INLINE: [string, RegExp][] = [
  ['code', /`([^`\n]{1,400})`/],
  ['link', /!?\[([^\]\n]{1,300})\]\(\s*([^()\s]{1,2000}(?:\([^()\s]{0,200}\)[^()\s]{0,200})*)(?:\s+"[^"\n]{0,300}")?\s*\)/],
  ['angle', /<(https?:\/\/[^\s<>]+)>/],
  ['bold', /\*\*(\S(?:.{0,400}?\S)?)\*\*/],
  ['em', /\*(\S(?:[^*\n]{0,400}?\S)?)\*|\b_([^\s_](?:[^_\n]{0,400}?[^\s_])?)_\b/],
  ['url', URL_RE],
]

const isHttp = (u: string) => /^https?:\/\/[^\s]+$/i.test(u)

function Link({ href, children }: { href: string; children: ComponentChild }) {
  return isHttp(href) ? (
    <a href={href} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  ) : (
    <span>{children}</span>
  )
}

/** Trailing punctuation is not part of a bare URL; a closing paren stays when the URL has an opening one. */
function trimUrl(u: string): [string, string] {
  let end = u.length
  while (end > 0) {
    const c = u[end - 1]
    if (/[.,;:!?'"*\]]/.test(c)) end--
    else if (c === ')' && (u.slice(0, end).match(/\(/g) || []).length < (u.slice(0, end).match(/\)/g) || []).length) end--
    else break
  }
  return [u.slice(0, end), u.slice(end)]
}

function inline(src: string, depth = 0): ComponentChild[] {
  const out: ComponentChild[] = []
  let rest = src
  let key = 0
  while (rest) {
    let best: { kind: string; m: RegExpExecArray } | null = null
    for (const [kind, re] of INLINE) {
      if (depth > 0 && (kind === 'link' || kind === 'url' || kind === 'angle')) continue // no links inside links
      const m = re.exec(rest)
      if (m && (!best || m.index < best.m.index)) best = { kind, m }
    }
    if (!best) {
      out.push(rest)
      break
    }
    const { kind, m } = best
    if (m.index) out.push(rest.slice(0, m.index))
    let len = m[0].length
    const k = key++
    if (kind === 'code') out.push(<code key={k}>{m[1]}</code>)
    else if (kind === 'link') out.push(<Link key={k} href={m[2]}>{inline(m[1], depth + 1)}</Link>)
    else if (kind === 'angle') out.push(<Link key={k} href={m[1]}>{m[1]}</Link>)
    else if (kind === 'bold') out.push(<strong key={k}>{inline(m[1], depth + 1)}</strong>)
    else if (kind === 'em') out.push(<em key={k}>{inline(m[1] ?? m[2], depth + 1)}</em>)
    else {
      const [u, tail] = trimUrl(m[0])
      len = u.length
      out.push(<Link key={k} href={u}>{u}</Link>)
      void tail // stays in `rest` and is emitted as text
    }
    rest = rest.slice(m.index + len)
  }
  return out
}

interface Item {
  indent: number
  ordered: boolean
  text: string
  kids: Item[]
}

function List({ items, ordered }: { items: Item[]; ordered: boolean }) {
  const Tag = ordered ? 'ol' : 'ul'
  return (
    <Tag>
      {items.map((it, i) => (
        <li key={i}>
          {inline(it.text)}
          {it.kids.length > 0 && <List items={it.kids} ordered={it.kids[0].ordered} />}
        </li>
      ))}
    </Tag>
  )
}

const LI = /^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$/

export function renderMarkdown(src: string): ComponentChild[] {
  const text = (src || '').replace(/\r\n?/g, '\n').slice(0, MAX)
  const lines = text.split('\n')
  const out: ComponentChild[] = []
  let i = 0
  let key = 0
  while (i < lines.length) {
    const line = lines[i]
    if (!line.trim()) {
      i++
      continue
    }
    // fenced code
    const fence = /^\s*(```|~~~)/.exec(line)
    if (fence) {
      const body: string[] = []
      i++
      while (i < lines.length && !lines[i].trim().startsWith(fence[1])) body.push(lines[i++])
      i++
      out.push(
        <pre key={key++}>
          <code>{body.join('\n')}</code>
        </pre>,
      )
      continue
    }
    // heading (# and ## render as h4: the panel title is h2 and the release version h3)
    const h = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/.exec(line)
    if (h) {
      const Tag = (['h4', 'h4', 'h5', 'h6', 'h6', 'h6'] as const)[h[1].length - 1]
      out.push(<Tag key={key++}>{inline(h[2])}</Tag>)
      i++
      continue
    }
    // list
    if (LI.test(line)) {
      const roots: Item[] = []
      const stack: Item[] = []
      while (i < lines.length) {
        const m = LI.exec(lines[i])
        if (!m) {
          // a wrapped continuation line (indented, not blank) belongs to the last item
          if (lines[i].trim() && /^\s+\S/.test(lines[i]) && stack.length) {
            stack[stack.length - 1].text += ' ' + lines[i].trim()
            i++
            continue
          }
          break
        }
        const it: Item = { indent: m[1].replace(/\t/g, '    ').length, ordered: /\d/.test(m[2]), text: m[3], kids: [] }
        while (stack.length && stack[stack.length - 1].indent >= it.indent) stack.pop()
        if (stack.length && stack.length < 6) stack[stack.length - 1].kids.push(it)
        else roots.push(it)
        stack.push(it)
        i++
      }
      out.push(<List key={key++} items={roots} ordered={roots[0].ordered} />)
      continue
    }
    // block quote
    if (/^\s{0,3}>/.test(line)) {
      const q: string[] = []
      while (i < lines.length && /^\s{0,3}>/.test(lines[i])) q.push(lines[i++].replace(/^\s{0,3}>\s?/, ''))
      out.push(<blockquote key={key++}>{renderMarkdown(q.join('\n'))}</blockquote>)
      continue
    }
    // horizontal rule
    if (/^\s{0,3}([-*_])(\s*\1){2,}\s*$/.test(line)) {
      out.push(<hr key={key++} />)
      i++
      continue
    }
    // paragraph: consecutive plain lines
    const p: string[] = []
    while (i < lines.length && lines[i].trim() && !/^\s*(```|~~~)/.test(lines[i]) && !/^\s{0,3}#{1,6}\s/.test(lines[i]) && !LI.test(lines[i]) && !/^\s{0,3}>/.test(lines[i])) p.push(lines[i++].trim())
    out.push(<p key={key++}>{inline(p.join(' '))}</p>)
  }
  if (src && src.length > MAX) out.push(<p key={key++}>…</p>)
  return out
}
