package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

const migrationsDir = "sql"

type SQLRunner interface {
	Exec(context.Context, string, ...any) error
	QueryAppliedMigrations(context.Context) (map[string]bool, error)
	ApplyMigration(context.Context, Migration) error
}

type Migration struct {
	Version string
	Name    string
	SQL     string
}

func Run(ctx context.Context, db SQLRunner, files fs.FS) error {
	if err := ensureSchemaMigrations(ctx, db); err != nil {
		return err
	}

	applied, err := db.QueryAppliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}

	migrations, err := Load(files)
	if err != nil {
		return err
	}

	for _, migration := range migrations {
		if applied[migration.Version] {
			continue
		}
		if err := db.ApplyMigration(ctx, migration); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}
		applied[migration.Version] = true
	}

	return nil
}

func Load(files fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(files, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	migrations := make([]Migration, 0, len(names))
	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return nil, err
		}
		body, err := fs.ReadFile(files, path.Join(migrationsDir, name))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		migrations = append(migrations, Migration{
			Version: version,
			Name:    name,
			SQL:     string(body),
		})
	}

	return migrations, nil
}

func ensureSchemaMigrations(ctx context.Context, db SQLRunner) error {
	err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
	version text PRIMARY KEY,
	name text NOT NULL,
	applied_at timestamptz NOT NULL DEFAULT now()
)`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	return nil
}

func migrationVersion(name string) (string, error) {
	i := strings.IndexByte(name, '_')
	if i <= 0 {
		return "", fmt.Errorf("migration %q must start with a numeric version and underscore", name)
	}
	version := name[:i]
	for _, r := range version {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("migration %q has non-numeric version %q", name, version)
		}
	}
	return version, nil
}
