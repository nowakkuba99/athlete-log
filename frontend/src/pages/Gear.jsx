import { useState, useEffect } from "react";
import { Plus, Pencil, Trash2, Check, X, AlertTriangle, Archive } from "lucide-react";
import { api } from "../api/client";
import { useUnits } from "../settings";
import { fmtDist as uFmtDist } from "../units";

const GEAR_TYPES = [
  { v: "shoe",    l: "Shoe",    emoji: "👟" },
  { v: "bike",    l: "Bike",    emoji: "🚲" },
  { v: "wetsuit", l: "Wetsuit", emoji: "🏊" },
  { v: "other",   l: "Other",   emoji: "🎽" },
];

function typeEmoji(t) {
  return GEAR_TYPES.find(g => g.v === t)?.emoji ?? "🎽";
}

function fmtKm(m, imp = false) { return uFmtDist(m, imp); }

function GearModal({ gear, onSave, onClose }) {
  const editing = !!gear;
  const [name, setName]       = useState(gear?.name ?? "");
  const [type, setType]       = useState(gear?.type ?? "shoe");
  const [brand, setBrand]     = useState(gear?.brand_model ?? "");
  const [date, setDate]       = useState(gear?.purchase_date ?? "");
  const [alert, setAlert]     = useState(gear?.distance_alert_meters ? String(Math.round(gear.distance_alert_meters / 1000)) : "");
  const [retire, setRetire]   = useState(false);
  const [saving, setSaving]   = useState(false);
  const [error, setError]     = useState(null);

  async function submit(e) {
    e.preventDefault();
    if (!name.trim()) return;
    setSaving(true);
    setError(null);
    try {
      const alertM = alert ? parseInt(alert, 10) * 1000 : null;
      const body = {
        name: name.trim(),
        type,
        brand_model: brand.trim(),
        purchase_date: date || null,
        distance_alert_meters: alertM,
        retire,
      };
      await onSave(body);
      onClose();
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center" onClick={onClose}>
      <div className="absolute inset-0 bg-black/20" />
      <div className="relative bg-white dark:bg-slate-900 rounded-2xl shadow-xl w-full max-w-sm p-6" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-5">
          <h3 className="font-semibold text-slate-800 dark:text-slate-100">{editing ? "Edit gear" : "Add gear"}</h3>
          <button onClick={onClose} className="p-1 text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 rounded"><X size={16} /></button>
        </div>

        <form onSubmit={submit} className="space-y-4">
          <div>
            <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1">Type</label>
            <div className="flex gap-2">
              {GEAR_TYPES.map(gt => (
                <button
                  key={gt.v} type="button"
                  onClick={() => setType(gt.v)}
                  className={`flex-1 py-2 rounded-xl text-sm transition-colors ${type === gt.v ? "bg-blue-600 text-white" : "bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700"}`}
                >
                  <span className="block text-base">{gt.emoji}</span>
                  <span className="text-xs">{gt.l}</span>
                </button>
              ))}
            </div>
          </div>

          <div>
            <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1">Name *</label>
            <input
              autoFocus value={name} onChange={e => setName(e.target.value)} required
              placeholder={type === "shoe" ? "e.g. Nike Pegasus 41" : type === "bike" ? "e.g. Canyon Grail" : "Name"}
              className="w-full border border-slate-200 dark:border-slate-700 rounded-xl px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1">Brand / model</label>
            <input
              value={brand} onChange={e => setBrand(e.target.value)}
              placeholder="e.g. Nike"
              className="w-full border border-slate-200 dark:border-slate-700 rounded-xl px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1">Purchase date</label>
              <input
                type="date" value={date} onChange={e => setDate(e.target.value)}
                className="w-full border border-slate-200 dark:border-slate-700 rounded-xl px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1">Alert at (km)</label>
              <input
                type="number" min="1" max="99999" value={alert} onChange={e => setAlert(e.target.value)}
                placeholder="e.g. 800"
                className="w-full border border-slate-200 dark:border-slate-700 rounded-xl px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>

          {editing && (
            <label className="flex items-center gap-2 cursor-pointer select-none">
              <input type="checkbox" checked={retire} onChange={e => setRetire(e.target.checked)} className="rounded border-slate-300 dark:border-slate-600" />
              <span className="text-sm text-slate-600 dark:text-slate-300">Mark as retired</span>
            </label>
          )}

          {error && <p className="text-sm text-red-600">{error}</p>}

          <button
            type="submit" disabled={saving || !name.trim()}
            className="w-full py-2.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-xl transition-colors disabled:opacity-60"
          >
            {saving ? "Saving…" : editing ? "Save changes" : "Add gear"}
          </button>
        </form>
      </div>
    </div>
  );
}

function DistanceBar({ current, alert, imp = false }) {
  if (!alert) return null;
  const pct = Math.min(100, (current / alert) * 100);
  const over = pct >= 100;
  const warn = pct >= 80;
  return (
    <div className="mt-2">
      <div className="flex items-center justify-between text-xs text-slate-400 dark:text-slate-500 mb-1">
        <span>{fmtKm(current, imp)}</span>
        <span className={over ? "text-red-500 font-medium" : warn ? "text-amber-500 font-medium" : ""}>
          / {fmtKm(alert, imp)} limit
        </span>
      </div>
      <div className="h-1.5 rounded-full bg-slate-100 dark:bg-slate-800 overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${over ? "bg-red-500" : warn ? "bg-amber-400" : "bg-blue-400"}`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

export default function Gear() {
  const [gear, setGear]             = useState([]);
  const [units] = useUnits();
  const imp = units === "imperial";
  const [loading, setLoading]       = useState(true);
  const [showModal, setShowModal]   = useState(false);
  const [editTarget, setEditTarget] = useState(null);
  const [showRetired, setShowRetired] = useState(false);

  function load() {
    setLoading(true);
    api.gear.list(showRetired).then(g => setGear(g ?? [])).finally(() => setLoading(false));
  }

  useEffect(() => { load(); }, [showRetired]); // eslint-disable-line react-hooks/exhaustive-deps

  const alerts = gear.filter(g => g.distance_alert_meters && g.total_distance_meters >= g.distance_alert_meters);

  async function create(body) {
    const g = await api.gear.create(body);
    setGear(prev => [...prev, g]);
  }

  async function update(body) {
    const g = await api.gear.update(editTarget.id, body);
    setGear(prev => prev.map(x => x.id === g.id ? g : x).filter(x => showRetired || !x.retired_at));
  }

  async function remove(id) {
    await api.gear.remove(id);
    setGear(prev => prev.filter(g => g.id !== id));
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Gear</h1>
          <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">Shoes, bikes, and equipment</p>
        </div>
        <button
          onClick={() => { setEditTarget(null); setShowModal(true); }}
          className="flex items-center gap-1.5 px-3 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors"
        >
          <Plus size={15} /> Add gear
        </button>
      </div>

      {/* Alert banners */}
      {alerts.map(g => (
        <div key={g.id} className="flex items-start gap-3 px-4 py-3 bg-amber-50 border border-amber-200 rounded-xl text-sm">
          <AlertTriangle size={16} className="text-amber-500 mt-0.5 shrink-0" />
          <span className="text-amber-800">
            <strong>{g.name}</strong> has reached {fmtKm(g.total_distance_meters, imp)} — consider replacing it (limit: {fmtKm(g.distance_alert_meters, imp)}).
          </span>
        </div>
      ))}

      {/* Show retired toggle */}
      <label className="flex items-center gap-2 cursor-pointer select-none text-sm text-slate-500 dark:text-slate-400">
        <input type="checkbox" checked={showRetired} onChange={e => setShowRetired(e.target.checked)} className="rounded border-slate-300 dark:border-slate-600" />
        Show retired gear
      </label>

      {loading && <div className="text-sm text-slate-400 dark:text-slate-500 animate-pulse">Loading…</div>}

      {!loading && gear.length === 0 && (
        <div className="rounded-xl border-2 border-dashed border-slate-200 dark:border-slate-700 p-12 text-center">
          <p className="text-2xl mb-2">👟</p>
          <p className="text-sm font-medium text-slate-500 dark:text-slate-400">No gear yet</p>
          <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">Add your shoes and bikes to track mileage.</p>
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        {gear.map(g => (
          <div key={g.id} className={`bg-white dark:bg-slate-900 rounded-xl border p-5 ${g.retired_at ? "border-slate-100 dark:border-slate-800 opacity-60" : "border-slate-200 dark:border-slate-700"}`}>
            <div className="flex items-start justify-between gap-2">
              <div className="flex items-center gap-3">
                <span className="text-2xl">{typeEmoji(g.type)}</span>
                <div>
                  <div className="flex items-center gap-1.5">
                    <p className="font-semibold text-slate-800 dark:text-slate-100">{g.name}</p>
                    {g.retired_at && (
                      <span className="text-[10px] font-semibold bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-500 px-1.5 py-0.5 rounded uppercase">
                        Retired
                      </span>
                    )}
                  </div>
                  {g.brand_model && <p className="text-xs text-slate-400 dark:text-slate-500">{g.brand_model}</p>}
                </div>
              </div>
              <div className="flex gap-1 shrink-0">
                <button
                  onClick={() => { setEditTarget(g); setShowModal(true); }}
                  className="p-1.5 text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-800"
                >
                  <Pencil size={13} />
                </button>
                <button
                  onClick={() => remove(g.id)}
                  className="p-1.5 text-slate-400 dark:text-slate-500 hover:text-red-500 rounded-lg hover:bg-red-50"
                >
                  <Trash2 size={13} />
                </button>
              </div>
            </div>

            <div className="mt-3 flex items-center gap-4 text-sm text-slate-500 dark:text-slate-400">
              <span className="font-medium text-slate-700 dark:text-slate-200">{fmtKm(g.total_distance_meters, imp)}</span>
              <span className="text-xs text-slate-400 dark:text-slate-500">{g.activity_count} {g.activity_count === 1 ? "activity" : "activities"}</span>
              {g.purchase_date && <span className="text-xs text-slate-400 dark:text-slate-500 ml-auto">Since {g.purchase_date}</span>}
            </div>

            <DistanceBar current={g.total_distance_meters} alert={g.distance_alert_meters} imp={imp} />
          </div>
        ))}
      </div>

      {showModal && (
        <GearModal
          gear={editTarget}
          onSave={editTarget ? update : create}
          onClose={() => { setShowModal(false); setEditTarget(null); }}
        />
      )}
    </div>
  );
}
