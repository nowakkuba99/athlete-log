import { useState, useEffect, useCallback } from "react";
import { Link } from "react-router-dom";
import { RefreshCw, CheckCircle, XCircle, Clock, Zap, AlertCircle, Folder, Trash2, ChevronDown, ChevronUp } from "lucide-react";
import { api } from "../api/client";

function fmtDate(iso) {
  if (!iso) return "Never";
  return new Date(iso).toLocaleString("en-GB", {
    day: "numeric", month: "short", year: "numeric",
    hour: "2-digit", minute: "2-digit",
  });
}

const SOURCE_LABEL = {
  nrc: "Nike Run Club",
  strava: "Strava",
  fit_upload: "FIT uploads",
  gpx_upload: "GPX uploads",
  tcx_upload: "TCX uploads",
  folder_watch: "Watched Folder",
};

const SOURCE_COLOR = {
  nrc: "bg-orange-500",
  strava: "bg-red-500",
  fit_upload: "bg-blue-500",
  gpx_upload: "bg-emerald-500",
  tcx_upload: "bg-purple-500",
  folder_watch: "bg-teal-600",
};

function StatusBadge({ status }) {
  if (!status) return null;
  const map = {
    succeeded: { icon: CheckCircle, color: "text-emerald-600", spin: false, label: "Synced" },
    running:   { icon: RefreshCw,  color: "text-blue-500",    spin: true,  label: "Running" },
    queued:    { icon: Clock,      color: "text-slate-400 dark:text-slate-500",   spin: false, label: "Queued" },
    failed:    { icon: XCircle,    color: "text-red-500",     spin: false, label: "Failed" },
  };
  const cfg = map[status] ?? { icon: Clock, color: "text-slate-400 dark:text-slate-500", spin: false, label: status };
  const Icon = cfg.icon;
  return (
    <span className={`flex items-center gap-1 text-xs font-medium ${cfg.color}`}>
      <Icon size={12} className={cfg.spin ? "animate-spin" : ""} />
      {cfg.label}
    </span>
  );
}

function FolderFileLog({ sourceId }) {
  const [files, setFiles] = useState(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!open) return;
    api.sources.files(sourceId).then(setFiles).catch(() => setFiles([]));
  }, [open, sourceId]);

  const statusColor = { imported: "text-emerald-600", duplicate: "text-slate-400 dark:text-slate-500", error: "text-red-500" };

  return (
    <div className="mt-3 border-t border-slate-100 dark:border-slate-800 pt-3">
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1 text-xs text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 transition-colors"
      >
        {open ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
        {open ? "Hide" : "Show"} file log
      </button>
      {open && (
        <div className="mt-2 space-y-1 max-h-48 overflow-y-auto">
          {files === null && <p className="text-xs text-slate-400 dark:text-slate-500">Loading…</p>}
          {files !== null && files.length === 0 && (
            <p className="text-xs text-slate-400 dark:text-slate-500">No files processed yet.</p>
          )}
          {(files ?? []).map((f) => (
            <div key={f.sha256} className="flex items-center gap-2 text-xs">
              <span className={`font-medium shrink-0 ${statusColor[f.status] ?? "text-slate-500 dark:text-slate-400"}`}>
                {f.status}
              </span>
              <span className="text-slate-500 dark:text-slate-400 truncate" title={f.file_path}>
                {f.file_path.split("/").pop()}
              </span>
              <span className="ml-auto text-slate-300 dark:text-slate-600 shrink-0">
                {new Date(f.synced_at).toLocaleString("en-GB", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function SourceCard({ source, onSync, onDelete }) {
  const [syncing, setSyncing] = useState(false);
  const [jobStatus, setJobStatus] = useState(source.last_sync_status);
  const [jobProgress, setJobProgress] = useState(0);
  const [error, setError] = useState(null);
  const [deleting, setDeleting] = useState(false);

  async function triggerSync() {
    setSyncing(true);
    setError(null);
    setJobProgress(0);
    try {
      const job = await api.sources.sync(source.id);
      setJobStatus("queued");
      const poll = setInterval(async () => {
        try {
          const j = await api.jobs.get(job.id);
          setJobStatus(j.status);
          setJobProgress(j.progress_current || 0);
          if (j.status === "succeeded" || j.status === "failed") {
            clearInterval(poll);
            setSyncing(false);
            if (j.status === "failed") setError(j.error || "Sync failed");
          }
        } catch {
          clearInterval(poll);
          setSyncing(false);
        }
      }, 1500);
    } catch (err) {
      setError(err.message);
      setSyncing(false);
    }
  }

  async function handleDelete() {
    if (!confirm("Remove this watched folder?")) return;
    setDeleting(true);
    try {
      await api.sources.deleteFolder(source.id);
      onDelete?.();
    } catch (err) {
      setError(err.message);
      setDeleting(false);
    }
  }

  const isUpload = source.source.endsWith("_upload");
  const isFolder = source.source === "folder_watch";
  const folderPath = isFolder ? source.metadata?.path ?? "" : "";

  return (
    <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className={`w-9 h-9 rounded-lg ${SOURCE_COLOR[source.source] ?? "bg-slate-400"} flex items-center justify-center`}>
            {isFolder ? <Folder size={16} className="text-white" /> : <Zap size={16} className="text-white" />}
          </div>
          <div>
            <p className="font-medium text-slate-800 dark:text-slate-100">
              {SOURCE_LABEL[source.source] ?? source.source}
            </p>
            <p className="text-xs text-slate-400 dark:text-slate-500 font-mono truncate max-w-xs" title={folderPath || undefined}>
              {isFolder ? folderPath || "No path set" : source.external_athlete_id ? `ID: ${source.external_athlete_id}` : "Connected"}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {!isUpload && (
            <button
              onClick={triggerSync}
              disabled={syncing}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800/50 disabled:opacity-50 transition-colors"
            >
              <RefreshCw size={13} className={syncing ? "animate-spin" : ""} />
              {syncing ? "Scanning…" : isFolder ? "Scan now" : "Sync now"}
            </button>
          )}
          {isFolder && (
            <button
              onClick={handleDelete}
              disabled={deleting}
              className="p-1.5 rounded-lg text-slate-400 dark:text-slate-500 hover:text-red-500 hover:bg-red-50 transition-colors disabled:opacity-50"
              title="Remove folder"
            >
              <Trash2 size={14} />
            </button>
          )}
        </div>
      </div>

      <div className="mt-4 flex items-center justify-between text-xs text-slate-400 dark:text-slate-500">
        <span>Last sync: {fmtDate(source.last_sync_at)}</span>
        <div className="flex flex-col items-end gap-0.5">
          <StatusBadge status={jobStatus} />
          {jobStatus === "running" && (
            <span className="text-slate-400 dark:text-slate-500">
              {jobProgress > 0 ? `${jobProgress} imported` : "Starting…"}
            </span>
          )}
        </div>
      </div>

      {isFolder && <FolderFileLog sourceId={source.id} />}

      {error && (
        <p className="mt-2 text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 rounded-lg px-3 py-2">{error}</p>
      )}
    </div>
  );
}

function NRCConnect({ onConnected }) {
  const [token, setToken] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);

  async function handleSubmit(e) {
    e.preventDefault();
    if (!token.trim()) return;
    setSaving(true);
    setError(null);
    try {
      await api.sources.connectNRC(token.trim());
      setToken("");
      onConnected();
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
      <p className="font-medium text-slate-800 dark:text-slate-100 mb-1">Connect Nike Run Club</p>
      <p className="text-xs text-slate-400 dark:text-slate-500 mb-3">
        Paste your NRC bearer token to import activities.
      </p>

      <div className="flex items-start gap-2 text-xs text-amber-700 bg-amber-50 border border-amber-200 rounded-lg px-3 py-2.5 mb-4">
        <AlertCircle size={14} className="mt-0.5 shrink-0" />
        <span>
          This integration uses Nike's{" "}
          <strong>unofficial, undocumented API</strong> — it may break without
          notice if Nike changes their backend.{" "}
          <a
            href="https://github.com/yasoob/nrc-exporter"
            target="_blank"
            rel="noopener noreferrer"
            className="underline underline-offset-2 hover:text-amber-900"
          >
            API reference
          </a>
        </span>
      </div>

      <form onSubmit={handleSubmit} className="flex gap-2">
        <input
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="Bearer eyJ…"
          className="flex-1 text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <button
          type="submit"
          disabled={saving || !token.trim()}
          className="px-4 py-2 text-sm font-medium btn-accent rounded-lg disabled:opacity-50 transition-colors"
        >
          {saving ? "Saving…" : "Connect"}
        </button>
      </form>
      {error && <p className="mt-2 text-xs text-red-600">{error}</p>}
    </div>
  );
}

function StravaConnect({ configured }) {
  return (
    <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
      <p className="font-medium text-slate-800 dark:text-slate-100 mb-1">Connect Strava</p>
      <p className="text-xs text-slate-400 dark:text-slate-500 mb-4">
        Authorise Athlete Log to read your Strava activities via OAuth.
      </p>
      {configured ? (
        <a
          href={api.sources.connectStravaUrl()}
          className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium bg-orange-500 text-white rounded-lg hover:bg-orange-600 transition-colors"
        >
          <Zap size={14} />
          Connect Strava
        </a>
      ) : (
        <div className="flex items-start gap-2 text-xs text-amber-700 bg-amber-50 border border-amber-200 rounded-lg px-3 py-2.5">
          <AlertCircle size={14} className="mt-0.5 shrink-0" />
          <span>
            Strava API credentials are not configured.{" "}
            <Link to="/settings" className="font-medium underline underline-offset-2 hover:text-amber-900">
              Add them in Settings
            </Link>{" "}
            to enable the OAuth flow.
          </span>
        </div>
      )}
    </div>
  );
}

function FolderWatchConnect({ onConnected }) {
  const [path, setPath] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);

  async function handleSubmit(e) {
    e.preventDefault();
    if (!path.trim()) return;
    setSaving(true);
    setError(null);
    try {
      await api.sources.connectFolder(path.trim());
      setPath("");
      onConnected();
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
      <div className="flex items-center gap-2 mb-1">
        <Folder size={15} className="text-teal-600" />
        <p className="font-medium text-slate-800 dark:text-slate-100">Watch a local folder</p>
      </div>
      <p className="text-xs text-slate-400 dark:text-slate-500 mb-4">
        Athlete Log will automatically import any <code className="bg-slate-100 dark:bg-slate-800 px-1 rounded">.fit</code>,{" "}
        <code className="bg-slate-100 dark:bg-slate-800 px-1 rounded">.gpx</code>, or{" "}
        <code className="bg-slate-100 dark:bg-slate-800 px-1 rounded">.tcx</code> files added to this directory.
        Useful for Garmin watches synced via USB or Garmin Express.
      </p>
      <form onSubmit={handleSubmit} className="flex gap-2">
        <input
          type="text"
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder="/Users/me/Downloads/Garmin"
          className="flex-1 text-sm border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-2 font-mono dark:bg-slate-800 dark:text-slate-100 focus:outline-none focus:ring-2 focus:ring-teal-500"
        />
        <button
          type="submit"
          disabled={saving || !path.trim()}
          className="px-4 py-2 text-sm font-medium bg-teal-600 text-white rounded-lg hover:bg-teal-700 disabled:opacity-50 transition-colors"
        >
          {saving ? "Adding…" : "Watch folder"}
        </button>
      </form>
      {error && <p className="mt-2 text-xs text-red-600">{error}</p>}
    </div>
  );
}

export default function Sources() {
  const [sources, setSources]                 = useState([]);
  const [loading, setLoading]                 = useState(true);
  const [error, setError]                     = useState(null);
  const [stravaConfigured, setStravaConfigured] = useState(false);

  const load = useCallback(async () => {
    try {
      const [data, settings] = await Promise.all([
        api.sources.list(),
        api.settings.get().catch(() => ({})),
      ]);
      setSources(data ?? []);
      setStravaConfigured(
        Boolean(settings.strava_client_id && settings.strava_client_secret_set)
      );
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const syncableSources = sources.filter((s) => !s.source.endsWith("_upload"));
  const hasNRC = sources.some((s) => s.source === "nrc");
  const hasStrava = sources.some((s) => s.source === "strava");

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Sources</h1>
        <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">Connected data sources and sync status</p>
      </div>

      {error && (
        <div className="rounded-xl border border-red-200 dark:border-red-800/50 bg-red-50 dark:bg-red-900/20 p-4 text-sm text-red-700 dark:text-red-400">{error}</div>
      )}

      {/* Connected sources */}
      {loading ? (
        <div className="space-y-3">
          {[0, 1].map((i) => (
            <div key={i} className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5 animate-pulse">
              <div className="h-9 bg-slate-200 dark:bg-slate-700 rounded-lg w-48" />
            </div>
          ))}
        </div>
      ) : syncableSources.length > 0 ? (
        <div className="space-y-3">
          {syncableSources.map((s) => (
            <SourceCard key={s.id} source={s} onSync={load} onDelete={load} />
          ))}
        </div>
      ) : null}

      {/* Connect new sources */}
      <div>
        <p className="text-xs font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-widest mb-3">
          Add a source
        </p>
        <div className="space-y-3">
          {!hasNRC && <NRCConnect onConnected={load} />}
          {!hasStrava && <StravaConnect configured={stravaConfigured} />}
          <FolderWatchConnect onConnected={load} />
        </div>
      </div>
    </div>
  );
}
