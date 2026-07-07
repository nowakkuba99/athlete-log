package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"athlete-log/internal/api"
	"athlete-log/internal/job"
	"athlete-log/internal/migrate"
	"athlete-log/internal/migrations"
	"athlete-log/internal/nrc"
	"athlete-log/internal/postgres"
	"athlete-log/internal/source/strava"
	"athlete-log/internal/sqlite"
	"athlete-log/internal/storage"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	addr := flag.String("addr", envOrDefault("APP_ADDR", "127.0.0.1:8080"), "HTTP server address")
	tokenPath := flag.String("token-file", envOrDefault("TOKEN_FILE", "bearer.token"), "path to the Nike bearer token file")
	staticDir := flag.String("static-dir", os.Getenv("STATIC_DIR"), "path to built frontend assets")
	flag.Parse()

	tokenStore, err := nrc.NewTokenStore(*tokenPath)
	if err != nil {
		slog.Error("load token store", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	dataDir := envOrDefault("DATA_DIR", "./data")
	storageConfig, err := storage.ConfigFromEnv(os.Getenv, dataDir)
	if err != nil {
		slog.Error("load storage config", "error", err)
		os.Exit(1)
	}

	var repo storage.Repository
	var closeRepo func()

	switch storageConfig.Backend {
	case storage.BackendPostgres:
		databaseURL := storageConfig.DatabaseURL
		// Migration pool uses SimpleProtocol to support multi-statement SQL files.
		migratePool, err := migrate.Connect(ctx, databaseURL)
		if err != nil {
			slog.Error("connect postgres (migrate)", "error", err)
			os.Exit(1)
		}
		if err := migrate.Run(ctx, migrate.PGXRunner{Pool: migratePool}, migrations.Files); err != nil {
			slog.Error("run migrations", "error", err)
			os.Exit(1)
		}
		migratePool.Close()

		// App pool uses the extended query protocol for prepared-statement caching.
		pool, err := postgres.NewPool(ctx, databaseURL)
		if err != nil {
			slog.Error("connect postgres (app)", "error", err)
			os.Exit(1)
		}
		repo = postgres.New(pool)
		closeRepo = pool.Close
		slog.Info("storage backend selected", "backend", storageConfig.Backend)
	case storage.BackendSQLite:
		sqliteRepo, err := sqlite.New(storageConfig.SQLitePath)
		if err != nil {
			slog.Error("connect sqlite", "path", storageConfig.SQLitePath, "error", err)
			os.Exit(1)
		}
		repo = sqliteRepo
		closeRepo = func() { _ = sqliteRepo.Close() }
		slog.Info("storage backend selected", "backend", storageConfig.Backend, "path", storageConfig.SQLitePath)
	default:
		slog.Error("unsupported storage backend", "backend", storageConfig.Backend)
		os.Exit(1)
	}
	if closeRepo != nil {
		defer closeRepo()
	}

	runner := job.New(repo)

	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		slog.Error("ensure default athlete", "error", err)
		os.Exit(1)
	}

	nikeClient := nrc.NewNikeClient(tokenStore, &http.Client{Timeout: 30 * time.Second})
	server := api.NewAPIServer(nikeClient, tokenStore).WithRepository(repo, runner, athlete.ID, dataDir)

	if baseURL := os.Getenv("BASE_URL"); baseURL != "" {
		server = server.WithBaseURL(baseURL)
	}

	if stravaClientID := os.Getenv("STRAVA_CLIENT_ID"); stravaClientID != "" {
		stravaAdapter := strava.New(
			strava.Config{ClientID: stravaClientID, ClientSecret: os.Getenv("STRAVA_CLIENT_SECRET")},
			&http.Client{Timeout: 30 * time.Second},
			dataDir,
		)
		server = server.WithStrava(stravaAdapter)
		slog.Info("Strava integration enabled")
	}

	slog.Info("athlete-log API listening", "addr", *addr)
	if *staticDir != "" {
		slog.Info("serving frontend assets", "dir", *staticDir)
	}
	if err := http.ListenAndServe(*addr, server.RoutesWithStatic(*staticDir)); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
