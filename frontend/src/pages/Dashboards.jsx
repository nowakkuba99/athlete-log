import { useState, useEffect, useCallback, useRef } from "react";
import { useParams, useNavigate, Link } from "react-router-dom";
import GridLayout, { WidthProvider } from "react-grid-layout/legacy";
import "react-grid-layout/css/styles.css";
import "react-resizable/css/styles.css";
import {
  BarChart3, Plus, Trash2, Pencil, Check, X, Settings2,
  Activity, Trophy, TrendingUp, PieChart, Zap, LayoutDashboard,
  ChevronLeft, ArrowRight, Home, Flame, ListChecks,
} from "lucide-react";
import {
  BarChart, Bar, AreaChart, Area, LineChart, Line, PieChart as RechartsPie, Pie, Cell,
  XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid,
} from "recharts";
import { api } from "../api/client";
import { useChartTheme, useDefaultDashboardId } from "../settings";

const RGL = WidthProvider(GridLayout);

// ─── Widget catalogue ─────────────────────────────────────────────────────────

const WIDGET_TYPES = [
  { type: "stat_card",       label: "Stat Card",         icon: Activity,        desc: "Single metric for a period" },
  { type: "bar_chart",       label: "Bar Chart",          icon: BarChart3,       desc: "Metric over time" },
  { type: "line_chart",      label: "Line Chart",         icon: TrendingUp,      desc: "Pace or HR trend" },
  { type: "donut",           label: "Donut Chart",        icon: PieChart,        desc: "Sport breakdown" },
  { type: "activity_feed",   label: "Activity Feed",      icon: LayoutDashboard, desc: "Recent activities list" },
  { type: "eddington",       label: "Eddington Number",   icon: Zap,             desc: "Largest N with ≥N activities ≥N km" },
  { type: "pr_card",         label: "Personal Record",    icon: Trophy,          desc: "Best time at a distance" },
  { type: "streak",          label: "Streak",             icon: Flame,           desc: "Current and longest training streaks" },
  { type: "pr_list",         label: "PR List",            icon: ListChecks,      desc: "Personal bests across distances" },
  { type: "consistency",     label: "Consistency",        icon: LayoutDashboard, desc: "52-week activity contribution grid" },
  { type: "training_load",   label: "Training Load",      icon: TrendingUp,      desc: "Acute (7-day) vs chronic (28-day) load" },
];

const METRIC_OPTS  = [{ v: "distance", l: "Distance" }, { v: "activities", l: "Activities" }, { v: "time", l: "Time" }];
const PERIOD_OPTS  = [{ v: "this_week", l: "This week" }, { v: "this_month", l: "This month" }, { v: "all_time", l: "All time" }];
const GRAN_OPTS    = [{ v: "day", l: "Day" }, { v: "week", l: "Week" }, { v: "month", l: "Month" }];
const SPORT_OPTS   = [{ v: "", l: "All sports" }, { v: "run", l: "Run" }, { v: "ride", l: "Ride" }, { v: "walk", l: "Walk" }, { v: "hike", l: "Hike" }];
const DIST_OPTS    = [
  { v: 400, l: "400 m" }, { v: 1000, l: "1 km" }, { v: 1609, l: "1 mile" },
  { v: 5000, l: "5 km" }, { v: 10000, l: "10 km" }, { v: 21097, l: "Half Marathon" }, { v: 42195, l: "Marathon" },
];

const DEFAULT_CONFIG = {
  stat_card:      { metric: "distance", period: "this_month", sport_type: "" },
  bar_chart:      { metric: "distance", granularity: "week",  sport_type: "" },
  line_chart:     { metric: "distance", granularity: "week",  sport_type: "" },
  donut:          { period: "all_time" },
  activity_feed:  { count: 5, sport_type: "" },
  eddington:      { sport_type: "ride" },
  pr_card:        { sport_type: "run", distance_meters: 5000 },
  streak:         {},
  pr_list:        { sport_type: "run", distances: [5000, 10000, 21097, 42195] },
  consistency:    { sport_type: "", weeks: 52, metric: "count" },
  training_load:  { sport_type: "" },
};

const SPORT_COLOR = { run: "#f97316", ride: "#3b82f6", walk: "#10b981", hike: "#a16207", swim: "#06b6d4" };
const DONUT_COLORS = ["#3b82f6", "#f97316", "#10b981", "#a16207", "#06b6d4", "#8b5cf6"];

// ─── Helpers ──────────────────────────────────────────────────────────────────

function periodDates(period) {
  const now = new Date();
  if (period === "this_week") {
    const d = new Date(now);
    d.setDate(d.getDate() - ((d.getDay() + 6) % 7)); // Monday
    return { from: d.toISOString().slice(0, 10), to: "" };
  }
  if (period === "this_month") {
    return { from: `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01`, to: "" };
  }
  return { from: "", to: "" };
}

function fmtDistance(m) {
  if (m == null) return "—";
  return m >= 1000 ? `${(m / 1000).toFixed(1)} km` : `${Math.round(m)} m`;
}

function fmtTime(ms) {
  if (ms == null) return "—";
  const h = Math.floor(ms / 3600000);
  const m = Math.floor((ms % 3600000) / 60000);
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

function fmtMs(ms) {
  const s = Math.round(ms / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0) return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return `${m}:${String(sec).padStart(2, "0")}`;
}

function fmtPace(ms, distM) {
  const spk = (ms / 1000) / (distM / 1000);
  const m = Math.floor(spk / 60);
  const s = Math.round(spk % 60);
  return `${m}:${String(s).padStart(2, "0")} /km`;
}

function computeEddington(activities, sportType) {
  const dists = activities
    .filter(a => !sportType || a.sport_type === sportType)
    .map(a => (a.distance_meters ?? 0) / 1000)
    .sort((a, b) => b - a);
  let e = 0;
  for (let i = 0; i < dists.length; i++) {
    if (dists[i] >= i + 1) e = i + 1;
    else break;
  }
  return e;
}

// ─── Widget renderers ─────────────────────────────────────────────────────────

function WidgetShell({ title, children, onConfig, onDelete, loading, error }) {
  return (
    <div className="card-hover h-full flex flex-col bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 group">
      <div className="flex items-center justify-between px-4 pt-3 pb-1 shrink-0">
        <span className="drag-handle text-xs font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-wide truncate cursor-grab active:cursor-grabbing flex-1">{title}</span>
        <div className="no-drag flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity shrink-0 ml-2">
          {onConfig && (
            <button onClick={onConfig} className="no-drag p-1 text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 rounded">
              <Settings2 size={13} />
            </button>
          )}
          {onDelete && (
            <button onClick={onDelete} className="no-drag p-1 text-slate-400 dark:text-slate-500 hover:text-red-500 rounded">
              <Trash2 size={13} />
            </button>
          )}
        </div>
      </div>
      <div className="flex-1 min-h-0 px-4 pb-3 overflow-hidden">
        {loading && <div className="h-full flex items-center justify-center text-xs text-slate-400 dark:text-slate-500 animate-pulse">Loading…</div>}
        {!loading && error && <div className="h-full flex items-center justify-center text-xs text-red-400">{error}</div>}
        {!loading && !error && children}
      </div>
    </div>
  );
}

function StatCardWidget({ config, onConfig, onDelete }) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    setLoading(true);
    const { from, to } = periodDates(config.period);
    api.stats.summary({ from, to, sport_type: config.sport_type })
      .then(setData).catch(e => setError(e.message)).finally(() => setLoading(false));
  }, [config.metric, config.period, config.sport_type]);

  const value = data
    ? config.metric === "distance"   ? fmtDistance(data.total_distance_meters)
    : config.metric === "activities" ? data.total_activities
    : fmtTime(data.total_elapsed_ms)
    : null;

  const label = METRIC_OPTS.find(m => m.v === config.metric)?.l ?? config.metric;
  const period = PERIOD_OPTS.find(p => p.v === config.period)?.l ?? config.period;

  return (
    <WidgetShell title={`${label} · ${period}`} onConfig={onConfig} onDelete={onDelete} loading={loading} error={error}>
      <div className="h-full flex flex-col justify-center">
        <p className="text-3xl font-bold text-slate-800 dark:text-slate-100">{value}</p>
        {config.sport_type && <p className="text-xs text-slate-400 dark:text-slate-500 mt-1 capitalize">{config.sport_type}</p>}
      </div>
    </WidgetShell>
  );
}

function BarChartWidget({ config, onConfig, onDelete }) {
  const ct = useChartTheme();
  const [data, setData] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    setLoading(true);
    api.stats.timeseries({ granularity: config.granularity, sport_type: config.sport_type })
      .then(d => setData(d ?? [])).catch(e => setError(e.message)).finally(() => setLoading(false));
  }, [config.metric, config.granularity, config.sport_type]);

  const key = config.metric === "distance" ? "distance_meters" : config.metric === "activities" ? "activity_count" : "elapsed_ms";
  const fmt = v => config.metric === "distance" ? `${(v / 1000).toFixed(0)} km` : config.metric === "activities" ? v : fmtTime(v);

  const chartData = data.map(d => ({
    label: new Date(d.period_start).toLocaleDateString(undefined, { month: "short", day: "numeric" }),
    value: d[key] ?? 0,
  }));

  return (
    <WidgetShell title={`${METRIC_OPTS.find(m => m.v === config.metric)?.l} by ${config.granularity}`} onConfig={onConfig} onDelete={onDelete} loading={loading} error={error}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart data={chartData} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke={ct.gridStroke} vertical={false} />
          <XAxis dataKey="label" tick={{ fontSize: 10, fill: ct.tickFill }} tickLine={false} axisLine={false} />
          <YAxis tick={{ fontSize: 10, fill: ct.tickFill }} tickLine={false} axisLine={false} tickFormatter={fmt} width={40} />
          <Tooltip formatter={v => [fmt(v)]} contentStyle={ct.tooltipStyle} />
          <Bar dataKey="value" fill="var(--accent)" radius={[3, 3, 0, 0]} />
        </BarChart>
      </ResponsiveContainer>
    </WidgetShell>
  );
}

function LineChartWidget({ config, onConfig, onDelete }) {
  const ct = useChartTheme();
  const [data, setData] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    setLoading(true);
    api.stats.timeseries({ granularity: config.granularity, sport_type: config.sport_type })
      .then(d => setData(d ?? [])).catch(e => setError(e.message)).finally(() => setLoading(false));
  }, [config.metric, config.granularity, config.sport_type]);

  const key = config.metric === "distance" ? "distance_meters" : config.metric === "activities" ? "activity_count" : "elapsed_ms";
  const fmt = v => config.metric === "distance" ? `${(v / 1000).toFixed(0)} km` : config.metric === "activities" ? v : fmtTime(v);

  const chartData = data.map(d => ({
    label: new Date(d.period_start).toLocaleDateString(undefined, { month: "short", day: "numeric" }),
    value: d[key] ?? 0,
  }));

  return (
    <WidgetShell title={`${METRIC_OPTS.find(m => m.v === config.metric)?.l} trend`} onConfig={onConfig} onDelete={onDelete} loading={loading} error={error}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={chartData} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke={ct.gridStroke} />
          <XAxis dataKey="label" tick={{ fontSize: 10, fill: ct.tickFill }} tickLine={false} axisLine={false} />
          <YAxis tick={{ fontSize: 10, fill: ct.tickFill }} tickLine={false} axisLine={false} tickFormatter={fmt} width={40} />
          <Tooltip formatter={v => [fmt(v)]} contentStyle={ct.tooltipStyle} />
          <Line type="monotone" dataKey="value" stroke="var(--accent)" strokeWidth={2} dot={false} />
        </LineChart>
      </ResponsiveContainer>
    </WidgetShell>
  );
}

function DonutWidget({ config, onConfig, onDelete }) {
  const ct = useChartTheme();
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    setLoading(true);
    const { from, to } = periodDates(config.period);
    api.stats.summary({ from, to })
      .then(setData).catch(e => setError(e.message)).finally(() => setLoading(false));
  }, [config.period]);

  const slices = (data?.by_sport ?? []).map((s, i) => ({
    name: s.sport_type,
    value: s.distance_meters ?? 0,
    color: SPORT_COLOR[s.sport_type] ?? DONUT_COLORS[i % DONUT_COLORS.length],
  }));

  return (
    <WidgetShell title="Sport breakdown" onConfig={onConfig} onDelete={onDelete} loading={loading} error={error}>
      <div className="h-full flex items-center gap-3">
        <ResponsiveContainer width="60%" height="100%">
          <RechartsPie>
            <Pie data={slices} dataKey="value" cx="50%" cy="50%" innerRadius="50%" outerRadius="80%" paddingAngle={2}>
              {slices.map((s, i) => <Cell key={i} fill={s.color} />)}
            </Pie>
            <Tooltip formatter={v => [fmtDistance(v)]} contentStyle={ct.tooltipStyle} />
          </RechartsPie>
        </ResponsiveContainer>
        <div className="flex-1 space-y-1.5 min-w-0">
          {slices.map(s => (
            <div key={s.name} className="flex items-center gap-1.5 text-xs">
              <span className="w-2 h-2 rounded-full shrink-0" style={{ background: s.color }} />
              <span className="capitalize truncate text-slate-600 dark:text-slate-300">{s.name}</span>
              <span className="ml-auto text-slate-400 dark:text-slate-500 shrink-0">{fmtDistance(s.value)}</span>
            </div>
          ))}
        </div>
      </div>
    </WidgetShell>
  );
}

const FEED_EMOJI = { run: "🏃", ride: "🚴", swim: "🏊", walk: "🚶", hike: "🥾" };
function fmtShortDate(iso) {
  return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

function ActivityFeedWidget({ config, onConfig, onDelete }) {
  const [activities, setActivities] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    setLoading(true);
    api.activities.list({ limit: config.count ?? 5, sport_type: config.sport_type })
      .then(d => setActivities(d?.activities ?? d ?? []))
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, [config.count, config.sport_type]);

  return (
    <WidgetShell title="Recent activities" onConfig={onConfig} onDelete={onDelete} loading={loading} error={error}>
      <div className="flex flex-col h-full -mx-4 -mb-3">
        <ul className="flex-1">
          {activities.length === 0 && (
            <li className="flex items-center justify-center h-full text-xs text-slate-400 dark:text-slate-500">No activities</li>
          )}
          {activities.map((a, i) => (
            <li key={a.id} className={i < activities.length - 1 ? "border-b border-slate-100 dark:border-slate-800" : ""}>
              <Link
                to={`/activities/${a.id}`}
                className="flex items-center gap-3 px-4 py-2.5 hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors"
              >
                <span className="text-lg w-6 text-center shrink-0">{FEED_EMOJI[a.sport_type] ?? "⚡"}</span>
                <div className="flex-1 min-w-0">
                  <p className="text-xs font-medium text-slate-700 dark:text-slate-200 truncate">{a.name || a.sport_type}</p>
                  <p className="text-[10px] text-slate-400 dark:text-slate-500">{fmtShortDate(a.start_at)}</p>
                </div>
                <div className="text-right shrink-0">
                  <p className="text-xs font-medium text-slate-700 dark:text-slate-200">{fmtDistance(a.distance_meters)}</p>
                  {a.elapsed_ms && <p className="text-[10px] text-slate-400 dark:text-slate-500">{fmtTime(a.elapsed_ms)}</p>}
                </div>
              </Link>
            </li>
          ))}
        </ul>
        <Link
          to="/activities"
          className="block px-4 py-2 border-t border-slate-100 dark:border-slate-800 text-xs text-blue-500 hover:text-blue-700 dark:hover:text-blue-400 transition-colors shrink-0"
        >
          View all →
        </Link>
      </div>
    </WidgetShell>
  );
}

function EddingtonWidget({ config, onConfig, onDelete }) {
  const [e, setE] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    setLoading(true);
    api.activities.list({ limit: 1000, sport_type: config.sport_type })
      .then(d => {
        const acts = d?.activities ?? d ?? [];
        setE(computeEddington(acts, config.sport_type));
      })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, [config.sport_type]);

  const sport = config.sport_type || "all sports";

  return (
    <WidgetShell title={`Eddington · ${sport}`} onConfig={onConfig} onDelete={onDelete} loading={loading} error={error}>
      <div className="h-full flex flex-col justify-center">
        <p className="text-5xl font-bold text-slate-800 dark:text-slate-100">{e ?? "—"}</p>
        <p className="text-xs text-slate-400 dark:text-slate-500 mt-2">
          {e != null && e > 0
            ? `${e} ${sport} activities ≥ ${e} km`
            : "Not enough data"}
        </p>
      </div>
    </WidgetShell>
  );
}

function PRCardWidget({ config, onConfig, onDelete }) {
  const [effort, setEffort] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    setLoading(true);
    api.records.list(config.sport_type)
      .then(groups => {
        const g = (groups ?? []).find(g => g.distance_meters === config.distance_meters);
        setEffort(g?.efforts?.[0] ?? null);
      })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, [config.sport_type, config.distance_meters]);

  const distLabel = DIST_OPTS.find(d => d.v === config.distance_meters)?.l ?? `${config.distance_meters} m`;

  return (
    <WidgetShell title={`PR · ${distLabel}`} onConfig={onConfig} onDelete={onDelete} loading={loading} error={error}>
      <div className="h-full flex flex-col justify-center">
        {effort ? (
          <>
            <p className="text-3xl font-bold text-slate-800 dark:text-slate-100">{fmtMs(effort.elapsed_ms)}</p>
            <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">{fmtPace(effort.elapsed_ms, config.distance_meters)}</p>
            <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">
              {new Date(effort.achieved_at).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })}
            </p>
          </>
        ) : (
          <p className="text-sm text-slate-400 dark:text-slate-500">No PR recorded yet</p>
        )}
      </div>
    </WidgetShell>
  );
}

function StreakWidget({ onConfig, onDelete }) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    api.stats.streaks()
      .then(setData).catch(e => setError(e.message)).finally(() => setLoading(false));
  }, []);

  return (
    <WidgetShell title="Streaks" onConfig={onConfig} onDelete={onDelete} loading={loading} error={error}>
      <div className="h-full grid grid-cols-2 gap-3 content-center">
        {[
          { label: "Current days",  value: data?.current_days  ?? 0 },
          { label: "Longest days",  value: data?.longest_days  ?? 0 },
          { label: "Current weeks", value: data?.current_weeks ?? 0 },
          { label: "Longest weeks", value: data?.longest_weeks ?? 0 },
        ].map(({ label, value }) => (
          <div key={label}>
            <p className="text-2xl font-bold text-slate-800 dark:text-slate-100">{value}</p>
            <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">{label}</p>
          </div>
        ))}
      </div>
    </WidgetShell>
  );
}

function PRListWidget({ config, onConfig, onDelete }) {
  const [groups, setGroups] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    api.records.list(config.sport_type)
      .then(g => setGroups(g ?? [])).catch(e => setError(e.message)).finally(() => setLoading(false));
  }, [config.sport_type]);

  const distances = config.distances ?? [5000, 10000, 21097, 42195];
  const rows = distances.map(dm => {
    const g = groups.find(g => g.distance_meters === dm);
    const best = g?.efforts?.[0] ?? null;
    return { label: DIST_OPTS.find(d => d.v === dm)?.l ?? `${dm} m`, best };
  });

  return (
    <WidgetShell title={`PRs · ${config.sport_type || "all"}`} onConfig={onConfig} onDelete={onDelete} loading={loading} error={error}>
      <div className="h-full overflow-y-auto space-y-1.5 py-1">
        {rows.map(({ label, best }) => (
          <div key={label} className="flex items-center justify-between text-sm py-0.5">
            <span className="text-slate-500 dark:text-slate-400 text-xs">{label}</span>
            <span className="font-semibold text-slate-800 dark:text-slate-100">
              {best ? fmtMs(best.elapsed_ms) : <span className="text-slate-300 dark:text-slate-600 font-normal">—</span>}
            </span>
          </div>
        ))}
      </div>
    </WidgetShell>
  );
}

function ConsistencyWidget({ config, onConfig, onDelete }) {
  const [dayCounts, setDayCounts] = useState({});
  const [dayDists, setDayDists]   = useState({});
  const [loading, setLoading]     = useState(true);
  const [error, setError]         = useState(null);

  const numWeeks = config.weeks ?? 52;
  const metric   = config.metric ?? "count";

  useEffect(() => {
    setLoading(true);
    setError(null);
    const d = new Date(); d.setDate(d.getDate() - numWeeks * 7 - 7);
    const fromStr = `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,"0")}-${String(d.getDate()).padStart(2,"0")}`;
    api.stats.timeseries({ granularity: "day", from: fromStr, sport_type: config.sport_type || undefined })
      .then(rows => {
        const counts = {}, dists = {};
        for (const r of (rows ?? [])) {
          const day = r.period_start?.slice(0, 10);
          if (day && (r.activity_count ?? 0) > 0) {
            counts[day] = r.activity_count;
            dists[day]  = r.distance_meters ?? 0;
          }
        }
        setDayCounts(counts);
        setDayDists(dists);
      })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, [config.sport_type, config.weeks]);

  const grid = (() => {
    const today = new Date();
    const dayOfWeek = (today.getDay() + 6) % 7; // Monday=0
    const start = new Date(today);
    start.setDate(today.getDate() - dayOfWeek - (numWeeks - 1) * 7);
    const weeks = [];
    for (let w = 0; w < numWeeks; w++) {
      const days = [];
      for (let d = 0; d < 7; d++) {
        const date = new Date(start);
        date.setDate(start.getDate() + w * 7 + d);
        const key = `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,"0")}-${String(date.getDate()).padStart(2,"0")}`;
        days.push({ key, count: dayCounts[key] ?? 0, dist: dayDists[key] ?? 0, future: date > today });
      }
      weeks.push(days);
    }
    return weeks;
  })();

  function cellColor({ count, dist, future }) {
    if (future) return "bg-transparent";
    if (metric === "distance") {
      const km = dist / 1000;
      if (km === 0)  return "bg-slate-100 dark:bg-slate-800";
      if (km < 5)   return "bg-blue-200 dark:bg-blue-900";
      if (km < 15)  return "bg-blue-300 dark:bg-blue-700";
      if (km < 30)  return "bg-blue-400 dark:bg-blue-600";
      return "bg-blue-500 dark:bg-blue-500";
    }
    if (count === 0) return "bg-slate-100 dark:bg-slate-800";
    if (count === 1) return "bg-blue-200 dark:bg-blue-900";
    if (count === 2) return "bg-blue-300 dark:bg-blue-700";
    if (count === 3) return "bg-blue-400 dark:bg-blue-600";
    return "bg-blue-500 dark:bg-blue-500";
  }

  function cellTitle({ key, count, dist, future }) {
    if (future) return "";
    if (metric === "distance") return `${key}: ${(dist / 1000).toFixed(1)} km`;
    return `${key}: ${count} activit${count === 1 ? "y" : "ies"}`;
  }

  return (
    <WidgetShell title={`Consistency — ${numWeeks}w · ${metric === "distance" ? "km" : "count"}`} onConfig={onConfig} onDelete={onDelete} loading={loading} error={error}>
      <div className="h-full flex items-center overflow-x-auto">
        <div className="flex gap-0.5 min-w-max">
          {grid.map((week, wi) => (
            <div key={wi} className="flex flex-col gap-0.5">
              {week.map((cell) => (
                <div
                  key={cell.key}
                  title={cellTitle(cell)}
                  className={`w-2.5 h-2.5 rounded-sm ${cellColor(cell)}`}
                />
              ))}
            </div>
          ))}
        </div>
      </div>
    </WidgetShell>
  );
}

function TrainingLoadWidget({ config, onConfig, onDelete }) {
  const ct = useChartTheme();
  const [chartData, setChartData] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    const from = (() => { const d = new Date(); d.setDate(d.getDate() - 365); return d.toISOString().slice(0, 10); })();
    api.stats.timeseries({ granularity: "week", from, sport_type: config.sport_type })
      .then(rows => {
        const km = (rows ?? []).map(r => (r.distance_meters ?? 0) / 1000);
        const points = (rows ?? []).map((r, i) => {
          const slice7  = km.slice(Math.max(0, i - 0), i + 1);
          const slice28 = km.slice(Math.max(0, i - 3), i + 1);
          const avg = arr => arr.reduce((s, v) => s + v, 0) / (arr.length || 1);
          return {
            label: new Date(r.period_start).toLocaleDateString(undefined, { month: "short", day: "numeric" }),
            acute:   parseFloat(avg(slice7).toFixed(1)),
            chronic: parseFloat(avg(slice28).toFixed(1)),
          };
        });
        setChartData(points.slice(-26));
      })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, [config.sport_type]);

  return (
    <WidgetShell title="Training load" onConfig={onConfig} onDelete={onDelete} loading={loading} error={error}>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={chartData} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
          <defs>
            <linearGradient id="acuteGrad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%"  stopColor="var(--accent)" stopOpacity={0.3} />
              <stop offset="95%" stopColor="var(--accent)" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="chronicGrad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%"  stopColor="#f97316" stopOpacity={0.2} />
              <stop offset="95%" stopColor="#f97316" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke={ct.gridStroke} vertical={false} />
          <XAxis dataKey="label" tick={{ fontSize: 9, fill: ct.tickFill }} tickLine={false} axisLine={false} interval="preserveStartEnd" />
          <YAxis tick={{ fontSize: 10, fill: ct.tickFill }} tickLine={false} axisLine={false} width={36} unit=" km" />
          <Tooltip contentStyle={ct.tooltipStyle} formatter={(v, n) => [`${v} km`, n === "acute" ? "7-day avg" : "28-day avg"]} />
          <Area type="monotone" dataKey="chronic" stroke="#f97316" strokeWidth={1.5} fill="url(#chronicGrad)" dot={false} />
          <Area type="monotone" dataKey="acute"   stroke="var(--accent)" strokeWidth={2}   fill="url(#acuteGrad)"   dot={false} />
        </AreaChart>
      </ResponsiveContainer>
    </WidgetShell>
  );
}

const WIDGET_COMPONENTS = {
  stat_card:      StatCardWidget,
  bar_chart:      BarChartWidget,
  line_chart:     LineChartWidget,
  donut:          DonutWidget,
  activity_feed:  ActivityFeedWidget,
  eddington:      EddingtonWidget,
  pr_card:        PRCardWidget,
  streak:         StreakWidget,
  pr_list:        PRListWidget,
  consistency:    ConsistencyWidget,
  training_load:  TrainingLoadWidget,
};

// ─── Widget config drawer ─────────────────────────────────────────────────────

function ConfigField({ label, children }) {
  return (
    <div>
      <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1">{label}</label>
      {children}
    </div>
  );
}

function ConfigSelect({ value, onChange, options }) {
  return (
    <select
      value={value}
      onChange={e => onChange(e.target.value)}
      className="w-full text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-1.5 text-slate-700 dark:text-slate-200 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
    >
      {options.map(o => <option key={o.v ?? o.value} value={o.v ?? o.value}>{o.l ?? o.label}</option>)}
    </select>
  );
}

function WidgetConfigDrawer({ widget, onSave, onClose }) {
  const [cfg, setCfg] = useState({ ...(widget?.config ?? {}) });

  useEffect(() => { setCfg({ ...(widget?.config ?? {}) }); }, [widget]);

  if (!widget) return null;

  const set = (k, v) => setCfg(prev => ({ ...prev, [k]: v }));

  return (
    <div className="fixed inset-0 z-40 flex justify-end" onClick={onClose}>
      <div className="absolute inset-0 bg-black/20" />
      <div
        className="relative w-80 bg-white dark:bg-slate-900 h-full shadow-xl flex flex-col"
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-5 py-4 border-b border-slate-100 dark:border-slate-800">
          <h3 className="font-semibold text-slate-800 dark:text-slate-100">Configure widget</h3>
          <button onClick={onClose} className="p-1 text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 rounded">
            <X size={16} />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-4">
          {/* Metric (stat_card, bar_chart, line_chart) */}
          {["stat_card", "bar_chart", "line_chart"].includes(widget.type) && (
            <ConfigField label="Metric">
              <ConfigSelect value={cfg.metric} onChange={v => set("metric", v)} options={METRIC_OPTS.map(o => ({ v: o.v, l: o.l }))} />
            </ConfigField>
          )}

          {/* Period (stat_card, donut) */}
          {["stat_card", "donut"].includes(widget.type) && (
            <ConfigField label="Period">
              <ConfigSelect value={cfg.period} onChange={v => set("period", v)} options={PERIOD_OPTS.map(o => ({ v: o.v, l: o.l }))} />
            </ConfigField>
          )}

          {/* Granularity (bar_chart, line_chart) */}
          {["bar_chart", "line_chart"].includes(widget.type) && (
            <ConfigField label="Granularity">
              <ConfigSelect value={cfg.granularity} onChange={v => set("granularity", v)} options={GRAN_OPTS.map(o => ({ v: o.v, l: o.l }))} />
            </ConfigField>
          )}

          {/* Sport type */}
          {["stat_card", "bar_chart", "line_chart", "activity_feed", "eddington", "pr_card", "pr_list", "consistency", "training_load"].includes(widget.type) && (
            <ConfigField label="Sport">
              <ConfigSelect value={cfg.sport_type ?? ""} onChange={v => set("sport_type", v)} options={SPORT_OPTS.map(o => ({ v: o.v, l: o.l }))} />
            </ConfigField>
          )}

          {/* Activity count (activity_feed) */}
          {widget.type === "activity_feed" && (
            <ConfigField label="Count">
              <ConfigSelect
                value={cfg.count ?? 5}
                onChange={v => set("count", Number(v))}
                options={[5, 10, 15, 20].map(n => ({ v: n, l: `${n} activities` }))}
              />
            </ConfigField>
          )}

          {/* Consistency: range + metric */}
          {widget.type === "consistency" && (
            <>
              <ConfigField label="Range">
                <ConfigSelect
                  value={cfg.weeks ?? 52}
                  onChange={v => set("weeks", Number(v))}
                  options={[13, 26, 52].map(n => ({ v: n, l: `${n} weeks` }))}
                />
              </ConfigField>
              <ConfigField label="Metric">
                <ConfigSelect
                  value={cfg.metric ?? "count"}
                  onChange={v => set("metric", v)}
                  options={[{ v: "count", l: "Activity count" }, { v: "distance", l: "Distance (km)" }]}
                />
              </ConfigField>
            </>
          )}

          {/* Distance (pr_card) */}
          {widget.type === "pr_card" && (
            <ConfigField label="Distance">
              <ConfigSelect
                value={cfg.distance_meters}
                onChange={v => set("distance_meters", Number(v))}
                options={DIST_OPTS.map(o => ({ v: o.v, l: o.l }))}
              />
            </ConfigField>
          )}
        </div>

        <div className="px-5 py-4 border-t border-slate-100 dark:border-slate-800">
          <button
            onClick={() => onSave(cfg)}
            className="w-full py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors"
          >
            Save
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Widget type picker ───────────────────────────────────────────────────────

function WidgetPicker({ onSelect, onClose }) {
  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center" onClick={onClose}>
      <div className="absolute inset-0 bg-black/20" />
      <div
        className="relative bg-white dark:bg-slate-900 rounded-2xl shadow-xl w-full max-w-md p-6"
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h3 className="font-semibold text-slate-800 dark:text-slate-100">Add widget</h3>
          <button onClick={onClose} className="p-1 text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 rounded"><X size={16} /></button>
        </div>
        <div className="overflow-y-auto max-h-[65vh] -mx-1 px-1">
          <p className="text-[10px] font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-widest mb-2">Charts &amp; Stats</p>
          <div className="grid grid-cols-2 gap-2 mb-4">
            {WIDGET_TYPES.slice(0, 7).map(wt => (
              <button
                key={wt.type}
                onClick={() => onSelect(wt.type)}
                className="card-hover flex flex-col items-start gap-1.5 p-3 rounded-xl border border-slate-200 dark:border-slate-700 hover:border-blue-300 hover:bg-blue-50 dark:hover:bg-blue-900/20 transition-colors text-left"
              >
                <wt.icon size={18} className="text-blue-500" />
                <span className="text-sm font-medium text-slate-700 dark:text-slate-200">{wt.label}</span>
                <span className="text-xs text-slate-400 dark:text-slate-500">{wt.desc}</span>
              </button>
            ))}
          </div>
          <p className="text-[10px] font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-widest mb-2">Analytics</p>
          <div className="grid grid-cols-2 gap-2">
            {WIDGET_TYPES.slice(7).map(wt => (
              <button
                key={wt.type}
                onClick={() => onSelect(wt.type)}
                className="card-hover flex flex-col items-start gap-1.5 p-3 rounded-xl border border-slate-200 dark:border-slate-700 hover:border-blue-300 hover:bg-blue-50 dark:hover:bg-blue-900/20 transition-colors text-left"
              >
                <wt.icon size={18} className="text-blue-500" />
                <span className="text-sm font-medium text-slate-700 dark:text-slate-200">{wt.label}</span>
                <span className="text-xs text-slate-400 dark:text-slate-500">{wt.desc}</span>
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Dashboard detail view ────────────────────────────────────────────────────

function DashboardView({ dashboardId }) {
  const [dashboard, setDashboard] = useState(null);
  const [widgets, setWidgets] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showPicker, setShowPicker] = useState(false);
  const [configWidget, setConfigWidget] = useState(null);
  const saveTimer = useRef(null);
  const [defaultDashId, setDefaultDashId] = useDefaultDashboardId();
  const isHome = defaultDashId === dashboardId;

  useEffect(() => {
    setLoading(true);
    Promise.all([
      api.dashboards.get(dashboardId),
      api.widgets.list(dashboardId),
    ]).then(([d, ws]) => {
      setDashboard(d);
      setWidgets(ws ?? []);
    }).finally(() => setLoading(false));
  }, [dashboardId]);

  const rglLayout = widgets.map(w => ({
    i: w.id,
    x: w.position?.x ?? 0,
    y: w.position?.y ?? 0,
    w: w.position?.w ?? 4,
    h: w.position?.h ?? 3,
    minW: 2, minH: 2,
  }));

  function onLayoutChange(layout) {
    // Debounce saves — persist on drag/resize end
    clearTimeout(saveTimer.current);
    saveTimer.current = setTimeout(() => {
      layout.forEach(l => {
        const widget = widgets.find(w => w.id === l.i);
        if (!widget) return;
        const pos = { x: l.x, y: l.y, w: l.w, h: l.h };
        const prevPos = widget.position;
        if (pos.x !== prevPos?.x || pos.y !== prevPos?.y || pos.w !== prevPos?.w || pos.h !== prevPos?.h) {
          api.widgets.update(dashboardId, l.i, { position: pos });
          setWidgets(prev => prev.map(w => w.id === l.i ? { ...w, position: pos } : w));
        }
      });
    }, 500);
  }

  async function addWidget(type) {
    setShowPicker(false);
    const cfg = DEFAULT_CONFIG[type] ?? {};
    // Find a free Y position
    const maxY = widgets.reduce((m, w) => Math.max(m, (w.position?.y ?? 0) + (w.position?.h ?? 3)), 0);
    const pos = { x: 0, y: maxY, w: 4, h: 3 };
    const w = await api.widgets.create(dashboardId, type, cfg, pos);
    setWidgets(prev => [...prev, w]);
  }

  async function deleteWidget(widgetId) {
    await api.widgets.remove(dashboardId, widgetId);
    setWidgets(prev => prev.filter(w => w.id !== widgetId));
  }

  async function saveWidgetConfig(cfg) {
    const w = await api.widgets.update(dashboardId, configWidget.id, { config: cfg });
    setWidgets(prev => prev.map(x => x.id === w.id ? w : x));
    setConfigWidget(null);
  }

  if (loading) return <div className="text-sm text-slate-400 dark:text-slate-500 animate-pulse">Loading dashboard…</div>;

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-3">
        <Link to="/dashboards" className="text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300">
          <ChevronLeft size={20} />
        </Link>
        <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100 flex-1">{dashboard?.name}</h1>
        <button
          onClick={() => setDefaultDashId(isHome ? "" : dashboardId)}
          className={`flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium rounded-lg transition-colors border ${
            isHome
              ? "border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400"
              : "border-slate-200 dark:border-slate-700 hover:border-slate-300 dark:hover:border-slate-600 text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200"
          }`}
        >
          <Home size={14} /> {isHome ? "Home ✓" : "Set as home"}
        </button>
        <button
          onClick={() => setShowPicker(true)}
          className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors"
        >
          <Plus size={15} /> Add widget
        </button>
      </div>

      {widgets.length === 0 ? (
        <div className="rounded-xl border-2 border-dashed border-slate-200 dark:border-slate-700 p-12 text-center">
          <BarChart3 size={32} className="mx-auto text-slate-300 dark:text-slate-600 mb-3" />
          <p className="text-sm font-medium text-slate-500 dark:text-slate-400">No widgets yet</p>
          <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">Click "Add widget" to start building your dashboard.</p>
        </div>
      ) : (
        <RGL
          layout={rglLayout}
          cols={12}
          rowHeight={80}
          onLayoutChange={onLayoutChange}
          draggableHandle=".drag-handle"
          draggableCancel=".no-drag"
          className="min-h-64"
        >
          {widgets.map(w => {
            const Cmp = WIDGET_COMPONENTS[w.type];
            return (
              <div key={w.id}>
                {Cmp ? (
                  <Cmp
                    config={w.config ?? {}}
                    onConfig={() => setConfigWidget(w)}
                    onDelete={() => deleteWidget(w.id)}
                  />
                ) : (
                  <WidgetShell title={w.type} onDelete={() => deleteWidget(w.id)}>
                    <p className="text-xs text-slate-400 dark:text-slate-500">Unknown widget type</p>
                  </WidgetShell>
                )}
              </div>
            );
          })}
        </RGL>
      )}

      {showPicker && <WidgetPicker onSelect={addWidget} onClose={() => setShowPicker(false)} />}
      {configWidget && (
        <WidgetConfigDrawer
          widget={configWidget}
          onSave={saveWidgetConfig}
          onClose={() => setConfigWidget(null)}
        />
      )}
    </div>
  );
}

// ─── Dashboard list view ──────────────────────────────────────────────────────

function DashboardList() {
  const navigate = useNavigate();
  const [dashboards, setDashboards] = useState([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");
  const [editingId, setEditingId] = useState(null);
  const [editName, setEditName] = useState("");

  useEffect(() => {
    api.dashboards.list().then(d => setDashboards(d ?? [])).finally(() => setLoading(false));
  }, []);

  async function create() {
    if (!newName.trim()) return;
    const d = await api.dashboards.create(newName.trim());
    setDashboards(prev => [...prev, d]);
    setCreating(false);
    setNewName("");
    navigate(`/dashboards/${d.id}`);
  }

  async function rename(id) {
    if (!editName.trim()) return;
    const d = await api.dashboards.update(id, editName.trim());
    setDashboards(prev => prev.map(x => x.id === id ? d : x));
    setEditingId(null);
  }

  async function remove(id) {
    await api.dashboards.remove(id);
    setDashboards(prev => prev.filter(d => d.id !== id));
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Dashboards</h1>
          <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">Custom widget dashboards</p>
        </div>
        <button
          onClick={() => setCreating(true)}
          className="flex items-center gap-1.5 px-3 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors"
        >
          <Plus size={15} /> New dashboard
        </button>
      </div>

      {creating && (
        <div className="flex items-center gap-2 p-4 bg-blue-50 rounded-xl border border-blue-200">
          <input
            autoFocus
            value={newName}
            onChange={e => setNewName(e.target.value)}
            onKeyDown={e => { if (e.key === "Enter") create(); if (e.key === "Escape") { setCreating(false); setNewName(""); } }}
            placeholder="Dashboard name…"
            className="flex-1 text-sm border border-blue-300 rounded-lg px-3 py-1.5 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <button onClick={create} className="p-1.5 text-blue-600 hover:text-blue-800"><Check size={16} /></button>
          <button onClick={() => { setCreating(false); setNewName(""); }} className="p-1.5 text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300"><X size={16} /></button>
        </div>
      )}

      {loading && <div className="text-sm text-slate-400 dark:text-slate-500 animate-pulse">Loading…</div>}

      {!loading && dashboards.length === 0 && !creating && (
        <div className="rounded-xl border-2 border-dashed border-slate-200 dark:border-slate-700 p-12 text-center">
          <BarChart3 size={32} className="mx-auto text-slate-300 dark:text-slate-600 mb-3" />
          <p className="text-sm font-medium text-slate-500 dark:text-slate-400">No dashboards yet</p>
          <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">Create a dashboard to start adding widgets.</p>
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        {dashboards.map(d => (
          <div key={d.id} className="card-hover group bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5 flex items-start justify-between gap-3">
            {editingId === d.id ? (
              <div className="flex items-center gap-2 flex-1">
                <input
                  autoFocus
                  value={editName}
                  onChange={e => setEditName(e.target.value)}
                  onKeyDown={e => { if (e.key === "Enter") rename(d.id); if (e.key === "Escape") setEditingId(null); }}
                  className="flex-1 text-sm border border-slate-300 dark:border-slate-600 rounded-lg px-2 py-1 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
                <button onClick={() => rename(d.id)} className="p-1 text-blue-600"><Check size={14} /></button>
                <button onClick={() => setEditingId(null)} className="p-1 text-slate-400 dark:text-slate-500"><X size={14} /></button>
              </div>
            ) : (
              <Link to={`/dashboards/${d.id}`} className="flex-1 min-w-0">
                <p className="font-semibold text-slate-800 dark:text-slate-100 truncate">{d.name}</p>
                <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">{d.widget_count} {d.widget_count === 1 ? "widget" : "widgets"}</p>
              </Link>
            )}
            {editingId !== d.id && (
              <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
                <button onClick={() => { setEditingId(d.id); setEditName(d.name); }} className="p-1.5 text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800">
                  <Pencil size={13} />
                </button>
                <button onClick={() => remove(d.id)} className="p-1.5 text-slate-400 dark:text-slate-500 hover:text-red-500 rounded-lg hover:bg-red-50">
                  <Trash2 size={13} />
                </button>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

// ─── Root export ──────────────────────────────────────────────────────────────

export default function Dashboards() {
  const { id } = useParams();
  return id ? <DashboardView dashboardId={id} /> : <DashboardList />;
}
