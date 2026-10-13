package folder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"athlete-log/internal/domain"
	"athlete-log/internal/source"
	"athlete-log/internal/source/upload"
	"athlete-log/internal/storage"
)

// Watcher monitors a local directory and imports new workout files.
type Watcher struct {
	conn      domain.SourceConnection
	repo      storage.FolderRepository
	athleteID string
	dataDir   string
	stopCh    chan struct{}
}

// New creates a Watcher for the given source connection.
// The connection's metadata must contain a "path" key.
func New(conn domain.SourceConnection, repo storage.FolderRepository, athleteID, dataDir string) *Watcher {
	return &Watcher{
		conn:      conn,
		repo:      repo,
		athleteID: athleteID,
		dataDir:   dataDir,
		stopCh:    make(chan struct{}),
	}
}

// Path returns the directory this watcher monitors.
func (w *Watcher) Path() string {
	if p, ok := w.conn.Metadata["path"].(string); ok {
		return p
	}
	return ""
}

// Stop signals the watcher goroutine to exit.
func (w *Watcher) Stop() {
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
}

// Start begins watching. It scans immediately, then watches via fsnotify
// with a 5-minute polling fallback. Blocks until ctx is cancelled or Stop is called.
func (w *Watcher) Start(ctx context.Context) {
	dir := w.Path()
	if dir == "" {
		slog.Warn("folder_watch: no path configured", "conn_id", w.conn.ID)
		return
	}

	// Initial scan.
	if n := w.Scan(ctx); n > 0 {
		slog.Info("folder_watch: startup import", "conn_id", w.conn.ID, "imported", n)
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("folder_watch: fsnotify unavailable, polling only", "conn_id", w.conn.ID, "error", err)
	} else {
		defer fsw.Close()
		if err := fsw.Add(dir); err != nil {
			slog.Warn("folder_watch: cannot watch dir, polling only", "conn_id", w.conn.ID, "dir", dir, "error", err)
			fsw.Close()
			fsw = nil
		}
	}

	poll := time.NewTicker(5 * time.Minute)
	defer poll.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case event, ok := <-func() <-chan fsnotify.Event {
			if fsw != nil {
				return fsw.Events
			}
			return nil
		}():
			if !ok {
				return
			}
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
				if isSupportedFile(event.Name) {
					w.processFile(ctx, event.Name)
				}
			}
		case <-func() <-chan time.Time {
			if fsw == nil {
				return poll.C
			}
			return nil
		}():
			w.Scan(ctx)
		case <-poll.C:
			// Periodic poll as a safety net even when fsnotify is active,
			// because some network filesystems don't generate events reliably.
			w.Scan(ctx)
		}
	}
}

// Scan walks the directory and processes any files not yet seen.
// Returns the number of newly imported activities.
func (w *Watcher) Scan(ctx context.Context) int {
	dir := w.Path()
	if dir == "" {
		return 0
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Error("folder_watch: cannot read dir", "conn_id", w.conn.ID, "dir", dir, "error", err)
		return 0
	}

	imported := 0
	for _, entry := range entries {
		if entry.IsDir() || !isSupportedFile(entry.Name()) {
			continue
		}
		if w.processFile(ctx, filepath.Join(dir, entry.Name())) {
			imported++
		}
	}
	return imported
}

// processFile imports a single file if it hasn't been seen before.
// Returns true if a new activity was created.
func (w *Watcher) processFile(ctx context.Context, path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Error("folder_watch: read file", "conn_id", w.conn.ID, "path", path, "error", err)
		return false
	}

	sum := sha256.Sum256(data)
	sha256hex := hex.EncodeToString(sum[:])

	already, err := w.repo.IsSyncedBySHA256(ctx, w.conn.ID, sha256hex)
	if err != nil {
		slog.Error("folder_watch: dedup check", "conn_id", w.conn.ID, "path", path, "error", err)
		return false
	}
	if already {
		return false
	}

	sa, err := upload.Parse(ctx, data, filepath.Base(path))
	if err != nil {
		slog.Error("folder_watch: parse file", "conn_id", w.conn.ID, "path", path, "error", err)
		_ = w.repo.UpsertSyncedFile(ctx, storage.SyncedFile{
			SourceConnectionID: w.conn.ID,
			FilePath:           path,
			SHA256:             sha256hex,
			Status:             "error",
			ErrorMessage:       err.Error(),
		})
		return false
	}

	ext := strings.ToLower(filepath.Ext(path))
	blobKey, _, _ := source.WriteBlobRaw(w.dataDir, data, ext)

	if sa.Activity.Name == "" {
		base := filepath.Base(path)
		sa.Activity.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}

	sourceType, _ := upload.SourceTypeForFilename(filepath.Base(path))
	sa.Ref.Source = sourceType
	sa.Ref.SourceConnectionID = w.conn.ID
	sa.Ref.SourceActivityID = sha256hex
	sa.Ref.RawBlobKey = blobKey
	sa.Ref.RawSHA256 = sha256hex

	result, err := w.repo.UpsertSourceActivity(ctx, w.athleteID, sa)
	if err != nil {
		slog.Error("folder_watch: upsert activity", "conn_id", w.conn.ID, "path", path, "error", err)
		_ = w.repo.UpsertSyncedFile(ctx, storage.SyncedFile{
			SourceConnectionID: w.conn.ID,
			FilePath:           path,
			SHA256:             sha256hex,
			Status:             "error",
			ErrorMessage:       fmt.Sprintf("upsert failed: %v", err),
		})
		return false
	}

	created := result.Ref.CreatedAt.Equal(result.Ref.UpdatedAt)
	status := "imported"
	if !created {
		status = "duplicate"
	}

	_ = w.repo.UpsertSyncedFile(ctx, storage.SyncedFile{
		SourceConnectionID: w.conn.ID,
		FilePath:           path,
		SHA256:             sha256hex,
		Status:             status,
	})

	slog.Info("folder_watch: file processed", "conn_id", w.conn.ID, "status", status, "file", filepath.Base(path), "sport", result.Activity.SportType)
	return created
}

func isSupportedFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".fit" || ext == ".gpx" || ext == ".tcx"
}
