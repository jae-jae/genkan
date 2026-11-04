package plugin

import (
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits for plugin output (docs/PLUGINS.md §3).
const (
	maxStr         = 500
	maxURLLen      = 2048
	maxDataURL     = 256 << 10
	maxBlocks      = 50
	maxStats       = 6
	maxListItems   = 20
	maxKVItems     = 20
	maxChartPoints = 60
	maxLinePoints  = 500
	maxRowDepth    = 3
	maxSearchItems = 8
	maxParamsJSON  = 2048
)

// WidgetView is the sanitized declarative UI of a widget.
type WidgetView struct {
	Title string           `json:"title,omitempty"`
	Icon  string           `json:"icon,omitempty"`
	Link  string           `json:"link,omitempty"`
	Body  []map[string]any `json:"body"`
	Error string           `json:"error,omitempty"`
}

// SearchResult is one sanitized search hit.
type SearchResult struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	URL      string `json:"url,omitempty"`
	Icon     string `json:"icon,omitempty"`
	Copy     string `json:"copy,omitempty"`
}

// ActionResult is the sanitized result of an action hook.
type ActionResult struct {
	Toast   string `json:"toast,omitempty"`
	Refresh bool   `json:"refresh,omitempty"`
}

var (
	dataImageRe = regexp.MustCompile(`^data:image/(png|jpe?g|gif|webp|avif|svg\+xml);base64,[A-Za-z0-9+/=]+$`)
	iconNameRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	actionRe    = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,40}$`)
)

// cleanStr trims, strips control characters (newlines and tabs stay) and truncates.
func cleanStr(v any, max int) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s, true
}

func cleanLine(v any, max int) string {
	s, ok := cleanStr(v, max)
	if !ok {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

func cleanHTTPURL(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	s = strings.TrimSpace(s)
	if len(s) > maxURLLen || !httpURL(s) || strings.ContainsAny(s, "\r\n\t ") {
		return ""
	}
	if _, err := url.Parse(s); err != nil {
		return ""
	}
	return s
}

// cleanImageURL accepts http(s) URLs and base64 data:image URLs.
func cleanImageURL(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "data:") {
		if len(s) <= maxDataURL && dataImageRe.MatchString(s) {
			return s
		}
		return ""
	}
	return cleanHTTPURL(s)
}

func cleanIconName(v any) string {
	s, _ := v.(string)
	s = strings.TrimSpace(s)
	if iconNameRe.MatchString(s) {
		return s
	}
	return ""
}

func setStr(dst map[string]any, key string, v any, max int) {
	if s := cleanLine(v, max); s != "" {
		dst[key] = s
	}
}

func finite(v any) (float64, bool) {
	f, ok := toFloat(v)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

func oneOf(v any, allowed ...string) string {
	s, _ := v.(string)
	for _, a := range allowed {
		if s == a {
			return s
		}
	}
	return ""
}

// SanitizeView normalizes the value a widget hook returned: unknown block
// types and fields are dropped, strings truncated, URLs filtered, counts limited.
func SanitizeView(raw any) WidgetView {
	view := WidgetView{Body: []map[string]any{}}
	o, ok := raw.(map[string]any)
	if !ok {
		view.Error = "widget() must return an object"
		return view
	}
	view.Title = cleanLine(o["title"], maxStr)
	view.Icon = cleanIconName(o["icon"])
	view.Link = cleanHTTPURL(o["link"])
	view.Error = cleanLine(o["error"], maxStr)
	budget := maxBlocks
	if list, ok := o["body"].([]any); ok {
		view.Body = sanitizeBlocks(list, &budget, 0)
	}
	return view
}

func sanitizeBlocks(list []any, budget *int, depth int) []map[string]any {
	out := []map[string]any{}
	for _, it := range list {
		if *budget <= 0 {
			break
		}
		b, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if blk := sanitizeBlock(b, budget, depth); blk != nil {
			out = append(out, blk)
		}
	}
	return out
}

// sanitizeBlock returns nil for an invalid block. It spends one unit of the
// budget for the block itself (nested blocks spend theirs from the same budget).
func sanitizeBlock(b map[string]any, budget *int, depth int) map[string]any {
	typ, _ := b["type"].(string)
	out := map[string]any{"type": typ}
	switch typ {
	case "text":
		s, ok := cleanStr(b["text"], maxStr)
		if !ok {
			return nil
		}
		out["text"] = s
		if v := oneOf(b["size"], "s", "m", "l", "xl"); v != "" {
			out["size"] = v
		}
		if m, ok := b["muted"].(bool); ok && m {
			out["muted"] = true
		}
		if v := oneOf(b["align"], "left", "center", "right"); v != "" {
			out["align"] = v
		}
	case "stat":
		st := sanitizeStat(b)
		if st == nil {
			return nil
		}
		out = st
	case "stats":
		items, _ := b["items"].([]any)
		list := []map[string]any{}
		for _, it := range items {
			if len(list) >= maxStats {
				break
			}
			if m, ok := it.(map[string]any); ok {
				if st := sanitizeStat(m); st != nil {
					list = append(list, st)
				}
			}
		}
		if len(list) == 0 {
			return nil
		}
		out["items"] = list
	case "list":
		items, _ := b["items"].([]any)
		list := []map[string]any{}
		for _, it := range items {
			if len(list) >= maxListItems {
				break
			}
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			title := cleanLine(m["title"], maxStr)
			if title == "" {
				continue
			}
			e := map[string]any{"title": title}
			setStr(e, "subtitle", m["subtitle"], maxStr)
			setStr(e, "right", m["right"], maxStr)
			setStr(e, "badge", m["badge"], maxStr)
			if u := cleanHTTPURL(m["url"]); u != "" {
				e["url"] = u
			}
			if ic := cleanIconName(m["icon"]); ic != "" {
				e["icon"] = ic
			}
			if im := cleanImageURL(m["image"]); im != "" {
				e["image"] = im
			}
			list = append(list, e)
		}
		out["items"] = list
	case "kv":
		items, _ := b["items"].([]any)
		list := []map[string]any{}
		for _, it := range items {
			if len(list) >= maxKVItems {
				break
			}
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			k := cleanLine(m["k"], maxStr)
			if k == "" {
				continue
			}
			list = append(list, map[string]any{"k": k, "v": cleanLine(scalarText(m["v"]), maxStr)})
		}
		out["items"] = list
	case "progress":
		f, ok := finite(b["value"])
		if !ok {
			return nil
		}
		out["value"] = math.Min(1, math.Max(0, f))
		setStr(out, "label", b["label"], maxStr)
		setStr(out, "text", b["text"], maxStr)
	case "image": // single image or slideshow (sanitize_media.go)
		if out = sanitizeImage(b); out == nil {
			return nil
		}
	case "iframe":
		if out = sanitizeIframe(b); out == nil {
			return nil
		}
	case "html":
		if out = sanitizeHTMLBlock(b); out == nil {
			return nil
		}
	case "media":
		if out = sanitizeMedia(b); out == nil {
			return nil
		}
	case "chart":
		if b["kind"] == "line" {
			pts := sanitizePoints(b["points"])
			if len(pts) == 0 {
				return nil
			}
			out["kind"], out["points"] = "line", pts
			if v := oneOf(b["format"], "int", "compact", "percent", "decimal"); v != "" {
				out["format"] = v
			}
			if v := oneOf(b["color"], "accent", "green", "red", "blue", "orange"); v != "" {
				out["color"] = v
			}
			break
		}
		kind := oneOf(b["kind"], "spark", "bar")
		vals, _ := b["values"].([]any)
		nums := []float64{}
		for _, v := range vals {
			if len(nums) >= maxChartPoints {
				break
			}
			if f, ok := finite(v); ok {
				nums = append(nums, f)
			}
		}
		if kind == "" || len(nums) == 0 {
			return nil
		}
		out["kind"], out["values"] = kind, nums
		if ls, ok := b["labels"].([]any); ok {
			labels := []string{}
			for _, l := range ls {
				if len(labels) >= maxChartPoints {
					break
				}
				labels = append(labels, cleanLine(scalarText(l), 100))
			}
			out["labels"] = labels
		}
	case "row":
		if depth >= maxRowDepth {
			return nil
		}
		ch, _ := b["children"].([]any)
		children := sanitizeBlocks(ch, budget, depth+1)
		if len(children) == 0 {
			return nil
		}
		out["children"] = children
	case "button":
		label := cleanLine(b["label"], maxStr)
		action, _ := b["action"].(string)
		if label == "" || !actionRe.MatchString(action) {
			return nil
		}
		out["label"], out["action"] = label, action
		if ic := cleanIconName(b["icon"]); ic != "" {
			out["icon"] = ic
		}
		if p, ok := b["params"]; ok && p != nil {
			if jsonSize(p) <= maxParamsJSON {
				out["params"] = p
			}
		}
	case "divider":
	default:
		return nil
	}
	*budget--
	return out
}

func sanitizeStat(m map[string]any) map[string]any {
	label := cleanLine(m["label"], maxStr)
	if label == "" {
		return nil
	}
	out := map[string]any{"type": "stat", "label": label}
	switch v := m["value"].(type) {
	case string:
		out["value"] = cleanLine(v, maxStr)
	default:
		f, ok := finite(v)
		if !ok {
			return nil
		}
		out["value"] = f
	}
	setStr(out, "unit", m["unit"], 40)
	if ic := cleanIconName(m["icon"]); ic != "" {
		out["icon"] = ic
	}
	if tr := oneOf(m["trend"], "up", "down", "flat"); tr != "" {
		out["trend"] = tr
	}
	return out
}

// scalarText renders a string or number as text ("" for anything else).
func scalarText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	if f, ok := finite(v); ok {
		return formatNum(f)
	}
	return ""
}

func formatNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func jsonSize(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return maxParamsJSON + 1
	}
	return len(b)
}

// SanitizeResults normalizes the value a search hook returned.
func SanitizeResults(raw any) []SearchResult {
	out := []SearchResult{}
	list, ok := raw.([]any)
	if !ok {
		return out
	}
	for _, it := range list {
		if len(out) >= maxSearchItems {
			break
		}
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		r := SearchResult{Title: cleanLine(m["title"], 200)}
		if r.Title == "" {
			continue
		}
		r.Subtitle = cleanLine(m["subtitle"], 300)
		r.URL = cleanHTTPURL(m["url"])
		r.Icon = cleanIconName(m["icon"])
		if c, ok := cleanStr(m["copy"], 2000); ok {
			r.Copy = c
		}
		out = append(out, r)
	}
	return out
}

// SanitizeAction normalizes the value an action hook returned.
func SanitizeAction(raw any) ActionResult {
	var r ActionResult
	if m, ok := raw.(map[string]any); ok {
		r.Toast = cleanLine(m["toast"], 200)
		r.Refresh, _ = m["refresh"].(bool)
	}
	return r
}

// WallpaperResult is a sanitized wallpaper hook result.
type WallpaperResult struct {
	URL    string `json:"url"`
	Title  string `json:"title,omitempty"`
	Credit string `json:"credit,omitempty"`
	Link   string `json:"link,omitempty"`
}

// SanitizeWallpaper normalizes the result (an object, or an array from which
// one entry is picked by pick(n)).
func SanitizeWallpaper(raw any, pick func(n int) int) (WallpaperResult, bool) {
	if list, ok := raw.([]any); ok {
		valid := []WallpaperResult{}
		for _, it := range list {
			if r, ok := SanitizeWallpaper(it, pick); ok {
				valid = append(valid, r)
			}
		}
		if len(valid) == 0 {
			return WallpaperResult{}, false
		}
		return valid[pick(len(valid))], true
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return WallpaperResult{}, false
	}
	r := WallpaperResult{URL: cleanImageURL(m["url"])}
	if r.URL == "" {
		return WallpaperResult{}, false
	}
	r.Title = cleanLine(m["title"], 200)
	r.Credit = cleanLine(m["credit"], 200)
	r.Link = cleanHTTPURL(m["link"])
	return r, true
}

// sanitizePoints cleans the points of a line chart: [[timestamp ms, value], ...].
// Invalid pairs are dropped, at most maxLinePoints are kept, the result is sorted by time.
func sanitizePoints(v any) [][2]float64 {
	list, _ := v.([]any)
	out := make([][2]float64, 0, min(len(list), maxLinePoints))
	for _, it := range list {
		if len(out) >= maxLinePoints {
			break
		}
		p, ok := it.([]any)
		if !ok || len(p) < 2 {
			continue
		}
		ts, ok1 := finite(p[0])
		val, ok2 := finite(p[1])
		if !ok1 || !ok2 || ts <= 0 {
			continue
		}
		out = append(out, [2]float64{math.Round(ts), val})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
