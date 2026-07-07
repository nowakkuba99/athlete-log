package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"athlete-log/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) EnsureDefaultAthlete(ctx context.Context) (domain.Athlete, error) {
	var athlete domain.Athlete
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, COALESCE(display_name, ''), COALESCE(email, ''), created_at, updated_at
		FROM athletes
		ORDER BY created_at, id
		LIMIT 1
	`).Scan(
		&athlete.ID,
		&athlete.DisplayName,
		&athlete.Email,
		&athlete.CreatedAt,
		&athlete.UpdatedAt,
	)
	if err == nil {
		return athlete, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Athlete{}, fmt.Errorf("query default athlete: %w", err)
	}
	return r.CreateAthlete(ctx, "Local Athlete")
}

func (r *Repository) CreateAthlete(ctx context.Context, displayName string) (domain.Athlete, error) {
	var athlete domain.Athlete
	err := r.pool.QueryRow(ctx, `
		INSERT INTO athletes (display_name)
		VALUES ($1)
		RETURNING id::text, COALESCE(display_name, ''), COALESCE(email, ''), created_at, updated_at
	`, nullString(displayName)).Scan(
		&athlete.ID,
		&athlete.DisplayName,
		&athlete.Email,
		&athlete.CreatedAt,
		&athlete.UpdatedAt,
	)
	if err != nil {
		return domain.Athlete{}, fmt.Errorf("create athlete: %w", err)
	}
	return athlete, nil
}

func (r *Repository) GetAthlete(ctx context.Context, athleteID string) (domain.Athlete, error) {
	var a domain.Athlete
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, COALESCE(display_name,''), COALESCE(email,''),
		       to_char(started_at,'YYYY-MM-DD'), COALESCE(avatar_path,''),
		       created_at, updated_at
		FROM athletes WHERE id = $1::uuid
	`, athleteID).Scan(&a.ID, &a.DisplayName, &a.Email, &a.StartedAt, &a.AvatarPath, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func (r *Repository) UpdateAthlete(ctx context.Context, athleteID, displayName, email, startedAt string) (domain.Athlete, error) {
	var sd *string
	if startedAt != "" {
		sd = &startedAt
	}
	var a domain.Athlete
	err := r.pool.QueryRow(ctx, `
		UPDATE athletes
		SET display_name = $2, email = NULLIF($3,''), started_at = $4::date, updated_at = now()
		WHERE id = $1::uuid
		RETURNING id::text, COALESCE(display_name,''), COALESCE(email,''),
		          to_char(started_at,'YYYY-MM-DD'), COALESCE(avatar_path,''),
		          created_at, updated_at
	`, athleteID, displayName, email, sd).Scan(
		&a.ID, &a.DisplayName, &a.Email, &a.StartedAt, &a.AvatarPath, &a.CreatedAt, &a.UpdatedAt,
	)
	return a, err
}

func (r *Repository) SetAthleteAvatar(ctx context.Context, athleteID, path string) error {
	_, err := r.pool.Exec(ctx, `UPDATE athletes SET avatar_path=$2, updated_at=now() WHERE id=$1::uuid`, athleteID, path)
	return err
}

func (r *Repository) UpsertSourceConnection(ctx context.Context, athleteID string, value domain.SourceConnection) (domain.SourceConnection, error) {
	if athleteID == "" {
		return domain.SourceConnection{}, errors.New("athlete id is required")
	}
	if value.Source == "" {
		return domain.SourceConnection{}, errors.New("source is required")
	}
	if value.Status == "" {
		value.Status = "active"
	}
	metadata, err := jsonForDB(value.Metadata)
	if err != nil {
		return domain.SourceConnection{}, fmt.Errorf("encode source connection metadata: %w", err)
	}

	value.AthleteID = athleteID
	err = r.pool.QueryRow(ctx, `
		INSERT INTO source_connections (
			athlete_id,
			source,
			external_athlete_id,
			status,
			secret_key,
			metadata,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, now())
		ON CONFLICT (athlete_id, source)
		DO UPDATE SET
			external_athlete_id = EXCLUDED.external_athlete_id,
			status = EXCLUDED.status,
			secret_key = EXCLUDED.secret_key,
			metadata = EXCLUDED.metadata,
			updated_at = now()
		RETURNING id::text, status, created_at, updated_at
	`,
		athleteID,
		string(value.Source),
		nullString(value.ExternalAthleteID),
		value.Status,
		nullString(value.SecretKey),
		metadata,
	).Scan(
		&value.ID,
		&value.Status,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	if err != nil {
		return domain.SourceConnection{}, fmt.Errorf("upsert source connection: %w", err)
	}
	return value, nil
}

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

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.SourceActivity{}, fmt.Errorf("begin source activity upsert: %w", err)
	}
	defer tx.Rollback(ctx)

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

	if err := tx.Commit(ctx); err != nil {
		return domain.SourceActivity{}, fmt.Errorf("commit source activity upsert: %w", err)
	}
	return value, nil
}

func upsertSourceActivityRef(ctx context.Context, tx pgx.Tx, ref *domain.SourceActivityRef) error {
	err := tx.QueryRow(ctx, `
		INSERT INTO source_activity_refs (
			athlete_id,
			source_connection_id,
			source,
			source_activity_id,
			source_last_modified_at,
			raw_blob_key,
			raw_sha256,
			tombstone,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (athlete_id, source, source_activity_id)
		DO UPDATE SET
			source_connection_id = EXCLUDED.source_connection_id,
			source_last_modified_at = EXCLUDED.source_last_modified_at,
			raw_blob_key = EXCLUDED.raw_blob_key,
			raw_sha256 = EXCLUDED.raw_sha256,
			tombstone = EXCLUDED.tombstone,
			updated_at = now()
		RETURNING id::text, created_at, updated_at
	`,
		ref.AthleteID,
		nullUUID(ref.SourceConnectionID),
		string(ref.Source),
		ref.SourceActivityID,
		ref.SourceLastModifiedAt,
		nullString(ref.RawBlobKey),
		nullString(ref.RawSHA256),
		ref.Tombstone,
	).Scan(&ref.ID, &ref.CreatedAt, &ref.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert source activity ref: %w", err)
	}
	return nil
}

func upsertActivity(ctx context.Context, tx pgx.Tx, activity *domain.Activity) error {
	err := tx.QueryRow(ctx, `
		INSERT INTO activities (
			athlete_id,
			source_activity_ref_id,
			sport_type,
			name,
			app_id,
			status,
			start_at,
			end_at,
			timezone,
			last_modified_at,
			elapsed_duration_ms,
			moving_duration_ms,
			distance_meters,
			elevation_gain_meters,
			elevation_loss_meters,
			calories,
			average_heart_rate,
			max_heart_rate,
			average_speed_meters_per_second,
			average_pace_seconds_per_km,
			indoor,
			delete_indicator,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, now())
		ON CONFLICT (source_activity_ref_id)
		DO UPDATE SET
			athlete_id = EXCLUDED.athlete_id,
			sport_type = EXCLUDED.sport_type,
			name = EXCLUDED.name,
			app_id = EXCLUDED.app_id,
			status = EXCLUDED.status,
			start_at = EXCLUDED.start_at,
			end_at = EXCLUDED.end_at,
			timezone = EXCLUDED.timezone,
			last_modified_at = EXCLUDED.last_modified_at,
			elapsed_duration_ms = EXCLUDED.elapsed_duration_ms,
			moving_duration_ms = EXCLUDED.moving_duration_ms,
			distance_meters = EXCLUDED.distance_meters,
			elevation_gain_meters = EXCLUDED.elevation_gain_meters,
			elevation_loss_meters = EXCLUDED.elevation_loss_meters,
			calories = EXCLUDED.calories,
			average_heart_rate = EXCLUDED.average_heart_rate,
			max_heart_rate = EXCLUDED.max_heart_rate,
			average_speed_meters_per_second = EXCLUDED.average_speed_meters_per_second,
			average_pace_seconds_per_km = EXCLUDED.average_pace_seconds_per_km,
			indoor = EXCLUDED.indoor,
			delete_indicator = EXCLUDED.delete_indicator,
			updated_at = now()
		RETURNING id::text, created_at, updated_at
	`,
		activity.AthleteID,
		activity.SourceActivityRefID,
		string(activity.SportType),
		nullString(activity.Name),
		nullString(activity.AppID),
		nullString(activity.Status),
		activity.StartAt,
		activity.EndAt,
		nullString(activity.Timezone),
		activity.LastModifiedAt,
		durationMillis(activity.ElapsedDuration),
		durationMillis(activity.MovingDuration),
		activity.DistanceMeters,
		activity.ElevationGainMeters,
		activity.ElevationLossMeters,
		activity.Calories,
		activity.AverageHeartRate,
		activity.MaxHeartRate,
		activity.AverageSpeedMetersPerSecond,
		activity.AveragePaceSecondsPerKM,
		activity.Indoor,
		activity.DeleteIndicator,
	).Scan(&activity.ID, &activity.CreatedAt, &activity.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert activity: %w", err)
	}
	return nil
}

func replaceActivitySamples(ctx context.Context, tx pgx.Tx, activityID string, samples []domain.ActivitySample) error {
	if _, err := tx.Exec(ctx, "DELETE FROM activity_samples WHERE activity_id = $1", activityID); err != nil {
		return fmt.Errorf("delete activity samples: %w", err)
	}
	if len(samples) == 0 {
		return nil
	}
	cols := []string{
		"activity_id", "sample_at", "elapsed_ms",
		"latitude", "longitude", "distance_meters", "elevation_meters",
		"heart_rate", "speed_meters_per_second", "cadence",
		"power_watts", "temperature_celsius", "raw",
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"activity_samples"}, cols,
		pgx.CopyFromSlice(len(samples), func(i int) ([]any, error) {
			s := &samples[i]
			raw, err := jsonForDB(s.Raw)
			if err != nil {
				return nil, fmt.Errorf("encode sample raw: %w", err)
			}
			return []any{
				activityID, s.SampleAt, durationMillis(s.Elapsed),
				s.Latitude, s.Longitude, s.DistanceMeters, s.ElevationMeters,
				s.HeartRate, s.SpeedMetersPerSecond, s.Cadence,
				s.PowerWatts, s.TemperatureCelsius, raw,
			}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("copy activity samples: %w", err)
	}
	// CopyFrom does not support RETURNING; fetch assigned IDs in insertion order.
	rows, err := tx.Query(ctx,
		"SELECT id, activity_id::text FROM activity_samples WHERE activity_id = $1 ORDER BY id ASC",
		activityID)
	if err != nil {
		return fmt.Errorf("fetch sample ids: %w", err)
	}
	defer rows.Close()
	for i := range samples {
		if !rows.Next() {
			break
		}
		if err := rows.Scan(&samples[i].ID, &samples[i].ActivityID); err != nil {
			return fmt.Errorf("scan sample id: %w", err)
		}
	}
	return rows.Err()
}

func replaceActivityLaps(ctx context.Context, tx pgx.Tx, activityID string, laps []domain.ActivityLap) error {
	if _, err := tx.Exec(ctx, "DELETE FROM activity_laps WHERE activity_id = $1", activityID); err != nil {
		return fmt.Errorf("delete activity laps: %w", err)
	}
	if len(laps) == 0 {
		return nil
	}
	cols := []string{
		"activity_id", "lap_index", "start_at", "end_at",
		"duration_ms", "distance_meters", "calories",
		"average_heart_rate", "max_heart_rate", "raw",
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"activity_laps"}, cols,
		pgx.CopyFromSlice(len(laps), func(i int) ([]any, error) {
			l := &laps[i]
			raw, err := jsonForDB(l.Raw)
			if err != nil {
				return nil, fmt.Errorf("encode lap raw: %w", err)
			}
			return []any{
				activityID, l.LapIndex, l.StartAt, l.EndAt,
				durationMillis(l.Duration), l.DistanceMeters, l.Calories,
				l.AverageHeartRate, l.MaxHeartRate, raw,
			}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("copy activity laps: %w", err)
	}
	return nil
}

func replaceActivityTags(ctx context.Context, tx pgx.Tx, activityID string, tags []domain.ActivityTag) error {
	if _, err := tx.Exec(ctx, "DELETE FROM activity_tags WHERE activity_id = $1", activityID); err != nil {
		return fmt.Errorf("delete activity tags: %w", err)
	}
	if len(tags) == 0 {
		return nil
	}
	cols := []string{"activity_id", "key", "value"}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"activity_tags"}, cols,
		pgx.CopyFromSlice(len(tags), func(i int) ([]any, error) {
			t := &tags[i]
			if t.Key == "" {
				return nil, errors.New("activity tag key is required")
			}
			return []any{activityID, t.Key, nullString(t.Value)}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("copy activity tags: %w", err)
	}
	return nil
}

func durationMillis(value *time.Duration) *int64 {
	if value == nil {
		return nil
	}
	millis := value.Milliseconds()
	return &millis
}

func jsonForDB(value map[string]any) (string, error) {
	if value == nil {
		value = map[string]any{}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func nullString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nullUUID(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// GetSetting returns the value for key, or "" if the key does not exist.
func (r *Repository) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := r.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

// SetSetting upserts a key/value pair in app_settings.
func (r *Repository) SetSetting(ctx context.Context, key, value string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, key, value)
	return err
}

// GetHeatmapGeoJSON returns a GeoJSON FeatureCollection of all activity routes for
// the given athlete. Each route is downsampled to at most 50 points.
// Optional filters: sportType, from/to (YYYY-MM-DD).
func (r *Repository) GetHeatmapGeoJSON(ctx context.Context, athleteID, sportType, from, to string) ([]byte, error) {
	q := `
		WITH ranked AS (
			SELECT
				a.id          AS activity_id,
				a.sport_type,
				to_char(a.start_at, 'YYYY-MM-DD') AS start_date,
				s.latitude,
				s.longitude,
				ROW_NUMBER() OVER (PARTITION BY a.id ORDER BY s.elapsed_ms ASC NULLS LAST) AS rn,
				COUNT(*)      OVER (PARTITION BY a.id)                                      AS cnt
			FROM activities a
			JOIN activity_samples s ON s.activity_id = a.id
			WHERE a.athlete_id = $1
			  AND s.latitude  IS NOT NULL
			  AND s.longitude IS NOT NULL
			  AND ($2 = '' OR a.sport_type = $2)
			  AND (NULLIF($3, '')::date IS NULL OR a.start_at >= NULLIF($3, '')::date)
			  AND (NULLIF($4, '')::date IS NULL OR a.start_at <  NULLIF($4, '')::date + INTERVAL '1 day')
		)
		SELECT activity_id, sport_type, start_date, latitude, longitude
		FROM   ranked
		WHERE  cnt <= 500 OR (rn - 1) % GREATEST(1, cnt / 500) = 0
		ORDER  BY activity_id, rn
	`

	rows, err := r.pool.Query(ctx, q, athleteID, sportType, from, to)
	if err != nil {
		return nil, fmt.Errorf("heatmap query: %w", err)
	}
	defer rows.Close()

	type feature struct {
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
		Geometry   struct {
			Type        string      `json:"type"`
			Coordinates [][2]float64 `json:"coordinates"`
		} `json:"geometry"`
	}

	var (
		features    []feature
		cur         *feature
		curID       string
	)

	for rows.Next() {
		var actID, sportT, startDate string
		var lat, lng float64
		if err := rows.Scan(&actID, &sportT, &startDate, &lat, &lng); err != nil {
			return nil, err
		}
		if actID != curID {
			if cur != nil {
				features = append(features, *cur)
			}
			f := feature{
				Type:       "Feature",
				Properties: map[string]any{"sport_type": sportT, "date": startDate},
			}
			f.Geometry.Type = "LineString"
			cur = &f
			curID = actID
		}
		cur.Geometry.Coordinates = append(cur.Geometry.Coordinates, [2]float64{lng, lat})
	}
	if cur != nil && len(cur.Geometry.Coordinates) > 1 {
		features = append(features, *cur)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Filter out single-point "routes" (no line to draw).
	valid := features[:0]
	for _, f := range features {
		if len(f.Geometry.Coordinates) > 1 {
			valid = append(valid, f)
		}
	}

	out := map[string]any{
		"type":     "FeatureCollection",
		"features": valid,
	}
	return json.Marshal(out)
}

// ─── Dashboards ───────────────────────────────────────────────────────────────

type Dashboard struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	WidgetCount int       `json:"widget_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Widget struct {
	ID          string          `json:"id"`
	DashboardID string          `json:"dashboard_id"`
	Type        string          `json:"type"`
	Config      json.RawMessage `json:"config"`
	Position    json.RawMessage `json:"position"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func (r *Repository) CreateDashboard(ctx context.Context, athleteID, name string) (Dashboard, error) {
	var d Dashboard
	err := r.pool.QueryRow(ctx, `
		INSERT INTO dashboards (athlete_id, name)
		VALUES ($1, $2)
		RETURNING id::text, name, 0, created_at, updated_at
	`, athleteID, name).Scan(&d.ID, &d.Name, &d.WidgetCount, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

func (r *Repository) ListDashboards(ctx context.Context, athleteID string) ([]Dashboard, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id::text, d.name,
		       COUNT(w.id) AS widget_count,
		       d.created_at, d.updated_at
		FROM dashboards d
		LEFT JOIN dashboard_widgets w ON w.dashboard_id = d.id
		WHERE d.athlete_id = $1
		GROUP BY d.id
		ORDER BY d.created_at ASC
	`, athleteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Dashboard
	for rows.Next() {
		var d Dashboard
		if err := rows.Scan(&d.ID, &d.Name, &d.WidgetCount, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repository) GetDashboard(ctx context.Context, athleteID, id string) (Dashboard, error) {
	var d Dashboard
	err := r.pool.QueryRow(ctx, `
		SELECT d.id::text, d.name,
		       COUNT(w.id) AS widget_count,
		       d.created_at, d.updated_at
		FROM dashboards d
		LEFT JOIN dashboard_widgets w ON w.dashboard_id = d.id
		WHERE d.athlete_id = $1 AND d.id = $2::uuid
		GROUP BY d.id
	`, athleteID, id).Scan(&d.ID, &d.Name, &d.WidgetCount, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Dashboard{}, fmt.Errorf("dashboard not found")
	}
	return d, err
}

func (r *Repository) UpdateDashboard(ctx context.Context, athleteID, id, name string) (Dashboard, error) {
	var d Dashboard
	err := r.pool.QueryRow(ctx, `
		UPDATE dashboards
		SET name = $3, updated_at = now()
		WHERE athlete_id = $1 AND id = $2::uuid
		RETURNING id::text, name, 0, created_at, updated_at
	`, athleteID, id, name).Scan(&d.ID, &d.Name, &d.WidgetCount, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Dashboard{}, fmt.Errorf("dashboard not found")
	}
	return d, err
}

func (r *Repository) DeleteDashboard(ctx context.Context, athleteID, id string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM dashboards WHERE athlete_id = $1 AND id = $2::uuid
	`, athleteID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("dashboard not found")
	}
	return nil
}

func (r *Repository) CreateWidget(ctx context.Context, dashboardID, athleteID, widgetType string, config, position json.RawMessage) (Widget, error) {
	// Verify dashboard belongs to athlete
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dashboards WHERE id=$1::uuid AND athlete_id=$2)`, dashboardID, athleteID).Scan(&exists)
	if err != nil || !exists {
		return Widget{}, fmt.Errorf("dashboard not found")
	}

	var w Widget
	err = r.pool.QueryRow(ctx, `
		INSERT INTO dashboard_widgets (dashboard_id, type, config, position)
		VALUES ($1::uuid, $2, $3, $4)
		RETURNING id::text, dashboard_id::text, type, config, position, created_at, updated_at
	`, dashboardID, widgetType, config, position).Scan(
		&w.ID, &w.DashboardID, &w.Type, &w.Config, &w.Position, &w.CreatedAt, &w.UpdatedAt,
	)
	return w, err
}

func (r *Repository) ListWidgets(ctx context.Context, dashboardID, athleteID string) ([]Widget, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT w.id::text, w.dashboard_id::text, w.type, w.config, w.position, w.created_at, w.updated_at
		FROM dashboard_widgets w
		JOIN dashboards d ON d.id = w.dashboard_id
		WHERE w.dashboard_id = $1::uuid AND d.athlete_id = $2
		ORDER BY w.created_at ASC
	`, dashboardID, athleteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Widget
	for rows.Next() {
		var w Widget
		if err := rows.Scan(&w.ID, &w.DashboardID, &w.Type, &w.Config, &w.Position, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateWidget(ctx context.Context, widgetID, dashboardID, athleteID string, config, position json.RawMessage) (Widget, error) {
	var w Widget
	err := r.pool.QueryRow(ctx, `
		UPDATE dashboard_widgets w
		SET config = COALESCE($4, w.config),
		    position = COALESCE($5, w.position),
		    updated_at = now()
		FROM dashboards d
		WHERE w.id = $1::uuid
		  AND w.dashboard_id = $2::uuid
		  AND d.id = w.dashboard_id
		  AND d.athlete_id = $3
		RETURNING w.id::text, w.dashboard_id::text, w.type, w.config, w.position, w.created_at, w.updated_at
	`, widgetID, dashboardID, athleteID, nullJSON(config), nullJSON(position)).Scan(
		&w.ID, &w.DashboardID, &w.Type, &w.Config, &w.Position, &w.CreatedAt, &w.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Widget{}, fmt.Errorf("widget not found")
	}
	return w, err
}

func (r *Repository) DeleteWidget(ctx context.Context, widgetID, dashboardID, athleteID string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM dashboard_widgets w
		USING dashboards d
		WHERE w.id = $1::uuid
		  AND w.dashboard_id = $2::uuid
		  AND d.id = w.dashboard_id
		  AND d.athlete_id = $3
	`, widgetID, dashboardID, athleteID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("widget not found")
	}
	return nil
}

// nullJSON returns nil if the slice is empty/null so COALESCE in SQL keeps the old value.
func nullJSON(b json.RawMessage) interface{} {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return b
}

// ─── Best Efforts ─────────────────────────────────────────────────────────────

type BestEffort struct {
	DistanceMeters int       `json:"distance_meters"`
	ElapsedMs      int       `json:"elapsed_ms"`
	ActivityID     string    `json:"activity_id"`
	ActivityName   string    `json:"activity_name"`
	AchievedAt     time.Time `json:"achieved_at"`
}

// ComputeAndStoreBestEfforts computes best efforts for all standard distances
// for a given athlete+sport and upserts them into best_efforts.
func (r *Repository) ComputeAndStoreBestEfforts(ctx context.Context, athleteID, sportType string) error {
	targets := []int{400, 1000, 1609, 5000, 10000, 15000, 21097, 42195}

	// Single query: fetch all samples for this sport type, ordered so the
	// two-pointer can process them without re-querying.
	rows, err := r.pool.Query(ctx, `
		SELECT s.activity_id::text, a.start_at, s.elapsed_ms, s.distance_meters
		FROM activity_samples s
		JOIN activities a ON a.id = s.activity_id
		WHERE a.athlete_id = $1
		  AND a.sport_type = $2
		  AND s.distance_meters IS NOT NULL
		  AND s.elapsed_ms IS NOT NULL
		  AND a.delete_indicator = false
		  AND NOT EXISTS (
		    SELECT 1 FROM activity_tags
		    WHERE activity_id = a.id AND key = 'exclude_from_records'
		  )
		ORDER BY s.activity_id, s.distance_meters
	`, athleteID, sportType)
	if err != nil {
		return fmt.Errorf("fetch samples: %w", err)
	}
	defer rows.Close()

	type sampleRow struct {
		elapsedMs  int64
		distMeters float64
	}
	type actEntry struct {
		startAt time.Time
		samples []sampleRow
	}

	var actOrder []string
	acts := map[string]*actEntry{}

	for rows.Next() {
		var actID string
		var startAt time.Time
		var elapsedMs int64
		var distMeters float64
		if err := rows.Scan(&actID, &startAt, &elapsedMs, &distMeters); err != nil {
			return fmt.Errorf("scan sample: %w", err)
		}
		if _, exists := acts[actID]; !exists {
			acts[actID] = &actEntry{startAt: startAt}
			actOrder = append(actOrder, actID)
		}
		acts[actID].samples = append(acts[actID].samples, sampleRow{elapsedMs, distMeters})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate samples: %w", err)
	}

	type topEntry struct {
		ms    int64
		actID string
		at    time.Time
	}
	type distResult struct {
		dist int
		top  []topEntry // up to 3, sorted ms ASC
	}
	var results []distResult

	// Two-pointer sliding window per target distance — O(n) per activity.
	// Track top-3 fastest efforts (not just the best) so the UI can show progression.
	for _, targetDist := range targets {
		var top3 []topEntry

		for _, actID := range actOrder {
			act := acts[actID]
			ss := act.samples
			actBestMs := int64(-1)
			hi := 0
			for lo := 0; lo < len(ss); lo++ {
				for hi < len(ss) && ss[hi].distMeters-ss[lo].distMeters < float64(targetDist) {
					hi++
				}
				if hi >= len(ss) {
					break
				}
				elapsed := ss[hi].elapsedMs - ss[lo].elapsedMs
				if elapsed > 0 && (actBestMs < 0 || elapsed < actBestMs) {
					actBestMs = elapsed
				}
			}
			if actBestMs < 0 {
				continue
			}
			entry := topEntry{actBestMs, actID, act.startAt}
			if len(top3) < 3 {
				top3 = append(top3, entry)
				sort.Slice(top3, func(i, j int) bool { return top3[i].ms < top3[j].ms })
			} else if actBestMs < top3[2].ms {
				top3[2] = entry
				sort.Slice(top3, func(i, j int) bool { return top3[i].ms < top3[j].ms })
			}
		}

		if len(top3) > 0 {
			results = append(results, distResult{targetDist, top3})
		}
	}

	// Replace all records for this athlete+sport atomically.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM best_efforts WHERE athlete_id = $1 AND sport_type = $2`,
		athleteID, sportType,
	); err != nil {
		return fmt.Errorf("delete best efforts: %w", err)
	}

	for _, res := range results {
		for _, e := range res.top {
			if _, err := tx.Exec(ctx, `
				INSERT INTO best_efforts
					(athlete_id, activity_id, sport_type, distance_meters, elapsed_ms, achieved_at)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (athlete_id, sport_type, distance_meters, elapsed_ms, activity_id)
				DO UPDATE SET achieved_at = EXCLUDED.achieved_at
			`, athleteID, e.actID, sportType, res.dist, e.ms, e.at); err != nil {
				return fmt.Errorf("upsert best effort %dm: %w", res.dist, err)
			}
		}
	}

	return tx.Commit(ctx)
}

// GetBestEfforts returns all stored best efforts for the athlete, grouped by distance.
// For each distance it returns all historical PRs (sorted by elapsed_ms ASC) plus
// the all-time best.
func (r *Repository) GetBestEfforts(ctx context.Context, athleteID, sportType string) ([]BestEffort, error) {
	q := `
		SELECT
			be.distance_meters,
			be.elapsed_ms,
			be.activity_id::text,
			COALESCE(a.name, ''),
			be.achieved_at
		FROM best_efforts be
		JOIN activities a ON a.id = be.activity_id
		WHERE be.athlete_id = $1
		  AND ($2 = '' OR be.sport_type = $2)
		ORDER BY be.distance_meters ASC, be.elapsed_ms ASC
	`

	rows, err := r.pool.Query(ctx, q, athleteID, sportType)
	if err != nil {
		return nil, fmt.Errorf("get best efforts: %w", err)
	}
	defer rows.Close()

	var efforts []BestEffort
	for rows.Next() {
		var e BestEffort
		if err := rows.Scan(&e.DistanceMeters, &e.ElapsedMs, &e.ActivityID, &e.ActivityName, &e.AchievedAt); err != nil {
			return nil, err
		}
		efforts = append(efforts, e)
	}
	return efforts, rows.Err()
}

// ─── Gear ─────────────────────────────────────────────────────────────────────

type Gear struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	Type                 string     `json:"type"`
	BrandModel           string     `json:"brand_model"`
	PurchaseDate         *string    `json:"purchase_date,omitempty"`
	DistanceAlertMeters  *int       `json:"distance_alert_meters,omitempty"`
	RetiredAt            *time.Time `json:"retired_at,omitempty"`
	TotalDistanceMeters  float64    `json:"total_distance_meters"`
	ActivityCount        int        `json:"activity_count"`
	CreatedAt            time.Time  `json:"created_at"`
}

func (r *Repository) CreateGear(ctx context.Context, athleteID, name, gearType, brandModel string, purchaseDate *string, alertMeters *int) (Gear, error) {
	var g Gear
	var pd, bm *string
	if purchaseDate != nil && *purchaseDate != "" {
		pd = purchaseDate
	}
	if brandModel != "" {
		bm = &brandModel
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO gear (athlete_id, name, type, brand_model, purchase_date, distance_alert_meters)
		VALUES ($1, $2, $3, COALESCE($4,''), $5::date, $6)
		RETURNING id::text, name, type, COALESCE(brand_model,''),
		          to_char(purchase_date,'YYYY-MM-DD'), distance_alert_meters, retired_at, 0, 0, created_at
	`, athleteID, name, gearType, bm, pd, alertMeters).Scan(
		&g.ID, &g.Name, &g.Type, &g.BrandModel,
		&g.PurchaseDate, &g.DistanceAlertMeters, &g.RetiredAt,
		&g.TotalDistanceMeters, &g.ActivityCount, &g.CreatedAt,
	)
	return g, err
}

func (r *Repository) ListGear(ctx context.Context, athleteID string, includeRetired bool) ([]Gear, error) {
	q := `
		SELECT g.id::text, g.name, g.type, COALESCE(g.brand_model,''),
		       to_char(g.purchase_date,'YYYY-MM-DD'), g.distance_alert_meters, g.retired_at,
		       COALESCE(SUM(a.distance_meters), 0) AS total_distance,
		       COUNT(DISTINCT ag.activity_id) AS activity_count,
		       g.created_at
		FROM gear g
		LEFT JOIN activity_gear ag ON ag.gear_id = g.id
		LEFT JOIN activities a ON a.id = ag.activity_id
		WHERE g.athlete_id = $1
	`
	if !includeRetired {
		q += " AND g.retired_at IS NULL"
	}
	q += " GROUP BY g.id ORDER BY g.created_at ASC"

	rows, err := r.pool.Query(ctx, q, athleteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Gear
	for rows.Next() {
		var g Gear
		if err := rows.Scan(&g.ID, &g.Name, &g.Type, &g.BrandModel,
			&g.PurchaseDate, &g.DistanceAlertMeters, &g.RetiredAt,
			&g.TotalDistanceMeters, &g.ActivityCount, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateGear(ctx context.Context, athleteID, id, name, gearType, brandModel string, purchaseDate *string, alertMeters *int, retire bool) (Gear, error) {
	var pd *string
	if purchaseDate != nil && *purchaseDate != "" {
		pd = purchaseDate
	}
	var retireExpr string
	if retire {
		retireExpr = "retired_at = now(),"
	} else {
		retireExpr = "retired_at = NULL,"
	}
	q := `UPDATE gear SET name=$3, type=$4, brand_model=COALESCE($5,''), purchase_date=$6::date,
	           distance_alert_meters=$7, ` + retireExpr + ` updated_at=now()
	      WHERE id=$1::uuid AND athlete_id=$2
	      RETURNING id::text, name, type, COALESCE(brand_model,''),
	                to_char(purchase_date,'YYYY-MM-DD'), distance_alert_meters, retired_at, 0, 0, created_at`
	var g Gear
	err := r.pool.QueryRow(ctx, q, id, athleteID, name, gearType, brandModel, pd, alertMeters).Scan(
		&g.ID, &g.Name, &g.Type, &g.BrandModel,
		&g.PurchaseDate, &g.DistanceAlertMeters, &g.RetiredAt,
		&g.TotalDistanceMeters, &g.ActivityCount, &g.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Gear{}, fmt.Errorf("gear not found")
	}
	return g, err
}

func (r *Repository) DeleteGear(ctx context.Context, athleteID, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM gear WHERE id=$1::uuid AND athlete_id=$2`, id, athleteID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("gear not found")
	}
	return nil
}

func (r *Repository) GetActivityGear(ctx context.Context, activityID string) ([]Gear, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT g.id::text, g.name, g.type, COALESCE(g.brand_model,''),
		       to_char(g.purchase_date,'YYYY-MM-DD'), g.distance_alert_meters, g.retired_at, 0, 0, g.created_at
		FROM gear g
		JOIN activity_gear ag ON ag.gear_id = g.id
		WHERE ag.activity_id = $1::uuid
		ORDER BY g.name
	`, activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Gear
	for rows.Next() {
		var g Gear
		if err := rows.Scan(&g.ID, &g.Name, &g.Type, &g.BrandModel,
			&g.PurchaseDate, &g.DistanceAlertMeters, &g.RetiredAt,
			&g.TotalDistanceMeters, &g.ActivityCount, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *Repository) SetActivityGear(ctx context.Context, activityID string, gearIDs []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `DELETE FROM activity_gear WHERE activity_id=$1::uuid`, activityID); err != nil {
		return err
	}
	for _, gid := range gearIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO activity_gear (activity_id, gear_id) VALUES ($1::uuid, $2::uuid) ON CONFLICT DO NOTHING`, activityID, gid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// updateActivityBbox computes the bounding box of an activity's GPS samples and
// stores it on the activity row for efficient route-repeat detection queries.
func updateActivityBbox(ctx context.Context, tx pgx.Tx, activityID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE activities a
		SET
			bbox_min_lat = sub.min_lat,
			bbox_max_lat = sub.max_lat,
			bbox_min_lng = sub.min_lng,
			bbox_max_lng = sub.max_lng
		FROM (
			SELECT MIN(latitude) AS min_lat, MAX(latitude) AS max_lat,
			       MIN(longitude) AS min_lng, MAX(longitude) AS max_lng
			FROM activity_samples
			WHERE activity_id = $1::uuid
			  AND latitude IS NOT NULL AND longitude IS NOT NULL
		) sub
		WHERE a.id = $1::uuid
	`, activityID)
	return err
}
