package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"athlete-log/internal/domain"
	"athlete-log/internal/storage"

	"github.com/google/uuid"
)

const activitySelect = `
	SELECT id, athlete_id, source_activity_ref_id,
	       sport_type, name, app_id, status,
	       start_at, end_at, timezone, last_modified_at,
	       elapsed_duration_ms, moving_duration_ms,
	       distance_meters, elevation_gain_meters, elevation_loss_meters,
	       calories, average_heart_rate, max_heart_rate,
	       average_speed_meters_per_second, average_pace_seconds_per_km,
	       indoor, delete_indicator, notes, created_at, updated_at
	FROM activities`

func (r *Repository) UpsertSourceActivity(ctx context.Context, athleteID string, value domain.SourceActivity) (domain.SourceActivity, error) {
	if athleteID == "" {
		return domain.SourceActivity{}, errors.New("athlete id is required")
	}
	if value.Ref.Source == "" {
		return domain.SourceActivity{}, errors.New("source is required")
	}
	if value.Ref.SourceActivityID == "" {
		return domain.SourceActivity{}, errors.New("source activity id is required")
	}
	if value.Activity.SportType == "" {
		return domain.SourceActivity{}, errors.New("activity sport type is required")
	}
	if value.Activity.StartAt.IsZero() {
		return domain.SourceActivity{}, errors.New("activity start time is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.SourceActivity{}, fmt.Errorf("begin source activity upsert: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	value.Ref.AthleteID = athleteID
	if err := upsertSourceActivityRef(ctx, tx, &value.Ref); err != nil {
		return domain.SourceActivity{}, err
	}

	value.Activity.AthleteID = athleteID
	value.Activity.SourceActivityRefID = value.Ref.ID
	if err := upsertActivity(ctx, tx, &value.Activity); err != nil {
		return domain.SourceActivity{}, err
	}
	if err := replaceActivitySamples(ctx, tx, value.Activity.ID, value.Activity.Samples); err != nil {
		return domain.SourceActivity{}, err
	}
	if err := updateActivityBbox(ctx, tx, value.Activity.ID); err != nil {
		return domain.SourceActivity{}, err
	}
	if err := replaceActivityLaps(ctx, tx, value.Activity.ID, value.Activity.Laps); err != nil {
		return domain.SourceActivity{}, err
	}
	if err := replaceActivityTags(ctx, tx, value.Activity.ID, value.Activity.Tags); err != nil {
		return domain.SourceActivity{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.SourceActivity{}, fmt.Errorf("commit source activity upsert: %w", err)
	}
	return value, nil
}

func upsertSourceActivityRef(ctx context.Context, tx *sql.Tx, ref *domain.SourceActivityRef) error {
	if ref.ID == "" {
		ref.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	var createdAtText, updatedAtText string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO source_activity_refs (
			id, athlete_id, source_connection_id, source, source_activity_id,
			source_last_modified_at, raw_blob_key, raw_sha256, tombstone, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (athlete_id, source, source_activity_id)
		DO UPDATE SET
			source_connection_id = excluded.source_connection_id,
			source_last_modified_at = excluded.source_last_modified_at,
			raw_blob_key = excluded.raw_blob_key,
			raw_sha256 = excluded.raw_sha256,
			tombstone = excluded.tombstone,
			updated_at = excluded.updated_at
		RETURNING id, created_at, updated_at`,
		ref.ID,
		ref.AthleteID,
		nullableString(ref.SourceConnectionID),
		string(ref.Source),
		ref.SourceActivityID,
		nullableTime(ref.SourceLastModifiedAt),
		ref.RawBlobKey,
		ref.RawSHA256,
		boolInt(ref.Tombstone),
		formatTime(now),
		formatTime(now),
	).Scan(&ref.ID, &createdAtText, &updatedAtText)
	if err != nil {
		return fmt.Errorf("upsert source activity ref: %w", err)
	}
	createdAt, err := parseTime(createdAtText)
	if err != nil {
		return err
	}
	updatedAt, err := parseTime(updatedAtText)
	if err != nil {
		return err
	}
	ref.CreatedAt = createdAt
	ref.UpdatedAt = updatedAt
	return nil
}

func upsertActivity(ctx context.Context, tx *sql.Tx, activity *domain.Activity) error {
	if activity.ID == "" {
		activity.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	var createdAtText, updatedAtText string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO activities (
			id, athlete_id, source_activity_ref_id, sport_type, name, app_id, status,
			start_at, end_at, timezone, last_modified_at,
			elapsed_duration_ms, moving_duration_ms,
			distance_meters, elevation_gain_meters, elevation_loss_meters,
			calories, average_heart_rate, max_heart_rate,
			average_speed_meters_per_second, average_pace_seconds_per_km,
			indoor, delete_indicator, notes, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (source_activity_ref_id)
		DO UPDATE SET
			athlete_id = excluded.athlete_id,
			sport_type = excluded.sport_type,
			name = excluded.name,
			app_id = excluded.app_id,
			status = excluded.status,
			start_at = excluded.start_at,
			end_at = excluded.end_at,
			timezone = excluded.timezone,
			last_modified_at = excluded.last_modified_at,
			elapsed_duration_ms = excluded.elapsed_duration_ms,
			moving_duration_ms = excluded.moving_duration_ms,
			distance_meters = excluded.distance_meters,
			elevation_gain_meters = excluded.elevation_gain_meters,
			elevation_loss_meters = excluded.elevation_loss_meters,
			calories = excluded.calories,
			average_heart_rate = excluded.average_heart_rate,
			max_heart_rate = excluded.max_heart_rate,
			average_speed_meters_per_second = excluded.average_speed_meters_per_second,
			average_pace_seconds_per_km = excluded.average_pace_seconds_per_km,
			indoor = excluded.indoor,
			delete_indicator = excluded.delete_indicator,
			notes = excluded.notes,
			updated_at = excluded.updated_at
		RETURNING id, created_at, updated_at`,
		activity.ID,
		activity.AthleteID,
		nullableString(activity.SourceActivityRefID),
		string(activity.SportType),
		activity.Name,
		activity.AppID,
		activity.Status,
		formatTime(activity.StartAt),
		nullableTime(activity.EndAt),
		activity.Timezone,
		nullableTime(activity.LastModifiedAt),
		nullableDurationMillis(activity.ElapsedDuration),
		nullableDurationMillis(activity.MovingDuration),
		activity.DistanceMeters,
		activity.ElevationGainMeters,
		activity.ElevationLossMeters,
		nullableInt(activity.Calories),
		activity.AverageHeartRate,
		activity.MaxHeartRate,
		activity.AverageSpeedMetersPerSecond,
		activity.AveragePaceSecondsPerKM,
		boolInt(activity.Indoor),
		boolInt(activity.DeleteIndicator),
		activity.Notes,
		formatTime(now),
		formatTime(now),
	).Scan(&activity.ID, &createdAtText, &updatedAtText)
	if err != nil {
		return fmt.Errorf("upsert activity: %w", err)
	}
	createdAt, err := parseTime(createdAtText)
	if err != nil {
		return err
	}
	updatedAt, err := parseTime(updatedAtText)
	if err != nil {
		return err
	}
	activity.CreatedAt = createdAt
	activity.UpdatedAt = updatedAt
	return nil
}

func replaceActivitySamples(ctx context.Context, tx *sql.Tx, activityID string, samples []domain.ActivitySample) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM activity_samples WHERE activity_id = ?`, activityID); err != nil {
		return fmt.Errorf("delete activity samples: %w", err)
	}
	for i := range samples {
		s := &samples[i]
		raw, err := jsonForDB(s.Raw)
		if err != nil {
			return fmt.Errorf("encode sample raw: %w", err)
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO activity_samples (
				activity_id, sample_at, elapsed_ms,
				latitude, longitude, distance_meters, elevation_meters,
				heart_rate, speed_meters_per_second, cadence,
				power_watts, temperature_celsius, raw
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			activityID,
			formatTime(s.SampleAt),
			nullableDurationMillis(s.Elapsed),
			s.Latitude,
			s.Longitude,
			s.DistanceMeters,
			s.ElevationMeters,
			s.HeartRate,
			s.SpeedMetersPerSecond,
			s.Cadence,
			s.PowerWatts,
			s.TemperatureCelsius,
			raw,
		)
		if err != nil {
			return fmt.Errorf("insert activity sample: %w", err)
		}
		id, _ := res.LastInsertId()
		s.ID = id
		s.ActivityID = activityID
	}
	return nil
}

func replaceActivityLaps(ctx context.Context, tx *sql.Tx, activityID string, laps []domain.ActivityLap) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM activity_laps WHERE activity_id = ?`, activityID); err != nil {
		return fmt.Errorf("delete activity laps: %w", err)
	}
	for i := range laps {
		l := &laps[i]
		if l.ID == "" {
			l.ID = uuid.NewString()
		}
		raw, err := jsonForDB(l.Raw)
		if err != nil {
			return fmt.Errorf("encode lap raw: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO activity_laps (
				id, activity_id, lap_index, start_at, end_at,
				duration_ms, distance_meters, calories,
				average_heart_rate, max_heart_rate, raw
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			l.ID,
			activityID,
			l.LapIndex,
			formatTime(l.StartAt),
			nullableTime(l.EndAt),
			nullableDurationMillis(l.Duration),
			l.DistanceMeters,
			nullableInt(l.Calories),
			l.AverageHeartRate,
			l.MaxHeartRate,
			raw,
		); err != nil {
			return fmt.Errorf("insert activity lap: %w", err)
		}
		l.ActivityID = activityID
	}
	return nil
}

func replaceActivityTags(ctx context.Context, tx *sql.Tx, activityID string, tags []domain.ActivityTag) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM activity_tags WHERE activity_id = ?`, activityID); err != nil {
		return fmt.Errorf("delete activity tags: %w", err)
	}
	now := formatTime(time.Now().UTC())
	for i := range tags {
		t := &tags[i]
		if t.Key == "" {
			return errors.New("activity tag key is required")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO activity_tags (activity_id, key, value, created_at)
			VALUES (?, ?, ?, ?)`,
			activityID, t.Key, t.Value, now,
		); err != nil {
			return fmt.Errorf("insert activity tag: %w", err)
		}
		t.ActivityID = activityID
		if t.CreatedAt.IsZero() {
			t.CreatedAt = time.Now().UTC()
		}
	}
	return nil
}

func updateActivityBbox(ctx context.Context, tx *sql.Tx, activityID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE activities
		SET
			bbox_min_lat = (SELECT MIN(latitude) FROM activity_samples WHERE activity_id = ? AND latitude IS NOT NULL AND longitude IS NOT NULL),
			bbox_max_lat = (SELECT MAX(latitude) FROM activity_samples WHERE activity_id = ? AND latitude IS NOT NULL AND longitude IS NOT NULL),
			bbox_min_lng = (SELECT MIN(longitude) FROM activity_samples WHERE activity_id = ? AND latitude IS NOT NULL AND longitude IS NOT NULL),
			bbox_max_lng = (SELECT MAX(longitude) FROM activity_samples WHERE activity_id = ? AND latitude IS NOT NULL AND longitude IS NOT NULL)
		WHERE id = ?`,
		activityID, activityID, activityID, activityID, activityID,
	)
	if err != nil {
		return fmt.Errorf("update activity bbox: %w", err)
	}
	return nil
}

func (r *Repository) ListActivities(ctx context.Context, athleteID string, filter storage.ListActivitiesFilter) ([]domain.Activity, string, error) {
	if athleteID == "" {
		return nil, "", errors.New("athlete id is required")
	}
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
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
	if filter.Cursor != "" {
		cursorAt, cursorID, err := decodeCursor(filter.Cursor)
		if err != nil {
			return nil, "", err
		}
		args = append(args, formatTime(cursorAt), formatTime(cursorAt), cursorID)
		where = append(where, "(start_at < ? OR (start_at = ? AND id < ?))")
	}
	args = append(args, limit+1)

	rows, err := r.db.QueryContext(ctx, activitySelect+`
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY start_at DESC, id DESC
		LIMIT ?`, args...)
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

func (r *Repository) GetActivity(ctx context.Context, athleteID, id string) (domain.Activity, error) {
	if athleteID == "" {
		return domain.Activity{}, errors.New("athlete id is required")
	}
	if id == "" {
		return domain.Activity{}, errors.New("activity id is required")
	}

	row := r.db.QueryRowContext(ctx, activitySelect+`
		WHERE id = ? AND athlete_id = ? AND delete_indicator = 0`, id, athleteID)
	activity, err := scanActivityRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Activity{}, fmt.Errorf("activity %q not found: %w", id, sql.ErrNoRows)
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

func (r *Repository) GetActivitySamples(ctx context.Context, activityID string, maxPoints int) ([]domain.ActivitySample, error) {
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, sample_at, elapsed_ms, latitude, longitude,
		       distance_meters, elevation_meters, heart_rate, speed_meters_per_second,
		       cadence, power_watts, temperature_celsius, raw
		FROM activity_samples
		WHERE activity_id = ?
		ORDER BY sample_at`, activityID)
	if err != nil {
		return nil, fmt.Errorf("query activity samples: %w", err)
	}
	defer rows.Close()

	var samples []domain.ActivitySample
	for rows.Next() {
		s, err := scanSampleRow(rows, activityID)
		if err != nil {
			return nil, fmt.Errorf("scan sample: %w", err)
		}
		samples = append(samples, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return downsampleSamples(samples, maxPoints), nil
}

func (r *Repository) GetActivityLaps(ctx context.Context, activityID string) ([]domain.ActivityLap, error) {
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, lap_index, start_at, end_at, duration_ms,
		       distance_meters, calories, average_heart_rate, max_heart_rate, raw
		FROM activity_laps
		WHERE activity_id = ?
		ORDER BY lap_index`, activityID)
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
	rows, err := r.db.QueryContext(ctx, `
		SELECT key, value, created_at
		FROM activity_tags
		WHERE activity_id = ?
		ORDER BY key`, activityID)
	if err != nil {
		return nil, fmt.Errorf("query activity tags: %w", err)
	}
	defer rows.Close()

	var tags []domain.ActivityTag
	for rows.Next() {
		var tag domain.ActivityTag
		var createdAtText string
		tag.ActivityID = activityID
		if err := rows.Scan(&tag.Key, &tag.Value, &createdAtText); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		createdAt, err := parseTime(createdAtText)
		if err != nil {
			return nil, err
		}
		tag.CreatedAt = createdAt
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (r *Repository) FindActivityNearTime(ctx context.Context, athleteID string, startAt time.Time, sportType, excludeID string) *domain.Activity {
	args := []any{
		athleteID,
		sportType,
		formatTime(startAt.Add(-60 * time.Second)),
		formatTime(startAt.Add(60 * time.Second)),
	}
	query := activitySelect + `
		WHERE athlete_id = ?
		  AND sport_type = ?
		  AND start_at BETWEEN ? AND ?
		  AND delete_indicator = 0`
	if excludeID != "" {
		query += " AND id != ?"
		args = append(args, excludeID)
	}
	query += " ORDER BY ABS(julianday(start_at) - julianday(?)) LIMIT 1"
	args = append(args, formatTime(startAt))
	row := r.db.QueryRowContext(ctx, query, args...)
	a, err := scanActivityRow(row)
	if err != nil {
		return nil
	}
	return &a
}

func (r *Repository) DeleteActivity(ctx context.Context, athleteID, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var refID sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT source_activity_ref_id FROM activities WHERE id = ? AND athlete_id = ?`,
		id, athleteID,
	).Scan(&refID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("activity not found")
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM activities WHERE id = ? AND athlete_id = ?`, id, athleteID); err != nil {
		return err
	}
	if refID.Valid {
		if _, err := tx.ExecContext(ctx, `DELETE FROM source_activity_refs WHERE id = ?`, refID.String); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) UpdateActivityName(ctx context.Context, athleteID, id, name string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE activities SET name = ?, updated_at = ? WHERE id = ? AND athlete_id = ?`,
		name, formatTime(time.Now().UTC()), id, athleteID,
	)
	return requireAffected(res, err, "activity not found")
}

func (r *Repository) UpdateActivityNotes(ctx context.Context, athleteID, id, notes string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE activities SET notes = ?, updated_at = ? WHERE id = ? AND athlete_id = ?`,
		notes, formatTime(time.Now().UTC()), id, athleteID,
	)
	return requireAffected(res, err, "activity not found")
}

func (r *Repository) SetActivityTag(ctx context.Context, athleteID, activityID, key, value string) error {
	var exists int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM activities WHERE id = ? AND athlete_id = ?`,
		activityID, athleteID,
	).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return errors.New("activity not found")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO activity_tags (activity_id, key, value, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (activity_id, key) DO UPDATE SET value = excluded.value`,
		activityID, key, value, formatTime(time.Now().UTC()),
	)
	return err
}

func (r *Repository) DeleteActivityTag(ctx context.Context, athleteID, activityID, key string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM activity_tags
		WHERE activity_id = (
			SELECT id FROM activities WHERE id = ? AND athlete_id = ?
		) AND key = ?`,
		activityID, athleteID, key,
	)
	return err
}

func (r *Repository) GetActivityWeather(ctx context.Context, activityID string) (*storage.ActivityWeather, error) {
	var w storage.ActivityWeather
	err := r.db.QueryRowContext(ctx, `
		SELECT temperature_c, weather_code, wind_speed_ms, precipitation_mm
		FROM activity_weather
		WHERE activity_id = ?`, activityID,
	).Scan(&w.TemperatureC, &w.WeatherCode, &w.WindSpeedMS, &w.PrecipitationMM)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get activity weather: %w", err)
	}
	return &w, nil
}

func (r *Repository) UpsertActivityWeather(ctx context.Context, activityID string, w storage.ActivityWeather) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO activity_weather (
			activity_id, fetched_at, temperature_c, weather_code, wind_speed_ms, precipitation_mm
		)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (activity_id) DO UPDATE SET
			fetched_at = excluded.fetched_at,
			temperature_c = excluded.temperature_c,
			weather_code = excluded.weather_code,
			wind_speed_ms = excluded.wind_speed_ms,
			precipitation_mm = excluded.precipitation_mm`,
		activityID,
		formatTime(time.Now().UTC()),
		w.TemperatureC,
		w.WeatherCode,
		w.WindSpeedMS,
		w.PrecipitationMM,
	)
	return err
}

func (r *Repository) GetActivityStartInfo(ctx context.Context, activityID string) (startAt time.Time, lat, lng float64, ok bool, err error) {
	var startAtText string
	err = r.db.QueryRowContext(ctx, `
		SELECT start_at,
		       (bbox_min_lat + bbox_max_lat) / 2,
		       (bbox_min_lng + bbox_max_lng) / 2
		FROM activities
		WHERE id = ? AND bbox_min_lat IS NOT NULL`,
		activityID,
	).Scan(&startAtText, &lat, &lng)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, 0, 0, false, nil
	}
	if err != nil {
		return time.Time{}, 0, 0, false, fmt.Errorf("get activity start info: %w", err)
	}
	startAt, err = parseTime(startAtText)
	if err != nil {
		return time.Time{}, 0, 0, false, err
	}
	return startAt, lat, lng, true, nil
}

func (r *Repository) GetSimilarActivities(ctx context.Context, athleteID, activityID string) ([]storage.SimilarActivity, error) {
	var (
		clat float64
		clng float64
		dist sql.NullFloat64
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT (bbox_min_lat + bbox_max_lat) / 2,
		       (bbox_min_lng + bbox_max_lng) / 2,
		       distance_meters
		FROM activities
		WHERE id = ? AND bbox_min_lat IS NOT NULL`,
		activityID,
	).Scan(&clat, &clng, &dist)
	if errors.Is(err, sql.ErrNoRows) || !dist.Valid {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get similar reference: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, start_at, distance_meters, elapsed_duration_ms,
		       average_pace_seconds_per_km, average_heart_rate
		FROM activities
		WHERE athlete_id = ?
		  AND id != ?
		  AND delete_indicator = 0
		  AND bbox_min_lat IS NOT NULL
		  AND (bbox_min_lat + bbox_max_lat) / 2 BETWEEN ? AND ?
		  AND (bbox_min_lng + bbox_max_lng) / 2 BETWEEN ? AND ?
		  AND distance_meters BETWEEN ? AND ?
		ORDER BY start_at DESC
		LIMIT 20`,
		athleteID,
		activityID,
		clat-0.009,
		clat+0.009,
		clng-0.013,
		clng+0.013,
		dist.Float64*0.80,
		dist.Float64*1.20,
	)
	if err != nil {
		return nil, fmt.Errorf("get similar activities: %w", err)
	}
	defer rows.Close()

	var out []storage.SimilarActivity
	for rows.Next() {
		var s storage.SimilarActivity
		var startAtText string
		if err := rows.Scan(
			&s.ID, &s.Name, &startAtText,
			&s.DistanceMeters, &s.ElapsedMs,
			&s.PaceSecsPerKM, &s.AvgHeartRate,
		); err != nil {
			return nil, fmt.Errorf("scan similar activity: %w", err)
		}
		s.StartAt, err = parseTime(startAtText)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) GetHeatmapGeoJSON(ctx context.Context, athleteID, sportType, from, to string) ([]byte, error) {
	args := []any{athleteID}
	where := []string{"a.athlete_id = ?", "a.delete_indicator = 0", "s.latitude IS NOT NULL", "s.longitude IS NOT NULL"}
	if sportType != "" {
		args = append(args, sportType)
		where = append(where, "a.sport_type = ?")
	}
	if from != "" {
		args = append(args, from)
		where = append(where, "date(a.start_at) >= date(?)")
	}
	if to != "" {
		args = append(args, to)
		where = append(where, "date(a.start_at) <= date(?)")
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.sport_type, substr(a.start_at, 1, 10), s.latitude, s.longitude
		FROM activities a
		JOIN activity_samples s ON s.activity_id = a.id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY a.id, s.sample_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("heatmap query: %w", err)
	}
	defer rows.Close()

	type feature struct {
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
		Geometry   struct {
			Type        string       `json:"type"`
			Coordinates [][2]float64 `json:"coordinates"`
		} `json:"geometry"`
	}

	var (
		features  []feature
		currentID string
		sport     string
		date      string
		coords    [][2]float64
	)
	flush := func() {
		if len(coords) <= 1 {
			return
		}
		f := feature{Type: "Feature", Properties: map[string]any{"sport_type": sport, "date": date}}
		f.Geometry.Type = "LineString"
		f.Geometry.Coordinates = downsampleCoords(coords, 500)
		features = append(features, f)
	}

	for rows.Next() {
		var actID string
		var lat, lng float64
		if err := rows.Scan(&actID, &sport, &date, &lat, &lng); err != nil {
			return nil, err
		}
		if currentID != "" && actID != currentID {
			flush()
			coords = nil
		}
		currentID = actID
		coords = append(coords, [2]float64{lng, lat})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	flush()
	return json.Marshal(map[string]any{"type": "FeatureCollection", "features": features})
}

func scanActivityRow(s scanner) (domain.Activity, error) {
	var (
		a                  domain.Activity
		sourceRef          sql.NullString
		sportType          string
		startAtText        string
		endAtText          sql.NullString
		lastModifiedAtText sql.NullString
		elapsedMs          sql.NullInt64
		movingMs           sql.NullInt64
		calories           sql.NullInt64
		indoor             int
		deleteIndicator    int
		createdAtText      string
		updatedAtText      string
	)
	err := s.Scan(
		&a.ID,
		&a.AthleteID,
		&sourceRef,
		&sportType,
		&a.Name,
		&a.AppID,
		&a.Status,
		&startAtText,
		&endAtText,
		&a.Timezone,
		&lastModifiedAtText,
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
		&indoor,
		&deleteIndicator,
		&a.Notes,
		&createdAtText,
		&updatedAtText,
	)
	if err != nil {
		return domain.Activity{}, err
	}
	if sourceRef.Valid {
		a.SourceActivityRefID = sourceRef.String
	}
	a.SportType = domain.SportType(sportType)
	startAt, err := parseTime(startAtText)
	if err != nil {
		return domain.Activity{}, err
	}
	a.StartAt = startAt
	if endAtText.Valid {
		endAt, err := parseTime(endAtText.String)
		if err != nil {
			return domain.Activity{}, err
		}
		a.EndAt = &endAt
	}
	if lastModifiedAtText.Valid {
		lastModifiedAt, err := parseTime(lastModifiedAtText.String)
		if err != nil {
			return domain.Activity{}, err
		}
		a.LastModifiedAt = &lastModifiedAt
	}
	a.ElapsedDuration = nullMillisToDuration(elapsedMs)
	a.MovingDuration = nullMillisToDuration(movingMs)
	if calories.Valid {
		c := int(calories.Int64)
		a.Calories = &c
	}
	a.Indoor = indoor != 0
	a.DeleteIndicator = deleteIndicator != 0
	createdAt, err := parseTime(createdAtText)
	if err != nil {
		return domain.Activity{}, err
	}
	updatedAt, err := parseTime(updatedAtText)
	if err != nil {
		return domain.Activity{}, err
	}
	a.CreatedAt = createdAt
	a.UpdatedAt = updatedAt
	return a, nil
}

func scanSampleRow(s scanner, activityID string) (domain.ActivitySample, error) {
	var (
		sample       domain.ActivitySample
		sampleAtText string
		elapsedMs    sql.NullInt64
		rawText      string
	)
	err := s.Scan(
		&sample.ID,
		&sampleAtText,
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
	sampleAt, err := parseTime(sampleAtText)
	if err != nil {
		return domain.ActivitySample{}, err
	}
	sample.SampleAt = sampleAt
	sample.Elapsed = nullMillisToDuration(elapsedMs)
	if err := json.Unmarshal([]byte(rawText), &sample.Raw); err != nil {
		sample.Raw = map[string]any{}
	}
	return sample, nil
}

func scanLapRow(s scanner, activityID string) (domain.ActivityLap, error) {
	var (
		lap         domain.ActivityLap
		startAtText string
		endAtText   sql.NullString
		durationMs  sql.NullInt64
		calories    sql.NullInt64
		rawText     string
	)
	err := s.Scan(
		&lap.ID,
		&lap.LapIndex,
		&startAtText,
		&endAtText,
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
	startAt, err := parseTime(startAtText)
	if err != nil {
		return domain.ActivityLap{}, err
	}
	lap.StartAt = startAt
	if endAtText.Valid {
		endAt, err := parseTime(endAtText.String)
		if err != nil {
			return domain.ActivityLap{}, err
		}
		lap.EndAt = &endAt
	}
	lap.Duration = nullMillisToDuration(durationMs)
	if calories.Valid {
		c := int(calories.Int64)
		lap.Calories = &c
	}
	if err := json.Unmarshal([]byte(rawText), &lap.Raw); err != nil {
		lap.Raw = map[string]any{}
	}
	return lap, nil
}

func downsampleSamples(samples []domain.ActivitySample, maxPoints int) []domain.ActivitySample {
	if maxPoints <= 0 || len(samples) <= maxPoints {
		return samples
	}
	if maxPoints == 1 {
		return samples[:1]
	}
	out := make([]domain.ActivitySample, 0, maxPoints)
	step := float64(len(samples)-1) / float64(maxPoints-1)
	lastIdx := -1
	for i := 0; i < maxPoints; i++ {
		idx := int(math.Round(float64(i) * step))
		if idx <= lastIdx {
			continue
		}
		out = append(out, samples[idx])
		lastIdx = idx
	}
	return out
}

func downsampleCoords(coords [][2]float64, maxPoints int) [][2]float64 {
	if maxPoints <= 0 || len(coords) <= maxPoints {
		return coords
	}
	if maxPoints == 1 {
		return coords[:1]
	}
	out := make([][2]float64, 0, maxPoints)
	step := float64(len(coords)-1) / float64(maxPoints-1)
	lastIdx := -1
	for i := 0; i < maxPoints; i++ {
		idx := int(math.Round(float64(i) * step))
		if idx <= lastIdx {
			continue
		}
		out = append(out, coords[idx])
		lastIdx = idx
	}
	return out
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return formatTime(*value)
}

func nullableDurationMillis(value *time.Duration) any {
	if value == nil {
		return nil
	}
	return value.Milliseconds()
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullMillisToDuration(value sql.NullInt64) *time.Duration {
	if !value.Valid {
		return nil
	}
	d := time.Duration(value.Int64) * time.Millisecond
	return &d
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func requireAffected(res sql.Result, err error, notFound string) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New(notFound)
	}
	return nil
}
