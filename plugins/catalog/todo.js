// todo — a to-do list you can use right on the start page. Fully offline.
//
// Hooks:  widget(ctx) -> WidgetView        action(ctx) -> { view }
// Storage: ctx.instance.storage key "items" = [{ id, text, done, at }]
//          (at most 200 items, text <= 500 characters). Every widget instance
//          has its own list.
// View:    an `input` (action "add"), then a `list` with unfinished items first
//          and finished ones after them (each group keeps its order). Every
//          item has an id, a checkbox (action "toggle"), in-place editing
//          (action "rename") and a trash button (action "remove"); the list can
//          be reordered by dragging (action "reorder"). A "clear finished"
//          button (action "clear") appears once something is done.
// Actions: add {value} · toggle {id, checked?} · rename {id, value} ·
//          remove {id} · reorder {ids} · clear. Each one returns
//          { view: await widget(ctx) } so the card updates without a reload.
//          Bad input (empty text, unknown id, a 201st item) throws a
//          localized error, which the UI shows as a toast.

export const messages = {
  en: {
    name: "To-do",
    desc: "A simple to-do list on your start page.",
    placeholder: "Add a task",
    add: "Add",
    remove: "Delete",
    empty: "Nothing to do. Enjoy!",
    clear: "Clear finished ({n})",
    errEmpty: "Please enter some text.",
    errNotFound: "That item no longer exists.",
    errFull: "The list is full (max {n} items).",
    errBad: "Invalid request.",
    errUnknown: "Unknown action: {action}",
  },
  zh: {
    name: "待办",
    desc: "放在起始页上的简单待办清单。",
    placeholder: "添加待办",
    add: "添加",
    remove: "删除",
    empty: "没有待办，轻松一下吧",
    clear: "清除已完成 ({n})",
    errEmpty: "请输入内容。",
    errNotFound: "该条目已不存在。",
    errFull: "清单已满（最多 {n} 条）。",
    errBad: "请求无效。",
    errUnknown: "未知操作：{action}",
  },
};

export const manifest = {
  id: "todo",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  icon: "list-todo",
  widgets: [{ id: "list", name: t("name"), size: "m", refresh: 60 }],
};

const MAX_ITEMS = 200;
const MAX_TEXT = 500;

// Single-line text: control characters removed, whitespace collapsed, at most MAX_TEXT characters.
function cleanText(v) {
  if (typeof v !== "string") return "";
  const s = v.replace(/[\u0000-\u001f\u007f]+/g, " ").replace(/\s+/g, " ").trim();
  return Array.from(s).slice(0, MAX_TEXT).join("");
}

function load(ctx) {
  const raw = ctx.instance.storage.get("items");
  if (!Array.isArray(raw)) return [];
  return raw
    .filter((i) => i && typeof i.id === "string" && typeof i.text === "string")
    .map((i) => ({ id: i.id, text: i.text, done: i.done === true, at: Number(i.at) || 0 }));
}

const save = (ctx, items) => ctx.instance.storage.set("items", items);

function newId(items, now) {
  const used = new Set(items.map((i) => i.id));
  for (;;) {
    const id = "i" + now.toString(36) + Math.floor(Math.random() * 1679616).toString(36);
    if (!used.has(id)) return id;
  }
}

export async function widget(ctx) {
  const { t } = ctx;
  const items = load(ctx);
  const open = items.filter((i) => !i.done);
  const done = items.filter((i) => i.done);
  const rows = [...open, ...done].map((i) => ({
    id: i.id,
    title: i.text,
    check: { checked: i.done, action: "toggle", params: { id: i.id } },
    edit: { action: "rename", params: { id: i.id } },
    actions: [{ icon: "trash-2", label: t("remove"), action: "remove", params: { id: i.id } }],
  }));
  const body = [
    { type: "input", action: "add", placeholder: t("placeholder"), submit: t("add"), icon: "plus", maxLength: MAX_TEXT },
    { type: "list", items: rows, sortAction: "reorder", empty: t("empty") },
  ];
  if (done.length > 0) {
    body.push({ type: "button", label: t("clear", { n: done.length }), action: "clear", icon: "eraser" });
  }
  const name = t("name");
  return { title: open.length > 0 ? `${name} · ${open.length}` : name, icon: "list-todo", body };
}

export async function action(ctx) {
  const { t } = ctx;
  const p = ctx.params && typeof ctx.params === "object" ? ctx.params : {};
  const items = load(ctx);
  const find = () => {
    const i = items.find((x) => x.id === p.id);
    if (!i) throw new Error(t("errNotFound"));
    return i;
  };

  switch (ctx.action) {
    case "add": {
      const text = cleanText(p.value);
      if (!text) throw new Error(t("errEmpty"));
      if (items.length >= MAX_ITEMS) throw new Error(t("errFull", { n: MAX_ITEMS }));
      items.push({ id: newId(items, ctx.now), text, done: false, at: ctx.now });
      break;
    }
    case "toggle": {
      const it = find();
      it.done = typeof p.checked === "boolean" ? p.checked : !it.done;
      break;
    }
    case "rename": {
      const it = find();
      const text = cleanText(p.value);
      if (!text) throw new Error(t("errEmpty"));
      it.text = text;
      break;
    }
    case "remove": {
      const it = find();
      items.splice(items.indexOf(it), 1);
      break;
    }
    case "reorder": {
      if (!Array.isArray(p.ids)) throw new Error(t("errBad"));
      // Items named in `ids` take that order; items it does not mention keep their order at the end.
      const byId = new Map(items.map((i) => [i.id, i]));
      const ordered = [];
      for (const id of p.ids) {
        const it = byId.get(id);
        if (it) {
          ordered.push(it);
          byId.delete(id);
        }
      }
      items.splice(0, items.length, ...ordered, ...byId.values());
      break;
    }
    case "clear": {
      const keep = items.filter((i) => !i.done);
      items.splice(0, items.length, ...keep);
      break;
    }
    default:
      throw new Error(t("errUnknown", { action: String(ctx.action) }));
  }
  save(ctx, items);
  return { view: await widget(ctx) };
}
