package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"athlete-log/internal/storage"
)

type statActivity struct {
	ID              string
	Name            string
	SportType       string
	StartAt         time.Time
	DistanceMeters  float64
	HasDistance     bool
	ElapsedMs       int64
	HasElapsed      bool
	ElevationMeters float64
	Calories        int
	HasCalories     bool
	Pace            *float64
	HeartRate       *float64
}

func (r *Repository) GetStats(ctx context.Context, athleteID string, filter storage.StatsFilter) (storage.Stats, error) {
	activities, err := r.statActivities(ctx, athleteID, filter)
	if err != nil {
		return storage.Stats{}, err
	}

	var stats storage.Stats
	var paceSum, hrSum float64
	var paceCount, hrCount int
	bySport := map[string]*storage.SportStats{}

	for _, a := range activities {
		stats.TotalActivities++
		stats.TotalDistanceMeters += a.DistanceMeters
		stats.TotalElapsedMs += a.ElapsedMs
		stats.TotalCalories += a.Calories
		if a.Pace != nil {
			paceSum += *a.Pace
			paceCount++
		}
		if a.HeartRate != nil {
			hrSum += *a.HeartRate
			hrCount++
		}
		item := bySport[a.SportType]
		if item == nil {
			item = &storage.SportStats{SportType: a.SportType}
			bySport[a.SportType] = item
		}
		item.ActivityCount++
		item.DistanceMeters += a.DistanceMeters
	}
	if paceCount > 0 {
		v := paceSum / float64(paceCount)
		stats.AvgPaceSecondsPerKM = &v
	}
	if hrCount > 0 {
		v := hrSum / float64(hrCount)
		stats.AvgHeartRate = &v
	}

	keys := make([]string, 0, len(bySport))
	for key := range bySport {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		stats.BySport = append(stats.BySport, *bySport[key])
	}
	return stats, nil
}

func (r *Repository) GetTimeseries(ctx context.Context, athleteID, granularity string, filter storage.StatsFilter) ([]storage.TimeseriesPoint, error) {
	switch granularity {
	case "day", "week", "month":
	default:
		return nil, fmt.Errorf("invalid granularity %q; must be day, week, or month", granularity)
	}
	activities, err := r.statActivities(ctx, athleteID, filter)
	if err != nil {
		return nil, err
	}

	type bucket struct {
		point     storage.TimeseriesPoint
		paceSum   float64
		paceCount int
		hrSum     float64
		hrCount   int
	}
	buckets := map[string]*bucket{}
	for _, a := range activities {
		start := bucketStart(a.StartAt, granularity)
		key := start.Format(time.RFC3339)
		b := buckets[key]
		if b == nil {
			b = &bucket{point: storage.TimeseriesPoint{PeriodStart: start}}
			buckets[key] = b
		}
		b.point.DistanceMeters += a.DistanceMeters
		b.point.ActivityCount++
		if a.Pace != nil {
			b.paceSum += *a.Pace
			b.paceCount++
		}
		if a.HeartRate != nil {
			b.hrSum += *a.HeartRate
			b.hrCount++
		}
	}

	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	points := make([]storage.TimeseriesPoint, 0, len(keys))
	for _, key := range keys {
		b := buckets[key]
		if b.paceCount > 0 {
			v := b.paceSum / float64(b.paceCount)
			b.point.AvgPace = &v
		}
		if b.hrCount > 0 {
			v := b.hrSum / float64(b.hrCount)
			b.point.AvgHeartRate = &v
		}
		points = append(points, b.point)
	}
	return points, nil
}

func (r *Repository) GetStreaks(ctx context.Context, athleteID string) (storage.Streaks, error) {
	activities, err := r.statActivities(ctx, athleteID, storage.StatsFilter{})
	if err != nil {
		return storage.Streaks{}, err
	}
	daySet := map[string]time.Time{}
	weekSet := map[string]time.Time{}
	for _, a := range activities {
		day := dayStart(a.StartAt)
		week := bucketStart(a.StartAt, "week")
		daySet[day.Format("2006-01-02")] = day
		weekSet[week.Format("2006-01-02")] = week
	}

	longestDays, currentDays := streakLengths(daySet, dayStart(time.Now().UTC()).AddDate(0, 0, -1), 24*time.Hour)
	longestWeeks, currentWeeks := streakLengths(weekSet, bucketStart(time.Now().UTC(), "week").AddDate(0, 0, -7), 7*24*time.Hour)
	return storage.Streaks{
		CurrentDays:  currentDays,
		LongestDays:  longestDays,
		CurrentWeeks: currentWeeks,
		LongestWeeks: longestWeeks,
	}, nil
}

func (r *Repository) GetBests(ctx context.Context, athleteID string) (storage.Bests, error) {
	var bests storage.Bests
	week, err := bestPeriod(ctx, r, athleteID, "week")
	if err != nil {
		return storage.Bests{}, err
	}
	month, err := bestPeriod(ctx, r, athleteID, "month")
	if err != nil {
		return storage.Bests{}, err
	}
	bests.BestWeek = week
	bests.BestMonth = month
	return bests, nil
}

func (r *Repository) GetPMC(ctx context.Context, athleteID string, from, to time.Time, hrMax int) ([]storage.PMCPoint, error) {
	activities, err := r.statActivities(ctx, athleteID, storage.StatsFilter{})
	if err != nil {
		return nil, err
	}
	if len(activities) == 0 {
		return nil, nil
	}

	type dayLoad struct {
		elapsedMs int64
		hrSum     float64
		hrCount   int
	}
	byDay := map[string]*dayLoad{}
	var firstDate time.Time
	for _, a := range activities {
		day := dayStart(a.StartAt)
		if firstDate.IsZero() || day.Before(firstDate) {
			firstDate = day
		}
		key := day.Format("2006-01-02")
		load := byDay[key]
		if load == nil {
			load = &dayLoad{}
			byDay[key] = load
		}
		load.elapsedMs += a.ElapsedMs
		if a.HeartRate != nil {
			load.hrSum += *a.HeartRate
			load.hrCount++
		}
	}

	const tauCTL = 42.0
	const tauATL = 7.0
	kCTL := math.Exp(-1.0 / tauCTL)
	kATL := math.Exp(-1.0 / tauATL)

	end := to
	if end.IsZero() {
		end = dayStart(time.Now().UTC())
	}
	var ctl, atl float64
	var points []storage.PMCPoint
	for d := firstDate; !d.After(end); d = d.AddDate(0, 0, 1) {
		load := byDay[d.Format("2006-01-02")]
		var t float64
		if load != nil {
			var avgHR *float64
			if load.hrCount > 0 {
				v := load.hrSum / float64(load.hrCount)
				avgHR = &v
			}
			t = trimp(load.elapsedMs, avgHR, hrMax)
		}
		tsb := ctl - atl
		ctl = ctl*kCTL + t*(1-kCTL)
		atl = atl*kATL + t*(1-kATL)
		if !from.IsZero() && d.Before(from) {
			continue
		}
		points = append(points, storage.PMCPoint{
			Date:  d,
			TRIMP: math.Round(t*10) / 10,
			CTL:   math.Round(ctl*10) / 10,
			ATL:   math.Round(atl*10) / 10,
			TSB:   math.Round(tsb*10) / 10,
		})
	}
	return points, nil
}

func (r *Repository) GetYearReview(ctx context.Context, athleteID string, year int) (storage.YearReview, error) {
	if athleteID == "" {
		return storage.YearReview{}, errors.New("athlete id is required")
	}
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
	activities, err := r.statActivities(ctx, athleteID, storage.StatsFilter{From: from, To: to})
	if err != nil {
		return storage.YearReview{}, err
	}

	rev := storage.YearReview{Year: year}
	bySport := map[string]*storage.SportStats{}
	byMonth := map[int]*storage.MonthStats{}
	for _, a := range activities {
		rev.TotalActivities++
		rev.TotalDistanceMeters += a.DistanceMeters
		rev.TotalElapsedMs += a.ElapsedMs
		rev.TotalElevationMeters += a.ElevationMeters
		sport := bySport[a.SportType]
		if sport == nil {
			sport = &storage.SportStats{SportType: a.SportType}
			bySport[a.SportType] = sport
		}
		sport.ActivityCount++
		sport.DistanceMeters += a.DistanceMeters

		monthNumber := int(a.StartAt.Month())
		month := byMonth[monthNumber]
		if month == nil {
			month = &storage.MonthStats{Month: monthNumber}
			byMonth[monthNumber] = month
		}
		month.ActivityCount++
		month.DistanceMeters += a.DistanceMeters

		if a.HasDistance && (rev.BiggestActivity == nil || a.DistanceMeters > rev.BiggestActivity.DistanceMeters) {
			rev.BiggestActivity = &storage.ActivitySummary{
				ID:             a.ID,
				Name:           a.Name,
				StartAt:        a.StartAt,
				DistanceMeters: a.DistanceMeters,
			}
		}
	}

	sportKeys := make([]string, 0, len(bySport))
	for key := range bySport {
		sportKeys = append(sportKeys, key)
	}
	sort.Strings(sportKeys)
	for _, key := range sportKeys {
		rev.BySport = append(rev.BySport, *bySport[key])
	}
	monthKeys := make([]int, 0, len(byMonth))
	for key := range byMonth {
		monthKeys = append(monthKeys, key)
	}
	sort.Ints(monthKeys)
	for _, key := range monthKeys {
		rev.ByMonth = append(rev.ByMonth, *byMonth[key])
	}

	var gear storage.GearSummary
	err = r.db.QueryRowContext(ctx, `
		SELECT g.id, g.name, COUNT(ag.activity_id), COALESCE(SUM(COALESCE(a.distance_meters, 0)), 0)
		FROM activity_gear ag
		JOIN gear g ON g.id = ag.gear_id
		JOIN activities a ON a.id = ag.activity_id
		WHERE a.athlete_id = ?
		  AND a.start_at >= ? AND a.start_at < ?
		  AND a.delete_indicator = 0
		GROUP BY g.id, g.name
		ORDER BY COUNT(ag.activity_id) DESC
		LIMIT 1`,
		athleteID, formatTime(from), formatTime(to),
	).Scan(&gear.ID, &gear.Name, &gear.ActivityCount, &gear.DistanceMeters)
	if err == nil {
		rev.MostUsedGear = &gear
	}

	err = r.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT be1.distance_meters)
		FROM best_efforts be1
		WHERE be1.athlete_id = ?
		  AND be1.achieved_at >= ? AND be1.achieved_at < ?
		  AND be1.elapsed_ms = (
		    SELECT MIN(be2.elapsed_ms)
		    FROM best_efforts be2
		    WHERE be2.athlete_id = be1.athlete_id
		      AND be2.sport_type = be1.sport_type
		      AND be2.distance_meters = be1.distance_meters
		  )`,
		athleteID, formatTime(from), formatTime(to),
	).Scan(&rev.PRsSet)
	if err != nil {
		rev.PRsSet = 0
	}
	return rev, nil
}

func (r *Repository) statActivities(ctx context.Context, athleteID string, filter storage.StatsFilter) ([]statActivity, error) {
	if athleteID == "" {
		return nil, errors.New("athlete id is required")
	}
	args := []any{athleteID}
	where := []string{"athlete_id = ?", "delete_indicator = 0"}
	if filter.SportType != "" {
		args = append(args, filter.SportType)
		where = append(where, "sport_type = ?")
	}
	if !filter.From.IsZero() {
		args = append(args, formatTime(filter.From.UTC()))
		where = append(where, "start_at >= ?")
	}
	if !filter.To.IsZero() {
		args = append(args, formatTime(filter.To.UTC()))
		where = append(where, "start_at < ?")
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, sport_type, start_at,
		       distance_meters, elapsed_duration_ms, elevation_gain_meters,
		       calories, average_pace_seconds_per_km, average_heart_rate
		FROM activities
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY start_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("query stat activities: %w", err)
	}
	defer rows.Close()

	var activities []statActivity
	for rows.Next() {
		var (
			a           statActivity
			startAtText string
			distance    sql.NullFloat64
			elapsedMs   sql.NullInt64
			elevation   sql.NullFloat64
			calories    sql.NullInt64
			pace        sql.NullFloat64
			heartRate   sql.NullFloat64
		)
		if err := rows.Scan(
			&a.ID,
			&a.Name,
			&a.SportType,
			&startAtText,
			&distance,
			&elapsedMs,
			&elevation,
			&calories,
			&pace,
			&heartRate,
		); err != nil {
			return nil, fmt.Errorf("scan stat activity: %w", err)
		}
		startAt, err := parseTime(startAtText)
		if err != nil {
			return nil, err
		}
		a.StartAt = startAt
		if distance.Valid {
			a.DistanceMeters = distance.Float64
			a.HasDistance = true
		}
		if elapsedMs.Valid {
			a.ElapsedMs = elapsedMs.Int64
			a.HasElapsed = true
		}
		if elevation.Valid {
			a.ElevationMeters = elevation.Float64
		}
		if calories.Valid {
			a.Calories = int(calories.Int64)
			a.HasCalories = true
		}
		if pace.Valid {
			a.Pace = &pace.Float64
		}
		if heartRate.Valid {
			a.HeartRate = &heartRate.Float64
		}
		activities = append(activities, a)
	}
	return activities, rows.Err()
}

func bestPeriod(ctx context.Context, r *Repository, athleteID, granularity string) (*storage.BestPeriod, error) {
	points, err := r.GetTimeseries(ctx, athleteID, granularity, storage.StatsFilter{})
	if err != nil {
		return nil, err
	}
	var best *storage.BestPeriod
	for _, p := range points {
		if p.DistanceMeters <= 0 {
			continue
		}
		if best == nil || p.DistanceMeters > best.DistanceMeters {
			best = &storage.BestPeriod{PeriodStart: p.PeriodStart, DistanceMeters: p.DistanceMeters}
		}
	}
	return best, nil
}

func bucketStart(t time.Time, granularity string) time.Time {
	t = dayStart(t)
	switch granularity {
	case "day":
		return t
	case "week":
		daysSinceMonday := (int(t.Weekday()) + 6) % 7
		return t.AddDate(0, 0, -daysSinceMonday)
	case "month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return t
	}
}

func dayStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func streakLengths(values map[string]time.Time, currentThreshold time.Time, step time.Duration) (longest, current int) {
	if len(values) == 0 {
		return 0, 0
	}
	dates := make([]time.Time, 0, len(values))
	for _, t := range values {
		dates = append(dates, t)
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })

	run := 0
	var prev time.Time
	var runLast time.Time
	for _, d := range dates {
		if run == 0 || d.Sub(prev) == step {
			run++
		} else {
			run = 1
		}
		prev = d
		runLast = d
		if run > longest {
			longest = run
		}
		if !runLast.Before(currentThreshold) {
			current = run
		}
	}
	return longest, current
}

func trimp(elapsedMs int64, avgHR *float64, hrMax int) float64 {
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
