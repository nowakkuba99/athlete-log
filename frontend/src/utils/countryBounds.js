// Bounding boxes for ~60 common countries used for heatmap country navigation.
// Point-in-box check: lng in [minLng, maxLng] && lat in [minLat, maxLat].
// Boxes are intentionally generous — overlaps are resolved by picking first match.
export const COUNTRY_BOUNDS = [
  // European ordering rule: put more-specific/smaller countries BEFORE their
  // larger neighbours that overlap their bounding box.  find() returns first
  // match, so whichever appears earlier wins for a given point.
  //
  // Key fixes vs. naïve alphabetical order:
  //   • Ireland before UK  (Dublin ∈ UK bbox without this)
  //   • Portugal before Spain (Lisbon ∈ Spain bbox without this)
  //   • Spain before France  (Barcelona lat 41.4 ∈ France bbox without this)
  //   • Austria/Switzerland/CZ before Germany (Innsbruck, Zürich etc.)
  //   • France minLat 42.5 = actual southernmost mainland point (Cap Bear)
  { name: "Netherlands",    flag: "🇳🇱", minLng: 3.3,   maxLng: 7.2,   minLat: 50.8,  maxLat: 53.6 },
  { name: "Belgium",        flag: "🇧🇪", minLng: 2.5,   maxLng: 6.4,   minLat: 49.5,  maxLat: 51.5 },
  { name: "Ireland",        flag: "🇮🇪", minLng: -10.5, maxLng: -6.0,  minLat: 51.4,  maxLat: 55.4 },
  { name: "Denmark",        flag: "🇩🇰", minLng: 8.1,   maxLng: 15.2,  minLat: 54.6,  maxLat: 57.8 },
  { name: "Switzerland",    flag: "🇨🇭", minLng: 5.9,   maxLng: 10.5,  minLat: 45.8,  maxLat: 47.8 },
  { name: "Austria",        flag: "🇦🇹", minLng: 9.5,   maxLng: 17.2,  minLat: 46.4,  maxLat: 49.0 },
  { name: "Czech Republic", flag: "🇨🇿", minLng: 12.1,  maxLng: 18.9,  minLat: 48.6,  maxLat: 51.1 },
  { name: "Slovakia",       flag: "🇸🇰", minLng: 16.8,  maxLng: 22.6,  minLat: 47.7,  maxLat: 49.6 },
  { name: "Hungary",        flag: "🇭🇺", minLng: 16.1,  maxLng: 22.9,  minLat: 45.7,  maxLat: 48.6 },
  { name: "Croatia",        flag: "🇭🇷", minLng: 13.5,  maxLng: 19.4,  minLat: 42.4,  maxLat: 46.6 },
  { name: "Serbia",         flag: "🇷🇸", minLng: 18.8,  maxLng: 23.0,  minLat: 42.2,  maxLat: 46.2 },
  { name: "Bulgaria",       flag: "🇧🇬", minLng: 22.4,  maxLng: 28.6,  minLat: 41.2,  maxLat: 44.2 },
  { name: "Greece",         flag: "🇬🇷", minLng: 19.4,  maxLng: 28.3,  minLat: 34.8,  maxLat: 41.8 },
  { name: "Romania",        flag: "🇷🇴", minLng: 20.3,  maxLng: 29.7,  minLat: 43.6,  maxLat: 48.3 },
  { name: "Ukraine",        flag: "🇺🇦", minLng: 22.1,  maxLng: 40.2,  minLat: 44.4,  maxLat: 52.4 },
  { name: "Poland",         flag: "🇵🇱", minLng: 14.1,  maxLng: 24.2,  minLat: 49.0,  maxLat: 54.9 },
  { name: "Portugal",       flag: "🇵🇹", minLng: -9.5,  maxLng: -6.2,  minLat: 36.9,  maxLat: 42.2 },
  { name: "Spain",          flag: "🇪🇸", minLng: -9.3,  maxLng: 4.3,   minLat: 35.9,  maxLat: 43.8 },
  { name: "Italy",          flag: "🇮🇹", minLng: 6.6,   maxLng: 18.5,  minLat: 36.6,  maxLat: 47.1 },
  { name: "Germany",        flag: "🇩🇪", minLng: 5.9,   maxLng: 15.0,  minLat: 47.5,  maxLat: 55.1 },
  { name: "France",         flag: "🇫🇷", minLng: -5.1,  maxLng: 9.6,   minLat: 42.5,  maxLat: 51.1 },
  { name: "UK",             flag: "🇬🇧", minLng: -8.7,  maxLng: 1.8,   minLat: 49.9,  maxLat: 60.9 },
  { name: "Sweden",         flag: "🇸🇪", minLng: 11.1,  maxLng: 24.2,  minLat: 55.3,  maxLat: 69.1 },
  { name: "Norway",         flag: "🇳🇴", minLng: 4.5,   maxLng: 31.1,  minLat: 57.9,  maxLat: 71.2 },
  { name: "Finland",        flag: "🇫🇮", minLng: 20.0,  maxLng: 31.6,  minLat: 59.7,  maxLat: 70.1 },
  { name: "Turkey",         flag: "🇹🇷", minLng: 26.0,  maxLng: 44.8,  minLat: 36.0,  maxLat: 42.1 },
  { name: "USA",            flag: "🇺🇸", minLng: -125.0,maxLng: -66.9, minLat: 24.4,  maxLat: 49.4 },
  { name: "Canada",         flag: "🇨🇦", minLng: -141.0,maxLng: -52.6, minLat: 41.7,  maxLat: 83.1 },
  { name: "Australia",      flag: "🇦🇺", minLng: 113.3, maxLng: 153.6, minLat: -43.6, maxLat: -10.7 },
  { name: "New Zealand",    flag: "🇳🇿", minLng: 166.4, maxLng: 178.6, minLat: -47.3, maxLat: -34.4 },
  { name: "Japan",          flag: "🇯🇵", minLng: 129.4, maxLng: 145.9, minLat: 31.0,  maxLat: 45.5 },
  { name: "South Korea",    flag: "🇰🇷", minLng: 126.0, maxLng: 129.6, minLat: 34.0,  maxLat: 38.6 },
  { name: "China",          flag: "🇨🇳", minLng: 73.5,  maxLng: 135.1, minLat: 18.2,  maxLat: 53.6 },
  { name: "India",          flag: "🇮🇳", minLng: 68.1,  maxLng: 97.4,  minLat: 8.1,   maxLat: 37.1 },
  { name: "Brazil",         flag: "🇧🇷", minLng: -73.9, maxLng: -28.8, minLat: -33.8, maxLat: 5.3  },
  { name: "Argentina",      flag: "🇦🇷", minLng: -73.6, maxLng: -53.6, minLat: -55.1, maxLat: -21.8 },
  { name: "South Africa",   flag: "🇿🇦", minLng: 16.5,  maxLng: 32.9,  minLat: -34.8, maxLat: -22.1 },
  { name: "Morocco",        flag: "🇲🇦", minLng: -13.2, maxLng: -1.0,  minLat: 27.7,  maxLat: 35.9 },
  { name: "Kenya",          flag: "🇰🇪", minLng: 33.9,  maxLng: 41.9,  minLat: -4.7,  maxLat: 5.0  },
  { name: "Ethiopia",       flag: "🇪🇹", minLng: 33.0,  maxLng: 47.9,  minLat: 3.4,   maxLat: 14.9 },
  { name: "Mexico",         flag: "🇲🇽", minLng: -117.1,maxLng: -86.7, minLat: 14.5,  maxLat: 32.7 },
  { name: "Singapore",      flag: "🇸🇬", minLng: 103.6, maxLng: 104.0, minLat: 1.2,   maxLat: 1.5  },
  { name: "UAE",            flag: "🇦🇪", minLng: 51.6,  maxLng: 56.4,  minLat: 22.6,  maxLat: 26.1 },
  { name: "Israel",         flag: "🇮🇱", minLng: 34.3,  maxLng: 35.9,  minLat: 29.5,  maxLat: 33.3 },
  { name: "Thailand",       flag: "🇹🇭", minLng: 97.3,  maxLng: 105.6, minLat: 5.6,   maxLat: 20.5 },
  { name: "Malaysia",       flag: "🇲🇾", minLng: 99.6,  maxLng: 119.3, minLat: 0.9,   maxLat: 7.4  },
  { name: "Indonesia",      flag: "🇮🇩", minLng: 95.0,  maxLng: 141.0, minLat: -11.0, maxLat: 6.1  },
  { name: "Philippines",    flag: "🇵🇭", minLng: 116.9, maxLng: 126.6, minLat: 4.6,   maxLat: 20.9 },
  { name: "Taiwan",         flag: "🇹🇼", minLng: 120.0, maxLng: 122.0, minLat: 21.9,  maxLat: 25.3 },
];

export function coordToCountry(lng, lat) {
  return COUNTRY_BOUNDS.find(
    c => lng >= c.minLng && lng <= c.maxLng && lat >= c.minLat && lat <= c.maxLat
  ) ?? null;
}
