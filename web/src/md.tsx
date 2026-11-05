import type { ComponentChild } from 'preact'

/**
 * A small, safe markdown subset (docs/PLUGINS.md 15.2), used by the update panel (release notes) and the `markdown` widget block:
 * headings, paragraphs, emphasis (**bold**, *italic*, ~~strike~~), `code` and fences, block quotes, (nested) ordered / unordered /
 * task lists, [links](https://…), <autolinks>, bare URLs, horizontal rules and GFM tables. Images become links (nothing is loaded).
 *
 * Output is VNodes only (never innerHTML): text is always a text node, so raw HTML is shown literally.
 * Only http(s) and mailto: URLs become links (target=_blank, rel=noopener noreferrer); any other scheme (`javascript:`, `data:` …)
 * renders as plain text. GitHub `@user` / `#123` references stay plain text.
 *
 * `widget` mode is for notes the user writes: real h1-h6, a line break is kept as <br>. Without it (release notes) `#` and `##` render
 * as h4 (the panel title is h2 and the release version h3) and wrapped lines are joined like GitHub does.
 * `onToggle(line)` makes task checkboxes clickable; `line` is the 0-based line of the item in the source (after \r\n -> \n).
 */

export interface MdOpts {
  widget?: boolean
  onToggle?: (line: number) => void
}

const MAX = 20000 // characters rendered; the rest is cut
const URL_RE = /https?:\/\/[^\s<>]+/
const INLINE: [string, RegExp][] = [
  ['esc', /\\([\\`*_{}[\]()#+\-.!~|<>])/],
  ['code', /`([^`\n]{1,400})`/],
  ['link', /(!?)\[([^\]\n]{0,300})\]\(\s*([^()\s]{1,2000}(?:\([^()\s]{0,200}\)[^()\s]{0,200})*)(?:\s+"[^"\n]{0,300}")?\s*\)/],
  ['angle', /<((?:https?:\/\/|mailto:)[^\s<>]+)>/],
  ['bold', /\*\*(\S(?:.{0,400}?\S)?)\*\*|\b__(\S(?:.{0,400}?\S)?)__\b/],
  ['strike', /~~(\S(?:.{0,400}?\S)?)~~/],
  ['em', /\*(\S(?:[^*\n]{0,400}?\S)?)\*|\b_([^\s_](?:[^_\n]{0,400}?[^\s_])?)_\b/],
  ['url', URL_RE],
]

const okHref = (u: string) => /^https?:\/\/[^\s]+$/i.test(u) || /^mailto:[^\s]+$/i.test(u)

function Link({ href, children }: { href: string; children: ComponentChild }) {
  return okHref(href) ? (
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
    if (/[.,;:!?'"*~\]]/.test(c)) end--
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
    if (kind === 'esc') out.push(m[1])
    else if (kind === 'code') out.push(<code key={k}>{m[1]}</code>)
    else if (kind === 'link') {
      // an image is just a link: nothing is ever loaded from it
      const label = m[1] ? m[2] || m[3] : m[2]
      out.push(<Link key={k} href={m[3]}>{m[1] ? label : inline(label, depth + 1)}</Link>)
    } else if (kind === 'angle') out.push(<Link key={k} href={m[1]}>{m[1].replace(/^mailto:/i, '')}</Link>)
    else if (kind === 'bold') out.push(<strong key={k}>{inline(m[1] ?? m[2], depth + 1)}</strong>)
    else if (kind === 'strike') out.push(<del key={k}>{inline(m[1], depth + 1)}</del>)
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
  num: number
  text: string
  line: number
  kids: Item[]
}

function List({ items, ordered, o }: { items: Item[]; ordered: boolean; o: MdOpts }) {
  const Tag = ordered ? 'ol' : 'ul'
  return (
    <Tag start={ordered && items[0].num !== 1 ? items[0].num : undefined}>
      {items.map((it, i) => {
        const task = /^\[([ xX])\](?:\s+(.*))?$/.exec(it.text)
        return (
          <li key={i} class={task ? 'task' : undefined}>
            {task && (
              <input
                type="checkbox"
                class="wg-md-check"
                data-testid="widget-md-check"
                data-line={it.line}
                checked={task[1] !== ' '}
                disabled={!o.onToggle}
                aria-label={task[2] || undefined}
                onChange={() => o.onToggle?.(it.line)}
              />
            )}
            {inline(task ? task[2] || '' : it.text)}
            {it.kids.length > 0 && <List items={it.kids} ordered={it.kids[0].ordered} o={o} />}
          </li>
        )
      })}
    </Tag>
  )
}

const LI = /^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$/
const HR = /^\s{0,3}([-*_])(\s*\1){2,}\s*$/
const HEAD = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/
const FENCE = /^\s*(```|~~~)/
const QUOTE = /^\s{0,3}>/
const DELIM = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/

/** Cells of a table row; `\|` is a literal pipe. */
function cells(line: string): string[] {
  let s = line.trim()
  if (s[0] === '|') s = s.slice(1)
  if (s.endsWith('|') && !s.endsWith('\\|')) s = s.slice(0, -1)
  const out: string[] = []
  let cur = ''
  for (let i = 0; i < s.length; i++) {
    if (s[i] === '\\' && s[i + 1] === '|') {
      cur += '|'
      i++
    } else if (s[i] === '|') {
      out.push(cur.trim())
      cur = ''
    } else cur += s[i]
  }
  out.push(cur.trim())
  return out
}

const isTableAt = (lines: string[], i: number) => lines[i].includes('|') && i + 1 < lines.length && lines[i + 1].includes('-') && DELIM.test(lines[i + 1]) && cells(lines[i]).length === cells(lines[i + 1]).length

function blocks(lines: string[], base: number, o: MdOpts, depth: number): ComponentChild[] {
  const out: ComponentChild[] = []
  let i = 0
  let key = 0
  const para = (l: string) => !l.trim() || FENCE.test(l) || HEAD.test(l) || LI.test(l) || QUOTE.test(l) || HR.test(l)
  while (i < lines.length) {
    const line = lines[i]
    if (!line.trim()) {
      i++
      continue
    }
    // fenced code
    const fence = FENCE.exec(line)
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
    // heading
    const h = HEAD.exec(line)
    if (h) {
      const n = h[1].length
      const Tag = o.widget ? (('h' + n) as 'h1') : (['h4', 'h4', 'h5', 'h6', 'h6', 'h6'] as const)[n - 1]
      out.push(<Tag key={key++}>{inline(h[2])}</Tag>)
      i++
      continue
    }
    // horizontal rule (before lists: `- - -`)
    if (HR.test(line)) {
      out.push(<hr key={key++} />)
      i++
      continue
    }
    // list
    if (LI.test(line)) {
      const roots: Item[] = []
      const stack: Item[] = []
      while (i < lines.length) {
        const m = LI.exec(lines[i])
        if (!m || HR.test(lines[i])) {
          // a wrapped continuation line (indented, not blank) belongs to the last item
          if (lines[i].trim() && /^\s+\S/.test(lines[i]) && stack.length && !m) {
            stack[stack.length - 1].text += ' ' + lines[i].trim()
            i++
            continue
          }
          break
        }
        const it: Item = { indent: m[1].replace(/\t/g, '    ').length, ordered: /\d/.test(m[2]), num: parseInt(m[2], 10) || 1, text: m[3], line: base + i, kids: [] }
        while (stack.length && stack[stack.length - 1].indent >= it.indent) stack.pop()
        if (stack.length && stack.length < 6) stack[stack.length - 1].kids.push(it)
        else roots.push(it)
        stack.push(it)
        i++
      }
      out.push(<List key={key++} items={roots} ordered={roots[0].ordered} o={o} />)
      continue
    }
    // block quote (every line maps 1:1 onto the source, so task line numbers stay valid inside)
    if (QUOTE.test(line)) {
      const start = i
      const q: string[] = []
      while (i < lines.length && QUOTE.test(lines[i])) q.push(lines[i++].replace(/^\s{0,3}>\s?/, ''))
      out.push(<blockquote key={key++}>{depth < 5 ? blocks(q, base + start, o, depth + 1) : q.join(' ')}</blockquote>)
      continue
    }
    // GFM table
    if (isTableAt(lines, i)) {
      const head = cells(line)
      const align = cells(lines[i + 1]).map((c) => (c.startsWith(':') ? (c.endsWith(':') ? 'center' : 'left') : c.endsWith(':') ? 'right' : undefined))
      i += 2
      const rows: string[][] = []
      while (i < lines.length && lines[i].trim() && lines[i].includes('|') && rows.length < 200) rows.push(cells(lines[i++]))
      const cols = Math.min(head.length, 12)
      out.push(
        <div class="wg-md-table" key={key++}>
          <table>
            <thead>
              <tr>
                {head.slice(0, cols).map((c, j) => (
                  <th key={j} style={align[j] ? { textAlign: align[j] } : undefined}>
                    {inline(c)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, k) => (
                <tr key={k}>
                  {head.slice(0, cols).map((_, j) => (
                    <td key={j} style={align[j] ? { textAlign: align[j] } : undefined}>
                      {inline(r[j] || '')}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>,
      )
      continue
    }
    // paragraph: consecutive plain lines
    const p: string[] = []
    while (i < lines.length && !para(lines[i]) && !(p.length && isTableAt(lines, i))) p.push(lines[i++].trim())
    if (!p.length) p.push(lines[i++].trim()) // cannot happen, but never loop forever
    out.push(<p key={key++}>{o.widget ? p.flatMap((l, j) => (j ? [<br key={'b' + j} />, ...inline(l)] : inline(l))) : inline(p.join(' '))}</p>)
  }
  return out
}

export function renderMarkdown(src: string, o: MdOpts = {}): ComponentChild[] {
  const text = (src || '').replace(/\r\n?/g, '\n').slice(0, MAX)
  const out = blocks(text.split('\n'), 0, o, 0)
  if (src && src.length > MAX) out.push(<p key="more">…</p>)
  return out
}
