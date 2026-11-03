// weather — current weather and a 7-day outlook from Open-Meteo (no API key).
//
// Hook:    widget(ctx) -> WidgetView
// Network: geocoding-api.open-meteo.com (city name -> coordinates)
//          api.open-meteo.com (forecast)
//
// Per-instance config (ctx.instance.config) holds the city and unit, so you
// can add several weather widgets for different cities.

export const manifest = {
  id: "weather",
  name: "Weather",
  version: "1.0.0",
  description: "Open-Meteo weather, no API key. 天气（Open-Meteo）。",
  author: "genkan",
  homepage: "https://open-meteo.com",
  icon: "cloud",
  permissions: { fetch: ["api.open-meteo.com", "geocoding-api.open-meteo.com"] },
  widgets: [
    {
      id: "current",
      name: "Current weather",
      size: "m",
      refresh: 600,
      config: [
        { key: "city", type: "string", label: "City", help: "e.g. Tokyo, 上海, Berlin", required: true },
        {
          key: "unit",
          type: "select",
          label: "Unit",
          default: "c",
          options: [
            { value: "c", label: "Celsius (°C)" },
            { value: "f", label: "Fahrenheit (°F)" },
          ],
        },
      ],
    },
  ],
};

// WMO weather interpretation codes -> [English, Chinese, lucide icon].
function describe(code, isDay) {
  if (code === 0) return isDay ? ["Clear", "晴", "sun"] : ["Clear", "晴", "moon"];
  if (code === 1) return ["Mostly clear", "大部晴朗", isDay ? "sun" : "moon"];
  if (code === 2) return ["Partly cloudy", "多云", "cloud-sun"];
  if (code === 3) return ["Overcast", "阴", "cloud"];
  if (code === 45 || code === 48) return ["Fog", "雾", "cloud-fog"];
  if (code >= 51 && code <= 57) return ["Drizzle", "毛毛雨", "cloud-rain"];
  if (code >= 61 && code <= 67) return ["Rain", "雨", "cloud-rain"];
  if (code >= 71 && code <= 77) return ["Snow", "雪", "cloud-snow"];
  if (code >= 80 && code <= 82) return ["Rain showers", "阵雨", "cloud-rain"];
  if (code === 85 || code === 86) return ["Snow showers", "阵雪", "cloud-snow"];
  if (code >= 95) return ["Thunderstorm", "雷暴", "cloud-lightning"];
  return ["Unknown", "未知", "cloud"];
}

async function getJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`HTTP ${res.status} from ${new URL(url).hostname}`);
  return res.json();
}

// City -> {lat, lon, name}. Cached in plugin storage so we geocode only once
// per city (and the widget still works if the geocoder is down later).
async function locate(city, lang) {
  const key = "geo:" + lang + ":" + city.toLowerCase();
  const cached = genkan.storage.get(key);
  if (cached) return cached;

  const q = new URLSearchParams({ name: city, count: "1", format: "json", language: lang });
  const data = await getJSON("https://geocoding-api.open-meteo.com/v1/search?" + q);
  const r = data.results && data.results[0];
  if (!r) return null;
  const place = { lat: r.latitude, lon: r.longitude, name: [r.name, r.country].filter(Boolean).join(", ") };
  try {
    genkan.storage.set(key, place);
  } catch (e) {
    // Storage is only an optimisation.
  }
  return place;
}

export async function widget(ctx) {
  const zh = ctx.lang === "zh";
  const title = zh ? "天气" : "Weather";
  const fail = (error) => ({ title, icon: "cloud", body: [], error });

  const cfg = (ctx.instance && ctx.instance.config) || {};
  const city = String(cfg.city || "").trim();
  if (!city) return fail(zh ? "请在小组件设置中填写城市" : "Set a city in the widget settings");
  const f = cfg.unit === "f";

  try {
    const place = await locate(city, zh ? "zh" : "en");
    if (!place) return fail((zh ? "找不到城市：" : "City not found: ") + city);

    const q = new URLSearchParams({
      latitude: String(place.lat),
      longitude: String(place.lon),
      current: "temperature_2m,apparent_temperature,relative_humidity_2m,weather_code,wind_speed_10m,is_day",
      daily: "temperature_2m_max,temperature_2m_min,weather_code",
      forecast_days: "7",
      timezone: "auto",
      temperature_unit: f ? "fahrenheit" : "celsius",
    });
    const w = await getJSON("https://api.open-meteo.com/v1/forecast?" + q);
    const cur = w.current;
    const unit = f ? "°F" : "°C";
    const [en, cn, icon] = describe(cur.weather_code, cur.is_day === 1);
    const round = (x) => Math.round(x);

    const highs = w.daily.temperature_2m_max.map(round);
    const lows = w.daily.temperature_2m_min.map(round);

    return {
      title: place.name,
      icon,
      link: "https://open-meteo.com/",
      body: [
        { type: "stat", label: zh ? cn : en, value: round(cur.temperature_2m), unit, icon },
        {
          type: "kv",
          items: [
            { k: zh ? "体感" : "Feels like", v: round(cur.apparent_temperature) + unit },
            { k: zh ? "湿度" : "Humidity", v: cur.relative_humidity_2m + "%" },
            { k: zh ? "风速" : "Wind", v: round(cur.wind_speed_10m) + " km/h" },
            { k: zh ? "今日" : "Today", v: `${lows[0]}° / ${highs[0]}°` },
          ],
        },
        { type: "text", text: zh ? "未来 7 天最高温" : "7-day highs", size: "s", muted: true },
        // labels are "MM-DD" for each forecast day
        { type: "chart", kind: "spark", values: highs, labels: w.daily.time.map((d) => d.slice(5)) },
      ],
    };
  } catch (e) {
    return fail((zh ? "天气获取失败：" : "Weather unavailable: ") + (e && e.message ? e.message : e));
  }
}
