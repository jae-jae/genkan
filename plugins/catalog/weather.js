// weather — current weather and a 7-day outlook from Open-Meteo (no API key).
//
// Hook:    widget(ctx) -> WidgetView
// Network: geocoding-api.open-meteo.com (city name -> coordinates)
//          api.open-meteo.com (forecast)
//
// Per-instance config (ctx.instance.config) holds the city and unit, so you
// can add several weather widgets for different cities.

export const messages = {
  en: {
    name: "Weather",
    desc: "Open-Meteo weather, no API key.",
    widget: "Current weather",
    city: "City",
    cityHelp: "e.g. Tokyo, 上海, Berlin",
    unit: "Unit",
    celsius: "Celsius (°C)",
    fahrenheit: "Fahrenheit (°F)",
    feels: "Feels like",
    humidity: "Humidity",
    wind: "Wind",
    today: "Today",
    highs: "{n}-day highs",
    setCity: "Set a city in the widget settings",
    notFound: "City not found: {city}",
    failed: "Weather unavailable: {msg}",
    clear: "Clear",
    mostlyClear: "Mostly clear",
    partlyCloudy: "Partly cloudy",
    overcast: "Overcast",
    fog: "Fog",
    drizzle: "Drizzle",
    rain: "Rain",
    snow: "Snow",
    showers: "Rain showers",
    snowShowers: "Snow showers",
    thunder: "Thunderstorm",
    unknown: "Unknown",
  },
  zh: {
    name: "天气",
    desc: "Open-Meteo 天气，无需 API key。",
    widget: "当前天气",
    city: "城市",
    cityHelp: "例如 Tokyo、上海、Berlin",
    unit: "单位",
    celsius: "摄氏度 (°C)",
    fahrenheit: "华氏度 (°F)",
    feels: "体感",
    humidity: "湿度",
    wind: "风速",
    today: "今日",
    highs: "未来 {n} 天最高温",
    setCity: "请在小组件设置中填写城市",
    notFound: "找不到城市：{city}",
    failed: "天气获取失败：{msg}",
    clear: "晴",
    mostlyClear: "大部晴朗",
    partlyCloudy: "多云",
    overcast: "阴",
    fog: "雾",
    drizzle: "毛毛雨",
    rain: "雨",
    snow: "雪",
    showers: "阵雨",
    snowShowers: "阵雪",
    thunder: "雷暴",
    unknown: "未知",
  },
};

export const manifest = {
  id: "weather",
  name: t("name"),
  version: "1.0.0",
  description: t("desc"),
  author: "genkan",
  homepage: "https://open-meteo.com",
  icon: "cloud",
  permissions: { fetch: ["api.open-meteo.com", "geocoding-api.open-meteo.com"] },
  widgets: [
    {
      id: "current",
      name: t("widget"),
      size: "m",
      refresh: 600,
      config: [
        { key: "city", type: "string", label: t("city"), help: t("cityHelp"), required: true },
        {
          key: "unit",
          type: "select",
          label: t("unit"),
          default: "c",
          options: [
            { value: "c", label: t("celsius") },
            { value: "f", label: t("fahrenheit") },
          ],
        },
      ],
    },
  ],
};

// WMO weather interpretation codes -> [message key, lucide icon].
function describe(code, isDay) {
  if (code === 0) return ["clear", isDay ? "sun" : "moon"];
  if (code === 1) return ["mostlyClear", isDay ? "sun" : "moon"];
  if (code === 2) return ["partlyCloudy", "cloud-sun"];
  if (code === 3) return ["overcast", "cloud"];
  if (code === 45 || code === 48) return ["fog", "cloud-fog"];
  if (code >= 51 && code <= 57) return ["drizzle", "cloud-rain"];
  if (code >= 61 && code <= 67) return ["rain", "cloud-rain"];
  if (code >= 71 && code <= 77) return ["snow", "cloud-snow"];
  if (code >= 80 && code <= 82) return ["showers", "cloud-rain"];
  if (code === 85 || code === 86) return ["snowShowers", "cloud-snow"];
  if (code >= 95) return ["thunder", "cloud-lightning"];
  return ["unknown", "cloud"];
}

async function getJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`HTTP ${res.status} from ${new URL(url).hostname}`);
  return res.json();
}

// City -> {lat, lon, name}. Cached in plugin storage so we geocode only once
// per city and language (and the widget still works if the geocoder is down
// later). Open-Meteo returns place names in the requested language.
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
  const { t } = ctx;
  const title = t("name");
  const fail = (error) => ({ title, icon: "cloud", body: [], error });

  const cfg = (ctx.instance && ctx.instance.config) || {};
  const city = String(cfg.city || "").trim();
  if (!city) return fail(t("setCity"));
  const f = cfg.unit === "f";

  try {
    const place = await locate(city, ctx.lang);
    if (!place) return fail(t("notFound", { city }));

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
    const [kind, icon] = describe(cur.weather_code, cur.is_day === 1);
    const round = (x) => Math.round(x);

    const highs = w.daily.temperature_2m_max.map(round);
    const lows = w.daily.temperature_2m_min.map(round);

    return {
      title: place.name,
      icon,
      link: "https://open-meteo.com/",
      body: [
        { type: "stat", label: t(kind), value: round(cur.temperature_2m), unit, icon },
        {
          type: "kv",
          items: [
            { k: t("feels"), v: round(cur.apparent_temperature) + unit },
            { k: t("humidity"), v: cur.relative_humidity_2m + "%" },
            { k: t("wind"), v: round(cur.wind_speed_10m) + " km/h" },
            { k: t("today"), v: `${lows[0]}° / ${highs[0]}°` },
          ],
        },
        { type: "text", text: t("highs", { n: highs.length }), size: "s", muted: true },
        // labels are "MM-DD" for each forecast day
        { type: "chart", kind: "spark", values: highs, labels: w.daily.time.map((d) => d.slice(5)) },
      ],
    };
  } catch (e) {
    return fail(t("failed", { msg: e && e.message ? e.message : e }));
  }
}
