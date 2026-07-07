package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"athlete-log/internal/domain"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) GetSourceConnection(ctx context.Context, id string) (domain.SourceConnection, error) {
	if id == "" {
		return domain.SourceConnection{}, errors.New("source connection id is required")
	}
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, athlete_id::text, source,
		       COALESCE(external_athlete_id, ''), status,
		       COALESCE(secret_key, ''), metadata::text,
		       created_at, updated_at
		FROM source_connections
		WHERE id = $1::uuid`,
		id)
	conn, err := scanSourceConnection(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SourceConnection{}, fmt.Errorf("source connection %q not found: %w", id, pgx.ErrNoRows)
	}
	if err != nil {
		return domain.SourceConnection{}, fmt.Errorf("get source connection: %w", err)
	}
	return conn, nil
}

func (r *Repository) ListSourceConnections(ctx context.Context, athleteID string) ([]domain.SourceConnection, error) {
	if athleteID == "" {
		return nil, errors.New("athlete id is required")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, athlete_id::text, source,
		       COALESCE(external_athlete_id, ''), status,
		       COALESCE(secret_key, ''), metadata::text,
		       created_at, updated_at
		FROM source_connections
		WHERE athlete_id = $1::uuid
		ORDER BY created_at`,
		athleteID)
	if err != nil {
		return nil, fmt.Errorf("list source connections: %w", err)
	}
	defer rows.Close()

	var conns []domain.SourceConnection
	for rows.Next() {
		conn, err := scanSourceConnection(rows)
		if err != nil {
			return nil, fmt.Errorf("scan source connection: %w", err)
		}
		conns = append(conns, conn)
	}
	return conns, rows.Err()
}

func (r *Repository) ListSourceConnectionsByType(ctx context.Context, athleteID string, sourceType domain.SourceType) ([]domain.SourceConnection, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, athlete_id::text, source,
		       COALESCE(external_athlete_id, ''), status,
		       COALESCE(secret_key, ''), metadata::text,
		       created_at, updated_at
		FROM source_connections
		WHERE athlete_id = $1::uuid AND source = $2
		ORDER BY created_at`,
		athleteID, string(sourceType))
	if err != nil {
		return nil, fmt.Errorf("list source connections by type: %w", err)
	}
	defer rows.Close()

	var conns []domain.SourceConnection
	for rows.Next() {
		conn, err := scanSourceConnection(rows)
		if err != nil {
			return nil, fmt.Errorf("scan source connection: %w", err)
		}
		conns = append(conns, conn)
	}
	return conns, rows.Err()
}

func (r *Repository) DeleteSourceConnection(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM source_connections WHERE id = $1::uuid`, id)
	return err
}

func scanSourceConnection(s scanner) (domain.SourceConnection, error) {
	var (
		conn     domain.SourceConnection
		source   string
		metaText string
	)
	err := s.Scan(
		&conn.ID,
		&conn.AthleteID,
		&source,
		&conn.ExternalAthleteID,
		&conn.Status,
		&conn.SecretKey,
		&metaText,
		&conn.CreatedAt,
		&conn.UpdatedAt,
	)
	if err != nil {
		return domain.SourceConnection{}, err
	}
	conn.Source = domain.SourceType(source)
	if err := json.Unmarshal([]byte(metaText), &conn.Metadata); err != nil {
		conn.Metadata = map[string]any{}
	}
	return conn, nil
}

// ListActivitiesFilter controls which activities ListActivities returns.
type ListActivitiesFilter struct {
	SportType string
	From      time.Time
	To        time.Time
	Cursor    string
	Limit     int
}

// ListActivities returns a page of activities for the given athlete ordered by start_at DESC.
// The returned cursor is non-empty when more pages exist; pass it as filter.Cursor to fetch the next page.
func (r *Repository) ListActivities(ctx context.Context, athleteID string, filter ListActivitiesFilter) ([]domain.Activity, string, error) {
	if athleteID == "" {
		return nil, "", errors.New("athlete id is required")
	}
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	args := []any{athleteID}
	where := []string{"athlete_id = $1", "delete_indicator = false"}

	if filter.SportType != "" {
		args = append(args, filter.SportType)
		where = append(where, fmt.Sprintf("sport_type = $%d", len(args)))
	}
	if !filter.From.IsZero() {
		args = append(args, filter.From.UTC())
		where = append(where, fmt.Sprintf("start_at >= $%d", len(args)))
	}
	if !filter.To.IsZero() {
		args = append(args, filter.To.UTC())
		where = append(where, fmt.Sprintf("start_at < $%d", len(args)))
	}
	if filter.Cursor != "" {
		cursorAt, cursorID, err := decodeCursor(filter.Cursor)
		if err != nil {
			return nil, "", err
		}
		args = append(args, cursorAt.UTC(), cursorID)
		// Matches the ORDER BY start_at DESC, id DESC tiebreak ordering.
		where = append(where, fmt.Sprintf(
			"(start_at < $%d OR (start_at = $%d AND id < $%d::uuid))",
			len(args)-1, len(args)-1, len(args),
		))
	}

	args = append(args, limit+1) // fetch one extra to detect a next page
	query := fmt.Sprintf(`
		SELECT id::text, athlete_id::text, COALESCE(source_activity_ref_id::text, ''),
		       sport_type, COALESCE(name, ''), COALESCE(app_id, ''), COALESCE(status, ''),
		       start_at, end_at, COALESCE(timezone, ''), last_modified_at,
		       elapsed_duration_ms, moving_duration_ms,
		       distance_meters, elevation_gain_meters, elevation_loss_meters,
		       calories, average_heart_rate, max_heart_rate,
		       average_speed_meters_per_second, average_pace_seconds_per_km,
		       indoor, delete_indicator, COALESCE(notes, ''), created_at, updated_at
		FROM activities
		WHERE %s
		ORDER BY start_at DESC, id DESC
		LIMIT $%d`, strings.Join(where, " AND "), len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list activities: %w", err)
	}
	defer rows.Close()

	var activities []domain.Activity
	for rows.Next() {
		a, err := scanActivityRow(rows)
		if err != nil {
			return nil, "", fmt.Errorf("scan activity: %w", err)
		}
		activities = append(activities, a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("list activities rows: %w", err)
	}

	var nextCursor string
	if len(activities) > limit {
		activities = activities[:limit]
		last := activities[len(activities)-1]
		nextCursor = encodeCursor(last.StartAt, last.ID)
	}
	return activities, nextCursor, nil
}

// GetActivity returns a single activity including its laps and tags, but not samples.
// Returns pgx.ErrNoRows wrapped in an error if not found.
func (r *Repository) GetActivity(ctx context.Context, athleteID, id string) (domain.Activity, error) {
	if athleteID == "" {
		return domain.Activity{}, errors.New("athlete id is required")
	}
	if id == "" {
		return domain.Activity{}, errors.New("activity id is required")
	}

	row := r.pool.QueryRow(ctx, `
		SELECT id::text, athlete_id::text, COALESCE(source_activity_ref_id::text, ''),
		       sport_type, COALESCE(name, ''), COALESCE(app_id, ''), COALESCE(status, ''),
		       start_at, end_at, COALESCE(timezone, ''), last_modified_at,
		       elapsed_duration_ms, moving_duration_ms,
		       distance_meters, elevation_gain_meters, elevation_loss_meters,
		       calories, average_heart_rate, max_heart_rate,
		       average_speed_meters_per_second, average_pace_seconds_per_km,
		       indoor, delete_indicator, COALESCE(notes, ''), created_at, updated_at
		FROM activities
		WHERE id = $1::uuid AND athlete_id = $2::uuid AND delete_indicator = false`,
		id, athleteID)

	activity, err := scanActivityRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Activity{}, fmt.Errorf("activity %q not found: %w", id, pgx.ErrNoRows)
	}
	if err != nil {
		return domain.Activity{}, fmt.Errorf("get activity: %w", err)
	}

	laps, err := r.GetActivityLaps(ctx, id)
	if err != nil {
		return domain.Activity{}, err
	}
	activity.Laps = laps

	tags, err := r.getActivityTags(ctx, id)
	if err != nil {
		return domain.Activity{}, err
	}
	activity.Tags = tags

	return activity, nil
}

// GetActivitySamples returns time-series samples for an activity.
// When maxPoints > 0 the result is evenly downsampled to approximately that many points;
// the first and last samples are always included. Pass 0 to return all samples.
func (r *Repository) GetActivitySamples(ctx context.Context, activityID string, maxPoints int) ([]domain.ActivitySample, error) {
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}

	rows, err := r.pool.Query(ctx, `
		WITH numbered AS (
			SELECT id, sample_at, elapsed_ms, latitude, longitude,
			       distance_meters, elevation_meters, heart_rate, speed_meters_per_second,
			       cadence, power_watts, temperature_celsius,
			       ROW_NUMBER() OVER (ORDER BY sample_at) AS rn,
			       COUNT(*)        OVER ()                AS total
			FROM activity_samples
			WHERE activity_id = $1::uuid
		)
		SELECT id, sample_at, elapsed_ms, latitude, longitude,
		       distance_meters, elevation_meters, heart_rate, speed_meters_per_second,
		       cadence, power_watts, temperature_celsius
		FROM numbered
		WHERE $2 <= 0
		   OR total <= $2
		   OR (rn - 1) % GREATEST(total / $2, 1) = 0
		   OR rn = total
		ORDER BY sample_at`,
		activityID, maxPoints)
	if err != nil {
		return nil, fmt.Errorf("query activity samples: %w", err)
	}
	defer rows.Close()

	var samples []domain.ActivitySample
	for rows.Next() {
		s, err := scanSampleRowNoRaw(rows, activityID)
		if err != nil {
			return nil, fmt.Errorf("scan sample: %w", err)
		}
		samples = append(samples, s)
	}
	return samples, rows.Err()
}

// GetActivityLaps returns laps for an activity ordered by lap index.
func (r *Repository) GetActivityLaps(ctx context.Context, activityID string) ([]domain.ActivityLap, error) {
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id::text, lap_index, start_at, end_at, duration_ms,
		       distance_meters, calories, average_heart_rate, max_heart_rate, raw::text
		FROM activity_laps
		WHERE activity_id = $1::uuid
		ORDER BY lap_index`,
		activityID)
	if err != nil {
		return nil, fmt.Errorf("query activity laps: %w", err)
	}
	defer rows.Close()

	var laps []domain.ActivityLap
	for rows.Next() {
		lap, err := scanLapRow(rows, activityID)
		if err != nil {
			return nil, fmt.Errorf("scan lap: %w", err)
		}
		laps = append(laps, lap)
	}
	return laps, rows.Err()
}

func (r *Repository) getActivityTags(ctx context.Context, activityID string) ([]domain.ActivityTag, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT key, COALESCE(value, ''), created_at
		FROM activity_tags
		WHERE activity_id = $1::uuid
		ORDER BY key`,
		activityID)
	if err != nil {
		return nil, fmt.Errorf("query activity tags: %w", err)
	}
	defer rows.Close()

	var tags []domain.ActivityTag
	for rows.Next() {
		tag := domain.ActivityTag{ActivityID: activityID}
		if err := rows.Scan(&tag.Key, &tag.Value, &tag.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// scanner is satisfied by both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanActivityRow(s scanner) (domain.Activity, error) {
	var (
		a          domain.Activity
		sportType  string
		elapsedMs  *int32
		movingMs   *int32
		calories   *int32
	)
	err := s.Scan(
		&a.ID,
		&a.AthleteID,
		&a.SourceActivityRefID,
		&sportType,
		&a.Name,
		&a.AppID,
		&a.Status,
		&a.StartAt,
		&a.EndAt,
		&a.Timezone,
		&a.LastModifiedAt,
		&elapsedMs,
		&movingMs,
		&a.DistanceMeters,
		&a.ElevationGainMeters,
		&a.ElevationLossMeters,
		&calories,
		&a.AverageHeartRate,
		&a.MaxHeartRate,
		&a.AverageSpeedMetersPerSecond,
		&a.AveragePaceSecondsPerKM,
		&a.Indoor,
		&a.DeleteIndicator,
		&a.Notes,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		return domain.Activity{}, err
	}
	a.SportType = domain.SportType(sportType)
	a.ElapsedDuration = int32MillisToDuration(elapsedMs)
	a.MovingDuration = int32MillisToDuration(movingMs)
	if calories != nil {
		c := int(*calories)
		a.Calories = &c
	}
	return a, nil
}

func scanSampleRow(s scanner, activityID string) (domain.ActivitySample, error) {
	var (
		sample    domain.ActivitySample
		elapsedMs *int32
		rawText   string
	)
	err := s.Scan(
		&sample.ID,
		&sample.SampleAt,
		&elapsedMs,
		&sample.Latitude,
		&sample.Longitude,
		&sample.DistanceMeters,
		&sample.ElevationMeters,
		&sample.HeartRate,
		&sample.SpeedMetersPerSecond,
		&sample.Cadence,
		&sample.PowerWatts,
		&sample.TemperatureCelsius,
		&rawText,
	)
	if err != nil {
		return domain.ActivitySample{}, err
	}
	sample.ActivityID = activityID
	sample.Elapsed = int32MillisToDuration(elapsedMs)
	if err := json.Unmarshal([]byte(rawText), &sample.Raw); err != nil {
		sample.Raw = map[string]any{}
	}
	return sample, nil
}

func scanSampleRowNoRaw(s scanner, activityID string) (domain.ActivitySample, error) {
	var (
		sample    domain.ActivitySample
		elapsedMs *int32
	)
	err := s.Scan(
		&sample.ID,
		&sample.SampleAt,
		&elapsedMs,
		&sample.Latitude,
		&sample.Longitude,
		&sample.DistanceMeters,
		&sample.ElevationMeters,
		&sample.HeartRate,
		&sample.SpeedMetersPerSecond,
		&sample.Cadence,
		&sample.PowerWatts,
		&sample.TemperatureCelsius,
	)
	if err != nil {
		return domain.ActivitySample{}, err
	}
	sample.ActivityID = activityID
	sample.Elapsed = int32MillisToDuration(elapsedMs)
	return sample, nil
}

func scanLapRow(s scanner, activityID string) (domain.ActivityLap, error) {
	var (
		lap        domain.ActivityLap
		durationMs *int32
		calories   *int32
		rawText    string
	)
	err := s.Scan(
		&lap.ID,
		&lap.LapIndex,
		&lap.StartAt,
		&lap.EndAt,
		&durationMs,
		&lap.DistanceMeters,
		&calories,
		&lap.AverageHeartRate,
		&lap.MaxHeartRate,
		&rawText,
	)
	if err != nil {
		return domain.ActivityLap{}, err
	}
	lap.ActivityID = activityID
	lap.Duration = int32MillisToDuration(durationMs)
	if calories != nil {
		c := int(*calories)
		lap.Calories = &c
	}
	if err := json.Unmarshal([]byte(rawText), &lap.Raw); err != nil {
		lap.Raw = map[string]any{}
	}
	return lap, nil
}

func int32MillisToDuration(ms *int32) *time.Duration {
	if ms == nil {
		return nil
	}
	d := time.Duration(*ms) * time.Millisecond
	return &d
}

// SimilarActivity is a lightweight summary used for the comparable-efforts card.
type SimilarActivity struct {
	ID             string
	Name           string
	StartAt        time.Time
	DistanceMeters *float64
	ElapsedMs      *int64
	PaceSecsPerKM  *float64
	AvgHeartRate   *float64
}

// ActivityWeather holds cached weather conditions for an activity.
type ActivityWeather struct {
	TemperatureC    *float64
	WeatherCode     *int
	WindSpeedMS     *float64
	PrecipitationMM *float64
}

// GetActivityWeather returns cached weather for an activity, or nil if not yet fetched.
func (r *Repository) GetActivityWeather(ctx context.Context, activityID string) (*ActivityWeather, error) {
	var w ActivityWeather
	err := r.pool.QueryRow(ctx, `
		SELECT temperature_c, weather_code, wind_speed_ms, precipitation_mm
		FROM activity_weather WHERE activity_id = $1::uuid
	`, activityID).Scan(&w.TemperatureC, &w.WeatherCode, &w.WindSpeedMS, &w.PrecipitationMM)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get activity weather: %w", err)
	}
	return &w, nil
}

// UpsertActivityWeather stores fetched weather conditions for an activity.
func (r *Repository) UpsertActivityWeather(ctx context.Context, activityID string, w ActivityWeather) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO activity_weather (activity_id, temperature_c, weather_code, wind_speed_ms, precipitation_mm)
		VALUES ($1::uuid, $2, $3, $4, $5)
		ON CONFLICT (activity_id) DO UPDATE SET
			temperature_c    = EXCLUDED.temperature_c,
			weather_code     = EXCLUDED.weather_code,
			wind_speed_ms    = EXCLUDED.wind_speed_ms,
			precipitation_mm = EXCLUDED.precipitation_mm,
			fetched_at       = now()
	`, activityID, w.TemperatureC, w.WeatherCode, w.WindSpeedMS, w.PrecipitationMM)
	return err
}

// GetActivityStartInfo returns the data needed to fetch weather: start time + GPS center.
// Returns (startAt, centerLat, centerLng, ok).
func (r *Repository) GetActivityStartInfo(ctx context.Context, activityID string) (startAt time.Time, lat, lng float64, ok bool, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT start_at,
		       (bbox_min_lat + bbox_max_lat) / 2,
		       (bbox_min_lng + bbox_max_lng) / 2
		FROM activities
		WHERE id = $1::uuid AND bbox_min_lat IS NOT NULL
	`, activityID).Scan(&startAt, &lat, &lng)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, 0, 0, false, nil
	}
	if err != nil {
		return time.Time{}, 0, 0, false, fmt.Errorf("get activity start info: %w", err)
	}
	return startAt, lat, lng, true, nil
}

// GetSimilarActivities finds activities by the same athlete whose GPS bounding box
// center is within ~1 km of activityID's center and whose distance is within ±20%.
// Returns up to 20 results ordered by date descending, excluding activityID itself.
func (r *Repository) GetSimilarActivities(ctx context.Context, athleteID, activityID string) ([]SimilarActivity, error) {
	rows, err := r.pool.Query(ctx, `
		WITH ref AS (
			SELECT
				(bbox_min_lat + bbox_max_lat) / 2 AS clat,
				(bbox_min_lng + bbox_max_lng) / 2 AS clng,
				distance_meters
			FROM activities
			WHERE id = $2::uuid AND bbox_min_lat IS NOT NULL
		)
		SELECT
			a.id::text, COALESCE(a.name, ''), a.start_at,
			a.distance_meters, a.elapsed_duration_ms,
			a.average_pace_seconds_per_km, a.average_heart_rate
		FROM activities a, ref
		WHERE a.athlete_id  = $1::uuid
		  AND a.id         != $2::uuid
		  AND a.delete_indicator = false
		  AND a.bbox_min_lat IS NOT NULL
		  AND (a.bbox_min_lat + a.bbox_max_lat) / 2 BETWEEN ref.clat - 0.009 AND ref.clat + 0.009
		  AND (a.bbox_min_lng + a.bbox_max_lng) / 2 BETWEEN ref.clng - 0.013 AND ref.clng + 0.013
		  AND a.distance_meters BETWEEN ref.distance_meters * 0.80
		                            AND ref.distance_meters * 1.20
		ORDER BY a.start_at DESC
		LIMIT 20
	`, athleteID, activityID)
	if err != nil {
		return nil, fmt.Errorf("get similar activities: %w", err)
	}
	defer rows.Close()

	var out []SimilarActivity
	for rows.Next() {
		var s SimilarActivity
		if err := rows.Scan(
			&s.ID, &s.Name, &s.StartAt,
			&s.DistanceMeters, &s.ElapsedMs,
			&s.PaceSecsPerKM, &s.AvgHeartRate,
		); err != nil {
			return nil, fmt.Errorf("scan similar activity: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ─── Synced files (folder watch) ─────────────────────────────────────────────

type SyncedFile struct {
	ID                 string
	SourceConnectionID string
	FilePath           string
	SHA256             string
	Status             string
	ErrorMessage       string
	SyncedAt           time.Time
}

func (r *Repository) IsSyncedBySHA256(ctx context.Context, connID, sha256 string) (bool, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM synced_files WHERE source_connection_id = $1::uuid AND sha256 = $2`,
		connID, sha256,
	).Scan(&n)
	return n > 0, err
}

func (r *Repository) UpsertSyncedFile(ctx context.Context, f SyncedFile) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO synced_files (source_connection_id, file_path, sha256, status, error_message)
		VALUES ($1::uuid, $2, $3, $4, $5)
		ON CONFLICT (source_connection_id, sha256) DO UPDATE
		  SET file_path = EXCLUDED.file_path,
		      status = EXCLUDED.status,
		      error_message = EXCLUDED.error_message,
		      synced_at = NOW()
	`, f.SourceConnectionID, f.FilePath, f.SHA256, f.Status, f.ErrorMessage)
	return err
}

func (r *Repository) ListSyncedFiles(ctx context.Context, connID string, limit int) ([]SyncedFile, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, source_connection_id, file_path, sha256, status, COALESCE(error_message, ''), synced_at
		FROM synced_files
		WHERE source_connection_id = $1::uuid
		ORDER BY synced_at DESC
		LIMIT $2
	`, connID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SyncedFile
	for rows.Next() {
		var f SyncedFile
		if err := rows.Scan(&f.ID, &f.SourceConnectionID, &f.FilePath, &f.SHA256, &f.Status, &f.ErrorMessage, &f.SyncedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FindActivityNearTime returns the first activity for the athlete that starts
// within ±60 seconds of startAt with the same sport type, excluding the given
// activity ID (pass "" to skip exclusion). Used for cross-source duplicate detection.
func (r *Repository) FindActivityNearTime(ctx context.Context, athleteID string, startAt time.Time, sportType, excludeID string) *domain.Activity {
	// Use a sargable range predicate so the (athlete_id, sport_type, start_at) index
	// can be used for a range scan instead of a full table scan with function evaluation.
	q := `SELECT id::text, COALESCE(name, ''), sport_type, start_at
	      FROM activities
	      WHERE athlete_id = $1::uuid
	        AND sport_type = $2
	        AND start_at BETWEEN $3::timestamptz - INTERVAL '60 seconds'
	                         AND $3::timestamptz + INTERVAL '60 seconds'
	        AND delete_indicator = false`
	args := []any{athleteID, sportType, startAt}
	if excludeID != "" {
		q += " AND id != $4::uuid"
		args = append(args, excludeID)
	}
	q += " ORDER BY ABS(EXTRACT(EPOCH FROM (start_at - $3::timestamptz))) LIMIT 1"

	row := r.pool.QueryRow(ctx, q, args...)
	var a domain.Activity
	var t time.Time
	if err := row.Scan(&a.ID, &a.Name, &a.SportType, &t); err != nil {
		return nil
	}
	a.StartAt = t
	return &a
}

// DeleteActivity hard-deletes an activity and its source_activity_ref so that
// re-importing the same file is treated as a fresh upload.
// Child rows (samples, laps, tags, gear, weather, best_efforts) are removed
// automatically via ON DELETE CASCADE.
func (r *Repository) DeleteActivity(ctx context.Context, athleteID, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var refID *string
	err = tx.QueryRow(ctx,
		`DELETE FROM activities WHERE id = $1::uuid AND athlete_id = $2::uuid RETURNING source_activity_ref_id::text`,
		id, athleteID,
	).Scan(&refID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("activity not found")
		}
		return err
	}

	if refID != nil {
		_, _ = tx.Exec(ctx, `DELETE FROM source_activity_refs WHERE id = $1::uuid`, *refID)
	}

	return tx.Commit(ctx)
}

// UpdateActivityName sets a new display name for an activity.
func (r *Repository) UpdateActivityName(ctx context.Context, athleteID, id, name string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE activities SET name = $1, updated_at = NOW() WHERE id = $2 AND athlete_id = $3`,
		name, id, athleteID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("activity not found")
	}
	return nil
}

// UpdateActivityNotes saves free-text notes for an activity.
func (r *Repository) UpdateActivityNotes(ctx context.Context, athleteID, id, notes string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE activities SET notes = $1, updated_at = NOW() WHERE id = $2 AND athlete_id = $3`,
		notes, id, athleteID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("activity not found")
	}
	return nil
}

// SetActivityTag upserts a tag key/value for an activity.
func (r *Repository) SetActivityTag(ctx context.Context, athleteID, activityID, key, value string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO activity_tags (activity_id, key, value, created_at)
		SELECT a.id, $3, $4, NOW()
		FROM activities a
		WHERE a.id = $2::uuid AND a.athlete_id = $1::uuid
		ON CONFLICT (activity_id, key) DO UPDATE SET value = EXCLUDED.value`,
		athleteID, activityID, key, value,
	)
	return err
}

// DeleteActivityTag removes a tag by key from an activity.
func (r *Repository) DeleteActivityTag(ctx context.Context, athleteID, activityID, key string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM activity_tags
		WHERE activity_id = (
			SELECT id FROM activities WHERE id = $2::uuid AND athlete_id = $1::uuid
		) AND key = $3`,
		athleteID, activityID, key,
	)
	return err
}
