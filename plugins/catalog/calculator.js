// calculator — inline calculator for the search box. Fully offline.
//
// Hook:   search(ctx) -> SearchResult[]
// Output: for a math query such as "1+2*3" the only result is
//           { title: "= 7", subtitle: "1+2*3", icon: "calculator", copy: "7" }
//         i.e. `title` is always "= <result>" and `copy` is the bare result,
//         so Enter copies the number. For anything that doesn't look like
//         math (or fails to parse, or isn't finite, e.g. 1/0) it returns [].
//
// Supported: + - * / % ^ (also ** × ÷), parentheses, unary minus/plus,
// decimals and exponents (1.5e3), constants pi and e, functions
// sqrt sin cos tan (radians) log (base 10) ln abs round(x[, digits]).
//   - `^` is right-associative and binds tighter than unary minus: -2^2 = -4.
//   - `%` is "modulo" when an operand follows (10%3 = 1) and "percent" when
//     it doesn't (50% = 0.5, 200*10% = 20).
//   - Results are rounded to 12 significant digits (0.1+0.2 = 0.3).
// A query must contain a digit plus an operator or a parenthesis, and perform
// at least one operation, so "42", "-5" and dates like 2026-11-03 are ignored.
//
// No eval / new Function: this is a small recursive-descent parser.

export const manifest = {
  id: "calculator",
  name: "Calculator",
  version: "1.0.0",
  description: "Inline calculator in the search box. 搜索框内联计算器。",
  author: "genkan",
  icon: "calculator",
  search: { name: "Calculator", prefix: "" },
};

const CONSTS = { pi: Math.PI, e: Math.E };
const FUNCS = {
  sqrt: Math.sqrt,
  sin: Math.sin,
  cos: Math.cos,
  tan: Math.tan,
  log: Math.log10,
  ln: Math.log,
  abs: Math.abs,
  round: (x, digits = 0) => {
    const k = 10 ** Math.trunc(digits);
    return Math.round(x * k) / k;
  },
};

// ---- lexer -----------------------------------------------------------------

function tokenize(src) {
  const toks = [];
  const re = /\s*(?:(\d+\.?\d*(?:e[+-]?\d+)?|\.\d+(?:e[+-]?\d+)?)|([a-z]+)|(.))/giy;
  let m;
  while (re.lastIndex < src.length && (m = re.exec(src))) {
    if (m[1] !== undefined) toks.push({ t: "num", v: parseFloat(m[1]) });
    else if (m[2] !== undefined) toks.push({ t: "id", v: m[2].toLowerCase() });
    else if (m[3] !== undefined) toks.push({ t: "op", v: m[3] });
  }
  return toks;
}

// ---- parser / evaluator ----------------------------------------------------
//   expr    = term (("+" | "-") term)*
//   term    = unary (("*" | "/" | "%") unary | "%")*
//   unary   = ("-" | "+") unary | power
//   power   = primary ("^" unary)?
//   primary = number | const | func "(" expr ("," expr)* ")" | "(" expr ")"

function evaluate(src) {
  const toks = tokenize(src);
  let pos = 0;
  let ops = 0; // operations performed, to ignore trivial input like "(5)"
  let depth = 0;
  const peek = (o = 0) => toks[pos + o];
  const isOp = (tok, v) => tok && tok.t === "op" && tok.v === v;
  const fail = (msg) => {
    throw new SyntaxError(msg);
  };
  const expect = (v) => (isOp(peek(), v) ? pos++ : fail("expected " + v));

  function expr() {
    let x = term();
    for (let t; (t = peek()) && (isOp(t, "+") || isOp(t, "-")); ) {
      pos++;
      const y = term();
      x = t.v === "+" ? x + y : x - y;
      ops++;
    }
    return x;
  }

  function term() {
    let x = unary();
    for (let t; (t = peek()) && (isOp(t, "*") || isOp(t, "/") || isOp(t, "%")); ) {
      pos++;
      if (t.v === "%") {
        const n = peek();
        const operandFollows = n && (n.t === "num" || n.t === "id" || isOp(n, "("));
        if (!operandFollows) {
          x /= 100; // postfix percent
          ops++;
          continue;
        }
      }
      const y = unary();
      x = t.v === "*" ? x * y : t.v === "/" ? x / y : x % y;
      ops++;
    }
    return x;
  }

  function unary() {
    if (isOp(peek(), "-")) return pos++, -unary();
    if (isOp(peek(), "+")) return pos++, unary();
    return power();
  }

  function power() {
    const base = primary();
    if (isOp(peek(), "^")) {
      pos++;
      ops++;
      return base ** unary(); // right-associative, allows 2^-1
    }
    return base;
  }

  function primary() {
    if (++depth > 40) fail("too deeply nested");
    const t = toks[pos++];
    if (!t) fail("unexpected end");
    let x;
    if (t.t === "num") x = t.v;
    else if (t.t === "id" && isOp(peek(), "(")) {
      const fn = Object.hasOwn(FUNCS, t.v) ? FUNCS[t.v] : fail("unknown function " + t.v);
      pos++;
      const args = [expr()];
      while (isOp(peek(), ",")) pos++, args.push(expr());
      expect(")");
      x = fn(...args.slice(0, 2));
      ops++;
    } else if (t.t === "id") x = Object.hasOwn(CONSTS, t.v) ? CONSTS[t.v] : fail("unknown name " + t.v);
    else if (isOp(t, "(")) {
      x = expr();
      expect(")");
    } else fail("unexpected " + t.v);
    depth--;
    return x;
  }

  const result = expr();
  if (pos !== toks.length) fail("trailing input");
  return { result, ops };
}

// ---- search hook -----------------------------------------------------------

export async function search(ctx) {
  // Normalise typographic operators and an optional leading/trailing "=".
  const q = String(ctx.query || "")
    .trim()
    .replace(/^=+|=+$/g, "")
    .replace(/×/g, "*")
    .replace(/÷/g, "/")
    .replace(/[−–]/g, "-")
    .replace(/\*\*/g, "^")
    .trim();

  // Cheap "does this look like math" gate before parsing.
  if (q.length === 0 || q.length > 200) return [];
  if (!/^[0-9a-z\s.,+\-*/%^()]+$/i.test(q)) return [];
  if (!/\d/.test(q) || !/[-+*/%^(]/.test(q)) return [];
  if (/^\d{4}[-/]\d{1,2}[-/]\d{1,2}$/.test(q)) return []; // a date, not a subtraction

  let r;
  try {
    r = evaluate(q);
  } catch (e) {
    return []; // not an expression we understand: stay silent
  }
  if (r.ops === 0 || !Number.isFinite(r.result)) return [];

  // Snap float noise such as cos(pi/2) = 6e-17 to 0, then keep 12 digits.
  const value = Math.abs(r.result) < 1e-12 ? 0 : r.result;
  const text = String(Number(value.toPrecision(12)));
  return [{ title: "= " + text, subtitle: q, icon: "calculator", copy: text }];
}
