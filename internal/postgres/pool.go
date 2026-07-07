package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates a connection pool suitable for the application repository.
// Unlike migrate.Connect it uses the default extended query protocol so pgx
// can cache prepared statement descriptions and avoid re-parsing on every call.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	// Default mode (QueryExecModeCache) — prepared statement descriptions are
	// cached per connection, giving a significant throughput improvement over
	// SimpleProtocol for the high-frequency query paths (ListActivities, GetStats…).
	return pgxpool.NewWithConfig(ctx, config)
}
