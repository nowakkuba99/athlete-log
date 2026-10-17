package storage

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Backend string

const (
	BackendSQLite   Backend = "sqlite"
	BackendPostgres Backend = "postgres"
)

type Config struct {
	Backend     Backend
	DatabaseURL string
	SQLitePath  string
}

func ConfigFromEnv(getenv func(string) string, dataDir string) (Config, error) {
	if dataDir == "" {
		dataDir = "./data"
	}

	backend := Backend(strings.ToLower(strings.TrimSpace(getenv("APP_DB"))))
	if backend == "" {
		if getenv("DATABASE_URL") != "" {
			backend = BackendPostgres
		} else {
			backend = BackendSQLite
		}
	}

	cfg := Config{Backend: backend}
	switch backend {
	case BackendSQLite:
		cfg.SQLitePath = getenv("SQLITE_PATH")
		if cfg.SQLitePath == "" {
			cfg.SQLitePath = filepath.Join(dataDir, "athlete-log.db")
			legacyPath := filepath.Join(dataDir, "athlete-vault.db")
			if info, err := os.Stat(legacyPath); err == nil && info.Mode().IsRegular() {
				if _, err := os.Stat(cfg.SQLitePath); os.IsNotExist(err) {
					cfg.SQLitePath = legacyPath
				} else if err == nil {
					// A prior start may already have created an empty database under
					// the new name. Reuse the old database only when it has data.
					currentCount, err := sqliteActivityCount(cfg.SQLitePath)
					if err != nil {
						return Config{}, fmt.Errorf("inspect current sqlite database: %w", err)
					}
					if currentCount == 0 {
						legacyCount, err := sqliteActivityCount(legacyPath)
						if err != nil {
							return Config{}, fmt.Errorf("inspect legacy sqlite database: %w", err)
						}
						if legacyCount > 0 {
							cfg.SQLitePath = legacyPath
						}
					}
				}
			}
		}
		if strings.EqualFold(getenv("APP_REQUIRE_EXISTING_DB"), "true") {
			count, err := sqliteActivityCount(cfg.SQLitePath)
			if err != nil {
				return Config{}, fmt.Errorf("required sqlite database %q is unavailable: %w", cfg.SQLitePath, err)
			}
			if count == 0 {
				return Config{}, fmt.Errorf("required sqlite database %q has no activities", cfg.SQLitePath)
			}
		}
	case BackendPostgres:
		cfg.DatabaseURL = getenv("DATABASE_URL")
		if cfg.DatabaseURL == "" {
			cfg.DatabaseURL = "postgres://athlete:athlete@127.0.0.1:5432/athlete_log?sslmode=disable"
		}
	default:
		return Config{}, fmt.Errorf("unsupported APP_DB %q (expected sqlite or postgres)", backend)
	}

	return cfg, nil
}

func sqliteActivityCount(path string) (int, error) {
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM activities WHERE delete_indicator = 0`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
