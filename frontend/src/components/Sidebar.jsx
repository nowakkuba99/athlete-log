import { NavLink } from "react-router-dom";
import {
  LayoutDashboard,
  Activity,
  CalendarDays,
  Map,
  Trophy,
  Target,
  BarChart3,
  TrendingUp,
  Sparkles,
  Backpack,
  Radio,
  Upload,
  Download,
  Settings,
  User,
  Zap,
  Lock,
  LogOut,
  Sun,
  Moon,
  Plug,
  GitBranch,
  ExternalLink,
} from "lucide-react";

const BASE_GROUPS = [
  [
    { to: "/", label: "Home", icon: LayoutDashboard, end: true },
    { to: "/activities", label: "Activities", icon: Activity },
    { to: "/calendar", label: "Calendar", icon: CalendarDays },
    { to: "/heatmap", label: "Heatmap", icon: Map },
    { to: "/records", label: "Records", icon: Trophy },
    { to: "/predictions", label: "Predictions", icon: Target },
    { to: "/fitness", label: "Fitness", icon: TrendingUp },
    { to: "/review", label: "Year in Review", icon: Sparkles },
    { to: "/gear", label: "Gear", icon: Backpack },
    { to: "/dashboards", label: "Dashboards", icon: BarChart3 },
  ],
  [
    { to: "/sources",     label: "Sources",    icon: Radio },
    { to: "/upload",      label: "Upload",     icon: Upload },
    { to: "/export",      label: "Export",     icon: Download },
    { to: "/connect-ai",  label: "Connect AI", icon: Plug },
  ],
  [
    { to: "/profile", label: "Profile", icon: User },
    { to: "/settings", label: "Settings", icon: Settings },
  ],
];

const DEMO_DISABLED = new Set(["/sources", "/upload", "/export", "/settings", "/connect-ai"]);

export default function Sidebar({ publicMode, isAuthenticated, onLoginClick, onLogout, demoMode, theme, onToggleTheme }) {
  const NAV_GROUPS = demoMode
    ? BASE_GROUPS.map(group =>
        group.map(item => DEMO_DISABLED.has(item.to) ? { ...item, disabled: true } : item)
      )
    : BASE_GROUPS;

  return (
    <aside className="flex flex-col w-56 h-full bg-slate-900 text-slate-300 shrink-0">
      {/* Logo */}
      <div className="flex items-center gap-2.5 px-5 py-5 border-b border-slate-800">
        <div className="flex h-7 w-7 items-center justify-center rounded-lg" style={{ background: "color-mix(in srgb, var(--accent) 20%, transparent)", border: "1px solid color-mix(in srgb, var(--accent) 30%, transparent)" }}>
          <Zap size={14} style={{ color: "var(--accent)" }} />
        </div>
        <span className="text-white font-semibold tracking-tight text-sm">
          Athlete Log
        </span>
      </div>

      {/* Nav */}
      <nav className="flex-1 py-3 overflow-y-auto">
        {NAV_GROUPS.map((group, gi) => (
          <div key={gi} className={gi > 0 ? "mt-1 pt-1 border-t border-slate-800/60" : ""}>
            {group.map(({ to, label, icon: Icon, end, disabled }) =>
              disabled ? (
                <span
                  key={to}
                  className="flex items-center gap-3 px-4 py-2 text-sm text-slate-700 cursor-not-allowed select-none mx-2 rounded-lg"
                >
                  <Icon size={15} />
                  {label}
                </span>
              ) : (
                <NavLink
                  key={to}
                  to={to}
                  end={end}
                  className={({ isActive }) =>
                    [
                      "flex items-center gap-3 px-4 py-2 text-sm transition-all duration-150 mx-2 rounded-lg",
                      isActive
                        ? "text-white bg-slate-800 shadow-sm"
                        : "text-slate-400 hover:text-white hover:bg-slate-800/50",
                    ].join(" ")
                  }
                >
                  {({ isActive }) => (
                    <>
                      <Icon size={15} style={isActive ? { color: "var(--accent)" } : {}} />
                      {label}
                    </>
                  )}
                </NavLink>
              )
            )}
          </div>
        ))}
      </nav>

      <div className="px-4 py-2 border-t border-slate-800/60">
        <button
          onClick={onToggleTheme}
          className="flex items-center gap-2 w-full px-3 py-2 rounded-lg text-xs text-slate-400 hover:text-white hover:bg-slate-800/50 transition-all duration-150"
          title={theme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
        >
          {theme === "dark"
            ? <><Sun size={14} /><span>Light mode</span></>
            : <><Moon size={14} /><span>Dark mode</span></>
          }
        </button>
      </div>

      <div className="px-4 py-3 border-t border-slate-800/60">
        <div className="space-y-1">
          <a
            href="https://github.com/nowakkuba99/athlete-log"
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-2 px-2 py-1.5 text-xs text-slate-500 hover:text-slate-300 hover:bg-slate-800/60 rounded transition-colors"
            title="Source code"
          >
            <GitBranch size={13} />
            Source code
            <ExternalLink size={11} className="ml-auto" />
          </a>
          <a
            href="https://athlete-log.app/docs"
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-2 px-2 py-1.5 text-xs text-slate-500 hover:text-slate-300 hover:bg-slate-800/60 rounded transition-colors"
            title="Public documentation"
          >
            <ExternalLink size={13} />
            Public docs
          </a>
        </div>
      </div>

      {/* Admin section — only shown in public mode */}
      {publicMode && (
        <div className="px-4 py-3 border-t border-slate-800">
          {isAuthenticated ? (
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-xs text-slate-400">
                <span className="w-2 h-2 rounded-full bg-emerald-400 shrink-0" />
                Owner
              </div>
              <button
                onClick={onLogout}
                className="flex items-center gap-1.5 text-xs text-slate-500 hover:text-slate-300 transition-colors"
                title="Log out"
              >
                <LogOut size={13} />
                Log out
              </button>
            </div>
          ) : (
            <button
              onClick={onLoginClick}
              className="flex items-center gap-2 w-full px-2 py-1.5 text-xs text-slate-500 hover:text-slate-300 hover:bg-slate-800/60 rounded transition-colors"
            >
              <Lock size={13} />
              Owner login
            </button>
          )}
        </div>
      )}
    </aside>
  );
}
