// Tiny JS/TS tokenizer for the plugin source views (lazy chunk only). Not a parser: it only has to colour plugin code well enough.
// Token classes: k keyword, s string / template text, e `${` `}` of a template, c comment, n number, r regex, l true/false/null/undefined,
// p property or object key, f function / method name, y Capitalised name (class, type), o punctuation, '' anything else.
export type Tok = [cls: string, text: string]

const KW = new Set(
  'break case catch class const continue debugger default delete do else export extends finally for function if import in instanceof let new of return static super switch this throw try typeof var void while with yield async await as interface enum implements declare readonly satisfies keyof namespace abstract private public protected'.split(' '),
)
const LIT = new Set(['true', 'false', 'null', 'undefined', 'NaN', 'Infinity'])
// 1 space, 2 comment, 3 string (an unterminated one ends at the line end), 4 backtick, 5 number, 6 name, 7 anything else (one character)
const RX = /(\s+)|(\/\/.*|\/\*[\s\S]*?(?:\*\/|$))|("(?:[^"\\\n]|\\[\s\S])*"?|'(?:[^'\\\n]|\\[\s\S])*'?)|(`)|(0[xob][\da-f_]+n?|(?:\d[\d_]*\.?[\d_]*|\.\d[\d_]*)(?:e[+-]?\d+)?n?)|([A-Za-z_$\u0080-￿][\w$\u0080-￿]*)|([\s\S])/iy
const REGEX = /\/(?:[^/\\\n[]|\\.|\[(?:[^\]\\\n]|\\.)*\])+\/[a-z]*/y
const TPL = /(?:[^`\\$]|\\[\s\S]|\$(?!\{))*/y
const NEXT = /[ \t]*([(:])/y
const PUNCT = '{}()[];,.:?<>=!+-*/&|^%~@#'

export function tokenize(src: string): Tok[] {
  const out: Tok[] = []
  const stack: number[] = [] // one entry per open `${`: how many `{` are open inside it
  let pc = '' // class and text of the previous significant token
  let pt = ''
  let i = 0
  const n = src.length
  const add = (c: string, s: string, sig = true) => {
    const l = out[out.length - 1]
    if (l && l[0] === c) l[1] += s
    else out.push([c, s])
    if (sig) (pc = c), (pt = s)
  }
  // template text up to the closing backtick or the next `${`; returns the new index
  const tpl = (j: number): number => {
    TPL.lastIndex = j
    const e = j + TPL.exec(src)![0].length
    if (e > j) add('s', src.slice(j, e), false)
    if (src[e] === '`') return add('s', '`'), e + 1
    if (src[e] === '$') return add('e', '${'), stack.push(0), e + 2
    return e
  }
  while (i < n) {
    RX.lastIndex = i
    const m = RX.exec(src)!
    const s = m[0]
    i += s.length
    if (m[1]) add('', s, false)
    else if (m[2]) add('c', s, false)
    else if (m[3]) add('s', s)
    else if (m[4]) (add('s', s), (i = tpl(i)))
    else if (m[5]) add('n', s)
    else if (m[6]) {
      NEXT.lastIndex = i
      const nx = NEXT.exec(src)?.[1]
      const dot = pt === '.' && pc === 'o'
      add(
        KW.has(s) && !dot ? 'k' : LIT.has(s) && !dot ? 'l' : nx === '(' ? 'f' : dot || (nx === ':' && pc === 'o' && (pt === '{' || pt === ',')) ? 'p' : /^[A-Z]/.test(s) && /[a-z]/.test(s) ? 'y' : '',
        s,
      )
    } else if (s === '}' && stack.length && !stack[stack.length - 1]) {
      stack.pop()
      add('e', s)
      i = tpl(i)
    } else if (s === '/' && (!pt || (pc === 'o' && !')]}'.includes(pt)) || (pc === 'k' && pt !== 'this' && pt !== 'super')) && ((REGEX.lastIndex = i - 1), REGEX.test(src))) {
      add('r', src.slice(i - 1, REGEX.lastIndex))
      i = REGEX.lastIndex
    } else {
      if (stack.length) stack[stack.length - 1] += s === '{' ? 1 : s === '}' ? -1 : 0
      add(PUNCT.includes(s) ? 'o' : '', s)
    }
  }
  return out
}

/** Replaces the children of `el` with the highlighted source. Only text nodes and class-only spans are built: the source never touches innerHTML. */
export function paint(el: HTMLElement, src: string) {
  const f = document.createDocumentFragment()
  for (const [c, s] of tokenize(src)) {
    if (!c) f.append(s)
    else {
      const e = document.createElement('span')
      e.className = 'h-' + c
      e.textContent = s
      f.append(e)
    }
  }
  el.replaceChildren(f)
}
