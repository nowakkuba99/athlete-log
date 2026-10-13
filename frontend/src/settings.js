import { useState, useEffect } from "react";

const KEYS = {
  mapStyle:     "av_map_style",
  protoMapsKey: "av_protomaps_key",
};

function useLocalStorage(key, defaultVal) {
  const [value, setValue] = useState(() => localStorage.getItem(key) ?? defaultVal);
  function set(v) {
    if (v === "" || v == null) {
      localStorage.removeItem(key);
    } else {
      localStorage.setItem(key, v);
    }
    setValue(v ?? "");
  }
  return [value, set];
}

export function useMapStyle()           { return useLocalStorage(KEYS.mapStyle,     "osm"); }
export function useProtoMapsKey()      { return useLocalStorage(KEYS.protoMapsKey, ""); }
export function useRouteColor()        { return useLocalStorage("av_route_color",  "#3b82f6"); }
export function useUnits()             { return useLocalStorage("av_units",         "metric"); }
export function useDefaultDashboardId() { return useLocalStorage("av_default_dashboard", ""); }
export function useTagDisplay()        { return useLocalStorage("av_tag_display",  "all"); }

// Keys that are auto-imported from Nike Run Club and not user-created.
const NRC_AUTO_TAG_KEYS = new Set(["location", "rpe", "terrain", "shoe_id"]);
export function isAutoImportedTag(key) {
  return key.startsWith("com.nike.") || NRC_AUTO_TAG_KEYS.has(key);
}

export function useGradient() {
  const [gradient, setGradientRaw] = useLocalStorage("av_gradient", "indigo");
  useEffect(() => {
    document.documentElement.setAttribute("data-gradient", gradient || "indigo");
  }, [gradient]);
  function setGradient(v) {
    document.documentElement.setAttribute("data-gradient", v || "indigo");
    setGradientRaw(v);
  }
  return [gradient || "indigo", setGradient];
}

export function useGradientStrength() {
  const [raw, setRaw] = useLocalStorage("av_gradient_strength", "100");
  const strength = Math.max(0, Math.min(100, parseInt(raw, 10) || 100));
  useEffect(() => {
    document.documentElement.style.setProperty("--gradient-strength", (strength / 100).toFixed(2));
  }, [strength]);
  function setStrength(v) {
    const clamped = Math.max(0, Math.min(100, Number(v)));
    document.documentElement.style.setProperty("--gradient-strength", (clamped / 100).toFixed(2));
    setRaw(String(clamped));
  }
  return [strength, setStrength];
}

export function useAccent() {
  const [accent, setAccentRaw] = useLocalStorage("av_accent", "blue");
  useEffect(() => {
    document.documentElement.setAttribute("data-accent", accent || "blue");
  }, [accent]);
  function setAccent(v) {
    document.documentElement.setAttribute("data-accent", v || "blue");
    setAccentRaw(v);
  }
  return [accent || "blue", setAccent];
}

export function useTheme() {
  const [theme, setThemeRaw] = useLocalStorage("av_theme", "light");

  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
  }, [theme]);

  function setTheme(v) {
    document.documentElement.classList.toggle("dark", v === "dark");
    setThemeRaw(v);
  }

  return [theme, setTheme];
}

export function useCalendarFillMode() {
  return useLocalStorage("av_calendar_fill", "sport");
}

export function useAISettings() {
  const [endpoint, setEndpoint] = useLocalStorage("av_ai_endpoint", "");
  const [apiKey, setApiKey]     = useLocalStorage("av_ai_key", "");
  const [model, setModel]       = useLocalStorage("av_ai_model", "llama3.2");
  return { endpoint, setEndpoint, apiKey, setApiKey, model, setModel };
}

export function useChartTheme() {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains("dark"));
  useEffect(() => {
    const obs = new MutationObserver(() => {
      setDark(document.documentElement.classList.contains("dark"));
    });
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    return () => obs.disconnect();
  }, []);
  return {
    tickFill:    dark ? "#64748b" : "#94a3b8",
    gridStroke:  dark ? "#1e293b" : "#f1f5f9",
    cursorFill:  dark ? "#1e293b" : "#f1f5f9",
    refStroke:   dark ? "#334155" : "#e2e8f0",
    tooltipStyle: {
      fontSize:     12,
      borderRadius: 8,
      border:       dark ? "1px solid #334155" : "1px solid #e2e8f0",
      background:   dark ? "#0f172a"            : "#ffffff",
      color:        dark ? "#e2e8f0"            : "#1e293b",
      boxShadow:    "0 2px 8px rgba(0,0,0,.08)",
    },
  };
}
