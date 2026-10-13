package storage

import "testing"

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
