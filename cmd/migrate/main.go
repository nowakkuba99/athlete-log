package main

import (
	"context"
	"log"
	"os"
	"time"

	"athlete-log/internal/migrate"
	"athlete-log/internal/migrations"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := migrate.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	defer pool.Close()

	if err := migrate.Run(ctx, migrate.PGXRunner{Pool: pool}, migrations.Files); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	log.Print("migrations applied")
}
