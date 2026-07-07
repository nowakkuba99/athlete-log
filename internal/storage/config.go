package storage

import (
	"fmt"
	"path/filepath"
	"strings"
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
