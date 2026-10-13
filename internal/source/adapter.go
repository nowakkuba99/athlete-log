package source

import (
	"context"

	"athlete-log/internal/domain"
)

// PullAdapter syncs activities from a remote API into the local store.
// Sync pages through the source, maps each activity to canonical form, and
// calls upsert for each one. It returns the first fatal error; transient
// per-activity failures should be logged and skipped by the implementation.
type PullAdapter interface {
	Source() domain.SourceType
	Sync(ctx context.Context, conn domain.SourceConnection, upsert func(domain.SourceActivity) error) error
}
