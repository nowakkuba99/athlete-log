import { useState, useEffect } from "react";
import { api } from "../api/client";
import { useUnits } from "../settings";

// Riegel formula: T2 = T1 × (D2/D1)^1.06
function riegelPredict(knownMs, knownDist, targetDist) {
  return knownMs * Math.pow(targetDist / knownDist, 1.06);
}

// Jack Daniels VDOT estimate
function vdotFromEffort(distM, elapsedMs) {
  const t = elapsedMs / 60000; // minutes
  const v = (distM / t); // m/min
  const pct = 0.8 + 0.1894393 * Math.exp(-0.012778 * t) + 0.2989558 * Math.exp(-0.1932605 * t);
  const vo2 = -4.6 + 0.182258 * v + 0.000104 * v * v;
  return vo2 / pct;
}

const VDOT_LEVELS = [
  { min: 66, label: "Sub-elite",    color: "text-purple-600 dark:text-purple-400" },
  { min: 56, label: "Competitive",  color: "text-blue-600 dark:text-blue-400"    },
  { min: 46, label: "Recreational", color: "text-green-600 dark:text-green-400"  },
  { min: 30, label: "Beginner",     color: "text-amber-600 dark:text-amber-400"  },
  { min: 0,  label: "Novice",       color: "text-slate-400"                      },
];

function vdotLevel(v) { return VDOT_LEVELS.find(l => v >= l.min) ?? VDOT_LEVELS.at(-1); }

const RACE_DISTANCES = [
  { label: "1 km",  dist: 1000   },
  { label: "5 km",  dist: 5000   },
  { label: "10 km", dist: 10000  },
  { label: "15 km", dist: 15000  },
  { label: "HM",    dist: 21097.5 },
  { label: "FM",    dist: 42195  },
];

function fmtMs(ms) {
  if (!ms || ms <= 0) return "—";
  const s = Math.round(ms / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0) return `${h}:${String(m).padStart(2,"0")}:${String(sec).padStart(2,"0")}`;
  return `${m}:${String(sec).padStart(2,"0")}`;
}

function fmtPace(ms, distM, imperial) {
  if (!ms || !distM) return "—";
  if (imperial) {
    const secPerMile = (ms / 1000) / (distM / 1609.34);
    let m = Math.floor(secPerMile / 60), s = Math.round(secPerMile % 60);
    if (s === 60) { m++; s = 0; }
    return `${m}:${String(s).padStart(2,"0")} /mi`;
  }
  const secPerKm = (ms / 1000) / (distM / 1000);
  let m = Math.floor(secPerKm / 60), s = Math.round(secPerKm % 60);
  if (s === 60) { m++; s = 0; }
  return `${m}:${String(s).padStart(2,"0")} /km`;
}

function fmtDate(iso) {
  return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

function fmtDistKm(m) {
  return m >= 1000 ? `${(m / 1000).toFixed(1)} km` : `${Math.round(m)} m`;
}

const SPORTS = ["run", "ride"];
const SPORT_MIN_DIST = { run: 3000, ride: 10000 };

export default function Predictions() {
  const [units] = useUnits();
  const imp = units === "imperial";
  const [sport, setSport] = useState("run");
  const [activities, setActivities] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const [records, setRecords] = useState([]);

  useEffect(() => {
    setLoading(true);
    setError(null);
    Promise.all([
      api.activities.list({ limit: 50, sport_type: sport }),
      api.records.list(sport),
    ])
      .then(([actData, recData]) => {
        setActivities(actData?.activities ?? actData ?? []);
        setRecords(recData ?? []);
      })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, [sport]);

  // Include best efforts from records as high-quality reference anchors.
  // A 1km PR is a far better Riegel reference for short-distance predictions
  // than a long run where rest time inflates the average pace.
  const minDist = SPORT_MIN_DIST[sport] ?? 3000;
  const validActs = activities.filter(
    a => (a.distance_meters ?? 0) >= minDist && (a.elapsed_ms ?? 0) > 0
  );
  const recordRefs = records
    .map(g => ({ distance_meters: g.distance_meters, elapsed_ms: g.efforts[0]?.elapsed_ms }))
    .filter(r => r.elapsed_ms > 0);

  // Combined pool: recent activities + actual PRs from every distance
  const allCandidates = [...validActs, ...recordRefs];

  const refAct = allCandidates.reduce((best, a) => {
    const pace = a.elapsed_ms / (a.distance_meters / 1000);
    if (!best) return a;
    return pace < best.elapsed_ms / (best.distance_meters / 1000) ? a : best;
  }, null);

  // For each target distance, use the best-paced reference within 0.4×–4× of it.
  // Riegel is most accurate when ref and target distances are close — and actual
  // PRs (from records) provide much more accurate speed anchors than long runs.
  const predictions = refAct
    ? RACE_DISTANCES.map(rd => {
        const nearby = allCandidates.filter(a => {
          const r = a.distance_meters / rd.dist;
          return r >= 0.4 && r <= 4.0;
        });
        const ref = nearby.length > 0
          ? nearby.reduce((best, a) =>
              a.elapsed_ms / a.distance_meters < best.elapsed_ms / best.distance_meters ? a : best
            )
          : refAct;
        return { ...rd, ms: riegelPredict(ref.elapsed_ms, ref.distance_meters, rd.dist) };
      })
    : [];

  const vdot = refAct && sport === "run"
    ? vdotFromEffort(refAct.distance_meters, refAct.elapsed_ms)
    : null;
  const vdotInfo = vdot ? vdotLevel(vdot) : null;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Predictions</h1>
          <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">
            Race time forecasts from your recent training
          </p>
        </div>
        {vdotInfo && vdot && (
          <div className="shrink-0 text-right">
            <p className="text-xs text-slate-400 dark:text-slate-500 uppercase tracking-wide font-semibold">VDOT</p>
            <p className={`text-3xl font-black leading-none ${vdotInfo.color}`}>{vdot.toFixed(1)}</p>
            <p className={`text-xs font-semibold mt-0.5 ${vdotInfo.color}`}>{vdotInfo.label}</p>
          </div>
        )}
      </div>

      {/* Sport tabs */}
      <div className="flex gap-1.5">
        {SPORTS.map(s => (
          <button
            key={s}
            onClick={() => setSport(s)}
            className={`px-3 py-1.5 rounded-lg text-sm font-medium capitalize transition-colors ${
              sport === s
                ? "bg-slate-800 dark:bg-slate-200 text-white dark:text-slate-900"
                : "bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:bg-slate-200 dark:hover:bg-slate-700"
            }`}
          >
            {s}
          </button>
        ))}
      </div>

      {error && (
        <div className="rounded-xl border border-red-200 dark:border-red-800/50 bg-red-50 dark:bg-red-900/20 p-4 text-sm text-red-700 dark:text-red-400">{error}</div>
      )}

      {loading && (
        <div className="h-48 rounded-xl bg-slate-100 dark:bg-slate-800 animate-pulse" />
      )}

      {!loading && !error && !refAct && (
        <div className="flex items-center justify-center h-40 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-950 text-sm text-slate-400 dark:text-slate-500">
          No recent {sport} activities over {fmtDistKm(minDist)} found.
        </div>
      )}

      {!loading && refAct && (
        <>
          {/* Reference activity banner */}
          <div className="rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800/50 px-5 py-3 flex items-center justify-between gap-4">
            <div>
              <p className="text-xs font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-wide">Based on</p>
              <p className="text-sm font-semibold text-slate-700 dark:text-slate-200 mt-0.5 truncate">
                {refAct.name || `${sport} activity`}
              </p>
              <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">
                {fmtDistKm(refAct.distance_meters)} · {fmtMs(refAct.elapsed_ms)} · {fmtDate(refAct.start_at)}
              </p>
            </div>
            <div className="text-right shrink-0">
              <p className="text-xs text-slate-400 dark:text-slate-500">Avg pace</p>
              <p className="text-sm font-bold text-slate-700 dark:text-slate-200">
                {fmtPace(refAct.elapsed_ms, refAct.distance_meters, imp)}
              </p>
            </div>
          </div>

          {/* Prediction card */}
          <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
            <p className="text-xs font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-wide mb-4">
              Race time predictions
            </p>
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
              {predictions.map(p => (
                <div key={p.label} className="rounded-lg p-3 bg-slate-50 dark:bg-slate-800">
                  <p className="text-[10px] font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-wide">{p.label}</p>
                  <p className="text-lg font-bold text-slate-800 dark:text-slate-100 leading-tight mt-0.5">{fmtMs(p.ms)}</p>
                  <p className="text-[10px] text-slate-400 dark:text-slate-500 mt-0.5">{fmtPace(p.ms, p.dist, imp)}</p>
                </div>
              ))}
            </div>
          </div>
        </>
      )}

      <p className="text-xs text-slate-400 dark:text-slate-500 text-center">
        Riegel endurance formula (1977) · Best-paced recent activity used as reference · Assumes optimal conditions
      </p>
    </div>
  );
}
