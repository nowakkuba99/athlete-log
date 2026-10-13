import { useState, useEffect, useRef } from "react";
import "maplibre-gl/dist/maplibre-gl.css";
import { useProtoMapsKey, useMapStyle } from "../settings";
import { MAP_STYLES } from "./ActivityDetail";
import { coordToCountry } from "../utils/countryBounds";

const SPORT_COLOR = {
  run:   "#f97316",
  ride:  "#3b82f6",
  swim:  "#06b6d4",
  walk:  "#10b981",
  hike:  "#a16207",
};

const BASE_URL = "/api/v1";

function buildUrl(sportType, from, to) {
  const p = new URLSearchParams();
  if (sportType) p.set("sport_type", sportType);
  if (from)      p.set("from", from);
  if (to)        p.set("to", to);
  const qs = p.toString();
  return `${BASE_URL}/heatmap${qs ? "?" + qs : ""}`;
}

export default function Heatmap() {
  const divRef = useRef(null);
  const mapRef = useRef(null);

  const [protoMapsKey] = useProtoMapsKey();
  const [mapStyleId]   = useMapStyle();

  const [sportFilter, setSportFilter] = useState("");
  const [from, setFrom]               = useState("");
  const [to, setTo]                   = useState("");
  const [loading, setLoading]         = useState(true);
  const [routeCount, setRouteCount]   = useState(0);
  const [error, setError]             = useState(null);
  const [colorBySport, setColorBySport] = useState(false);
  const [countries, setCountries]     = useState([]); // [{ name, flag, bounds }]

  // Fetch GeoJSON and (re)paint the map layer.
  useEffect(() => {
    if (!divRef.current) return;

    let cancelled = false;
    setLoading(true);
    setError(null);

    fetch(buildUrl(sportFilter, from, to))
      .then(r => r.ok ? r.json() : r.json().then(b => { throw new Error(b.error || r.statusText); }))
      .then(async (geojson) => {
        if (cancelled) return;
        const features = geojson.features ?? [];
        setRouteCount(features.length);

        // Build country list from all route centroids
        const countryMap = new Map();
        for (const f of features) {
          const coords = f.geometry.coordinates;
          if (!coords.length) continue;
          let fMinLng = Infinity, fMaxLng = -Infinity, fMinLat = Infinity, fMaxLat = -Infinity;
          for (const [ln, la] of coords) {
            if (ln < fMinLng) fMinLng = ln; if (ln > fMaxLng) fMaxLng = ln;
            if (la < fMinLat) fMinLat = la; if (la > fMaxLat) fMaxLat = la;
          }
          const cLng = (fMinLng + fMaxLng) / 2, cLat = (fMinLat + fMaxLat) / 2;
          const country = coordToCountry(cLng, cLat);
          if (country) {
            if (!countryMap.has(country.name)) {
              countryMap.set(country.name, { ...country, bounds: [fMinLng, fMinLat, fMaxLng, fMaxLat] });
            } else {
              const c = countryMap.get(country.name);
              c.bounds = [
                Math.min(c.bounds[0], fMinLng), Math.min(c.bounds[1], fMinLat),
                Math.max(c.bounds[2], fMaxLng), Math.max(c.bounds[3], fMaxLat),
              ];
            }
          }
        }
        setCountries(countryMap.size > 1 ? [...countryMap.values()] : []);

        // Lazy-load MapLibre
        const { default: maplibregl } = await import("../maplibre");

        if (cancelled) return;

        const style = MAP_STYLES.find(s => s.id === mapStyleId) ?? MAP_STYLES[0];
        let mapStyle;

        if (style.type === "protomaps" && protoMapsKey) {
          mapStyle = `https://api.protomaps.com/styles/v2/${style.theme}.json?key=${protoMapsKey}`;
        } else {
          const raster = style.type === "raster" ? style : MAP_STYLES[0];
          const tileUrl = raster.url.replace("{r}", "");
          const tiles = tileUrl.includes("{s}")
            ? ["a","b","c"].map(sub => tileUrl.replace("{s}", sub))
            : [tileUrl];
          mapStyle = {
            version: 8,
            sources: { raster: { type: "raster", tiles, tileSize: 256, attribution: raster.attr } },
            layers:  [{ id: "raster", type: "raster", source: "raster" }],
          };
        }

        // Remove previous map instance
        if (mapRef.current) { mapRef.current.remove(); mapRef.current = null; }

        // Fit to most recent activity's bounding box; fall back to all-routes centroid
        let initOptions = { center: [0, 20], zoom: 2 };
        if (features.length > 0) {
          // Fit to the 3 most recent activities for geographic context
          const sorted = [...features].sort((a, b) =>
            (b.properties.date ?? "").localeCompare(a.properties.date ?? "")
          );
          const recent = sorted.slice(0, 3);
          let minLng = Infinity, maxLng = -Infinity, minLat = Infinity, maxLat = -Infinity;
          for (const feat of recent) {
            for (const [ln, la] of feat.geometry.coordinates) {
              if (ln < minLng) minLng = ln; if (ln > maxLng) maxLng = ln;
              if (la < minLat) minLat = la; if (la > maxLat) maxLat = la;
            }
          }
          if (minLng !== Infinity) {
            initOptions = {
              bounds: [[minLng, minLat], [maxLng, maxLat]],
              fitBoundsOptions: { padding: 100, maxZoom: 13 },
            };
          }
        }

        const map = new maplibregl.Map({ container: divRef.current, style: mapStyle, ...initOptions, attributionControl: true });
        mapRef.current = map;
        map.addControl(new maplibregl.NavigationControl({ showCompass: false }), "top-left");

        map.on("load", () => {
          if (cancelled || !mapRef.current) return;

          map.addSource("routes", { type: "geojson", data: geojson });

          if (colorBySport) {
            // One layer per sport type with sport-specific colour
            const sports = [...new Set(geojson.features.map(f => f.properties.sport_type))];
            for (const sport of sports) {
              map.addLayer({
                id: `route-${sport}`,
                type: "line",
                source: "routes",
                filter: ["==", ["get", "sport_type"], sport],
                paint: {
                  "line-color": SPORT_COLOR[sport] ?? "#94a3b8",
                  "line-width": 1.5,
                  "line-opacity": 0.5,
                },
              });
            }
          } else {
            // Single heat layer — overlapping routes build density naturally
            map.addLayer({
              id: "routes-heat",
              type: "line",
              source: "routes",
              paint: {
                "line-color": "#f97316",
                "line-width": 1.5,
                "line-opacity": 0.35,
              },
            });
          }
        });

        setLoading(false);
      })
      .catch(err => {
        if (!cancelled) { setError(err.message); setLoading(false); }
      });

    return () => { cancelled = true; };
  }, [sportFilter, from, to, mapStyleId, protoMapsKey, colorBySport]);

  // Cleanup on unmount
  useEffect(() => () => { if (mapRef.current) { mapRef.current.remove(); mapRef.current = null; } }, []);

  return (
    <div className="space-y-5">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-slate-800 dark:text-slate-100">Heatmap</h1>
        <p className="text-sm text-slate-400 dark:text-slate-500 mt-0.5">All your routes in one place</p>
      </div>

      {/* Controls */}
      <div className="flex flex-wrap items-center gap-3">
        {/* Sport filter */}
        <div className="flex gap-1.5">
          {["", "run", "ride", "walk", "hike", "swim"].map(s => (
            <button
              key={s}
              onClick={() => setSportFilter(s)}
              className={`px-2.5 py-1 rounded-full text-xs font-medium capitalize transition-colors ${
                sportFilter === s
                  ? s ? "text-white" : "bg-slate-800 text-white"
                  : "bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:bg-slate-200 dark:hover:bg-slate-700"
              }`}
              style={sportFilter === s && s ? { background: SPORT_COLOR[s] } : {}}
            >
              {s || "All"}
            </button>
          ))}
        </div>

        <div className="h-4 w-px bg-slate-200 dark:bg-slate-700" />

        {/* Date range */}
        <div className="flex items-center gap-1.5">
          <input
            type="date"
            value={from}
            onChange={e => setFrom(e.target.value)}
            className="text-xs border border-slate-200 dark:border-slate-700 rounded-lg px-2 py-1 text-slate-600 dark:text-slate-300 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <span className="text-xs text-slate-400 dark:text-slate-500">to</span>
          <input
            type="date"
            value={to}
            onChange={e => setTo(e.target.value)}
            className="text-xs border border-slate-200 dark:border-slate-700 rounded-lg px-2 py-1 text-slate-600 dark:text-slate-300 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          {(from || to) && (
            <button
              onClick={() => { setFrom(""); setTo(""); }}
              className="text-xs text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300"
            >
              Clear
            </button>
          )}
        </div>

        <div className="h-4 w-px bg-slate-200 dark:bg-slate-700" />

        {/* Colour mode */}
        <label className="flex items-center gap-1.5 cursor-pointer select-none">
          <input
            type="checkbox"
            checked={colorBySport}
            onChange={e => setColorBySport(e.target.checked)}
            className="rounded border-slate-300 dark:border-slate-600 text-blue-600 focus:ring-blue-500"
          />
          <span className="text-xs text-slate-500 dark:text-slate-400">Colour by sport</span>
        </label>

        {/* Stats */}
        {!loading && (
          <span className="ml-auto text-xs text-slate-400 dark:text-slate-500">
            {routeCount} {routeCount === 1 ? "route" : "routes"}
          </span>
        )}
      </div>

      {/* Country navigation pills */}
      {countries.length > 1 && (
        <div className="flex flex-wrap gap-1.5">
          {countries.map(c => (
            <button
              key={c.name}
              onClick={() => {
                if (!mapRef.current) return;
                mapRef.current.fitBounds(
                  [[c.bounds[0], c.bounds[1]], [c.bounds[2], c.bounds[3]]],
                  { padding: 60, maxZoom: 13 }
                );
              }}
              className="flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
            >
              <span>{c.flag}</span>
              <span>{c.name}</span>
            </button>
          ))}
        </div>
      )}

      {error && (
        <div className="rounded-xl border border-red-200 dark:border-red-800/50 bg-red-50 dark:bg-red-900/20 p-4 text-sm text-red-700 dark:text-red-400">{error}</div>
      )}

      {/* Map */}
      <div className="relative rounded-xl overflow-hidden border border-slate-200 dark:border-slate-700" style={{ height: "70vh" }}>
        {loading && (
          <div className="absolute inset-0 z-10 flex items-center justify-center bg-slate-100 dark:bg-slate-800">
            <div className="text-sm text-slate-500 dark:text-slate-400 animate-pulse">Loading routes…</div>
          </div>
        )}
        <div ref={divRef} className="w-full h-full" />
      </div>
    </div>
  );
}
