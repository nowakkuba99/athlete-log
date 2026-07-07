import { useState, useEffect } from "react";
import { Link } from "react-router-dom";
import { Trophy, TrendingUp, Calendar, ArrowRight } from "lucide-react";
import { useChartTheme } from "../settings";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  CartesianGrid,
} from "recharts";
import { api } from "../api/client";

const SPORT_TABS = ["run", "ride", "walk", "hike"];

const DISTANCE_LABELS = {
  400:   "400 m",
  1000:  "1 km",
  1609:  "1 mile",
  5000:  "5 km",
  10000: "10 km",
  15000: "15 km",
  21097: "Half Marathon",
  42195: "Marathon",
};

function fmtMs(ms) {
  const s = Math.round(ms / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0) return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return `${m}:${String(sec).padStart(2, "0")}`;
}

function fmtPace(ms, distM) {
  const secPerKm = (ms / 1000) / (distM / 1000);
  let m = Math.floor(secPerKm / 60), s = Math.round(secPerKm % 60);
  if (s === 60) { m++; s = 0; }
  return `${m}:${String(s).padStart(2, "0")} /km`;
}

function fmtDate(iso) {
  return new Date(iso).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
}

function PRCard({ group, expanded, onToggle }) {
  const ct = useChartTheme();
  const label = DISTANCE_LABELS[group.distance_meters] ?? `${group.distance_meters} m`;
  const best = group.efforts[0];

  // Chart data: historical PRs sorted by date (ascending)
  const chartData = [...group.efforts]
    .sort((a, b) => new Date(a.achieved_at) - new Date(b.achieved_at))
    .map((e) => ({ date: fmtDate(e.achieved_at), ms: -e.elapsed_ms })); // negated so fastest (lowest ms) is at the top

  return (
    <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700">
      {/* Card header */}
      <button
        className="w-full text-left px-5 py-4 flex items-start justify-between gap-3 hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors"
        onClick={onToggle}
      >
        <div className="flex items-center gap-3">
          <div className="flex-shrink-0 w-9 h-9 rounded-lg bg-amber-50 flex items-center justify-center">
            <Trophy size={16} className="text-amber-500" />
          </div>
          <div>
            <p className="text-xs font-medium text-slate-400 dark:text-slate-500 uppercase tracking-wide">{label}</p>
            <p className="text-2xl font-bold text-slate-800 dark:text-slate-100 leading-none mt-0.5">{fmtMs(best.elapsed_ms)}</p>
          </div>
        </div>
        <div className="text-right shrink-0">
          <p className="text-sm font-medium text-slate-600 dark:text-slate-300">{fmtPace(best.elapsed_ms, group.distance_meters)}</p>
          <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5 flex items-center justify-end gap-1">
            <Calendar size={11} />
            {fmtDate(best.achieved_at)}
          </p>
        </div>
      </button>

      {/* Expanded: progression chart + effort list */}
      {expanded && (
        <div className="border-t border-slate-100 dark:border-slate-800 px-5 pb-5 pt-4 space-y-4">
          {group.efforts.length > 1 && (
            <div>
              <p className="text-xs font-medium text-slate-400 dark:text-slate-500 uppercase tracking-wide mb-2 flex items-center gap-1.5">
                <TrendingUp size={12} />
                Progression
              </p>
              <ResponsiveContainer width="100%" height={120}>
                <LineChart data={chartData} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke={ct.gridStroke} />
                  <XAxis dataKey="date" tick={{ fontSize: 10, fill: ct.tickFill }} tickLine={false} axisLine={false} />
                  <YAxis
                    tick={{ fontSize: 10, fill: ct.tickFill }}
                    tickLine={false}
                    axisLine={false}
                    tickFormatter={(v) => fmtMs(-v)}
                    domain={["auto", "auto"]}
                    width={45}
                  />
                  <Tooltip
                    formatter={(v) => [fmtMs(-v), "Time"]}
                    contentStyle={ct.tooltipStyle}
                  />
                  <Line
                    type="monotone"
                    dataKey="ms"
                    stroke="#f59e0b"
                    strokeWidth={2}
                    dot={{ r: 3, fill: "#f59e0b" }}
                    activeDot={{ r: 5 }}
                  />
                </LineChart>
              </ResponsiveContainer>
            </div>
          )}

          {/* Effort list */}
          <div className="space-y-1">
            {group.efforts.map((e, i) => (
              <div key={e.activity_id + e.elapsed_ms} className="flex items-center gap-3 py-1.5 text-sm">
                <span className="text-base leading-none w-6 text-center shrink-0">
                  {i === 0 ? "🥇" : i === 1 ? "🥈" : i === 2 ? "🥉" : <span className="text-[10px] text-slate-400 dark:text-slate-500">#{i+1}</span>}
                </span>
                <span className="font-mono font-semibold text-slate-700 dark:text-slate-200">{fmtMs(e.elapsed_ms)}</span>
                <span className="text-slate-400 dark:text-slate-500 text-xs">{fmtPace(e.elapsed_ms, group.distance_meters)}</span>
                <span className="text-slate-400 dark:text-slate-500 text-xs ml-auto">{fmtDate(e.achieved_at)}</span>
                <Link
                  to={`/activities/${e.activity_id}`}
                  className="text-blue-500 hover:text-blue-700 flex items-center gap-0.5 text-xs"
                >
                  View <ArrowRight size={11} />
                </Link>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export default function Records() {
  const [sport, setSport] = useState("run");
  const [groups, setGroups] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [expanded, setExpanded] = useState({});

  useEffect(() => {
    setExpanded({});
    setLoading(true);
    setError(null);
    api.records
      .list(sport)
      .then((data) => {
        setGroups(data ?? []);
        if (data?.length === 1) setExpanded({ [data[0].distance_meters]: true });
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [sport]);

  function toggle(dist) {
    setExpanded((prev) => ({ ...prev, [dist]: !prev[dist] }));
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Personal Records</h1>
        <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">Best efforts at standard distances</p>
      </div>

      {/* Sport tabs */}
      <div className="flex gap-1.5">
        {SPORT_TABS.map((s) => (
          <button
            key={s}
            onClick={() => setSport(s)}
            className={`px-3 py-1.5 rounded-lg text-sm font-medium capitalize transition-colors ${
              sport === s
                ? "bg-slate-800 text-white"
                : "bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:bg-slate-200 dark:hover:bg-slate-700"
            }`}
          >
            {s}
          </button>
        ))}
      </div>

      {/* Content */}
      {loading && (
        <div className="text-sm text-slate-400 dark:text-slate-500 animate-pulse">Loading records…</div>
      )}

      {error && (
        <div className="rounded-xl border border-red-200 dark:border-red-800/50 bg-red-50 dark:bg-red-900/20 p-4 text-sm text-red-700 dark:text-red-400">{error}</div>
      )}

      {!loading && !error && groups.length === 0 && (
        <div className="rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-950 p-8 text-center">
          <Trophy size={32} className="mx-auto text-slate-300 dark:text-slate-600 mb-3" />
          <p className="text-sm font-medium text-slate-500 dark:text-slate-400">No records yet</p>
          <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">
            Sync or upload {sport} activities with GPS distance data to see your PRs.
          </p>
        </div>
      )}

      {!loading && !error && groups.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 items-start">
          {groups.map((g) => (
            <PRCard
              key={g.distance_meters}
              group={g}
              expanded={!!expanded[g.distance_meters]}
              onToggle={() => toggle(g.distance_meters)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
