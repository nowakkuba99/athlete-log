import { useState, useRef, useCallback } from "react";
import { Link } from "react-router-dom";
import { Upload as UploadIcon, CheckCircle, XCircle, AlertCircle, X } from "lucide-react";
import { api } from "../api/client";

const ACCEPT = ".fit,.gpx,.tcx";

function fmtSize(bytes) {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(0) + " KB";
  return (bytes / (1024 * 1024)).toFixed(1) + " MB";
}

function fmtDate(iso) {
  return new Date(iso).toLocaleDateString("en-GB", {
    day: "numeric", month: "short", year: "numeric",
  });
}

const SPORT_EMOJI = { run: "🏃", ride: "🚴", swim: "🏊", walk: "🚶", hike: "🥾", unknown: "⚡" };

function FileRow({ item, onRemove }) {
  const { file, status, result, error } = item;

  return (
    <div className="flex items-center gap-3 px-4 py-3 bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700">
      {/* Status icon */}
      <div className="shrink-0">
        {status === "pending" && (
          <div className="w-5 h-5 rounded-full border-2 border-slate-200 dark:border-slate-700" />
        )}
        {status === "uploading" && (
          <div className="w-5 h-5 rounded-full border-2 border-blue-500 border-t-transparent animate-spin" />
        )}
        {status === "done" && !result?.duplicate_of_id && <CheckCircle size={20} className="text-emerald-500" />}
        {status === "done" && result?.duplicate_of_id && <AlertCircle size={20} className="text-amber-500" />}
        {status === "duplicate" && <AlertCircle size={20} className="text-amber-500" />}
        {status === "error" && <XCircle size={20} className="text-red-500" />}
      </div>

      {/* File info */}
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium text-slate-700 dark:text-slate-200 truncate">{file.name}</p>
        {status === "pending" && (
          <p className="text-xs text-slate-400 dark:text-slate-500">{fmtSize(file.size)}</p>
        )}
        {status === "done" && result && (
          <>
            <p className="text-xs text-slate-400 dark:text-slate-500">
              {SPORT_EMOJI[result.sport_type] ?? "⚡"} {result.name || result.sport_type} ·{" "}
              {fmtDate(result.start_at)}
            </p>
            {result.duplicate_of_id && (
              <p className="text-xs text-amber-600 mt-0.5">
                Possible duplicate of{" "}
                <Link to={`/activities/${result.duplicate_of_id}`} className="underline hover:text-amber-800">
                  {result.duplicate_of_name}
                </Link>{" "}
                from another source
              </p>
            )}
          </>
        )}
        {status === "duplicate" && (
          <p className="text-xs text-amber-600">Already imported (same file)</p>
        )}
        {status === "error" && (
          <p className="text-xs text-red-600 truncate">{error}</p>
        )}
      </div>

      {/* Link to activity or remove */}
      {status === "done" && result?.id && (
        <Link
          to={`/activities/${result.id}`}
          className="text-xs text-blue-500 hover:text-blue-700 font-medium shrink-0"
        >
          View →
        </Link>
      )}
      {(status === "pending" || status === "error") && (
        <button onClick={() => onRemove(item.id)} className="text-slate-300 dark:text-slate-500 hover:text-slate-500 dark:hover:text-slate-300">
          <X size={16} />
        </button>
      )}
    </div>
  );
}

export default function Upload() {
  const [items, setItems] = useState([]);
  const [dragging, setDragging] = useState(false);
  const [uploading, setUploading] = useState(false);
  const inputRef = useRef(null);
  const nextId = useRef(0);

  function addFiles(files) {
    const newItems = [...files].map((file) => ({
      id: nextId.current++,
      file,
      status: "pending",
      result: null,
      error: null,
    }));
    setItems((prev) => [...prev, ...newItems]);
  }

  function removeItem(id) {
    setItems((prev) => prev.filter((i) => i.id !== id));
  }

  function updateItem(id, patch) {
    setItems((prev) => prev.map((i) => (i.id === id ? { ...i, ...patch } : i)));
  }

  const handleDrop = useCallback((e) => {
    e.preventDefault();
    setDragging(false);
    addFiles(e.dataTransfer.files);
  }, []);

  const handleDragOver = useCallback((e) => {
    e.preventDefault();
    setDragging(true);
  }, []);

  const handleDragLeave = useCallback(() => setDragging(false), []);

  async function uploadAll() {
    const pending = items.filter((i) => i.status === "pending");
    if (!pending.length) return;
    setUploading(true);

    // Mark all as uploading immediately so the user sees the queue start at once.
    pending.forEach((item) => updateItem(item.id, { status: "uploading" }));

    // Upload all files in parallel — each resolves independently and updates its own row.
    await Promise.allSettled(
      pending.map(async (item) => {
        try {
          const result = await api.uploads.upload(item.file);
          updateItem(item.id, {
            status: result.created === false ? "duplicate" : "done",
            result,
          });
        } catch (err) {
          updateItem(item.id, { status: "error", error: err.message });
        }
      })
    );

    setUploading(false);
  }

  const pendingCount = items.filter((i) => i.status === "pending").length;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Upload</h1>
        <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">
          Import activities from FIT, GPX, or TCX files
        </p>
      </div>

      {/* Drop zone */}
      <div
        onDrop={handleDrop}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onClick={() => inputRef.current?.click()}
        className={[
          "border-2 border-dashed rounded-xl p-10 text-center cursor-pointer transition-colors",
          dragging
            ? "border-blue-400 bg-blue-50"
            : "border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 hover:border-blue-300 hover:bg-slate-50 dark:hover:bg-slate-800/50",
        ].join(" ")}
      >
        <UploadIcon size={32} className="mx-auto mb-3 text-slate-300 dark:text-slate-600" />
        <p className="text-sm font-medium text-slate-600 dark:text-slate-300">
          Drop files here or <span className="text-blue-500">browse</span>
        </p>
        <p className="text-xs text-slate-400 dark:text-slate-500 mt-1">Supports .fit, .gpx, .tcx files</p>
        <input
          ref={inputRef}
          type="file"
          accept={ACCEPT}
          multiple
          className="hidden"
          onChange={(e) => addFiles(e.target.files)}
        />
      </div>

      {/* Queue */}
      {items.length > 0 && (
        <div className="space-y-2">
          {items.map((item) => (
            <FileRow key={item.id} item={item} onRemove={removeItem} />
          ))}

          {pendingCount > 0 && (
            <div className="flex justify-end pt-1">
              <button
                onClick={uploadAll}
                disabled={uploading}
                className="px-5 py-2 text-sm font-medium btn-accent rounded-lg disabled:opacity-50 transition-colors"
              >
                {uploading
                  ? "Uploading…"
                  : `Upload ${pendingCount} file${pendingCount > 1 ? "s" : ""}`}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
