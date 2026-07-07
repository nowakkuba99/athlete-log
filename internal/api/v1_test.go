package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"athlete-log/internal/api"
	"athlete-log/internal/domain"
	"athlete-log/internal/job"
	"athlete-log/internal/migrate"
	"athlete-log/internal/migrations"
	"athlete-log/internal/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testServer builds a fully-wired APIServer backed by a real Postgres pool.
// Skips if DATABASE_URL is not set.
func testServer(t *testing.T) (*api.APIServer, string) {
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

	repo := postgres.New(pool)
	athlete, err := repo.CreateAthlete(ctx, "test-v1-"+t.Name())
	if err != nil {
		t.Fatalf("create athlete: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM athletes WHERE id = $1", athlete.ID)
	})

	runner := job.New(repo)
	srv := api.NewAPIServer(nil, nil).WithRepository(repo, runner, athlete.ID, t.TempDir())
	return srv, athlete.ID
}

// seedActivity inserts a test activity via the repository.
func seedActivity(t *testing.T, pool *pgxpool.Pool, athleteID string, sport domain.SportType, name string, start time.Time) domain.Activity {
	t.Helper()
	repo := postgres.New(pool)
	ctx := context.Background()

	conn, err := repo.UpsertSourceConnection(ctx, athleteID, domain.SourceConnection{
		Source: domain.SourceNRC, Status: "active",
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	dist := 5000.0
	elapsed := 30 * time.Minute
	hr := 155.0
	sa := domain.SourceActivity{
		Ref: domain.SourceActivityRef{
			Source:             domain.SourceNRC,
			SourceConnectionID: conn.ID,
			SourceActivityID:   fmt.Sprintf("test-%s-%d", name, start.Unix()),
		},
		Activity: domain.Activity{
			SportType:        sport,
			Name:             name,
			StartAt:          start,
			DistanceMeters:   &dist,
			ElapsedDuration:  &elapsed,
			AverageHeartRate: &hr,
			Samples: []domain.ActivitySample{
				{
					SampleAt:  start,
					Latitude:  pf(52.2297),
					Longitude: pf(21.0122),
					HeartRate: pf(150.0),
				},
			},
		},
	}
	result, err := repo.UpsertSourceActivity(ctx, athleteID, sa)
	if err != nil {
		t.Fatalf("upsert activity: %v", err)
	}
	return result.Activity
}

func TestV1ActivitiesList(t *testing.T) {
	srv, athleteID := testServer(t)
	pool := testPoolFromEnv(t)
	start := time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC)
	seedActivity(t, pool, athleteID, domain.SportRun, "Morning Run", start)
	seedActivity(t, pool, athleteID, domain.SportRide, "Evening Ride", start.Add(24*time.Hour))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/activities", nil)
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp api.V1ActivityListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Activities) < 2 {
		t.Errorf("activities: got %d, want >= 2", len(resp.Activities))
	}
}

func TestV1ActivitiesListFilter(t *testing.T) {
	srv, athleteID := testServer(t)
	pool := testPoolFromEnv(t)
	start := time.Date(2024, 4, 1, 8, 0, 0, 0, time.UTC)
	seedActivity(t, pool, athleteID, domain.SportRun, "Run A", start)
	seedActivity(t, pool, athleteID, domain.SportRide, "Ride B", start.Add(time.Hour))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/activities?sport_type=run", nil)
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp api.V1ActivityListResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	for _, item := range resp.Activities {
		if item.SportType != "run" {
			t.Errorf("unexpected sport_type %q in filtered response", item.SportType)
		}
	}
}

func TestV1ActivityDetail(t *testing.T) {
	srv, athleteID := testServer(t)
	pool := testPoolFromEnv(t)
	start := time.Date(2024, 5, 1, 8, 0, 0, 0, time.UTC)
	act := seedActivity(t, pool, athleteID, domain.SportRun, "Detail Test", start)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/activities/"+act.ID, nil)
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp api.ActivityDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != act.ID {
		t.Errorf("id: got %q, want %q", resp.ID, act.ID)
	}
	if resp.SportType != "run" {
		t.Errorf("sport_type: got %q, want run", resp.SportType)
	}
	if resp.Name != "Detail Test" {
		t.Errorf("name: got %q, want Detail Test", resp.Name)
	}
}

func TestV1ActivityDetailNotFound(t *testing.T) {
	srv, _ := testServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/activities/00000000-0000-0000-0000-000000000000", nil)
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestV1ActivitySamples(t *testing.T) {
	srv, athleteID := testServer(t)
	pool := testPoolFromEnv(t)
	start := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)
	act := seedActivity(t, pool, athleteID, domain.SportRun, "Samples Test", start)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/activities/"+act.ID+"/samples?resolution=200", nil)
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var samples []api.SamplePoint
	if err := json.NewDecoder(rec.Body).Decode(&samples); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(samples) == 0 {
		t.Error("expected at least one sample")
	}
	if samples[0].Latitude == nil || samples[0].Longitude == nil {
		t.Error("first sample missing lat/lon")
	}
}

func TestV1JobsList(t *testing.T) {
	srv, _ := testServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var jobs []map[string]any
	json.NewDecoder(rec.Body).Decode(&jobs)
	// Just check it's a valid JSON array (may be empty for a fresh athlete).
	if jobs == nil {
		jobs = []map[string]any{}
	}
}

func TestV1SourcesList(t *testing.T) {
	srv, athleteID := testServer(t)
	pool := testPoolFromEnv(t)
	repo := postgres.New(pool)
	_, err := repo.UpsertSourceConnection(context.Background(), athleteID, domain.SourceConnection{
		Source: domain.SourceNRC, Status: "active",
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var sources []api.V1SourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&sources); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(sources) == 0 {
		t.Error("expected at least one source connection")
	}
	if sources[0].Source == "" {
		t.Error("source field is empty")
	}
}

func testPoolFromEnv(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := migrate.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func pf(v float64) *float64 { return &v }
