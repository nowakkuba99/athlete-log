package nrcsource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"athlete-log/internal/domain"
	"athlete-log/internal/migrate"
	"athlete-log/internal/migrations"
	"athlete-log/internal/nrc"
	"athlete-log/internal/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := migrate.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := migrate.Run(ctx, migrate.PGXRunner{Pool: pool}, migrations.Files); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return pool
}

func TestNRCSyncAdapterIntegration(t *testing.T) {
	pool := testPool(t)
	repo := postgres.New(pool)
	ctx := context.Background()

	athlete, err := repo.CreateAthlete(ctx, "NRC Sync Test")
	if err != nil {
		t.Fatalf("CreateAthlete: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM athletes WHERE id = $1", athlete.ID)
	})

	// Build two sample NRC activities for the fake server to return.
	start1 := int64(1748441777960)
	start2 := start1 + 3600000

	act1 := nrc.Activity{
		ID: "nrc-run-a", Type: "run",
		StartEpochMs: start1, EndEpochMs: start1 + 1800000,
		ActiveDurationMs: 1800000,
		Summaries: []nrc.Summary{
			{Metric: "distance", Summary: "total", Value: 5.0},
		},
	}
	act2 := nrc.Activity{
		ID: "nrc-run-b", Type: "run",
		StartEpochMs: start2, EndEpochMs: start2 + 2400000,
		ActiveDurationMs: 2400000,
		Summaries: []nrc.Summary{
			{Metric: "distance", Summary: "total", Value: 8.0},
		},
	}

	// Fake NRC server — models real API: first page has activities, second page is empty.
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/plus/v3/activities/before_id/v3/*":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: act1.ID, StartEpochMs: act1.StartEpochMs},
					{ID: act2.ID, StartEpochMs: act2.StartEpochMs},
				},
				Paging: nrc.Paging{},
			})
		case "/plus/v3/activities/before_id/v3/nrc-run-b":
			// Empty page signals end of data.
			_ = json.NewEncoder(w).Encode(nrc.Activities{})
		case "/sport/v3/me/activity/nrc-run-a":
			_ = json.NewEncoder(w).Encode(act1)
		case "/sport/v3/me/activity/nrc-run-b":
			_ = json.NewEncoder(w).Encode(act2)
		default:
			t.Errorf("unexpected NRC path: %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer fakeServer.Close()

	conn, err := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source:    domain.SourceNRC,
		SecretKey: "test-token",
		Status:    "active",
	})
	if err != nil {
		t.Fatalf("UpsertSourceConnection: %v", err)
	}

	adapter := New(fakeServer.Client(), "")
	adapter.BaseURL = fakeServer.URL

	var upserted []domain.SourceActivity
	err = adapter.Sync(ctx, conn, func(a domain.SourceActivity) error {
		result, upsertErr := repo.UpsertSourceActivity(ctx, athlete.ID, a)
		if upsertErr != nil {
			return upsertErr
		}
		upserted = append(upserted, result)
		return nil
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(upserted) != 2 {
		t.Fatalf("upserted %d activities, want 2", len(upserted))
	}
	for _, a := range upserted {
		if a.Activity.ID == "" {
			t.Fatal("activity ID is empty after upsert")
		}
		if a.Ref.Source != domain.SourceNRC {
			t.Fatalf("source = %q, want nrc", a.Ref.Source)
		}
		if a.Ref.SourceConnectionID != conn.ID {
			t.Fatalf("source connection id = %q, want %q", a.Ref.SourceConnectionID, conn.ID)
		}
	}

	// Verify idempotency: second sync produces same activity IDs.
	var secondRun []domain.SourceActivity
	if err := adapter.Sync(ctx, conn, func(a domain.SourceActivity) error {
		result, upsertErr := repo.UpsertSourceActivity(ctx, athlete.ID, a)
		if upsertErr != nil {
			return upsertErr
		}
		secondRun = append(secondRun, result)
		return nil
	}); err != nil {
		t.Fatalf("second Sync: %v", err)
	}

	if len(secondRun) != 2 {
		t.Fatalf("second sync upserted %d, want 2", len(secondRun))
	}
	if secondRun[0].Activity.ID != upserted[0].Activity.ID {
		t.Fatal("second sync produced different activity IDs — upsert not idempotent")
	}
}

func TestNRCSyncPagination(t *testing.T) {
	pool := testPool(t)
	repo := postgres.New(pool)
	ctx := context.Background()

	athlete, err := repo.CreateAthlete(ctx, "NRC Pagination Test")
	if err != nil {
		t.Fatalf("CreateAthlete: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM athletes WHERE id = $1", athlete.ID)
	})

	base := int64(1748441777960)
	makeAct := func(id string, startMs int64) nrc.Activity {
		return nrc.Activity{
			ID: id, Type: "run",
			StartEpochMs: startMs, EndEpochMs: startMs + 1800000,
			ActiveDurationMs: 1800000,
			Summaries:        []nrc.Summary{{Metric: "distance", Summary: "total", Value: 5.0}},
		}
	}
	acts := []nrc.Activity{
		makeAct("pg-1", base),
		makeAct("pg-2", base+3600000),
		makeAct("pg-3", base+7200000),
	}

	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/plus/v3/activities/before_id/v3/*":
			_ = json.NewEncoder(w).Encode(nrc.Activities{Activities: []nrc.ActivityListItem{{ID: "pg-1", StartEpochMs: acts[0].StartEpochMs}}})
		case "/plus/v3/activities/before_id/v3/pg-1":
			_ = json.NewEncoder(w).Encode(nrc.Activities{Activities: []nrc.ActivityListItem{{ID: "pg-2", StartEpochMs: acts[1].StartEpochMs}}})
		case "/plus/v3/activities/before_id/v3/pg-2":
			_ = json.NewEncoder(w).Encode(nrc.Activities{Activities: []nrc.ActivityListItem{{ID: "pg-3", StartEpochMs: acts[2].StartEpochMs}}})
		case "/plus/v3/activities/before_id/v3/pg-3":
			_ = json.NewEncoder(w).Encode(nrc.Activities{}) // end of pages
		case "/sport/v3/me/activity/pg-1":
			_ = json.NewEncoder(w).Encode(acts[0])
		case "/sport/v3/me/activity/pg-2":
			_ = json.NewEncoder(w).Encode(acts[1])
		case "/sport/v3/me/activity/pg-3":
			_ = json.NewEncoder(w).Encode(acts[2])
		default:
			t.Errorf("unexpected NRC path: %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer fakeServer.Close()

	conn, err := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source:    domain.SourceNRC,
		SecretKey: "test-token",
		Status:    "active",
	})
	if err != nil {
		t.Fatalf("UpsertSourceConnection: %v", err)
	}

	adapter := New(fakeServer.Client(), "")
	adapter.BaseURL = fakeServer.URL

	var upserted []domain.SourceActivity
	if err := adapter.Sync(ctx, conn, func(a domain.SourceActivity) error {
		result, upsertErr := repo.UpsertSourceActivity(ctx, athlete.ID, a)
		if upsertErr != nil {
			return upsertErr
		}
		upserted = append(upserted, result)
		return nil
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(upserted) != 3 {
		t.Fatalf("upserted %d activities across 3 pages, want 3", len(upserted))
	}
}

func TestNRCSyncSkipsFailedDetail(t *testing.T) {
	pool := testPool(t)
	repo := postgres.New(pool)
	ctx := context.Background()

	athlete, err := repo.CreateAthlete(ctx, "NRC Skip Test")
	if err != nil {
		t.Fatalf("CreateAthlete: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM athletes WHERE id = $1", athlete.ID)
	})

	base := int64(1748441777960)
	act1 := nrc.Activity{
		ID: "skip-1", Type: "run",
		StartEpochMs: base, EndEpochMs: base + 1800000,
		ActiveDurationMs: 1800000,
		Summaries:        []nrc.Summary{{Metric: "distance", Summary: "total", Value: 5.0}},
	}
	act2 := nrc.Activity{ID: "skip-2", Type: "run", StartEpochMs: base + 3600000, EndEpochMs: base + 5400000, ActiveDurationMs: 1800000}

	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/plus/v3/activities/before_id/v3/*":
			_ = json.NewEncoder(w).Encode(nrc.Activities{Activities: []nrc.ActivityListItem{
				{ID: act1.ID, StartEpochMs: act1.StartEpochMs},
				{ID: act2.ID, StartEpochMs: act2.StartEpochMs},
			}})
		case "/plus/v3/activities/before_id/v3/skip-2":
			_ = json.NewEncoder(w).Encode(nrc.Activities{})
		case "/sport/v3/me/activity/skip-1":
			_ = json.NewEncoder(w).Encode(act1)
		case "/sport/v3/me/activity/skip-2":
			http.Error(w, "internal error", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected NRC path: %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer fakeServer.Close()

	conn, err := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source:    domain.SourceNRC,
		SecretKey: "test-token",
		Status:    "active",
	})
	if err != nil {
		t.Fatalf("UpsertSourceConnection: %v", err)
	}

	adapter := New(fakeServer.Client(), "")
	adapter.BaseURL = fakeServer.URL

	var upserted []domain.SourceActivity
	err = adapter.Sync(ctx, conn, func(a domain.SourceActivity) error {
		result, upsertErr := repo.UpsertSourceActivity(ctx, athlete.ID, a)
		if upsertErr != nil {
			return upsertErr
		}
		upserted = append(upserted, result)
		return nil
	})
	if err != nil {
		t.Fatalf("Sync returned error for non-fatal detail failure: %v", err)
	}
	if len(upserted) != 1 {
		t.Fatalf("upserted %d activities, want 1 (second had 500 on detail)", len(upserted))
	}
	if upserted[0].Ref.SourceActivityID != act1.ID {
		t.Fatalf("upserted wrong activity: %q, want %q", upserted[0].Ref.SourceActivityID, act1.ID)
	}
}

func TestNRCSyncRateLimitAborts(t *testing.T) {
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
	}))
	defer fakeServer.Close()

	conn := domain.SourceConnection{
		Source:    domain.SourceNRC,
		SecretKey: "test-token",
		Status:    "active",
	}
	adapter := New(fakeServer.Client(), "")
	adapter.BaseURL = fakeServer.URL

	err := adapter.Sync(context.Background(), conn, func(domain.SourceActivity) error { return nil })
	if err == nil {
		t.Fatal("Sync returned nil for 429, want error")
	}
}
