package plugin

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/MoeForge/moejs"
)

// Limits for the html block and genkan.html.select (docs/PLUGINS.md §14).
const (
	maxHTMLOut      = 64 << 10 // sanitized output of one html block / selected element
	maxHTMLBlockIn  = 512 << 10
	maxHTMLSelectIn = 2 << 20 // input of genkan.html.select
	maxHTMLDepth    = 20
	maxSelectText   = 4000
	defaultSelect   = 10
	maxSelect       = 100
	maxSelectorLen  = 500
)

// htmlAllowed is the allow-list of elements an html block may contain. Every
// other element is unwrapped (its children are kept) unless it is in htmlDropped.
var htmlAllowed = setOf("p", "div", "span", "br", "hr", "h1", "h2", "h3", "h4", "h5", "h6",
	"strong", "b", "em", "i", "u", "s", "small", "sub", "sup", "code", "pre", "blockquote",
	"ul", "ol", "li", "dl", "dt", "dd", "table", "thead", "tbody", "tr", "th", "td",
	"a", "img", "figure", "figcaption", "time")

// htmlDropped elements are removed together with everything inside them
// (executable, embedding, form-control and non-content elements).
var htmlDropped = setOf("script", "style", "iframe", "frame", "frameset", "object", "embed", "applet",
	"svg", "math", "template", "noscript", "noembed", "noframes", "head", "title", "meta", "link", "base",
	"audio", "video", "source", "track", "canvas", "map", "area", "dialog", "plaintext", "xmp",
	"textarea", "select", "option", "optgroup", "datalist", "button", "input", "slot", "portal")

var htmlVoid = setOf("br", "hr", "img")

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

var digitsRe = regexp.MustCompile(`^[0-9]{1,4}$`)

// htmlCleaner rebuilds a parsed HTML tree into a fresh tree that only holds
// allow-listed elements and attributes. Nothing from the source tree is reused,
// so unknown constructs can never leak through.
type htmlCleaner struct {
	base    *url.URL // resolves relative URLs; nil = relative URLs are dropped
	budget  int      // rough byte budget left for the output
	visible bool     // output has visible text or an image
}

func newCleaner(base *url.URL, budget int) *htmlCleaner {
	return &htmlCleaner{base: base, budget: budget}
}

// parseBase accepts "" (no base) or an absolute http(s) URL.
func parseBase(s string) (*url.URL, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("base must be an absolute http(s) URL")
	}
	return u, nil
}

// absURL returns an absolute http(s) URL for raw, or "" when it is not one.
// Entities are already decoded by the HTML parser, so "&#106;avascript:" and
// "java\tscript:" reach this function in their real form; control characters
// are rejected outright because browsers strip them inside schemes.
func (c *htmlCleaner) absURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxURLLen {
		return ""
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !u.IsAbs() {
		if c.base == nil {
			return ""
		}
		u = c.base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	if s := u.String(); len(s) <= maxURLLen {
		return s
	}
	return ""
}

func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || (unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r') {
			return -1
		}
		return r
	}, s)
}

func (c *htmlCleaner) addText(dst *html.Node, s string) {
	s = cleanText(s)
	if s == "" || c.budget <= 0 {
		return
	}
	if len(s) > c.budget {
		s = strings.ToValidUTF8(s[:c.budget], "")
	}
	c.budget -= len(s)
	if strings.TrimSpace(s) != "" {
		c.visible = true
	}
	// merge with a preceding text node (unwrapped elements leave neighbours)
	if l := dst.LastChild; l != nil && l.Type == html.TextNode {
		l.Data += s
		return
	}
	dst.AppendChild(&html.Node{Type: html.TextNode, Data: s})
}

// textOf is the text content of n without script/style-like content.
func textOf(n *html.Node, sb *strings.Builder) {
	switch n.Type {
	case html.TextNode:
		sb.WriteString(n.Data)
	case html.ElementNode:
		if n.Namespace != "" || htmlDropped[strings.ToLower(n.Data)] {
			return
		}
		if tag := strings.ToLower(n.Data); tag == "br" || tag == "p" || tag == "div" || tag == "li" || tag == "tr" {
			sb.WriteByte(' ')
		}
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		textOf(ch, sb)
	}
}

func (c *htmlCleaner) cleanChildren(dst, src *html.Node, depth int) {
	for ch := src.FirstChild; ch != nil && c.budget > 0; ch = ch.NextSibling {
		c.cleanNode(dst, ch, depth)
	}
}

func (c *htmlCleaner) cleanNode(dst, n *html.Node, depth int) {
	if c.budget <= 0 {
		return
	}
	switch n.Type {
	case html.TextNode:
		c.addText(dst, n.Data)
	case html.DocumentNode:
		c.cleanChildren(dst, n, depth)
	case html.ElementNode:
		if n.Namespace != "" { // svg / math (foreign content)
			return
		}
		tag := strings.ToLower(n.Data)
		switch {
		case htmlDropped[tag]:
			return
		case !htmlAllowed[tag]:
			c.cleanChildren(dst, n, depth) // unknown wrapper: keep what is inside
			return
		case depth >= maxHTMLDepth: // too deep: keep only the text
			var sb strings.Builder
			textOf(n, &sb)
			c.addText(dst, sb.String())
			return
		}
		attrs, ok := c.cleanAttrs(tag, n)
		if !ok {
			// an <a> without a usable href is plain content, an <img> without src is nothing
			if tag == "a" {
				c.cleanChildren(dst, n, depth)
			}
			return
		}
		el := &html.Node{Type: html.ElementNode, Data: tag, DataAtom: atom.Lookup([]byte(tag)), Attr: attrs}
		c.budget -= 2*len(tag) + 5
		for _, a := range attrs {
			c.budget -= len(a.Key) + len(a.Val) + 4
		}
		if tag == "img" {
			c.visible = true
		}
		dst.AppendChild(el)
		if !htmlVoid[tag] {
			c.cleanChildren(el, n, depth+1)
		}
	}
}

// cleanAttrs keeps the allow-listed attributes of tag. ok=false means the
// element is useless without one that was missing or invalid.
func (c *htmlCleaner) cleanAttrs(tag string, n *html.Node) ([]html.Attribute, bool) {
	var out []html.Attribute
	have := map[string]bool{}
	put := func(k, v string) {
		if !have[k] {
			have[k] = true
			out = append(out, html.Attribute{Key: k, Val: v})
		}
	}
	for _, a := range n.Attr {
		if a.Namespace != "" {
			continue
		}
		switch k := strings.ToLower(a.Key); k {
		case "title":
			if s := cleanLine(a.Val, 300); s != "" {
				put(k, s)
			}
		case "datetime":
			if tag == "time" {
				if s := cleanLine(a.Val, 64); s != "" {
					put(k, s)
				}
			}
		case "href":
			if tag == "a" {
				if u := c.absURL(a.Val); u != "" {
					put(k, u)
				}
			}
		case "src":
			if tag == "img" {
				if u := c.absURL(a.Val); u != "" {
					put(k, u)
				}
			}
		case "alt":
			if tag == "img" {
				put(k, cleanLine(a.Val, 300))
			}
		case "width", "height":
			if tag == "img" && digitsRe.MatchString(strings.TrimSpace(a.Val)) {
				put(k, strings.TrimSpace(a.Val))
			}
		case "colspan", "rowspan":
			if (tag == "td" || tag == "th") && digitsRe.MatchString(strings.TrimSpace(a.Val)) {
				v := strings.TrimLeft(strings.TrimSpace(a.Val), "0")
				if v != "" && len(v) <= 3 {
					put(k, v)
				}
			}
		}
	}
	switch tag {
	case "a":
		if !have["href"] {
			return nil, false
		}
	case "img":
		if !have["src"] {
			return nil, false
		}
	}
	return out, true
}

// render serializes the children of root.
func renderChildren(root *html.Node) string {
	var buf bytes.Buffer
	for ch := root.FirstChild; ch != nil; ch = ch.NextSibling {
		if err := html.Render(&buf, ch); err != nil {
			return ""
		}
	}
	return buf.String()
}

// cleanTo runs the cleaner with a shrinking budget until the rendered output
// fits maxHTMLOut (escaping can make it larger than the rough estimate).
func cleanTo(base *url.URL, build func(c *htmlCleaner, root *html.Node)) string {
	budget := maxHTMLOut
	for i := 0; i < 6; i++ {
		c := newCleaner(base, budget)
		root := &html.Node{Type: html.DocumentNode}
		build(c, root)
		if !c.visible {
			return ""
		}
		out := strings.TrimSpace(renderChildren(root))
		if len(out) <= maxHTMLOut {
			return out
		}
		budget = budget * 3 / 4
	}
	return ""
}

// SanitizeHTML cleans an HTML fragment for the html block. Relative URLs are
// resolved against base (empty = relative URLs are dropped). The result holds
// only allow-listed elements and attributes, is at most 64 KiB and nested at
// most 20 deep; "" means nothing visible is left.
func SanitizeHTML(src, base string) string {
	bu, err := parseBase(base)
	if err != nil {
		bu = nil
	}
	if len(src) > maxHTMLBlockIn {
		src = strings.ToValidUTF8(src[:maxHTMLBlockIn], "")
	}
	ctx := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(src), ctx)
	if err != nil {
		return ""
	}
	return cleanTo(bu, func(c *htmlCleaner, root *html.Node) {
		for _, n := range nodes {
			c.cleanNode(root, n, 0)
		}
	})
}

// SelectHTML runs a CSS selector over a whole HTML document and returns the
// sanitized outer HTML and the text of up to limit matches (document order;
// matches nested inside an earlier match are skipped).
func SelectHTML(src, selector, base string, limit int) ([]map[string]any, error) {
	if len(src) > maxHTMLSelectIn {
		return nil, fmt.Errorf("html is larger than %d MiB", maxHTMLSelectIn>>20)
	}
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, errors.New("selector is empty")
	}
	if len(selector) > maxSelectorLen {
		return nil, errors.New("selector is too long")
	}
	sel, err := cascadia.Compile(selector)
	if err != nil {
		return nil, fmt.Errorf("invalid selector %q: %v", selector, err)
	}
	bu, err := parseBase(base)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultSelect
	}
	if limit > maxSelect {
		limit = maxSelect
	}
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("cannot parse html: %v", err)
	}
	out := []map[string]any{}
	chosen := map[*html.Node]bool{}
	for _, m := range sel.MatchAll(doc) {
		if len(out) >= limit {
			break
		}
		nested := false
		for p := m.Parent; p != nil; p = p.Parent {
			if chosen[p] {
				nested = true
				break
			}
		}
		if nested {
			continue
		}
		chosen[m] = true
		h := cleanTo(bu, func(c *htmlCleaner, root *html.Node) { c.cleanNode(root, m, 0) })
		if h == "" {
			continue
		}
		var sb strings.Builder
		textOf(m, &sb)
		text := strings.Join(strings.Fields(cleanText(sb.String())), " ")
		if utf8.RuneCountInString(text) > maxSelectText {
			text = string([]rune(text)[:maxSelectText])
		}
		out = append(out, map[string]any{"html": h, "text": text})
	}
	return out, nil
}

// htmlAPI builds genkan.html (docs/PLUGINS.md §14.2). It is pure computation,
// so it is also available while a plugin is only being inspected.
func htmlAPI(rt *moejs.Runtime) map[string]any {
	return map[string]any{
		"select": moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			src, sel := moejs.Arg(args, 0), moejs.Arg(args, 1)
			if !src.IsString() {
				return moejs.Undefined(), errors.New("genkan.html.select(html, selector, opts): html must be a string")
			}
			if !sel.IsString() {
				return moejs.Undefined(), errors.New("genkan.html.select(html, selector, opts): selector must be a string")
			}
			var base string
			limit := defaultSelect
			if ov := moejs.Arg(args, 2); ov.IsObject() {
				g, err := rt.ToGoWith(ov, moejs.DecodeOptions{MaxBytes: 4 << 10})
				if err != nil {
					return moejs.Undefined(), fmt.Errorf("genkan.html.select: bad options: %v", err)
				}
				o, _ := g.(map[string]any)
				base, _ = o["base"].(string)
				if f, ok := finite(o["limit"]); ok && f >= 1 {
					limit = int(f)
				}
			}
			res, err := SelectHTML(src.String(), sel.String(), base, limit)
			if err != nil {
				return moejs.Undefined(), fmt.Errorf("genkan.html.select: %v", err)
			}
			return rt.FromGo(res)
		}),
	}
}
