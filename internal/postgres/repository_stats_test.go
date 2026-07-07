package postgres

import (
	"context"
	"testing"
	"time"

	"athlete-log/internal/domain"
)

func TestGetStats_empty(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "stats-empty")

	stats, err := repo.GetStats(context.Background(), athlete.ID, StatsFilter{})
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalActivities != 0 {
		t.Errorf("total_activities = %d, want 0", stats.TotalActivities)
	}
	if stats.TotalDistanceMeters != 0 {
		t.Errorf("total_distance = %.1f, want 0", stats.TotalDistanceMeters)
	}
}

func TestGetStats_aggregates(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "stats-agg")
	ctx := context.Background()

	conn, _ := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source: domain.SourceNRC, Status: "active",
	})
	seedAct := func(id string, sport domain.SportType, dist float64, pace float64, hr float64, start time.Time) {
		t.Helper()
		elapsed := 30 * time.Minute
		repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
			Ref: domain.SourceActivityRef{
				Source:             domain.SourceNRC,
				SourceConnectionID: conn.ID,
				SourceActivityID:   id,
			},
			Activity: domain.Activity{
				SportType:               sport,
				StartAt:                 start,
				DistanceMeters:          &dist,
				ElapsedDuration:         &elapsed,
				AveragePaceSecondsPerKM: &pace,
				AverageHeartRate:        &hr,
			},
		})
	}

	base := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)
	seedAct("s1", domain.SportRun, 5000, 360, 150, base)
	seedAct("s2", domain.SportRun, 6000, 350, 155, base.AddDate(0, 0, 7))
	seedAct("s3", domain.SportRide, 20000, 0, 140, base.AddDate(0, 0, 14))

	stats, err := repo.GetStats(ctx, athlete.ID, StatsFilter{})
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalActivities != 3 {
		t.Errorf("total_activities = %d, want 3", stats.TotalActivities)
	}
	if stats.TotalDistanceMeters != 31000 {
		t.Errorf("total_distance = %.1f, want 31000", stats.TotalDistanceMeters)
	}
	if len(stats.BySport) != 2 {
		t.Errorf("by_sport count = %d, want 2", len(stats.BySport))
	}

	// Filter to runs only.
	runStats, err := repo.GetStats(ctx, athlete.ID, StatsFilter{SportType: "run"})
	if err != nil {
		t.Fatalf("GetStats(run): %v", err)
	}
	if runStats.TotalActivities != 2 {
		t.Errorf("run activities = %d, want 2", runStats.TotalActivities)
	}
	if runStats.TotalDistanceMeters != 11000 {
		t.Errorf("run distance = %.1f, want 11000", runStats.TotalDistanceMeters)
	}
}

func TestGetTimeseries_weeklyTotals(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "timeseries-weekly")
	ctx := context.Background()

	conn, _ := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source: domain.SourceNRC, Status: "active",
	})

	// Seed one activity per week for 4 weeks starting 2024-W01 (Mon 2024-01-01).
	weeks := []struct {
		id   string
		dist float64
		start time.Time
	}{
		{"w1", 5000, time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)},  // week 1
		{"w2", 6000, time.Date(2024, 1, 8, 8, 0, 0, 0, time.UTC)},  // week 2
		{"w3", 7000, time.Date(2024, 1, 15, 8, 0, 0, 0, time.UTC)}, // week 3
		{"w4", 8000, time.Date(2024, 1, 22, 8, 0, 0, 0, time.UTC)}, // week 4
	}
	elapsed := 30 * time.Minute
	for _, w := range weeks {
		dist := w.dist
		repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
			Ref: domain.SourceActivityRef{
				Source:             domain.SourceNRC,
				SourceConnectionID: conn.ID,
				SourceActivityID:   w.id,
			},
			Activity: domain.Activity{
				SportType:       domain.SportRun,
				StartAt:         w.start,
				DistanceMeters:  &dist,
				ElapsedDuration: &elapsed,
			},
		})
	}

	filter := StatsFilter{
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	points, err := repo.GetTimeseries(ctx, athlete.ID, "week", filter)
	if err != nil {
		t.Fatalf("GetTimeseries: %v", err)
	}
	if len(points) != 4 {
		t.Fatalf("points = %d, want 4", len(points))
	}

	// Verify each week's distance exactly.
	expected := []float64{5000, 6000, 7000, 8000}
	for i, p := range points {
		if p.ActivityCount != 1 {
			t.Errorf("week %d: activity_count = %d, want 1", i+1, p.ActivityCount)
		}
		if p.DistanceMeters != expected[i] {
			t.Errorf("week %d: distance = %.1f, want %.1f", i+1, p.DistanceMeters, expected[i])
		}
	}
}

func TestGetTimeseries_invalidGranularity(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "timeseries-bad")

	_, err := repo.GetTimeseries(context.Background(), athlete.ID, "year", StatsFilter{})
	if err == nil {
		t.Error("expected error for invalid granularity, got nil")
	}
}

func TestGetTimeseries_dailyAndMonthly(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "timeseries-granularity")
	ctx := context.Background()

	conn, _ := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source: domain.SourceNRC, Status: "active",
	})
	elapsed := 30 * time.Minute
	dist := 5000.0
	repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
		Ref: domain.SourceActivityRef{
			Source: domain.SourceNRC, SourceConnectionID: conn.ID, SourceActivityID: "gran1",
		},
		Activity: domain.Activity{
			SportType: domain.SportRun, StartAt: time.Date(2024, 3, 5, 8, 0, 0, 0, time.UTC),
			DistanceMeters: &dist, ElapsedDuration: &elapsed,
		},
	})

	for _, gran := range []string{"day", "month"} {
		pts, err := repo.GetTimeseries(ctx, athlete.ID, gran, StatsFilter{
			From: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Errorf("GetTimeseries(%s): %v", gran, err)
			continue
		}
		if len(pts) == 0 {
			t.Errorf("GetTimeseries(%s): expected at least 1 point", gran)
		}
	}
}
