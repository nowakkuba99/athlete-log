package api

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// ─── Share card SVG generator ─────────────────────────────────────────────────
//
// Produces a 1200×630 SVG suitable for social sharing (OG image dimensions).
// Three themes: "dark" (default), "light", "transparent".

const (
	cardW = 1200
	cardH = 630

	routePanelW = 720
	routePad    = 50
	statsPanelX = 760
)

type cardColors struct {
	bg      string
	text    string
	muted   string
	faint   string
	routeA  string
	routeB  string
	divider string
}

var cardThemes = map[string]cardColors{
	"dark": {
		bg: "#0f172a", text: "#ffffff", muted: "#94a3b8", faint: "#334155",
		routeA: "#0d9488", routeB: "#3b82f6", divider: "#1e293b",
	},
	"light": {
		bg: "#f8fafc", text: "#0f172a", muted: "#64748b", faint: "#cbd5e1",
		routeA: "#0d9488", routeB: "#2563eb", divider: "#e2e8f0",
	},
	"transparent": {
		bg: "", text: "#ffffff", muted: "#e2e8f0", faint: "#ffffff40",
		routeA: "#2dd4bf", routeB: "#93c5fd", divider: "#ffffff20",
	},
}

type cardActivity struct {
	Name                string
	SportType           string
	StartAt             string
	DistanceMeters      *float64
	ElapsedMs           *int64
	PaceSecsPerKM       *float64
	ElevationGainMeters *float64
	AvgHeartRate        *float64
	MaxHeartRate        *float64
}

type routePt struct{ x, y float64 }

// xmlEscape escapes text content for embedding in SVG XML.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// truncateRunes safely cuts a string to at most n runes, appending "…" if trimmed.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

// titleCase capitalises the first letter of each word (simple ASCII version).
func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// GenerateShareCard creates a 1200×630 SVG share card.
// samples is [lng, lat] pairs; imperial selects unit system.
func GenerateShareCard(act cardActivity, samples [][2]float64, theme string, imperial bool) string {
	tc, ok := cardThemes[theme]
	if !ok {
		tc = cardThemes["dark"]
	}

	pts := normalizeRoute(samples,
		float64(routePad), float64(routePad),
		float64(routePanelW-2*routePad),
		float64(cardH-2*routePad),
	)

	var minX, maxX, minY, maxY float64
	if len(pts) > 0 {
		minX, maxX = pts[0].x, pts[0].x
		minY, maxY = pts[0].y, pts[0].y
		for _, p := range pts[1:] {
			if p.x < minX {
				minX = p.x
			}
			if p.x > maxX {
				maxX = p.x
			}
			if p.y < minY {
				minY = p.y
			}
			if p.y > maxY {
				maxY = p.y
			}
		}
	}

	routePath := svgPath(pts)

	// Format stats (all safe ASCII — no escaping needed).
	distNum, distUnit := cardDistParts(act.DistanceMeters, imperial)
	durStr := fmtCardElapsed(act.ElapsedMs)
	paceStr := fmtCardPace(act.PaceSecsPerKM, imperial)
	eleStr := fmtCardEle(act.ElevationGainMeters, imperial)
	hrStr := fmtCardHR(act.AvgHeartRate)

	// Text content that needs escaping.
	sportLabel := xmlEscape(strings.ToUpper(act.SportType))
	dateLabel := xmlEscape(fmtCardDate(act.StartAt))

	actName := act.Name
	if actName == "" {
		actName = titleCase(act.SportType)
	}
	actName = xmlEscape(truncateRunes(actName, 26))

	var sb strings.Builder

	// ── SVG header ─────────────────────────────────────────────────────────
	sb.WriteString(fmt.Sprintf(
		`<svg width="%d" height="%d" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg"`+
			` font-family="ui-sans-serif,system-ui,-apple-system,sans-serif">`,
		cardW, cardH, cardW, cardH,
	))

	// ── Defs ───────────────────────────────────────────────────────────────
	sb.WriteString("<defs>")

	// Lock icon + branding gradient
	sb.WriteString(`<linearGradient id="lg" x1="0%" y1="0%" x2="100%" y2="100%">`)
	sb.WriteString(fmt.Sprintf(`<stop offset="0%%" stop-color="%s"/>`, tc.routeA))
	sb.WriteString(fmt.Sprintf(`<stop offset="100%%" stop-color="%s"/>`, tc.routeB))
	sb.WriteString(`</linearGradient>`)

	// Route gradient across bounding box
	if len(pts) > 1 {
		sb.WriteString(fmt.Sprintf(
			`<linearGradient id="rg" gradientUnits="userSpaceOnUse" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f">`,
			minX, minY, maxX, maxY,
		))
		sb.WriteString(fmt.Sprintf(`<stop offset="0%%" stop-color="%s"/>`, tc.routeA))
		sb.WriteString(fmt.Sprintf(`<stop offset="100%%" stop-color="%s"/>`, tc.routeB))
		sb.WriteString(`</linearGradient>`)
	}

	// Glow filter
	sb.WriteString(`<filter id="gf" x="-60%" y="-60%" width="220%" height="220%">`)
	sb.WriteString(`<feGaussianBlur in="SourceGraphic" stdDeviation="28"/>`)
	sb.WriteString(`</filter>`)

	sb.WriteString("</defs>")

	// ── Background ─────────────────────────────────────────────────────────
	if tc.bg != "" {
		sb.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" fill="%s"/>`, cardW, cardH, tc.bg))
	}

	// Atmospheric glow centred on the route
	if len(pts) > 0 {
		cx := (minX + maxX) / 2
		cy := (minY + maxY) / 2
		sb.WriteString(fmt.Sprintf(
			`<ellipse cx="%.0f" cy="%.0f" rx="200" ry="160" fill="%s" fill-opacity="0.12" filter="url(#gf)"/>`,
			cx, cy, tc.routeA,
		))
	}

	// Panel divider
	sb.WriteString(fmt.Sprintf(
		`<line x1="%d" y1="40" x2="%d" y2="%d" stroke="%s" stroke-width="1"/>`,
		statsPanelX-20, statsPanelX-20, cardH-40, tc.divider,
	))

	// ── Route ──────────────────────────────────────────────────────────────
	if routePath != "" {
		// Glow layer
		sb.WriteString(fmt.Sprintf(
			`<path d="%s" stroke="url(#rg)" stroke-width="14" fill="none" stroke-opacity="0.20" stroke-linecap="round" stroke-linejoin="round"/>`,
			routePath,
		))
		// Main stroke
		sb.WriteString(fmt.Sprintf(
			`<path d="%s" stroke="url(#rg)" stroke-width="3.5" fill="none" stroke-opacity="0.92" stroke-linecap="round" stroke-linejoin="round"/>`,
			routePath,
		))
		// Start (teal) and end (blue) dots
		sb.WriteString(fmt.Sprintf(
			`<circle cx="%.1f" cy="%.1f" r="6" fill="%s" stroke="white" stroke-width="2"/>`,
			pts[0].x, pts[0].y, tc.routeA,
		))
		sb.WriteString(fmt.Sprintf(
			`<circle cx="%.1f" cy="%.1f" r="6" fill="%s" stroke="white" stroke-width="2"/>`,
			pts[len(pts)-1].x, pts[len(pts)-1].y, tc.routeB,
		))
	} else {
		sb.WriteString(fmt.Sprintf(
			`<text x="%d" y="%d" text-anchor="middle" font-size="16" fill="%s">No GPS data</text>`,
			routePanelW/2, cardH/2, tc.muted,
		))
	}

	// ── Lock icon branding (top-left of route panel) ────────────────────────
	lx, ly := 40, 24
	sb.WriteString(fmt.Sprintf(`<g transform="translate(%d,%d)">`, lx, ly))
	sb.WriteString(`<rect width="30" height="30" rx="7" fill="url(#lg)"/>`)
	sb.WriteString(`<path d="M9 15 L9 10 A6 6 0 0 1 21 10 L21 15" stroke="white" stroke-width="2.5" fill="none" stroke-linecap="round"/>`)
	sb.WriteString(`<rect x="5" y="15" width="20" height="12" rx="3" fill="white" fill-opacity="0.96"/>`)
	sb.WriteString(fmt.Sprintf(
		`<polyline points="7,21 9.5,21 11,17.5 13,24 14.5,17.5 16,21 23,21" stroke="%s" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>`,
		tc.routeA,
	))
	sb.WriteString(`</g>`)
	sb.WriteString(fmt.Sprintf(
		`<text x="%d" y="%d" font-size="13" font-weight="700" fill="%s" letter-spacing="0.5">ATHLETE LOG</text>`,
		lx+36, ly+20, tc.muted,
	))

	// ── Stats panel ────────────────────────────────────────────────────────
	sx := statsPanelX
	sy := 58

	// Sport · date
	sb.WriteString(fmt.Sprintf(
		`<text x="%d" y="%d" font-size="13" font-weight="600" fill="%s" letter-spacing="1">%s · %s</text>`,
		sx, sy+22, tc.muted, sportLabel, dateLabel,
	))

	// Activity name
	sb.WriteString(fmt.Sprintf(
		`<text x="%d" y="%d" font-size="22" font-weight="800" fill="%s">%s</text>`,
		sx, sy+58, tc.text, actName,
	))

	// Separator
	sb.WriteString(fmt.Sprintf(
		`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="1"/>`,
		sx, sy+74, sx+380, sy+74, tc.divider,
	))

	// Distance hero — number + unit on same baseline
	sb.WriteString(fmt.Sprintf(
		`<text x="%d" y="%d" font-size="78" font-weight="800" fill="%s" font-family="ui-monospace,monospace">%s</text>`,
		sx, sy+170, tc.text, distNum,
	))
	sb.WriteString(fmt.Sprintf(
		`<text x="%d" y="%d" font-size="28" font-weight="600" fill="%s">%s</text>`,
		sx, sy+206, tc.muted, distUnit,
	))

	// Stats grid 2×2
	col1, col2 := sx, sx+195
	row1y, row2y := sy+270, sy+360

	writeStat := func(x, y int, label, value string) {
		sb.WriteString(fmt.Sprintf(
			`<text x="%d" y="%d" font-size="10" font-weight="700" fill="%s" letter-spacing="1.8">%s</text>`,
			x, y, tc.faint, label,
		))
		sb.WriteString(fmt.Sprintf(
			`<text x="%d" y="%d" font-size="26" font-weight="700" fill="%s" font-family="ui-monospace,monospace">%s</text>`,
			x, y+32, tc.text, value,
		))
	}

	writeStat(col1, row1y, "DURATION", durStr)
	writeStat(col2, row1y, "AVG PACE", paceStr)
	writeStat(col1, row2y, "ELEVATION ↑", eleStr)
	if hrStr != "" {
		writeStat(col2, row2y, "AVG HR", hrStr)
	}

	// Footer branding
	sb.WriteString(fmt.Sprintf(
		`<text x="%d" y="%d" font-size="11" fill="%s">athlete-log · your data, your machine</text>`,
		sx, cardH-28, tc.faint,
	))

	sb.WriteString("</svg>")
	return sb.String()
}

// ─── Route normalization ──────────────────────────────────────────────────────

func normalizeRoute(gps [][2]float64, offX, offY, width, height float64) []routePt {
	if len(gps) == 0 {
		return nil
	}
	pts := gps
	if len(pts) > 300 {
		step := float64(len(pts)) / 300.0
		out := make([][2]float64, 0, 300)
		for i := 0.0; i < float64(len(pts)); i += step {
			out = append(out, pts[int(i)])
		}
		if out[len(out)-1] != pts[len(pts)-1] {
			out = append(out, pts[len(pts)-1])
		}
		pts = out
	}

	minLng, maxLng := pts[0][0], pts[0][0]
	minLat, maxLat := pts[0][1], pts[0][1]
	for _, c := range pts {
		if c[0] < minLng {
			minLng = c[0]
		}
		if c[0] > maxLng {
			maxLng = c[0]
		}
		if c[1] < minLat {
			minLat = c[1]
		}
		if c[1] > maxLat {
			maxLat = c[1]
		}
	}

	lngSpan := maxLng - minLng
	latSpan := maxLat - minLat
	if lngSpan < 0.0001 {
		lngSpan = 0.0001
	}
	if latSpan < 0.0001 {
		latSpan = 0.0001
	}

	scaleX := width / lngSpan
	scaleY := height / latSpan
	scale := math.Min(scaleX, scaleY)

	centreX := offX + (width-lngSpan*scale)/2
	centreY := offY + (height-latSpan*scale)/2

	out := make([]routePt, len(pts))
	for i, c := range pts {
		out[i] = routePt{
			x: centreX + (c[0]-minLng)*scale,
			y: centreY + (maxLat-c[1])*scale,
		}
	}
	return out
}

func svgPath(pts []routePt) string {
	if len(pts) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "M%.1f %.1f", pts[0].x, pts[0].y)
	for _, p := range pts[1:] {
		fmt.Fprintf(&sb, " L%.1f %.1f", p.x, p.y)
	}
	return sb.String()
}

// ─── Formatters ───────────────────────────────────────────────────────────────

// cardDistParts returns the numeric string and unit label separately.
func cardDistParts(m *float64, imperial bool) (num, unit string) {
	if m == nil {
		return "—", ""
	}
	if imperial {
		return fmt.Sprintf("%.2f", *m/1609.34), "mi"
	}
	return fmt.Sprintf("%.2f", *m/1000), "km"
}

func fmtCardPace(secsPerKM *float64, imperial bool) string {
	if secsPerKM == nil {
		return "—"
	}
	v := *secsPerKM
	unit := "/km"
	if imperial {
		v *= 1.60934
		unit = "/mi"
	}
	return fmt.Sprintf("%d:%02d%s", int(v)/60, int(v)%60, unit)
}

func fmtCardEle(m *float64, imperial bool) string {
	if m == nil {
		return "—"
	}
	if imperial {
		return fmt.Sprintf("+%.0fft", *m*3.28084)
	}
	return fmt.Sprintf("+%.0fm", *m)
}

func fmtCardElapsed(ms *int64) string {
	if ms == nil {
		return "—"
	}
	total := *ms / 1000
	h := total / 3600
	min := (total % 3600) / 60
	sec := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, min, sec)
	}
	return fmt.Sprintf("%d:%02d", min, sec)
}

func fmtCardHR(bpm *float64) string {
	if bpm == nil {
		return ""
	}
	return fmt.Sprintf("%.0f bpm", *bpm)
}

func fmtCardDate(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		if len(iso) >= 10 {
			t, err = time.Parse("2006-01-02", iso[:10])
		}
		if err != nil {
			return iso
		}
	}
	return strings.ToUpper(t.Format("Mon 2 Jan 2006"))
}
