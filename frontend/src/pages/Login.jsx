import { useState } from "react";
import { Zap, X } from "lucide-react";
import { api, setAuthToken } from "../api/client";

export default function Login({ onSuccess, modal = false }) {
  const [password, setPassword] = useState("");
  const [error, setError]       = useState("");
  const [loading, setLoading]   = useState(false);

  async function handleSubmit(e) {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      const res = await api.auth.verify(password);
      if (res.ok) {
        setAuthToken(password);
        onSuccess();
      } else {
        setError("Incorrect password.");
      }
    } catch {
      setError("Could not reach server.");
    } finally {
      setLoading(false);
    }
  }

  const card = (
    <div className="w-full max-w-sm">
      {!modal && (
        <div className="flex items-center justify-center gap-2 mb-8">
          <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-blue-600 to-cyan-500 flex items-center justify-center">
            <Zap size={20} className="text-white" />
          </div>
          <span className="text-xl font-bold text-slate-800 dark:text-slate-100">Athlete Log</span>
        </div>
      )}

      <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 shadow-sm p-8">
        {modal && (
          <div className="flex items-center justify-between mb-4">
            <h1 className="text-lg font-semibold text-slate-800 dark:text-slate-100">Owner login</h1>
            <button onClick={() => onSuccess(false)} className="text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300">
              <X size={18} />
            </button>
          </div>
        )}
        {!modal && (
          <>
            <h1 className="text-lg font-semibold text-slate-800 dark:text-slate-100 mb-1">Sign in</h1>
            <p className="text-sm text-slate-400 dark:text-slate-500 mb-6">
              Password protected — enter your password.
            </p>
          </>
        )}
        {modal && (
          <p className="text-sm text-slate-400 dark:text-slate-500 mb-6">
            Enter the owner password to manage data and sources.
          </p>
        )}

        <form onSubmit={handleSubmit} className="space-y-4">
          <input
            type="password"
            autoFocus
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Password"
            className="w-full px-3 py-2.5 text-sm border border-slate-200 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          {error && <p className="text-xs text-red-500">{error}</p>}
          <button
            type="submit"
            disabled={loading || !password}
            className="w-full py-2.5 text-sm font-medium btn-accent rounded-lg disabled:opacity-50 transition-colors"
          >
            {loading ? "Checking…" : "Unlock"}
          </button>
        </form>
      </div>

      {!modal && (
        <p className="text-center text-xs text-slate-400 dark:text-slate-500 mt-6">
          Running locally? Disable auth in Settings.
        </p>
      )}
    </div>
  );

  if (modal) {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40 backdrop-blur-sm">
        {card}
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-slate-50 dark:bg-slate-950 flex items-center justify-center p-4">
      {card}
    </div>
  );
}
