// Unit-aware formatters. Pass `imperial=true` for US/UK display.

export function fmtDist(meters, imperial = false) {
  if (meters == null) return "—";
  if (imperial) {
    const miles = meters / 1609.34;
    return miles >= 0.1 ? `${miles.toFixed(2)} mi` : `${Math.round(meters * 3.28084)} ft`;
  }
  return meters >= 1000 ? `${(meters / 1000).toFixed(2)} km` : `${Math.round(meters)} m`;
}

export function fmtDistShort(meters, imperial = false) {
  if (meters == null) return "—";
  if (imperial) return `${(meters / 1609.34).toFixed(1)} mi`;
  return `${(meters / 1000).toFixed(1)} km`;
}

// secsPerKm → "M:SS /km" or "M:SS /mi"
export function fmtPace(secsPerKm, imperial = false) {
  if (!secsPerKm) return "—";
  const s = imperial ? secsPerKm * 1.60934 : secsPerKm;
  const min = Math.floor(s / 60), sec = Math.round(s % 60);
  return `${min}:${String(sec).padStart(2, "0")} /${imperial ? "mi" : "km"}`;
}

export function fmtEle(meters, imperial = false) {
  if (meters == null) return "—";
  if (imperial) return `${Math.round(meters * 3.28084)} ft`;
  return `${Math.round(meters)} m`;
}

export function fmtSpeed(mps, imperial = false) {
  if (!mps) return "—";
  if (imperial) return `${(mps * 2.23694).toFixed(1)} mph`;
  return `${(mps * 3.6).toFixed(1)} km/h`;
}

export function distUnit(imperial = false) { return imperial ? "mi" : "km"; }
export function eleUnit(imperial = false)  { return imperial ? "ft" : "m"; }
