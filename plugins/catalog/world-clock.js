// world-clock — a list of clocks for several time zones. Fully offline.
//
// Hook:   widget(ctx) -> WidgetView
// Output: a `list` block, one item per zone:
//           title    = label           e.g. "Tokyo"
//           subtitle = "Mon 11/03 · UTC+9"
//           right    = "HH:mm" (24 h)  e.g. "21:05"
//         Defaults to UTC, Tokyo, London and New York, so a freshly added
//         widget renders immediately.
//
// Config `zones`: comma or newline separated IANA names ("Asia/Shanghai"),
// optionally with a label ("Home=Europe/Berlin"). Fixed offsets such as
// "UTC+8" or "-05:30" are accepted too.
//
// Implementation notes: time zones come from Intl.DateTimeFormat's `timeZone`
// option (moejs reads the host's tz database, so the Genkan binary should
// embed `time/tzdata`). We only ask Intl for the numeric wall-clock *parts*
// in en-US and format them ourselves, which keeps the output independent of
// locale data. Fixed offsets don't need Intl at all (that is also the
// fallback when a zone name is unknown to the runtime: it is shown as invalid).

export const manifest = {
  id: "world-clock",
  name: "World Clock",
  version: "1.0.0",
  description: "Clocks for several time zones, offline. 世界时钟（离线）。",
  author: "genkan",
  icon: "clock",
  widgets: [
    {
      id: "clock",
      name: "World clock",
      size: "m",
      refresh: 30, // the minimum; the display has minute resolution
      config: [
        {
          key: "zones",
          type: "text",
          label: "Time zones",
          help: "IANA names separated by commas or new lines; optional label: Home=Europe/Berlin",
          default: "UTC, Asia/Tokyo, Europe/London, America/New_York",
        },
      ],
    },
  ],
};

const DEFAULT_ZONES = "UTC, Asia/Tokyo, Europe/London, America/New_York";
const WEEKDAYS = {
  en: ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"],
  zh: ["周日", "周一", "周二", "周三", "周四", "周五", "周六"],
};
const pad = (n) => String(n).padStart(2, "0");

// "UTC+8", "GMT-05:30", "+0530", "Z" -> offset in minutes, or null.
function fixedOffset(zone) {
  if (/^(utc|gmt|z)$/i.test(zone)) return 0;
  const m = /^(?:utc|gmt)?\s*([+-])(\d{1,2})(?::?(\d{2}))?$/i.exec(zone);
  if (!m) return null;
  return (m[1] === "-" ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3] || 0));
}

// Wall-clock fields of instant `ms` in `zone`; throws for an unknown zone.
function wallClock(zone, ms) {
  const fixed = fixedOffset(zone);
  if (fixed !== null) return fromWall(Date.UTC(1970, 0, 1) + ms + fixed * 60000, fixed);

  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: zone,
    hourCycle: "h23",
    year: "numeric",
    month: "numeric",
    day: "numeric",
    hour: "numeric",
    minute: "numeric",
  }).formatToParts(ms);
  const p = {};
  for (const x of parts) p[x.type] = Number(x.value);
  const wall = Date.UTC(p.year, p.month - 1, p.day, p.hour % 24, p.minute);
  // offset = wall-clock minus real time (both truncated to the minute)
  return fromWall(wall, Math.round((wall - Math.floor(ms / 60000) * 60000) / 60000));
}

function fromWall(wall, offsetMin) {
  const d = new Date(wall);
  return {
    month: d.getUTCMonth() + 1,
    day: d.getUTCDate(),
    hour: d.getUTCHours(),
    minute: d.getUTCMinutes(),
    weekday: d.getUTCDay(),
    offsetMin,
  };
}

function offsetLabel(min) {
  const a = Math.abs(min);
  return "UTC" + (min < 0 ? "-" : "+") + Math.floor(a / 60) + (a % 60 ? ":" + pad(a % 60) : "");
}

export async function widget(ctx) {
  const zh = ctx.lang === "zh";
  const cfg = (ctx.instance && ctx.instance.config) || {};
  const specs = String(cfg.zones || "").trim() || DEFAULT_ZONES;

  const items = specs
    .split(/[,\n;]+/)
    .map((s) => s.trim())
    .filter(Boolean)
    .slice(0, 12)
    .map((spec) => {
      // "Label=Zone" or just "Zone"
      const eq = spec.indexOf("=");
      const zone = (eq >= 0 ? spec.slice(eq + 1) : spec).trim();
      const label = eq >= 0 ? spec.slice(0, eq).trim() : zone.split("/").pop().replace(/_/g, " ");
      try {
        const t = wallClock(zone, ctx.now);
        return {
          title: label,
          subtitle: `${WEEKDAYS[zh ? "zh" : "en"][t.weekday]} ${pad(t.month)}/${pad(t.day)} · ${offsetLabel(t.offsetMin)}`,
          right: `${pad(t.hour)}:${pad(t.minute)}`,
        };
      } catch (e) {
        return { title: label, subtitle: (zh ? "未知时区：" : "Unknown time zone: ") + zone, right: "--:--" };
      }
    });

  return { title: zh ? "世界时钟" : "World Clock", icon: "clock", body: [{ type: "list", items }] };
}
