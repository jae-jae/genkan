package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/jae-jae/genkan/internal/store"
	"github.com/jae-jae/genkan/plugins"
)

// mustMessages builds a Messages from a JSON literal.
func mustMessages(t *testing.T, js string) *Messages {
	t.Helper()
	var raw any
	if err := json.Unmarshal([]byte(js), &raw); err != nil {
		t.Fatal(err)
	}
	ms, err := parseMessages(raw)
	if err != nil {
		t.Fatalf("parseMessages(%s): %v", js, err)
	}
	return ms
}

func TestLangHelpers(t *testing.T) {
	for in, want := range map[string]string{
		"zh": "zh", "ZH-cn": "zh", "zh-CN,zh;q=0.9,en;q=0.8": "zh", "en-US": "en", "fr-CA,fr;q=0.9": "fr", "ja": "ja",
		"": "en", "*": "en", "x": "en", "english": "en", "12": "en", "zh_TW": "zh", " de ": "de",
	} {
		if got := NormLang(in); got != want {
			t.Errorf("NormLang(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"zh": "zh-CN", "en": "en-US", "ja": "ja-JP", "pt": "pt-BR", "zh-TW": "zh-TW", "sv": "sv", "": "en-US", "not a tag": "en-US",
	} {
		if got := Locale(in); got != want {
			t.Errorf("Locale(%q) = %q, want %q", in, got, want)
		}
	}
	if got := strings.Join(langChain("zh-Hant-TW"), ","); got != "zh-hant-tw,zh-hant,zh,en" {
		t.Errorf("chain %s", got)
	}
	if got := strings.Join(langChain("en-GB"), ","); got != "en-gb,en" {
		t.Errorf("chain %s", got)
	}
	for _, ok := range []string{"en", "zh-CN", "zh-Hant-TW", "es-419", "sr-Latn-RS", "fil", "EN"} {
		if !ValidLangTag(ok) {
			t.Errorf("%q should be a valid tag", ok)
		}
	}
	for _, bad := range []string{"", "e", "english", "en_US", "en-", "-en", "zh--CN", "zh CN", "en-US-x-y-z-w", "1a", "zh-中文", "en-toolongsubtagx"} {
		if ValidLangTag(bad) {
			t.Errorf("%q should be an invalid tag", bad)
		}
	}
}

func TestTranslateFallbackInterpolationMissing(t *testing.T) {
	ms := mustMessages(t, `{
		"en":    {"hello":"Hello", "next":"Next {n} days", "only_en":"Only English", "pair":"{a} and {b}", "esc":"{{literal}} {x}", "plain":"no vars"},
		"zh":    {"hello":"你好", "next":"未来 {n} 天"},
		"zh-TW": {"hello":"哈囉"},
		"de":    {"deonly":"Nur Deutsch"}
	}`)
	cases := []struct {
		lang, key string
		params    map[string]any
		want      string
	}{
		{"en", "hello", nil, "Hello"},
		{"zh", "hello", nil, "你好"},
		{"zh-TW", "hello", nil, "哈囉"},                         // exact language
		{"zh-tw", "hello", nil, "哈囉"},                         // tags are case-insensitive
		{"zh-HK", "hello", nil, "你好"},                         // base language
		{"zh-TW", "next", map[string]any{"n": 7.0}, "未来 7 天"}, // exact table lacks the key: base language
		{"fr", "hello", nil, "Hello"},                         // unknown language: en
		{"zh", "only_en", nil, "Only English"},                // key only in en
		{"fr", "deonly", nil, "Nur Deutsch"},                  // not in the chain, but in the first table? no: de is first alphabetically
		{"zh", "nope", nil, "nope"},                           // missing everywhere: the key
		{"", "hello", nil, "Hello"},
		{"en", "next", map[string]any{"n": 3.0}, "Next 3 days"},
		{"en", "next", map[string]any{"n": "x"}, "Next x days"},
		{"en", "next", map[string]any{"n": 1.5}, "Next 1.5 days"},
		{"en", "next", map[string]any{"n": true}, "Next true days"},
		{"en", "next", nil, "Next {n} days"}, // placeholder without a param stays
		{"en", "next", map[string]any{"other": 1.0}, "Next {n} days"},
		{"en", "pair", map[string]any{"a": 1.0, "b": "two"}, "1 and two"},
		{"en", "esc", map[string]any{"x": "X", "literal": "BAD"}, "{literal} X"},
		{"en", "plain", map[string]any{"a": 1.0}, "no vars"},
		{"en", "hello", map[string]any{"n": 1.0}, "Hello"},
		{"en", "missing {n}", map[string]any{"n": 1.0}, "missing {n}"}, // a missing key is returned as is
	}
	for _, c := range cases {
		if got := ms.T(c.lang, c.key, c.params); got != c.want {
			t.Errorf("T(%q, %q, %v) = %q, want %q", c.lang, c.key, c.params, got, c.want)
		}
	}

	// without en the first table (alphabetically) is the last resort
	ms2 := mustMessages(t, `{"zh":{"a":"甲"},"ja":{"a":"あ","b":"い"}}`)
	if got := ms2.T("fr", "a", nil); got != "あ" {
		t.Errorf("first table: %q", got)
	}
	if got := ms2.T("zh", "b", nil); got != "い" { // zh lacks b; en is absent; the first table (ja) has it
		t.Errorf("first table for a missing key: %q", got)
	}
	// no table at all
	var none *Messages
	if got := none.T("en", "k {x}", map[string]any{"x": 1.0}); got != "k {x}" {
		t.Errorf("nil table: %q", got)
	}
	if got := mustMessages(t, `{}`).T("en", "k", nil); got != "k" {
		t.Errorf("empty table: %q", got)
	}
}

func TestParamTextAndInterpolate(t *testing.T) {
	for v, want := range map[string]string{"7": "7", "1.5": "1.5", "-3": "-3", "1e21": "1e+21", "0.1": "0.1", "100000": "100000"} {
		var f float64
		fmt.Sscan(v, &f)
		if got := paramText(f); got != want {
			t.Errorf("paramText(%v) = %q, want %q", v, got, want)
		}
	}
	if got := paramText(nil); got != "null" {
		t.Error(got)
	}
	if got := paramText([]any{1.0, "a"}); got != `[1,"a"]` {
		t.Error(got)
	}
	// params are inserted once: a param value is never expanded again
	if got := interpolate("{a}", map[string]any{"a": "{b}", "b": "no"}); got != "{b}" {
		t.Error(got)
	}
	for in, want := range map[string]string{"{": "{", "}": "}", "a{": "a{", "{}": "{}", "{ n }": "{ n }", "{{{n}}}": "{1}", "{n}}": "1}", "{{n}": "{n}"} {
		if got := interpolate(in, map[string]any{"n": 1.0}); got != want {
			t.Errorf("interpolate(%q) = %q, want %q", in, got, want)
		}
	}
}

const tPlugin = `
export const messages = {
  en: { name: "Tee", desc: "A test plugin", w: "Widget {n}", hi: "Hi {who}", locale: "locale is" },
  zh: { name: "测试", desc: "测试插件", w: "小组件 {n}", hi: "你好 {who}" },
};
export const manifest = {
  id: "tee", name: t("name"), version: "1", description: t("desc"),
  widgets: [{ id: "w", name: t("w", { ignored: 1 }) }],
  search: { name: t("name"), prefix: "" },
  wallpapers: [{ id: "wp", name: "WP" }],
  schedules: [{ id: "s", every: 60 }],
};
export async function widget(ctx) {
  const { t } = ctx;
  return { title: t("w", { n: 3 }), body: [{ type: "text", text: JSON.stringify([ctx.lang, ctx.locale, t("hi", { who: "Ann" }), t("missing"), t(), t(5), t("hi", 7), t("hi", { who: null })]) }] };
}
export async function search(ctx) { return [{ title: ctx.t("hi", { who: ctx.query }), subtitle: ctx.lang + "/" + ctx.locale }] }
export async function wallpaper(ctx) { return { url: "data:image/png;base64,AAAA", title: ctx.t("name"), credit: ctx.locale } }
export async function action(ctx) { return { toast: ctx.t("hi", { who: ctx.action }) } }
export async function schedule(ctx) { genkan.log(ctx.lang, ctx.locale, ctx.t("hi", { who: "cron" })) }
`

func TestCtxTInEveryHook(t *testing.T) {
	e := newEnv(t)
	e.install(tPlugin)
	bg := context.Background()
	run := func(req TestRequest) TestResult {
		t.Helper()
		res, err := e.m.Test(bg, "tee", req)
		if err != nil || !res.OK {
			t.Fatalf("%+v: %+v %v", req, res, err)
		}
		return res
	}

	// widget, in zh and en
	w := run(TestRequest{Hook: "widget", Lang: "zh"}).Result.(WidgetView)
	if w.Title != "小组件 3" {
		t.Errorf("zh title %q", w.Title)
	}
	if got := w.Body[0]["text"]; got != `["zh","zh-CN","你好 Ann","missing","","5","你好 {who}","你好 null"]` {
		t.Errorf("zh body %v", got)
	}
	w = run(TestRequest{Hook: "widget", Lang: "en"}).Result.(WidgetView)
	if w.Title != "Widget 3" || w.Body[0]["text"] != `["en","en-US","Hi Ann","missing","","5","Hi {who}","Hi null"]` {
		t.Errorf("en: %q %v", w.Title, w.Body[0]["text"])
	}
	// a language without a table falls back to en; ctx.lang is the request language
	w = run(TestRequest{Hook: "widget", Lang: "fr-CA"}).Result.(WidgetView)
	if w.Title != "Widget 3" || !strings.HasPrefix(w.Body[0]["text"].(string), `["fr","fr-FR","Hi Ann"`) {
		t.Errorf("fr: %q %v", w.Title, w.Body[0]["text"])
	}
	// no language: en
	if w = run(TestRequest{Hook: "widget"}).Result.(WidgetView); w.Title != "Widget 3" {
		t.Errorf("default: %q", w.Title)
	}

	// search
	rs := run(TestRequest{Hook: "search", Query: "bob", Lang: "zh"}).Result.([]SearchResult)
	if len(rs) != 1 || rs[0].Title != "你好 bob" || rs[0].Subtitle != "zh/zh-CN" {
		t.Errorf("search %+v", rs)
	}
	// wallpaper
	wp := run(TestRequest{Hook: "wallpaper", Lang: "zh"}).Result
	if b, _ := json.Marshal(wp); !strings.Contains(string(b), `"title":"测试"`) || !strings.Contains(string(b), `zh-CN`) {
		t.Errorf("wallpaper %s", b)
	}
	// action
	ar := run(TestRequest{Hook: "action", Action: "go", Lang: "zh"}).Result.(ActionResult)
	if ar.Toast != "你好 go" {
		t.Errorf("action %+v", ar)
	}
	// schedule: always en
	sr := run(TestRequest{Hook: "schedule", Lang: "zh"})
	var logged string
	for _, l := range sr.Logs {
		logged += l.Msg
	}
	if !strings.Contains(logged, "en en-US Hi cron") {
		t.Errorf("schedule logs %+v", sr.Logs)
	}

	// the real endpoints use the request language too
	inf, _ := e.m.Info("tee", true)
	wo := addWidget(t, e, "tee", inf.Widgets[0].ID, nil)
	d, err := e.m.Data(bg, wo.ID, "zh", false)
	if err != nil || d.View.Title != "小组件 3" {
		t.Errorf("Data zh: %+v %v", d.View, err)
	}
	d, _ = e.m.Data(bg, wo.ID, "en", false)
	if d.View.Title != "Widget 3" {
		t.Errorf("Data en (cached per language): %+v", d.View)
	}
	ar2, err := e.m.Action(bg, wo.ID, ActionRequest{Action: "x"}, "zh")
	if err != nil || ar2.Toast != "你好 x" {
		t.Errorf("Action: %+v %v", ar2, err)
	}
	groups := e.m.Search(bg, "carl", "zh")
	if len(groups) != 1 || groups[0].Results[0].Title != "你好 carl" {
		t.Errorf("Search: %+v", groups)
	}
}

func TestManifestMarkers(t *testing.T) {
	e := newEnv(t)
	inf := e.install(tPlugin)
	// expanded to LocalizedString objects, only languages that have the key
	if inf.Name.Tr["en"] != "Tee" || inf.Name.Tr["zh"] != "测试" || inf.Name.Text != "" {
		t.Errorf("name %+v", inf.Name)
	}
	if inf.Description.Resolve("zh") != "测试插件" || inf.Search.Name.Resolve("en") != "Tee" {
		t.Errorf("%+v %+v", inf.Description, inf.Search)
	}
	if inf.Widgets[0].Name.Tr["zh"] != "小组件 {n}" { // manifest t() takes no params: the template is kept
		t.Errorf("widget name %+v", inf.Widgets[0].Name)
	}
	b, _ := json.Marshal(inf)
	if !strings.Contains(string(b), `"name":{"en":"Tee","zh":"测试"}`) || strings.Contains(string(b), `$t`) || strings.Contains(string(b), `"messages"`) {
		t.Errorf("wire format: %s", b)
	}

	// a key that exists in one language only
	one := e.install(plug(`{id:"one", name:t("a"), version:"1", description:t("b"), widgets:[{id:"w",name:"W"}]}`,
		`export const messages = { en: { a: "A", b: "B" }, zh: { a: "甲" } }; export async function widget(){ return {body:[]} }`))
	if len(one.Name.Tr) != 2 || len(one.Description.Tr) != 1 || one.Description.Tr["en"] != "B" {
		t.Errorf("partial: %+v %+v", one.Name, one.Description)
	}

	// all three forms in one manifest
	mixed := e.install(plug(`{id:"mixed", name:{en:"Mixed", "zh-CN":"混合"}, version:"1", description:"plain text",
	  config:[{key:"k", type:"select", label:t("lbl"), help:{en:"inline"}, options:[{value:"a", label:t("opt")},{value:"b", label:"Plain B"}]}],
	  widgets:[{id:"w", name:t("lbl")}]}`,
		`export const messages = { en: { lbl: "Label", opt: "Option A" }, zh: { lbl: "标签", opt: "选项 A" } }; export async function widget(){ return {body:[]} }`))
	f := mixed.ConfigSchema[0]
	if mixed.Name.Tr["zh-CN"] != "混合" || mixed.Description.Text != "plain text" || f.Label.Tr["zh"] != "标签" || f.Help.Tr["en"] != "inline" ||
		f.Options[0].Label.Tr["zh"] != "选项 A" || f.Options[1].Label.Text != "Plain B" || mixed.Widgets[0].Name.Tr["en"] != "Label" {
		t.Errorf("%+v", mixed)
	}

	// errors name the key and the field path
	for src, want := range map[string]string{
		`{id:"e1", name:t("zzz"), version:"1", widgets:[{id:"w",name:"W"}]}`:                                                                            `manifest.name: t("zzz") is not defined in any language`,
		`{id:"e2", name:"N", version:"1", widgets:[{id:"w",name:t("zzz")}]}`:                                                                            `manifest.widgets[0].name: t("zzz")`,
		`{id:"e3", name:"N", version:"1", config:[{key:"k",type:"string",label:"L",help:t("zzz")}], widgets:[{id:"w",name:"W"}]}`:                       `manifest.config[0].help: t("zzz")`,
		`{id:"e4", name:"N", version:"1", widgets:[{id:"w",name:"W",config:[{key:"k",type:"select",label:"L",options:[{value:"a",label:t("zzz")}]}]}]}`: `manifest.widgets[0].config[0].options[0].label: t("zzz")`,
	} {
		err := e.installErr(plug(src, `export const messages = { en: { present: "x" } }; export async function widget(){ return {body:[]} }`))
		if !strings.Contains(err.Error(), want) || store.ErrKind(err) != store.KindInvalid {
			t.Errorf("%s\n  error %q does not contain %q", src, err, want)
		}
	}
	// t() without any messages export
	err := e.installErr(plug(`{id:"nom", name:t("name"), version:"1", widgets:[{id:"w",name:"W"}]}`, `export async function widget(){ return {body:[]} }`))
	if !strings.Contains(err.Error(), `manifest.name: t("name")`) || !strings.Contains(err.Error(), "messages") {
		t.Errorf("no messages: %v", err)
	}
	// t() with a non-string key
	if err := e.installErr(plug(`{id:"nk", name:t(5), version:"1", widgets:[{id:"w",name:"W"}]}`, `export const messages = {en:{}}; export async function widget(){ return {body:[]} }`)); !strings.Contains(err.Error(), "key must be a string") {
		t.Errorf("non-string key: %v", err)
	}
	// an expanded text still goes through LocalizedString validation
	err = e.installErr(plug(`{id:"long", name:t("n"), version:"1", widgets:[{id:"w",name:"W"}]}`,
		`export const messages = { en: { n: "`+strings.Repeat("x", 41)+`" } }; export async function widget(){ return {body:[]} }`))
	if !strings.Contains(err.Error(), "manifest.name.en") || !strings.Contains(err.Error(), "too long") {
		t.Errorf("long: %v", err)
	}
}

func TestGlobalTOnlyWorksInManifest(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"gt", name:t("n"), version:"1", widgets:[{id:"w",name:"W"}], search:{name:"S"}}`, `
export const messages = { en: { n: "N", x: "X" } };
export async function widget(ctx) { return { body: [{ type: "text", text: t("x") }] } }
export async function search(ctx) { const { t } = ctx; return [{ title: t("x") }] }
`))
	res, err := e.m.Test(context.Background(), "gt", TestRequest{Hook: "widget"})
	if err != nil || res.OK || !strings.Contains(res.Error, "ctx.t") {
		t.Fatalf("the global t in a hook must fail and point to ctx.t: %+v %v", res, err)
	}
	// a local t (destructured from ctx) shadows the global one
	res, err = e.m.Test(context.Background(), "gt", TestRequest{Hook: "search", Query: "ab"})
	if err != nil || !res.OK || res.Result.([]SearchResult)[0].Title != "X" {
		t.Fatalf("shadowing: %+v %v", res, err)
	}
	// repeated calls on a pooled runtime keep the same behaviour
	for i := 0; i < 3; i++ {
		res, _ = e.m.Test(context.Background(), "gt", TestRequest{Hook: "widget"})
		if res.OK || !strings.Contains(res.Error, "ctx.t") {
			t.Fatalf("run %d: %+v", i, res)
		}
	}
}

func TestLocalizedStringValidation(t *testing.T) {
	e := newEnv(t)
	body := "export async function widget(ctx){ return {body:[]} }"
	mk := func(name string) string {
		return fmt.Sprintf(`{id:"ls", name:%s, version:"1", widgets:[{id:"w",name:"W"}]}`, name)
	}
	ok := []string{
		`"Plain"`,
		`{en:"Weather"}`,
		`{en:"Weather", zh:"天气"}`,
		`{"zh-CN":"天气", "zh-Hant-TW":"天氣", "es-419":"Clima", en:"Weather"}`,
		`{zh:"只有中文"}`, // en is recommended, not required
		`{EN:"Upper case tag"}`,
	}
	for _, n := range ok {
		if _, err := e.m.Install(context.Background(), InstallRequest{Source: plug(mk(n), body)}); err != nil {
			t.Errorf("%s should be valid: %v", n, err)
		}
	}
	bad := map[string]string{
		`{}`:                                     "at least one language",
		`{en:""}`:                                "required",
		`{en:"  "}`:                              "required",
		`{en:5}`:                                 "must be a string",
		`{en:null}`:                              "required",
		`{en:{x:"y"}}`:                           "must be a string",
		`{english:"Weather"}`:                    "not a language tag",
		`{"en_US":"Weather"}`:                    "not a language tag",
		`{"":"Weather"}`:                         "not a language tag",
		`{"zh CN":"x"}`:                          "not a language tag",
		`{en:"A", EN:"B"}`:                       "given twice",
		`{en:"` + strings.Repeat("x", 41) + `"}`: "too long",
		`{en:"ok", zh:"` + strings.Repeat("字", 41) + `"}`: "too long",
		`{en:"tab\there"}`: "control characters",
		`[1]`:              "must be a string",
	}
	for n, want := range bad {
		err := e.installErr(plug(mk(n), body))
		if !strings.Contains(err.Error(), want) || store.ErrKind(err) != store.KindInvalid {
			t.Errorf("%s: error %q does not contain %q", n, err, want)
		}
	}
	// too many languages
	var many []string
	for i := 0; i < maxLangTags+1; i++ {
		many = append(many, fmt.Sprintf(`"a%c":"x"`, 'a'+i%26)+fmt.Sprintf(`,"b%c%c":"y"`, 'a'+i/26, 'a'+i%26))
	}
	if err := e.installErr(plug(mk("{"+strings.Join(many, ",")+"}"), body)); !strings.Contains(err.Error(), "too many languages") {
		t.Errorf("many: %v", err)
	}

	// every user-visible field accepts the object form, with its own length limit
	full := `{id:"ls-all", name:{en:"N"}, version:"1", description:{en:"D", zh:"描述"},
	  config:[{key:"k", type:"select", label:{en:"L", zh:"标"}, help:{en:"H"}, options:[{value:"a", label:{en:"A", zh:"甲"}}]}],
	  widgets:[{id:"w", name:{en:"W", zh:"组"}, config:[{key:"c", type:"string", label:{en:"CL"}, help:{en:"CH"}}]}],
	  wallpapers:[{id:"p", name:{en:"P", zh:"墙纸"}}], search:{name:{en:"S", zh:"搜"}}, schedules:[{id:"s", name:{en:"Sync"}, every:60}]}`
	inf, err := e.m.Install(context.Background(), InstallRequest{Source: plug(full, "export async function widget(){return{body:[]}}\nexport async function wallpaper(){return{}}\nexport async function search(){return[]}\nexport async function schedule(){}")})
	if err != nil {
		t.Fatal(err)
	}
	if inf.Widgets[0].Config[0].Help.Tr["en"] != "CH" || inf.Wallpapers[0].Name.Tr["zh"] != "墙纸" || inf.Schedules[0].Name.Tr["en"] != "Sync" || inf.ConfigSchema[0].Options[0].Label.Tr["zh"] != "甲" {
		t.Errorf("%+v", inf)
	}
	limits := map[string]string{
		`description:{en:"` + strings.Repeat("x", 301) + `"}`:                                                            "manifest.description.en is too long (max 300",
		`config:[{key:"k",type:"string",label:{en:"` + strings.Repeat("x", 61) + `"}}]`:                                  "label.en is too long (max 60",
		`config:[{key:"k",type:"string",label:"L",help:{en:"` + strings.Repeat("x", 201) + `"}}]`:                        "help.en is too long (max 200",
		`config:[{key:"k",type:"select",label:"L",options:[{value:"a",label:{en:"` + strings.Repeat("x", 101) + `"}}]}]`: "options[0].label.en is too long (max 100",
	}
	for extra, want := range limits {
		src := fmt.Sprintf(`{id:"ls-lim", name:"N", version:"1", %s, widgets:[{id:"w",name:"W"}]}`, extra)
		if err := e.installErr(plug(src, body)); !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", extra, err)
		}
	}
	// plain strings keep their original rules (trimmed, required, 40 characters)
	if err := e.installErr(plug(mk(`""`), body)); !strings.Contains(err.Error(), "manifest.name is required") {
		t.Errorf("empty plain name: %v", err)
	}
}

func TestLocalizedStringResolveAndJSON(t *testing.T) {
	l := LocalizedString{Tr: map[string]string{"en": "Weather", "zh": "天气", "zh-TW": "天氣", "ja": "天気"}}
	for lang, want := range map[string]string{"en": "Weather", "zh": "天气", "zh-TW": "天氣", "zh-tw": "天氣", "zh-HK": "天气", "ja": "天気", "fr": "Weather", "": "Weather"} {
		if got := l.Resolve(lang); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", lang, got, want)
		}
	}
	// without en the first language alphabetically
	if got := (LocalizedString{Tr: map[string]string{"zh": "天气", "ja": "天気"}}).Resolve("fr"); got != "天気" {
		t.Errorf("first: %q", got)
	}
	if got := Plain("x").Resolve("zh"); got != "x" {
		t.Error(got)
	}
	if !(LocalizedString{}).IsZero() || Plain("x").IsZero() || (LocalizedString{Tr: map[string]string{"en": "x"}}).IsZero() {
		t.Error("IsZero")
	}
	// wire format: string or object; round trip
	type box struct {
		A LocalizedString `json:"a"`
		B LocalizedString `json:"b"`
		C LocalizedString `json:"c,omitzero"`
	}
	b, _ := json.Marshal(box{A: Plain("hi"), B: l})
	if string(b) != `{"a":"hi","b":{"en":"Weather","ja":"天気","zh":"天气","zh-TW":"天氣"}}` {
		t.Errorf("%s", b)
	}
	var back box
	if err := json.Unmarshal(b, &back); err != nil || back.A.Text != "hi" || back.B.Tr["ja"] != "天気" {
		t.Errorf("%+v %v", back, err)
	}
}

func TestMessagesValidation(t *testing.T) {
	e := newEnv(t)
	body := "export async function widget(ctx){ return {body:[]} }"
	man := `{id:"mv", name:"N", version:"1", widgets:[{id:"w",name:"W"}]}`
	good := []string{
		`{}`,
		`{ en: {} }`,
		`{ en: { a: "A" }, "zh-CN": { a: "甲", "with space and 中文": "ok", multi: "line1\nline2\tTabbed" } }`,
	}
	for _, m := range good {
		if _, err := e.m.Install(context.Background(), InstallRequest{Source: "export const messages = " + m + ";\n" + plug(man, body)}); err != nil {
			t.Errorf("messages %s should be valid: %v", m, err)
		}
	}
	// absent / undefined / null are fine
	for _, m := range []string{"undefined", "null"} {
		if _, err := e.m.Install(context.Background(), InstallRequest{Source: "export const messages = " + m + ";\n" + plug(man, body)}); err != nil {
			t.Errorf("messages = %s: %v", m, err)
		}
	}
	bad := map[string]string{
		`[]`:                        "messages must be an object",
		`"text"`:                    "messages must be an object",
		`5`:                         "messages must be an object",
		`{ en: "flat" }`:            "messages.en must be an object",
		`{ en: [] }`:                "messages.en must be an object",
		`{ english: { a: "A" } }`:   "not a language tag",
		`{ "en_US": { a: "A" } }`:   "not a language tag",
		`{ en: { a: 1 } }`:          "messages.en.a must be a string",
		`{ en: { a: null } }`:       "messages.en.a must be a string",
		`{ en: { a: { b: "c" } } }`: "messages.en.a must be a string",
		`{ en: { "": "x" } }`:       "key",
		`{ en: { a: "x\u0001y" } }`: "control characters",
		`{ en: { a: "` + strings.Repeat("x", maxMsgValueLen+1) + `" } }`: "messages.en.a is too long",
		`{ en: { "` + strings.Repeat("k", maxMsgKeyLen+1) + `": "x" } }`: "key",
		`{ en: { a: "A" }, EN: { a: "B" } }`:                             "given twice",
	}
	for m, want := range bad {
		_, err := e.m.Install(context.Background(), InstallRequest{Source: "export const messages = " + m + ";\n" + plug(man, body)})
		if err == nil || !strings.Contains(err.Error(), want) || store.ErrKind(err) != store.KindInvalid {
			t.Errorf("messages %.60s: error %v does not contain %q", m, err, want)
		}
	}
	// size caps
	var keys []string
	for i := 0; i <= maxMsgKeys; i++ {
		keys = append(keys, fmt.Sprintf("k%d:'x'", i))
	}
	if _, err := e.m.Install(context.Background(), InstallRequest{Source: "export const messages = { en: {" + strings.Join(keys, ",") + "} };\n" + plug(man, body)}); err == nil || !strings.Contains(err.Error(), "too many keys") {
		t.Errorf("too many keys: %v", err)
	}
	keys = keys[:maxMsgKeys]
	if _, err := e.m.Install(context.Background(), InstallRequest{Source: "export const messages = { en: {" + strings.Join(keys, ",") + "} };\n" + plug(man, body)}); err != nil {
		t.Errorf("exactly %d keys should be fine: %v", maxMsgKeys, err)
	}
	var langs []string
	for i := 0; i <= maxLangTags; i++ {
		langs = append(langs, fmt.Sprintf(`"x%c%c": {}`, 'a'+i/26, 'a'+i%26))
	}
	if _, err := e.m.Install(context.Background(), InstallRequest{Source: "export const messages = {" + strings.Join(langs, ",") + "};\n" + plug(man, body)}); err == nil || !strings.Contains(err.Error(), "too many languages") {
		t.Errorf("too many languages: %v", err)
	}
	// total size: 40 languages are not needed, 130 KiB of text in a few keys is
	var big []string
	for i := 0; i < 70; i++ {
		big = append(big, fmt.Sprintf("k%d:'%s'", i, strings.Repeat("y", 1900)))
	}
	if _, err := e.m.Install(context.Background(), InstallRequest{Source: "export const messages = { en: {" + strings.Join(big, ",") + "} };\n" + plug(man, body)}); err == nil || !strings.Contains(err.Error(), "messages is not usable") {
		t.Errorf("total size: %v", err)
	}
}

func TestSearchGroupNameResolution(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"gn-obj", name:"Obj", version:"1", search:{name:{en:"Calc", zh:"计算器", "zh-TW":"計算機"}, prefix:"o"}}`,
		`export async function search(ctx){ return [{title:"r " + ctx.query}] }`))
	e.install(plug(`{id:"gn-plain", name:"Plain", version:"1", search:{name:"Just a name", prefix:"p"}}`,
		`export async function search(ctx){ return [{title:"p " + ctx.query}] }`))
	e.install(plug(`{id:"gn-t", name:"T", version:"1", search:{name:t("g"), prefix:"t"}}`,
		`export const messages = { en: { g: "Group" }, zh: { g: "分组" } };
export async function search(ctx){ return [{title:"t " + ctx.query}] }`))
	name := func(q, lang string) string {
		t.Helper()
		g := e.m.Search(context.Background(), q, lang)
		if len(g) != 1 {
			t.Fatalf("%q/%s: %+v", q, lang, g)
		}
		return g[0].Name
	}
	// request languages are base languages; unknown ones fall back to en
	for lang, want := range map[string]string{"en": "Calc", "zh": "计算器", "fr": "Calc", "": "Calc"} {
		if got := name("o x", lang); got != want {
			t.Errorf("object name in %q = %q, want %q", lang, got, want)
		}
	}
	for _, lang := range []string{"en", "zh", "ja"} {
		if got := name("p x", lang); got != "Just a name" {
			t.Errorf("plain name in %q = %q", lang, got)
		}
	}
	if got := name("t x", "zh"); got != "分组" {
		t.Errorf("t() name in zh = %q", got)
	}
	if got := name("t x", "en"); got != "Group" {
		t.Errorf("t() name in en = %q", got)
	}
	// the group JSON carries a plain string, never an object
	g := e.m.Search(context.Background(), "o x", "zh")
	if b, _ := json.Marshal(g); !strings.Contains(string(b), `"name":"计算器"`) {
		t.Errorf("%s", b)
	}
}

func TestConfigErrorLabelsFollowLanguage(t *testing.T) {
	e := newEnv(t)
	e.install(plug(`{id:"cl", name:"CL", version:"1", widgets:[{id:"w", name:"W", config:[{key:"city", type:"string", label:{en:"City", zh:"城市"}, required:true}]}]}`,
		`export async function widget(ctx){ return {body:[]} }`))
	for lang, want := range map[string]string{"zh": "城市 is required", "en": "City is required", "": "City is required"} {
		_, err := e.m.AddWidget(WidgetAdd{Plugin: "cl", Widget: "w", Lang: lang})
		if err == nil || err.Error() != want {
			t.Errorf("lang %q: %v, want %q", lang, err, want)
		}
	}
}

func TestPreviewCatalogAndMarketShowObjects(t *testing.T) {
	src := `export const messages = { en: { n: "Tee", d: "Desc" }, zh: { n: "测试", d: "说明" } };
export const manifest = { id: "pv-t", name: t("n"), version: "1", description: t("d"), widgets: [{ id: "w", name: t("n") }] };
export async function widget(){ return {body:[]} }`
	wantName := `"name":{"en":"Tee","zh":"测试"}`

	e := newEnv(t)
	res, err := e.m.Preview(context.Background(), InstallRequest{Source: src})
	if err != nil || res.Error != "" {
		t.Fatalf("%v %+v", err, res)
	}
	if b, _ := json.Marshal(res); !strings.Contains(string(b), wantName) || !strings.Contains(string(b), `"description":{"en":"Desc","zh":"说明"}`) || strings.Contains(string(b), `$t`) {
		t.Errorf("preview: %s", b)
	}

	e.m.cfg.Catalog = mapFS(map[string]string{"pv-t.js": src})
	cat := e.m.Catalog()
	if b, _ := json.Marshal(cat); len(cat) != 1 || !strings.Contains(string(b), wantName) || !strings.Contains(string(b), `"description":{"en":"Desc","zh":"说明"}`) {
		t.Errorf("catalog: %s", b)
	}
	res, _ = e.m.Preview(context.Background(), InstallRequest{Catalog: "pv-t"})
	if res.Plugin == nil || res.Plugin.Name.Tr["zh"] != "测试" {
		t.Errorf("catalog preview: %+v", res)
	}

	inf := e.install(src)
	if b, _ := json.Marshal(e.m.Infos(false)); !strings.Contains(string(b), wantName) {
		t.Errorf("infos: %s", b)
	}
	if inf.Name.Tr["en"] != "Tee" {
		t.Errorf("%+v", inf.Name)
	}

	g := newFakeGH(t)
	g.mgr(e, "")
	g.trees["a/b"] = []map[string]any{blob("p.js", 100)}
	g.files["a/b/main/p.js"] = src
	scan, err := e.m.MarketRepo("a/b")
	if err != nil || len(scan.Plugins) != 1 || scan.Plugins[0].Plugin == nil {
		t.Fatalf("%v %+v", err, scan)
	}
	if b, _ := json.Marshal(scan); !strings.Contains(string(b), wantName) {
		t.Errorf("market scan: %s", b)
	}
}

// The built-in examples are the reference for the recommended style.
func TestCatalogUsesMessages(t *testing.T) {
	e := catalogEnv(t)
	ents, err := fs.ReadDir(plugins.Catalog(), ".")
	if err != nil {
		t.Fatal(err)
	}
	branch := regexp.MustCompile(`(ctx\.lang\s*[!=]==?)|(\bzh\s*\?)|(lang\s*===?\s*["']zh)`)
	keyRe := regexp.MustCompile(`\bt\(\s*"([^"]+)"`)
	for _, de := range ents {
		src, _ := fs.ReadFile(plugins.Catalog(), de.Name())
		if loc := branch.FindString(string(src)); loc != "" {
			t.Errorf("%s still branches on the language: %q", de.Name(), loc)
		}
		id := strings.TrimSuffix(de.Name(), ".js")
		inf, err := e.m.Install(context.Background(), InstallRequest{Catalog: id})
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		// name and description are localized in en and zh
		if inf.Name.Tr["en"] == "" || inf.Name.Tr["zh"] == "" || inf.Description.Tr["en"] == "" || inf.Description.Tr["zh"] == "" {
			t.Errorf("%s: name/description need en and zh: %+v %+v", id, inf.Name, inf.Description)
		}
		if strings.ContainsAny(inf.Description.Tr["en"], "一鿿") {
			t.Errorf("%s: the English description contains Chinese: %q", id, inf.Description.Tr["en"])
		}
		// en and zh have the same keys, and every key used in the source exists
		ms := e.m.entryRec(mustPlugin(t, e, id)).man.messages
		en, zh := ms.tab["en"], ms.tab["zh"]
		for k := range en {
			if _, ok := zh[k]; !ok {
				t.Errorf("%s: key %q has no zh text", id, k)
			}
		}
		for k := range zh {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: key %q has no en text", id, k)
			}
		}
		code := regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAllString(string(src), "")
		for _, m := range keyRe.FindAllStringSubmatch(code, -1) {
			if _, ok := en[m[1]]; !ok {
				t.Errorf("%s: t(%q) is not in messages", id, m[1])
			}
		}
	}
}

func mustPlugin(t *testing.T, e *tenv, id string) store.Plugin {
	t.Helper()
	rec, ok := e.st.GetPlugin(id)
	if !ok {
		t.Fatalf("plugin %s not installed", id)
	}
	return rec
}

// Output of the localized examples in both UI languages.
func TestCatalogOutputFollowsLanguage(t *testing.T) {
	e := catalogEnv(t)
	installCatalog(t, e, "countdown")
	installCatalog(t, e, "world-clock")
	bg := context.Background()
	cd, _ := e.m.Info("countdown", true)
	target := "2999-01-01"
	zh, err := e.m.Test(bg, "countdown", TestRequest{Hook: "widget", Lang: "zh", Config: map[string]any{"date": target}})
	if err != nil || !zh.OK {
		t.Fatalf("%+v %v", zh, err)
	}
	en, _ := e.m.Test(bg, "countdown", TestRequest{Hook: "widget", Lang: "en", Config: map[string]any{"date": target}})
	zv, ev := zh.Result.(WidgetView), en.Result.(WidgetView)
	if zv.Title != "新年" || ev.Title != "New Year" || zv.Body[0]["label"] != "还剩" || ev.Body[0]["label"] != "Days left" || zv.Body[0]["unit"] != "天" || ev.Body[0]["unit"] != "days" {
		t.Errorf("countdown zh %+v en %+v", zv, ev)
	}
	if !regexp.MustCompile(`^\d+ 小时 \d+ 分$`).MatchString(zv.Body[1]["text"].(string)) || !regexp.MustCompile(`^\d+h \d+m$`).MatchString(ev.Body[1]["text"].(string)) {
		t.Errorf("countdown remainder: %v / %v", zv.Body[1]["text"], ev.Body[1]["text"])
	}
	bad, _ := e.m.Test(bg, "countdown", TestRequest{Hook: "widget", Lang: "zh", Config: map[string]any{"date": "nope"}})
	if bv := bad.Result.(WidgetView); bv.Error != "日期格式无效：nope (YYYY-MM-DD)" {
		t.Errorf("bad date: %q", bv.Error)
	}
	if cd.Name.Tr["zh"] != "倒计时" || cd.Widgets[0].Config[0].Label.Tr["zh"] != "标签" {
		t.Errorf("countdown manifest: %+v", cd.Name)
	}

	wz, _ := e.m.Test(bg, "world-clock", TestRequest{Hook: "widget", Lang: "zh", Config: map[string]any{"zones": "Asia/Tokyo, UTC+8, Nope/Zone"}})
	we, _ := e.m.Test(bg, "world-clock", TestRequest{Hook: "widget", Lang: "en", Config: map[string]any{"zones": "Asia/Tokyo, UTC+8, Nope/Zone"}})
	if !wz.OK || !we.OK {
		t.Fatalf("%+v %+v", wz, we)
	}
	zi := wz.Result.(WidgetView).Body[0]["items"].([]map[string]any)
	ei := we.Result.(WidgetView).Body[0]["items"].([]map[string]any)
	zhDay := regexp.MustCompile(`^周[日一二三四五六] \d\d/\d\d · UTC\+9$`)
	enDay := regexp.MustCompile(`^(Sun|Mon|Tue|Wed|Thu|Fri|Sat) \d\d/\d\d · UTC\+9$`)
	if !zhDay.MatchString(zi[0]["subtitle"].(string)) || !enDay.MatchString(ei[0]["subtitle"].(string)) {
		t.Errorf("weekday: %v / %v", zi[0]["subtitle"], ei[0]["subtitle"])
	}
	if zi[2]["subtitle"] != "未知时区：Nope/Zone" || ei[2]["subtitle"] != "Unknown time zone: Nope/Zone" {
		t.Errorf("unknown zone: %v / %v", zi[2]["subtitle"], ei[2]["subtitle"])
	}
	if wz.Result.(WidgetView).Title != "世界时钟" || we.Result.(WidgetView).Title != "World Clock" {
		t.Errorf("titles")
	}
}
