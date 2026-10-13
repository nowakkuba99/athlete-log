package sqlite

import (
	"context"
	"time"

	"athlete-log/internal/storage"

	"github.com/google/uuid"
)

func (r *Repository) IsSyncedBySHA256(ctx context.Context, connID, sha256 string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM synced_files WHERE source_connection_id = ? AND sha256 = ?`,
		connID, sha256,
	).Scan(&n)
	return n > 0, err
}

func (r *Repository) UpsertSyncedFile(ctx context.Context, f storage.SyncedFile) error {
	id := f.ID
	if id == "" {
		id = uuid.NewString()
	}
	syncedAt := f.SyncedAt
	if syncedAt.IsZero() {
		syncedAt = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO synced_files (
			id, source_connection_id, file_path, sha256, status, error_message, synced_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (source_connection_id, sha256) DO UPDATE SET
			file_path = excluded.file_path,
			status = excluded.status,
			error_message = excluded.error_message,
			synced_at = excluded.synced_at`,
		id,
		f.SourceConnectionID,
		f.FilePath,
		f.SHA256,
		f.Status,
		f.ErrorMessage,
		formatTime(syncedAt),
	)
	return err
}

func (r *Repository) ListSyncedFiles(ctx context.Context, connID string, limit int) ([]storage.SyncedFile, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, source_connection_id, file_path, sha256, status, error_message, synced_at
		FROM synced_files
		WHERE source_connection_id = ?
		ORDER BY synced_at DESC
		LIMIT ?`,
		connID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []storage.SyncedFile
	for rows.Next() {
		var f storage.SyncedFile
		var syncedAtText string
		if err := rows.Scan(
			&f.ID,
			&f.SourceConnectionID,
			&f.FilePath,
			&f.SHA256,
			&f.Status,
			&f.ErrorMessage,
			&syncedAtText,
		); err != nil {
			return nil, err
		}
		syncedAt, err := parseTime(syncedAtText)
		if err != nil {
			return nil, err
		}
		f.SyncedAt = syncedAt
		out = append(out, f)
	}
	return out, rows.Err()
}
