import { http, HttpResponse } from "msw";
import {
  ACTIVITIES,
  HEATMAP_GEOJSON,
  GEAR,
  PROFILE,
  SETTINGS,
  DASHBOARDS,
  WIDGETS,
  STREAKS,
  BESTS,
  buildSamples,
  getStats,
  getTimeseries,
  getPMC,
  getRecords,
  getYearReview,
} from "./data";

const BASE = "/api/v1";

const DEMO_ERROR = HttpResponse.json(
  { error: "Demo mode — changes are not saved." },
  { status: 403 }
);

// ── Health ────────────────────────────────────────────────────────────────────
const health = http.get(`${BASE}/health`, () =>
  HttpResponse.json({ ok: true, auth_enabled: false, public_mode: false, time: new Date().toISOString() })
);

// ── Activities ────────────────────────────────────────────────────────────────
const actList = http.get(`${BASE}/activities`, ({ request }) => {
  const url = new URL(request.url);
  const sport = url.searchParams.get("sport_type") || "";
  const limitParam = parseInt(url.searchParams.get("limit") || "20", 10);
  const cursor = url.searchParams.get("cursor") || "";
  const from = url.searchParams.get("from");
  const to   = url.searchParams.get("to");

  let acts = [...ACTIVITIES];
  if (sport) acts = acts.filter(a => a.sport_type === sport);
  if (from)  acts = acts.filter(a => a.start_at >= from);
  if (to)    acts = acts.filter(a => a.start_at <= to + "T23:59:59Z");

  // Adjacent-activity nav uses { from, limit:2 } with no `to`.
  // Sort ascending so the activity at `from` comes first, next newer second.
  // All other queries keep the default newest-first order.
  if (from && !to) {
    acts.sort((a, b) => a.start_at.localeCompare(b.start_at));
  }

  // cursor-based pagination: cursor = start index
  const startIdx = cursor ? parseInt(cursor, 10) : 0;
  const page = acts.slice(startIdx, startIdx + limitParam);
  const nextCursor = startIdx + limitParam < acts.length
    ? String(startIdx + limitParam)
    : "";

  // Strip internal fields before returning
  const items = page.map(({ _route, _routeCoords, _samples, laps, tags, notes, ...rest }) => rest);
  return HttpResponse.json({ activities: items, next_cursor: nextCursor });
});

const actGet = http.get(`${BASE}/activities/:id`, ({ params }) => {
  const act = ACTIVITIES.find(a => a.id === params.id);
  if (!act) return HttpResponse.json({ error: "not found" }, { status: 404 });
  const { _route, _routeCoords, _samples, ...rest } = act;
  return HttpResponse.json(rest);
});

const actUpdate   = http.patch(`${BASE}/activities/:id`, () => DEMO_ERROR);
const actDelete   = http.delete(`${BASE}/activities/:id`, () => DEMO_ERROR);

const actSamples = http.get(`${BASE}/activities/:id/samples`, ({ params }) => {
  const act = ACTIVITIES.find(a => a.id === params.id);
  if (!act) return HttpResponse.json([], { status: 200 });
  return HttpResponse.json(buildSamples(act));
});

const actSimilar = http.get(`${BASE}/activities/:id/similar`, ({ params }) => {
  const act = ACTIVITIES.find(a => a.id === params.id);
  if (!act) return HttpResponse.json([]);
  // Return up to 3 activities with the same route
  const similar = ACTIVITIES
    .filter(a => a._route === act._route && a.id !== act.id)
    .slice(0, 3)
    .map(({ _route, _routeCoords, _samples, laps, tags, ...rest }) => rest);
  return HttpResponse.json(similar);
});

const actWeather = http.get(`${BASE}/activities/:id/weather`, () =>
  HttpResponse.json({
    temperature_celsius: 16.5,
    feels_like_celsius: 15.2,
    humidity_percent: 72,
    wind_speed_kph: 14,
    condition: "Partly cloudy",
    icon: "partly-cloudy-day",
  })
);

const actGear = http.get(`${BASE}/activities/:id/gear`, () =>
  HttpResponse.json([{ id: "gear-001", name: "Nike Pegasus 41" }])
);

const actSetTag    = http.put(`${BASE}/activities/:id/tags/:key`, () => DEMO_ERROR);
const actDeleteTag = http.delete(`${BASE}/activities/:id/tags/:key`, () => DEMO_ERROR);
const actSetGear   = http.put(`${BASE}/activities/:id/gear`, () => DEMO_ERROR);

// ── Heatmap ───────────────────────────────────────────────────────────────────
const heatmap = http.get(`${BASE}/heatmap`, () =>
  HttpResponse.json(HEATMAP_GEOJSON)
);

// ── Stats ─────────────────────────────────────────────────────────────────────
const stats = http.get(`${BASE}/stats`, ({ request }) => {
  const url = new URL(request.url);
  return HttpResponse.json(getStats(url.searchParams.get("from"), url.searchParams.get("to")));
});

const statsTimeseries = http.get(`${BASE}/stats/timeseries`, ({ request }) => {
  const url = new URL(request.url);
  return HttpResponse.json(getTimeseries(
    url.searchParams.get("granularity") || "week",
    url.searchParams.get("from"),
    url.searchParams.get("to"),
  ));
});

const statsPMC = http.get(`${BASE}/stats/pmc`, ({ request }) => {
  const url = new URL(request.url);
  return HttpResponse.json(getPMC(url.searchParams.get("from"), url.searchParams.get("to")));
});

const statsStreaks    = http.get(`${BASE}/stats/streaks`, () => HttpResponse.json(STREAKS));
const statsBests      = http.get(`${BASE}/stats/bests`,   () => HttpResponse.json(BESTS));

const statsYearReview = http.get(`${BASE}/stats/year-review`, ({ request }) => {
  const year = new URL(request.url).searchParams.get("year") || new Date().getFullYear();
  const rev = getYearReview(year);
  if (!rev) return HttpResponse.json({ error: "no data" }, { status: 404 });
  return HttpResponse.json(rev);
});

// ── Records ───────────────────────────────────────────────────────────────────
const records = http.get(`${BASE}/records`, ({ request }) => {
  const sport = new URL(request.url).searchParams.get("sport_type") || "run";
  return HttpResponse.json(getRecords(sport));
});

// ── Gear ──────────────────────────────────────────────────────────────────────
const gearList   = http.get(`${BASE}/gear`,  () => HttpResponse.json(GEAR));
const gearCreate = http.post(`${BASE}/gear`, () => DEMO_ERROR);
const gearUpdate = http.put(`${BASE}/gear/:id`, () => DEMO_ERROR);
const gearDelete = http.delete(`${BASE}/gear/:id`, () => DEMO_ERROR);

// ── Profile ───────────────────────────────────────────────────────────────────
const profileGet    = http.get(`${BASE}/profile`, () => HttpResponse.json(PROFILE));
const profileUpdate = http.put(`${BASE}/profile`, () => DEMO_ERROR);
const profileAvatar = http.get(`${BASE}/profile/avatar`, () =>
  // Return a simple SVG placeholder avatar
  new HttpResponse(
    `<svg xmlns="http://www.w3.org/2000/svg" width="96" height="96" viewBox="0 0 96 96">
      <circle cx="48" cy="48" r="48" fill="#e2e8f0"/>
      <circle cx="48" cy="38" r="18" fill="#94a3b8"/>
      <ellipse cx="48" cy="80" rx="28" ry="18" fill="#94a3b8"/>
    </svg>`,
    { headers: { "Content-Type": "image/svg+xml" } }
  )
);

// ── Settings ──────────────────────────────────────────────────────────────────
const settingsGet  = http.get(`${BASE}/settings`, () => HttpResponse.json(SETTINGS));
const settingsSave = http.put(`${BASE}/settings`,  () => DEMO_ERROR);

// ── Dashboards ────────────────────────────────────────────────────────────────
const dashList    = http.get(`${BASE}/dashboards`,       () => HttpResponse.json(DASHBOARDS));
const dashGet     = http.get(`${BASE}/dashboards/:id`,   () => HttpResponse.json(DASHBOARDS[0]));
const dashCreate  = http.post(`${BASE}/dashboards`,      () => DEMO_ERROR);
const dashUpdate  = http.put(`${BASE}/dashboards/:id`,   () => DEMO_ERROR);
const dashDelete  = http.delete(`${BASE}/dashboards/:id`, () => DEMO_ERROR);

const widgetList   = http.get(`${BASE}/dashboards/:id/widgets`,    () => HttpResponse.json(WIDGETS));
const widgetCreate = http.post(`${BASE}/dashboards/:id/widgets`,   () => DEMO_ERROR);
const widgetUpdate = http.put(`${BASE}/dashboards/:id/widgets/:wid`, () => DEMO_ERROR);
const widgetDelete = http.delete(`${BASE}/dashboards/:id/widgets/:wid`, () => DEMO_ERROR);

// ── Upload / Export / Sources / Jobs (demo-blocked) ───────────────────────────
const uploadBlock  = http.post(`${BASE}/uploads`,  () => DEMO_ERROR);
const exportBlock  = http.post(`${BASE}/exports`,  () => DEMO_ERROR);
const sourcesBlock = http.post(`${BASE}/sources/:rest*`, () => DEMO_ERROR);
const jobsGet      = http.get(`${BASE}/jobs`,  () => HttpResponse.json([]));

// ── Export all handlers ───────────────────────────────────────────────────────
export const handlers = [
  health,
  actList, actGet, actUpdate, actDelete,
  actSamples, actSimilar, actWeather, actGear,
  actSetTag, actDeleteTag, actSetGear,
  heatmap,
  stats, statsTimeseries, statsPMC, statsStreaks, statsBests, statsYearReview,
  records,
  gearList, gearCreate, gearUpdate, gearDelete,
  profileGet, profileUpdate, profileAvatar,
  settingsGet, settingsSave,
  dashList, dashGet, dashCreate, dashUpdate, dashDelete,
  widgetList, widgetCreate, widgetUpdate, widgetDelete,
  uploadBlock, exportBlock, sourcesBlock, jobsGet,
];
