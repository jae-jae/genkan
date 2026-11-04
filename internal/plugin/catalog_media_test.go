package plugin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// mediaView runs the widget hook of one media widget with the given config.
func mediaView(t *testing.T, e *tenv, widget string, cfg map[string]any) WidgetView {
	t.Helper()
	res, err := e.m.Test(context.Background(), "media", TestRequest{Hook: "widget", Target: widget, Config: cfg})
	if err != nil || !res.OK {
		t.Fatalf("%s %v: %+v %v", widget, cfg, res, err)
	}
	return res.Result.(WidgetView)
}

// mediaRejected: config validation refuses the value before the hook runs.
func mediaRejected(t *testing.T, e *tenv, widget string, cfg map[string]any) {
	t.Helper()
	if res, err := e.m.Test(context.Background(), "media", TestRequest{Hook: "widget", Target: widget, Config: cfg}); err == nil || !strings.Contains(err.Error(), "http(s)") {
		t.Errorf("%s %v should be refused as config: %+v %v", widget, cfg, res, err)
	}
}

func mediaEnv(t *testing.T) *tenv {
	e := catalogEnv(t)
	installCatalog(t, e, "media")
	return e
}

func TestCatalogMediaManifest(t *testing.T) {
	e := mediaEnv(t)
	inf, err := e.m.Info("media", true)
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[string]string{}
	for _, w := range inf.Widgets {
		sizes[w.ID] = w.Size
	}
	want := map[string]string{"page": "l", "snippet": "m", "image": "m", "video": "m", "audio": "s"}
	if fmt.Sprint(sizes) != fmt.Sprint(want) {
		t.Errorf("widgets and default sizes: %v, want %v", sizes, want)
	}
	// no fixed fetch hosts: the browser loads page/video/audio, the snippet url field opens its own host
	if len(inf.Permissions.Fetch) != 0 {
		t.Errorf("permissions.fetch: %v", inf.Permissions.Fetch)
	}
	for _, w := range inf.Widgets {
		for _, f := range w.Config {
			if f.AllowFetch != (w.ID == "snippet" && f.Key == "url") {
				t.Errorf("%s.%s allowFetch=%v", w.ID, f.Key, f.AllowFetch)
			}
		}
	}
}

func TestCatalogMediaPageAndImage(t *testing.T) {
	e := mediaEnv(t)
	v := mediaView(t, e, "page", map[string]any{"url": "https://example.com/dash", "height": 500, "cropX": 10, "cropY": 20, "cropW": 300, "cropH": 200, "zoom": 1.5, "refresh": 60})
	b := v.Body[0]
	crop, _ := b["crop"].(map[string]any)
	if len(v.Body) != 1 || b["type"] != "iframe" || b["src"] != "https://example.com/dash" || b["height"] != 500 || b["zoom"] != 1.5 || b["refresh"] != 60 ||
		crop["x"] != 10 || crop["y"] != 20 || crop["width"] != 300 || crop["height"] != 200 || v.Title != "example.com" || v.Link != "https://example.com/dash" {
		t.Fatalf("page: %+v", v)
	}
	// defaults: no crop (width and height both needed), no refresh
	v = mediaView(t, e, "page", map[string]any{"url": "https://example.com/", "cropW": 300})
	if b := v.Body[0]; b["crop"] != nil || b["refresh"] != nil || b["height"] != 360 {
		t.Fatalf("page defaults: %+v", b)
	}
	if v = mediaView(t, e, "page", map[string]any{"url": ""}); v.Error == "" || len(v.Body) != 0 {
		t.Errorf("empty url: %+v", v)
	}
	mediaRejected(t, e, "page", map[string]any{"url": "javascript:alert(1)"}) // the config form refuses non-http urls

	// image slideshow
	v = mediaView(t, e, "image", map[string]any{
		"urls": "https://a.example/1.jpg\n\n  https://a.example/2.jpg \njavascript:alert(1)\nftp://x/y.png\nhttps://a.example/3.jpg\r\n",
		"interval": 8, "fit": "contain", "height": 240, "link": "https://a.example/gallery",
	})
	im := v.Body[0]
	slides, _ := im["images"].([]map[string]any)
	if im["type"] != "image" || len(slides) != 3 || slides[1]["src"] != "https://a.example/2.jpg" || slides[0]["href"] != "https://a.example/gallery" ||
		im["interval"] != 8 || im["fit"] != "contain" || im["height"] != 240 {
		t.Fatalf("image: %+v", im)
	}
	if v = mediaView(t, e, "image", map[string]any{"urls": "nope\nftp://x"}); v.Error == "" {
		t.Errorf("no valid image url: %+v", v)
	}
	// a single image is a one-slide slideshow with the default settings
	v = mediaView(t, e, "image", map[string]any{"urls": "https://a.example/1.jpg"})
	if im := v.Body[0]; im["interval"] != 6 || im["fit"] != "cover" || len(im["images"].([]map[string]any)) != 1 {
		t.Errorf("single image: %+v", im)
	}
}

func TestCatalogMediaVideoDetection(t *testing.T) {
	e := mediaEnv(t)
	const yt = "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"
	iframes := map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":                    yt,
		"https://youtube.com/watch?v=dQw4w9WgXcQ&list=PL1&index=2":       yt,
		"https://www.youtube.com/watch?feature=share&v=dQw4w9WgXcQ":      yt,
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ":                      yt,
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ":                  yt,
		"https://youtu.be/dQw4w9WgXcQ":                                   yt,
		"https://youtu.be/dQw4w9WgXcQ?si=abc":                            yt,
		"https://www.youtube.com/shorts/dQw4w9WgXcQ":                     yt,
		"https://www.youtube.com/live/dQw4w9WgXcQ?feature=share":         yt,
		"https://www.youtube.com/embed/dQw4w9WgXcQ":                      yt,
		"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ":             yt,
		"https://youtu.be/dQw4w9WgXcQ?t=1m30s":                           yt + "?start=90",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42":               yt + "?start=42",
		"https://www.bilibili.com/video/BV1GJ411x7h7":                    "https://player.bilibili.com/player.html?bvid=BV1GJ411x7h7&autoplay=0",
		"https://www.bilibili.com/video/BV1GJ411x7h7/?spm_id_from=333.1": "https://player.bilibili.com/player.html?bvid=BV1GJ411x7h7&autoplay=0",
		"https://bilibili.com/video/BV1GJ411x7h7?p=2":                    "https://player.bilibili.com/player.html?bvid=BV1GJ411x7h7&autoplay=0",
		"https://m.bilibili.com/video/BV1GJ411x7h7":                      "https://player.bilibili.com/player.html?bvid=BV1GJ411x7h7&autoplay=0",
	}
	for in, want := range iframes {
		v := mediaView(t, e, "video", map[string]any{"url": in})
		b := v.Body[0]
		if b["type"] != "iframe" || b["src"] != want || b["ratio"] != 1.778 {
			t.Errorf("%s -> %+v, want iframe %s", in, b, want)
		}
	}
	// everything else is a media video block; look-alike hosts and embedded URLs are not detected
	plain := []string{
		"https://example.com/clip.mp4",
		"https://cdn.example.com/a/b.webm?token=1",
		"https://vimeo.com/76979871",
		"https://evilyoutube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com.evil.example/watch?v=dQw4w9WgXcQ",
		"https://evil.example/?u=https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=short",
		"https://www.youtube.com/channel/UC1234567890123456789012",
		"https://www.bilibili.com/bangumi/play/ep123",
		"https://bilibili.com.evil.example/video/BV1GJ411x7h7",
		"http://127.0.0.1:9/video.mp4",
	}
	for _, in := range plain {
		v := mediaView(t, e, "video", map[string]any{"url": in})
		if b := v.Body[0]; b["type"] != "media" || b["kind"] != "video" || b["src"] != in || b["controls"] != true {
			t.Errorf("%s -> %+v, want media video", in, b)
		}
	}
	// options: poster/loop/controls/autoplay on media; autoplay always muted
	v := mediaView(t, e, "video", map[string]any{"url": "https://example.com/c.mp4", "poster": "https://example.com/p.jpg", "autoplay": true, "loop": true, "controls": false})
	b := v.Body[0]
	if b["poster"] != "https://example.com/p.jpg" || b["autoplay"] != true || b["muted"] != true || b["loop"] != true || b["controls"] != false {
		t.Errorf("media options: %+v", b)
	}
	mediaRejected(t, e, "video", map[string]any{"url": "https://example.com/c.mp4", "poster": "javascript:alert(1)"})
	// options on embeds
	b = mediaView(t, e, "video", map[string]any{"url": "https://youtu.be/dQw4w9WgXcQ", "autoplay": true, "loop": true, "controls": false}).Body[0]
	if b["src"] != yt+"?autoplay=1&mute=1&loop=1&playlist=dQw4w9WgXcQ&controls=0" {
		t.Errorf("youtube options: %v", b["src"])
	}
	if b = mediaView(t, e, "video", map[string]any{"url": "https://www.bilibili.com/video/BV1GJ411x7h7", "autoplay": true}).Body[0]; !strings.HasSuffix(b["src"].(string), "&autoplay=1") {
		t.Errorf("bilibili autoplay: %v", b["src"])
	}
	mediaRejected(t, e, "video", map[string]any{"url": "ftp://x/y.mp4"})
}

func TestCatalogMediaAudio(t *testing.T) {
	e := mediaEnv(t)
	v := mediaView(t, e, "audio", map[string]any{"url": "http://radio.example:8000/stream.mp3", "title": "Lofi Radio", "loop": true})
	b := v.Body[0]
	if b["type"] != "media" || b["kind"] != "audio" || b["src"] != "http://radio.example:8000/stream.mp3" || b["title"] != "Lofi Radio" || b["loop"] != true || b["controls"] != true || v.Title != "Lofi Radio" {
		t.Fatalf("audio: %+v", v)
	}
	if v = mediaView(t, e, "audio", map[string]any{"url": "https://x.example/a.mp3"}); v.Body[0]["title"] != nil || v.Title != "Audio" {
		t.Errorf("untitled audio: %+v", v)
	}
	if v = mediaView(t, e, "audio", map[string]any{"url": ""}); v.Error == "" {
		t.Errorf("empty url: %+v", v)
	}
	// localized runtime text
	res, _ := e.m.Test(context.Background(), "media", TestRequest{Hook: "widget", Target: "audio", Lang: "zh", Config: map[string]any{"url": ""}})
	if v := res.Result.(WidgetView); !strings.Contains(v.Error, "地址") {
		t.Errorf("zh error: %q", v.Error)
	}
}

const snippetPage = `<!doctype html><html><head><title>News</title><script>window.x=1</script></head><body>
<table>
 <tr class="athing"><td><span class="titleline"><a href="item?id=1">First <b>story</b></a></span></td></tr>
 <tr class="athing"><td><span class="titleline"><a href="/item?id=2" onclick="evil()">Second &amp; more</a></span></td></tr>
 <tr class="athing"><td><span class="titleline"><a href="https://other.example/3">Third</a></span></td></tr>
 <tr class="athing"><td><span class="titleline"><a href="javascript:alert(1)">Fourth</a></span></td></tr>
</table>
<ul><li class="x">one</li><li class="x">two</li></ul>
<p class="note">Plain <i>note</i> with <a href="../up">link</a></p>
</body></html>`

func TestCatalogMediaSnippet(t *testing.T) {
	var ua atomic.Value
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		ua.Store(r.Header.Get("User-Agent"))
		switch r.URL.Path {
		case "/news/", "/news":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, snippetPage)
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"a":1}`)
		case "/boom":
			http.Error(w, "nope", 500)
		case "/redirect":
			http.Redirect(w, r, "/news/", http.StatusFound)
		}
	}))
	defer srv.Close()
	e := mediaEnv(t)
	url := srv.URL + "/news/"

	// every match is an html block, links absolutized against the page URL, handlers and javascript: gone
	v := mediaView(t, e, "snippet", map[string]any{"url": url, "selector": ".titleline > a", "max": 3})
	if v.Error != "" || len(v.Body) != 3 {
		t.Fatalf("snippet: %+v", v)
	}
	want := []string{
		`<a href="` + srv.URL + `/news/item?id=1">First <b>story</b></a>`,
		`<a href="` + srv.URL + `/item?id=2">Second &amp; more</a>`,
		`<a href="https://other.example/3">Third</a>`,
	}
	for i, w := range want {
		if v.Body[i]["type"] != "html" || v.Body[i]["html"] != w {
			t.Errorf("block %d: %v, want %s", i, v.Body[i], w)
		}
	}
	if ua, _ := ua.Load().(string); !strings.HasPrefix(ua, "Mozilla/5.0") {
		t.Errorf("User-Agent: %q", ua)
	}
	if v.Title == "" || v.Link != url {
		t.Errorf("title/link: %q %q", v.Title, v.Link)
	}
	// default max is 5 (all 4 here); a custom title wins
	v = mediaView(t, e, "snippet", map[string]any{"url": url, "selector": ".titleline > a", "title": "My news"})
	if len(v.Body) != 4 || v.Title != "My news" || v.Body[3]["html"] != "Fourth" {
		t.Errorf("default max: %d blocks %+v", len(v.Body), v.Title)
	}
	// rows and items are merged into one block with their container, so the parser keeps them
	v = mediaView(t, e, "snippet", map[string]any{"url": url, "selector": "tr.athing", "max": 2})
	if len(v.Body) != 1 || !strings.HasPrefix(v.Body[0]["html"].(string), "<table><tbody><tr><td>") || strings.Count(v.Body[0]["html"].(string), "<tr>") != 2 {
		t.Errorf("rows: %+v", v.Body)
	}
	v = mediaView(t, e, "snippet", map[string]any{"url": url, "selector": "li.x"})
	if len(v.Body) != 1 || v.Body[0]["html"] != "<ul><li>one</li><li>two</li></ul>" {
		t.Errorf("items: %+v", v.Body)
	}
	// relative links resolve against the final URL after a redirect
	v = mediaView(t, e, "snippet", map[string]any{"url": srv.URL + "/redirect", "selector": "p.note"})
	if len(v.Body) != 1 || !strings.Contains(v.Body[0]["html"].(string), `href="`+srv.URL+`/up"`) {
		t.Errorf("redirect base: %+v", v.Body)
	}

	// errors are localized messages, not exceptions
	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"no match shows the selector", map[string]any{"url": url, "selector": "div.nothing"}, "div.nothing"},
		{"invalid selector", map[string]any{"url": url, "selector": "a[["}, "Invalid CSS selector"},
		{"non-html", map[string]any{"url": srv.URL + "/json", "selector": "a"}, "application/json"},
		{"http error", map[string]any{"url": srv.URL + "/boom", "selector": "a"}, "HTTP 500"},
		{"fetch failure", map[string]any{"url": "http://127.0.0.1:1/", "selector": "a"}, "Could not load the page"},
		{"no url", map[string]any{"url": "", "selector": "a"}, "URL"},
		{"no selector", map[string]any{"url": url, "selector": " "}, "selector"},
	}
	for _, c := range cases {
		v := mediaView(t, e, "snippet", c.cfg)
		if len(v.Body) != 0 || !strings.Contains(v.Error, c.want) {
			t.Errorf("%s: %+v (want error containing %q)", c.name, v, c.want)
		}
	}
	res, _ := e.m.Test(context.Background(), "media", TestRequest{Hook: "widget", Target: "snippet", Lang: "zh", Config: map[string]any{"url": url, "selector": "div.nothing"}})
	if v := res.Result.(WidgetView); !strings.Contains(v.Error, "div.nothing") || !strings.Contains(v.Error, "没有匹配") {
		t.Errorf("zh no-match: %q", v.Error)
	}
}
