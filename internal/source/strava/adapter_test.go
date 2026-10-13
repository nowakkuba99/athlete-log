package strava

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
	"athlete-log/internal/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

// --- unit tests (no DB) ---

func TestNeedsRefresh(t *testing.T) {
	expiredConn := domain.SourceConnection{
		Metadata: map[string]any{
			"token_expires_at": float64(time.Now().Add(-1 * time.Hour).Unix()),
		},
	}
	if !NeedsRefresh(expiredConn) {
		t.Fatal("NeedsRefresh = false for expired token, want true")
	}

	freshConn := domain.SourceConnection{
		Metadata: map[string]any{
			"token_expires_at": float64(time.Now().Add(2 * time.Hour).Unix()),
		},
	}
	if NeedsRefresh(freshConn) {
		t.Fatal("NeedsRefresh = true for fresh token, want false")
	}
}

func TestExchangeCode(t *testing.T) {
	fakeStrava := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/token" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  "access-abc",
			RefreshToken: "refresh-xyz",
			ExpiresAt:    time.Now().Add(6 * time.Hour).Unix(),
			Athlete: &struct {
				ID int64 `json:"id"`
			}{ID: 12345},
		})
	}))
	defer fakeStrava.Close()

	adapter := New(Config{ClientID: "id", ClientSecret: "secret"}, fakeStrava.Client(), "")
	adapter.BaseURL = fakeStrava.URL

	meta, externalID, err := adapter.ExchangeCode(context.Background(), "auth-code-123")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if meta["access_token"] != "access-abc" {
		t.Fatalf("access_token = %v, want access-abc", meta["access_token"])
	}
	if meta["refresh_token"] != "refresh-xyz" {
		t.Fatalf("refresh_token = %v", meta["refresh_token"])
	}
	if externalID != "12345" {
		t.Fatalf("external athlete id = %q, want 12345", externalID)
	}
}

func TestRefreshToken(t *testing.T) {
	fakeStrava := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if r.FormValue("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", r.FormValue("grant_type"))
		}
		if r.FormValue("refresh_token") != "old-refresh" {
			t.Errorf("refresh_token = %q, want old-refresh", r.FormValue("refresh_token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresAt:    time.Now().Add(6 * time.Hour).Unix(),
		})
	}))
	defer fakeStrava.Close()

	adapter := New(Config{ClientID: "id", ClientSecret: "secret"}, fakeStrava.Client(), "")
	adapter.BaseURL = fakeStrava.URL

	conn := domain.SourceConnection{
		Source: domain.SourceStrava,
		Metadata: map[string]any{
			"access_token":     "old-access",
			"refresh_token":    "old-refresh",
			"token_expires_at": float64(time.Now().Add(-1 * time.Hour).Unix()),
		},
	}

	refreshed, err := adapter.RefreshToken(context.Background(), conn)
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if refreshed.Metadata["access_token"] != "new-access" {
		t.Fatalf("refreshed access_token = %v", refreshed.Metadata["access_token"])
	}
	if refreshed.Metadata["refresh_token"] != "new-refresh" {
		t.Fatalf("refreshed refresh_token = %v", refreshed.Metadata["refresh_token"])
	}
}

// --- integration test (requires DATABASE_URL) ---

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

func TestStravaSyncIntegration(t *testing.T) {
	pool := testPool(t)
	repo := postgres.New(pool)
	ctx := context.Background()

	athlete, err := repo.CreateAthlete(ctx, "Strava Sync Test")
	if err != nil {
		t.Fatalf("CreateAthlete: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM athletes WHERE id = $1", athlete.ID)
	})

	start := int64(1748441777)
	fakeStrava := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v3/athlete/activities" && r.URL.Query().Get("page") == "1":
			_ = json.NewEncoder(w).Encode([]stravaActivity{{
				ID:           99001,
				Name:         "Morning Ride",
				SportType:    "Ride",
				StartDate:    "2025-09-15T08:00:00Z",
				ElapsedTime:  3600,
				MovingTime:   3400,
				Distance:     30000,
				AverageSpeed: 8.33,
			}})
		case r.URL.Path == "/api/v3/athlete/activities" && r.URL.Query().Get("page") == "2":
			_ = json.NewEncoder(w).Encode([]stravaActivity{}) // empty = end
		case r.URL.Path == "/api/v3/activities/99001":
			_ = json.NewEncoder(w).Encode(stravaActivityDetail{
				Laps: []stravaLap{{
					LapIndex:    1,
					StartDate:   "2025-09-15T08:00:00Z",
					ElapsedTime: 3600,
					Distance:    30000,
				}},
			})
		case r.URL.Path == "/api/v3/activities/99001/streams":
			_ = json.NewEncoder(w).Encode(streamsResponse{
				Time: &struct {
					Data []int `json:"data"`
				}{Data: []int{0, 60, 120}},
				Latlng: &struct {
					Data [][2]float64 `json:"data"`
				}{Data: [][2]float64{
					{52.22, 21.01},
					{52.23, 21.02},
					{52.24, 21.03},
				}},
				Heartrate: &struct {
					Data []float64 `json:"data"`
				}{Data: []float64{130, 135, 140}},
			})
		default:
			t.Errorf("unexpected path: %s?%s", r.URL.Path, r.URL.RawQuery)
			http.NotFound(w, r)
		}
		_ = start
	}))
	defer fakeStrava.Close()

	conn, err := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source: domain.SourceStrava,
		Status: "active",
		Metadata: map[string]any{
			"access_token":     "test-strava-token",
			"refresh_token":    "test-refresh",
			"token_expires_at": float64(time.Now().Add(2 * time.Hour).Unix()),
		},
	})
	if err != nil {
		t.Fatalf("UpsertSourceConnection: %v", err)
	}

	adapter := New(Config{ClientID: "id", ClientSecret: "secret"}, fakeStrava.Client(), "")
	adapter.BaseURL = fakeStrava.URL

	var upserted []domain.SourceActivity
	_, err = adapter.Sync(ctx, conn, func(a domain.SourceActivity) error {
		result, err := repo.UpsertSourceActivity(ctx, athlete.ID, a)
		if err != nil {
			return err
		}
		upserted = append(upserted, result)
		return nil
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(upserted) != 1 {
		t.Fatalf("upserted %d activities, want 1", len(upserted))
	}
	a := upserted[0]
	if a.Activity.SportType != domain.SportRide {
		t.Fatalf("sport type = %q, want ride", a.Activity.SportType)
	}
	if a.Activity.Name != "Morning Ride" {
		t.Fatalf("name = %q", a.Activity.Name)
	}
	if a.Ref.Source != domain.SourceStrava {
		t.Fatalf("source = %q, want strava", a.Ref.Source)
	}
	if len(a.Activity.Samples) != 3 {
		t.Fatalf("samples = %d, want 3", len(a.Activity.Samples))
	}
	if !a.Activity.Samples[0].HasLocation() {
		t.Fatal("first sample has no location")
	}

	// Idempotency: second sync produces same activity ID.
	var second []domain.SourceActivity
	if _, err := adapter.Sync(ctx, conn, func(a domain.SourceActivity) error {
		r, err := repo.UpsertSourceActivity(ctx, athlete.ID, a)
		if err != nil {
			return err
		}
		second = append(second, r)
		return nil
	}); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if len(second) != 1 || second[0].Activity.ID != a.Activity.ID {
		t.Fatal("second sync produced different activity ID — upsert not idempotent")
	}
}

func TestStravaSyncIncrementalAfterParam(t *testing.T) {
	var capturedAfter []string
	fakeStrava := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v3/athlete/activities" {
			capturedAfter = append(capturedAfter, r.URL.Query().Get("after"))
			_ = json.NewEncoder(w).Encode([]stravaActivity{})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer fakeStrava.Close()

	conn := domain.SourceConnection{
		Source: domain.SourceStrava,
		Status: "active",
		Metadata: map[string]any{
			"access_token":     "test-token",
			"refresh_token":    "test-refresh",
			"token_expires_at": float64(time.Now().Add(2 * time.Hour).Unix()),
		},
	}

	adapter := New(Config{ClientID: "id", ClientSecret: "secret"}, fakeStrava.Client(), "")
	adapter.BaseURL = fakeStrava.URL
	ctx := context.Background()

	// First sync — no last_synced_at, so no after param.
	updatedConn, err := adapter.Sync(ctx, conn, func(domain.SourceActivity) error { return nil })
	if err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if len(capturedAfter) == 0 || capturedAfter[0] != "" {
		t.Errorf("first sync: after param = %q, want empty", capturedAfter)
	}

	// Second sync — last_synced_at is now set, after param must be a unix timestamp.
	_, err = adapter.Sync(ctx, updatedConn, func(domain.SourceActivity) error { return nil })
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if len(capturedAfter) < 2 || capturedAfter[1] == "" {
		t.Error("second sync: after param is empty, want unix timestamp from first sync")
	}
}

func TestStravaSyncSkipsUnavailableStreams(t *testing.T) {
	fakeStrava := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v3/athlete/activities" && r.URL.Query().Get("page") == "1":
			_ = json.NewEncoder(w).Encode([]stravaActivity{{
				ID:          12345,
				Name:        "Private Commute",
				SportType:   "Ride",
				StartDate:   "2025-09-15T08:00:00Z",
				ElapsedTime: 1800,
				MovingTime:  1700,
				Distance:    9000,
			}})
		case r.URL.Path == "/api/v3/athlete/activities" && r.URL.Query().Get("page") == "2":
			_ = json.NewEncoder(w).Encode([]stravaActivity{})
		case r.URL.Path == "/api/v3/activities/12345":
			_ = json.NewEncoder(w).Encode(stravaActivityDetail{})
		case r.URL.Path == "/api/v3/activities/12345/streams":
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		default:
			t.Errorf("unexpected path: %s?%s", r.URL.Path, r.URL.RawQuery)
			http.NotFound(w, r)
		}
	}))
	defer fakeStrava.Close()

	conn := domain.SourceConnection{
		Source: domain.SourceStrava,
		Status: "active",
		ID:     "conn-1",
		Metadata: map[string]any{
			"access_token":     "test-token",
			"refresh_token":    "test-refresh",
			"token_expires_at": float64(time.Now().Add(2 * time.Hour).Unix()),
		},
	}

	adapter := New(Config{ClientID: "id", ClientSecret: "secret"}, fakeStrava.Client(), "")
	adapter.BaseURL = fakeStrava.URL

	var upserted []domain.SourceActivity
	updatedConn, err := adapter.Sync(context.Background(), conn, func(a domain.SourceActivity) error {
		upserted = append(upserted, a)
		return nil
	})
	if err != nil {
		t.Fatalf("Sync returned error for unavailable streams: %v", err)
	}
	if len(upserted) != 1 {
		t.Fatalf("upserted %d activities, want 1", len(upserted))
	}
	if samples := len(upserted[0].Activity.Samples); samples != 0 {
		t.Fatalf("samples = %d, want 0 when streams are unavailable", samples)
	}
	if updatedConn.Metadata["last_synced_at"] == nil {
		t.Fatal("last_synced_at was not set after successful sync")
	}
}

func TestStravaCrossSourceDedup(t *testing.T) {
	pool := testPool(t)
	repo := postgres.New(pool)
	ctx := context.Background()

	athlete, err := repo.CreateAthlete(ctx, "Strava Dedup Test")
	if err != nil {
		t.Fatalf("CreateAthlete: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM athletes WHERE id = $1", athlete.ID)
	})

	// Insert a run from NRC at a known start time directly into the repo.
	nrcStart := time.Date(2025, 9, 15, 8, 0, 0, 0, time.UTC)
	nrcConn, err := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source:    domain.SourceNRC,
		SecretKey: "nrc-token",
		Status:    "active",
	})
	if err != nil {
		t.Fatalf("UpsertSourceConnection (NRC): %v", err)
	}
	_, err = repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
		Ref: domain.SourceActivityRef{
			Source:             domain.SourceNRC,
			SourceActivityID:   "nrc-dedup-run",
			SourceConnectionID: nrcConn.ID,
		},
		Activity: domain.Activity{
			SportType: domain.SportRun,
			Name:      "Morning Run (NRC)",
			StartAt:   nrcStart,
		},
	})
	if err != nil {
		t.Fatalf("UpsertSourceActivity (NRC): %v", err)
	}

	// Strava fake API returns the same run at the same start time.
	fakeStrava := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v3/athlete/activities" && r.URL.Query().Get("page") == "1":
			_ = json.NewEncoder(w).Encode([]stravaActivity{{
				ID:          77001,
				Name:        "Morning Run (Strava)",
				SportType:   "Run",
				StartDate:   nrcStart.Format(time.RFC3339),
				ElapsedTime: 1800,
				MovingTime:  1750,
				Distance:    5000,
			}})
		case r.URL.Path == "/api/v3/athlete/activities" && r.URL.Query().Get("page") == "2":
			_ = json.NewEncoder(w).Encode([]stravaActivity{})
		case r.URL.Path == "/api/v3/activities/77001":
			_ = json.NewEncoder(w).Encode(stravaActivityDetail{})
		case r.URL.Path == "/api/v3/activities/77001/streams":
			_ = json.NewEncoder(w).Encode(streamsResponse{})
		default:
			t.Errorf("unexpected path: %s?%s", r.URL.Path, r.URL.RawQuery)
			http.NotFound(w, r)
		}
	}))
	defer fakeStrava.Close()

	stravaConn, err := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source: domain.SourceStrava,
		Status: "active",
		Metadata: map[string]any{
			"access_token":     "strava-token",
			"refresh_token":    "strava-refresh",
			"token_expires_at": float64(time.Now().Add(2 * time.Hour).Unix()),
		},
	})
	if err != nil {
		t.Fatalf("UpsertSourceConnection (Strava): %v", err)
	}

	adapter := New(Config{ClientID: "id", ClientSecret: "secret"}, fakeStrava.Client(), "")
	adapter.BaseURL = fakeStrava.URL

	upsertCalled := 0
	_, err = adapter.Sync(ctx, stravaConn, func(a domain.SourceActivity) error {
		// Mirror server-side cross-source dedup: skip if a nearby activity already exists.
		if !a.Activity.StartAt.IsZero() {
			if dup := repo.FindActivityNearTime(ctx, athlete.ID, a.Activity.StartAt, string(a.Activity.SportType), ""); dup != nil {
				return nil
			}
		}
		if _, upsertErr := repo.UpsertSourceActivity(ctx, athlete.ID, a); upsertErr != nil {
			return upsertErr
		}
		upsertCalled++
		return nil
	})
	if err != nil {
		t.Fatalf("Strava Sync: %v", err)
	}
	if upsertCalled != 0 {
		t.Fatalf("upsert called %d times, want 0 — Strava run must be deduped against NRC run", upsertCalled)
	}

	// Confirm only 1 activity in DB for this athlete.
	var count int
	if err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM activities WHERE athlete_id = $1::uuid AND delete_indicator = false",
		athlete.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count activities: %v", err)
	}
	if count != 1 {
		t.Fatalf("DB has %d activities, want 1 (cross-source dedup failed)", count)
	}
}
