package plugin

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Interactive blocks and fields (docs/PLUGINS.md §15.2): the `input` and
// `markdown` blocks and the id / check / edit / actions / sortAction / empty
// fields of `list`.

// cleanParams validates action params declared by a plugin. nil is fine (no
// params). Params must be a JSON object (the browser merges its own keys such
// as value / checked / ids into it) of at most maxParamsJSON bytes.
func cleanParams(v any) (map[string]any, bool) {
	if v == nil {
		return nil, true
	}
	m, ok := v.(map[string]any)
	if !ok || jsonSize(m) > maxParamsJSON {
		return nil, false
	}
	return m, true
}

// cleanActionRef reads { action, params? } from m. ok is false when the action
// name is invalid or the params are not acceptable.
func cleanActionRef(m map[string]any) (action string, params map[string]any, ok bool) {
	action, _ = m["action"].(string)
	if !actionRe.MatchString(action) {
		return "", nil, false
	}
	params, ok = cleanParams(m["params"])
	return action, params, ok
}

// cleanMultiline strips control characters except \n and \t (CRLF becomes LF),
// keeps all other whitespace as is and truncates to max characters.
func cleanMultiline(s string, max int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

func sanitizeInput(b map[string]any) map[string]any {
	action, _ := b["action"].(string)
	if !actionRe.MatchString(action) {
		return nil
	}
	out := map[string]any{"type": "input", "action": action}
	if p, ok := cleanParams(b["params"]); ok && p != nil { // like button: unusable params are dropped, the block stays
		out["params"] = p
	}
	setStr(out, "placeholder", b["placeholder"], maxEmpty)
	setStr(out, "submit", b["submit"], maxEmpty)
	if ic := cleanIconName(b["icon"]); ic != "" {
		out["icon"] = ic
	}
	maxLen := defaultInputLen
	if f, ok := finite(b["maxLength"]); ok {
		maxLen = int(max(1, min(maxNote, f)))
	}
	out["maxLength"] = maxLen
	if v := cleanLine(b["value"], maxLen); v != "" {
		out["value"] = v
	}
	return out
}

func sanitizeMarkdown(b map[string]any) map[string]any {
	text, ok := b["text"].(string)
	if !ok {
		return nil
	}
	out := map[string]any{"type": "markdown", "text": cleanMultiline(text, maxNote)}
	if a, _ := b["editAction"].(string); actionRe.MatchString(a) {
		out["editAction"] = a
		if p, ok := cleanParams(b["params"]); ok && p != nil {
			out["params"] = p
		}
	}
	setStr(out, "placeholder", b["placeholder"], maxEmpty)
	return out
}

func sanitizeList(b map[string]any) map[string]any {
	out := map[string]any{"type": "list"}
	items, _ := b["items"].([]any)
	list := []map[string]any{}
	seen := map[string]bool{}
	interactive, allIDs := false, true
	for _, it := range items {
		if len(list) >= maxInteractiveItems {
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
		if id, _ := m["id"].(string); itemIDRe.MatchString(id) && !seen[id] {
			seen[id] = true
			e["id"] = id
		} else {
			allIDs = false
		}
		if c, ok := m["check"].(map[string]any); ok {
			if action, params, ok := cleanActionRef(c); ok {
				checked, _ := c["checked"].(bool)
				chk := map[string]any{"checked": checked, "action": action}
				if params != nil {
					chk["params"] = params
				}
				e["check"] = chk
				interactive = true
			}
		}
		if ed, ok := m["edit"].(map[string]any); ok {
			if action, params, ok := cleanActionRef(ed); ok {
				edit := map[string]any{"action": action}
				if params != nil {
					edit["params"] = params
				}
				e["edit"] = edit
				interactive = true
			}
		}
		if acts, ok := m["actions"].([]any); ok {
			var buttons []map[string]any
			for _, a := range acts {
				if len(buttons) >= maxItemActions {
					break
				}
				am, ok := a.(map[string]any)
				if !ok {
					continue
				}
				action, params, ok := cleanActionRef(am)
				icon, label := cleanIconName(am["icon"]), cleanLine(am["label"], maxStr)
				if !ok || icon == "" || label == "" {
					continue
				}
				btn := map[string]any{"icon": icon, "label": label, "action": action}
				if params != nil {
					btn["params"] = params
				}
				buttons = append(buttons, btn)
			}
			if len(buttons) > 0 {
				e["actions"] = buttons
				interactive = true
			}
		}
		list = append(list, e)
	}
	// sortAction needs a unique id on every item (the browser sends the new order of all ids)
	if sa, _ := b["sortAction"].(string); actionRe.MatchString(sa) && allIDs {
		out["sortAction"] = sa
		interactive = true
	}
	if !interactive && len(list) > maxListItems {
		list = list[:maxListItems]
	}
	out["items"] = list
	setStr(out, "empty", b["empty"], maxEmpty)
	return out
}
