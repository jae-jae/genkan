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
// embed `time/tzdata`). We ask Intl for the numeric wall-clock *parts* in
// en-US and format "MM/DD" and "HH:mm" ourselves; only the weekday name is
// localized (Intl with ctx.locale). Fixed offsets don't need the tz database
// at all (that is also the fallback when a zone name is unknown to the
// runtime: it is shown as invalid).

export const messages = {
  en: {
    name: "World Clock",
    desc: "Clocks for several time zones, offline.",
    zones: "Time zones",
    zonesHelp: "IANA names separated by commas or new lines; optional label: Home=Europe/Berlin",
    unknownZone: "Unknown time zone: {zone}",
  },
  zh: {
    name: "世界时钟",
    desc: "多个时区的时钟（离线）。",
    zones: "时区",
    zonesHelp: "IANA 时区名，用逗号或换行分隔；可加标签，例如 Home=Europe/Berlin",
    unknownZone: "未知时区：{zone}",
  },
};

export const manifest = {
  id: "world-clock",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  icon: "clock",
  widgets: [
    {
      id: "clock",
      name: t("name"),
      size: "m",
      refresh: 30, // the minimum; the display has minute resolution
      config: [
        {
          key: "zones",
          type: "text",
          label: t("zones"),
          help: t("zonesHelp"),
          default: "UTC, Asia/Tokyo, Europe/London, America/New_York",
        },
      ],
    },
  ],
};

const DEFAULT_ZONES = "UTC, Asia/Tokyo, Europe/London, America/New_York";
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
    wall,
    offsetMin,
  };
}

function offsetLabel(min) {
  const a = Math.abs(min);
  return "UTC" + (min < 0 ? "-" : "+") + Math.floor(a / 60) + (a % 60 ? ":" + pad(a % 60) : "");
}

export async function widget(ctx) {
  const { t, locale } = ctx;
  // "Mon" / "周一": the wall-clock instant is already shifted, so format it as UTC.
  const weekday = new Intl.DateTimeFormat(locale, { weekday: "short", timeZone: "UTC" });
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
        const c = wallClock(zone, ctx.now);
        return {
          title: label,
          subtitle: `${weekday.format(c.wall)} ${pad(c.month)}/${pad(c.day)} · ${offsetLabel(c.offsetMin)}`,
          right: `${pad(c.hour)}:${pad(c.minute)}`,
        };
      } catch (e) {
        return { title: label, subtitle: t("unknownZone", { zone }), right: "--:--" };
      }
    });

  return { title: t("name"), icon: "clock", body: [{ type: "list", items }] };
}
