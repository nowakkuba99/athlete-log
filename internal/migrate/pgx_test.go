package migrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"athlete-log/internal/migrations"
)

func TestRunWithPostgresIsIdempotent(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	embeddedMigrations, err := Load(migrations.Files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminPool, err := Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer adminPool.Close()

	schema := "migration_test_" + randomHex(t, 6)
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+quoteIdent(schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer adminPool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+quoteIdent(schema)+` CASCADE`)

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	defer pool.Close()

	runner := PGXRunner{Pool: pool}
	if err := Run(ctx, runner, migrations.Files); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := Run(ctx, runner, migrations.Files); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != len(embeddedMigrations) {
		t.Fatalf("schema_migrations rows = %d, want %d", count, len(embeddedMigrations))
	}

	for _, table := range []string{
		"athletes",
		"source_connections",
		"source_activity_refs",
		"activities",
		"activity_samples",
		"activity_laps",
		"activity_tags",
		"jobs",
		"job_events",
		"artifacts",
	} {
		assertTableExists(ctx, t, pool, schema, table)
	}
}

func assertTableExists(ctx context.Context, t *testing.T, pool *pgxpool.Pool, schema string, table string) {
	t.Helper()

	var exists bool
	err := pool.QueryRow(ctx, `
SELECT EXISTS (
	SELECT 1
	FROM information_schema.tables
	WHERE table_schema = $1
		AND table_name = $2
		AND table_type = 'BASE TABLE'
)`, schema, table).Scan(&exists)
	if err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	if !exists {
		t.Fatalf("table %s does not exist", table)
	}
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func quoteIdent(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
