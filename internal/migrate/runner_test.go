package migrate

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"athlete-log/internal/migrations"
)

func TestLoadOrdersSQLMigrations(t *testing.T) {
	files := fstest.MapFS{
		"sql/000002_second.sql": {Data: []byte("SELECT 2;")},
		"sql/notes.txt":         {Data: []byte("ignored")},
		"sql/000001_first.sql":  {Data: []byte("SELECT 1;")},
	}

	migrations, err := Load(files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := len(migrations), 2; got != want {
		t.Fatalf("len(migrations) = %d, want %d", got, want)
	}
	assertMigration(t, migrations[0], "000001", "000001_first.sql", "SELECT 1;")
	assertMigration(t, migrations[1], "000002", "000002_second.sql", "SELECT 2;")
}

func TestRunAppliesOnlyPendingMigrations(t *testing.T) {
	files := fstest.MapFS{
		"sql/000001_first.sql":  {Data: []byte("SELECT 1;")},
		"sql/000002_second.sql": {Data: []byte("SELECT 2;")},
	}
	db := &fakeRunner{applied: map[string]bool{"000001": true}}

	if err := Run(context.Background(), db, files); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got, want := len(db.appliedMigrations), 1; got != want {
		t.Fatalf("applied migrations = %d, want %d", got, want)
	}
	assertMigration(t, db.appliedMigrations[0], "000002", "000002_second.sql", "SELECT 2;")

	if err := Run(context.Background(), db, files); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if got, want := len(db.appliedMigrations), 1; got != want {
		t.Fatalf("applied migrations after second run = %d, want %d", got, want)
	}
}

func TestLoadRejectsInvalidMigrationName(t *testing.T) {
	files := fstest.MapFS{
		"sql/schema.sql": {Data: []byte("SELECT 1;")},
	}

	if _, err := Load(files); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestEmbeddedMigrationsLoad(t *testing.T) {
	migrations, err := Load(migrations.Files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := len(migrations); got == 0 {
		t.Fatalf("len(migrations) = 0, want > 0")
	}
	assertMigrationMeta(t, migrations[0], "000001", "000001_schema_migrations.sql")
	assertMigrationMeta(t, migrations[1], "000002", "000002_initial_schema.sql")
}

func TestInitialSchemaMigrationIncludesPlannedColumns(t *testing.T) {
	embedded, err := Load(migrations.Files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var initialSchema Migration
	for _, migration := range embedded {
		if migration.Version == "000002" {
			initialSchema = migration
			break
		}
	}
	if initialSchema.Version == "" {
		t.Fatal("000002 initial schema migration was not found")
	}

	for _, pattern := range []string{
		"raw_blob_key text",
		"sport_type text NOT NULL",
		"name text",
		"timezone text",
		"moving_duration_ms integer",
		"elevation_gain_meters numeric",
		"average_pace_seconds_per_km numeric",
		"power_watts numeric",
		"temperature_celsius numeric",
		"progress_current integer NOT NULL DEFAULT 0",
		"last_error text",
		"artifact_id uuid REFERENCES artifacts",
	} {
		if !strings.Contains(initialSchema.SQL, pattern) {
			t.Fatalf("initial schema does not contain %q", pattern)
		}
	}
	if strings.Contains(initialSchema.SQL, "credentials jsonb") {
		t.Fatal("initial schema stores source credentials in Postgres")
	}
}

func assertMigrationMeta(t *testing.T, got Migration, version, name string) {
	t.Helper()
	if got.Version != version || got.Name != name {
		t.Fatalf("migration = %#v, want version=%q name=%q", got, version, name)
	}
}

func assertMigration(t *testing.T, got Migration, version, name, sql string) {
	t.Helper()
	if got.Version != version || got.Name != name || got.SQL != sql {
		t.Fatalf("migration = %#v, want version=%q name=%q sql=%q", got, version, name, sql)
	}
}

type fakeRunner struct {
	applied           map[string]bool
	appliedMigrations []Migration
}

func (r *fakeRunner) Exec(context.Context, string, ...any) error {
	if r.applied == nil {
		r.applied = make(map[string]bool)
	}
	return nil
}

func (r *fakeRunner) QueryAppliedMigrations(context.Context) (map[string]bool, error) {
	if r.applied == nil {
		r.applied = make(map[string]bool)
	}
	applied := make(map[string]bool, len(r.applied))
	for version, ok := range r.applied {
		applied[version] = ok
	}
	return applied, nil
}

func (r *fakeRunner) ApplyMigration(_ context.Context, migration Migration) error {
	r.appliedMigrations = append(r.appliedMigrations, migration)
	r.applied[migration.Version] = true
	return nil
}
