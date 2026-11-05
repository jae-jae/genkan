// note — a sticky note with Markdown, edited right on the start page. Fully offline.
//
// Hooks:   widget(ctx) -> WidgetView        action(ctx) -> { view }
// Config:  `tone` (instance): default | yellow | pink | blue | green | purple;
//          anything but "default" tints the card.
// Storage: ctx.instance.storage key "text" = the note, at most 20000 characters.
//          Every widget instance has its own note.
// View:    no title bar; one `markdown` block with editAction "save". The admin
//          edits it in place (double-click or the edit button); `- [ ]` /
//          `- [x]` task items can be ticked directly. Guests see it read-only.
// Action:  save {value} stores the text (an empty value clears the note) and
//          returns { view: await widget(ctx) }.

export const messages = {
  en: {
    name: "Note",
    desc: "A sticky note with Markdown, edited in place.",
    tone: "Color",
    toneDefault: "Default",
    yellow: "Yellow",
    pink: "Pink",
    blue: "Blue",
    green: "Green",
    purple: "Purple",
    placeholder: "Double-click to write a note (Markdown supported)…",
    errBad: "Invalid request.",
    errUnknown: "Unknown action: {action}",
  },
  zh: {
    name: "便签",
    desc: "支持 Markdown 的便签，就地编辑。",
    tone: "颜色",
    toneDefault: "默认",
    yellow: "黄色",
    pink: "粉色",
    blue: "蓝色",
    green: "绿色",
    purple: "紫色",
    placeholder: "双击写点什么（支持 Markdown）…",
    errBad: "请求无效。",
    errUnknown: "未知操作：{action}",
  },
};

export const manifest = {
  id: "note",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  icon: "sticky-note",
  widgets: [
    {
      id: "note",
      name: t("name"),
      size: "m",
      refresh: 60,
      config: [
        {
          key: "tone",
          type: "select",
          label: t("tone"),
          default: "yellow",
          options: [
            { value: "default", label: t("toneDefault") },
            { value: "yellow", label: t("yellow") },
            { value: "pink", label: t("pink") },
            { value: "blue", label: t("blue") },
            { value: "green", label: t("green") },
            { value: "purple", label: t("purple") },
          ],
        },
      ],
    },
  ],
};

const MAX_TEXT = 20000;
const TONES = ["yellow", "pink", "blue", "green", "purple"];

// Keeps newlines and tabs, drops other control characters, cuts at MAX_TEXT characters.
function cleanText(v) {
  const s = v.replace(/\r\n?/g, "\n").replace(/[\u0000-\u0008\u000b-\u001f\u007f]/g, "");
  return s.length > MAX_TEXT ? Array.from(s).slice(0, MAX_TEXT).join("") : s;
}

export async function widget(ctx) {
  const { t } = ctx;
  const stored = ctx.instance.storage.get("text");
  const tone = String((ctx.instance.config && ctx.instance.config.tone) || "");
  const view = {
    body: [
      {
        type: "markdown",
        text: typeof stored === "string" ? stored : "",
        editAction: "save",
        placeholder: t("placeholder"),
      },
    ],
  };
  if (TONES.includes(tone)) view.tone = tone;
  return view;
}

export async function action(ctx) {
  const { t } = ctx;
  if (ctx.action !== "save") throw new Error(t("errUnknown", { action: String(ctx.action) }));
  const value = ctx.params && ctx.params.value;
  if (typeof value !== "string") throw new Error(t("errBad"));
  const text = cleanText(value);
  if (text === "") ctx.instance.storage.delete("text");
  else ctx.instance.storage.set("text", text);
  return { view: await widget(ctx) };
}
