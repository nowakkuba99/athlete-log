package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"athlete-log/internal/domain"

	"github.com/jackc/pgx/v5"
)

// JobUpdate carries the new state to apply to a job row.
// All fields are applied on every call — callers set the full desired state.
type JobUpdate struct {
	Status          domain.JobStatus
	Phase           string
	Message         string
	ProgressCurrent int
	ProgressTotal   *int
	Result          map[string]any
	LastError       string
	ArtifactID      string
	StartedAt       *time.Time
	CompletedAt     *time.Time
}

func (r *Repository) CreateJob(ctx context.Context, athleteID, jobType string, params map[string]any) (domain.Job, error) {
	if athleteID == "" {
		return domain.Job{}, errors.New("athlete id is required")
	}
	if jobType == "" {
		return domain.Job{}, errors.New("job type is required")
	}

	paramsJSON, err := jsonForDB(params)
	if err != nil {
		return domain.Job{}, fmt.Errorf("encode job params: %w", err)
	}

	var job domain.Job
	job.AthleteID = athleteID
	job.Type = jobType
	job.Status = domain.JobQueued
	job.Params = params

	err = r.pool.QueryRow(ctx, `
		INSERT INTO jobs (athlete_id, type, status, params)
		VALUES ($1::uuid, $2, $3, $4::jsonb)
		RETURNING id::text, created_at, updated_at`,
		athleteID, jobType, string(domain.JobQueued), paramsJSON,
	).Scan(&job.ID, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return domain.Job{}, fmt.Errorf("create job: %w", err)
	}
	return job, nil
}

func (r *Repository) GetJob(ctx context.Context, id string) (domain.Job, error) {
	if id == "" {
		return domain.Job{}, errors.New("job id is required")
	}

	row := r.pool.QueryRow(ctx, `
		SELECT id::text, COALESCE(athlete_id::text, ''), type, status,
		       COALESCE(phase, ''), COALESCE(message, ''),
		       progress_current, progress_total,
		       attempts, params::text, result::text,
		       COALESCE(last_error, ''), COALESCE(artifact_id::text, ''),
		       created_at, updated_at, started_at, completed_at
		FROM jobs
		WHERE id = $1::uuid`,
		id)

	job, err := scanJobRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, fmt.Errorf("job %q not found: %w", id, pgx.ErrNoRows)
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (r *Repository) UpdateJob(ctx context.Context, id string, update JobUpdate) error {
	if id == "" {
		return errors.New("job id is required")
	}
	if update.Status == "" {
		return errors.New("job status is required")
	}

	resultJSON, err := jsonForDB(update.Result)
	if err != nil {
		return fmt.Errorf("encode job result: %w", err)
	}

	tag, err := r.pool.Exec(ctx, `
		UPDATE jobs SET
			status           = $1,
			phase            = $2,
			message          = $3,
			progress_current = $4,
			progress_total   = COALESCE($5, progress_total),
			result           = $6::jsonb,
			last_error       = $7,
			artifact_id      = COALESCE($8::uuid, artifact_id),
			started_at       = COALESCE($9, started_at),
			completed_at     = COALESCE($10, completed_at),
			updated_at       = now()
		WHERE id = $11::uuid`,
		string(update.Status),
		nullString(update.Phase),
		nullString(update.Message),
		update.ProgressCurrent,
		update.ProgressTotal,
		resultJSON,
		nullString(update.LastError),
		nullUUID(update.ArtifactID),
		update.StartedAt,
		update.CompletedAt,
		id,
	)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("job %q not found", id)
	}
	return nil
}

func (r *Repository) ListJobs(ctx context.Context, athleteID string, limit int) ([]domain.Job, error) {
	if athleteID == "" {
		return nil, errors.New("athlete id is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id::text, COALESCE(athlete_id::text, ''), type, status,
		       COALESCE(phase, ''), COALESCE(message, ''),
		       progress_current, progress_total,
		       attempts, params::text, result::text,
		       COALESCE(last_error, ''), COALESCE(artifact_id::text, ''),
		       created_at, updated_at, started_at, completed_at
		FROM jobs
		WHERE athlete_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT $2`,
		athleteID, limit)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []domain.Job
	for rows.Next() {
		job, err := scanJobRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// GetLastJobForSourceConnection returns the most recent job whose params reference
// the given source_connection_id, or (zero, false, nil) when none exists.
func (r *Repository) GetLastJobForSourceConnection(ctx context.Context, athleteID, connID string) (domain.Job, bool, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, COALESCE(athlete_id::text, ''), type, status,
		       COALESCE(phase, ''), COALESCE(message, ''),
		       progress_current, progress_total,
		       attempts, params::text, result::text,
		       COALESCE(last_error, ''), COALESCE(artifact_id::text, ''),
		       created_at, updated_at, started_at, completed_at
		FROM jobs
		WHERE athlete_id = $1::uuid
		  AND params->>'source_connection_id' = $2
		ORDER BY created_at DESC
		LIMIT 1`,
		athleteID, connID)
	job, err := scanJobRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, false, nil
	}
	if err != nil {
		return domain.Job{}, false, fmt.Errorf("get last job for source: %w", err)
	}
	return job, true, nil
}

// GetLastJobsForSourceConnections returns the most-recent job for each of the
// given source connection IDs in a single query, keyed by connection ID.
func (r *Repository) GetLastJobsForSourceConnections(ctx context.Context, athleteID string, connIDs []string) (map[string]domain.Job, error) {
	if len(connIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (params->>'source_connection_id')
			id::text, COALESCE(athlete_id::text, ''), type, status,
			COALESCE(phase, ''), COALESCE(message, ''),
			progress_current, progress_total,
			attempts, params::text, result::text,
			COALESCE(last_error, ''), COALESCE(artifact_id::text, ''),
			created_at, updated_at, started_at, completed_at
		FROM jobs
		WHERE athlete_id = $1::uuid
		  AND params->>'source_connection_id' = ANY($2)
		ORDER BY params->>'source_connection_id', created_at DESC`,
		athleteID, connIDs)
	if err != nil {
		return nil, fmt.Errorf("get last jobs for sources: %w", err)
	}
	defer rows.Close()

	out := make(map[string]domain.Job, len(connIDs))
	for rows.Next() {
		job, err := scanJobRow(rows)
		if err != nil {
			return nil, err
		}
		if p, ok := job.Params["source_connection_id"].(string); ok {
			out[p] = job
		}
	}
	return out, rows.Err()
}

func (r *Repository) AppendJobEvent(ctx context.Context, jobID, level, message string, data map[string]any) error {
	if jobID == "" {
		return errors.New("job id is required")
	}
	if level == "" {
		level = "info"
	}
	dataJSON, err := jsonForDB(data)
	if err != nil {
		return fmt.Errorf("encode event data: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO job_events (job_id, level, message, data)
		VALUES ($1::uuid, $2, $3, $4::jsonb)`,
		jobID, level, message, dataJSON)
	if err != nil {
		return fmt.Errorf("append job event: %w", err)
	}
	return nil
}

func (r *Repository) CreateArtifact(ctx context.Context, artifact domain.Artifact) (domain.Artifact, error) {
	if artifact.AthleteID == "" {
		return domain.Artifact{}, errors.New("athlete id is required")
	}
	if artifact.Kind == "" {
		return domain.Artifact{}, errors.New("artifact kind is required")
	}
	if artifact.StorageKey == "" {
		return domain.Artifact{}, errors.New("artifact storage key is required")
	}

	metaJSON, err := jsonForDB(artifact.Metadata)
	if err != nil {
		return domain.Artifact{}, fmt.Errorf("encode artifact metadata: %w", err)
	}

	err = r.pool.QueryRow(ctx, `
		INSERT INTO artifacts (
			athlete_id, job_id, activity_id, kind, content_type,
			filename, storage_key, byte_size, checksum, metadata
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $10::jsonb)
		RETURNING id::text, created_at`,
		artifact.AthleteID,
		nullUUID(artifact.JobID),
		nullUUID(artifact.ActivityID),
		artifact.Kind,
		artifact.ContentType,
		nullString(artifact.Filename),
		artifact.StorageKey,
		artifact.ByteSize,
		nullString(artifact.Checksum),
		metaJSON,
	).Scan(&artifact.ID, &artifact.CreatedAt)
	if err != nil {
		return domain.Artifact{}, fmt.Errorf("create artifact: %w", err)
	}
	return artifact, nil
}

func (r *Repository) GetArtifact(ctx context.Context, id string) (domain.Artifact, error) {
	if id == "" {
		return domain.Artifact{}, errors.New("artifact id is required")
	}

	row := r.pool.QueryRow(ctx, `
		SELECT id::text, COALESCE(athlete_id::text, ''), COALESCE(job_id::text, ''),
		       COALESCE(activity_id::text, ''), kind, content_type,
		       COALESCE(filename, ''), storage_key, byte_size,
		       COALESCE(checksum, ''), metadata::text, created_at
		FROM artifacts
		WHERE id = $1::uuid`,
		id)

	artifact, err := scanArtifactRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Artifact{}, fmt.Errorf("artifact %q not found: %w", id, pgx.ErrNoRows)
	}
	if err != nil {
		return domain.Artifact{}, fmt.Errorf("get artifact: %w", err)
	}
	return artifact, nil
}

func scanJobRow(s scanner) (domain.Job, error) {
	var (
		job        domain.Job
		status     string
		paramsText string
		resultText string
	)
	err := s.Scan(
		&job.ID,
		&job.AthleteID,
		&job.Type,
		&status,
		&job.Phase,
		&job.Message,
		&job.ProgressCurrent,
		&job.ProgressTotal,
		&job.Attempts,
		&paramsText,
		&resultText,
		&job.LastError,
		&job.ArtifactID,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.StartedAt,
		&job.CompletedAt,
	)
	if err != nil {
		return domain.Job{}, err
	}
	job.Status = domain.JobStatus(status)
	if err := json.Unmarshal([]byte(paramsText), &job.Params); err != nil {
		job.Params = map[string]any{}
	}
	if err := json.Unmarshal([]byte(resultText), &job.Result); err != nil {
		job.Result = map[string]any{}
	}
	return job, nil
}

func scanArtifactRow(s scanner) (domain.Artifact, error) {
	var (
		artifact domain.Artifact
		metaText string
	)
	err := s.Scan(
		&artifact.ID,
		&artifact.AthleteID,
		&artifact.JobID,
		&artifact.ActivityID,
		&artifact.Kind,
		&artifact.ContentType,
		&artifact.Filename,
		&artifact.StorageKey,
		&artifact.ByteSize,
		&artifact.Checksum,
		&metaText,
		&artifact.CreatedAt,
	)
	if err != nil {
		return domain.Artifact{}, err
	}
	if err := json.Unmarshal([]byte(metaText), &artifact.Metadata); err != nil {
		artifact.Metadata = map[string]any{}
	}
	return artifact, nil
}
