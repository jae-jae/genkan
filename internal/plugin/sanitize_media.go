package plugin

import "math"

// Limits of the iframe, media, html and slideshow blocks (docs/PLUGINS.md §14).
const (
	iframeMinH, iframeMaxH, iframeDefH = 120, 1600, 360
	iframeMinZoom, iframeMaxZoom       = 0.25, 2.0
	iframeMinRefresh, maxRefresh       = 5, 86400
	cropMax                            = 20000
	maxSlides                          = 20
	slideMinInterval, slideMaxInterval = 3, 3600
	slideMinH, slideMaxH               = 60, 1000
)

func clampF(f, lo, hi float64) float64 { return math.Min(hi, math.Max(lo, f)) }

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// sanitizeIframe: { src, height?, ratio?, crop?{x,y,width,height}, zoom?, refresh? }.
// The same-origin check needs the page's origin and happens in the browser.
func sanitizeIframe(b map[string]any) map[string]any {
	src := cleanHTTPURL(b["src"])
	if src == "" {
		return nil
	}
	out := map[string]any{"type": "iframe", "src": src, "height": iframeDefH}
	if h, ok := finite(b["height"]); ok {
		out["height"] = int(clampF(math.Round(h), iframeMinH, iframeMaxH))
	}
	if c, ok := b["crop"].(map[string]any); ok {
		w, wok := finite(c["width"])
		h, hok := finite(c["height"])
		if wok && hok && w >= 1 && h >= 1 {
			x, _ := finite(c["x"])
			y, _ := finite(c["y"])
			out["crop"] = map[string]any{
				"x": int(clampF(math.Round(x), 0, cropMax)), "y": int(clampF(math.Round(y), 0, cropMax)),
				"width": int(clampF(math.Round(w), 1, cropMax)), "height": int(clampF(math.Round(h), 1, cropMax)),
			}
		}
	}
	if z, ok := finite(b["zoom"]); ok {
		out["zoom"] = round2(clampF(z, iframeMinZoom, iframeMaxZoom))
	}
	if r, ok := finite(b["ratio"]); ok && r > 0 {
		out["ratio"] = math.Round(clampF(r, 0.2, 5)*1000) / 1000 // width/height: the frame keeps this shape at any card width
	}
	if r, ok := finite(b["refresh"]); ok && r > 0 {
		out["refresh"] = int(clampF(math.Round(r), iframeMinRefresh, maxRefresh))
	}
	return out
}

// sanitizeHTMLBlock: { html, base? } -> { html } (cleaned; relative URLs resolved).
func sanitizeHTMLBlock(b map[string]any) map[string]any {
	s, _ := b["html"].(string)
	base, _ := b["base"].(string)
	h := SanitizeHTML(s, base)
	if h == "" {
		return nil
	}
	return map[string]any{"type": "html", "html": h}
}

// sanitizeMedia: { kind, src, poster?, title?, autoplay?, muted?, loop?, controls? }.
// Autoplay is only kept together with muted (browsers refuse sound-on autoplay).
func sanitizeMedia(b map[string]any) map[string]any {
	kind := oneOf(b["kind"], "video", "audio")
	src := cleanHTTPURL(b["src"])
	if kind == "" || src == "" {
		return nil
	}
	out := map[string]any{"type": "media", "kind": kind, "src": src}
	if p := cleanHTTPURL(b["poster"]); p != "" && kind == "video" {
		out["poster"] = p
	}
	setStr(out, "title", b["title"], 200)
	muted, _ := b["muted"].(bool)
	if muted {
		out["muted"] = true
	}
	if a, _ := b["autoplay"].(bool); a && muted {
		out["autoplay"] = true
	}
	if l, _ := b["loop"].(bool); l {
		out["loop"] = true
	}
	if c, ok := b["controls"].(bool); ok {
		out["controls"] = c
	}
	return out
}

// sanitizeImage keeps the single-image form { src, alt?, href?, ratio? } and
// adds the slideshow form { images:[{src,alt?,href?}], interval?, fit?, height? }.
func sanitizeImage(b map[string]any) map[string]any {
	out := map[string]any{"type": "image"}
	if list, ok := b["images"].([]any); ok {
		slides := []map[string]any{}
		for _, it := range list {
			if len(slides) >= maxSlides {
				break
			}
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			src := cleanImageURL(m["src"])
			if src == "" {
				continue
			}
			s := map[string]any{"src": src}
			setStr(s, "alt", m["alt"], maxStr)
			if h := cleanHTTPURL(m["href"]); h != "" {
				s["href"] = h
			}
			slides = append(slides, s)
		}
		if len(slides) == 0 {
			return nil
		}
		out["images"] = slides
		if iv, ok := finite(b["interval"]); ok {
			out["interval"] = int(clampF(math.Round(iv), slideMinInterval, slideMaxInterval))
		}
		imageFit(out, b)
		return out
	}
	src := cleanImageURL(b["src"])
	if src == "" {
		return nil
	}
	out["src"] = src
	setStr(out, "alt", b["alt"], maxStr)
	if h := cleanHTTPURL(b["href"]); h != "" {
		out["href"] = h
	}
	if r, ok := finite(b["ratio"]); ok && r > 0 {
		out["ratio"] = math.Min(r, 10)
	}
	imageFit(out, b)
	return out
}

// imageFit copies the optional fit ("cover"|"contain") and height (px).
func imageFit(out, b map[string]any) {
	if f := oneOf(b["fit"], "cover", "contain"); f != "" {
		out["fit"] = f
	}
	if h, ok := finite(b["height"]); ok {
		out["height"] = int(clampF(math.Round(h), slideMinH, slideMaxH))
	}
}
