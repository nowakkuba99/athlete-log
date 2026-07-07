import { useState, useEffect, useRef, forwardRef } from "react";
import { useUnits, useCalendarFillMode } from "../settings";
import { fmtDistShort } from "../units";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { Link } from "react-router-dom";
import { api } from "../api/client";

// ─── Constants ────────────────────────────────────────────────────────────────

const DOW_LABELS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

const SPORT_COLOR = {
  run:     { bg: "#f97316", light: "#fff7ed" },
  ride:    { bg: "#3b82f6", light: "#eff6ff" },
  swim:    { bg: "#06b6d4", light: "#ecfeff" },
  walk:    { bg: "#10b981", light: "#ecfdf5" },
  hike:    { bg: "#a16207", light: "#fefce8" },
  unknown: { bg: "#94a3b8", light: "#f8fafc" },
};

function sportColor(type) {
  return SPORT_COLOR[type?.toLowerCase()] ?? SPORT_COLOR.unknown;
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function fmtDist(m) {
  if (!m) return null;
  return (m / 1000).toFixed(1) + " km";
}

function fmtDuration(ms) {
  if (!ms) return null;
  const min = Math.floor(ms / 60000);
  const h = Math.floor(min / 60), m = min % 60;
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}

function fmtTime(iso) {
  return new Date(iso).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" });
}

// Build an array of 35 or 42 cells (null = empty padding day, number = day of month).
// Calendar starts on Monday.
function buildMonthGrid(year, month) {
  const firstDow = (new Date(year, month, 1).getDay() + 6) % 7; // 0=Mon
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const cells = [
    ...Array(firstDow).fill(null),
    ...Array.from({ length: daysInMonth }, (_, i) => i + 1),
  ];
  while (cells.length % 7 !== 0) cells.push(null);
  return cells;
}

function isoDate(year, month, day) {
  return `${year}-${String(month + 1).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}

function isToday(year, month, day) {
  const t = new Date();
  return t.getFullYear() === year && t.getMonth() === month && t.getDate() === day;
}

// ─── Sub-components ───────────────────────────────────────────────────────────

// WaveFill: a water-level fill that rises on mount (fill-rise animation) with a
// continuous horizontal wave at the surface. Exposes a ref so CalendarCell can
// add .wave-splash to trigger a one-shot water-disturbance animation on hover.
//
// Wave math (cubic-bezier sine, magic 0.5523):
//   Container: absolute bottom-0, height = pct% of cell.
//   SVG inside: 200% wide, viewBox "0 0 200 100". Wave at y=8, amp ±6.
//   Two full periods across 200 units; translateX(-50%) = exactly one period.
//   Result: wave surface stays at the TOP of the rising container (= water level).
const WaveFill = forwardRef(function WaveFill({ pct, color, onFillDone }, ref) {
  const [{ dur, delay }] = useState(() => ({
    dur:   6 + Math.random() * 5,   // 6–11 s per wave loop (calm)
    delay: -(Math.random() * 11),   // random phase per cell
  }));

  if (!pct || pct <= 0) return null;

  const y0 = 8, amp = 6;
  const [y1, y2] = [y0 - amp, y0 + amp]; // crest=2, trough=14

  const path = [
    `M0,${y0}`,
    `C13.8,${y1} 36.2,${y1} 50,${y0}`,    // period 1: up
    `C63.8,${y2} 86.2,${y2} 100,${y0}`,   // period 1: down
    `C113.8,${y1} 136.2,${y1} 150,${y0}`, // period 2: up
    `C163.8,${y2} 186.2,${y2} 200,${y0}`, // period 2: down
    `L200,100 L0,100 Z`,
  ].join(" ");

  return (
    <div
      ref={ref}
      className="wave-cell absolute bottom-0 left-0 right-0 overflow-hidden pointer-events-none"
      style={{ height: `${pct}%` }}
      onAnimationEnd={(e) => {
        if (e.animationName === "fill-rise") onFillDone?.();
        if (e.animationName === "wave-splash") e.currentTarget.classList.remove("wave-splash");
      }}
    >
      <svg
        style={{
          position: "absolute", top: 0, left: 0,
          width: "200%", height: "100%",
          animationName: "wave-flow",
          animationTimingFunction: "linear",
          animationIterationCount: "infinite",
          animationDuration: `${dur}s`,
          animationDelay: `${delay}s`,
        }}
        viewBox="0 0 200 100"
        preserveAspectRatio="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        <path d={path} fill={color + "38"} />
      </svg>
    </div>
  );
});

function CalendarCell({ day, acts, peakDist, year, month, selected, onSelect }) {
  // Hooks must come before the early return
  const waveRef     = useRef(null);
  const fillDoneRef = useRef(false);
  const [calFillMode] = useCalendarFillMode();

  if (day === null) return <div className="aspect-square" />;

  const bySport = acts.reduce((acc, a) => {
    const s = a.sport_type ?? "unknown";
    acc[s] = (acc[s] ?? 0) + (a.distance_meters ?? 0);
    return acc;
  }, {});
  const dayDist = acts.reduce((s, a) => s + (a.distance_meters ?? 0), 0);
  const primarySport = Object.entries(bySport).sort((a, b) => b[1] - a[1])[0]?.[0] ?? "unknown";
  const fillPct = peakDist > 0 && dayDist > 0 ? Math.max(8, (dayDist / peakDist) * 100) : 0;
  const hasActs = acts.length > 0;

  function handleMouseEnter() {
    const el = waveRef.current;
    if (!hasActs || fillPct <= 0 || !el || !fillDoneRef.current) return;
    if (el.classList.contains("wave-splash")) return;
    el.classList.add("wave-splash");
  }

  return (
    <button
      onClick={() => hasActs && onSelect(day)}
      onMouseEnter={handleMouseEnter}
      disabled={!hasActs}
      className={[
        "relative aspect-square rounded-xl p-1.5 flex flex-col transition-all text-left w-full overflow-hidden",
        selected
          ? "ring-2 ring-blue-400 bg-blue-50 dark:bg-blue-900/30"
          : hasActs
            ? "hover:bg-slate-50 dark:hover:bg-slate-800/50 cursor-pointer"
            : "cursor-default",
      ].join(" ")}
    >
      <WaveFill
        ref={waveRef}
        pct={fillPct}
        color={calFillMode === "accent"
          ? getComputedStyle(document.documentElement).getPropertyValue("--accent").trim()
          : sportColor(primarySport).bg}
        onFillDone={() => { fillDoneRef.current = true; }}
      />
      <span className={[
        "relative z-10 text-xs font-medium w-5 h-5 flex items-center justify-center rounded-full shrink-0",
        isToday(year, month, day) ? "bg-blue-500 text-white"
          : hasActs ? "text-slate-700 dark:text-slate-200"
          : "text-slate-300 dark:text-slate-600",
      ].join(" ")}>
        {day}
      </span>
      {fillPct > 0 && dayDist > 0 && (
        <span className="relative z-10 mt-auto text-[10px] leading-none font-bold self-end"
          style={{ color: calFillMode === "accent" ? "var(--accent)" : sportColor(primarySport).bg }}>
          {(dayDist / 1000).toFixed(1)}k
        </span>
      )}
      {hasActs && peakDist === 0 && (
        <div className="relative z-10 mt-auto flex flex-wrap gap-0.5 pt-1">
          {acts.slice(0, 3).map((a, j) => (
            <span key={j} className="w-1.5 h-1.5 rounded-full" style={{ background: sportColor(a.sport_type).bg }} />
          ))}
        </div>
      )}
    </button>
  );
}

function ActivityRow({ act }) {
  const { bg } = sportColor(act.sport_type);
  return (
    <Link
      to={`/activities/${act.id}`}
      className="flex items-center gap-3 px-4 py-2.5 hover:bg-slate-50 dark:hover:bg-slate-800/50 rounded-lg transition-colors group"
    >
      <span className="w-2 h-2 rounded-full shrink-0" style={{ background: bg }} />
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium text-slate-800 dark:text-slate-100 truncate group-hover:text-blue-600 transition-colors">
          {act.name || act.sport_type}
        </p>
        <p className="text-xs text-slate-400 dark:text-slate-500">
          {fmtTime(act.start_at)}
          {act.distance_meters ? ` · ${fmtDist(act.distance_meters)}` : ""}
          {act.elapsed_ms ? ` · ${fmtDuration(act.elapsed_ms)}` : ""}
        </p>
      </div>
      <span className="text-xs text-slate-300 dark:text-slate-600 capitalize shrink-0">{act.sport_type}</span>
    </Link>
  );
}

function DayCell({ day, acts, selected, onClick }) {
  if (day === null) return <div className="aspect-square" />;

  const hasActs = acts.length > 0;
  const today = isToday(...this_ym_placeholder, day); // filled below via closure

  return (
    <button
      onClick={() => hasActs && onClick(day)}
      disabled={!hasActs}
      className={[
        "aspect-square rounded-xl p-1.5 flex flex-col transition-colors text-left w-full",
        selected
          ? "ring-2 ring-blue-400 bg-blue-50"
          : hasActs
            ? "hover:bg-slate-100 dark:hover:bg-slate-800 cursor-pointer"
            : "cursor-default",
      ].join(" ")}
    >
      <span
        className={[
          "text-xs font-medium w-5 h-5 flex items-center justify-center rounded-full shrink-0",
          today
            ? "bg-blue-500 text-white"
            : hasActs
              ? "text-slate-700 dark:text-slate-200"
              : "text-slate-300 dark:text-slate-600",
        ].join(" ")}
      >
        {day}
      </span>
      {hasActs && (
        <div className="mt-auto flex flex-wrap gap-0.5 pt-1">
          {acts.slice(0, 4).map((a, i) => (
            <span
              key={i}
              className="w-2 h-2 rounded-full shrink-0"
              style={{ background: sportColor(a.sport_type).bg }}
            />
          ))}
          {acts.length > 4 && (
            <span className="text-[9px] text-slate-400 dark:text-slate-500 leading-none self-end">+{acts.length - 4}</span>
          )}
        </div>
      )}
    </button>
  );
}

// ─── Main page ────────────────────────────────────────────────────────────────

export default function Calendar() {
  const [units] = useUnits();
  const imp = units === "imperial";
  const today = new Date();
  const [year,  setYear]  = useState(today.getFullYear());
  const [month, setMonth] = useState(today.getMonth()); // 0-indexed

  const [activities, setActivities] = useState([]);
  const [loading,    setLoading]    = useState(true);
  const [error,      setError]      = useState(null);
  const [selectedDay, setSelectedDay] = useState(null);
  const [sportFilter, setSportFilter] = useState("all");
  const [streaks, setStreaks] = useState(null);

  // Streaks are global — fetch once on mount
  useEffect(() => {
    api.stats.streaks().then(setStreaks).catch(() => {});
  }, []);

  useEffect(() => {
    setLoading(true);
    setError(null);
    setSelectedDay(null);

    const from = isoDate(year, month, 1);
    const daysInMonth = new Date(year, month + 1, 0).getDate();
    const to = isoDate(year, month, daysInMonth);

    api.activities.list({ from, to, limit: 200 })
      .then((data) => setActivities(data.activities ?? []))
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, [year, month]);

  function prevMonth() {
    if (month === 0) { setYear(y => y - 1); setMonth(11); }
    else setMonth(m => m - 1);
  }
  function nextMonth() {
    if (month === 11) { setYear(y => y + 1); setMonth(0); }
    else setMonth(m => m + 1);
  }
  function goToday() {
    setYear(today.getFullYear());
    setMonth(today.getMonth());
  }

  const monthLabel = new Date(year, month, 1).toLocaleString("en-GB", { month: "long", year: "numeric" });

  // Group by day-of-month
  const byDay = {};
  for (const act of activities) {
    const d = new Date(act.start_at);
    // compare in local time
    const actYear = d.getFullYear(), actMonth = d.getMonth(), actDay = d.getDate();
    if (actYear === year && actMonth === month) {
      if (!byDay[actDay]) byDay[actDay] = [];
      byDay[actDay].push(act);
    }
  }

  // Sport types present this month
  const sports = [...new Set(activities.map(a => a.sport_type))].sort();

  const filtered = sportFilter === "all"
    ? byDay
    : Object.fromEntries(
        Object.entries(byDay).map(([d, acts]) => [d, acts.filter(a => a.sport_type === sportFilter)])
      );

  const grid = buildMonthGrid(year, month);

  // Peak total distance across all active days this month — used to scale bars
  const peakDist = Object.values(filtered).reduce((max, acts) => {
    const tot = acts.reduce((s, a) => s + (a.distance_meters ?? 0), 0);
    return Math.max(max, tot);
  }, 0);

  const selectedActs = selectedDay
    ? (filtered[selectedDay] ?? []).sort((a, b) => a.start_at.localeCompare(b.start_at))
    : [];

  // Monthly summary
  const totalDist = activities
    .filter(a => sportFilter === "all" || a.sport_type === sportFilter)
    .reduce((s, a) => s + (a.distance_meters ?? 0), 0);
  const activeDays = new Set(
    activities
      .filter(a => sportFilter === "all" || a.sport_type === sportFilter)
      .map(a => new Date(a.start_at).getDate())
  ).size;

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Calendar</h1>
          <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">Training overview by month</p>
          {streaks && (streaks.current_days > 0 || streaks.longest_days > 0) && (
            <div className="flex items-center gap-3 mt-2 text-xs text-slate-500 dark:text-slate-400">
              {streaks.current_days > 0 && (
                <span className="flex items-center gap-1 font-medium text-orange-600">
                  🔥 {streaks.current_days} day streak
                </span>
              )}
              {streaks.longest_days > 0 && (
                <span>Longest: {streaks.longest_days} days</span>
              )}
              {streaks.current_weeks > 0 && (
                <span className="text-slate-400 dark:text-slate-500">·</span>
              )}
              {streaks.current_weeks > 0 && (
                <span className="flex items-center gap-1 font-medium text-blue-600">
                  📅 {streaks.current_weeks} week streak
                </span>
              )}
              {streaks.longest_weeks > 0 && (
                <span>Longest: {streaks.longest_weeks} weeks</span>
              )}
            </div>
          )}
        </div>

        {/* Month nav */}
        <div className="flex items-center gap-2 shrink-0">
          <button
            onClick={prevMonth}
            className="p-2 rounded-lg border border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            <ChevronLeft size={16} />
          </button>
          <button
            onClick={goToday}
            className="px-3 py-1.5 text-sm font-medium text-slate-600 dark:text-slate-300 border border-slate-200 dark:border-slate-700 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors min-w-[160px] text-center"
          >
            {monthLabel}
          </button>
          <button
            onClick={nextMonth}
            className="p-2 rounded-lg border border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
          >
            <ChevronRight size={16} />
          </button>
        </div>
      </div>

      {/* Monthly summary + sport filter */}
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-4 text-sm text-slate-500 dark:text-slate-400">
          {!loading && (
            <>
              <span><span className="font-semibold text-slate-700 dark:text-slate-200">{activeDays}</span> active days</span>
              {totalDist > 0 && (
                <span><span className="font-semibold text-slate-700 dark:text-slate-200">{fmtDistShort(totalDist, imp)}</span> total</span>
              )}
              <span><span className="font-semibold text-slate-700 dark:text-slate-200">{activities.filter(a => sportFilter === "all" || a.sport_type === sportFilter).length}</span> activities</span>
            </>
          )}
        </div>
        {sports.length > 1 && (
          <div className="flex gap-1.5 shrink-0">
            <button
              onClick={() => setSportFilter("all")}
              className={`px-2.5 py-1 rounded-full text-xs font-medium transition-colors ${sportFilter === "all" ? "bg-slate-800 text-white" : "bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:bg-slate-200 dark:hover:bg-slate-700"}`}
            >
              All
            </button>
            {sports.map(s => (
              <button
                key={s}
                onClick={() => setSportFilter(s)}
                className={`px-2.5 py-1 rounded-full text-xs font-medium capitalize transition-colors ${sportFilter === s ? "text-white" : "bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:bg-slate-200 dark:hover:bg-slate-700"}`}
                style={sportFilter === s ? { background: sportColor(s).bg } : {}}
              >
                {s}
              </button>
            ))}
          </div>
        )}
      </div>

      {error && (
        <div className="rounded-xl border border-red-200 dark:border-red-800/50 bg-red-50 dark:bg-red-900/20 p-4 text-sm text-red-700 dark:text-red-400">{error}</div>
      )}

      {/* Calendar grid */}
      <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-4">
        {/* Day-of-week header */}
        <div className="grid grid-cols-7 mb-1">
          {DOW_LABELS.map(d => (
            <div key={d} className="text-center text-xs font-medium text-slate-400 dark:text-slate-500 py-1">{d}</div>
          ))}
        </div>

        {loading ? (
          <div className="grid grid-cols-7 gap-1">
            {Array.from({ length: 35 }).map((_, i) => (
              <div key={i} className="aspect-square rounded-xl bg-slate-100 dark:bg-slate-800 animate-pulse" />
            ))}
          </div>
        ) : (
          <div className="grid grid-cols-7 gap-1">
            {grid.map((day, i) => (
              <CalendarCell
                key={i}
                day={day}
                acts={day != null ? (filtered[day] ?? []) : []}
                peakDist={peakDist}
                year={year}
                month={month}
                selected={selectedDay === day}
                onSelect={d => setSelectedDay(selectedDay === d ? null : d)}
              />
            ))}
          </div>
        )}
      </div>

      {/* Expanded day panel */}
      {selectedDay && selectedActs.length > 0 && (
        <div className="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 overflow-hidden">
          <div className="px-5 py-3 border-b border-slate-100 dark:border-slate-800 flex items-center justify-between">
            <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">
              {new Date(year, month, selectedDay).toLocaleDateString("en-GB", {
                weekday: "long", day: "numeric", month: "long",
              })}
            </p>
            <span className="text-xs text-slate-400 dark:text-slate-500">{selectedActs.length} {selectedActs.length === 1 ? "activity" : "activities"}</span>
          </div>
          <div className="divide-y divide-slate-50 dark:divide-slate-800">
            {selectedActs.map((act) => (
              <Link
                key={act.id}
                to={`/activities/${act.id}`}
                className="flex items-center gap-3 px-5 py-3 hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors group"
              >
                <span
                  className="w-2.5 h-2.5 rounded-full shrink-0"
                  style={{ background: sportColor(act.sport_type).bg }}
                />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-slate-800 dark:text-slate-100 truncate group-hover:text-blue-600 transition-colors">
                    {act.name || act.sport_type}
                  </p>
                  <p className="text-xs text-slate-400 dark:text-slate-500">
                    {fmtTime(act.start_at)}
                    {act.distance_meters ? ` · ${fmtDistShort(act.distance_meters, imp)}` : ""}
                    {act.elapsed_ms ? ` · ${fmtDuration(act.elapsed_ms)}` : ""}
                  </p>
                </div>
                <span className="text-xs text-slate-400 dark:text-slate-500 capitalize shrink-0">{act.sport_type}</span>
              </Link>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
