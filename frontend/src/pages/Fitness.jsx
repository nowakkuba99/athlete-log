import { useState, useEffect, useCallback } from "react";
import {
  ComposedChart, Line, Bar, XAxis, YAxis, Tooltip,
  ResponsiveContainer, CartesianGrid, ReferenceLine, Legend,
} from "recharts";
import { api } from "../api/client";
import { useChartTheme } from "../settings";

// ─── Date helpers ─────────────────────────────────────────────────────────────

function toISO(date) {
  return date.toISOString().slice(0, 10);
}

function addDays(date, n) {
  const d = new Date(date);
  d.setDate(d.getDate() + n);
  return d;
}

function fmtAxisDate(iso) {
  const d = new Date(iso + "T00:00:00");
  return d.toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

function fmtFullDate(iso) {
  return new Date(iso + "T00:00:00").toLocaleDateString("en-GB", {
    weekday: "short", day: "numeric", month: "short", year: "numeric",
  });
}

// ─── Range presets ────────────────────────────────────────────────────────────

const RANGES = [
  { label: "3 mo",  days: 90 },
  { label: "6 mo",  days: 180 },
  { label: "1 yr",  days: 365 },
  { label: "All",   days: null },
];

// ─── Custom tooltip ───────────────────────────────────────────────────────────

function PMCTooltip({ active, payload, label }) {
  if (!active || !payload?.length || !label) return null;
  const d = payload[0]?.payload;
  if (!d) return null;
  return (
    <div className="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-xl shadow-lg px-3.5 py-3 text-xs pointer-events-none min-w-[160px]">
      <p className="font-semibold text-slate-700 dark:text-slate-200 mb-2">{fmtFullDate(label)}</p>
      <div className="space-y-1">
        <div className="flex justify-between gap-4">
          <span className="text-blue-600 font-medium">Fitness (CTL)</span>
          <span className="font-mono text-slate-700 dark:text-slate-200">{d.ctl?.toFixed(1)}</span>
        </div>
        <div className="flex justify-between gap-4">
          <span className="text-red-500 font-medium">Fatigue (ATL)</span>
          <span className="font-mono text-slate-700 dark:text-slate-200">{d.atl?.toFixed(1)}</span>
        </div>
        <div className="flex justify-between gap-4">
          <span className={d.tsb >= 0 ? "text-emerald-600 font-medium" : "text-rose-500 font-medium"}>
            Form (TSB)
          </span>
          <span className="font-mono text-slate-700 dark:text-slate-200">{d.tsb >= 0 ? "+" : ""}{d.tsb?.toFixed(1)}</span>
        </div>
        {d.trimp > 0 && (
          <div className="flex justify-between gap-4 pt-1 mt-1 border-t border-slate-100 dark:border-slate-800">
            <span className="text-slate-400 dark:text-slate-500">Load</span>
            <span className="font-mono text-slate-500 dark:text-slate-400">{d.trimp?.toFixed(0)}</span>
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Main page ────────────────────────────────────────────────────────────────

export default function Fitness() {
  const ct = useChartTheme();
  const [data,    setData]    = useState([]);
  const [loading, setLoading] = useState(true);
  const [error,   setError]   = useState(null);
  const [range,   setRange]   = useState(180); // days; null = all
  const [hasHR,   setHasHR]   = useState(true);

  const load = useCallback((days) => {
    setLoading(true);
    setError(null);
    const params = {};
    if (days != null) {
      params.from = toISO(addDays(new Date(), -days));
    }
    // Always fetch to today
    params.to = toISO(new Date());
    api.stats.pmc(params)
      .then((pts) => {
        setData(pts ?? []);
        setHasHR((pts ?? []).some((p) => p.trimp > 0 && p.trimp !== Math.round(p.trimp)));
      })
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(range); }, [range, load]);

  // Current values (last data point)
  const last = data.length ? data[data.length - 1] : null;

  // x-axis: show every ~30 days to avoid crowding
  const tickInterval = data.length > 270 ? 29 : data.length > 90 ? 13 : 6;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Fitness</h1>
          <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">
            Performance Management Chart — fitness, fatigue, and form over time
          </p>
        </div>
        {/* Range selector */}
        <div className="flex rounded-lg border border-slate-200 dark:border-slate-700 overflow-hidden text-sm font-medium shrink-0">
          {RANGES.map((r) => (
            <button
              key={r.label}
              onClick={() => setRange(r.days)}
              className={[
                "px-3 py-1.5 transition-colors",
                range === r.days
                  ? "bg-slate-800 text-white"
                  : "text-slate-500 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800/50",
              ].join(" ")}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>

      {/* KPI strip */}
      {last && !loading && (
        <div className="grid grid-cols-3 gap-3">
          <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 px-5 py-4">
            <p className="text-xs text-slate-400 dark:text-slate-500 mb-1">Fitness (CTL)</p>
            <p className="text-2xl font-bold text-blue-600">{last.ctl.toFixed(1)}</p>
            <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">42-day avg load</p>
          </div>
          <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 px-5 py-4">
            <p className="text-xs text-slate-400 dark:text-slate-500 mb-1">Fatigue (ATL)</p>
            <p className="text-2xl font-bold text-red-500">{last.atl.toFixed(1)}</p>
            <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">7-day avg load</p>
          </div>
          <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 px-5 py-4">
            <p className="text-xs text-slate-400 dark:text-slate-500 mb-1">Form (TSB)</p>
            <p className={`text-2xl font-bold ${last.tsb >= 0 ? "text-emerald-600" : "text-rose-500"}`}>
              {last.tsb >= 0 ? "+" : ""}{last.tsb.toFixed(1)}
            </p>
            <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">
              {last.tsb >= 5 ? "Fresh — ready to race" : last.tsb >= -10 ? "Neutral — train well" : last.tsb >= -25 ? "Tired — build phase" : "Very tired — rest"}
            </p>
          </div>
        </div>
      )}

      {/* Main chart */}
      <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
        {error && (
          <p className="text-sm text-red-500 mb-4">{error}</p>
        )}
        {loading ? (
          <div className="h-72 flex items-center justify-center text-sm text-slate-400 dark:text-slate-500 animate-pulse">
            Computing training load…
          </div>
        ) : data.length === 0 ? (
          <div className="h-72 flex items-center justify-center text-sm text-slate-400 dark:text-slate-500">
            No activity data yet.
          </div>
        ) : (
          <ResponsiveContainer width="100%" height={320}>
            <ComposedChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke={ct.gridStroke} />
              <XAxis
                dataKey="date"
                tickFormatter={fmtAxisDate}
                interval={tickInterval}
                tick={{ fontSize: 11, fill: ct.tickFill }}
                axisLine={false}
                tickLine={false}
              />
              <YAxis
                tick={{ fontSize: 11, fill: ct.tickFill }}
                axisLine={false}
                tickLine={false}
                width={36}
              />
              <Tooltip content={<PMCTooltip />} />
              <ReferenceLine y={0} stroke={ct.refStroke} strokeWidth={1.5} />
              {/* Training load bars */}
              <Bar dataKey="trimp" fill={ct.refStroke} name="Load" barSize={2} opacity={0.7} />
              {/* CTL — fitness */}
              <Line
                dataKey="ctl"
                name="Fitness (CTL)"
                stroke="var(--accent)"
                strokeWidth={2.5}
                dot={false}
                activeDot={{ r: 4, fill: "var(--accent)" }}
              />
              {/* ATL — fatigue */}
              <Line
                dataKey="atl"
                name="Fatigue (ATL)"
                stroke="#ef4444"
                strokeWidth={2}
                strokeDasharray="5 3"
                dot={false}
                activeDot={{ r: 4, fill: "#ef4444" }}
              />
              {/* TSB — form */}
              <Line
                dataKey="tsb"
                name="Form (TSB)"
                stroke="#10b981"
                strokeWidth={1.5}
                dot={false}
                activeDot={{ r: 4, fill: "#10b981" }}
              />
              <Legend
                wrapperStyle={{ fontSize: 12, paddingTop: 12 }}
                formatter={(v) => <span style={{ color: ct.tickFill }}>{v}</span>}
              />
            </ComposedChart>
          </ResponsiveContainer>
        )}
      </div>

      {/* Explainer */}
      <div className="bg-slate-50 dark:bg-slate-950 rounded-xl border border-slate-200 dark:border-slate-700 p-5 text-xs text-slate-500 dark:text-slate-400 space-y-1.5">
        <p><span className="font-semibold text-blue-600">Fitness (CTL)</span> — 42-day exponentially weighted average of daily training load. Rises slowly as you train consistently.</p>
        <p><span className="font-semibold text-red-500">Fatigue (ATL)</span> — 7-day weighted average. Spikes after hard blocks, drops quickly during rest.</p>
        <p><span className="font-semibold text-emerald-600">Form (TSB)</span> — Fitness minus Fatigue. Positive = fresh and ready; negative = in training load; deeply negative = overreaching.</p>
        {!hasHR && (
          <p className="text-amber-600 pt-1 border-t border-slate-200 dark:border-slate-700">
            No heart rate data found — load is estimated from duration only. Set your max HR in <a href="/settings" className="underline">Settings</a> and connect a source with HR for more accurate values.
          </p>
        )}
      </div>
    </div>
  );
}
