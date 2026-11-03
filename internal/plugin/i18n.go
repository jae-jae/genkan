package plugin

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Plugin localization (docs/PLUGINS.md §12).
//
//   - Manifest strings are LocalizedStrings: a plain string or {tag: text}.
//   - Runtime strings come from the plugin's exported `messages` table
//     through ctx.t(key, params).
//   - The global t(key), usable only while the module is evaluated, lets the
//     manifest point at the same table; markers are expanded to
//     LocalizedStrings before the manifest is validated.

// Limits.
const (
	maxLangTags     = 32        // languages in a LocalizedString or in `messages`
	maxMsgKeys      = 500       // keys per language in `messages`
	maxMsgKeyLen    = 64        // characters of a message key
	maxMsgValueLen  = 2000      // characters of a message text
	maxMessagesJSON = 128 << 10 // serialized size of `messages`
	maxTParams      = 16 << 10  // serialized size of the params given to ctx.t
	markerKey       = "$t"      // property of the object that t(key) returns in a manifest
)

var langTagRe = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`)

// ValidLangTag reports whether s looks like a BCP-47 language tag
// ("en", "zh-CN", "zh-Hant-TW", "es-419").
func ValidLangTag(s string) bool { return len(s) <= 35 && langTagRe.MatchString(s) }

// NormLang maps an Accept-Language style value (or a plain tag) to its
// lower-case base language, e.g. "zh-CN,zh;q=0.9" -> "zh", "fr-CA" -> "fr".
// Anything unusable gives "en".
func NormLang(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, ",;"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	base, _, _ := strings.Cut(strings.ReplaceAll(s, "_", "-"), "-")
	if len(base) < 2 || len(base) > 3 {
		return "en"
	}
	for _, r := range base {
		if r < 'a' || r > 'z' {
			return "en"
		}
	}
	return base
}

// defaultRegion gives the BCP-47 tag Intl should use for a bare language.
var defaultRegion = map[string]string{
	"en": "en-US", "zh": "zh-CN", "ja": "ja-JP", "ko": "ko-KR", "de": "de-DE", "fr": "fr-FR",
	"es": "es-ES", "it": "it-IT", "pt": "pt-BR", "ru": "ru-RU",
}

// Locale returns the BCP-47 tag for ctx.locale: a language with a region is
// kept, a bare one gets its usual region (zh -> zh-CN, en -> en-US) and an
// unknown one stays as is (Intl picks a default for it).
func Locale(lang string) string {
	lang = strings.ReplaceAll(strings.TrimSpace(lang), "_", "-")
	if lang == "" || !ValidLangTag(lang) {
		return "en-US"
	}
	if strings.Contains(lang, "-") {
		return lang
	}
	if r, ok := defaultRegion[strings.ToLower(lang)]; ok {
		return r
	}
	return strings.ToLower(lang)
}

// langChain lists the tags to try for lang, most specific first: the tag
// itself, each shorter prefix ("zh-Hant-TW" -> "zh-hant" -> "zh") and "en".
// Tags are lower case.
func langChain(lang string) []string {
	lang = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(lang), "_", "-"))
	var out []string
	for lang != "" {
		out = append(out, lang)
		i := strings.LastIndexByte(lang, '-')
		if i < 0 {
			break
		}
		lang = lang[:i]
	}
	for _, t := range out {
		if t == "en" {
			return out
		}
	}
	return append(out, "en")
}

// pickTag chooses among tags (sorted, as written by the plugin) the best one
// for lang: exact, base language, "en", else the first tag.
func pickTag(lang string, tags []string) (string, bool) {
	if len(tags) == 0 {
		return "", false
	}
	byLower := make(map[string]string, len(tags))
	for _, t := range tags {
		byLower[strings.ToLower(t)] = t
	}
	for _, c := range langChain(lang) {
		if t, ok := byLower[c]; ok {
			return t, true
		}
	}
	return tags[0], true
}

// LocalizedString ------------------------------------------------------------------------

// LocalizedString is a user-visible manifest string: a plain string (Tr is
// nil) or one text per language. Its JSON form is the string or the object.
type LocalizedString struct {
	Text string
	Tr   map[string]string
}

// Plain makes a LocalizedString without translations.
func Plain(s string) LocalizedString { return LocalizedString{Text: s} }

func (l LocalizedString) IsZero() bool { return l.Text == "" && len(l.Tr) == 0 }

// Resolve returns the text for lang: exact language, base language, "en",
// then the first language alphabetically. A plain string resolves to itself.
func (l LocalizedString) Resolve(lang string) string {
	if len(l.Tr) == 0 {
		return l.Text
	}
	tags := make([]string, 0, len(l.Tr))
	for t := range l.Tr {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	t, _ := pickTag(lang, tags)
	return l.Tr[t]
}

func (l LocalizedString) MarshalJSON() ([]byte, error) {
	if len(l.Tr) == 0 {
		return json.Marshal(l.Text)
	}
	return json.Marshal(l.Tr)
}

func (l *LocalizedString) UnmarshalJSON(b []byte) error {
	*l = LocalizedString{}
	if string(b) == "null" {
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &l.Text)
	}
	return json.Unmarshal(b, &l.Tr)
}

// lstr reads an optional or required LocalizedString: a plain string or an
// object of language tag -> text. Every text must be non-empty and at most
// max characters long. A plain empty string means "absent" for optional fields.
func (o obj) lstr(key, path string, required bool, max int) (LocalizedString, error) {
	v, ok := o[key]
	if !ok || v == nil {
		if required {
			return LocalizedString{}, fmt.Errorf("%s.%s is required", path, key)
		}
		return LocalizedString{}, nil
	}
	m, isObj := v.(map[string]any)
	if !isObj {
		s, err := o.str(key, path, required, max)
		return Plain(s), err
	}
	fp := path + "." + key
	if len(m) == 0 {
		return LocalizedString{}, fmt.Errorf("%s needs at least one language, e.g. { en: \"…\" }", fp)
	}
	if len(m) > maxLangTags {
		return LocalizedString{}, fmt.Errorf("%s has too many languages (max %d)", fp, maxLangTags)
	}
	tags := make([]string, 0, len(m))
	for t := range m {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	out := make(map[string]string, len(m))
	seen := map[string]bool{}
	for _, tag := range tags {
		if !ValidLangTag(tag) {
			return LocalizedString{}, fmt.Errorf("%s: %q is not a language tag such as \"en\" or \"zh-CN\"", fp, tag)
		}
		if low := strings.ToLower(tag); seen[low] {
			return LocalizedString{}, fmt.Errorf("%s: language %q is given twice", fp, tag)
		} else {
			seen[low] = true
		}
		sub := obj{tag: m[tag]}
		s, err := sub.str(tag, fp, true, max)
		if err != nil {
			return LocalizedString{}, err
		}
		out[tag] = s
	}
	return LocalizedString{Tr: out}, nil
}

// Messages -------------------------------------------------------------------------------

// Messages is the validated `export const messages` table: language tag ->
// key -> text. A nil *Messages is an empty table.
type Messages struct {
	tags []string                     // as written, sorted
	tab  map[string]map[string]string // lower-case tag -> key -> text
}

func parseMessages(raw any) (*Messages, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("messages must be an object of language -> { key: text }, e.g. { en: { hello: \"Hello\" } }")
	}
	if len(m) > maxLangTags {
		return nil, fmt.Errorf("messages has too many languages (max %d)", maxLangTags)
	}
	ms := &Messages{tab: make(map[string]map[string]string, len(m))}
	for t := range m {
		ms.tags = append(ms.tags, t)
	}
	sort.Strings(ms.tags)
	for _, tag := range ms.tags {
		if !ValidLangTag(tag) {
			return nil, fmt.Errorf("messages: %q is not a language tag such as \"en\" or \"zh-CN\"", tag)
		}
		low := strings.ToLower(tag)
		if _, dup := ms.tab[low]; dup {
			return nil, fmt.Errorf("messages: language %q is given twice", tag)
		}
		t, ok := m[tag].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("messages.%s must be an object of key -> text", tag)
		}
		if len(t) > maxMsgKeys {
			return nil, fmt.Errorf("messages.%s has too many keys (max %d)", tag, maxMsgKeys)
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		tab := make(map[string]string, len(t))
		for _, k := range keys {
			if k == "" || utf8.RuneCountInString(k) > maxMsgKeyLen || hasCtl(k) {
				return nil, fmt.Errorf("messages.%s: key %q must be 1-%d characters without control characters", tag, k, maxMsgKeyLen)
			}
			s, ok := t[k].(string)
			if !ok {
				return nil, fmt.Errorf("messages.%s.%s must be a string", tag, k)
			}
			if utf8.RuneCountInString(s) > maxMsgValueLen {
				return nil, fmt.Errorf("messages.%s.%s is too long (max %d characters)", tag, k, maxMsgValueLen)
			}
			for _, r := range s {
				if unicode.IsControl(r) && r != '\n' && r != '\t' {
					return nil, fmt.Errorf("messages.%s.%s contains control characters", tag, k)
				}
			}
			tab[k] = s
		}
		ms.tab[low] = tab
	}
	return ms, nil
}

// Lookup finds key for lang: exact language, base language, "en", the first
// table (alphabetically), per key. ok is false when no table has the key.
func (ms *Messages) Lookup(lang, key string) (string, bool) {
	if ms == nil || len(ms.tags) == 0 {
		return "", false
	}
	chain := append(langChain(lang), strings.ToLower(ms.tags[0]))
	for _, c := range chain {
		if s, ok := ms.tab[c][key]; ok {
			return s, true
		}
	}
	return "", false
}

// T is ctx.t: the text for key in lang with {name} placeholders replaced by
// params. A missing key gives the key itself; T never fails.
func (ms *Messages) T(lang, key string, params map[string]any) string {
	s, ok := ms.Lookup(lang, key)
	if !ok {
		return key
	}
	return interpolate(s, params)
}

// expand turns key into the LocalizedString object {tag: text} of every
// language that has it (empty when none does).
func (ms *Messages) expand(key string) map[string]any {
	out := map[string]any{}
	if ms == nil {
		return out
	}
	for _, tag := range ms.tags {
		if s, ok := ms.tab[strings.ToLower(tag)][key]; ok {
			out[tag] = s
		}
	}
	return out
}

// interpolate replaces {name} with params[name]. "{{" and "}}" give a literal
// brace; a placeholder without a param stays as written.
func interpolate(s string, params map[string]any) string {
	if !strings.ContainsAny(s, "{}") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '{' && i+1 < len(s) && s[i+1] == '{':
			b.WriteByte('{')
			i++
		case c == '}' && i+1 < len(s) && s[i+1] == '}':
			b.WriteByte('}')
			i++
		case c == '{':
			end := strings.IndexByte(s[i:], '}')
			if end > 0 {
				name := s[i+1 : i+end]
				if v, ok := params[name]; ok && validParamName(name) {
					b.WriteString(paramText(v))
					i += end
					continue
				}
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func validParamName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// paramText renders a param like JavaScript's String(v).
func paramText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case float64:
		switch {
		case x != x:
			return "NaN"
		case x >= 1e21 || x <= -1e21:
			return strconv.FormatFloat(x, 'g', -1, 64)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprint(v)
}

// Manifest markers ------------------------------------------------------------------------

// expandMarkers replaces every t(key) marker of the manifest value v with the
// LocalizedString object built from ms. path names the value in errors.
func expandMarkers(v any, path string, ms *Messages) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		if k, ok := x[markerKey]; ok && len(x) == 1 {
			key, isStr := k.(string)
			if !isStr {
				return nil, fmt.Errorf("%s: t() needs a string key", path)
			}
			out := ms.expand(key)
			if len(out) == 0 {
				if ms == nil {
					return nil, fmt.Errorf("%s: t(%q) needs `export const messages = { en: { %s: \"…\" } }`", path, key, key)
				}
				return nil, fmt.Errorf("%s: t(%q) is not defined in any language of `messages`", path, key)
			}
			return out, nil
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			n, err := expandMarkers(x[k], path+"."+k, ms)
			if err != nil {
				return nil, err
			}
			x[k] = n
		}
	case []any:
		for i := range x {
			n, err := expandMarkers(x[i], fmt.Sprintf("%s[%d]", path, i), ms)
			if err != nil {
				return nil, err
			}
			x[i] = n
		}
	}
	return v, nil
}
