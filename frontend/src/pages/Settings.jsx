import { useState, useEffect } from "react";
import { ExternalLink } from "lucide-react";
import { MAP_STYLES, ROUTE_COLORS } from "./ActivityDetail";
import { useMapStyle, useProtoMapsKey, useRouteColor, useUnits, useGradient, useAccent, useGradientStrength, useTagDisplay, useAISettings, useCalendarFillMode } from "../settings";
import { api } from "../api/client";

const STYLE_EMOJI = {
  osm:            "🗺️",
  "pm-light":     "🌤️",
  "pm-dark":      "🌒",
  "pm-white":     "⬜",
  "pm-grayscale": "🩶",
};

export default function Settings() {
  const [mapStyleId, setMapStyle] = useMapStyle();
  const [protoMapsKey, setProtoMapsKey] = useProtoMapsKey();
  const [routeColor, setRouteColor] = useRouteColor();
  const [units, setUnits] = useUnits();
  const [gradient, setGradient]           = useGradient();
  const [gradientStrength, setGradientStrength] = useGradientStrength();
  const [accent, setAccent]               = useAccent();
  const [tagDisplay, setTagDisplay] = useTagDisplay();
  const { endpoint: aiEndpoint, setEndpoint: setAiEndpoint,
          apiKey: aiKey, setApiKey: setAiKey,
          model: aiModel, setModel: setAiModel } = useAISettings();
  const [calFillMode, setCalFillMode] = useCalendarFillMode();
  const [keyDraft, setKeyDraft] = useState(protoMapsKey);
  const [keySaved, setKeySaved] = useState(false);

  // Server-side settings (stored in DB)
  const [stravaClientId, setStravaClientId]         = useState("");
  const [stravaClientSecret, setStravaClientSecret] = useState("");
  const [stravaSecretSet, setStravaSecretSet]       = useState(false);
  const [stravaSaved, setStravaSaved]               = useState(false);
  const [stravaError, setStravaError]               = useState("");

  const [hrMax, setHrMax]       = useState("");
  const [hrMaxSaved, setHrMaxSaved] = useState(false);
  const [hrMaxError, setHrMaxError] = useState("");

  const [authEnabled,    setAuthEnabled]    = useState(false);
  const [authPasswordSet, setAuthPasswordSet] = useState(false);
  const [authPassword,   setAuthPassword]   = useState("");
  const [authConfirm,    setAuthConfirm]    = useState("");
  const [authSaved,      setAuthSaved]      = useState(false);
  const [authError,      setAuthError]      = useState("");

  useEffect(() => {
    if (!MAP_STYLES.some((style) => style.id === mapStyleId)) {
      setMapStyle(MAP_STYLES[0].id);
    }
  }, [mapStyleId, setMapStyle]);

  useEffect(() => {
    api.settings.get().then((s) => {
      setStravaClientId(s.strava_client_id ?? "");
      setStravaSecretSet(s.strava_client_secret_set ?? false);
      setHrMax(s.hr_max ?? "");
      setAuthEnabled(s.auth_enabled ?? false);
      setAuthPasswordSet(s.auth_password_set ?? false);
      // Seed localStorage from DB if the key is missing locally
      if (s.protomaps_key && !protoMapsKey) {
        setProtoMapsKey(s.protomaps_key);
        setKeyDraft(s.protomaps_key);
      }
    }).catch(() => {});
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  async function saveStravaCredentials(e) {
    e.preventDefault();
    setStravaError("");
    try {
      const body = {};
      if (stravaClientId.trim())     body.strava_client_id     = stravaClientId.trim();
      if (stravaClientSecret.trim()) body.strava_client_secret = stravaClientSecret.trim();
      const res = await api.settings.save(body);
      setStravaClientId(res.strava_client_id ?? "");
      setStravaSecretSet(res.strava_client_secret_set ?? false);
      setStravaClientSecret("");
      setStravaSaved(true);
      setTimeout(() => setStravaSaved(false), 2000);
    } catch (err) {
      setStravaError(err.message);
    }
  }

  async function saveHrMax(e) {
    e.preventDefault();
    setHrMaxError("");
    const val = parseInt(hrMax, 10);
    if (isNaN(val) || val < 100 || val > 230) {
      setHrMaxError("Enter a value between 100 and 230 bpm.");
      return;
    }
    try {
      await api.settings.save({ hr_max: String(val) });
      setHrMaxSaved(true);
      setTimeout(() => setHrMaxSaved(false), 2000);
    } catch (err) {
      setHrMaxError(err.message);
    }
  }

  async function saveKey(e) {
    e.preventDefault();
    const trimmed = keyDraft.trim();
    setProtoMapsKey(trimmed);
    try {
      await api.settings.save({ protomaps_key: trimmed });
    } catch {
      // non-fatal — key is already saved to localStorage
    }
    setKeySaved(true);
    setTimeout(() => setKeySaved(false), 2000);
  }

  async function saveAuth(e) {
    e.preventDefault();
    setAuthError("");
    if (authPassword && authPassword !== authConfirm) {
      setAuthError("Passwords do not match.");
      return;
    }
    if (authEnabled && !authPasswordSet && !authPassword) {
      setAuthError("Set a password before enabling authentication.");
      return;
    }
    try {
      const body = { auth_enabled: authEnabled };
      if (authPassword) body.auth_password = authPassword;
      const res = await api.settings.save(body);
      setAuthEnabled(res.auth_enabled ?? false);
      setAuthPasswordSet(res.auth_password_set ?? false);
      setAuthPassword("");
      setAuthConfirm("");
      setAuthSaved(true);
      setTimeout(() => setAuthSaved(false), 2000);
    } catch (err) {
      setAuthError(err.message);
    }
  }

  const rasterStyles   = MAP_STYLES.filter((s) => s.type === "raster");
  const protomapsStyles = MAP_STYLES.filter((s) => s.type === "protomaps");
  const hasProtoKey = Boolean(protoMapsKey);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Settings</h1>
        <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">Appearance and display preferences</p>
      </div>

      {/* Appearance */}
      <section className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5 space-y-5">
        <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">Appearance</p>

        {/* Background gradient */}
        <div>
          <p className="text-xs font-medium text-slate-500 dark:text-slate-400 mb-2">Background gradient</p>
          <div className="flex gap-2 flex-wrap">
            {[
              { id: "indigo", label: "Indigo",  from: "#6366f1", to: "#a855f7" },
              { id: "emerald",label: "Emerald", from: "#10b981", to: "#059669" },
              { id: "amber",  label: "Amber",   from: "#f59e0b", to: "#d97706" },
              { id: "rose",   label: "Rose",    from: "#f43f5e", to: "#e11d48" },
              { id: "mono",   label: "None",    from: "#94a3b8", to: "#64748b" },
            ].map(g => (
              <button
                key={g.id}
                title={g.label}
                onClick={() => setGradient(g.id)}
                className={[
                  "w-9 h-9 rounded-lg border-2 transition-all hover:scale-110",
                  gradient === g.id ? "border-slate-800 dark:border-white scale-110" : "border-transparent",
                ].join(" ")}
                style={{ background: `linear-gradient(135deg, ${g.from}, ${g.to})` }}
              />
            ))}
          </div>
          {gradient !== "mono" && (
            <div className="mt-3 flex items-center gap-3">
              <span className="text-xs text-slate-400 dark:text-slate-500 w-14 shrink-0">Intensity</span>
              <input
                type="range"
                min={0}
                max={100}
                step={5}
                value={gradientStrength}
                onChange={(e) => setGradientStrength(Number(e.target.value))}
                className="flex-1 accent-[var(--accent)]"
              />
              <span className="text-xs text-slate-500 dark:text-slate-400 w-8 text-right">{gradientStrength}%</span>
            </div>
          )}
        </div>

        {/* Accent color */}
        <div>
          <p className="text-xs font-medium text-slate-500 dark:text-slate-400 mb-2">Accent color</p>
          <div className="flex gap-2 flex-wrap">
            {[
              { id: "blue",   label: "Blue",    hex: "#3b82f6" },
              { id: "violet", label: "Violet",  hex: "#7c3aed" },
              { id: "emerald",label: "Emerald", hex: "#10b981" },
              { id: "rose",   label: "Rose",    hex: "#f43f5e" },
              { id: "amber",  label: "Amber",   hex: "#f59e0b" },
            ].map(a => (
              <button
                key={a.id}
                title={a.label}
                onClick={() => setAccent(a.id)}
                className={[
                  "w-9 h-9 rounded-full border-2 transition-all hover:scale-110",
                  accent === a.id ? "border-slate-800 dark:border-white scale-110" : "border-transparent",
                ].join(" ")}
                style={{ background: a.hex }}
              />
            ))}
          </div>
        </div>
      </section>

      {/* Tag display */}
      <section className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5 space-y-3">
        <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">Tag display</p>
        <p className="text-xs text-slate-400 dark:text-slate-500">
          Activities imported from Nike Run Club include many auto-generated tags. Choose what to show.
        </p>
        <div className="flex gap-2">
          {[
            { id: "all",       label: "Show all tags" },
            { id: "user-only", label: "User-added only" },
          ].map((opt) => (
            <button
              key={opt.id}
              onClick={() => setTagDisplay(opt.id)}
              className={[
                "px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors",
                tagDisplay === opt.id
                  ? "bg-slate-800 dark:bg-slate-200 text-white dark:text-slate-900 border-slate-800 dark:border-slate-200"
                  : "bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-transparent hover:bg-slate-200 dark:hover:bg-slate-700",
              ].join(" ")}
            >
              {opt.label}
            </button>
          ))}
        </div>
      </section>

      {/* Calendar fill color */}
      <section className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5 space-y-3">
        <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">Calendar fill color</p>
        <p className="text-xs text-slate-400 dark:text-slate-500">
          Choose the water fill color on the monthly calendar cells.
        </p>
        <div className="flex gap-2">
          {[
            { id: "sport",  label: "Sport color" },
            { id: "accent", label: "Accent color" },
          ].map((opt) => (
            <button
              key={opt.id}
              onClick={() => setCalFillMode(opt.id)}
              className={[
                "px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors",
                calFillMode === opt.id
                  ? "bg-slate-800 dark:bg-slate-200 text-white dark:text-slate-900 border-slate-800 dark:border-slate-200"
                  : "bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-transparent hover:bg-slate-200 dark:hover:bg-slate-700",
              ].join(" ")}
            >
              {opt.label}
            </button>
          ))}
        </div>
      </section>

      {/* Strava app credentials */}
      <section className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
        <div className="flex items-center justify-between mb-1">
          <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">Strava app credentials</p>
          <a
            href="https://www.strava.com/settings/api"
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-1 text-xs text-blue-500 hover:text-blue-700"
          >
            Create API app <ExternalLink size={11} />
          </a>
        </div>
        <p className="text-xs text-slate-400 dark:text-slate-500 mb-3">
          Required to connect your Strava account. Set the callback domain to{" "}
          <code className="bg-slate-100 dark:bg-slate-800 px-1 rounded">localhost</code> in the Strava API settings.
        </p>
        <form onSubmit={saveStravaCredentials} className="space-y-2">
          <input
            type="text"
            value={stravaClientId}
            onChange={(e) => setStravaClientId(e.target.value)}
            placeholder="Client ID"
            className="w-full text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <input
            type="password"
            value={stravaClientSecret}
            onChange={(e) => setStravaClientSecret(e.target.value)}
            placeholder={stravaSecretSet ? "Client Secret (saved — paste to update)" : "Client Secret"}
            className="w-full text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          {stravaError && <p className="text-xs text-red-500">{stravaError}</p>}
          <div className="flex items-center gap-2 pt-1">
            <button
              type="submit"
              className="px-4 py-2 text-sm font-medium btn-accent rounded-lg transition-colors"
            >
              {stravaSaved ? "Saved ✓" : "Save"}
            </button>
            {(stravaClientId || stravaSecretSet) && (
              <span className="text-xs text-slate-400 dark:text-slate-500">
                {stravaSecretSet ? "Secret saved" : "Secret not set"}
              </span>
            )}
          </div>
        </form>
      </section>

      {/* Protomaps API key */}
      <section className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
        <div className="flex items-center justify-between mb-1">
          <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">Protomaps API key</p>
          <a
            href="https://protomaps.com/dashboard"
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-1 text-xs text-blue-500 hover:text-blue-700"
          >
            Get a free key <ExternalLink size={11} />
          </a>
        </div>
        <p className="text-xs text-slate-400 dark:text-slate-500 mb-3">
          Required to use Protomaps vector tile styles. Free tier is generous for personal use.
        </p>
        <form onSubmit={saveKey} className="flex gap-2">
          <input
            type="password"
            value={keyDraft}
            onChange={(e) => setKeyDraft(e.target.value)}
            placeholder="Paste your Protomaps API key…"
            className="flex-1 text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <button
            type="submit"
            className="px-4 py-2 text-sm font-medium btn-accent rounded-lg transition-colors"
          >
            {keySaved ? "Saved ✓" : "Save"}
          </button>
          {protoMapsKey && (
            <button
              type="button"
              onClick={() => { setProtoMapsKey(""); setKeyDraft(""); }}
              className="px-3 py-2 text-sm text-slate-500 dark:text-slate-400 border border-slate-200 dark:border-slate-700 rounded-lg hover:bg-slate-50 dark:hover:bg-slate-800/50"
            >
              Clear
            </button>
          )}
        </form>
      </section>

      {/* Map style */}
      <section className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
        <p className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-1">Map style</p>
        <p className="text-xs text-slate-400 dark:text-slate-500 mb-4">
          Choose the tile layer for route maps. Protomaps styles require an API key above.
        </p>

        <p className="text-xs font-medium text-slate-500 dark:text-slate-400 uppercase tracking-widest mb-2">Standard</p>
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-2 mb-4">
          {rasterStyles.map((style) => (
            <StyleButton key={style.id} style={style} active={mapStyleId === style.id} onSelect={setMapStyle} emoji={STYLE_EMOJI[style.id]} />
          ))}
        </div>

        <p className="text-xs font-medium text-slate-500 dark:text-slate-400 uppercase tracking-widest mb-2">
          Protomaps {!hasProtoKey && <span className="text-amber-500 normal-case font-normal ml-1">— API key required</span>}
        </p>
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
          {protomapsStyles.map((style) => (
            <StyleButton
              key={style.id}
              style={style}
              active={mapStyleId === style.id}
              onSelect={setMapStyle}
              emoji={STYLE_EMOJI[style.id]}
              disabled={!hasProtoKey}
            />
          ))}
        </div>
      </section>

      {/* Units */}
      <section className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
        <p className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-1">Units</p>
        <p className="text-xs text-slate-400 dark:text-slate-500 mb-3">Affects distances, pace, elevation, and speed across the entire app.</p>
        <div className="flex rounded-lg border border-slate-200 dark:border-slate-700 overflow-hidden text-sm font-medium w-fit">
          {[["metric", "Metric (km, m)"], ["imperial", "Imperial (mi, ft)"]].map(([val, label]) => (
            <button
              key={val}
              onClick={() => setUnits(val)}
              className={[
                "px-4 py-2 transition-colors",
                units === val ? "bg-slate-800 text-white" : "text-slate-500 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800/50",
              ].join(" ")}
            >
              {label}
            </button>
          ))}
        </div>
      </section>

      {/* Route color */}
      <section className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
        <p className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-1">Route color</p>
        <p className="text-xs text-slate-400 dark:text-slate-500 mb-3">Color of the route line on activity maps.</p>
        <div className="flex gap-3">
          {ROUTE_COLORS.map((c) => (
            <button
              key={c.id}
              title={c.label}
              onClick={() => setRouteColor(c.hex)}
              className={[
                "w-8 h-8 rounded-full border-2 transition-transform hover:scale-110",
                routeColor === c.hex ? "border-slate-800 scale-110" : "border-transparent",
              ].join(" ")}
              style={{ background: c.hex }}
            />
          ))}
        </div>
      </section>

      {/* HR zones */}
      <section className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
        <p className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-1">Max heart rate</p>
        <p className="text-xs text-slate-400 dark:text-slate-500 mb-3">
          Used to calculate HR zones on activity detail pages. Rule of thumb: 220 − your age.
        </p>
        <form onSubmit={saveHrMax} className="flex gap-2 items-start">
          <div className="relative">
            <input
              type="number"
              value={hrMax}
              onChange={(e) => setHrMax(e.target.value)}
              placeholder="e.g. 185"
              min={100}
              max={230}
              className="w-32 text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 pr-10 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
            <span className="absolute right-3 top-2 text-xs text-slate-400 dark:text-slate-500 pointer-events-none">bpm</span>
          </div>
          <button
            type="submit"
            className="px-4 py-2 text-sm font-medium btn-accent rounded-lg transition-colors"
          >
            {hrMaxSaved ? "Saved ✓" : "Save"}
          </button>
        </form>
        {hrMaxError && <p className="mt-2 text-xs text-red-500">{hrMaxError}</p>}
      </section>

      {/* Password protection */}
      <section className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
        <div className="flex items-center justify-between mb-1">
          <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">Password protection</p>
          {authSaved && <span className="text-xs text-emerald-500">Saved ✓</span>}
        </div>
        <p className="text-xs text-slate-400 dark:text-slate-500 mb-4">
          When enabled, a password is required to access the app. Useful if you expose the app beyond localhost.
          The password is stored as a bcrypt hash — never in plain text.
        </p>
        <form onSubmit={saveAuth} className="space-y-3">
          <label className="flex items-center gap-3 cursor-pointer">
            <div
              onClick={() => setAuthEnabled((v) => !v)}
              className={`relative w-10 h-6 rounded-full transition-colors ${authEnabled ? "bg-blue-600" : "bg-slate-200 dark:bg-slate-700"}`}
            >
              <span className={`absolute top-1 left-1 w-4 h-4 bg-white dark:bg-slate-900 rounded-full shadow transition-transform ${authEnabled ? "translate-x-4" : ""}`}/>
            </div>
            <span className="text-sm text-slate-700 dark:text-slate-200">
              {authEnabled ? "Authentication enabled" : "Authentication disabled"}
            </span>
          </label>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs text-slate-500 dark:text-slate-400 block mb-1">
                {authPasswordSet ? "New password (leave blank to keep)" : "Set password"}
              </label>
              <input
                type="password"
                value={authPassword}
                onChange={(e) => setAuthPassword(e.target.value)}
                placeholder={authPasswordSet ? "••••••••" : "Choose a password"}
                className="w-full text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="text-xs text-slate-500 dark:text-slate-400 block mb-1">Confirm password</label>
              <input
                type="password"
                value={authConfirm}
                onChange={(e) => setAuthConfirm(e.target.value)}
                placeholder="Repeat password"
                className="w-full text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>

          {authError && <p className="text-xs text-red-500">{authError}</p>}

          <button
            type="submit"
            className="px-4 py-2 text-sm font-medium btn-accent rounded-lg transition-colors"
          >
            Save
          </button>
        </form>
      </section>

      {/* AI Assistant */}
      <section className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5 space-y-3">
        <div>
          <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">AI Assistant</p>
          <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">
            Connect any OpenAI-compatible endpoint. Once configured, the AI button appears bottom-right.
          </p>
        </div>

        {/* Quick presets */}
        <div>
          <p className="text-xs text-slate-500 dark:text-slate-400 mb-1.5">Quick setup</p>
          <div className="flex flex-wrap gap-2">
            {[
              { label: "Ollama (local)", endpoint: "http://localhost:11434/v1", model: "llama3.2", key: "" },
              { label: "LM Studio",     endpoint: "http://localhost:1234/v1",   model: "local-model", key: "" },
              { label: "ChatGPT",       endpoint: "https://api.openai.com/v1",  model: "gpt-4o", key: "" },
            ].map((p) => (
              <button
                key={p.label}
                onClick={() => { setAiEndpoint(p.endpoint); setAiModel(p.model); if (p.key !== undefined) setAiKey(p.key); }}
                className="px-3 py-1.5 text-xs rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800 text-slate-600 dark:text-slate-300 hover:border-slate-400 dark:hover:border-slate-500 transition-colors"
              >
                {p.label}
              </button>
            ))}
          </div>
          <p className="text-[11px] text-slate-400 dark:text-slate-600 mt-1.5">
            Claude / Anthropic uses a different API format — not OpenAI-compatible. Use an OpenAI proxy or Ollama with a Claude-derivative model instead.
          </p>
        </div>

        <div className="space-y-2">
          <div>
            <label className="text-xs text-slate-500 dark:text-slate-400 block mb-1">Endpoint (OpenAI-compatible)</label>
            <input
              type="url"
              value={aiEndpoint}
              onChange={(e) => setAiEndpoint(e.target.value)}
              placeholder="http://localhost:11434/v1"
              className="w-full text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-xs text-slate-500 dark:text-slate-400 block mb-1">Model</label>
              <input
                type="text"
                value={aiModel}
                onChange={(e) => setAiModel(e.target.value)}
                placeholder="llama3.2"
                className="w-full text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="text-xs text-slate-500 dark:text-slate-400 block mb-1">API Key (optional)</label>
              <input
                type="password"
                value={aiKey}
                onChange={(e) => setAiKey(e.target.value)}
                placeholder="sk-… or leave blank for local"
                className="w-full text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>
        </div>
      </section>

    </div>
  );
}

function StyleButton({ style, active, onSelect, emoji, disabled }) {
  return (
    <button
      onClick={() => !disabled && onSelect(style.id)}
      disabled={disabled}
      className={[
        "flex items-center gap-2 px-4 py-3 rounded-xl border text-sm font-medium text-left transition-colors",
        active
          ? "border-blue-500 bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400"
          : disabled
            ? "border-slate-100 dark:border-slate-800 bg-slate-50 dark:bg-slate-950 text-slate-300 dark:text-slate-600 cursor-not-allowed"
            : "border-slate-200 dark:border-slate-700 hover:border-slate-300 dark:hover:border-slate-600 text-slate-600 dark:text-slate-300",
      ].join(" ")}
    >
      <span className="text-lg">{emoji}</span>
      <span className="text-xs leading-tight">{style.label}</span>
    </button>
  );
}
