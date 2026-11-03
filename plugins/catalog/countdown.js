// countdown — days left until a date. Fully offline.
//
// Hook:   widget(ctx) -> WidgetView
// Output: title = the label, then
//           1. a `stat`  { label: "Days left" | "Days since", value: <whole days>, unit: "days" }
//           2. a `text`  "<h>h <m>m" (the remainder after the whole days)
//           3. a `text`  the target as "YYYY-MM-DD HH:mm (UTC+x)"
//         `value` is floor(|target - now| / 1 day): deterministic and easy to
//         assert. After the target has passed the label switches to
//         "Days since" and counts upwards.
//
// Config `date`: "YYYY-MM-DD" or "YYYY-MM-DDTHH:mm". It is interpreted in the
// time zone given by `utc_offset` (hours, default 0 = UTC) — never the
// server's local zone. If `date` is empty the widget counts down to the next
// 1 January, so a freshly added widget renders immediately.

export const messages = {
  en: {
    name: "Countdown",
    desc: "Days until a date, offline.",
    label: "Label",
    labelHelp: "Defaults to “New Year”.",
    date: "Date",
    dateHelp: "YYYY-MM-DD or YYYY-MM-DDTHH:mm. Empty = next New Year.",
    offset: "UTC offset (hours)",
    offsetHelp: "Time zone of the date, e.g. 8 for UTC+8.",
    newYear: "New Year",
    badDate: "Invalid date: {date} (YYYY-MM-DD)",
    left: "Days left",
    since: "Days since",
    day: "day",
    days: "days",
    hm: "{h}h {m}m",
  },
  zh: {
    name: "倒计时",
    desc: "距离某一天还有几天（离线）。",
    label: "标签",
    labelHelp: "默认为“新年”。",
    date: "日期",
    dateHelp: "YYYY-MM-DD 或 YYYY-MM-DDTHH:mm。留空 = 下一个新年。",
    offset: "UTC 偏移（小时）",
    offsetHelp: "日期所在的时区，例如 UTC+8 填 8。",
    newYear: "新年",
    badDate: "日期格式无效：{date} (YYYY-MM-DD)",
    left: "还剩",
    since: "已过去",
    day: "天",
    days: "天",
    hm: "{h} 小时 {m} 分",
  },
};

export const manifest = {
  id: "countdown",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  icon: "calendar",
  widgets: [
    {
      id: "timer",
      name: t("name"),
      size: "s",
      refresh: 60,
      config: [
        { key: "label", type: "string", label: t("label"), help: t("labelHelp") },
        { key: "date", type: "string", label: t("date"), help: t("dateHelp") },
        { key: "utc_offset", type: "number", label: t("offset"), help: t("offsetHelp"), default: 0, min: -12, max: 14 },
      ],
    },
  ],
};

const HOUR = 3600000;
const DAY = 24 * HOUR;
const pad = (n) => String(n).padStart(2, "0");

// "2027-01-01" / "2027-01-01T09:30" -> {y,mo,d,h,mi} or null (also rejects 02-30).
function parseDate(s) {
  const m = /^(\d{4})-(\d{1,2})-(\d{1,2})(?:[T ](\d{1,2}):(\d{2}))?$/.exec(s.trim());
  if (!m) return null;
  const [y, mo, d, h, mi] = [m[1], m[2], m[3], m[4] || 0, m[5] || 0].map(Number);
  const t = new Date(Date.UTC(y, mo - 1, d, h, mi));
  if (t.getUTCFullYear() !== y || t.getUTCMonth() !== mo - 1 || t.getUTCDate() !== d || h > 23 || mi > 59) return null;
  return { y, mo, d, h, mi };
}

export async function widget(ctx) {
  const { t } = ctx;
  const cfg = (ctx.instance && ctx.instance.config) || {};
  const offset = Number.isFinite(Number(cfg.utc_offset)) ? Number(cfg.utc_offset) : 0;
  const label = String(cfg.label || "").trim() || t("newYear");
  const base = { title: label, icon: "calendar" };

  let date = String(cfg.date || "").trim();
  if (!date) date = `${new Date(ctx.now).getUTCFullYear() + 1}-01-01`;
  const p = parseDate(date);
  if (!p) return { ...base, body: [], error: t("badDate", { date }) };

  // Target instant: the wall-clock time `p` in the zone UTC+offset.
  const target = Date.UTC(p.y, p.mo - 1, p.d, p.h, p.mi) - offset * HOUR;
  const diff = target - ctx.now;
  const past = diff < 0;
  const abs = Math.abs(diff);
  const days = Math.floor(abs / DAY);
  const hours = Math.floor((abs % DAY) / HOUR);
  const minutes = Math.floor((abs % HOUR) / 60000);

  const sign = offset < 0 ? "-" : "+";
  const tz = "UTC" + sign + Math.abs(offset);
  return {
    ...base,
    body: [
      {
        type: "stat",
        label: past ? t("since") : t("left"),
        value: days,
        unit: days === 1 ? t("day") : t("days"),
        icon: "calendar",
        trend: past ? "up" : "down",
      },
      { type: "text", text: t("hm", { h: hours, m: minutes }), align: "center", muted: true },
      { type: "text", text: `${date.slice(0, 10)} ${pad(p.h)}:${pad(p.mi)} (${tz})`, size: "s", align: "center", muted: true },
    ],
  };
}
