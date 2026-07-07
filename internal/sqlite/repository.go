package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"athlete-log/internal/domain"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Repository struct {
	db *sql.DB
}

func New(path string) (*Repository, error) {
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	r := &Repository{db: db}
	if err := r.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	return r, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) migrate() error {
	_, err := r.db.Exec(`
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS athletes (
  id           TEXT PRIMARY KEY,
  display_name TEXT NOT NULL DEFAULT '',
  email        TEXT NOT NULL DEFAULT '',
  started_at   TEXT,
  avatar_path  TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS app_settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS source_connections (
  id                   TEXT PRIMARY KEY,
  athlete_id           TEXT NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
  source               TEXT NOT NULL,
  external_athlete_id  TEXT NOT NULL DEFAULT '',
  status               TEXT NOT NULL DEFAULT 'active',
  secret_key           TEXT NOT NULL DEFAULT '',
  metadata             TEXT NOT NULL DEFAULT '{}',
  created_at           TEXT NOT NULL,
  updated_at           TEXT NOT NULL,
  UNIQUE (athlete_id, source)
);

CREATE INDEX IF NOT EXISTS idx_source_connections_athlete
  ON source_connections(athlete_id, created_at);

CREATE TABLE IF NOT EXISTS source_activity_refs (
  id                      TEXT PRIMARY KEY,
  athlete_id              TEXT NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
  source_connection_id    TEXT REFERENCES source_connections(id) ON DELETE SET NULL,
  source                  TEXT NOT NULL,
  source_activity_id      TEXT NOT NULL,
  source_last_modified_at TEXT,
  raw_blob_key            TEXT NOT NULL DEFAULT '',
  raw_sha256              TEXT NOT NULL DEFAULT '',
  tombstone               INTEGER NOT NULL DEFAULT 0,
  created_at              TEXT NOT NULL,
  updated_at              TEXT NOT NULL,
  UNIQUE (athlete_id, source, source_activity_id)
);

CREATE INDEX IF NOT EXISTS idx_source_activity_refs_athlete
  ON source_activity_refs(athlete_id);
CREATE INDEX IF NOT EXISTS idx_source_activity_refs_connection
  ON source_activity_refs(source_connection_id);

CREATE TABLE IF NOT EXISTS activities (
  id                                  TEXT PRIMARY KEY,
  athlete_id                          TEXT NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
  source_activity_ref_id              TEXT REFERENCES source_activity_refs(id) ON DELETE SET NULL,
  sport_type                          TEXT NOT NULL,
  name                                TEXT NOT NULL DEFAULT '',
  app_id                              TEXT NOT NULL DEFAULT '',
  status                              TEXT NOT NULL DEFAULT '',
  start_at                            TEXT NOT NULL,
  end_at                              TEXT,
  timezone                            TEXT NOT NULL DEFAULT '',
  last_modified_at                    TEXT,
  elapsed_duration_ms                 INTEGER,
  moving_duration_ms                  INTEGER,
  distance_meters                     REAL,
  elevation_gain_meters               REAL,
  elevation_loss_meters               REAL,
  calories                            INTEGER,
  average_heart_rate                  REAL,
  max_heart_rate                      REAL,
  average_speed_meters_per_second     REAL,
  average_pace_seconds_per_km         REAL,
  indoor                              INTEGER NOT NULL DEFAULT 0,
  delete_indicator                    INTEGER NOT NULL DEFAULT 0,
  notes                               TEXT NOT NULL DEFAULT '',
  bbox_min_lat                        REAL,
  bbox_max_lat                        REAL,
  bbox_min_lng                        REAL,
  bbox_max_lng                        REAL,
  created_at                          TEXT NOT NULL,
  updated_at                          TEXT NOT NULL,
  UNIQUE (source_activity_ref_id)
);

CREATE INDEX IF NOT EXISTS idx_activities_athlete_start_active
  ON activities(athlete_id, start_at DESC)
  WHERE delete_indicator = 0;
CREATE INDEX IF NOT EXISTS idx_activities_athlete_sport_start_active
  ON activities(athlete_id, sport_type, start_at DESC)
  WHERE delete_indicator = 0;
CREATE INDEX IF NOT EXISTS idx_activities_source_ref
  ON activities(source_activity_ref_id);

CREATE TABLE IF NOT EXISTS activity_samples (
  id                              INTEGER PRIMARY KEY AUTOINCREMENT,
  activity_id                     TEXT NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
  sample_at                       TEXT NOT NULL,
  elapsed_ms                      INTEGER,
  latitude                        REAL,
  longitude                       REAL,
  distance_meters                 REAL,
  elevation_meters                REAL,
  heart_rate                      REAL,
  speed_meters_per_second         REAL,
  cadence                         REAL,
  power_watts                     REAL,
  temperature_celsius             REAL,
  raw                             TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_activity_samples_activity_sample_at
  ON activity_samples(activity_id, sample_at);
CREATE INDEX IF NOT EXISTS idx_activity_samples_activity_distance
  ON activity_samples(activity_id, distance_meters)
  WHERE distance_meters IS NOT NULL AND elapsed_ms IS NOT NULL;

CREATE TABLE IF NOT EXISTS activity_laps (
  id                  TEXT PRIMARY KEY,
  activity_id          TEXT NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
  lap_index            INTEGER NOT NULL,
  start_at             TEXT NOT NULL,
  end_at               TEXT,
  duration_ms          INTEGER,
  distance_meters      REAL,
  calories             INTEGER,
  average_heart_rate   REAL,
  max_heart_rate       REAL,
  raw                  TEXT NOT NULL DEFAULT '{}',
  UNIQUE (activity_id, lap_index)
);

CREATE INDEX IF NOT EXISTS idx_activity_laps_activity
  ON activity_laps(activity_id);

CREATE TABLE IF NOT EXISTS activity_tags (
  activity_id TEXT NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
  key         TEXT NOT NULL,
  value       TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  PRIMARY KEY (activity_id, key)
);

CREATE TABLE IF NOT EXISTS best_efforts (
  id               TEXT PRIMARY KEY,
  athlete_id       TEXT NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
  activity_id      TEXT NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
  sport_type       TEXT NOT NULL,
  distance_meters  INTEGER NOT NULL,
  elapsed_ms       INTEGER NOT NULL,
  achieved_at      TEXT NOT NULL,
  created_at       TEXT NOT NULL,
  UNIQUE (athlete_id, sport_type, distance_meters, elapsed_ms, activity_id)
);

CREATE INDEX IF NOT EXISTS idx_best_efforts_athlete_sport
  ON best_efforts(athlete_id, sport_type, distance_meters);

CREATE TABLE IF NOT EXISTS dashboards (
  id          TEXT PRIMARY KEY,
  athlete_id  TEXT NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
  name        TEXT NOT NULL DEFAULT 'Untitled',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_dashboards_athlete
  ON dashboards(athlete_id, created_at);

CREATE TABLE IF NOT EXISTS dashboard_widgets (
  id            TEXT PRIMARY KEY,
  dashboard_id  TEXT NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
  type          TEXT NOT NULL,
  config        TEXT NOT NULL DEFAULT '{}',
  position      TEXT NOT NULL DEFAULT '{"x":0,"y":0,"w":4,"h":3}',
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_dashboard_widgets_dashboard
  ON dashboard_widgets(dashboard_id);

CREATE TABLE IF NOT EXISTS gear (
  id                     TEXT PRIMARY KEY,
  athlete_id             TEXT NOT NULL REFERENCES athletes(id) ON DELETE CASCADE,
  name                   TEXT NOT NULL,
  type                   TEXT NOT NULL DEFAULT 'other',
  brand_model            TEXT NOT NULL DEFAULT '',
  purchase_date          TEXT,
  distance_alert_meters  INTEGER,
  retired_at             TEXT,
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_gear_athlete
  ON gear(athlete_id, retired_at);

CREATE TABLE IF NOT EXISTS activity_gear (
  activity_id  TEXT NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
  gear_id      TEXT NOT NULL REFERENCES gear(id) ON DELETE CASCADE,
  PRIMARY KEY (activity_id, gear_id)
);

CREATE INDEX IF NOT EXISTS idx_activity_gear_gear
  ON activity_gear(gear_id);

CREATE TABLE IF NOT EXISTS jobs (
  id                TEXT PRIMARY KEY,
  athlete_id        TEXT REFERENCES athletes(id) ON DELETE SET NULL,
  type              TEXT NOT NULL,
  status            TEXT NOT NULL,
  phase             TEXT NOT NULL DEFAULT '',
  message           TEXT NOT NULL DEFAULT '',
  progress_current  INTEGER NOT NULL DEFAULT 0,
  progress_total    INTEGER,
  attempts          INTEGER NOT NULL DEFAULT 0,
  params            TEXT NOT NULL DEFAULT '{}',
  result            TEXT NOT NULL DEFAULT '{}',
  last_error        TEXT NOT NULL DEFAULT '',
  artifact_id       TEXT,
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  started_at        TEXT,
  completed_at      TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_athlete_created
  ON jobs(athlete_id, created_at DESC);

CREATE TABLE IF NOT EXISTS job_events (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id    TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  event_at  TEXT NOT NULL,
  level     TEXT NOT NULL DEFAULT 'info',
  message   TEXT NOT NULL,
  data      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_job_events_job_time
  ON job_events(job_id, event_at);

CREATE TABLE IF NOT EXISTS artifacts (
  id            TEXT PRIMARY KEY,
  athlete_id    TEXT REFERENCES athletes(id) ON DELETE SET NULL,
  job_id        TEXT REFERENCES jobs(id) ON DELETE SET NULL,
  activity_id   TEXT REFERENCES activities(id) ON DELETE SET NULL,
  kind          TEXT NOT NULL,
  content_type  TEXT NOT NULL,
  filename      TEXT NOT NULL DEFAULT '',
  storage_key   TEXT NOT NULL,
  byte_size     INTEGER NOT NULL,
  checksum      TEXT NOT NULL DEFAULT '',
  metadata      TEXT NOT NULL DEFAULT '{}',
  created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_artifacts_athlete
  ON artifacts(athlete_id);
CREATE INDEX IF NOT EXISTS idx_artifacts_job
  ON artifacts(job_id);

CREATE TABLE IF NOT EXISTS activity_weather (
  activity_id       TEXT PRIMARY KEY REFERENCES activities(id) ON DELETE CASCADE,
  fetched_at        TEXT NOT NULL,
  temperature_c     REAL,
  weather_code      INTEGER,
  wind_speed_ms     REAL,
  precipitation_mm  REAL
);

CREATE TABLE IF NOT EXISTS synced_files (
  id                    TEXT PRIMARY KEY,
  source_connection_id  TEXT NOT NULL REFERENCES source_connections(id) ON DELETE CASCADE,
  file_path             TEXT NOT NULL,
  sha256                TEXT NOT NULL,
  status                TEXT NOT NULL DEFAULT 'imported',
  error_message         TEXT NOT NULL DEFAULT '',
  synced_at             TEXT NOT NULL,
  UNIQUE (source_connection_id, sha256)
);

CREATE INDEX IF NOT EXISTS idx_synced_files_conn_time
  ON synced_files(source_connection_id, synced_at DESC);
`)
	return err
}

func (r *Repository) EnsureDefaultAthlete(ctx context.Context) (domain.Athlete, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, display_name, email, started_at, avatar_path, created_at, updated_at
		FROM athletes
		ORDER BY created_at, id
		LIMIT 1`)
	athlete, err := scanAthlete(row)
	if err == nil {
		return athlete, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.Athlete{}, fmt.Errorf("query default athlete: %w", err)
	}
	return r.CreateAthlete(ctx, "Local Athlete")
}

func (r *Repository) CreateAthlete(ctx context.Context, displayName string) (domain.Athlete, error) {
	now := time.Now().UTC()
	athlete := domain.Athlete{
		ID:          uuid.NewString(),
		DisplayName: displayName,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO athletes (id, display_name, email, avatar_path, created_at, updated_at)
		VALUES (?, ?, '', '', ?, ?)`,
		athlete.ID, athlete.DisplayName, formatTime(now), formatTime(now),
	); err != nil {
		return domain.Athlete{}, fmt.Errorf("create athlete: %w", err)
	}
	return athlete, nil
}

func (r *Repository) GetAthlete(ctx context.Context, athleteID string) (domain.Athlete, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, display_name, email, started_at, avatar_path, created_at, updated_at
		FROM athletes
		WHERE id = ?`, athleteID)
	return scanAthlete(row)
}

func (r *Repository) UpdateAthlete(ctx context.Context, athleteID, displayName, email, startedAt string) (domain.Athlete, error) {
	var started any
	if startedAt != "" {
		started = startedAt
	}
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
		UPDATE athletes
		SET display_name = ?, email = ?, started_at = ?, updated_at = ?
		WHERE id = ?`,
		displayName, email, started, formatTime(now), athleteID,
	)
	if err != nil {
		return domain.Athlete{}, fmt.Errorf("update athlete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.Athlete{}, sql.ErrNoRows
	}
	return r.GetAthlete(ctx, athleteID)
}

func (r *Repository) SetAthleteAvatar(ctx context.Context, athleteID, path string) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx,
		`UPDATE athletes SET avatar_path = ?, updated_at = ? WHERE id = ?`,
		path, formatTime(now), athleteID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (r *Repository) SetSetting(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO app_settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, formatTime(time.Now().UTC()),
	)
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
	if value.Metadata == nil {
		value.Metadata = map[string]any{}
	}
	metadata, err := json.Marshal(value.Metadata)
	if err != nil {
		return domain.SourceConnection{}, fmt.Errorf("encode source connection metadata: %w", err)
	}

	now := time.Now().UTC()
	id := value.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO source_connections (
			id, athlete_id, source, external_athlete_id, status, secret_key, metadata, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(athlete_id, source) DO UPDATE SET
			external_athlete_id = excluded.external_athlete_id,
			status = excluded.status,
			secret_key = excluded.secret_key,
			metadata = excluded.metadata,
			updated_at = excluded.updated_at`,
		id,
		athleteID,
		string(value.Source),
		value.ExternalAthleteID,
		value.Status,
		value.SecretKey,
		string(metadata),
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return domain.SourceConnection{}, fmt.Errorf("upsert source connection: %w", err)
	}
	return r.getSourceConnectionByAthleteAndSource(ctx, athleteID, value.Source)
}

func (r *Repository) GetSourceConnection(ctx context.Context, id string) (domain.SourceConnection, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, athlete_id, source, external_athlete_id, status, secret_key, metadata, created_at, updated_at
		FROM source_connections
		WHERE id = ?`, id)
	return scanSourceConnection(row)
}

func (r *Repository) ListSourceConnections(ctx context.Context, athleteID string) ([]domain.SourceConnection, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, athlete_id, source, external_athlete_id, status, secret_key, metadata, created_at, updated_at
		FROM source_connections
		WHERE athlete_id = ?
		ORDER BY created_at`, athleteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSourceConnections(rows)
}

func (r *Repository) ListSourceConnectionsByType(ctx context.Context, athleteID string, sourceType domain.SourceType) ([]domain.SourceConnection, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, athlete_id, source, external_athlete_id, status, secret_key, metadata, created_at, updated_at
		FROM source_connections
		WHERE athlete_id = ? AND source = ?
		ORDER BY created_at`, athleteID, string(sourceType))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSourceConnections(rows)
}

func (r *Repository) DeleteSourceConnection(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM source_connections WHERE id = ?`, id)
	return err
}

func (r *Repository) getSourceConnectionByAthleteAndSource(ctx context.Context, athleteID string, source domain.SourceType) (domain.SourceConnection, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, athlete_id, source, external_athlete_id, status, secret_key, metadata, created_at, updated_at
		FROM source_connections
		WHERE athlete_id = ? AND source = ?`, athleteID, string(source))
	return scanSourceConnection(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAthlete(s scanner) (domain.Athlete, error) {
	var (
		a             domain.Athlete
		startedAt     sql.NullString
		createdAtText string
		updatedAtText string
	)
	if err := s.Scan(
		&a.ID,
		&a.DisplayName,
		&a.Email,
		&startedAt,
		&a.AvatarPath,
		&createdAtText,
		&updatedAtText,
	); err != nil {
		return domain.Athlete{}, err
	}
	if startedAt.Valid {
		a.StartedAt = &startedAt.String
	}
	createdAt, err := parseTime(createdAtText)
	if err != nil {
		return domain.Athlete{}, err
	}
	updatedAt, err := parseTime(updatedAtText)
	if err != nil {
		return domain.Athlete{}, err
	}
	a.CreatedAt = createdAt
	a.UpdatedAt = updatedAt
	return a, nil
}

func scanSourceConnection(s scanner) (domain.SourceConnection, error) {
	var (
		conn          domain.SourceConnection
		source        string
		metadata      string
		createdAtText string
		updatedAtText string
	)
	if err := s.Scan(
		&conn.ID,
		&conn.AthleteID,
		&source,
		&conn.ExternalAthleteID,
		&conn.Status,
		&conn.SecretKey,
		&metadata,
		&createdAtText,
		&updatedAtText,
	); err != nil {
		return domain.SourceConnection{}, err
	}
	conn.Source = domain.SourceType(source)
	if err := json.Unmarshal([]byte(metadata), &conn.Metadata); err != nil {
		conn.Metadata = map[string]any{}
	}
	createdAt, err := parseTime(createdAtText)
	if err != nil {
		return domain.SourceConnection{}, err
	}
	updatedAt, err := parseTime(updatedAtText)
	if err != nil {
		return domain.SourceConnection{}, err
	}
	conn.CreatedAt = createdAt
	conn.UpdatedAt = updatedAt
	return conn, nil
}

func scanSourceConnections(rows *sql.Rows) ([]domain.SourceConnection, error) {
	var out []domain.SourceConnection
	for rows.Next() {
		conn, err := scanSourceConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, conn)
	}
	return out, rows.Err()
}

func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
