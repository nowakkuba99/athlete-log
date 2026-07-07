import { useState, useEffect } from "react";
import { useUnits, useChartTheme } from "../settings";
import { fmtDistShort } from "../units";
import { Link } from "react-router-dom";
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer,
  PieChart, Pie, Cell,
} from "recharts";
import { api } from "../api/client";

// ─── date helpers ───────────────────────────────────────────────────────────

function toDateStr(d) {
  return `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,"0")}-${String(d.getDate()).padStart(2,"0")}`;
}

function todayStr() {
  return toDateStr(new Date());
}

function weekFrom() {
  const now = new Date();
  const day = now.getDay();
  const diff = day === 0 ? -6 : 1 - day; // back to Monday
  const monday = new Date(now);
  monday.setDate(now.getDate() + diff);
  monday.setHours(0, 0, 0, 0);
  return toDateStr(monday);
}

function monthFrom() {
  const now = new Date();
  return toDateStr(new Date(now.getFullYear(), now.getMonth(), 1));
}

function yearFrom() {
  return toDateStr(new Date(new Date().getFullYear(), 0, 1));
}

function twelveWeeksAgo() {
  const d = new Date();
  d.setDate(d.getDate() - 84);
  return toDateStr(d);
}

// ─── formatters ─────────────────────────────────────────────────────────────

function fmtDist(m, imp = false) { return fmtDistShort(m ?? 0, imp); }

function fmtDuration(ms) {
  if (!ms) return "0m";
  const min = Math.floor(ms / 60000);
  const h = Math.floor(min / 60);
  const m = min % 60;
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}

function fmtShortDate(iso) {
  return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

function fmtWeekLabel(iso) {
  return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

// ─── constants ───────────────────────────────────────────────────────────────

const SPORT_COLOR = {
  run: "#3b82f6",
  ride: "#f59e0b",
  swim: "#06b6d4",
  walk: "#10b981",
  hike: "#84cc16",
  unknown: "#94a3b8",
};

const SPORT_EMOJI = {
  run: "🏃",
  ride: "🚴",
  swim: "🏊",
  walk: "🚶",
  hike: "🥾",
  unknown: "⚡",
};

// ─── small components ────────────────────────────────────────────────────────

function Skeleton({ className = "" }) {
  return <div className={`bg-slate-200 dark:bg-slate-700 rounded-lg animate-pulse ${className}`} />;
}

function KpiCard({ label, stats, loading, imp = false }) {
  return (
    <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
      <p className="text-xs font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-widest mb-3">
        {label}
      </p>
      {loading ? (
        <div className="space-y-2">
          <Skeleton className="h-7 w-28" />
          <Skeleton className="h-4 w-36" />
        </div>
      ) : (
        <>
          <p className="text-2xl font-bold text-slate-800 dark:text-slate-100">
            {fmtDist(stats?.total_distance_meters, imp)}
          </p>
          <p className="text-sm text-slate-400 dark:text-slate-500 mt-1">
            {stats?.total_activities ?? 0}{" "}
            {stats?.total_activities === 1 ? "activity" : "activities"} ·{" "}
            {fmtDuration(stats?.total_elapsed_ms)}
            {stats?.total_calories > 0 && (
              <> · {stats.total_calories.toLocaleString()} kcal</>
            )}
          </p>
        </>
      )}
    </div>
  );
}

// ─── main page ───────────────────────────────────────────────────────────────

export default function Home() {
  const [units] = useUnits();
  const imp = units === "imperial";
  const ct = useChartTheme();
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const today = todayStr();

  useEffect(() => {
    Promise.all([
      api.stats.summary({ from: weekFrom(), to: today }),
      api.stats.summary({ from: monthFrom(), to: today }),
      api.stats.summary({ from: yearFrom(), to: today }),
      api.stats.timeseries({ granularity: "week", from: twelveWeeksAgo(), to: today }),
      api.activities.list({ limit: 5 }),
    ])
      .then(([week, month, year, ts, acts]) =>
        setData({ week, month, year, ts, recent: acts.activities ?? [] })
      )
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, []);

  if (error) {
    return (
      <div className="rounded-xl border border-red-200 dark:border-red-800/50 bg-red-50 dark:bg-red-900/20 p-4 text-sm text-red-700 dark:text-red-400">
        Could not load dashboard: {error}
      </div>
    );
  }

  const chartData = (data?.ts ?? []).map((p) => ({
    week: fmtWeekLabel(p.period_start),
    km: parseFloat((p.distance_meters / 1000).toFixed(1)),
  }));

  const bySport = data?.week?.by_sport ?? [];

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Home</h1>
        <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">Your training at a glance</p>
      </div>

      {/* KPI row */}
      <div className="grid grid-cols-3 gap-4">
        <KpiCard label="This week" stats={data?.week} loading={loading} imp={imp} />
        <KpiCard label="This month" stats={data?.month} loading={loading} imp={imp} />
        <KpiCard label="This year" stats={data?.year} loading={loading} imp={imp} />
      </div>

      {/* Chart row */}
      <div className="grid grid-cols-3 gap-4">
        {/* Weekly bar chart — 2/3 */}
        <div className="col-span-2 card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
          <p className="text-sm font-medium text-slate-600 dark:text-slate-300 mb-4">
            Weekly distance — last 12 weeks
          </p>
          {loading ? (
            <Skeleton className="h-40 w-full" />
          ) : chartData.length === 0 ? (
            <EmptyState className="h-40" text="No data yet" />
          ) : (
            <ResponsiveContainer width="100%" height={160}>
              <BarChart data={chartData} barCategoryGap="35%">
                <XAxis
                  dataKey="week"
                  tick={{ fontSize: 11, fill: ct.tickFill }}
                  tickLine={false}
                  axisLine={false}
                />
                <YAxis
                  tick={{ fontSize: 11, fill: ct.tickFill }}
                  tickLine={false}
                  axisLine={false}
                  unit=" km"
                  width={44}
                />
                <Tooltip
                  cursor={{ fill: ct.cursorFill }}
                  contentStyle={ct.tooltipStyle}
                  formatter={(v) => [`${v} km`, "Distance"]}
                />
                <Bar dataKey="km" fill="var(--accent)" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          )}
        </div>

        {/* Sport donut — 1/3 */}
        <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
          <p className="text-sm font-medium text-slate-600 dark:text-slate-300 mb-4">
            By sport — this week
          </p>
          {loading ? (
            <Skeleton className="h-40 w-full" />
          ) : bySport.length === 0 ? (
            <EmptyState className="h-40" text="No data" />
          ) : (
            <>
              <ResponsiveContainer width="100%" height={120}>
                <PieChart>
                  <Pie
                    data={bySport}
                    dataKey="activity_count"
                    nameKey="sport_type"
                    cx="50%"
                    cy="50%"
                    innerRadius={36}
                    outerRadius={54}
                    paddingAngle={3}
                  >
                    {bySport.map((entry) => (
                      <Cell
                        key={entry.sport_type}
                        fill={SPORT_COLOR[entry.sport_type] ?? SPORT_COLOR.unknown}
                      />
                    ))}
                  </Pie>
                  <Tooltip
                    contentStyle={ct.tooltipStyle}
                    formatter={(v, name) => [`${v} activities`, name]}
                  />
                </PieChart>
              </ResponsiveContainer>
              <ul className="mt-2 space-y-1">
                {bySport.map((s) => (
                  <li key={s.sport_type} className="flex items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
                    <span
                      className="w-2.5 h-2.5 rounded-full shrink-0"
                      style={{ background: SPORT_COLOR[s.sport_type] ?? SPORT_COLOR.unknown }}
                    />
                    <span className="capitalize">{s.sport_type}</span>
                    <span className="ml-auto text-slate-400 dark:text-slate-500">
                      {fmtDist(s.distance_meters, imp)}
                    </span>
                  </li>
                ))}
              </ul>
            </>
          )}
        </div>
      </div>

      {/* Recent activities */}
      <div className="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700">
        <div className="flex items-center justify-between px-5 py-4 border-b border-slate-100 dark:border-slate-800">
          <p className="text-sm font-medium text-slate-600 dark:text-slate-300">Recent activities</p>
          <Link to="/activities" className="text-xs text-blue-500 hover:text-blue-700 font-medium">
            View all →
          </Link>
        </div>

        {loading ? (
          <div className="p-5 space-y-3">
            {[0, 1, 2].map((i) => <Skeleton key={i} className="h-12 w-full" />)}
          </div>
        ) : !data?.recent.length ? (
          <div className="px-5 py-8 text-center">
            <p className="text-sm text-slate-500 dark:text-slate-400">No activities yet.</p>
            <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">
              <Link to="/sources" className="text-blue-500 hover:text-blue-700">
                Connect a source
              </Link>{" "}
              or{" "}
              <Link to="/upload" className="text-blue-500 hover:text-blue-700">
                upload files
              </Link>{" "}
              to get started.
            </p>
          </div>
        ) : (
          <ul>
            {data.recent.map((act, i) => (
              <li
                key={act.id}
                className={i < data.recent.length - 1 ? "border-b border-slate-100 dark:border-slate-800" : ""}
              >
                <Link
                  to={`/activities/${act.id}`}
                  className="flex items-center gap-4 px-5 py-3 hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors"
                >
                  <span className="text-xl w-7 text-center shrink-0">
                    {SPORT_EMOJI[act.sport_type] ?? "⚡"}
                  </span>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-slate-700 dark:text-slate-200 truncate">
                      {act.name || capitalize(act.sport_type)}
                    </p>
                    <p className="text-xs text-slate-400 dark:text-slate-500">{fmtShortDate(act.start_at)}</p>
                  </div>
                  <div className="text-right shrink-0">
                    <p className="text-sm font-medium text-slate-700 dark:text-slate-200">
                      {fmtDist(act.distance_meters, imp)}
                    </p>
                    {act.elapsed_ms && (
                      <p className="text-xs text-slate-400 dark:text-slate-500">{fmtDuration(act.elapsed_ms)}</p>
                    )}
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function EmptyState({ className = "", text = "No data" }) {
  return (
    <div className={`flex items-center justify-center text-sm text-slate-400 dark:text-slate-500 ${className}`}>
      {text}
    </div>
  );
}

function capitalize(s) {
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : "";
}
