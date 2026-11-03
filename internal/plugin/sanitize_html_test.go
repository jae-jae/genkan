package plugin

import (
	"fmt"
	"strings"
	"testing"
)

// assertSafe fails when out holds anything an html block must never contain.
func assertSafe(t *testing.T, in, out string) {
	t.Helper()
	low := strings.ToLower(out)
	for _, bad := range []string{"<script", "<style", "<svg", "<iframe", "<object", "<embed", "<form", "<input", "<math", "<video", "<link", "<meta", "<base",
		"javascript:", "vbscript:", "data:", "onerror", "onclick", "onload", "srcset", " style=", " class=", " id=", "alert("} {
		if strings.Contains(low, bad) {
			t.Errorf("input %q: output %q contains %q", in, out, bad)
		}
	}
}

func TestSanitizeHTMLXSSVectors(t *testing.T) {
	vectors := []string{
		`<script>alert(1)</script>ok`,
		`<SCRIPT SRC=//evil.example/x.js></SCRIPT>ok`,
		`<img src=x onerror=alert(1)>ok`,
		`<img src="javascript:alert(1)">ok`,
		`<a href="javascript:alert(1)">ok</a>`,
		`<a href="JaVaScRiPt:alert(1)">ok</a>`,
		`<a href="java&#x09;script:alert(1)">ok</a>`,
		`<a href="java&#10;script:alert(1)">ok</a>`,
		`<a href="&#106;&#97;&#118;&#97;&#115;&#99;&#114;&#105;&#112;&#116;&#58;alert(1)">ok</a>`,
		`<a href="&#x6A;avascript&colon;alert(1)">ok</a>`,
		`<a href=" javascript:alert(1)">ok</a>`,
		"<a href=\"\x01javascript:alert(1)\">ok</a>",
		`<a href="vbscript:msgbox(1)">ok</a>`,
		`<a href="data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==">ok</a>`,
		`<img src="data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=">ok`,
		`<svg onload=alert(1)><circle/></svg>ok`,
		`<svg><script>alert(1)</script></svg>ok`,
		`<svg><style>*{}</style><a xlink:href="javascript:alert(1)"><text>x</text></a></svg>ok`,
		`<math><mtext><table><mglyph><style><img src=x onerror=alert(1)>ok`,
		`<style>body{background:url(javascript:alert(1))}</style>ok`,
		`<p style="background:url(javascript:alert(1))">ok</p>`,
		`<iframe src="https://evil.example"></iframe>ok`,
		`<iframe srcdoc="<script>alert(1)</script>"></iframe>ok`,
		`<object data="x.swf"></object><embed src="x.swf">ok`,
		`<form action="https://evil.example"><input name=a><button>go</button></form>ok`,
		`<div class="x" id="y" onclick="alert(1)" onmouseover=alert(1)>ok</div>`,
		`<img src="https://a.example/x.png" srcset="javascript:alert(1) 1x, https://b.example/y.png 2x" onerror=alert(1)>ok`,
		`<picture><source srcset="x.png"><img src="https://a.example/x.png"></picture>ok`,
		`<video src="x.mp4" onerror=alert(1)></video><audio src=x onerror=alert(1)></audio>ok`,
		`<noscript><p title="</noscript><img src=x onerror=alert(1)>">ok</noscript>`,
		`<template><script>alert(1)</script></template>ok`,
		`<!--<script>alert(1)</script>-->ok<!-- [if IE]><img src=x onerror=alert(1)> -->`,
		`<base href="javascript:alert(1)//"><a href="x">ok</a>`,
		`<meta http-equiv="refresh" content="0;url=javascript:alert(1)">ok`,
		`<link rel=stylesheet href="https://evil.example/x.css">ok`,
		`<a href="https://a.example" target="_self" onfocus=alert(1) autofocus>ok</a>`,
		`<table><tr><td onclick=alert(1) style=x colspan="2">ok</td></tr></table>`,
		`<textarea><script>alert(1)</script></textarea>ok`,
		`<xmp><script>alert(1)</script></xmp>ok`,
		`<<script>alert(1);//<</script>ok`,
		`<img/src="x"/onerror=alert(1)>ok`,
		`<a href="//evil.example/x">ok</a>`,
		`<a href="data&colon;text/html,&lt;script&gt;alert(1)&lt;/script&gt;">ok</a>`,
	}
	for _, in := range vectors {
		out := SanitizeHTML(in, "")
		assertSafe(t, in, out)
		// same with a base: relative and scheme-relative URLs get resolved but never become script URLs
		assertSafe(t, in, SanitizeHTML(in, "https://base.example/dir/"))
	}
	// text survives where there is some
	if out := SanitizeHTML(`<script>alert(1)</script>ok`, ""); out != "ok" {
		t.Errorf("script stripped, text kept: %q", out)
	}
}

func TestSanitizeHTMLAllowedMarkup(t *testing.T) {
	cases := []struct{ in, base, want string }{
		{`<p>Hello <strong>world</strong> &amp; <em>all</em></p>`, "", `<p>Hello <strong>world</strong> &amp; <em>all</em></p>`},
		{`<a href="https://a.example/x?y=1&z=2" title="T" class="c">link</a>`, "", `<a href="https://a.example/x?y=1&amp;z=2" title="T">link</a>`},
		{`<a href="/rel/path?x=1">rel</a>`, "https://site.example/dir/page", `<a href="https://site.example/rel/path?x=1">rel</a>`},
		{`<a href="sub/x">rel</a>`, "https://site.example/dir/page", `<a href="https://site.example/dir/sub/x">rel</a>`},
		{`<a href="//cdn.example/x">proto-relative</a>`, "https://site.example/", `<a href="https://cdn.example/x">proto-relative</a>`},
		{`<a href="/rel">rel</a>`, "", `rel`},                                   // no base: relative link dropped, text kept
		{`<a href="javascript:alert(1)">bad</a> good`, "", `bad good`},             // unusable link unwrapped
		{`<img src="/i.png" alt="A" width="40" height="30" srcset="x 2x">`, "https://s.example/", `<img src="https://s.example/i.png" alt="A" width="40" height="30"/>`},
		{`<img src="/i.png" width="100%" height="auto">`, "https://s.example/", `<img src="https://s.example/i.png"/>`},
		{`before<img src="data:image/png;base64,AAAA">after`, "", `beforeafter`},
		{`<table><thead><tr><th colspan="2" scope="col">H</th></tr></thead><tbody><tr><td rowspan="0">a</td><td colspan="9999">b</td></tr></tbody></table>`, "",
			`<table><thead><tr><th colspan="2">H</th></tr></thead><tbody><tr><td>a</td><td>b</td></tr></tbody></table>`},
		{`<time datetime="2026-11-03" title="d" lang="en">Nov 3</time>`, "", `<time datetime="2026-11-03" title="d">Nov 3</time>`},
		{`<article><header><h2>Title</h2></header><section><p>Body</p></section></article>`, "", `<h2>Title</h2><p>Body</p>`}, // unknown wrappers unwrap
		{`<ul><li>a</li><li>b<br>c</li></ul><hr>`, "", `<ul><li>a</li><li>b<br/>c</li></ul><hr/>`},
		{`<figure><img src="https://a.example/x.jpg"><figcaption>cap</figcaption></figure>`, "", `<figure><img src="https://a.example/x.jpg"/><figcaption>cap</figcaption></figure>`},
		{`<font color=red><center>x</center></font>`, "", `x`},
		{`<p>a < b && c > d</p>`, "", `<p>a &lt; b &amp;&amp; c &gt; d</p>`},
	}
	for _, c := range cases {
		if got := SanitizeHTML(c.in, c.base); got != c.want {
			t.Errorf("SanitizeHTML(%q, %q)\n got  %q\n want %q", c.in, c.base, got, c.want)
		}
	}
	// nothing visible -> ""
	for _, in := range []string{"", "   ", "<div></div>", "<script>x</script>", "<p> </p>", "<!-- c -->"} {
		if got := SanitizeHTML(in, ""); got != "" {
			t.Errorf("%q should be empty, got %q", in, got)
		}
	}
	// invalid base is ignored (relative URLs are then dropped, absolute ones kept)
	if got := SanitizeHTML(`<a href="/x">a</a><a href="https://k.example/">b</a>`, "javascript:alert(1)"); got != `a<a href="https://k.example/">b</a>` {
		t.Errorf("bad base: %q", got)
	}
}

func TestSanitizeHTMLCaps(t *testing.T) {
	// output <= 64 KiB, even with markup that grows when escaped
	big := strings.Repeat(`<p>a&b <b>c</b></p>`, 20000)
	out := SanitizeHTML(big, "")
	if len(out) == 0 || len(out) > maxHTMLOut {
		t.Fatalf("output is %d bytes (limit %d)", len(out), maxHTMLOut)
	}
	if strings.Count(out, "<p>") != strings.Count(out, "</p>") {
		t.Errorf("truncated output is not well formed")
	}
	if got := SanitizeHTML(strings.Repeat("&", 200000), ""); len(got) > maxHTMLOut {
		t.Errorf("escaped output is %d bytes", len(got))
	}
	// nesting <= 20: deeper elements are flattened to their text
	deep := strings.Repeat("<div>", 40) + "deep text" + strings.Repeat("</div>", 40)
	out = SanitizeHTML(deep, "")
	if n := strings.Count(out, "<div>"); n != maxHTMLDepth {
		t.Errorf("want %d nested divs, got %d: %s", maxHTMLDepth, n, out)
	}
	if !strings.Contains(out, "deep text") {
		t.Errorf("text of too deep elements is kept: %s", out)
	}
	// pathological nesting: flattened at 20 levels, and the parser itself refuses beyond 512 open elements
	if out := SanitizeHTML(strings.Repeat("<span>", 400)+"x", ""); strings.Count(out, "<span>") != maxHTMLDepth || !strings.Contains(out, "x") {
		t.Errorf("400 nested spans: %q", out)
	}
	if out := SanitizeHTML(strings.Repeat("<span>", 100000)+"x", ""); out != "" {
		t.Errorf("unparsable nesting gives nothing, got %d bytes", len(out))
	}
}

func TestSanitizeBlockTypes(t *testing.T) {
	long := strings.Repeat("x", 3000)
	v := SanitizeView(map[string]any{"body": []any{
		// html
		map[string]any{"type": "html", "html": `<p onclick="x">hi <a href="/a">a</a></p><script>x</script>`, "base": "https://b.example/"},
		map[string]any{"type": "html", "html": `<script>x</script>`},
		map[string]any{"type": "html", "html": 5},
		// iframe
		map[string]any{"type": "iframe", "src": "https://a.example/p", "height": 99999.0, "zoom": 9.0, "refresh": 1.0, "ratio": 16.0 / 9, "crop": map[string]any{"x": -5.0, "y": 10.4, "width": 300.0, "height": 200.0}},
		map[string]any{"type": "iframe", "src": "https://a.example/p"},
		map[string]any{"type": "iframe", "src": "https://a.example/p", "height": 1.0, "zoom": 0.01, "refresh": 0.0, "crop": map[string]any{"width": 0.0, "height": 5.0}},
		map[string]any{"type": "iframe", "src": "javascript:alert(1)"},
		map[string]any{"type": "iframe", "src": "data:text/html,<script>alert(1)</script>"},
		map[string]any{"type": "iframe", "src": "ftp://a.example/"},
		map[string]any{"type": "iframe"},
		// media
		map[string]any{"type": "media", "kind": "video", "src": "https://a.example/v.mp4", "poster": "https://a.example/p.jpg", "title": "T", "autoplay": true, "loop": true, "controls": false},
		map[string]any{"type": "media", "kind": "video", "src": "https://a.example/v.mp4", "autoplay": true, "muted": true},
		map[string]any{"type": "media", "kind": "audio", "src": "http://radio.example/stream", "poster": "https://a.example/p.jpg", "title": long},
		map[string]any{"type": "media", "kind": "video", "src": "javascript:alert(1)"},
		map[string]any{"type": "media", "kind": "video", "src": "https://a.example/v.mp4", "poster": "javascript:alert(1)"},
		map[string]any{"type": "media", "kind": "flash", "src": "https://a.example/v.swf"},
		map[string]any{"type": "media", "kind": "audio", "src": "data:audio/mp3;base64,AAAA"},
		// image: single (unchanged), slideshow
		map[string]any{"type": "image", "src": "https://a.example/i.png", "alt": "A", "href": "https://a.example/", "ratio": 2.0},
		map[string]any{"type": "image", "src": "https://a.example/i.png", "fit": "contain", "height": 5000.0},
		map[string]any{"type": "image", "interval": 1.0, "fit": "weird", "height": 10.0, "images": []any{
			map[string]any{"src": "https://a.example/1.png", "alt": "one", "href": "https://a.example/1"},
			map[string]any{"src": "javascript:alert(1)"},
			"junk",
			map[string]any{"src": "https://a.example/2.png", "href": "javascript:x"},
		}},
		map[string]any{"type": "image", "images": []any{map[string]any{"src": "javascript:1"}}},
		map[string]any{"type": "image", "images": []any{}},
	}})
	b := v.Body
	var kinds []string
	for _, x := range b {
		kinds = append(kinds, fmt.Sprint(x["type"]))
	}
	if got := strings.Join(kinds, ","); got != "html,iframe,iframe,iframe,media,media,media,media,image,image,image" {
		t.Fatalf("blocks: %s", got)
	}
	if h := b[0]["html"]; h != `<p>hi <a href="https://b.example/a">a</a></p>` {
		t.Errorf("html block: %v", h)
	}
	if _, has := b[0]["base"]; has {
		t.Error("base must not be passed on")
	}
	// iframe
	f := b[1]
	crop := f["crop"].(map[string]any)
	if f["src"] != "https://a.example/p" || f["height"] != 1600 || f["zoom"] != 2.0 || f["refresh"] != 5 || crop["x"] != 0 || crop["y"] != 10 || crop["width"] != 300 || crop["height"] != 200 || f["ratio"] != 1.778 {
		t.Errorf("iframe clamps: %+v", f)
	}
	if f := b[2]; f["height"] != 360 || f["crop"] != nil || f["zoom"] != nil || f["refresh"] != nil {
		t.Errorf("iframe defaults: %+v", f)
	}
	if f := b[3]; f["height"] != 120 || f["zoom"] != 0.25 || f["refresh"] != nil || f["crop"] != nil {
		t.Errorf("iframe lower clamps: %+v", f)
	}
	// media: autoplay needs muted
	m := b[4]
	if m["autoplay"] != nil || m["muted"] != nil || m["loop"] != true || m["controls"] != false || m["poster"] != "https://a.example/p.jpg" || m["title"] != "T" {
		t.Errorf("video: %+v", m)
	}
	if m := b[5]; m["autoplay"] != true || m["muted"] != true {
		t.Errorf("muted autoplay: %+v", m)
	}
	if m := b[6]; m["kind"] != "audio" || m["poster"] != nil || utf8Len(m["title"].(string)) != 200 {
		t.Errorf("audio: %+v", m)
	}
	if m := b[7]; m["poster"] != nil || m["src"] != "https://a.example/v.mp4" {
		t.Errorf("bad poster dropped, video kept: %+v", m)
	}
	// images
	if im := b[8]; im["src"] != "https://a.example/i.png" || im["alt"] != "A" || im["href"] != "https://a.example/" || im["ratio"] != 2.0 || im["images"] != nil {
		t.Errorf("single image: %+v", im)
	}
	if im := b[9]; im["fit"] != "contain" || im["height"] != 1000 {
		t.Errorf("single image fit/height: %+v", im)
	}
	im := b[10]
	slides, _ := im["images"].([]map[string]any)
	if len(slides) != 2 || slides[0]["src"] != "https://a.example/1.png" || slides[0]["href"] != "https://a.example/1" || slides[0]["alt"] != "one" || slides[1]["href"] != nil {
		t.Errorf("slides: %+v", im)
	}
	if im["interval"] != 3 || im["fit"] != nil || im["height"] != 60 || im["src"] != nil {
		t.Errorf("slideshow settings: %+v", im)
	}
	// more than maxSlides images are cut
	var many []any
	for i := 0; i < 40; i++ {
		many = append(many, map[string]any{"src": fmt.Sprintf("https://a.example/%d.png", i)})
	}
	v = SanitizeView(map[string]any{"body": []any{map[string]any{"type": "image", "images": many}}})
	if n := len(v.Body[0]["images"].([]map[string]any)); n != maxSlides {
		t.Errorf("slides kept: %d", n)
	}
}

const selectDoc = `<!doctype html><html><head><title>T</title><script>var x=1</script></head><body>
<ul id="news">
  <li class="item"><span class="titleline"><a href="/item?id=1" onclick="x()">First &amp; best</a></span> <span class="age">1 hour</span></li>
  <li class="item"><span class="titleline"><a href="https://other.example/2">Second</a></span></li>
  <li class="item"><span class="titleline"><a href="javascript:alert(1)">Third</a></span></li>
  <li class="item"><span class="titleline"><a href="//cdn.example/4">Fourth</a></span></li>
</ul>
<div class="outer"><div class="outer">nested <b>x</b></div></div>
<article><script>evil()</script><p>Para <img src="/pic.png" srcset="a 1x"> end</p></article>
<table><tr class="row"><td>a</td><td>b</td></tr></table>
</body></html>`

func TestSelectHTML(t *testing.T) {
	res, err := SelectHTML(selectDoc, ".titleline > a", "https://news.example/front/", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 4 {
		t.Fatalf("matches: %+v", res)
	}
	wants := []struct{ html, text string }{
		{`<a href="https://news.example/item?id=1">First &amp; best</a>`, "First & best"},
		{`<a href="https://other.example/2">Second</a>`, "Second"},
		{`Third`, "Third"}, // javascript: link unwrapped to its text
		{`<a href="https://cdn.example/4">Fourth</a>`, "Fourth"},
	}
	for i, w := range wants {
		if res[i]["html"] != w.html || res[i]["text"] != w.text {
			t.Errorf("match %d: %+v, want %+v", i, res[i], w)
		}
	}
	// limit
	res, _ = SelectHTML(selectDoc, "li.item", "https://news.example/", 2)
	if len(res) != 2 || !strings.Contains(res[0]["html"].(string), "<li>") || res[0]["text"] != "First & best 1 hour" {
		t.Errorf("limit 2: %+v", res)
	}
	if res, _ = SelectHTML(selectDoc, "li", "", 1000); len(res) != 4 {
		t.Errorf("limit above the number of matches: %d", len(res))
	}
	// without base relative URLs are dropped; absolute ones stay
	res, _ = SelectHTML(selectDoc, ".titleline", "", 10)
	if res[0]["html"] != `<span>First &amp; best</span>` || res[1]["html"] != `<span><a href="https://other.example/2">Second</a></span>` {
		t.Errorf("no base: %+v", res)
	}
	// elements outside the allow-list unwrap; script is gone; srcset is stripped; img resolves
	res, _ = SelectHTML(selectDoc, "article", "https://s.example/a/b", 10)
	if len(res) != 1 || res[0]["html"] != `<p>Para <img src="https://s.example/pic.png"/> end</p>` || res[0]["text"] != "Para end" {
		t.Errorf("article: %+v", res)
	}
	// matches nested in an earlier match are skipped
	if res, _ = SelectHTML(selectDoc, "div.outer", "", 10); len(res) != 1 {
		t.Errorf("nested: %+v", res)
	}
	// table rows survive in a full-document parse
	if res, _ = SelectHTML(selectDoc, "tr.row", "", 10); len(res) != 1 || res[0]["html"] != `<tr><td>a</td><td>b</td></tr>` {
		t.Errorf("tr: %+v", res)
	}
	// selecting a script or other dropped element gives nothing
	if res, _ = SelectHTML(selectDoc, "script", "", 10); len(res) != 0 {
		t.Errorf("script: %+v", res)
	}
	// no match is an empty list, not an error
	if res, err = SelectHTML(selectDoc, ".nope", "", 10); err != nil || res == nil || len(res) != 0 {
		t.Errorf("no match: %+v %v", res, err)
	}
	// results never contain unsafe markup
	for _, sel := range []string{"body", "html", "*", "ul", "article"} {
		res, _ := SelectHTML(selectDoc, sel, "https://x.example/", 100)
		for _, r := range res {
			assertSafe(t, sel, r["html"].(string))
		}
	}
}

func TestSelectHTMLErrors(t *testing.T) {
	for _, sel := range []string{"div[", ">>", "a:nope(", "", "   ", "((", strings.Repeat("a", 600)} {
		if _, err := SelectHTML(selectDoc, sel, "", 5); err == nil {
			t.Errorf("selector %q should fail", sel)
		}
	}
	_, err := SelectHTML(selectDoc, "div[", "", 5)
	if err == nil || !strings.Contains(err.Error(), "invalid selector") || !strings.Contains(err.Error(), "div[") {
		t.Errorf("error should name the selector: %v", err)
	}
	if _, err := SelectHTML(selectDoc, "p", "javascript:alert(1)", 5); err == nil || !strings.Contains(err.Error(), "base") {
		t.Errorf("bad base: %v", err)
	}
	// 2 MiB input cap
	big := strings.Repeat("a", maxHTMLSelectIn+1)
	if _, err := SelectHTML(big, "p", "", 5); err == nil || !strings.Contains(err.Error(), "2 MiB") {
		t.Errorf("size cap: %v", err)
	}
	if _, err := SelectHTML(big[:maxHTMLSelectIn], "p", "", 5); err != nil {
		t.Errorf("exactly 2 MiB is fine: %v", err)
	}
}

// genkan.html.select as plugin code sees it.
func TestHostHTMLSelect(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"hs", name:"HS", version:"1", widgets:[{id:"w",name:"W"}]}`, `
export async function widget(ctx) {
  const doc = '<ul><li><a href="a">A</a></li><li><a href="b">B</a></li><li><a href="c">C</a></li></ul>';
  const all = genkan.html.select(doc, "li a", { base: "https://h.example/x/", limit: 2 });
  const dflt = genkan.html.select(doc, "li a");
  let err = "";
  try { genkan.html.select(doc, "a[[", {}); } catch (e) { err = e.message; }
  let err2 = "";
  try { genkan.html.select(42, "a"); } catch (e) { err2 = e.message; }
  return { body: [{ type: "text", text: JSON.stringify({ all, n: dflt.length, first: dflt[0], err, err2 }) }] };
}`))
	res, err := e.m.Test(t.Context(), "hs", TestRequest{Hook: "widget"})
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	s, _ := jsonString(res.Result)
	for _, want := range []string{`\"n\":3`, `https://h.example/x/a`, `https://h.example/x/b`, `invalid selector`, `html must be a string`} {
		if !strings.Contains(s, want) {
			t.Errorf("result lacks %s: %s", want, s)
		}
	}
	if strings.Contains(s, "h.example/x/c") {
		t.Errorf("limit 2 ignored: %s", s)
	}
	// the default (no base) drops relative links
	if !strings.Contains(s, `\"first\":{\"html\":\"A\"`) {
		t.Errorf("no-base result: %s", s)
	}
}
