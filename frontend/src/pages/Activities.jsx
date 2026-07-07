import { useState, useEffect, useCallback } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import { useUnits } from "../settings";
import { fmtDistShort, fmtPace as uFmtPace } from "../units";

const SPORTS = ["run", "ride", "swim", "walk", "hike"];

const SPORT_EMOJI = {
  run: "🏃", ride: "🚴", swim: "🏊", walk: "🚶", hike: "🥾", unknown: "⚡",
};

function fmtDuration(ms) {
  if (!ms) return "—";
  const min = Math.floor(ms / 60000);
  const h = Math.floor(min / 60);
  const m = min % 60;
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}

function fmtHR(hr) {
  if (!hr) return "—";
  return Math.round(hr) + " bpm";
}

function fmtDate(iso) {
  return new Date(iso).toLocaleDateString("en-GB", {
    day: "numeric", month: "short", year: "numeric",
  });
}

function capitalize(s) {
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : "";
}

function Skeleton({ className = "" }) {
  return <div className={`bg-slate-200 dark:bg-slate-700 rounded animate-pulse ${className}`} />;
}

export default function Activities() {
  const [units] = useUnits();
  const imp = units === "imperial";
  const [activities, setActivities] = useState([]);
  const [nextCursor, setNextCursor] = useState(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState(null);

  const [sport, setSport] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [period, setPeriod] = useState("all");
  const [selectedYear, setSelectedYear] = useState(new Date().getFullYear());

  function localDateStr(d) {
    return `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,"0")}-${String(d.getDate()).padStart(2,"0")}`;
  }

  function selectPeriod(p, year) {
    const now = new Date();
    const yr = year ?? selectedYear;
    if (p === "W") {
      const day = now.getDay();
      const diff = day === 0 ? -6 : 1 - day;
      const mon = new Date(now); mon.setDate(now.getDate() + diff);
      const sun = new Date(mon); sun.setDate(mon.getDate() + 6);
      setFrom(localDateStr(mon)); setTo(localDateStr(sun));
    } else if (p === "M") {
      const m = now.getMonth();
      const last = new Date(yr, m + 1, 0).getDate();
      setFrom(`${yr}-${String(m+1).padStart(2,"0")}-01`);
      setTo(`${yr}-${String(m+1).padStart(2,"0")}-${String(last).padStart(2,"0")}`);
    } else if (p === "Y") {
      setFrom(`${yr}-01-01`);
      setTo(`${yr}-12-31`);
    } else {
      setFrom(""); setTo("");
    }
    if (year !== undefined) setSelectedYear(year);
    setPeriod(p);
  }

  const fetchPage = useCallback(
    async (cursor = null, replace = true) => {
      replace ? setLoading(true) : setLoadingMore(true);
      setError(null);
      try {
        const res = await api.activities.list({
          sport_type: sport,
          from,
          to,
          cursor: cursor ?? "",
          limit: 20,
        });
        const rows = res.activities ?? [];
        setActivities((prev) => (replace ? rows : [...prev, ...rows]));
        setNextCursor(res.next_cursor || null);
      } catch (err) {
        setError(err.message);
      } finally {
        replace ? setLoading(false) : setLoadingMore(false);
      }
    },
    [sport, from, to]
  );

  // Re-fetch when filters change
  useEffect(() => {
    fetchPage(null, true);
  }, [fetchPage]);

  return (
    <div className="space-y-5">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Activities</h1>
        <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">All synced and uploaded activities</p>
      </div>

      {/* Filter bar */}
      <div className="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-4 space-y-3">
        {/* Period tabs + year stepper */}
        <div className="flex items-center gap-3 flex-wrap">
          <div className="flex gap-1">
            {["W","M","Y","all"].map(p => (
              <button
                key={p}
                onClick={() => selectPeriod(p)}
                className={`px-3 py-1 rounded-full text-xs font-semibold transition-colors ${
                  period === p
                    ? "bg-slate-800 dark:bg-slate-200 text-white dark:text-slate-900"
                    : "bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:bg-slate-200 dark:hover:bg-slate-700"
                }`}
              >
                {p === "all" ? "All" : p}
              </button>
            ))}
          </div>
          {(period === "Y" || period === "M") && (
            <div className="flex items-center gap-1">
              <button
                onClick={() => selectPeriod(period, selectedYear - 1)}
                className="p-1 rounded text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 transition-colors"
              >‹</button>
              <span className="text-sm font-semibold text-slate-700 dark:text-slate-200 w-12 text-center">{selectedYear}</span>
              <button
                onClick={() => selectPeriod(period, selectedYear + 1)}
                disabled={selectedYear >= new Date().getFullYear()}
                className="p-1 rounded text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 disabled:opacity-30 transition-colors"
              >›</button>
            </div>
          )}
          {period === "all" && (from || to) && (
            <button
              onClick={() => { setFrom(""); setTo(""); }}
              className="text-xs text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300"
            >Clear dates</button>
          )}
        </div>

        {/* Sport chips + manual date range (only when period = all) */}
        <div className="flex flex-wrap gap-3 items-end">
          <div className="flex gap-1.5">
            <ChipEl active={sport === ""} onClick={() => setSport("")}>All</ChipEl>
            {SPORTS.map((s) => (
              <ChipEl key={s} active={sport === s} onClick={() => setSport(s === sport ? "" : s)}>
                {SPORT_EMOJI[s]} {capitalize(s)}
              </ChipEl>
            ))}
          </div>
          {period === "all" && (
            <div className="flex gap-2 items-end">
              <label className="flex flex-col gap-1">
                <span className="text-xs text-slate-400 dark:text-slate-500 font-medium">From</span>
                <input type="date" value={from} onChange={(e) => setFrom(e.target.value)}
                  className="text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-1.5 text-slate-700 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-slate-400 dark:text-slate-500 font-medium">To</span>
                <input type="date" value={to} onChange={(e) => setTo(e.target.value)}
                  className="text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-1.5 text-slate-700 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
              </label>
            </div>
          )}
        </div>
      </div>

      {/* Error */}
      {error && (
        <div className="rounded-xl border border-red-200 dark:border-red-800/50 bg-red-50 dark:bg-red-900/20 p-4 text-sm text-red-700 dark:text-red-400">
          {error}
        </div>
      )}

      {/* Table */}
      <div className="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 overflow-hidden">
        {/* Column headers */}
        <div className="grid grid-cols-[2fr_1fr_1fr_1fr_1fr_1fr] gap-4 px-5 py-2.5 border-b border-slate-100 dark:border-slate-800 text-xs font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-wide">
          <span>Activity</span>
          <span>Distance</span>
          <span>Duration</span>
          <span>Pace</span>
          <span>Avg HR</span>
          <span>Calories</span>
        </div>

        {loading ? (
          <div className="p-5 space-y-3">
            {[...Array(5)].map((_, i) => (
              <Skeleton key={i} className="h-14 w-full" />
            ))}
          </div>
        ) : activities.length === 0 ? (
          <div className="px-5 py-12 text-center">
            <p className="text-sm text-slate-500 dark:text-slate-400">No activities match your filters.</p>
          </div>
        ) : (
          <ul>
            {activities.map((act, i) => (
              <li
                key={act.id}
                className={i < activities.length - 1 ? "border-b border-slate-100 dark:border-slate-800" : ""}
              >
                <Link
                  to={`/activities/${act.id}`}
                  className="grid grid-cols-[2fr_1fr_1fr_1fr_1fr_1fr] gap-4 px-5 py-3.5 hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors items-center"
                >
                  <div className="flex items-center gap-3 min-w-0">
                    <span className="text-lg shrink-0">
                      {SPORT_EMOJI[act.sport_type] ?? "⚡"}
                    </span>
                    <div className="min-w-0">
                      <p className="text-sm font-medium text-slate-700 dark:text-slate-200 truncate">
                        {act.name || capitalize(act.sport_type)}
                      </p>
                      <p className="text-xs text-slate-400 dark:text-slate-500">{fmtDate(act.start_at)}</p>
                    </div>
                  </div>
                  <span className="text-sm text-slate-600 dark:text-slate-300">{fmtDistShort(act.distance_meters, imp)}</span>
                  <span className="text-sm text-slate-600 dark:text-slate-300">{fmtDuration(act.elapsed_ms)}</span>
                  <span className="text-sm text-slate-600 dark:text-slate-300">{uFmtPace(act.average_pace_seconds_per_km, imp)}</span>
                  <span className="text-sm text-slate-600 dark:text-slate-300">{fmtHR(act.average_heart_rate)}</span>
                  <span className="text-sm text-slate-600 dark:text-slate-300">
                    {act.calories ? `${act.calories.toLocaleString()} kcal` : "—"}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}

        {/* Load more */}
        {nextCursor && !loading && (
          <div className="px-5 py-4 border-t border-slate-100 dark:border-slate-800">
            <button
              onClick={() => fetchPage(nextCursor, false)}
              disabled={loadingMore}
              className="w-full py-2 text-sm text-blue-600 dark:text-blue-400 font-medium hover:text-blue-700 dark:hover:text-blue-300 disabled:text-slate-400 dark:disabled:text-slate-600"
            >
              {loadingMore ? "Loading…" : "Load more"}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

function chip(active) {
  return [
    "px-3 py-1 rounded-full text-xs font-medium transition-colors",
    active
      ? "text-white"
      : "bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700",
  ].join(" ");
}

function ChipEl({ active, onClick, children }) {
  return (
    <button
      onClick={onClick}
      className={chip(active)}
      style={active ? { backgroundColor: "var(--accent)" } : {}}
    >
      {children}
    </button>
  );
}
