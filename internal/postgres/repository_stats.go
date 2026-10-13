package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// StatsFilter scopes aggregate queries to a subset of activities.
type StatsFilter struct {
	SportType string
	From      time.Time
	To        time.Time
}

// Stats holds aggregate metrics for a set of activities.
type Stats struct {
	TotalActivities     int
	TotalDistanceMeters float64
	TotalElapsedMs      int64
	TotalCalories       int
	AvgPaceSecondsPerKM *float64
	AvgHeartRate        *float64
	BySport             []SportStats
}

// SportStats holds per-sport aggregate metrics.
type SportStats struct {
	SportType      string
	ActivityCount  int
	DistanceMeters float64
}

// TimeseriesPoint holds aggregate metrics for a single time bucket.
type TimeseriesPoint struct {
	PeriodStart    time.Time
	DistanceMeters float64
	ActivityCount  int
	AvgPace        *float64
	AvgHeartRate   *float64
}

// GetStats returns aggregate metrics for the given athlete and filter.
func (r *Repository) GetStats(ctx context.Context, athleteID string, filter StatsFilter) (Stats, error) {
	if athleteID == "" {
		return Stats{}, errors.New("athlete id is required")
	}

	args, where := statsArgs(athleteID, filter)
	clause := strings.Join(where, " AND ")

	// Single pass: totals via FILTER aggregates + per-sport breakdown via GROUPING SETS.
	// GROUPING SETS((),(sport_type)) produces two groups per row:
	//   - sport_type IS NULL → the overall total row
	//   - sport_type IS NOT NULL → per-sport subtotals
	var s Stats
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			sport_type,
			COUNT(*)                                                               AS cnt,
			COALESCE(SUM(distance_meters), 0)                                     AS dist,
			COALESCE(SUM(elapsed_duration_ms), 0)                                 AS elapsed,
			COALESCE(SUM(calories), 0)                                            AS cals,
			AVG(average_pace_seconds_per_km) FILTER (WHERE average_pace_seconds_per_km IS NOT NULL),
			AVG(average_heart_rate)          FILTER (WHERE average_heart_rate IS NOT NULL)
		FROM activities
		WHERE %s
		GROUP BY GROUPING SETS ((), (sport_type))
		ORDER BY sport_type NULLS FIRST`, clause), args...)
	if err != nil {
		return Stats{}, fmt.Errorf("get stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sportType *string
		var cnt int
		var dist float64
		var elapsed int64
		var cals int64
		var pace, hr *float64
		if err := rows.Scan(&sportType, &cnt, &dist, &elapsed, &cals, &pace, &hr); err != nil {
			return Stats{}, fmt.Errorf("scan stats: %w", err)
		}
		if sportType == nil {
			// Overall total row
			s.TotalActivities = cnt
			s.TotalDistanceMeters = dist
			s.TotalElapsedMs = elapsed
			s.TotalCalories = int(cals)
			s.AvgPaceSecondsPerKM = pace
			s.AvgHeartRate = hr
		} else {
			s.BySport = append(s.BySport, SportStats{
				SportType:      *sportType,
				ActivityCount:  cnt,
				DistanceMeters: dist,
			})
		}
	}
	return s, rows.Err()
}

// GetTimeseries returns per-bucket aggregate metrics grouped by the given granularity.
// Supported granularities: "day", "week", "month".
func (r *Repository) GetTimeseries(ctx context.Context, athleteID, granularity string, filter StatsFilter) ([]TimeseriesPoint, error) {
	if athleteID == "" {
		return nil, errors.New("athlete id is required")
	}
	switch granularity {
	case "day", "week", "month":
	default:
		return nil, fmt.Errorf("invalid granularity %q; must be day, week, or month", granularity)
	}

	args, where := statsArgs(athleteID, filter)
	clause := strings.Join(where, " AND ")

	// granularity is whitelisted above; safe to interpolate into the query.
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			date_trunc('%s', start_at) AS period_start,
			COALESCE(SUM(distance_meters), 0),
			COUNT(*),
			AVG(average_pace_seconds_per_km) FILTER (WHERE average_pace_seconds_per_km IS NOT NULL),
			AVG(average_heart_rate)          FILTER (WHERE average_heart_rate IS NOT NULL)
		FROM activities
		WHERE %s
		GROUP BY period_start
		ORDER BY period_start`, granularity, clause), args...,
	)
	if err != nil {
		return nil, fmt.Errorf("get timeseries: %w", err)
	}
	defer rows.Close()

	var points []TimeseriesPoint
	for rows.Next() {
		var p TimeseriesPoint
		if err := rows.Scan(&p.PeriodStart, &p.DistanceMeters, &p.ActivityCount, &p.AvgPace, &p.AvgHeartRate); err != nil {
			return nil, fmt.Errorf("scan timeseries point: %w", err)
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// Streaks holds current and longest day/week training streaks.
type Streaks struct {
	CurrentDays   int
	LongestDays   int
	CurrentWeeks  int
	LongestWeeks  int
}

// GetStreaks computes day and week training streaks using the islands-and-gaps technique.
// A day streak is still "current" if the last active day was today or yesterday.
// A week streak is still "current" if the last active ISO week includes or precedes this week.
func (r *Repository) GetStreaks(ctx context.Context, athleteID string) (Streaks, error) {
	if athleteID == "" {
		return Streaks{}, errors.New("athlete id is required")
	}

	row := r.pool.QueryRow(ctx, `
		WITH active_days AS (
			SELECT DISTINCT start_at::date AS day
			FROM activities
			WHERE athlete_id = $1 AND delete_indicator = false
		),
		numbered_days AS (
			SELECT day, ROW_NUMBER() OVER (ORDER BY day) AS rn
			FROM active_days
		),
		day_groups AS (
			SELECT day, (day - (rn || ' days')::interval)::date AS grp
			FROM numbered_days
		),
		day_streaks AS (
			SELECT grp, COUNT(*) AS len, MAX(day) AS last_day
			FROM day_groups
			GROUP BY grp
		),
		active_weeks AS (
			SELECT DISTINCT date_trunc('week', start_at)::date AS week
			FROM activities
			WHERE athlete_id = $1 AND delete_indicator = false
		),
		numbered_weeks AS (
			SELECT week, ROW_NUMBER() OVER (ORDER BY week) AS rn
			FROM active_weeks
		),
		week_groups AS (
			SELECT week, (week - (rn * 7 || ' days')::interval)::date AS grp
			FROM numbered_weeks
		),
		week_streaks AS (
			SELECT grp, COUNT(*) AS len, MAX(week) AS last_week
			FROM week_groups
			GROUP BY grp
		)
		SELECT
			COALESCE(MAX(ds.len), 0),
			COALESCE((SELECT len FROM day_streaks  WHERE last_day  >= CURRENT_DATE - 1 ORDER BY last_day  DESC LIMIT 1), 0),
			COALESCE(MAX(ws.len), 0),
			COALESCE((SELECT len FROM week_streaks WHERE last_week >= date_trunc('week', CURRENT_DATE)::date - 7 ORDER BY last_week DESC LIMIT 1), 0)
		FROM day_streaks ds, week_streaks ws
	`, athleteID)

	var s Streaks
	if err := row.Scan(&s.LongestDays, &s.CurrentDays, &s.LongestWeeks, &s.CurrentWeeks); err != nil {
		return Streaks{}, fmt.Errorf("get streaks: %w", err)
	}
	return s, nil
}

// BestPeriod holds a period start and total distance.
type BestPeriod struct {
	PeriodStart time.Time
	DistanceMeters float64
}

// Bests holds the best week and best month by distance.
type Bests struct {
	BestWeek  *BestPeriod
	BestMonth *BestPeriod
}

// GetBests returns the single best week and best month by total distance.
func (r *Repository) GetBests(ctx context.Context, athleteID string) (Bests, error) {
	if athleteID == "" {
		return Bests{}, errors.New("athlete id is required")
	}

	query := func(gran string) (*BestPeriod, error) {
		var p BestPeriod
		err := r.pool.QueryRow(ctx, fmt.Sprintf(`
			SELECT date_trunc('%s', start_at), COALESCE(SUM(distance_meters), 0)
			FROM activities
			WHERE athlete_id = $1 AND delete_indicator = false AND distance_meters IS NOT NULL
			GROUP BY 1
			ORDER BY 2 DESC
			LIMIT 1`, gran), athleteID,
		).Scan(&p.PeriodStart, &p.DistanceMeters)
		if err != nil {
			return nil, err
		}
		return &p, nil
	}

	var b Bests
	var err error
	if b.BestWeek, err = query("week"); err != nil && err.Error() != "no rows in result set" {
		return Bests{}, fmt.Errorf("get best week: %w", err)
	}
	if b.BestMonth, err = query("month"); err != nil && err.Error() != "no rows in result set" {
		return Bests{}, fmt.Errorf("get best month: %w", err)
	}
	return b, nil
}

// PMCPoint holds one day's training load and derived fitness/fatigue/form values.
type PMCPoint struct {
	Date  time.Time
	TRIMP float64
	CTL   float64
	ATL   float64
	TSB   float64
}

// GetPMC computes the Performance Management Chart data for the given athlete.
// It reads all activities from the beginning of recorded history to build accurate
// CTL/ATL values, then returns only the [from, to] slice.
// hrMax is the athlete's max heart rate; if 0, load is estimated from duration only.
func (r *Repository) GetPMC(ctx context.Context, athleteID string, from, to time.Time, hrMax int) ([]PMCPoint, error) {
	if athleteID == "" {
		return nil, errors.New("athlete id is required")
	}

	rows, err := r.pool.Query(ctx, `
		SELECT start_at::date, COALESCE(SUM(elapsed_duration_ms), 0), AVG(average_heart_rate)
		FROM activities
		WHERE athlete_id = $1 AND delete_indicator = false
		GROUP BY start_at::date
		ORDER BY start_at::date
	`, athleteID)
	if err != nil {
		return nil, fmt.Errorf("pmc query: %w", err)
	}
	defer rows.Close()

	type dayLoad struct {
		elapsedMs int64
		avgHR     *float64
	}
	byDay := map[string]dayLoad{}
	var firstDate time.Time

	for rows.Next() {
		var day time.Time
		var elapsedMs int64
		var avgHR *float64
		if err := rows.Scan(&day, &elapsedMs, &avgHR); err != nil {
			return nil, fmt.Errorf("pmc scan: %w", err)
		}
		key := day.Format("2006-01-02")
		byDay[key] = dayLoad{elapsedMs, avgHR}
		if firstDate.IsZero() {
			firstDate = day
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if firstDate.IsZero() {
		return nil, nil
	}

	// EWA decay constants
	const tau_ctl = 42.0
	const tau_atl = 7.0
	kCTL := math.Exp(-1.0 / tau_ctl)
	kATL := math.Exp(-1.0 / tau_atl)

	trimp := func(elapsedMs int64, avgHR *float64) float64 {
		mins := float64(elapsedMs) / 60000.0
		if hrMax > 0 && avgHR != nil && *avgHR > 0 {
			ratio := *avgHR / float64(hrMax)
			if ratio > 1 {
				ratio = 1
			}
			return mins * ratio * math.Exp(1.92*ratio)
		}
		return mins
	}

	end := to
	if end.IsZero() {
		end = time.Now().UTC().Truncate(24 * time.Hour)
	}

	var ctl, atl float64
	var points []PMCPoint
	for d := firstDate; !d.After(end); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		load := byDay[key]
		t := trimp(load.elapsedMs, load.avgHR)
		tsb := ctl - atl
		ctl = ctl*kCTL + t*(1-kCTL)
		atl = atl*kATL + t*(1-kATL)
		if !from.IsZero() && d.Before(from) {
			continue
		}
		points = append(points, PMCPoint{
			Date:  d,
			TRIMP: math.Round(t*10) / 10,
			CTL:   math.Round(ctl*10) / 10,
			ATL:   math.Round(atl*10) / 10,
			TSB:   math.Round(tsb*10) / 10,
		})
	}
	return points, nil
}

// YearReview holds all aggregated data for a single year's review page.
type YearReview struct {
	Year                int
	TotalDistanceMeters float64
	TotalElapsedMs      int64
	TotalElevationMeters float64
	TotalActivities     int
	BySport             []SportStats
	ByMonth             []MonthStats
	BiggestActivity     *ActivitySummary
	MostUsedGear        *GearSummary
	PRsSet              int
}

type MonthStats struct {
	Month          int
	DistanceMeters float64
	ActivityCount  int
}

type ActivitySummary struct {
	ID             string
	Name           string
	StartAt        time.Time
	DistanceMeters float64
}

type GearSummary struct {
	ID             string
	Name           string
	ActivityCount  int
	DistanceMeters float64
}

// GetYearReview aggregates all data needed for the year-in-review page.
func (r *Repository) GetYearReview(ctx context.Context, athleteID string, year int) (YearReview, error) {
	if athleteID == "" {
		return YearReview{}, errors.New("athlete id is required")
	}

	rev := YearReview{Year: year}
	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd   := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)

	// ── Totals ────────────────────────────────────────────────────────────────
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(distance_meters), 0),
		       COALESCE(SUM(elapsed_duration_ms), 0),
		       COALESCE(SUM(elevation_gain_meters), 0)
		FROM activities
		WHERE athlete_id = $1 AND delete_indicator = false
		  AND start_at >= $2 AND start_at < $3
	`, athleteID, yearStart, yearEnd,
	).Scan(&rev.TotalActivities, &rev.TotalDistanceMeters, &rev.TotalElapsedMs, &rev.TotalElevationMeters)
	if err != nil {
		return YearReview{}, fmt.Errorf("year review totals: %w", err)
	}

	// ── By sport ──────────────────────────────────────────────────────────────
	sportRows, err := r.pool.Query(ctx, `
		SELECT sport_type, COUNT(*), COALESCE(SUM(distance_meters), 0)
		FROM activities
		WHERE athlete_id = $1 AND delete_indicator = false
		  AND start_at >= $2 AND start_at < $3
		GROUP BY sport_type ORDER BY COUNT(*) DESC
	`, athleteID, yearStart, yearEnd)
	if err != nil {
		return YearReview{}, fmt.Errorf("year review by sport: %w", err)
	}
	defer sportRows.Close()
	for sportRows.Next() {
		var s SportStats
		if err := sportRows.Scan(&s.SportType, &s.ActivityCount, &s.DistanceMeters); err != nil {
			return YearReview{}, err
		}
		rev.BySport = append(rev.BySport, s)
	}
	sportRows.Close()

	// ── By month ──────────────────────────────────────────────────────────────
	monthRows, err := r.pool.Query(ctx, `
		SELECT EXTRACT(MONTH FROM start_at)::int,
		       COALESCE(SUM(distance_meters), 0),
		       COUNT(*)
		FROM activities
		WHERE athlete_id = $1 AND delete_indicator = false
		  AND start_at >= $2 AND start_at < $3
		GROUP BY 1 ORDER BY 1
	`, athleteID, yearStart, yearEnd)
	if err != nil {
		return YearReview{}, fmt.Errorf("year review by month: %w", err)
	}
	defer monthRows.Close()
	for monthRows.Next() {
		var m MonthStats
		if err := monthRows.Scan(&m.Month, &m.DistanceMeters, &m.ActivityCount); err != nil {
			return YearReview{}, err
		}
		rev.ByMonth = append(rev.ByMonth, m)
	}
	monthRows.Close()

	// ── Biggest activity ──────────────────────────────────────────────────────
	var big ActivitySummary
	err = r.pool.QueryRow(ctx, `
		SELECT id::text, COALESCE(name,''), start_at, COALESCE(distance_meters, 0)
		FROM activities
		WHERE athlete_id = $1 AND delete_indicator = false
		  AND start_at >= $2 AND start_at < $3
		  AND distance_meters IS NOT NULL
		ORDER BY distance_meters DESC LIMIT 1
	`, athleteID, yearStart, yearEnd).Scan(&big.ID, &big.Name, &big.StartAt, &big.DistanceMeters)
	if err == nil {
		rev.BiggestActivity = &big
	}

	// ── Most used gear ────────────────────────────────────────────────────────
	var gear GearSummary
	err = r.pool.QueryRow(ctx, `
		SELECT g.id::text, g.name, COUNT(ag.activity_id), COALESCE(SUM(a.distance_meters), 0)
		FROM activity_gear ag
		JOIN gear g ON g.id = ag.gear_id
		JOIN activities a ON a.id = ag.activity_id
		WHERE a.athlete_id = $1
		  AND a.start_at >= $2 AND a.start_at < $3
		  AND a.delete_indicator = false
		GROUP BY g.id, g.name
		ORDER BY COUNT(ag.activity_id) DESC LIMIT 1
	`, athleteID, yearStart, yearEnd).Scan(&gear.ID, &gear.Name, &gear.ActivityCount, &gear.DistanceMeters)
	if err == nil {
		rev.MostUsedGear = &gear
	}

	// ── PRs set this year ─────────────────────────────────────────────────────
	// Count distinct distances where the all-time best was first achieved in this year.
	err = r.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT be1.distance_meters)
		FROM best_efforts be1
		WHERE be1.athlete_id = $1
		  AND be1.achieved_at >= $2 AND be1.achieved_at < $3
		  AND be1.elapsed_ms = (
		    SELECT MIN(be2.elapsed_ms)
		    FROM best_efforts be2
		    WHERE be2.athlete_id = be1.athlete_id
		      AND be2.sport_type = be1.sport_type
		      AND be2.distance_meters = be1.distance_meters
		  )
	`, athleteID, yearStart, yearEnd).Scan(&rev.PRsSet)
	if err != nil {
		rev.PRsSet = 0
	}

	return rev, nil
}

func statsArgs(athleteID string, f StatsFilter) ([]any, []string) {
	args := []any{athleteID}
	where := []string{"athlete_id = $1", "delete_indicator = false"}
	if f.SportType != "" {
		args = append(args, f.SportType)
		where = append(where, fmt.Sprintf("sport_type = $%d", len(args)))
	}
	if !f.From.IsZero() {
		args = append(args, f.From.UTC())
		where = append(where, fmt.Sprintf("start_at >= $%d", len(args)))
	}
	if !f.To.IsZero() {
		args = append(args, f.To.UTC())
		where = append(where, fmt.Sprintf("start_at < $%d", len(args)))
	}
	return args, where
}
