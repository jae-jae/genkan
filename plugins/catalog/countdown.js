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

export const manifest = {
  id: "countdown",
  name: "Countdown",
  version: "1.0.0",
  description: "Days until a date, offline. 倒计时（离线）。",
  author: "genkan",
  icon: "calendar",
  widgets: [
    {
      id: "timer",
      name: "Countdown",
      size: "s",
      refresh: 60,
      config: [
        { key: "label", type: "string", label: "Label", help: "Defaults to “New Year”." },
        { key: "date", type: "string", label: "Date", help: "YYYY-MM-DD or YYYY-MM-DDTHH:mm. Empty = next New Year." },
        { key: "utc_offset", type: "number", label: "UTC offset (hours)", help: "Time zone of the date, e.g. 8 for UTC+8.", default: 0, min: -12, max: 14 },
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
  const zh = ctx.lang === "zh";
  const cfg = (ctx.instance && ctx.instance.config) || {};
  const offset = Number.isFinite(Number(cfg.utc_offset)) ? Number(cfg.utc_offset) : 0;
  const label = String(cfg.label || "").trim() || (zh ? "新年" : "New Year");
  const base = { title: label, icon: "calendar" };

  let date = String(cfg.date || "").trim();
  if (!date) date = `${new Date(ctx.now).getUTCFullYear() + 1}-01-01`;
  const p = parseDate(date);
  if (!p) return { ...base, body: [], error: (zh ? "日期格式无效：" : "Invalid date: ") + date + " (YYYY-MM-DD)" };

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
        label: past ? (zh ? "已过去" : "Days since") : zh ? "还剩" : "Days left",
        value: days,
        unit: zh ? "天" : days === 1 ? "day" : "days",
        icon: "calendar",
        trend: past ? "up" : "down",
      },
      { type: "text", text: zh ? `${hours} 小时 ${minutes} 分` : `${hours}h ${minutes}m`, align: "center", muted: true },
      { type: "text", text: `${date.slice(0, 10)} ${pad(p.h)}:${pad(p.mi)} (${tz})`, size: "s", align: "center", muted: true },
    ],
  };
}
