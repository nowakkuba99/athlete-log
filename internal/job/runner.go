package job

import (
	"context"
	"sync"

	"athlete-log/internal/storage"
)

// Runner manages background job execution and bridges progress updates to storage.
// Zip bytes are held in memory until CP-10 replaces them with artifact disk storage.
type Runner struct {
	repo     storage.JobRepository
	zipStore sync.Map
}

func New(repo storage.JobRepository) *Runner {
	return &Runner{repo: repo}
}

// Start spawns fn in a goroutine. fn receives a context and an update function it
// should call to report progress; all updates are written immediately.
// If fn returns non-nil bytes they are stored in memory for the download handler.
func (r *Runner) Start(jobID string, fn func(ctx context.Context, update func(storage.JobUpdate)) []byte) {
	go func() {
		zipBytes := fn(context.Background(), func(u storage.JobUpdate) {
			_ = r.repo.UpdateJob(context.Background(), jobID, u)
		})
		if len(zipBytes) > 0 {
			r.zipStore.Store(jobID, zipBytes)
		}
	}()
}

// ZipBytes returns the in-memory zip for a completed job.
// Returns false if the server was restarted since the job completed.
func (r *Runner) ZipBytes(jobID string) ([]byte, bool) {
	v, ok := r.zipStore.Load(jobID)
	if !ok {
		return nil, false
	}
	return v.([]byte), true
}
