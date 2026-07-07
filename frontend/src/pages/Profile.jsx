import { useState, useEffect, useRef } from "react";
import { useUnits, useChartTheme } from "../settings";
import { fmtDist as uFmtDist } from "../units";
import { Check, Camera, ArrowRight } from "lucide-react";
import { Link } from "react-router-dom";
import {
  PieChart, Pie, Cell, Tooltip, ResponsiveContainer,
} from "recharts";
import { api } from "../api/client";

const SPORT_COLOR = {
  run:  "#f97316",
  ride: "#3b82f6",
  walk: "#10b981",
  hike: "#a16207",
  swim: "#06b6d4",
};

const DIST_LABEL = {
  400: "400 m", 1000: "1 km", 1609: "1 mi",
  5000: "5 km", 10000: "10 km", 15000: "15 km",
  21097: "Half", 42195: "Marathon",
};

function initials(name) {
  if (!name) return "?";
  return name.split(" ").filter(Boolean).map(w => w[0]).join("").slice(0, 2).toUpperCase();
}

// fmtDistance is called in both static sub-components and the main component.
// Sub-components receive `imp` as a prop; the main component injects it from useUnits().
function fmtDistance(m, imp = false) { return uFmtDist(m, imp); }

function fmtTime(ms) {
  if (!ms) return "—";
  const h = Math.floor(ms / 3600000);
  const m = Math.floor((ms % 3600000) / 60000);
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}

function fmtMs(ms) {
  const s = Math.round(ms / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0) return `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return `${m}:${String(sec).padStart(2, "0")}`;
}

function computeEddington(activities) {
  const dists = activities.map(a => (a.distance_meters ?? 0) / 1000).sort((a, b) => b - a);
  let e = 0;
  for (let i = 0; i < dists.length; i++) {
    if (dists[i] >= i + 1) e = i + 1; else break;
  }
  return e;
}

// ─── Sub-components ────────────────────────────────────────────────────────────

function StatChip({ label, value }) {
  return (
    <div className="flex-1 bg-slate-50 dark:bg-slate-950 rounded-xl px-4 py-3 text-center">
      <p className="text-xl font-bold text-slate-800 dark:text-slate-100">{value}</p>
      <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">{label}</p>
    </div>
  );
}

function SportBreakdown({ bySport }) {
  const ct = useChartTheme();
  if (!bySport?.length) return null;
  const slices = bySport.map((s, i) => ({
    name: s.sport_type,
    value: s.distance_meters ?? 0,
    color: SPORT_COLOR[s.sport_type] ?? "#94a3b8",
  }));
  return (
    <section>
      <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-3">Sport breakdown</h2>
      <div className="flex items-center gap-4">
        <ResponsiveContainer width={100} height={100}>
          <PieChart>
            <Pie data={slices} dataKey="value" cx="50%" cy="50%" innerRadius={28} outerRadius={46} paddingAngle={2}>
              {slices.map((s, i) => <Cell key={i} fill={s.color} />)}
            </Pie>
            <Tooltip formatter={v => [fmtDistance(v)]} contentStyle={ct.tooltipStyle} />
          </PieChart>
        </ResponsiveContainer>
        <div className="flex-1 space-y-1.5">
          {slices.map(s => (
            <div key={s.name} className="flex items-center gap-2 text-sm">
              <span className="w-2.5 h-2.5 rounded-full shrink-0" style={{ background: s.color }} />
              <span className="capitalize text-slate-600 dark:text-slate-300 flex-1">{s.name}</span>
              <span className="text-slate-400 dark:text-slate-500 text-xs">{fmtDistance(s.value)}</span>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function PRHighlights({ records }) {
  const highlights = records.slice(0, 4);
  if (!highlights.length) return null;
  return (
    <section>
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200">Personal records</h2>
        <Link to="/records" className="text-xs text-blue-500 hover:text-blue-700 flex items-center gap-0.5">
          All records <ArrowRight size={11} />
        </Link>
      </div>
      <div className="grid grid-cols-2 gap-2">
        {highlights.map(g => (
          <div key={g.distance_meters} className="bg-slate-50 dark:bg-slate-950 rounded-xl px-3 py-2.5">
            <p className="text-xs text-slate-400 dark:text-slate-500">{DIST_LABEL[g.distance_meters] ?? `${g.distance_meters} m`}</p>
            <p className="text-base font-bold text-slate-800 dark:text-slate-100 mt-0.5">{fmtMs(g.efforts[0].elapsed_ms)}</p>
          </div>
        ))}
      </div>
    </section>
  );
}

function fmtMonth(iso) {
  return new Date(iso + "T00:00:00").toLocaleDateString("en-GB", { month: "long", year: "numeric" });
}

function fmtWeek(iso) {
  const d = new Date(iso + "T00:00:00");
  const end = new Date(d);
  end.setDate(d.getDate() + 6);
  const opts = { day: "numeric", month: "short" };
  return `${d.toLocaleDateString("en-GB", opts)} – ${end.toLocaleDateString("en-GB", opts)}`;
}

function BestStats({ bests }) {
  if (!bests) return null;
  const { best_week, best_month } = bests;
  if (!best_week && !best_month) return null;
  return (
    <section>
      <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-3">Personal bests</h2>
      <div className="space-y-2">
        {best_week && (
          <div className="bg-slate-50 dark:bg-slate-950 rounded-xl px-4 py-3">
            <p className="text-xs text-slate-400 dark:text-slate-500 mb-0.5">Best week</p>
            <p className="text-lg font-bold text-slate-800 dark:text-slate-100">{fmtDistance(best_week.distance_meters)}</p>
            <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">{fmtWeek(best_week.period_start)}</p>
          </div>
        )}
        {best_month && (
          <div className="bg-slate-50 dark:bg-slate-950 rounded-xl px-4 py-3">
            <p className="text-xs text-slate-400 dark:text-slate-500 mb-0.5">Best month</p>
            <p className="text-lg font-bold text-slate-800 dark:text-slate-100">{fmtDistance(best_month.distance_meters)}</p>
            <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">{fmtMonth(best_month.period_start)}</p>
          </div>
        )}
      </div>
    </section>
  );
}

function ActiveGear({ gear }) {
  if (!gear.length) return null;
  return (
    <section>
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200">Active gear</h2>
        <Link to="/gear" className="text-xs text-blue-500 hover:text-blue-700 flex items-center gap-0.5">
          Manage <ArrowRight size={11} />
        </Link>
      </div>
      <div className="space-y-2">
        {gear.slice(0, 4).map(g => (
          <div key={g.id} className="flex items-center justify-between text-sm">
            <span className="text-slate-700 dark:text-slate-200 truncate">{g.name}</span>
            <span className="text-slate-400 dark:text-slate-500 text-xs shrink-0 ml-3">{fmtDistance(g.total_distance_meters)}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

// ─── Main page ─────────────────────────────────────────────────────────────────

export default function Profile() {
  const [units] = useUnits();
  const imp = units === "imperial";
  const [profile, setProfile]         = useState(null);
  const [displayName, setDisplayName] = useState("");
  const [email, setEmail]             = useState("");
  const [startedAt, setStartedAt]     = useState("");
  const [avatarUrl, setAvatarUrl]     = useState(null);
  const [saved, setSaved]             = useState(false);
  const [loading, setLoading]         = useState(true);
  const [saving, setSaving]           = useState(false);
  const [uploading, setUploading]     = useState(false);
  const [error, setError]             = useState(null);

  // Stats data
  const [stats, setStats]     = useState(null);
  const [records, setRecords] = useState([]);
  const [gear, setGear]       = useState([]);
  const [eddington, setEddington] = useState(null);
  const [bests, setBests]     = useState(null);

  const fileRef = useRef(null);

  useEffect(() => {
    Promise.all([
      api.profile.get(),
      api.stats.summary({}),
      api.records.list("run"),
      api.gear.list(false),
      api.activities.list({ limit: 1000 }),
      api.stats.bests().catch(() => null),
    ]).then(([p, s, r, g, acts, b]) => {
      setProfile(p);
      setDisplayName(p.display_name ?? "");
      setEmail(p.email ?? "");
      setStartedAt(p.started_at ?? "");
      if (p.avatar_path) setAvatarUrl(api.profile.avatarUrl() + "?t=" + Date.now());
      setStats(s);
      setRecords(r ?? []);
      setGear(g ?? []);
      const activities = acts?.activities ?? acts ?? [];
      setEddington(computeEddington(activities));
      setBests(b);
    }).catch(e => setError(e.message)).finally(() => setLoading(false));
  }, []);

  async function save(e) {
    e.preventDefault();
    setSaving(true);
    setError(null);
    try {
      const p = await api.profile.update({ display_name: displayName, email, started_at: startedAt });
      setProfile(p);
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  async function onAvatarFile(e) {
    const file = e.target.files?.[0];
    if (!file) return;
    setUploading(true);
    setError(null);
    try {
      await api.profile.uploadAvatar(file);
      setAvatarUrl(api.profile.avatarUrl() + "?t=" + Date.now());
    } catch (err) {
      setError(err.message);
    } finally {
      setUploading(false);
    }
  }

  const letter = initials(displayName || profile?.display_name);

  return (
    <div className="max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Profile</h1>
        <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">Your athlete identity</p>
      </div>

      {loading && <div className="text-sm text-slate-400 dark:text-slate-500 animate-pulse">Loading…</div>}

      {!loading && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">

          {/* ── Left column: identity form ── */}
          <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 p-6 space-y-5">
            {/* Avatar */}
            <div className="flex items-center gap-4">
              <div className="relative group shrink-0">
                {avatarUrl ? (
                  <img src={avatarUrl} alt="Profile" className="w-16 h-16 rounded-full object-cover border-2 border-slate-200 dark:border-slate-700" />
                ) : (
                  <div className="w-16 h-16 rounded-full bg-blue-600 flex items-center justify-center text-white text-xl font-bold select-none">
                    {letter}
                  </div>
                )}
                <button
                  type="button"
                  onClick={() => fileRef.current?.click()}
                  disabled={uploading}
                  className="absolute inset-0 rounded-full bg-black/40 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity cursor-pointer disabled:cursor-wait"
                >
                  <Camera size={18} className="text-white" />
                </button>
                <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" className="hidden" onChange={onAvatarFile} />
              </div>
              <div>
                <p className="font-semibold text-slate-800 dark:text-slate-100">{profile?.display_name || "Local Athlete"}</p>
                {startedAt ? (
                  <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">
                    Training since {new Date(startedAt + "T00:00:00").toLocaleDateString(undefined, { month: "long", year: "numeric" })}
                  </p>
                ) : null}
                {uploading && <p className="text-xs text-blue-500 mt-0.5">Uploading…</p>}
              </div>
            </div>

            {/* Form */}
            <form onSubmit={save} className="space-y-3">
              <div>
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1">Display name</label>
                <input
                  value={displayName}
                  onChange={e => setDisplayName(e.target.value)}
                  className="w-full border border-slate-200 dark:border-slate-700 rounded-xl px-3 py-2 text-sm dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="Your name"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1">Email</label>
                <input
                  type="email"
                  value={email}
                  onChange={e => setEmail(e.target.value)}
                  className="w-full border border-slate-200 dark:border-slate-700 rounded-xl px-3 py-2 text-sm dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="you@example.com"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1">Training since</label>
                <input
                  type="date"
                  value={startedAt}
                  onChange={e => setStartedAt(e.target.value)}
                  className="w-full border border-slate-200 dark:border-slate-700 rounded-xl px-3 py-2 text-sm dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>

              {error && <p className="text-xs text-red-600">{error}</p>}

              <button
                type="submit"
                disabled={saving}
                className="flex items-center gap-1.5 px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-xl transition-colors disabled:opacity-60"
              >
                {saved ? <><Check size={14} /> Saved</> : saving ? "Saving…" : "Save changes"}
              </button>
            </form>
          </div>

          {/* ── Right column: stats widgets ── */}
          <div className="space-y-5">
            {/* All-time stats */}
            {stats && (
              <section>
                <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-3">All time</h2>
                <div className="flex gap-2">
                  <StatChip label="Distance" value={fmtDistance(stats.total_distance_meters, imp)} />
                  <StatChip label="Activities" value={stats.total_activities ?? 0} />
                  <StatChip label="Time" value={fmtTime(stats.total_elapsed_ms)} />
                </div>
              </section>
            )}

            {/* Eddington */}
            {eddington != null && (
              <section className="bg-slate-50 dark:bg-slate-950 rounded-xl px-4 py-3 flex items-center gap-3">
                <div>
                  <p className="text-3xl font-bold text-slate-800 dark:text-slate-100">{eddington}</p>
                  <p className="text-xs text-slate-400 dark:text-slate-500">Eddington number</p>
                </div>
                <p className="text-xs text-slate-500 dark:text-slate-400 ml-2">
                  {eddington > 0
                    ? `${eddington} activities of ≥ ${eddington} km`
                    : "Complete activities ≥ 1 km to build your Eddington number"}
                </p>
              </section>
            )}

            {/* Personal bests */}
            {bests && <BestStats bests={bests} />}

            {/* Sport breakdown */}
            {stats?.by_sport?.length > 0 && (
              <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 p-4">
                <SportBreakdown bySport={stats.by_sport} />
              </div>
            )}

            {/* PR highlights */}
            {records.length > 0 && (
              <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 p-4">
                <PRHighlights records={records} />
              </div>
            )}

            {/* Active gear */}
            {gear.length > 0 && (
              <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 p-4">
                <ActiveGear gear={gear} />
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
