package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigFromEnvDefaultsToSQLiteWhenNoDatabaseURL(t *testing.T) {
	cfg, err := ConfigFromEnv(func(string) string { return "" }, "/data")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backend != BackendSQLite {
		t.Fatalf("backend = %q, want %q", cfg.Backend, BackendSQLite)
	}
	if cfg.SQLitePath != "/data/athlete-log.db" {
		t.Fatalf("sqlite path = %q", cfg.SQLitePath)
	}
}

func TestConfigFromEnvReusesLegacySQLiteDatabase(t *testing.T) {
	dataDir := t.TempDir()
	legacyPath := filepath.Join(dataDir, "athlete-vault.db")
	if err := os.WriteFile(legacyPath, []byte("existing database"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := ConfigFromEnv(func(string) string { return "" }, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SQLitePath != legacyPath {
		t.Fatalf("sqlite path = %q, want existing legacy database %q", cfg.SQLitePath, legacyPath)
	}
}

func TestConfigFromEnvPrefersCurrentSQLiteDatabase(t *testing.T) {
	dataDir := t.TempDir()
	for _, name := range []string{"athlete-vault.db", "athlete-log.db"} {
		db, err := sql.Open("sqlite", filepath.Join(dataDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TABLE activities (delete_indicator INTEGER NOT NULL DEFAULT 0); INSERT INTO activities VALUES (0)`); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := ConfigFromEnv(func(string) string { return "" }, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataDir, "athlete-log.db")
	if cfg.SQLitePath != want {
		t.Fatalf("sqlite path = %q, want current database %q", cfg.SQLitePath, want)
	}
}

func TestConfigFromEnvDefaultsToPostgresWhenDatabaseURLExists(t *testing.T) {
	cfg, err := ConfigFromEnv(func(key string) string {
		if key == "DATABASE_URL" {
			return "postgres://example"
		}
		return ""
	}, "/data")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backend != BackendPostgres {
		t.Fatalf("backend = %q, want %q", cfg.Backend, BackendPostgres)
	}
	if cfg.DatabaseURL != "postgres://example" {
		t.Fatalf("database url = %q", cfg.DatabaseURL)
	}
}

func TestConfigFromEnvAllowsExplicitBackend(t *testing.T) {
	cfg, err := ConfigFromEnv(func(key string) string {
		switch key {
		case "APP_DB":
			return "sqlite"
		case "SQLITE_PATH":
			return "/custom/athlete-log.db"
		case "DATABASE_URL":
			return "postgres://ignored"
		default:
			return ""
		}
	}, "/data")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backend != BackendSQLite {
		t.Fatalf("backend = %q, want %q", cfg.Backend, BackendSQLite)
	}
	if cfg.SQLitePath != "/custom/athlete-log.db" {
		t.Fatalf("sqlite path = %q", cfg.SQLitePath)
	}
}

func TestConfigFromEnvRejectsUnknownBackend(t *testing.T) {
	_, err := ConfigFromEnv(func(key string) string {
		if key == "APP_DB" {
			return "mysql"
		}
		return ""
	}, "/data")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestConfigFromEnvRequiresPopulatedProductionSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "athlete-log.db")
	getenv := func(key string) string {
		switch key {
		case "APP_DB":
			return "sqlite"
		case "SQLITE_PATH":
			return path
		case "APP_REQUIRE_EXISTING_DB":
			return "true"
		default:
			return ""
		}
	}
	if _, err := ConfigFromEnv(getenv, t.TempDir()); err == nil {
		t.Fatal("missing database was accepted")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE activities (delete_indicator INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigFromEnv(getenv, t.TempDir()); err == nil || !strings.Contains(err.Error(), "has no activities") {
		t.Fatalf("empty database error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO activities VALUES (0)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigFromEnv(getenv, t.TempDir()); err != nil {
		t.Fatalf("populated database rejected: %v", err)
	}
}
