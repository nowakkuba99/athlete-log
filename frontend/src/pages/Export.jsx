import { useState, useEffect, useCallback } from "react";
import { Download, CheckCircle, XCircle, Clock, RefreshCw } from "lucide-react";
import { api } from "../api/client";

const SPORTS = ["run", "ride", "swim", "walk", "hike"];

function capitalize(s) { return s ? s.charAt(0).toUpperCase() + s.slice(1) : ""; }

function fmtDate(iso) {
  if (!iso) return "—";
  return new Date(iso).toLocaleString("en-GB", {
    day: "numeric", month: "short", year: "numeric",
    hour: "2-digit", minute: "2-digit",
  });
}

function StatusBadge({ status }) {
  const map = {
    succeeded: { icon: CheckCircle, color: "text-emerald-600 bg-emerald-50", label: "Done" },
    running:   { icon: RefreshCw,   color: "text-blue-600 bg-blue-50 [&>svg]:animate-spin", label: "Running" },
    queued:    { icon: Clock,       color: "text-slate-500 dark:text-slate-400 bg-slate-100 dark:bg-slate-800", label: "Queued" },
    failed:    { icon: XCircle,     color: "text-red-600 bg-red-50", label: "Failed" },
  };
  const cfg = map[status] ?? map.queued;
  const Icon = cfg.icon;
  return (
    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium ${cfg.color}`}>
      <Icon size={11} />
      {cfg.label}
    </span>
  );
}

function ProgressBar({ current, total }) {
  if (!total) return null;
  const pct = Math.min(100, Math.round((current / total) * 100));
  return (
    <div className="mt-2 h-1.5 bg-slate-100 dark:bg-slate-800 rounded-full overflow-hidden">
      <div
        className="h-full bg-blue-500 transition-all duration-500 rounded-full"
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}

function JobRow({ job, onRefresh }) {
  const result = job.result ?? {};
  const total = job.progress_total;
  const current = job.progress_current;
  const artifactId = result.artifact_id;

  return (
    <div className="px-5 py-4 flex items-start gap-4">
      <StatusBadge status={job.status} />
      <div className="flex-1 min-w-0">
        <p className="text-sm text-slate-600 dark:text-slate-300">
          {job.message || "Export job"}
        </p>
        {(job.status === "running" || job.status === "queued") && (
          <ProgressBar current={current} total={total} />
        )}
        {job.status === "failed" && job.error && (
          <p className="text-xs text-red-600 mt-1">{job.error}</p>
        )}
        <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">{fmtDate(job.created_at)}</p>
      </div>
      {job.status === "succeeded" && artifactId && (
        <a
          href={api.artifacts.downloadUrl(artifactId)}
          download
          className="shrink-0 flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium btn-accent rounded-lg transition-colors"
        >
          <Download size={12} />
          Download
        </a>
      )}
    </div>
  );
}

export default function Export() {
  const [jobs, setJobs] = useState([]);
  const [loadingJobs, setLoadingJobs] = useState(true);

  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [sport, setSport] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState(null);

  const loadJobs = useCallback(async () => {
    try {
      const data = await api.jobs.list(20);
      setJobs((data ?? []).filter((j) => j.type === "export"));
    } catch {
      /* non-fatal */
    } finally {
      setLoadingJobs(false);
    }
  }, []);

  useEffect(() => {
    loadJobs();
  }, [loadJobs]);

  // Poll while any job is running/queued
  useEffect(() => {
    const active = jobs.some((j) => j.status === "running" || j.status === "queued");
    if (!active) return;
    const t = setInterval(loadJobs, 2000);
    return () => clearInterval(t);
  }, [jobs, loadJobs]);

  async function handleSubmit(e) {
    e.preventDefault();
    setSubmitting(true);
    setFormError(null);
    try {
      await api.exports.create({ from, to, sport_type: sport });
      await loadJobs();
    } catch (err) {
      setFormError(err.message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Export</h1>
        <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">
          Download your activities as a TCX ZIP archive
        </p>
      </div>

      {/* Export form */}
      <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
        <p className="text-sm font-medium text-slate-700 dark:text-slate-200 mb-4">New export</p>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <label className="flex flex-col gap-1">
              <span className="text-xs font-medium text-slate-400 dark:text-slate-500">From date</span>
              <input
                type="date"
                value={from}
                onChange={(e) => setFrom(e.target.value)}
                className="text-sm border border-slate-200 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100 rounded-lg px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-xs font-medium text-slate-400 dark:text-slate-500">To date</span>
              <input
                type="date"
                value={to}
                onChange={(e) => setTo(e.target.value)}
                className="text-sm border border-slate-200 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100 rounded-lg px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </label>
          </div>

          <div>
            <span className="text-xs font-medium text-slate-400 dark:text-slate-500 block mb-1.5">Sport</span>
            <div className="flex flex-wrap gap-1.5">
              <button
                type="button"
                onClick={() => setSport("")}
                className={chip(sport === "")}
              >
                All sports
              </button>
              {SPORTS.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => setSport(s === sport ? "" : s)}
                  className={chip(sport === s)}
                >
                  {capitalize(s)}
                </button>
              ))}
            </div>
          </div>

          {formError && (
            <p className="text-sm text-red-600">{formError}</p>
          )}

          <div className="flex justify-end">
            <button
              type="submit"
              disabled={submitting}
              className="px-5 py-2 text-sm font-medium btn-accent rounded-lg disabled:opacity-50 transition-colors"
            >
              {submitting ? "Starting…" : "Export"}
            </button>
          </div>
        </form>
      </div>

      {/* Job history */}
      <div className="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 overflow-hidden">
        <p className="text-sm font-medium text-slate-600 dark:text-slate-300 px-5 py-4 border-b border-slate-100 dark:border-slate-800">
          Export history
        </p>
        {loadingJobs ? (
          <div className="p-5 space-y-3">
            {[0, 1].map((i) => (
              <div key={i} className="h-12 bg-slate-200 dark:bg-slate-700 rounded-lg animate-pulse" />
            ))}
          </div>
        ) : jobs.length === 0 ? (
          <div className="px-5 py-10 text-center text-sm text-slate-400 dark:text-slate-500">
            No exports yet. Run your first export above.
          </div>
        ) : (
          <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {jobs.map((job) => (
              <li key={job.id}>
                <JobRow job={job} />
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function chip(active) {
  return [
    "px-3 py-1 rounded-full text-xs font-medium transition-colors",
    active ? "bg-blue-600 text-white" : "bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700",
  ].join(" ");
}
