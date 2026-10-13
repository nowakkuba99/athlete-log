import { useState, useEffect } from "react";
import { Link } from "react-router-dom";
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell,
} from "recharts";
import { ChevronLeft, ChevronRight, ArrowRight } from "lucide-react";
import { api } from "../api/client";
import { useUnits, useChartTheme } from "../settings";
import { fmtDist as uFmtDist, fmtDistShort as uFmtDistShort, fmtEle as uFmtEle } from "../units";

// ─── Formatters ───────────────────────────────────────────────────────────────

function fmtDist(m, imp = false)    { return uFmtDist(m, imp); }
function fmtDistDec(m, imp = false) { return uFmtDistShort(m, imp); }

function fmtTime(ms) {
  if (!ms) return "—";
  const h = Math.floor(ms / 3600000);
  const min = Math.floor((ms % 3600000) / 60000);
  return h > 0 ? `${h}h ${min}m` : `${min}m`;
}

const MONTHS = ["Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sep","Oct","Nov","Dec"];

const SPORT_COLOR = {
  run:  "#f97316", ride: "#3b82f6", walk: "#10b981",
  hike: "#a16207", swim: "#06b6d4",
};

function sportColor(type) {
  return SPORT_COLOR[type?.toLowerCase()] ?? "#94a3b8";
}

// ─── Sub-components ───────────────────────────────────────────────────────────

function StatCard({ label, value, sub }) {
  return (
    <div className="card-hover bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 px-6 py-5 flex flex-col gap-1">
      <p className="text-xs text-slate-400 dark:text-slate-500 font-medium">{label}</p>
      <p className="text-3xl font-bold text-slate-800 dark:text-slate-100 leading-none">{value}</p>
      {sub && <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">{sub}</p>}
    </div>
  );
}

function MonthChart({ byMonth }) {
  const ct = useChartTheme();
  if (!byMonth?.length) return null;

  // Fill all 12 months (missing = 0)
  const data = MONTHS.map((name, i) => {
    const m = byMonth.find((b) => b.month === i + 1);
    return { name, km: m ? +(m.distance_meters / 1000).toFixed(1) : 0, count: m?.activity_count ?? 0 };
  });

  const peak = Math.max(...data.map((d) => d.km));

  return (
    <div className="card-hover bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 p-6">
      <p className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-4">Distance by month</p>
      <ResponsiveContainer width="100%" height={180}>
        <BarChart data={data} margin={{ top: 4, right: 0, left: -20, bottom: 0 }}>
          <XAxis dataKey="name" tick={{ fontSize: 11, fill: ct.tickFill }} axisLine={false} tickLine={false} />
          <YAxis tick={{ fontSize: 11, fill: ct.tickFill }} axisLine={false} tickLine={false} unit=" km" />
          <Tooltip
            formatter={(v, _, { payload }) => [`${v} km · ${payload.count} activities`]}
            contentStyle={ct.tooltipStyle}
            cursor={{ fill: ct.cursorFill }}
          />
          <Bar dataKey="km" radius={[4, 4, 0, 0]}>
            {data.map((d, i) => (
              <Cell key={i} fill={d.km === peak ? "#3b82f6" : ct.gridStroke} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}

function SportBreakdown({ bySport, imp = false }) {
  if (!bySport?.length) return null;
  const total = bySport.reduce((s, b) => s + b.distance_meters, 0);
  return (
    <div className="card-hover bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 p-6">
      <p className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-4">By sport</p>
      <div className="space-y-3">
        {bySport.map((s) => {
          const pct = total > 0 ? (s.distance_meters / total) * 100 : 0;
          const color = sportColor(s.sport_type);
          return (
            <div key={s.sport_type}>
              <div className="flex justify-between text-xs mb-1">
                <span className="capitalize text-slate-600 dark:text-slate-300 font-medium">{s.sport_type}</span>
                <span className="text-slate-400 dark:text-slate-500">{fmtDist(s.distance_meters, imp)} · {s.activity_count} activities</span>
              </div>
              <div className="h-2 bg-slate-100 dark:bg-slate-800 rounded-full overflow-hidden">
                <div className="h-full rounded-full" style={{ width: `${pct}%`, background: color }} />
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ─── Main page ────────────────────────────────────────────────────────────────

export default function Review() {
  const [units] = useUnits();
  const imp = units === "imperial";
  const currentYear = new Date().getFullYear();
  const defaultYear = currentYear;

  const [year,    setYear]    = useState(defaultYear);
  const [data,    setData]    = useState(null);
  const [loading, setLoading] = useState(true);
  const [error,   setError]   = useState(null);

  useEffect(() => {
    setLoading(true);
    setError(null);
    api.stats.yearReview(year)
      .then(setData)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [year]);

  const empty = !loading && data && data.total_activities === 0;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Year in Review</h1>
          <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">Your training year, wrapped up.</p>
        </div>
        {/* Year picker */}
        <div className="flex items-center gap-1 shrink-0">
          <button
            onClick={() => setYear((y) => y - 1)}
            disabled={year <= 2010}
            className="p-2 rounded-lg border border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30"
          >
            <ChevronLeft size={16} />
          </button>
          <span className="w-16 text-center text-sm font-semibold text-slate-700 dark:text-slate-200">{year}</span>
          <button
            onClick={() => setYear((y) => y + 1)}
            disabled={year >= currentYear}
            className="p-2 rounded-lg border border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30"
          >
            <ChevronRight size={16} />
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-xl border border-red-200 dark:border-red-800/50 bg-red-50 dark:bg-red-900/20 p-4 text-sm text-red-700 dark:text-red-400">{error}</div>
      )}

      {loading && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 h-28 animate-pulse" />
          ))}
        </div>
      )}

      {empty && (
        <div className="flex items-center justify-center h-40 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-950 text-sm text-slate-400 dark:text-slate-500">
          No activities recorded in {year}.
        </div>
      )}

      {!loading && data && !empty && (
        <>
          {/* Hero — total distance */}
          <div className="bg-gradient-to-br from-blue-600 to-blue-700 rounded-2xl p-8 text-white">
            <p className="text-blue-200 text-sm font-medium mb-1">Total distance in {year}</p>
            <p className="text-6xl font-black tracking-tight leading-none">
              {fmtDist(data.total_distance_meters, imp)}
            </p>
            <p className="text-blue-200 text-sm mt-3">
              {data.total_activities} activities · {fmtTime(data.total_elapsed_ms)} moving
            </p>
          </div>

          {/* Stat grid */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <StatCard label="Activities"    value={data.total_activities} />
            <StatCard label="Moving time"   value={fmtTime(data.total_elapsed_ms)} />
            <StatCard label="Elevation ↑"   value={data.total_elevation_meters > 0 ? uFmtEle(data.total_elevation_meters, imp) : "—"} />
            <StatCard
              label="PRs set"
              value={data.prs_set > 0 ? data.prs_set : "—"}
              sub={data.prs_set > 0 ? "personal records" : "no new PRs"}
            />
          </div>

          {/* Month chart */}
          <MonthChart byMonth={data.by_month} />

          {/* Sport breakdown + highlights row */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <SportBreakdown bySport={data.by_sport} imp={imp} />

            {/* Highlight cards */}
            <div className="space-y-4">
              {/* Biggest activity */}
              {data.biggest_activity && (
                <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 p-5">
                  <p className="text-xs text-slate-400 dark:text-slate-500 font-medium mb-2">Biggest activity</p>
                  <p className="text-2xl font-bold text-slate-800 dark:text-slate-100">{fmtDistDec(data.biggest_activity.distance_meters, imp)}</p>
                  <p className="text-sm text-slate-600 dark:text-slate-300 mt-1 truncate">
                    {data.biggest_activity.name || "Activity"}
                  </p>
                  <p className="text-xs text-slate-400 dark:text-slate-500">
                    {new Date(data.biggest_activity.start_at).toLocaleDateString("en-GB", {
                      day: "numeric", month: "long",
                    })}
                  </p>
                  <Link
                    to={`/activities/${data.biggest_activity.id}`}
                    className="mt-3 inline-flex items-center gap-1 text-xs text-blue-500 hover:text-blue-700"
                  >
                    View activity <ArrowRight size={11} />
                  </Link>
                </div>
              )}

              {/* Most used gear */}
              {data.most_used_gear && (
                <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 p-5">
                  <p className="text-xs text-slate-400 dark:text-slate-500 font-medium mb-2">Most used gear</p>
                  <p className="text-xl font-bold text-slate-800 dark:text-slate-100 truncate">{data.most_used_gear.name}</p>
                  <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">
                    {data.most_used_gear.activity_count} activities · {fmtDist(data.most_used_gear.distance_meters, imp)}
                  </p>
                  <Link
                    to="/gear"
                    className="mt-3 inline-flex items-center gap-1 text-xs text-blue-500 hover:text-blue-700"
                  >
                    View gear <ArrowRight size={11} />
                  </Link>
                </div>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
