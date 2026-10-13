package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"athlete-log/internal/domain"
	"athlete-log/internal/storage"

	"github.com/google/uuid"
)

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

	now := time.Now().UTC()
	job := domain.Job{
		ID:        uuid.NewString(),
		AthleteID: athleteID,
		Type:      jobType,
		Status:    domain.JobQueued,
		Params:    params,
		Result:    map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO jobs (
			id, athlete_id, type, status, params, result, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, '{}', ?, ?)`,
		job.ID,
		athleteID,
		jobType,
		string(domain.JobQueued),
		paramsJSON,
		formatTime(now),
		formatTime(now),
	); err != nil {
		return domain.Job{}, fmt.Errorf("create job: %w", err)
	}
	return job, nil
}

func (r *Repository) GetJob(ctx context.Context, id string) (domain.Job, error) {
	if id == "" {
		return domain.Job{}, errors.New("job id is required")
	}
	row := r.db.QueryRowContext(ctx, jobSelect()+` WHERE id = ?`, id)
	job, err := scanJobRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Job{}, fmt.Errorf("job %q not found: %w", id, sql.ErrNoRows)
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (r *Repository) UpdateJob(ctx context.Context, id string, update storage.JobUpdate) error {
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
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET
			status = ?,
			phase = ?,
			message = ?,
			progress_current = ?,
			progress_total = COALESCE(?, progress_total),
			result = ?,
			last_error = ?,
			artifact_id = COALESCE(?, artifact_id),
			started_at = COALESCE(?, started_at),
			completed_at = COALESCE(?, completed_at),
			updated_at = ?
		WHERE id = ?`,
		string(update.Status),
		update.Phase,
		update.Message,
		update.ProgressCurrent,
		update.ProgressTotal,
		resultJSON,
		update.LastError,
		nullableString(update.ArtifactID),
		nullableTime(update.StartedAt),
		nullableTime(update.CompletedAt),
		formatTime(time.Now().UTC()),
		id,
	)
	return requireAffected(res, err, fmt.Sprintf("job %q not found", id))
}

func (r *Repository) ListJobs(ctx context.Context, athleteID string, limit int) ([]domain.Job, error) {
	if athleteID == "" {
		return nil, errors.New("athlete id is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, jobSelect()+`
		WHERE athlete_id = ?
		ORDER BY created_at DESC
		LIMIT ?`,
		athleteID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	return scanJobRows(rows)
}

func (r *Repository) GetLastJobForSourceConnection(ctx context.Context, athleteID, connID string) (domain.Job, bool, error) {
	rows, err := r.db.QueryContext(ctx, jobSelect()+`
		WHERE athlete_id = ?
		ORDER BY created_at DESC`,
		athleteID,
	)
	if err != nil {
		return domain.Job{}, false, fmt.Errorf("get last job for source: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		job, err := scanJobRow(rows)
		if err != nil {
			return domain.Job{}, false, err
		}
		if value, ok := job.Params["source_connection_id"].(string); ok && value == connID {
			return job, true, nil
		}
	}
	return domain.Job{}, false, rows.Err()
}

func (r *Repository) GetLastJobsForSourceConnections(ctx context.Context, athleteID string, connIDs []string) (map[string]domain.Job, error) {
	if len(connIDs) == 0 {
		return nil, nil
	}
	wanted := make(map[string]struct{}, len(connIDs))
	for _, id := range connIDs {
		wanted[id] = struct{}{}
	}
	rows, err := r.db.QueryContext(ctx, jobSelect()+`
		WHERE athlete_id = ?
		ORDER BY created_at DESC`,
		athleteID,
	)
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
		connID, ok := job.Params["source_connection_id"].(string)
		if !ok {
			continue
		}
		if _, isWanted := wanted[connID]; isWanted {
			if _, exists := out[connID]; !exists {
				out[connID] = job
			}
		}
		if len(out) == len(wanted) {
			break
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
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO job_events (job_id, event_at, level, message, data)
		VALUES (?, ?, ?, ?, ?)`,
		jobID,
		formatTime(time.Now().UTC()),
		level,
		message,
		dataJSON,
	)
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
	if artifact.ID == "" {
		artifact.ID = uuid.NewString()
	}
	artifact.CreatedAt = time.Now().UTC()
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO artifacts (
			id, athlete_id, job_id, activity_id, kind, content_type,
			filename, storage_key, byte_size, checksum, metadata, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		artifact.ID,
		artifact.AthleteID,
		nullableString(artifact.JobID),
		nullableString(artifact.ActivityID),
		artifact.Kind,
		artifact.ContentType,
		artifact.Filename,
		artifact.StorageKey,
		artifact.ByteSize,
		artifact.Checksum,
		metaJSON,
		formatTime(artifact.CreatedAt),
	); err != nil {
		return domain.Artifact{}, fmt.Errorf("create artifact: %w", err)
	}
	return artifact, nil
}

func (r *Repository) GetArtifact(ctx context.Context, id string) (domain.Artifact, error) {
	if id == "" {
		return domain.Artifact{}, errors.New("artifact id is required")
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, athlete_id, job_id, activity_id, kind, content_type,
		       filename, storage_key, byte_size, checksum, metadata, created_at
		FROM artifacts
		WHERE id = ?`,
		id,
	)
	artifact, err := scanArtifactRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Artifact{}, fmt.Errorf("artifact %q not found: %w", id, sql.ErrNoRows)
	}
	if err != nil {
		return domain.Artifact{}, fmt.Errorf("get artifact: %w", err)
	}
	return artifact, nil
}

func jobSelect() string {
	return `
		SELECT id, athlete_id, type, status,
		       phase, message, progress_current, progress_total,
		       attempts, params, result, last_error, artifact_id,
		       created_at, updated_at, started_at, completed_at
		FROM jobs`
}

func scanJobRows(rows *sql.Rows) ([]domain.Job, error) {
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

func scanJobRow(s scanner) (domain.Job, error) {
	var (
		job             domain.Job
		athleteID       sql.NullString
		status          string
		paramsText      string
		resultText      string
		progressTotal   sql.NullInt64
		artifactID      sql.NullString
		createdAtText   string
		updatedAtText   string
		startedAtText   sql.NullString
		completedAtText sql.NullString
	)
	err := s.Scan(
		&job.ID,
		&athleteID,
		&job.Type,
		&status,
		&job.Phase,
		&job.Message,
		&job.ProgressCurrent,
		&progressTotal,
		&job.Attempts,
		&paramsText,
		&resultText,
		&job.LastError,
		&artifactID,
		&createdAtText,
		&updatedAtText,
		&startedAtText,
		&completedAtText,
	)
	if err != nil {
		return domain.Job{}, err
	}
	if athleteID.Valid {
		job.AthleteID = athleteID.String
	}
	job.Status = domain.JobStatus(status)
	if progressTotal.Valid {
		total := int(progressTotal.Int64)
		job.ProgressTotal = &total
	}
	if artifactID.Valid {
		job.ArtifactID = artifactID.String
	}
	createdAt, err := parseTime(createdAtText)
	if err != nil {
		return domain.Job{}, err
	}
	updatedAt, err := parseTime(updatedAtText)
	if err != nil {
		return domain.Job{}, err
	}
	job.CreatedAt = createdAt
	job.UpdatedAt = updatedAt
	if startedAtText.Valid {
		startedAt, err := parseTime(startedAtText.String)
		if err != nil {
			return domain.Job{}, err
		}
		job.StartedAt = &startedAt
	}
	if completedAtText.Valid {
		completedAt, err := parseTime(completedAtText.String)
		if err != nil {
			return domain.Job{}, err
		}
		job.CompletedAt = &completedAt
	}
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
		artifact     domain.Artifact
		athleteID    sql.NullString
		jobID        sql.NullString
		activityID   sql.NullString
		metaText     string
		createdAtTxt string
	)
	err := s.Scan(
		&artifact.ID,
		&athleteID,
		&jobID,
		&activityID,
		&artifact.Kind,
		&artifact.ContentType,
		&artifact.Filename,
		&artifact.StorageKey,
		&artifact.ByteSize,
		&artifact.Checksum,
		&metaText,
		&createdAtTxt,
	)
	if err != nil {
		return domain.Artifact{}, err
	}
	if athleteID.Valid {
		artifact.AthleteID = athleteID.String
	}
	if jobID.Valid {
		artifact.JobID = jobID.String
	}
	if activityID.Valid {
		artifact.ActivityID = activityID.String
	}
	createdAt, err := parseTime(createdAtTxt)
	if err != nil {
		return domain.Artifact{}, err
	}
	artifact.CreatedAt = createdAt
	if err := json.Unmarshal([]byte(metaText), &artifact.Metadata); err != nil {
		artifact.Metadata = map[string]any{}
	}
	return artifact, nil
}
