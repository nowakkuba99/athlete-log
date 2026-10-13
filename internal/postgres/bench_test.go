package postgres

// Performance benchmarks for the heavy repository paths.
// All benchmarks require a real Postgres instance and are skipped when
// DATABASE_URL is unset, so they never block CI.
//
// Run with:
//   DATABASE_URL=... go test -bench=. -benchmem -run=^$ ./internal/postgres/...

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"athlete-log/internal/domain"
	"athlete-log/internal/migrate"
	"athlete-log/internal/migrations"
)

func newBenchRepo(b *testing.B) (*Repository, func()) {
	b.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		b.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := migrate.Connect(ctx, url)
	if err != nil {
		b.Fatalf("connect: %v", err)
	}
	if err := migrate.Run(ctx, migrate.PGXRunner{Pool: pool}, migrations.Files); err != nil {
		b.Fatalf("migrate: %v", err)
	}
	repo := New(pool)
	cleanup := func() {
		_, _ = pool.Exec(ctx, `
			DELETE FROM activities
			WHERE source_activity_ref_id IN (
				SELECT id FROM source_activity_refs
				WHERE source_activity_id LIKE 'bench-%'
			)`)
		_, _ = pool.Exec(ctx, `
			DELETE FROM source_activity_refs
			WHERE source_activity_id LIKE 'bench-%'`)
		pool.Close()
	}
	return repo, cleanup
}

// BenchmarkUpsertSourceActivity measures the full write path including
// CopyFrom for samples. Seeded with a realistic 500-sample activity.
func BenchmarkUpsertSourceActivity(b *testing.B) {
	repo, cleanup := newBenchRepo(b)
	defer cleanup()

	ctx := context.Background()
	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		b.Fatal(err)
	}

	samples := make([]domain.ActivitySample, 500)
	now := time.Now()
	for i := range samples {
		d := time.Duration(i) * 6 * time.Second
		dist := float64(i) * 10
		hr := float64(140 + i%30)
		spd := 3.5
		samples[i] = domain.ActivitySample{
			SampleAt:            now.Add(d),
			Elapsed:             &d,
			DistanceMeters:      &dist,
			HeartRate:           &hr,
			SpeedMetersPerSecond: &spd,
		}
	}

	sa := domain.SourceActivity{
		Ref: domain.SourceActivityRef{
			Source:           "gpx_upload",
			SourceActivityID: "bench-upsert",
		},
		Activity: domain.Activity{
			SportType:      "run",
			StartAt:        now,
			DistanceMeters: func() *float64 { v := 5000.0; return &v }(),
			Samples:        samples,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sa.Ref.SourceActivityID = "bench-upsert" // same ID = upsert path
		if _, err := repo.UpsertSourceActivity(ctx, athlete.ID, sa); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkComputeAndStoreBestEfforts measures the two-pointer sliding window.
// Seeds 20 activities with 500 GPS samples each before benchmarking.
func BenchmarkComputeAndStoreBestEfforts(b *testing.B) {
	repo, cleanup := newBenchRepo(b)
	defer cleanup()

	ctx := context.Background()
	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		b.Fatal(err)
	}

	// Seed 20 runs with 500 samples each.
	now := time.Now()
	for act := 0; act < 20; act++ {
		samples := make([]domain.ActivitySample, 500)
		start := now.Add(-time.Duration(act) * 24 * time.Hour)
		for i := range samples {
			d := time.Duration(i) * 6 * time.Second
			dist := float64(i) * 10.5
			hr := float64(145 + i%20)
			spd := 3.5 + float64(i%5)*0.1
			samples[i] = domain.ActivitySample{
				SampleAt:            start.Add(d),
				Elapsed:             &d,
				DistanceMeters:      &dist,
				HeartRate:           &hr,
				SpeedMetersPerSecond: &spd,
			}
		}
		dist := 5250.0
		sa := domain.SourceActivity{
			Ref: domain.SourceActivityRef{
				Source:           "gpx_upload",
				SourceActivityID: "bench-be-" + string(rune('A'+act)),
			},
			Activity: domain.Activity{
				SportType:      "run",
				StartAt:        start,
				DistanceMeters: &dist,
				Samples:        samples,
			},
		}
		if _, err := repo.UpsertSourceActivity(ctx, athlete.ID, sa); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := repo.ComputeAndStoreBestEfforts(ctx, athlete.ID, "run"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetStats measures the single-pass GROUPING SETS stats query.
func BenchmarkGetStats(b *testing.B) {
	repo, cleanup := newBenchRepo(b)
	defer cleanup()

	ctx := context.Background()
	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := repo.GetStats(ctx, athlete.ID, StatsFilter{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkListActivities measures cursor-paginated listing.
func BenchmarkListActivities(b *testing.B) {
	repo, cleanup := newBenchRepo(b)
	defer cleanup()

	ctx := context.Background()
	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := repo.ListActivities(ctx, athlete.ID, ListActivitiesFilter{Limit: 20}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetActivitySamples measures downsampled sample retrieval.
func BenchmarkGetActivitySamples(b *testing.B) {
	repo, cleanup := newBenchRepo(b)
	defer cleanup()

	ctx := context.Background()
	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		b.Fatal(err)
	}

	// Find a seeded activity to query.
	activities, _, err := repo.ListActivities(ctx, athlete.ID, ListActivitiesFilter{Limit: 1})
	if err != nil || len(activities) == 0 {
		b.Skip("no activities to benchmark")
	}
	actID := activities[0].ID

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := repo.GetActivitySamples(ctx, actID, 200); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetSimilarActivities measures the bbox range-scan query.
func BenchmarkGetSimilarActivities(b *testing.B) {
	repo, cleanup := newBenchRepo(b)
	defer cleanup()

	ctx := context.Background()
	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		b.Fatal(err)
	}

	activities, _, err := repo.ListActivities(ctx, athlete.ID, ListActivitiesFilter{Limit: 1})
	if err != nil || len(activities) == 0 {
		b.Skip("no activities to benchmark")
	}
	actID := activities[0].ID

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := repo.GetSimilarActivities(ctx, athlete.ID, actID); err != nil {
			b.Fatal(err)
		}
	}
}

// Ensure math is imported (used in seeding above if needed).
var _ = math.Pi
