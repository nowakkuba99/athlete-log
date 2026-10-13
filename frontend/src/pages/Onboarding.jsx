import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Zap, ArrowRight, CheckCircle2, Loader2 } from "lucide-react";

const STEPS = ["Welcome", "Connect", "Import", "Ready"];

export default function Onboarding({ onDone }) {
  const [step, setStep] = useState(0);
  const navigate = useNavigate();

  // Mark onboarding complete, dismiss the overlay, then navigate client-side.
  // Dismissing first is essential: while showOnboarding is true the overlay
  // covers every route, so navigating without it would appear to do nothing.
  function complete(to = "/") {
    localStorage.setItem("av_onboarding_done", "1");
    onDone?.();
    navigate(to, { replace: true });
  }

  return (
    <div className="min-h-screen bg-app flex flex-col items-center justify-center px-4">
      {/* Progress dots */}
      <div className="flex gap-2 mb-10">
        {STEPS.map((_, i) => (
          <div
            key={i}
            className={[
              "h-1.5 rounded-full transition-all duration-300",
              i === step ? "w-8 bg-accent" : i < step ? "w-3 bg-accent opacity-40" : "w-3 bg-slate-200 dark:bg-slate-700",
            ].join(" ")}
          />
        ))}
      </div>

      <div className="w-full max-w-md">
        {step === 0 && <StepWelcome onNext={() => setStep(1)} />}
        {step === 1 && <StepConnect onNext={() => setStep(2)} onSkip={() => setStep(2)} onPick={complete} />}
        {step === 2 && <StepImport onNext={() => setStep(3)} />}
        {step === 3 && <StepReady onFinish={() => complete("/")} />}
      </div>
    </div>
  );
}

function StepWelcome({ onNext }) {
  return (
    <div className="text-center space-y-6">
      <div className="mx-auto w-16 h-16 rounded-2xl flex items-center justify-center"
           style={{ background: "color-mix(in srgb, var(--accent) 15%, transparent)" }}>
        <Zap size={28} style={{ color: "var(--accent)" }} />
      </div>
      <div className="space-y-2">
        <h1 className="text-3xl font-bold text-slate-800 dark:text-slate-100">Welcome to Athlete Log</h1>
        <p className="text-slate-500 dark:text-slate-400 leading-relaxed">
          Your private, self-hosted training diary. No subscriptions, no tracking — just your data, your way.
        </p>
      </div>
      <ul className="text-left space-y-2 text-sm text-slate-600 dark:text-slate-300">
        {[
          "Import from Nike Run Club, Strava, FIT files, and GPX",
          "Personal records, fitness trends, and heatmaps",
          "AI-powered coaching powered by a local model",
          "All data stays on your machine",
        ].map((item) => (
          <li key={item} className="flex items-start gap-2">
            <CheckCircle2 size={15} className="mt-0.5 shrink-0" style={{ color: "var(--accent)" }} />
            {item}
          </li>
        ))}
      </ul>
      <button onClick={onNext} className="w-full py-3 rounded-xl font-medium flex items-center justify-center gap-2 btn-accent">
        Get started <ArrowRight size={16} />
      </button>
    </div>
  );
}

function StepConnect({ onNext, onSkip, onPick }) {
  return (
    <div className="space-y-6">
      <div className="text-center space-y-2">
        <h2 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Connect a source</h2>
        <p className="text-sm text-slate-500 dark:text-slate-400">
          Choose how you want to get your activities in. You can add more later in Settings.
        </p>
      </div>
      <div className="space-y-3">
        {[
          { label: "Nike Run Club export", desc: "Drag-and-drop your NRC data export ZIP", path: "/upload" },
          { label: "Strava", desc: "Connect your Strava account via OAuth", path: "/sources" },
          { label: "FIT / GPX files", desc: "Upload individual activity files", path: "/upload" },
        ].map((src) => (
          <button
            key={src.label}
            type="button"
            onClick={() => onPick(src.path)}
            className="w-full text-left flex items-center justify-between p-4 rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 hover:border-slate-400 dark:hover:border-slate-500 transition-colors group"
          >
            <div>
              <p className="text-sm font-medium text-slate-700 dark:text-slate-200">{src.label}</p>
              <p className="text-xs text-slate-400 dark:text-slate-500">{src.desc}</p>
            </div>
            <ArrowRight size={16} className="text-slate-300 group-hover:text-slate-500 dark:text-slate-600 dark:group-hover:text-slate-400 transition-colors" />
          </button>
        ))}
      </div>
      <div className="flex gap-3">
        <button onClick={onNext} className="flex-1 py-2.5 rounded-xl font-medium text-sm btn-accent">
          Continue
        </button>
        <button onClick={onSkip} className="px-4 py-2.5 rounded-xl text-sm text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 transition-colors">
          Skip
        </button>
      </div>
    </div>
  );
}

function StepImport({ onNext }) {
  const [checking, setChecking] = useState(true);
  const [count, setCount] = useState(0);

  useEffect(() => {
    const t = setInterval(async () => {
      try {
        const res = await fetch("/api/v1/stats");
        if (res.ok) {
          const data = await res.json();
          setCount(data.total_activities ?? 0);
          if ((data.total_activities ?? 0) > 0) setChecking(false);
        }
      } catch { /* ignore */ }
    }, 2000);
    return () => clearInterval(t);
  }, []);

  return (
    <div className="text-center space-y-6">
      <div className="space-y-2">
        <h2 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Importing your data</h2>
        <p className="text-sm text-slate-500 dark:text-slate-400">
          Your activities are being processed in the background.
        </p>
      </div>
      <div className="bg-white dark:bg-slate-900 rounded-2xl border border-slate-200 dark:border-slate-700 p-8 space-y-4">
        {checking ? (
          <>
            <Loader2 size={32} className="animate-spin mx-auto" style={{ color: "var(--accent)" }} />
            <p className="text-sm text-slate-500 dark:text-slate-400">Processing…</p>
          </>
        ) : (
          <>
            <CheckCircle2 size={32} className="mx-auto" style={{ color: "var(--accent)" }} />
            <p className="text-2xl font-bold text-slate-800 dark:text-slate-100">{count.toLocaleString()}</p>
            <p className="text-sm text-slate-500 dark:text-slate-400">activities imported</p>
          </>
        )}
      </div>
      <button onClick={onNext} className="w-full py-3 rounded-xl font-medium flex items-center justify-center gap-2 btn-accent">
        {checking ? "Skip for now" : "Continue"} <ArrowRight size={16} />
      </button>
    </div>
  );
}

function StepReady({ onFinish }) {
  return (
    <div className="text-center space-y-6">
      <div className="mx-auto w-16 h-16 rounded-2xl flex items-center justify-center"
           style={{ background: "color-mix(in srgb, var(--accent) 15%, transparent)" }}>
        <CheckCircle2 size={28} style={{ color: "var(--accent)" }} />
      </div>
      <div className="space-y-2">
        <h2 className="text-2xl font-bold text-slate-800 dark:text-slate-100">You're all set</h2>
        <p className="text-slate-500 dark:text-slate-400 leading-relaxed">
          Explore your activities, check your records, and start uncovering insights from your training history.
        </p>
      </div>
      <button onClick={onFinish} className="w-full py-3 rounded-xl font-medium flex items-center justify-center gap-2 btn-accent text-base">
        Open Athlete Log <ArrowRight size={18} />
      </button>
    </div>
  );
}
