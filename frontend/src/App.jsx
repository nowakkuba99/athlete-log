import { useState, useEffect, useCallback, useRef } from "react";
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { Lock } from "lucide-react";

const IS_DEMO = import.meta.env.VITE_DEMO_MODE === "true";
const DEMO_BASE = import.meta.env.VITE_DEMO_BASE ?? "/";
import Sidebar from "./components/Sidebar";
import AIPanel from "./components/AIPanel";
import Home from "./pages/Home";
import Activities from "./pages/Activities";
import ActivityDetail from "./pages/ActivityDetail";
import Sources from "./pages/Sources";
import Upload from "./pages/Upload";
import Export from "./pages/Export";
import Dashboards from "./pages/Dashboards";
import Calendar from "./pages/Calendar";
import Heatmap from "./pages/Heatmap";
import Records from "./pages/Records";
import Predictions from "./pages/Predictions";
import Fitness from "./pages/Fitness";
import Review from "./pages/Review";
import Gear from "./pages/Gear";
import Profile from "./pages/Profile";
import Settings from "./pages/Settings";
import Login from "./pages/Login";
import Onboarding from "./pages/Onboarding";
import MCPConnect from "./pages/MCPConnect";
import { api, getAuthToken, clearAuthToken } from "./api/client";
import { useTheme, useGradient, useAccent, useGradientStrength, useDefaultDashboardId } from "./settings";

function DemoGate({ children }) {
  if (!IS_DEMO) return children;
  return (
    <div className="flex flex-col items-center justify-center py-32 text-center gap-3">
      <div className="w-12 h-12 rounded-full bg-slate-100 dark:bg-slate-800 flex items-center justify-center">
        <Lock size={20} className="text-slate-400 dark:text-slate-500" />
      </div>
      <p className="font-medium text-slate-700 dark:text-slate-200">Not available in the demo</p>
      <p className="text-sm text-slate-400 dark:text-slate-500 max-w-xs">
        Browse activities, heatmaps, fitness charts, and records — then self-host to manage sources and settings.
      </p>
      <a
        href="https://github.com/nowakkuba99/athlete-log"
        target="_blank"
        rel="noopener noreferrer"
        className="mt-2 px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors"
      >
        Self-host →
      </a>
    </div>
  );
}

// Shown in place of a management page when the visitor isn't the owner.
function AdminGate({ publicMode, isAuthenticated, onLoginClick, children }) {
  if (publicMode && !isAuthenticated) {
    return (
      <div className="flex flex-col items-center justify-center py-32 text-center gap-3">
        <div className="w-12 h-12 rounded-full bg-slate-100 dark:bg-slate-800 flex items-center justify-center">
          <Lock size={20} className="text-slate-400 dark:text-slate-500" />
        </div>
        <p className="font-medium text-slate-700 dark:text-slate-200">Owner access required</p>
        <p className="text-sm text-slate-400 dark:text-slate-500 max-w-xs">
          This section is restricted to the app owner.
        </p>
        <button
          onClick={onLoginClick}
          className="mt-2 px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors"
        >
          Owner login
        </button>
      </div>
    );
  }
  return children;
}

function Layout({ children, publicMode, isAuthenticated, onLoginClick, onLogout, theme, onToggleTheme }) {
  return (
    <div className="flex h-screen overflow-hidden bg-app">
      <Sidebar
        publicMode={publicMode}
        isAuthenticated={isAuthenticated}
        onLoginClick={onLoginClick}
        onLogout={onLogout}
        demoMode={IS_DEMO}
        theme={theme}
        onToggleTheme={onToggleTheme}
      />
      <AIPanel />
      <main className="relative flex-1 overflow-y-auto">
        <div className="pointer-events-none fixed inset-0 overflow-hidden -z-10">
          <div className="absolute -top-32 right-1/4 h-[500px] w-[500px] rounded-full bg-blue-400/[0.06] dark:bg-blue-500/[0.22] blur-[100px]" />
          <div className="absolute bottom-0 left-0 h-96 w-96 rounded-full bg-violet-400/[0.05] dark:bg-violet-600/[0.16] blur-[80px]" />
          <div className="absolute inset-0 bg-gradient-to-b from-blue-950/[0] dark:from-blue-950/[0.12] to-transparent" />
        </div>
        <div className="relative max-w-5xl mx-auto px-6 py-8">{children}</div>
      </main>
    </div>
  );
}

function HomeOrDashboard() {
  const [defaultDashId] = useDefaultDashboardId();
  if (defaultDashId) return <Navigate to={`/dashboards/${defaultDashId}`} replace />;
  return <Home />;
}

export default function App() {
  const [theme, setTheme] = useTheme();
  useGradient();         // initializes data-gradient attribute on <html>
  useAccent();           // initializes data-accent attribute on <html>
  useGradientStrength(); // initializes --gradient-strength CSS var on <html>
  const [authRequired, setAuthRequired]   = useState(false);
  const [authModalOpen, setAuthModalOpen] = useState(false);
  const [authChecked, setAuthChecked]     = useState(false);
  const [publicMode, setPublicMode]       = useState(false);
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [showOnboarding, setShowOnboarding] = useState(false);
  const publicModeRef = useRef(false);

  function toggleTheme() {
    setTheme(theme === "dark" ? "light" : "dark");
  }

  const checkAuth = useCallback(async () => {
    try {
      const health = await api.health();
      const pm = health.public_mode ?? false;
      publicModeRef.current = pm;
      setPublicMode(pm);
      const hasToken = !!getAuthToken();
      setIsAuthenticated(hasToken);
      if (!pm && health.auth_enabled && !hasToken) {
        setAuthRequired(true);
      }
      // First-run onboarding: trigger when no onboarding flag and zero activities
      if (!localStorage.getItem("av_onboarding_done") && !IS_DEMO) {
        try {
          const stats = await api.stats.summary();
          if ((stats.total_activities ?? 0) === 0) setShowOnboarding(true);
        } catch { /* ignore — onboarding is non-critical */ }
      }
    } catch {
      // server unreachable — proceed and let individual requests handle errors
    } finally {
      setAuthChecked(true);
    }
  }, []);

  useEffect(() => {
    checkAuth();
    const handler = () => {
      clearAuthToken();
      setIsAuthenticated(false);
      if (publicModeRef.current) {
        setAuthModalOpen(true);
      } else {
        setAuthRequired(true);
      }
    };
    window.addEventListener("av:auth-required", handler);
    return () => window.removeEventListener("av:auth-required", handler);
  }, [checkAuth]);

  if (!authChecked) return null;

  if (authRequired) {
    return (
      <Login
        onSuccess={() => {
          setAuthRequired(false);
          setIsAuthenticated(true);
        }}
      />
    );
  }

  function handleLoginSuccess(didLogin = true) {
    if (didLogin) setIsAuthenticated(true);
    setAuthModalOpen(false);
  }

  function handleLogout() {
    clearAuthToken();
    setIsAuthenticated(false);
  }

  function gate(element) {
    return (
      <AdminGate
        publicMode={publicMode}
        isAuthenticated={isAuthenticated}
        onLoginClick={() => setAuthModalOpen(true)}
      >
        {element}
      </AdminGate>
    );
  }

  return (
    <BrowserRouter basename={IS_DEMO ? DEMO_BASE : "/"}>
      {showOnboarding ? (
        <Onboarding onDone={() => setShowOnboarding(false)} />
      ) : (
        <>
      {IS_DEMO && (
        <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 flex items-center gap-3 px-4 py-2 rounded-xl bg-slate-800/90 backdrop-blur text-white text-xs shadow-lg">
          <span className="font-semibold text-amber-400">Demo</span>
          <span className="text-slate-300">Read-only preview · Changes are not saved</span>
          <a
            href="https://github.com/nowakkuba99/athlete-log"
            target="_blank"
            rel="noopener noreferrer"
            className="ml-1 px-2.5 py-1 rounded-lg bg-white/10 hover:bg-white/20 transition-colors font-medium"
          >
            Self-host →
          </a>
        </div>
      )}
      {authModalOpen && <Login modal onSuccess={handleLoginSuccess} />}
      <Routes>
        <Route
          path="/*"
          element={
            <Layout
              publicMode={publicMode}
              isAuthenticated={isAuthenticated}
              onLoginClick={() => setAuthModalOpen(true)}
              onLogout={handleLogout}
              theme={theme}
              onToggleTheme={toggleTheme}
            >
              <Routes>
                {/* Public — visible to everyone */}
                <Route path="/" element={<HomeOrDashboard />} />
                <Route path="/activities" element={<Activities />} />
                <Route path="/activities/:id" element={<ActivityDetail />} />
                <Route path="/calendar" element={<Calendar />} />
                <Route path="/heatmap" element={<Heatmap />} />
                <Route path="/records" element={<Records />} />
                <Route path="/predictions" element={<Predictions />} />
                <Route path="/fitness" element={<Fitness />} />
                <Route path="/review" element={<Review />} />
                <Route path="/gear" element={<Gear />} />
                <Route path="/dashboards" element={<Dashboards />} />
                <Route path="/dashboards/:id" element={<Dashboards />} />
                {/* Admin — owner only in public mode; hidden in demo */}
                <Route path="/connect-ai" element={<DemoGate>{gate(<MCPConnect />)}</DemoGate>} />
                <Route path="/sources"  element={<DemoGate>{gate(<Sources />)}</DemoGate>} />
                <Route path="/upload"   element={<DemoGate>{gate(<Upload />)}</DemoGate>} />
                <Route path="/export"   element={<DemoGate>{gate(<Export />)}</DemoGate>} />
                <Route path="/profile"  element={gate(<Profile />)} />
                <Route path="/settings" element={<DemoGate>{gate(<Settings />)}</DemoGate>} />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </Layout>
          }
        />
      </Routes>
        </>
      )}
    </BrowserRouter>
  );
}
