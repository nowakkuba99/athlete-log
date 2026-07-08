import { useState, useEffect, useRef, useMemo, forwardRef, useImperativeHandle, useCallback } from "react";
import { useParams, useNavigate, Link } from "react-router-dom";
import { ComposedChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer } from "recharts";
import { ChevronLeft, ChevronRight, ChevronDown, Share2, Download, Play, Square, X, Trash2, ShieldOff } from "lucide-react";
import L from "leaflet";
import "leaflet/dist/leaflet.css";
import "maplibre-gl/dist/maplibre-gl.css";
import { api } from "../api/client";
import { useMapStyle, useProtoMapsKey, useRouteColor, useUnits, useChartTheme, useTagDisplay, isAutoImportedTag } from "../settings";
import { fmtDist as uFmtDist, fmtPace as uFmtPace, fmtEle as uFmtEle } from "../units";

// ─── Map style registry ──────────────────────────────────────────────────────
//
// Protomaps styles use the official hosted style JSON served from
// api.protomaps.com/styles/v2/{theme}.json — this is the same WebGL/MapLibre
// renderer used on the Protomaps website, so the output matches their demo.
// Raster styles fall back to the lightweight Leaflet raster layer.

export const MAP_STYLES = [
  // Raster — no API key required
  { id: "osm",          label: "OpenStreetMap",   type: "raster", url: "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",             attr: "© OpenStreetMap" },
  { id: "cartodb-light",label: "Light (CartoDB)", type: "raster", url: "https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png", attr: "© CartoDB" },
  { id: "cartodb-dark", label: "Dark (CartoDB)",  type: "raster", url: "https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png",  attr: "© CartoDB" },

  // Protomaps — requires API key; rendered via MapLibre GL with the official hosted style
  { id: "pm-light",     label: "Light",     type: "protomaps", theme: "light"     },
  { id: "pm-dark",      label: "Dark",      type: "protomaps", theme: "dark"      },
  { id: "pm-white",     label: "White",     type: "protomaps", theme: "white"     },
  { id: "pm-grayscale", label: "Grayscale", type: "protomaps", theme: "grayscale" },
];

// Route colour presets shown as chips below the map.
export const ROUTE_COLORS = [
  { id: "blue",   label: "Blue",   hex: "#3b82f6" },
  { id: "orange", label: "Orange", hex: "#f97316" },
  { id: "red",    label: "Red",    hex: "#ef4444" },
  { id: "green",  label: "Green",  hex: "#10b981" },
  { id: "purple", label: "Purple", hex: "#8b5cf6" },
];

// ─── Formatters ──────────────────────────────────────────────────────────────

// Unit-unaware helpers (time, HR, date) stay local.
// Distance / pace / elevation use the shared unit-aware functions from ../units.js,
// called as fmtDist(m, imp), fmtPace(s, imp), fmtEle(m, imp).
function fmtDist(m, imp)  { return uFmtDist(m, imp); }
function fmtPace(s, imp)  { return uFmtPace(s, imp); }
function fmtEle(m, imp)   { return uFmtEle(m, imp); }
function fmtDuration(ms){ if (!ms) return "—"; const min = Math.floor(ms/60000), h = Math.floor(min/60), m = min%60; return h>0 ? `${h}h ${m}m` : `${m}m`; }
function fmtElapsed(ms) {
  if (ms == null) return "";
  const s = Math.round(ms / 1000);
  const h = Math.floor(s / 3600), min = Math.floor((s%3600)/60), sec = s%60;
  if (h > 0) return `${h}:${String(min).padStart(2,"0")}:${String(sec).padStart(2,"0")}`;
  return `${min}:${String(sec).padStart(2,"0")}`;
}
function fmtHR(hr)      { return hr ? Math.round(hr) + " bpm" : "—"; }
function fmtDate(iso)   { return new Date(iso).toLocaleDateString("en-GB",{ weekday:"long", day:"numeric", month:"long", year:"numeric" }); }
function capitalize(s)  { return s ? s.charAt(0).toUpperCase()+s.slice(1) : ""; }

const SPORT_EMOJI = { run:"🏃", ride:"🚴", swim:"🏊", walk:"🚶", hike:"🥾", unknown:"⚡" };

// ─── Metrics config ───────────────────────────────────────────────────────────

const METRICS = [
  {
    key: "ele",
    label: "Elevation",
    color: "#10b981",
    format: (v) => v != null ? `${v.toFixed(0)} m` : "—",
  },
  {
    key: "hr",
    label: "Heart rate",
    color: "#ef4444",
    format: (v) => v != null ? `${Math.round(v)} bpm` : "—",
  },
  {
    key: "pace",
    label: "Pace",
    color: "#3b82f6",
    invert: true,
    format: (v) => {
      if (v == null) return "—";
      const min = Math.floor(v);
      const sec = Math.round((v - min) * 60);
      return `${min}:${String(sec).padStart(2,"0")} /km`;
    },
  },
];

// ─── Normalization ────────────────────────────────────────────────────────────

function normalize(data, key, invert = false) {
  const vals = data.map((d) => d[key]).filter((v) => v != null);
  if (vals.length === 0) return data;
  const min = Math.min(...vals), max = Math.max(...vals);
  const range = max - min || 1;
  return data.map((d) => ({
    ...d,
    [`${key}_n`]: d[key] != null
      ? invert ? ((max - d[key]) / range) * 100 : ((d[key] - min) / range) * 100
      : null,
  }));
}

// ─── Find nearest GPS sample by elapsed time ─────────────────────────────────

function findNearest(samples, elapsedMs) {
  if (!samples.length || elapsedMs == null) return null;
  let best = null, bestDiff = Infinity;
  for (const s of samples) {
    if (s.elapsed_ms == null || s.latitude == null || s.longitude == null) continue;
    const diff = Math.abs(s.elapsed_ms - elapsedMs);
    if (diff < bestDiff) { bestDiff = diff; best = s; }
  }
  return best;
}

// ─── Route map (forwardRef) ───────────────────────────────────────────────────
// Protomaps styles → MapLibre GL with api.protomaps.com/styles/v2/{theme}.json
// Raster styles    → Leaflet tile layer (no key required)

// ─── Flyover helpers ─────────────────────────────────────────────────────────

function routeBearing(from, to) {
  const toRad = (d) => (d * Math.PI) / 180;
  const y = Math.sin(toRad(to[0] - from[0])) * Math.cos(toRad(to[1]));
  const x =
    Math.cos(toRad(from[1])) * Math.sin(toRad(to[1])) -
    Math.sin(toRad(from[1])) * Math.cos(toRad(to[1])) * Math.cos(toRad(to[0] - from[0]));
  return ((Math.atan2(y, x) * 180) / Math.PI + 360) % 360;
}

function lerp(a, b, t) { return a + (b - a) * t; }

// Build cumulative distances along a coord array so we can interpolate at constant speed.
function cumulativeDist(coords) {
  const d = [0];
  for (let i = 1; i < coords.length; i++) {
    const dx = (coords[i][0] - coords[i - 1][0]) * Math.cos((coords[i - 1][1] * Math.PI) / 180);
    const dy = coords[i][1] - coords[i - 1][1];
    d.push(d[i - 1] + Math.sqrt(dx * dx + dy * dy));
  }
  return d;
}

function interpolateCoord(coords, cumDist, d) {
  const total = cumDist[cumDist.length - 1];
  if (d <= 0) return coords[0];
  if (d >= total) return coords[coords.length - 1];
  for (let i = 1; i < cumDist.length; i++) {
    if (cumDist[i] >= d) {
      const t = (d - cumDist[i - 1]) / (cumDist[i] - cumDist[i - 1]);
      return [lerp(coords[i - 1][0], coords[i][0], t), lerp(coords[i - 1][1], coords[i][1], t)];
    }
  }
  return coords[coords.length - 1];
}

// ─── Route map (forwardRef) ───────────────────────────────────────────────────

const RouteMap = forwardRef(function RouteMap({ samples, mapStyleId, protoMapsKey, routeColor = "#3b82f6" }, ref) {
  const divRef      = useRef(null);
  const instRef     = useRef(null);
  const flyRef      = useRef(null);       // rAF id
  const flySpeedRef = useRef(1);          // live speed multiplier
  const [flyActive, setFlyActive] = useState(false);

  useImperativeHandle(ref, () => ({
    showMarker(elapsedMs, allSamples) {
      if (flyRef.current) return;
      const inst = instRef.current;
      if (!inst) return;
      const s = findNearest(allSamples, elapsedMs);
      if (!s) return;
      if (inst.type === "leaflet") {
        const ll = [s.latitude, s.longitude];
        if (inst.marker) { inst.marker.setLatLng(ll); }
        else { inst.marker = L.circleMarker(ll, { radius:7, color:"#fff", fillColor:"#f59e0b", fillOpacity:1, weight:2.5 }).addTo(inst.map); }
      } else if (inst.type === "maplibre") {
        try {
          inst.map.getSource("hover-pt")?.setData({ type:"Feature", geometry:{ type:"Point", coordinates:[s.longitude, s.latitude] } });
          if (inst.loaded) inst.map.setLayoutProperty("hover-pt", "visibility", "visible");
        } catch {}
      }
    },
    hideMarker() {
      const inst = instRef.current;
      if (!inst) return;
      if (inst.type === "leaflet" && inst.marker) {
        inst.map.removeLayer(inst.marker);
        inst.marker = null;
      } else if (inst.type === "maplibre" && inst.loaded) {
        try { inst.map.setLayoutProperty("hover-pt", "visibility", "none"); } catch {}
      }
    },
    canFlyover() {
      const inst = instRef.current;
      return inst?.type === "maplibre" && inst?.loaded;
    },
    setFlyoverSpeed(n) {
      flySpeedRef.current = n;
    },
    startFlyover(onEnd) {
      const inst = instRef.current;
      if (!inst || inst.type !== "maplibre" || !inst.loaded) return;
      if (flyRef.current) return;

      const gpsCoords = samples
        .filter((s) => s.latitude != null && s.longitude != null)
        .map((s) => [s.longitude, s.latitude]);
      if (gpsCoords.length < 2) return;

      const mlMap = inst.map;

      // ── Terrain ────────────────────────────────────────────────────────
      if (!mlMap.getSource("_terrain")) {
        mlMap.addSource("_terrain", {
          type: "raster-dem",
          tiles: ["https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png"],
          encoding: "terrarium",
          tileSize: 256,
          maxzoom: 14,
        });
        mlMap.setTerrain({ source: "_terrain", exaggeration: 2.0 });
      }

      // ── Sky / atmosphere ────────────────────────────────────────────────
      try {
        mlMap.setSky({
          "sky-color": "#1e293b",
          "sky-horizon-blend": 0.4,
          "horizon-color": "#60a5fa",
          "horizon-fog-blend": 0.4,
          "fog-color": "#0f172a",
          "fog-ground-blend": 0.5,
        });
      } catch {}

      // ── Position marker ─────────────────────────────────────────────────
      if (!mlMap.getSource("_fly-pt")) {
        mlMap.addSource("_fly-pt", {
          type: "geojson",
          data: { type: "Feature", geometry: { type: "Point", coordinates: gpsCoords[0] } },
        });
        mlMap.addLayer({
          id: "_fly-pt",
          type: "circle",
          source: "_fly-pt",
          paint: {
            "circle-radius": 8,
            "circle-color": "#f59e0b",
            "circle-stroke-color": "#ffffff",
            "circle-stroke-width": 2.5,
          },
        });
      }

      const cumDist    = cumulativeDist(gpsCoords);
      const totalDist  = cumDist[cumDist.length - 1];
      const lngs       = gpsCoords.map(([ln]) => ln);
      const lats       = gpsCoords.map(([, la]) => la);
      const extent     = Math.max(Math.max(...lngs) - Math.min(...lngs), Math.max(...lats) - Math.min(...lats));
      // BASE_DURATION is how long 1× speed takes (45–120 s).
      const BASE_MS    = Math.max(45000, Math.min(120000, extent * 400000));
      const LOOK_AHEAD = totalDist * 0.025;
      const SMOOTH     = 0.07;

      let progress     = 0; // 0–1; advanced each frame by dt / (BASE_MS / speed)
      let lastTime     = null;
      let smoothBearing = routeBearing(gpsCoords[0], gpsCoords[Math.min(5, gpsCoords.length - 1)]);

      setFlyActive(true);

      function frame(now) {
        if (lastTime !== null) {
          const dt = now - lastTime;
          progress = Math.min(progress + (dt / BASE_MS) * flySpeedRef.current, 1);
        }
        lastTime = now;

        const d     = progress * totalDist;
        const cur   = interpolateCoord(gpsCoords, cumDist, d);
        const ahead = interpolateCoord(gpsCoords, cumDist, Math.min(d + LOOK_AHEAD, totalDist));

        const targetBearing = routeBearing(cur, ahead);
        const diff = ((targetBearing - smoothBearing + 540) % 360) - 180;
        smoothBearing = (smoothBearing + diff * SMOOTH + 360) % 360;

        mlMap.jumpTo({ center: cur, bearing: smoothBearing, pitch: 62, zoom: 15.5 });

        // Update position marker.
        try {
          mlMap.getSource("_fly-pt")?.setData({ type: "Feature", geometry: { type: "Point", coordinates: cur } });
        } catch {}

        if (progress < 1) {
          flyRef.current = requestAnimationFrame(frame);
        } else {
          doStop(mlMap, gpsCoords);
        }
      }

      flyRef.current = requestAnimationFrame(frame);

      function doStop(map, coords) {
        if (flyRef.current) { cancelAnimationFrame(flyRef.current); flyRef.current = null; }
        try { map.setTerrain(null); } catch {}
        try { map.setSky({}); } catch {}
        try { map.removeLayer("_fly-pt"); } catch {}
        try { map.removeSource("_fly-pt"); } catch {}
        try { map.removeSource("_terrain"); } catch {}
        const allLngs = coords.map(([ln]) => ln);
        const allLats = coords.map(([, la]) => la);
        map.easeTo({ pitch: 0, bearing: 0, duration: 600 });
        setTimeout(() => {
          map.fitBounds(
            [[Math.min(...allLngs), Math.min(...allLats)], [Math.max(...allLngs), Math.max(...allLats)]],
            { padding: 40, duration: 900 }
          );
        }, 650);
        setFlyActive(false);
        onEnd?.(); // notify parent so it can sync its own state
      }

      inst._stopFlyover = () => doStop(mlMap, gpsCoords);
    },
    stopFlyover() {
      const inst = instRef.current;
      if (inst?._stopFlyover) {
        inst._stopFlyover();
      } else {
        if (flyRef.current) { cancelAnimationFrame(flyRef.current); flyRef.current = null; }
        setFlyActive(false);
      }
    },
    isFlyoverActive() { return flyRef.current != null; },
    resetView() {
      const inst = instRef.current;
      if (!inst) return;
      if (inst.type === "maplibre" && inst.loaded && inst.bounds) {
        inst.map.fitBounds(inst.bounds, { padding: 30, duration: 600 });
      } else if (inst.type === "leaflet" && inst.leafletBounds) {
        inst.map.fitBounds(inst.leafletBounds, { padding: [20, 20] });
      }
    },
  }));

  useEffect(() => {
    if (!divRef.current) return;

    const gpsPoints = samples.filter((s) => s.latitude != null && s.longitude != null);
    if (gpsPoints.length === 0) return;

    // [lng, lat] for MapLibre; [lat, lng] for Leaflet
    const lnglats  = gpsPoints.map((s) => [s.longitude, s.latitude]);
    const latlngs  = gpsPoints.map((s) => [s.latitude,  s.longitude]);

    // Clean up previous instance
    if (instRef.current) {
      instRef.current.map.remove();
      instRef.current = null;
    }

    const style = MAP_STYLES.find((s) => s.id === mapStyleId) ?? MAP_STYLES[0];

    if (style.type === "protomaps" && protoMapsKey) {
      // ── MapLibre GL path ──────────────────────────────────────────────────
      import("maplibre-gl").then(({ Map: MlMap, NavigationControl }) => {
        if (!divRef.current) return;

        const lngs = lnglats.map(([ln]) => ln);
        const lats = lnglats.map(([, la]) => la);
        const bounds = [[Math.min(...lngs), Math.min(...lats)], [Math.max(...lngs), Math.max(...lats)]];

        const mlMap = new MlMap({
          container: divRef.current,
          style: `https://api.protomaps.com/styles/v2/${style.theme}.json?key=${protoMapsKey}`,
          bounds,
          fitBoundsOptions: { padding: 30 },
          attributionControl: true,
        });

        const mlBounds = [[Math.min(...lngs), Math.min(...lats)], [Math.max(...lngs), Math.max(...lats)]];
        const inst = { type: "maplibre", map: mlMap, loaded: false, marker: null, bounds: mlBounds };
        instRef.current = inst;

        mlMap.addControl(new NavigationControl({ showCompass: false }), "top-left");

        mlMap.on("load", () => {
          if (instRef.current !== inst) return; // unmounted / re-rendered
          inst.loaded = true;

          // Route line
          mlMap.addSource("route", { type:"geojson", data:{ type:"Feature", geometry:{ type:"LineString", coordinates: lnglats } } });
          mlMap.addLayer({ id:"route", type:"line", source:"route",
            layout: { "line-join":"round", "line-cap":"round" },
            paint:  { "line-color": routeColor, "line-width": 3.5, "line-opacity": 0.9 },
          });

          // Start dot (green)
          mlMap.addSource("start-pt", { type:"geojson", data:{ type:"Feature", geometry:{ type:"Point", coordinates: lnglats[0] } } });
          mlMap.addLayer({ id:"start-pt", type:"circle", source:"start-pt",
            paint: { "circle-radius":6, "circle-color":"#10b981", "circle-stroke-color":"#fff", "circle-stroke-width":2 },
          });

          // End dot (red)
          mlMap.addSource("end-pt", { type:"geojson", data:{ type:"Feature", geometry:{ type:"Point", coordinates: lnglats[lnglats.length-1] } } });
          mlMap.addLayer({ id:"end-pt", type:"circle", source:"end-pt",
            paint: { "circle-radius":6, "circle-color":"#ef4444", "circle-stroke-color":"#fff", "circle-stroke-width":2 },
          });

          // Hover marker (hidden until chart hover)
          mlMap.addSource("hover-pt", { type:"geojson", data:{ type:"FeatureCollection", features:[] } });
          mlMap.addLayer({ id:"hover-pt", type:"circle", source:"hover-pt",
            layout: { visibility:"none" },
            paint:  { "circle-radius":7, "circle-color":"#f59e0b", "circle-stroke-color":"#fff", "circle-stroke-width":2.5 },
          });
        });

        mlMap.on("error", (e) => console.error("MapLibre error:", e));
      });
    } else {
      // ── Leaflet raster path ───────────────────────────────────────────────
      const leafletMap = L.map(divRef.current, { zoomControl: true, scrollWheelZoom: false });
      const raster = style.type === "raster" ? style : MAP_STYLES[0];
      L.tileLayer(raster.url, { attribution: raster.attr, maxZoom: 19 }).addTo(leafletMap);

      const poly = L.polyline(latlngs, { color: routeColor, weight: 3.5, opacity: 0.9 });
      poly.addTo(leafletMap);
      leafletMap.fitBounds(poly.getBounds(), { padding: [20, 20] });
      L.circleMarker(latlngs[0],               { radius:6, color:"#fff", fillColor:"#10b981", fillOpacity:1, weight:2 }).addTo(leafletMap);
      L.circleMarker(latlngs[latlngs.length-1],{ radius:6, color:"#fff", fillColor:"#ef4444", fillOpacity:1, weight:2 }).addTo(leafletMap);

      instRef.current = { type: "leaflet", map: leafletMap, marker: null, leafletBounds: poly.getBounds() };
    }

    return () => {
      if (instRef.current) { instRef.current.map.remove(); instRef.current = null; }
    };
  }, [samples, mapStyleId, protoMapsKey, routeColor]);

  return (
    <div className="relative">
      <div ref={divRef} className="w-full h-80 rounded-xl border border-slate-200 dark:border-slate-700 z-0" />
      {flyActive && (
        <button
          onClick={() => instRef.current?._stopFlyover?.()}
          className="absolute top-3 right-3 z-10 flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-black/60 text-white text-xs font-medium hover:bg-black/80 transition-colors backdrop-blur-sm"
        >
          <Square size={12} fill="currentColor" /> Stop flyover
        </button>
      )}
    </div>
  );
});

// ─── Activity chart ───────────────────────────────────────────────────────────
// Renders all 3 metrics overlaid, with `primary` highlighted and others faded.
// If `onPrimaryChange` is provided, shows a picker for which metric is primary.

function ActivityChart({ data, primary, onPrimaryChange, onHover, onLeave }) {
  const ct = useChartTheme();
  let d = data;
  for (const m of METRICS) d = normalize(d, m.key, m.invert);

  const hasAny = METRICS.some((m) => data.some((p) => p[m.key] != null));
  if (!hasAny) return null;

  function tooltipContent({ active, payload, label }) {
    if (!active || !payload?.length || label == null) return null;
    const pt = payload[0]?.payload;
    if (!pt) return null;
    return (
      <div className="bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg shadow-md px-2.5 py-2 text-xs pointer-events-none">
        <p className="text-slate-500 dark:text-slate-400 mb-1">⏱ {fmtElapsed(label)}</p>
        {METRICS.filter((m) => pt[m.key] != null).map((m) => (
          <p key={m.key} style={{ color: m.color }}>{m.label}: {m.format(pt[m.key])}</p>
        ))}
      </div>
    );
  }

  return (
    <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
      <div className="flex items-center justify-between mb-3 gap-2">
        {/* Legend */}
        <div className="flex gap-4">
          {METRICS.map((m) => {
            const active = primary === m.key;
            return (
              <span
                key={m.key}
                className={`flex items-center gap-1.5 text-xs transition-colors ${active ? "text-slate-700 dark:text-slate-200 font-semibold" : "text-slate-400 dark:text-slate-500"}`}
              >
                <span
                  className="inline-block w-4 rounded-full"
                  style={{ height: active ? 3 : 2, background: m.color, opacity: active ? 1 : 0.35 }}
                />
                {m.label}
              </span>
            );
          })}
        </div>

        {/* Picker (shown only on the top chart) */}
        {onPrimaryChange && (
          <div className="flex gap-1 shrink-0">
            {METRICS.map((m) => (
              <button
                key={m.key}
                onClick={() => onPrimaryChange(m.key)}
                className={`px-2.5 py-0.5 rounded-full text-xs font-medium transition-colors ${
                  primary === m.key
                    ? "bg-slate-800 text-white"
                    : "bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:bg-slate-200 dark:hover:bg-slate-700"
                }`}
              >
                {m.label}
              </button>
            ))}
          </div>
        )}
      </div>

      <ResponsiveContainer width="100%" height={130}>
        <ComposedChart
          data={d}
          onMouseMove={(e) => {
            const ms = e?.activeLabel ?? e?.activePayload?.[0]?.payload?.elapsedMs;
            if (ms != null) onHover?.(ms);
          }}
          onMouseLeave={() => onLeave?.()}
        >
          <XAxis
            dataKey="elapsedMs"
            tickFormatter={fmtElapsed}
            tick={{ fontSize: 10, fill: ct.tickFill }}
            tickLine={false}
            axisLine={false}
            minTickGap={80}
          />
          <YAxis hide domain={[0, 100]} />
          <Tooltip content={tooltipContent} offset={24} />
          {METRICS.map((m) => {
            const active = primary === m.key;
            return (
              <Line
                key={m.key}
                type="monotone"
                dataKey={`${m.key}_n`}
                stroke={m.color}
                strokeWidth={active ? 2.5 : 1.2}
                strokeOpacity={active ? 1 : 0.22}
                dot={false}
                connectNulls
                name={`${m.key}_n`}
                isAnimationActive={false}
              />
            );
          })}
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  );
}

// ─── Skeleton ─────────────────────────────────────────────────────────────────

function Skeleton({ className = "" }) {
  return <div className={`bg-slate-200 dark:bg-slate-700 rounded-lg animate-pulse ${className}`} />;
}

function Stat({ label, value }) {
  return (
    <div className="flex flex-col items-center text-center min-w-[80px]">
      <span className="text-xs text-slate-400 dark:text-slate-500 font-medium uppercase tracking-wide">{label}</span>
      <span className="text-xl font-bold text-slate-800 dark:text-slate-100 mt-0.5">{value}</span>
    </div>
  );
}

// ─── HR Zones ────────────────────────────────────────────────────────────────

const HR_ZONES = [
  { name: "Z1", label: "Recovery",  min: 0,    max: 0.60, color: "#94a3b8" },
  { name: "Z2", label: "Base",      min: 0.60, max: 0.70, color: "#3b82f6" },
  { name: "Z3", label: "Aerobic",   min: 0.70, max: 0.80, color: "#10b981" },
  { name: "Z4", label: "Threshold", min: 0.80, max: 0.90, color: "#f97316" },
  { name: "Z5", label: "Max",       min: 0.90, max: Infinity, color: "#ef4444" },
];

function computeHRZones(samples, maxHR) {
  const withHR = samples.filter((s) => s.hr != null);
  if (withHR.length === 0) return null;
  const counts = HR_ZONES.map(() => 0);
  for (const s of withHR) {
    const pct = s.hr / maxHR;
    const idx = HR_ZONES.findIndex((z) => pct >= z.min && pct < z.max);
    if (idx >= 0) counts[idx]++;
  }
  const total = withHR.length;
  return HR_ZONES.map((z, i) => ({ ...z, pct: (counts[i] / total) * 100, count: counts[i] }));
}

function fmtZoneTime(pct, elapsedMs) {
  if (!elapsedMs) return "";
  const ms = (pct / 100) * elapsedMs;
  const totalSec = Math.round(ms / 1000);
  const m = Math.floor(totalSec / 60), s = totalSec % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}

function HRZones({ samples, elapsedMs, maxHR }) {
  if (!maxHR) {
    return (
      <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
        <p className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-1">HR Zones</p>
        <p className="text-xs text-slate-400 dark:text-slate-500">
          Set your max heart rate in{" "}
          <a href="/settings" className="text-blue-500 underline underline-offset-2">Settings</a>{" "}
          to see zone breakdown.
        </p>
      </div>
    );
  }

  const zones = computeHRZones(samples, maxHR);
  if (!zones) return null;

  const hasAnyHR = zones.some((z) => z.count > 0);
  if (!hasAnyHR) return null;

  return (
    <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-5">
      <p className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-4">HR Zones</p>
      <div className="space-y-2.5">
        {zones.map((z) => (
          <div key={z.name} className="flex items-center gap-3">
            <span className="w-16 text-xs font-medium text-slate-500 dark:text-slate-400 shrink-0">
              {z.name} <span className="font-normal text-slate-400 dark:text-slate-500">{z.label}</span>
            </span>
            <div className="flex-1 h-3 bg-slate-100 dark:bg-slate-800 rounded-full overflow-hidden">
              <div
                className="h-full rounded-full transition-all"
                style={{ width: `${z.pct}%`, background: z.color }}
              />
            </div>
            <span className="w-10 text-right text-xs text-slate-500 dark:text-slate-400 shrink-0">
              {z.pct.toFixed(0)}%
            </span>
            <span className="w-12 text-right text-xs text-slate-400 dark:text-slate-500 shrink-0">
              {fmtZoneTime(z.pct, elapsedMs)}
            </span>
          </div>
        ))}
      </div>
      <p className="text-xs text-slate-300 dark:text-slate-400 mt-3">Max HR: {maxHR} bpm</p>
    </div>
  );
}

// ─── Laps table ──────────────────────────────────────────────────────────────

// ─── Auto-splits ─────────────────────────────────────────────────────────────

function computeSplits(samples, unitMeters) {
  // samples must have distance_meters and elapsed_ms; filter to valid rows
  const pts = samples
    .filter((s) => s.distance_meters != null && s.elapsed_ms != null)
    .sort((a, b) => a.distance_meters - b.distance_meters);
  if (pts.length < 2) return [];

  const total = pts[pts.length - 1].distance_meters;
  const numSplits = Math.floor(total / unitMeters);
  if (numSplits === 0) return [];

  // linear-interpolate a value at a given distance crossing
  function interp(a, b, dist, field) {
    const t = (dist - a.distance_meters) / (b.distance_meters - a.distance_meters);
    const va = a[field], vb = b[field];
    if (va == null || vb == null) return null;
    return va + (vb - va) * t;
  }

  const splits = [];
  let pIdx = 0;

  for (let km = 1; km <= numSplits; km++) {
    const crossDist = km * unitMeters;
    const prevDist  = (km - 1) * unitMeters;

    // advance to the pair that straddles crossDist
    while (pIdx < pts.length - 1 && pts[pIdx + 1].distance_meters < crossDist) pIdx++;
    if (pIdx >= pts.length - 1) break;
    const a = pts[pIdx], b = pts[pIdx + 1];

    const eleAtCross = interp(a, b, crossDist, "elevation_meters");

    // find pair straddling prevDist for elevation at split start
    let qIdx = 0;
    while (qIdx < pts.length - 1 && pts[qIdx + 1].distance_meters < prevDist) qIdx++;
    const p = pts[qIdx], q = pts[qIdx + 1];
    const eleAtPrev = prevDist === 0 ? pts[0].elevation_meters : interp(p, q, prevDist, "elevation_meters");

    // samples within this split window — pace and HR computed from them
    const inKm = pts.filter((s) => s.distance_meters >= prevDist && s.distance_meters <= crossDist);

    // pace: average instantaneous speed → seconds per unit distance
    const speeds = inKm.map((s) => s.speed_meters_per_second).filter((v) => v != null && v > 0);
    const avgSpeed = speeds.length ? speeds.reduce((a, v) => a + v, 0) / speeds.length : null;
    const paceSecPerUnit = avgSpeed ? unitMeters / avgSpeed : null;

    const hrs = inKm.map((s) => s.heart_rate).filter((v) => v != null);
    const avgHr = hrs.length ? hrs.reduce((a, b) => a + b, 0) / hrs.length : null;

    const eleDelta = (eleAtCross != null && eleAtPrev != null) ? eleAtCross - eleAtPrev : null;

    splits.push({ km, paceSecPerUnit, avgHr, eleDelta });
  }
  return splits;
}

function fmtSplitPace(s, isKm) {
  if (s == null) return "—";
  const min = Math.floor(s / 60), sec = Math.round(s % 60);
  return `${min}:${String(sec).padStart(2, "0")} /${isKm ? "km" : "mi"}`;
}

function SplitsTable({ samples, imperial = false }) {
  const [unit, setUnit] = useState(imperial ? "mi" : "km");
  const isKm = unit === "km";
  const unitMeters = isKm ? 1000 : 1609.34;

  const splits = computeSplits(samples, unitMeters);
  if (splits.length === 0) return null;

  return (
    <div className="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 overflow-hidden">
      <div className="flex items-center justify-between px-5 py-4 border-b border-slate-100 dark:border-slate-800">
        <p className="text-sm font-medium text-slate-600 dark:text-slate-300">
          Splits <span className="text-slate-400 dark:text-slate-500 font-normal">({splits.length})</span>
        </p>
        <div className="flex rounded-lg border border-slate-200 dark:border-slate-700 overflow-hidden text-xs font-medium">
          {["km", "mi"].map((u) => (
            <button
              key={u}
              onClick={() => setUnit(u)}
              className={[
                "px-2.5 py-1 transition-colors",
                unit === u ? "bg-slate-800 text-white" : "text-slate-500 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800/50",
              ].join(" ")}
            >
              {u}
            </button>
          ))}
        </div>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-xs text-slate-400 dark:text-slate-500 uppercase tracking-wide border-b border-slate-100 dark:border-slate-800">
              {[isKm ? "Km" : "Mile", "Pace", "Avg HR", "Elev Δ"].map((h) => (
                <th key={h} className={`px-5 py-2 font-semibold ${h === (isKm ? "Km" : "Mile") ? "text-left" : "text-right"}`}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {splits.map((s, i) => (
              <tr key={s.km} className={i < splits.length - 1 ? "border-b border-slate-100 dark:border-slate-800" : ""}>
                <td className="px-5 py-2.5 text-slate-600 dark:text-slate-300 font-medium">{s.km}</td>
                <td className="px-5 py-2.5 text-right text-slate-600 dark:text-slate-300 font-mono text-xs">{fmtSplitPace(s.paceSecPerUnit, isKm)}</td>
                <td className="px-5 py-2.5 text-right text-slate-500 dark:text-slate-400">{s.avgHr != null ? `${Math.round(s.avgHr)} bpm` : "—"}</td>
                <td className={`px-5 py-2.5 text-right font-medium ${s.eleDelta > 0 ? "text-emerald-600" : s.eleDelta < 0 ? "text-rose-500" : "text-slate-400 dark:text-slate-500"}`}>
                  {s.eleDelta != null ? `${s.eleDelta > 0 ? "+" : ""}${fmtEle(Math.abs(s.eleDelta), imperial)}` : "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function LapsTable({ laps }) {
  if (!laps?.length) return null;
  return (
    <div className="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 overflow-hidden">
      <p className="text-sm font-medium text-slate-600 dark:text-slate-300 px-5 py-4 border-b border-slate-100 dark:border-slate-800">
        Laps <span className="text-slate-400 dark:text-slate-500 font-normal">({laps.length})</span>
      </p>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-xs text-slate-400 dark:text-slate-500 uppercase tracking-wide border-b border-slate-100 dark:border-slate-800">
              {["Lap","Distance","Duration","Avg HR","Max HR"].map((h) => (
                <th key={h} className={`px-5 py-2 font-semibold ${h==="Lap" ? "text-left" : "text-right"}`}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {laps.map((lap, i) => (
              <tr key={i} className={i < laps.length-1 ? "border-b border-slate-100 dark:border-slate-800" : ""}>
                <td className="px-5 py-2.5 text-slate-600 dark:text-slate-300 font-medium">#{lap.index+1}</td>
                <td className="px-5 py-2.5 text-right text-slate-600 dark:text-slate-300">{fmtDist(lap.distance_meters)}</td>
                <td className="px-5 py-2.5 text-right text-slate-600 dark:text-slate-300">{fmtDuration(lap.duration_ms)}</td>
                <td className="px-5 py-2.5 text-right text-slate-600 dark:text-slate-300">{fmtHR(lap.average_heart_rate)}</td>
                <td className="px-5 py-2.5 text-right text-slate-600 dark:text-slate-300">{fmtHR(lap.max_heart_rate)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ─── Weather ─────────────────────────────────────────────────────────────────

const WMO = {
  0:"Clear sky",1:"Mainly clear",2:"Partly cloudy",3:"Overcast",
  45:"Foggy",48:"Icy fog",51:"Light drizzle",53:"Drizzle",55:"Heavy drizzle",
  61:"Light rain",63:"Rain",65:"Heavy rain",71:"Light snow",73:"Snow",75:"Heavy snow",
  77:"Snow grains",80:"Light showers",81:"Showers",82:"Heavy showers",
  85:"Snow showers",86:"Heavy snow showers",95:"Thunderstorm",96:"Thunderstorm w/ hail",99:"Thunderstorm w/ heavy hail",
};
const WMO_EMOJI = {
  0:"☀️",1:"🌤️",2:"⛅",3:"☁️",45:"🌫️",48:"🌫️",
  51:"🌦️",53:"🌧️",55:"🌧️",61:"🌦️",63:"🌧️",65:"🌧️",
  71:"🌨️",73:"❄️",75:"❄️",77:"🌨️",80:"🌦️",81:"🌧️",82:"⛈️",
  85:"🌨️",86:"❄️",95:"⛈️",96:"⛈️",99:"⛈️",
};

function WeatherChip({ activityId, imperial }) {
  const [wx, setWx] = useState(null);
  useEffect(() => {
    api.activities.weather(activityId).then(setWx).catch(() => {});
  }, [activityId]);
  if (!wx || wx.temperature_c == null) return null;

  const temp = imperial
    ? `${Math.round(wx.temperature_c * 9/5 + 32)}°F`
    : `${Math.round(wx.temperature_c)}°C`;
  const wind = imperial
    ? `${Math.round((wx.wind_speed_ms ?? 0) * 2.237)} mph`
    : `${Math.round((wx.wind_speed_ms ?? 0) * 3.6)} km/h`;
  const emoji = WMO_EMOJI[wx.weather_code] ?? "🌡️";
  const label = WMO[wx.weather_code] ?? "";

  return (
    <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300 text-xs font-medium">
      {emoji} {temp} · {wind}{label ? ` · ${label}` : ""}
    </span>
  );
}

// ─── Similar routes ──────────────────────────────────────────────────────────

function SimilarRoutes({ activityId }) {
  const [similar, setSimilar] = useState(null);

  useEffect(() => {
    api.activities.similar(activityId).then(setSimilar).catch(() => setSimilar([]));
  }, [activityId]);

  if (!similar || similar.length === 0) return null;

  const bestPace = Math.min(...similar.map((s) => s.average_pace_seconds_per_km).filter(Boolean));

  return (
    <div className="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 overflow-hidden">
      <p className="text-sm font-medium text-slate-600 dark:text-slate-300 px-5 py-4 border-b border-slate-100 dark:border-slate-800">
        Similar routes <span className="text-slate-400 dark:text-slate-500 font-normal">({similar.length})</span>
      </p>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-xs text-slate-400 dark:text-slate-500 uppercase tracking-wide border-b border-slate-100 dark:border-slate-800">
              {["Date", "Distance", "Pace", "vs Best", "Avg HR"].map((h) => (
                <th key={h} className={`px-5 py-2 font-semibold ${h === "Date" ? "text-left" : "text-right"}`}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {similar.map((s, i) => {
              const paceDelta = s.average_pace_seconds_per_km && bestPace
                ? s.average_pace_seconds_per_km - bestPace : null;
              return (
                <tr
                  key={s.id}
                  className={`cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors ${i < similar.length - 1 ? "border-b border-slate-100 dark:border-slate-800" : ""}`}
                  onClick={() => window.location.href = `/activities/${s.id}`}
                >
                  <td className="px-5 py-2.5 text-slate-600 dark:text-slate-300">
                    {new Date(s.start_at).toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" })}
                  </td>
                  <td className="px-5 py-2.5 text-right text-slate-600 dark:text-slate-300">{fmtDist(s.distance_meters)}</td>
                  <td className="px-5 py-2.5 text-right text-slate-600 dark:text-slate-300 font-mono text-xs">{fmtPace(s.average_pace_seconds_per_km)}</td>
                  <td className={`px-5 py-2.5 text-right font-medium text-xs ${paceDelta === 0 ? "text-blue-600" : paceDelta < 0 ? "text-emerald-600" : "text-slate-400 dark:text-slate-500"}`}>
                    {paceDelta === null ? "—" : paceDelta === 0 ? "Best" : `+${Math.floor(paceDelta / 60)}:${String(Math.round(paceDelta % 60)).padStart(2, "0")}`}
                  </td>
                  <td className="px-5 py-2.5 text-right text-slate-500 dark:text-slate-400">{fmtHR(s.average_heart_rate)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ─── Gear assignment ─────────────────────────────────────────────────────────

const GEAR_EMOJI = { shoes: "👟", bike: "🚴", watch: "⌚", other: "🎒" };

function GearPicker({ allGear, selected, onToggle, onSave, onClose }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={onClose}>
      <div className="bg-white dark:bg-slate-900 rounded-2xl shadow-xl p-5 w-72 max-h-[28rem] flex flex-col" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4">
          <p className="text-sm font-semibold text-slate-800 dark:text-slate-100">Assign gear</p>
          <button onClick={onClose} className="text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 text-lg leading-none">✕</button>
        </div>
        {allGear.length === 0 ? (
          <p className="text-sm text-slate-400 dark:text-slate-500 text-center py-4">
            No gear yet. <Link to="/gear" className="text-blue-500 underline">Add some</Link>
          </p>
        ) : (
          <div className="flex-1 overflow-y-auto space-y-1">
            {allGear.map((g) => (
              <label key={g.id} className="flex items-center gap-3 px-2 py-2 rounded-xl hover:bg-slate-50 dark:hover:bg-slate-800/50 cursor-pointer">
                <input
                  type="checkbox"
                  checked={selected.has(g.id)}
                  onChange={() => onToggle(g.id)}
                  className="w-4 h-4 rounded accent-blue-600"
                />
                <span className="text-lg">{GEAR_EMOJI[g.type] ?? "🎒"}</span>
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-slate-700 dark:text-slate-200 truncate">{g.name}</p>
                  {g.brand_model && <p className="text-xs text-slate-400 dark:text-slate-500 truncate">{g.brand_model}</p>}
                </div>
              </label>
            ))}
          </div>
        )}
        <div className="flex gap-2 mt-4 pt-4 border-t border-slate-100 dark:border-slate-800">
          <button onClick={onClose} className="flex-1 px-3 py-2 text-sm text-slate-600 dark:text-slate-300 border border-slate-200 dark:border-slate-700 rounded-xl hover:bg-slate-50 dark:hover:bg-slate-800/50">Cancel</button>
          <button onClick={onSave} className="flex-1 px-3 py-2 text-sm font-medium bg-blue-600 text-white rounded-xl hover:bg-blue-700">Save</button>
        </div>
      </div>
    </div>
  );
}

function GearSection({ assigned, allGear, onSave }) {
  const [open, setOpen]     = useState(false);
  const [selected, setSelected] = useState(() => new Set(assigned.map((g) => g.id)));
  const [saving, setSaving] = useState(false);

  useEffect(() => { setSelected(new Set(assigned.map((g) => g.id))); }, [assigned]);

  function toggle(id) {
    setSelected((prev) => { const s = new Set(prev); s.has(id) ? s.delete(id) : s.add(id); return s; });
  }

  async function save() {
    setSaving(true);
    try { await onSave([...selected]); setOpen(false); }
    finally { setSaving(false); }
  }

  return (
    <>
      <div className="flex items-center gap-1.5 flex-wrap">
        {assigned.map((g) => (
          <span key={g.id} className="flex items-center gap-1 px-2.5 py-1 bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300 rounded-full text-xs font-medium">
            {GEAR_EMOJI[g.type] ?? "🎒"} {g.name}
          </span>
        ))}
        <button
          onClick={() => setOpen(true)}
          className="flex items-center gap-1 px-2.5 py-1 rounded-full text-xs border border-dashed border-slate-300 dark:border-slate-600 text-slate-400 dark:text-slate-500 hover:border-slate-500 hover:text-slate-600 dark:hover:text-slate-300 transition-colors"
        >
          {assigned.length ? "Edit gear" : "+ Add gear"}
        </button>
      </div>
      {open && (
        <GearPicker
          allGear={allGear}
          selected={selected}
          onToggle={toggle}
          onSave={save}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  );
}

// ─── Share card: canvas capture + overlay ─────────────────────────────────────

const CARD_FORMATS = [
  { id: "16:9",  label: "16:9",  sub: "Twitter · OG · LinkedIn" },
  { id: "9:16",  label: "9:16",  sub: "Stories · Reels" },
];

// Fit text inside maxWidth, appending "…" if needed.
function fitText(ctx, text, maxWidth) {
  if (ctx.measureText(text).width <= maxWidth) return text;
  let s = text;
  while (s.length > 1 && ctx.measureText(s + "…").width > maxWidth) s = s.slice(0, -1);
  return s + "…";
}

function drawRouteOnCanvas(ctx, w, h, gpsCoords, routeColor, padBottom) {
  if (gpsCoords.length < 2) return;
  const lngs = gpsCoords.map(([ln]) => ln);
  const lats  = gpsCoords.map(([, la]) => la);
  const minLng = Math.min(...lngs), maxLng = Math.max(...lngs);
  const minLat = Math.min(...lats), maxLat = Math.max(...lats);

  // Correct longitude distortion: 1° lng ≠ 1° lat in distance
  const avgLat = (minLat + maxLat) / 2;
  const cosLat = Math.cos((avgLat * Math.PI) / 180);

  const padT = 40, padL = 40, padR = 40;
  const availW = w - padL - padR;
  const availH = h - padT - padBottom;

  const rangeW = (maxLng - minLng) * cosLat || 0.0001;
  const rangeH = maxLat - minLat || 0.0001;
  const scale  = Math.min(availW / rangeW, availH / rangeH);
  const oX = padL + (availW - rangeW * scale) / 2;
  const oY = padT + (availH - rangeH * scale) / 2;

  function toXY([lng, lat]) {
    return [oX + (lng - minLng) * cosLat * scale, oY + (maxLat - lat) * scale];
  }
  const pts = gpsCoords.map(toXY);

  ctx.save();
  ctx.lineCap = "round";
  ctx.lineJoin = "round";
  ctx.strokeStyle = routeColor;
  ctx.lineWidth = 12;
  ctx.globalAlpha = 0.25;
  ctx.beginPath();
  ctx.moveTo(pts[0][0], pts[0][1]);
  for (const [x, y] of pts.slice(1)) ctx.lineTo(x, y);
  ctx.stroke();
  ctx.lineWidth = 3.5;
  ctx.globalAlpha = 0.95;
  ctx.beginPath();
  ctx.moveTo(pts[0][0], pts[0][1]);
  for (const [x, y] of pts.slice(1)) ctx.lineTo(x, y);
  ctx.stroke();
  ctx.restore();

  function dot(x, y, fill) {
    ctx.save();
    ctx.fillStyle = fill;
    ctx.strokeStyle = "#fff";
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.arc(x, y, 6, 0, Math.PI * 2);
    ctx.fill();
    ctx.stroke();
    ctx.restore();
  }
  dot(pts[0][0], pts[0][1], "#10b981");
  dot(pts[pts.length - 1][0], pts[pts.length - 1][1], "#ef4444");
}

function pillRect(ctx, x, y, w, h, r) {
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.lineTo(x + w - r, y);
  ctx.arcTo(x + w, y, x + w, y + r, r);
  ctx.lineTo(x + w, y + h - r);
  ctx.arcTo(x + w, y + h, x + w - r, y + h, r);
  ctx.lineTo(x + r, y + h);
  ctx.arcTo(x, y + h, x, y + h - r, r);
  ctx.lineTo(x, y + r);
  ctx.arcTo(x, y, x + r, y, r);
  ctx.closePath();
}

function drawCardOverlay(ctx, w, h, format, stats, transparent = false) {
  const { name, sport, date, distNum, distUnit, dur, pace, ele } = stats;
  const sans = "system-ui, -apple-system, 'Segoe UI', Arial, sans-serif";
  const mono = "'Courier New', Courier, monospace";

  if (transparent) {
    // All transparent text uses white + shadow so it reads on any background
    ctx.save();
    ctx.shadowColor = "rgba(0,0,0,0.80)";
    ctx.shadowBlur = 10;

    const m = 16;
    if (format === "16:9") {
      const pad = Math.round(w * 0.04);
      const row1 = h - Math.round(h * 0.210);
      const row2 = h - Math.round(h * 0.155);
      const row3 = h - Math.round(h * 0.052);

      ctx.fillStyle = "#ffffff";
      ctx.font = `700 ${Math.round(h * 0.056)}px ${sans}`;
      ctx.textAlign = "left";
      ctx.fillText(fitText(ctx, name, w * 0.55), pad, row1);

      ctx.fillStyle = "rgba(255,255,255,0.65)";
      ctx.font = `500 ${Math.round(h * 0.030)}px ${sans}`;
      ctx.fillText(`${sport.toUpperCase()} · ${date}`, pad, row2);

      ctx.fillStyle = "#ffffff";
      ctx.font = `800 ${Math.round(h * 0.100)}px ${sans}`;
      const dw = ctx.measureText(distNum).width;
      ctx.fillText(distNum, pad, row3);
      ctx.fillStyle = "rgba(255,255,255,0.60)";
      ctx.font = `600 ${Math.round(h * 0.038)}px ${sans}`;
      ctx.fillText(distUnit, pad + dw + 7, row3 - Math.round(h * 0.012));

      ctx.fillStyle = "rgba(255,255,255,0.85)";
      ctx.font = `500 ${Math.round(h * 0.032)}px ${mono}`;
      ctx.textAlign = "right";
      ctx.fillText(dur,  w - pad, row2 - Math.round(h * 0.012));
      ctx.fillText(pace, w - pad, row2 + Math.round(h * 0.038));
      if (ele) {
        ctx.fillStyle = "rgba(255,255,255,0.55)";
        ctx.font = `400 ${Math.round(h * 0.026)}px ${sans}`;
        ctx.fillText(ele, w - pad, row3 - Math.round(h * 0.010));
      }
      ctx.fillStyle = "rgba(255,255,255,0.30)";
      ctx.font = `400 ${Math.round(h * 0.022)}px ${sans}`;
      ctx.fillText("athlete-log", w - pad, h - Math.round(h * 0.016));
      ctx.textAlign = "left";

    } else {
      // 9:16 portrait — no pill, just centered white text
      const splitY = Math.round(h * 0.50);
      const panelH = h - splitY - m;
      const panelW = w - m * 2;
      const cx = m + panelW / 2;
      const pad9 = 18;

      const GT = [
        Math.round(h * 0.034),
        Math.round(h * 0.020),
        Math.round(h * 0.046),
        Math.round(h * 0.028),
        Math.round(h * 0.056),
        Math.round(h * 0.028),
        Math.round(h * 0.048),
      ];
      const totalGT = GT.reduce((s, v) => s + v, 0);
      let y = splitY + Math.round((panelH - totalGT) / 2);

      ctx.textAlign = "center";

      ctx.fillStyle = "rgba(255,255,255,0.65)";
      ctx.font = `500 ${Math.round(w * 0.028)}px ${sans}`;
      ctx.fillText(`${sport.toUpperCase()} · ${date}`, cx, y);
      y += GT[0];

      ctx.fillStyle = "#ffffff";
      ctx.font = `700 ${Math.round(w * 0.050)}px ${sans}`;
      ctx.fillText(fitText(ctx, name, panelW - pad9 * 2), cx, y);
      y += GT[1];

      ctx.save();
      ctx.shadowBlur = 0;
      ctx.strokeStyle = "rgba(255,255,255,0.20)";
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(m + pad9 * 2, y); ctx.lineTo(m + panelW - pad9 * 2, y);
      ctx.stroke();
      ctx.restore();
      y += GT[2];

      ctx.fillStyle = "#ffffff";
      ctx.font = `800 ${Math.round(w * 0.125)}px ${sans}`;
      ctx.fillText(distNum, cx, y);
      y += GT[3];

      ctx.fillStyle = "rgba(255,255,255,0.60)";
      ctx.font = `600 ${Math.round(w * 0.040)}px ${sans}`;
      ctx.fillText(distUnit, cx, y);
      y += GT[4];

      ctx.fillStyle = "rgba(255,255,255,0.75)";
      ctx.font = `400 ${Math.round(w * 0.034)}px ${mono}`;
      ctx.fillText(dur, cx, y);  y += GT[5];
      ctx.fillText(pace, cx, y);
      if (ele) {
        y += Math.round(h * 0.022);
        ctx.fillStyle = "rgba(255,255,255,0.55)";
        ctx.font = `400 ${Math.round(w * 0.026)}px ${sans}`;
        ctx.fillText(ele, cx, y);
        y += Math.round(h * 0.024);
      } else {
        y += GT[6];
      }

      ctx.fillStyle = "rgba(255,255,255,0.30)";
      ctx.font = `400 ${Math.round(w * 0.020)}px ${sans}`;
      ctx.fillText("athlete-log", cx, y);
      ctx.textAlign = "left";
    }

    ctx.restore();
    return;
  }

  const pad = Math.round(w * 0.04);

  if (format === "16:9") {
    // ── Bottom gradient ──────────────────────────────────────────────────────
    const fadeH = Math.round(h * 0.55);
    const g = ctx.createLinearGradient(0, h - fadeH, 0, h);
    g.addColorStop(0,    "rgba(15,23,42,0)");
    g.addColorStop(0.28, "rgba(15,23,42,0.70)");
    g.addColorStop(0.6,  "rgba(15,23,42,0.88)");
    g.addColorStop(1,    "rgba(15,23,42,0.96)");
    ctx.fillStyle = g;
    ctx.fillRect(0, h - fadeH, w, fadeH);

    const row1 = h - Math.round(h * 0.210); // activity name
    const row2 = h - Math.round(h * 0.155); // sport · date
    const row3 = h - Math.round(h * 0.052); // distance / bottom stats

    // Activity name
    ctx.fillStyle = "#ffffff";
    ctx.font = `700 ${Math.round(h * 0.056)}px ${sans}`;
    ctx.textAlign = "left";
    ctx.fillText(fitText(ctx, name, w * 0.55), pad, row1);

    // Sport · date
    ctx.fillStyle = "#94a3b8";
    ctx.font = `500 ${Math.round(h * 0.030)}px ${sans}`;
    ctx.fillText(`${sport.toUpperCase()} · ${date}`, pad, row2);

    // Distance (left, large)
    ctx.fillStyle = "#ffffff";
    ctx.font = `800 ${Math.round(h * 0.100)}px ${sans}`;
    const dw = ctx.measureText(distNum).width;
    ctx.fillText(distNum, pad, row3);
    ctx.fillStyle = "#64748b";
    ctx.font = `600 ${Math.round(h * 0.038)}px ${sans}`;
    ctx.fillText(distUnit, pad + dw + 7, row3 - Math.round(h * 0.012));

    // Stats (right column)
    ctx.fillStyle = "#e2e8f0";
    ctx.font = `500 ${Math.round(h * 0.032)}px ${mono}`;
    ctx.textAlign = "right";
    ctx.fillText(dur,  w - pad, row2 - Math.round(h * 0.012));
    ctx.fillText(pace, w - pad, row2 + Math.round(h * 0.038));
    if (ele) {
      ctx.fillStyle = "#94a3b8";
      ctx.font = `400 ${Math.round(h * 0.026)}px ${sans}`;
      ctx.fillText(ele, w - pad, row3 - Math.round(h * 0.010));
    }

    // Branding — bottom right, subtle
    ctx.fillStyle = "#475569";
    ctx.font = `400 ${Math.round(h * 0.022)}px ${sans}`;
    ctx.fillText("athlete-log", w - pad, h - Math.round(h * 0.016));
    ctx.textAlign = "left";

  } else {
    // ── 9:16 portrait — map 60%, stats panel 40% ───────────────────────────
    const splitY = Math.round(h * 0.60);

    // Gradient transition
    const g = ctx.createLinearGradient(0, splitY - 60, 0, splitY + 40);
    g.addColorStop(0, "rgba(15,23,42,0)");
    g.addColorStop(1, "rgba(15,23,42,0.97)");
    ctx.fillStyle = g;
    ctx.fillRect(0, splitY - 60, w, 100);

    // Dark panel
    ctx.fillStyle = "rgba(15,23,42,0.97)";
    ctx.fillRect(0, splitY + 40, w, h - splitY - 40);

    // Inter-baseline gaps — summed to center content block in the panel
    const G = [
      Math.round(h * 0.034), // sport/date → name
      Math.round(h * 0.020), // name → separator
      Math.round(h * 0.068), // separator → distance
      Math.round(h * 0.028), // distance → distUnit
      Math.round(h * 0.056), // distUnit → dur
      Math.round(h * 0.028), // dur → pace
      Math.round(h * 0.048), // pace → branding
    ];
    const totalGaps = G.reduce((s, v) => s + v, 0);
    const darkStart = splitY + 40;
    const cx = w / 2;
    let y = darkStart + Math.round((h - darkStart - totalGaps) / 2);

    ctx.textAlign = "center";

    ctx.fillStyle = "#64748b";
    ctx.font = `500 ${Math.round(w * 0.030)}px ${sans}`;
    ctx.fillText(`${sport.toUpperCase()} · ${date}`, cx, y);
    y += G[0];

    ctx.fillStyle = "#ffffff";
    ctx.font = `700 ${Math.round(w * 0.056)}px ${sans}`;
    ctx.fillText(fitText(ctx, name, w * 0.82), cx, y);
    y += G[1];

    ctx.strokeStyle = "#1e293b";
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(pad * 3, y); ctx.lineTo(w - pad * 3, y);
    ctx.stroke();
    y += G[2];

    ctx.fillStyle = "#ffffff";
    ctx.font = `800 ${Math.round(w * 0.140)}px ${sans}`;
    ctx.fillText(distNum, cx, y);
    y += G[3];

    ctx.fillStyle = "#64748b";
    ctx.font = `600 ${Math.round(w * 0.046)}px ${sans}`;
    ctx.fillText(distUnit, cx, y);
    y += G[4];

    ctx.fillStyle = "#94a3b8";
    ctx.font = `400 ${Math.round(w * 0.038)}px ${mono}`;
    ctx.fillText(dur, cx, y);  y += G[5];
    ctx.fillText(pace, cx, y);
    if (ele) {
      y += Math.round(h * 0.026);
      ctx.fillStyle = "#64748b";
      ctx.font = `400 ${Math.round(w * 0.030)}px ${sans}`;
      ctx.fillText(ele, cx, y);
      y += Math.round(h * 0.026);
    } else {
      y += G[6];
    }

    ctx.fillStyle = "#334155";
    ctx.font = `400 ${Math.round(w * 0.024)}px ${sans}`;
    ctx.fillText("athlete-log", cx, y);
    ctx.textAlign = "left";
  }
}

async function captureCard({ format, stats, protoMapsKey, gpsCoords, routeColor, transparent = false, theme = "dark" }) {
  if (gpsCoords.length < 2) throw new Error("No GPS data for this activity.");
  if (!transparent && !protoMapsKey) throw new Error("A Protomaps API key is required. Add it in Settings → Protomaps API key.");

  const [w, h] = format === "9:16" ? [675, 1200] : [1200, 675];

  if (transparent) {
    const canvas = document.createElement("canvas");
    canvas.width = w; canvas.height = h;
    const ctx = canvas.getContext("2d");
    const padBottom = format === "9:16" ? Math.round(h * 0.54) : Math.round(h * 0.34);
    drawRouteOnCanvas(ctx, w, h, gpsCoords, routeColor || "#3b82f6", padBottom);
    drawCardOverlay(ctx, w, h, format, stats, true);
    return new Promise((resolve, reject) => {
      canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("Canvas export failed."))), "image/png");
    });
  }

  // In portrait mode push route into top 60% so the dark stats panel has room.
  const padding = format === "9:16"
    ? { top: 48, bottom: Math.round(h * 0.46), left: 40, right: 40 }
    : { top: 60, bottom: Math.round(h * 0.32), left: 60, right: 60 };

  const { Map: MlMap } = await import("maplibre-gl");

  const lngs = gpsCoords.map(([ln]) => ln);
  const lats  = gpsCoords.map(([, la]) => la);
  const bounds = [[Math.min(...lngs), Math.min(...lats)], [Math.max(...lngs), Math.max(...lats)]];

  const container = document.createElement("div");
  container.style.cssText = `position:fixed;left:-9999px;top:0;width:${w}px;height:${h}px;`;
  document.body.appendChild(container);

  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => { cleanup(); reject(new Error("Map render timed out (15 s).")); }, 15000);

    const cap = new MlMap({
      container,
      style: `https://api.protomaps.com/styles/v2/${theme}.json?key=${protoMapsKey}`,
      bounds,
      fitBoundsOptions: { padding, bearing: 0, pitch: 0 },
      preserveDrawingBuffer: true,
      interactive: false,
      attributionControl: false,
    });

    function cleanup() {
      clearTimeout(timeout);
      try { cap.remove(); } catch {}
      try { document.body.removeChild(container); } catch {}
    }

    cap.on("load", () => {
      // Glow layer
      cap.addSource("r", { type: "geojson", data: { type: "Feature", geometry: { type: "LineString", coordinates: gpsCoords } } });
      cap.addLayer({ id: "r-glow", type: "line", source: "r", layout: { "line-join": "round", "line-cap": "round" }, paint: { "line-color": routeColor || "#0d9488", "line-width": 12, "line-opacity": 0.25 } });
      cap.addLayer({ id: "r-line", type: "line", source: "r", layout: { "line-join": "round", "line-cap": "round" }, paint: { "line-color": routeColor || "#0d9488", "line-width": 3.5, "line-opacity": 0.95 } });
      // Start dot
      cap.addSource("s", { type: "geojson", data: { type: "Feature", geometry: { type: "Point", coordinates: gpsCoords[0] } } });
      cap.addLayer({ id: "s-dot", type: "circle", source: "s", paint: { "circle-radius": 6, "circle-color": "#10b981", "circle-stroke-color": "#fff", "circle-stroke-width": 2 } });
      // End dot
      cap.addSource("e", { type: "geojson", data: { type: "Feature", geometry: { type: "Point", coordinates: gpsCoords[gpsCoords.length - 1] } } });
      cap.addLayer({ id: "e-dot", type: "circle", source: "e", paint: { "circle-radius": 6, "circle-color": "#ef4444", "circle-stroke-color": "#fff", "circle-stroke-width": 2 } });

      cap.once("idle", () => {
        try {
          const mapCanvas = cap.getCanvas();
          const out = document.createElement("canvas");
          out.width = w; out.height = h;
          const ctx = out.getContext("2d");
          ctx.drawImage(mapCanvas, 0, 0, w, h);
          drawCardOverlay(ctx, w, h, format, stats);
          out.toBlob((blob) => {
            cleanup();
            blob ? resolve(blob) : reject(new Error("Canvas export failed."));
          }, "image/png");
        } catch (e) { cleanup(); reject(e); }
      });
    });

    cap.on("error", (e) => { cleanup(); reject(new Error(e.error?.message || "Map failed to load.")); });
  });
}

// ─── Share card modal ────────────────────────────────────────────────────────

function ShareCardModal({ activityId, activity, mapSamples, protoMapsKey, routeColor, units, onClose }) {
  const [format,      setFormat]      = useState("16:9");
  const [theme,       setTheme]       = useState("dark");
  const [transparent, setTransparent] = useState(false);
  const [customName,  setCustomName]  = useState(
    () => activity?.name || capitalize(activity?.sport_type || "")
  );
  const [currentBlob, setCurrentBlob] = useState(null);
  const [preview,     setPreview]     = useState(null);
  const [generating,  setGenerating]  = useState(false);
  const [genError,    setGenError]    = useState(null);
  const imp = units === "imperial";

  const gpsCoords = useMemo(
    () => mapSamples.filter((s) => s.latitude != null && s.longitude != null).map((s) => [s.longitude, s.latitude]),
    [mapSamples],
  );

  const stats = useMemo(() => {
    const dist = activity?.distance_meters;
    return {
      name:     customName,
      sport:    activity?.sport_type || "",
      date:     activity?.start_at
        ? new Date(activity.start_at).toLocaleDateString("en-GB", { weekday: "short", day: "numeric", month: "short", year: "numeric" }).toUpperCase()
        : "",
      distNum:  dist == null ? "—" : imp ? (dist / 1609.34).toFixed(2) : (dist / 1000).toFixed(2),
      distUnit: imp ? "mi" : "km",
      dur:      fmtDuration(activity?.elapsed_ms),
      pace:     fmtPace(activity?.average_pace_seconds_per_km, imp),
      ele:      activity?.elevation_gain_meters != null ? fmtEle(activity.elevation_gain_meters, imp) : null,
    };
  }, [activity, imp, customName]);

  async function generate() {
    setGenerating(true);
    setGenError(null);
    try {
      const blob = await captureCard({ format, stats, protoMapsKey, gpsCoords, routeColor, transparent, theme });
      setCurrentBlob(blob);
      setPreview((prev) => { if (prev) URL.revokeObjectURL(prev); return URL.createObjectURL(blob); });
    } catch (e) {
      setGenError(e.message);
    } finally {
      setGenerating(false);
    }
  }

  // Auto-generate on mount and when format, theme, or transparent mode changes.
  useEffect(() => { generate(); }, [format, theme, transparent]); // eslint-disable-line

  function downloadPNG() {
    if (!preview) return;
    const a = document.createElement("a");
    a.href = preview;
    a.download = `activity-${activityId}-${format.replace(":", "x")}.png`;
    a.click();
  }

  async function downloadSVG() {
    if (!currentBlob) return;
    const [w, h] = format === "9:16" ? [675, 1200] : [1200, 675];
    const dataUrl = await new Promise((resolve) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result);
      reader.readAsDataURL(currentBlob);
    });
    const svgStr = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><image href="${dataUrl}" width="${w}" height="${h}"/></svg>`;
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([svgStr], { type: "image/svg+xml" }));
    a.download = `activity-${activityId}-${format.replace(":", "x")}.svg`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }

  const isPortrait = format === "9:16";

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/65 backdrop-blur-sm p-4"
      onClick={onClose}
    >
      <div
        className="bg-white dark:bg-slate-900 rounded-2xl shadow-2xl flex flex-col overflow-hidden"
        style={{ maxWidth: isPortrait ? 460 : 700, width: "100%" }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-slate-100 dark:border-slate-800">
          <p className="font-semibold text-slate-800 dark:text-slate-100">Share this activity</p>
          <button onClick={onClose} className="text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300"><X size={18} /></button>
        </div>

        {/* Title input */}
        <div className="px-5 py-3 border-b border-slate-100 dark:border-slate-800">
          <input
            type="text"
            value={customName}
            onChange={(e) => setCustomName(e.target.value)}
            placeholder="Activity title"
            className="w-full text-sm font-semibold text-slate-800 dark:text-slate-100 bg-transparent focus:outline-none placeholder:text-slate-300 dark:placeholder:text-slate-500"
          />
        </div>

        {/* Format + theme + options row */}
        <div className="flex items-center gap-3 px-5 py-3 border-b border-slate-100 dark:border-slate-800 flex-wrap">
          <div className="flex gap-2">
            {CARD_FORMATS.map((f) => (
              <button
                key={f.id}
                onClick={() => setFormat(f.id)}
                className={`px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors leading-tight ${
                  format === f.id
                    ? "bg-slate-800 text-white border-slate-800"
                    : "bg-white dark:bg-slate-900 text-slate-600 dark:text-slate-300 border-slate-200 dark:border-slate-700 hover:border-slate-400"
                }`}
              >
                <div className="font-bold">{f.label}</div>
                <div className={`text-[10px] mt-0.5 ${format === f.id ? "text-slate-300 dark:text-slate-600" : "text-slate-400 dark:text-slate-500"}`}>{f.sub}</div>
              </button>
            ))}
          </div>

          {!transparent && (
            <div className="flex rounded-lg border border-slate-200 dark:border-slate-700 overflow-hidden text-xs font-medium">
              {["dark", "light"].map((t) => (
                <button
                  key={t}
                  onClick={() => setTheme(t)}
                  className={`px-2.5 py-1.5 capitalize transition-colors ${theme === t ? "bg-slate-800 text-white" : "text-slate-500 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800/50"}`}
                >
                  {t === "dark" ? "🌙 Dark" : "☀️ Light"}
                </button>
              ))}
            </div>
          )}

          <label className="flex items-center gap-2 cursor-pointer select-none" title="Route on transparent background (no map tiles)">
            <span className={`text-xs font-medium transition-colors ${transparent ? "text-slate-700 dark:text-slate-200" : "text-slate-400 dark:text-slate-500"}`}>
              Transparent
            </span>
            <button
              role="switch"
              aria-checked={transparent}
              onClick={() => setTransparent((t) => !t)}
              className={`relative inline-flex w-9 h-5 shrink-0 rounded-full transition-colors duration-200 focus:outline-none ${transparent ? "bg-slate-800" : "bg-slate-200 dark:bg-slate-700"}`}
            >
              <span className={`absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white dark:bg-slate-900 shadow-sm transition-transform duration-200 ${transparent ? "translate-x-4" : "translate-x-0"}`} />
            </button>
          </label>

          <button
            onClick={generate}
            disabled={generating}
            className="ml-auto text-xs text-blue-600 hover:text-blue-800 disabled:opacity-40 font-medium shrink-0"
          >
            {generating ? "Generating…" : "↻ Regenerate"}
          </button>
        </div>

        {/* Preview */}
        <div
          className={`p-4 flex items-center justify-center ${!transparent ? "bg-slate-100 dark:bg-slate-800" : ""}`}
          style={transparent
            ? { backgroundColor: "#fff", backgroundImage: "linear-gradient(45deg,#e5e7eb 25%,transparent 25%),linear-gradient(-45deg,#e5e7eb 25%,transparent 25%),linear-gradient(45deg,transparent 75%,#e5e7eb 75%),linear-gradient(-45deg,transparent 75%,#e5e7eb 75%)", backgroundSize: "16px 16px", backgroundPosition: "0 0,0 8px,8px -8px,-8px 0" }
            : undefined
          }
        >
          {generating && (
            <div className={`w-full rounded-xl bg-slate-300 animate-pulse ${isPortrait ? "max-w-[200px]" : "w-full"}`}
              style={{ aspectRatio: isPortrait ? "9/16" : "16/9" }} />
          )}
          {genError && !generating && (
            <div className="w-full rounded-xl bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800/50 p-4 text-xs text-red-700 dark:text-red-400">
              {genError}
            </div>
          )}
          {preview && !generating && (
            <img
              src={preview}
              alt="Share card"
              className={`rounded-xl shadow-lg ${isPortrait ? "max-h-[60vh]" : "w-full"}`}
              style={{ aspectRatio: isPortrait ? "9/16" : "16/9" }}
            />
          )}
        </div>

        {/* Actions */}
        <div className="flex items-center gap-2 px-5 py-4">
          <button
            onClick={downloadPNG}
            disabled={!currentBlob || generating}
            className="flex items-center gap-1.5 px-4 py-2 rounded-lg bg-slate-800 text-white text-sm font-medium hover:bg-slate-700 disabled:opacity-40 transition-colors"
          >
            <Download size={14} /> PNG
          </button>
          <button
            onClick={downloadSVG}
            disabled={!currentBlob || generating}
            className="flex items-center gap-1.5 px-3 py-2 rounded-lg border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 text-sm font-medium hover:bg-slate-50 dark:hover:bg-slate-800/50 disabled:opacity-40 transition-colors"
          >
            <Download size={14} /> SVG
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Main page ───────────────────────────────────────────────────────────────

export default function ActivityDetail() {
  const { id }     = useParams();
  const navigate   = useNavigate();
  const [mapStyleId]   = useMapStyle();
  const [protoMapsKey] = useProtoMapsKey();
  const mapRef = useRef(null);

  const [activity,     setActivity]     = useState(null);
  const [chartSamples, setChartSamples] = useState([]);
  const [mapSamples,   setMapSamples]   = useState([]);
  const [adjIds,       setAdjIds]       = useState({ prev: null, next: null });
  const [loading,      setLoading]      = useState(true);
  const [error,        setError]        = useState(null);

  const [topPrimary,   setTopPrimary]   = useState("pace");
  const [routeColor]                    = useRouteColor();
  const [units]                         = useUnits();
  const imp = units === "imperial";
  const [tagDisplay]                    = useTagDisplay();
  const [hrMax,        setHrMax]        = useState(null);
  const [activityGear, setActivityGear] = useState([]);
  const [allGear,      setAllGear]      = useState([]);
  const [showShare,     setShowShare]     = useState(false);
  const [flyoverActive, setFlyoverActive] = useState(false);
  const [flySpeed,      setFlySpeed]      = useState(1);
  const [editingTitle,  setEditingTitle]  = useState(false);
  const [dangerOpen,    setDangerOpen]    = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState(false);
  const [deleting,      setDeleting]      = useState(false);
  const [titleDraft,    setTitleDraft]    = useState("");
  const titleInputRef = useRef(null);

  // Notes
  const [notesDraft,  setNotesDraft]  = useState("");
  const [notesSaved,  setNotesSaved]  = useState(false);

  // Tags
  const [tags,        setTags]        = useState([]);
  const [addingTag,   setAddingTag]   = useState(false);
  const [tagKey,      setTagKey]      = useState("");
  const [tagValue,    setTagValue]    = useState("");

  const maxHRFromSamples = useMemo(() => {
    const hrs = mapSamples.map((s) => s.heart_rate).filter((v) => v != null);
    return hrs.length ? Math.round(Math.max(...hrs)) : null;
  }, [mapSamples]);

  async function handleGearSave(gearIds) {
    await api.gear.setForActivity(id, gearIds);
    setActivityGear(await api.gear.getForActivity(id).catch(() => []));
  }

  function startEditTitle() {
    setTitleDraft(activity?.name || capitalize(activity?.sport_type || ""));
    setEditingTitle(true);
    setTimeout(() => titleInputRef.current?.select(), 0);
  }

  async function commitTitle() {
    const trimmed = titleDraft.trim();
    if (trimmed && trimmed !== activity?.name) {
      await api.activities.update(id, { name: trimmed }).catch(() => {});
      setActivity((a) => a ? { ...a, name: trimmed } : a);
    }
    setEditingTitle(false);
  }

  function cancelEditTitle() {
    setEditingTitle(false);
  }

  async function deleteActivity() {
    setDeleting(true);
    try {
      await api.activities.delete(id);
      navigate("/activities");
    } catch {
      setDeleting(false);
      setDeleteConfirm(false);
    }
  }

  async function saveNotes() {
    await api.activities.update(id, { notes: notesDraft }).catch(() => {});
    setNotesSaved(true);
    setTimeout(() => setNotesSaved(false), 2000);
  }

  async function handleAddTag(e) {
    e.preventDefault();
    const k = tagKey.trim(), v = tagValue.trim();
    if (!k) return;
    await api.activities.setTag(id, k, v).catch(() => {});
    setTags((prev) => {
      const rest = prev.filter((t) => t.key !== k);
      return [...rest, { key: k, value: v }];
    });
    setTagKey(""); setTagValue(""); setAddingTag(false);
  }

  async function handleRemoveTag(key) {
    await api.activities.deleteTag(id, key).catch(() => {});
    setTags((prev) => prev.filter((t) => t.key !== key));
  }

  const isExcluded = tags.some((t) => t.key === "exclude_from_records");

  async function handleToggleExclude() {
    if (isExcluded) {
      await api.activities.deleteTag(id, "exclude_from_records").catch(() => {});
      setTags((prev) => prev.filter((t) => t.key !== "exclude_from_records"));
    } else {
      await api.activities.setTag(id, "exclude_from_records", "true").catch(() => {});
      setTags((prev) => [...prev.filter((t) => t.key !== "exclude_from_records"), { key: "exclude_from_records", value: "true" }]);
    }
  }

  const handleHover = useCallback(
    (elapsedMs) => mapRef.current?.showMarker(elapsedMs, mapSamples),
    [mapSamples]
  );
  const handleLeave = useCallback(() => mapRef.current?.hideMarker(), []);

  useEffect(() => {
    setLoading(true);
    setError(null);
    Promise.all([
      api.activities.get(id),
      api.activities.samples(id, 500),
      api.activities.samples(id, 1000),
      api.settings.get().catch(() => ({})),
      api.gear.getForActivity(id).catch(() => []),
      api.gear.list(false).catch(() => []),
    ])
      .then(([act, chart, map, settings, gearAssigned, gearAll]) => {
        setActivity(act);
        setNotesDraft(act.notes ?? "");
        setTags(act.tags ?? []);
        setChartSamples(chart ?? []);
        setMapSamples(map ?? []);
        const parsed = parseInt(settings.hr_max, 10);
        if (!isNaN(parsed) && parsed > 0) setHrMax(parsed);
        setActivityGear(gearAssigned ?? []);
        setAllGear(gearAll ?? []);
        const dateStr = act.start_at.split("T")[0];
        Promise.all([
          api.activities.list({ to: dateStr, limit: 2 }),
          api.activities.list({ from: dateStr, limit: 2 }),
        ]).then(([older, newer]) => {
          setAdjIds({
            prev: (older.activities ?? []).find((a) => a.id !== id)?.id ?? null,
            next: (newer.activities ?? []).find((a) => a.id !== id)?.id ?? null,
          });
        });
      })
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, [id]);

  const chartData = useMemo(() => {
    const raw = chartSamples.map((s) => ({
      elapsedMs: s.elapsed_ms ?? null,
      ele:  s.elevation_meters != null ? +s.elevation_meters.toFixed(1) : null,
      hr:   s.heart_rate != null ? Math.round(s.heart_rate) : null,
      pace: s.speed_meters_per_second > 0 ? 1000 / s.speed_meters_per_second / 60 : null,
      _distM: s.distance_meters ?? null,
    }));

    // If no sample carries speed (e.g. GPX uploads), derive pace from cumulative
    // distance and elapsed time using a ±3-point centered-difference window.
    const hasPace = raw.some((p) => p.pace != null);
    if (!hasPace && raw.length >= 3) {
      const W = 3;
      for (let i = 0; i < raw.length; i++) {
        const lo = Math.max(0, i - W);
        const hi = Math.min(raw.length - 1, i + W);
        if (lo >= hi) continue;
        const dDist = (raw[hi]._distM ?? 0) - (raw[lo]._distM ?? 0);
        const dMs   = (raw[hi].elapsedMs ?? 0) - (raw[lo].elapsedMs ?? 0);
        if (dDist > 0 && dMs > 0) {
          const mps = dDist / (dMs / 1000);
          raw[i].pace = mps > 0 ? 1000 / mps / 60 : null;
        }
      }
    }

    return raw.map(({ _distM, ...rest }) => rest);
  }, [chartSamples]);

  const hasGPS = mapSamples.some((s) => s.latitude != null);

  if (error) {
    return (
      <div className="rounded-xl border border-red-200 dark:border-red-800/50 bg-red-50 dark:bg-red-900/20 p-4 text-sm text-red-700 dark:text-red-400">
        {error}. <Link to="/activities" className="underline">Back to activities</Link>
      </div>
    );
  }

  return (
    <div className="space-y-5">
      {/* Header + prev/next */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <Link to="/activities" className="text-xs text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300 mb-1 inline-block">
            ← Activities
          </Link>
          {loading ? <Skeleton className="h-8 w-56 mt-1" /> : editingTitle ? (
            <div className="flex items-center gap-2 mt-1">
              <span className="text-2xl">{SPORT_EMOJI[activity?.sport_type] ?? "⚡"}</span>
              <input
                ref={titleInputRef}
                value={titleDraft}
                onChange={(e) => setTitleDraft(e.target.value)}
                onBlur={commitTitle}
                onKeyDown={(e) => { if (e.key === "Enter") commitTitle(); else if (e.key === "Escape") cancelEditTitle(); }}
                className="text-2xl font-bold text-slate-800 dark:text-slate-100 bg-transparent border-b-2 border-blue-500 focus:outline-none min-w-0 flex-1"
                autoFocus
              />
            </div>
          ) : (
            <h1
              className="text-2xl font-bold text-slate-800 dark:text-slate-100 cursor-pointer hover:text-slate-600 dark:hover:text-slate-300 transition-colors"
              onDoubleClick={startEditTitle}
              title="Double-click to edit title"
            >
              {SPORT_EMOJI[activity?.sport_type] ?? "⚡"}{" "}
              {activity?.name || capitalize(activity?.sport_type)}
            </h1>
          )}
          {!loading && activity && (
            <div className="flex items-center gap-3 mt-0.5 flex-wrap">
              <p className="text-sm text-slate-400 dark:text-slate-500">{fmtDate(activity.start_at)}</p>
              {hasGPS && <WeatherChip activityId={id} imperial={imp} />}
            </div>
          )}
        </div>
        <div className="flex gap-1 shrink-0 mt-6 items-center">
          <button
            onClick={() => adjIds.prev && navigate(`/activities/${adjIds.prev}`)}
            disabled={!adjIds.prev}
            title="Older"
            className="p-2 rounded-lg border border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30 disabled:cursor-not-allowed"
          >
            <ChevronLeft size={16} />
          </button>
          <button
            onClick={() => adjIds.next && navigate(`/activities/${adjIds.next}`)}
            disabled={!adjIds.next}
            title="Newer"
            className="p-2 rounded-lg border border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-30 disabled:cursor-not-allowed"
          >
            <ChevronRight size={16} />
          </button>

        </div>
      </div>

      {/* Stats header */}
      {loading ? (
        <div className="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-6">
          <div className="flex flex-wrap justify-center gap-x-8 gap-y-4">{[...Array(5)].map((_,i)=><Skeleton key={i} className="h-14 w-20"/>)}</div>
        </div>
      ) : activity && (
        <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-6">
          <div className="flex flex-wrap justify-center gap-x-8 gap-y-5">
            <Stat label="Distance"   value={fmtDist(activity.distance_meters, imp)} />
            <Stat label="Duration"   value={fmtDuration(activity.elapsed_ms)} />
            <Stat label="Avg pace"   value={fmtPace(activity.average_pace_seconds_per_km, imp)} />
            <Stat label="Avg HR"     value={fmtHR(activity.average_heart_rate)} />
            <Stat label="Max HR"     value={fmtHR(activity.max_heart_rate ?? maxHRFromSamples)} />
            {activity.elevation_gain_meters != null && (
              <Stat label="Elevation ↑" value={fmtEle(activity.elevation_gain_meters, imp)} />
            )}
            {activity.calories != null && activity.calories > 0 && (
              <Stat label="Calories" value={`${activity.calories.toLocaleString()} kcal`} />
            )}
          </div>
        </div>
      )}

      {/* Map + map action buttons */}
      {loading ? <Skeleton className="h-80 w-full" /> : hasGPS ? (
        <div className="space-y-2">
          <RouteMap
            ref={mapRef}
            samples={mapSamples}
            mapStyleId={mapStyleId}
            protoMapsKey={protoMapsKey}
            routeColor={routeColor}
          />
          {/* Map action row */}
          <div className="flex items-center gap-2 flex-wrap">
            {protoMapsKey && (
              <button
                onClick={() => {
                  if (flyoverActive) {
                    mapRef.current?.stopFlyover();
                    setFlyoverActive(false);
                  } else {
                    setFlySpeed(1);
                    mapRef.current?.setFlyoverSpeed(1);
                    mapRef.current?.startFlyover(() => setFlyoverActive(false));
                    setFlyoverActive(true);
                  }
                }}
                className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors ${
                  flyoverActive
                    ? "bg-slate-800 text-white border-slate-800 hover:bg-slate-700"
                    : "bg-white dark:bg-slate-900 text-slate-600 dark:text-slate-300 border-slate-200 dark:border-slate-700 hover:bg-slate-50 dark:hover:bg-slate-800/50"
                }`}
              >
                {flyoverActive
                  ? <><Square size={12} fill="currentColor" /> Stop</>
                  : <><Play size={12} fill="currentColor" /> 3D Flyover</>}
              </button>
            )}

            {/* Speed buttons — only when flyover is active */}
            {flyoverActive && (
              <div className="flex items-center gap-1">
                {[0.5, 1, 2, 4].map((s) => (
                  <button
                    key={s}
                    onClick={() => {
                      setFlySpeed(s);
                      mapRef.current?.setFlyoverSpeed(s);
                    }}
                    className={`px-2.5 py-1 rounded text-xs font-medium transition-colors border ${
                      flySpeed === s
                        ? "bg-blue-600 text-white border-blue-600"
                        : "bg-white dark:bg-slate-900 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700 hover:bg-slate-50 dark:hover:bg-slate-800/50"
                    }`}
                  >
                    {s === 0.5 ? "½×" : `${s}×`}
                  </button>
                ))}
              </div>
            )}

            <button
              onClick={() => mapRef.current?.resetView()}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 text-slate-500 dark:text-slate-400 hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors"
              title="Reset map to fit route"
            >
              ⊙ Fit
            </button>

            <button
              onClick={() => setShowShare(true)}
              className="ml-auto flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors"
            >
              <Share2 size={12} /> Share
            </button>
          </div>
        </div>
      ) : (
        <div className="flex items-center justify-center h-32 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-950 text-sm text-slate-400 dark:text-slate-500">
          No GPS data for this activity
        </div>
      )}

      {/* Three charts — hover on any moves the map marker */}
      {!loading && chartData.length > 0 && (
        <div className="space-y-3">
          {/* Top chart: has metric picker */}
          <ActivityChart
            data={chartData}
            primary={topPrimary}
            onPrimaryChange={setTopPrimary}
            onHover={handleHover}
            onLeave={handleLeave}

          />
          {/* Middle: HR fixed */}
          <ActivityChart
            data={chartData}
            primary="hr"
            onHover={handleHover}
            onLeave={handleLeave}

          />
          {/* Bottom: Elevation fixed */}
          <ActivityChart
            data={chartData}
            primary="ele"
            onHover={handleHover}
            onLeave={handleLeave}

          />
        </div>
      )}

      {/* HR Zones */}
      {!loading && chartData.some((s) => s.hr != null) && (
        <HRZones
          samples={chartData}
          elapsedMs={activity?.elapsed_ms}
          maxHR={hrMax}
        />
      )}

      {/* Laps */}
      {!loading && <LapsTable laps={activity?.laps} />}

      {/* Auto-splits */}
      {!loading && <SplitsTable samples={mapSamples} imperial={imp} />}

      {/* Similar routes */}
      {!loading && hasGPS && <SimilarRoutes activityId={id} />}

      {/* Notes */}
      {!loading && (
        <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 p-4">
          <div className="flex items-center justify-between mb-2">
            <p className="text-xs font-semibold text-slate-400 dark:text-slate-500 uppercase tracking-wide">Notes</p>
            {notesSaved && <span className="text-xs text-emerald-500">Saved</span>}
          </div>
          <textarea
            value={notesDraft}
            onChange={(e) => setNotesDraft(e.target.value)}
            onBlur={saveNotes}
            placeholder="Add a note about this activity…"
            rows={3}
            className="w-full text-sm text-slate-700 dark:text-slate-200 placeholder:text-slate-300 dark:text-slate-600 resize-none focus:outline-none"
          />
        </div>
      )}

      {/* Tags + Gear */}
      {!loading && activity && (
        <div className="card-hover bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 px-5 py-4 space-y-3">
          {/* Exclude from records toggle */}
          <div className="flex items-center justify-between">
            <button
              onClick={handleToggleExclude}
              className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors ${
                isExcluded
                  ? "bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-400 border border-amber-300 dark:border-amber-700"
                  : "bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:bg-amber-50 dark:hover:bg-amber-900/20 hover:text-amber-600 dark:hover:text-amber-400 border border-transparent"
              }`}
            >
              <ShieldOff size={12} />
              {isExcluded ? "Excluded from records" : "Exclude from records"}
            </button>
            {isExcluded && (
              <span className="text-[10px] text-slate-400 dark:text-slate-500">GPS error or invalid effort</span>
            )}
          </div>
          <div className="flex flex-wrap gap-1.5 items-center">
            {tags
              .filter((t) => t.key !== "exclude_from_records")
              .filter((t) => tagDisplay === "user-only" ? !isAutoImportedTag(t.key) : true)
              .map((t) => (
                <span key={t.key}
                  className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full bg-slate-100 dark:bg-slate-800 text-xs text-slate-600 dark:text-slate-300">
                  <span className="font-medium text-slate-500 dark:text-slate-400">{t.key}</span>
                  {t.value && <span>: {t.value}</span>}
                  <button onClick={() => handleRemoveTag(t.key)}
                    className="ml-0.5 text-slate-400 dark:text-slate-500 hover:text-red-400 leading-none">×</button>
                </span>
              ))}
            {addingTag ? (
              <form onSubmit={handleAddTag} className="flex items-center gap-1.5">
                <input autoFocus value={tagKey} onChange={(e) => setTagKey(e.target.value)}
                  placeholder="key" maxLength={40}
                  className="w-20 text-xs border border-slate-200 dark:border-slate-700 rounded-lg px-2 py-1 focus:outline-none focus:ring-1 focus:ring-blue-400"/>
                <input value={tagValue} onChange={(e) => setTagValue(e.target.value)}
                  placeholder="value" maxLength={100}
                  className="w-24 text-xs border border-slate-200 dark:border-slate-700 rounded-lg px-2 py-1 focus:outline-none focus:ring-1 focus:ring-blue-400"/>
                <button type="submit"
                  className="text-xs px-2.5 py-1 btn-accent rounded-lg">Add</button>
                <button type="button" onClick={() => { setAddingTag(false); setTagKey(""); setTagValue(""); }}
                  className="text-xs text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300">Cancel</button>
              </form>
            ) : (
              <button onClick={() => setAddingTag(true)}
                className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full border border-dashed border-slate-300 dark:border-slate-600 text-xs text-slate-400 dark:text-slate-500 hover:border-blue-400 hover:text-blue-500 transition-colors">
                + tag
              </button>
            )}
          </div>
          {allGear.length > 0 && (
            <div className="pt-3 border-t border-slate-100 dark:border-slate-800">
              <GearSection assigned={activityGear} allGear={allGear} onSave={handleGearSave} />
            </div>
          )}
        </div>
      )}

      {/* Danger zone */}
      {!loading && activity && (
        <div className="border border-red-200 rounded-xl overflow-hidden">
          <button
            onClick={() => { setDangerOpen((o) => !o); setDeleteConfirm(false); }}
            className="w-full flex items-center justify-between px-5 py-3 text-left hover:bg-red-50 transition-colors"
          >
            <span className="text-xs font-semibold text-red-500 uppercase tracking-wide">Danger zone</span>
            <ChevronDown size={14} className={`text-red-400 transition-transform duration-200 ${dangerOpen ? "rotate-180" : ""}`} />
          </button>
          {dangerOpen && (
            <div className="flex items-center justify-between gap-4 px-5 pb-4 pt-1 border-t border-red-100">
              <p className="text-sm text-slate-500 dark:text-slate-400">
                Permanently removes this activity from your log. This cannot be undone.
              </p>
              {deleteConfirm ? (
                <div className="flex gap-2 shrink-0">
                  <button
                    onClick={deleteActivity}
                    disabled={deleting}
                    className="px-3 py-1.5 text-sm font-medium rounded-lg bg-red-600 text-white hover:bg-red-700 disabled:opacity-50 transition-colors"
                  >
                    {deleting ? "Deleting…" : "Yes, delete"}
                  </button>
                  <button
                    onClick={() => setDeleteConfirm(false)}
                    disabled={deleting}
                    className="px-3 py-1.5 text-sm font-medium rounded-lg border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800/50 transition-colors"
                  >
                    Cancel
                  </button>
                </div>
              ) : (
                <button
                  onClick={() => setDeleteConfirm(true)}
                  className="shrink-0 flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium rounded-lg border border-red-300 text-red-600 hover:bg-red-50 transition-colors"
                >
                  <Trash2 size={14} />
                  Delete activity
                </button>
              )}
            </div>
          )}
        </div>
      )}

      {/* Share card modal */}
      {showShare && (
        <ShareCardModal
          activityId={id}
          activity={activity}
          mapSamples={mapSamples}
          protoMapsKey={protoMapsKey}
          routeColor={routeColor}
          units={units}
          onClose={() => setShowShare(false)}
        />
      )}
    </div>
  );
}
