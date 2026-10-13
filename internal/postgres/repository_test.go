package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"athlete-log/internal/domain"
	"athlete-log/internal/migrate"
	"athlete-log/internal/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool connects to Postgres, runs migrations, and registers cleanup.
// Skips the test if DATABASE_URL is not set.
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

// testAthlete creates an athlete and registers cleanup via CASCADE delete.
func testAthlete(t *testing.T, repo *Repository, name string) domain.Athlete {
	t.Helper()
	athlete, err := repo.CreateAthlete(context.Background(), name)
	if err != nil {
		t.Fatalf("CreateAthlete(%q): %v", name, err)
	}
	t.Cleanup(func() {
		repo.pool.Exec(context.Background(), "DELETE FROM athletes WHERE id = $1", athlete.ID)
	})
	return athlete
}

func TestUpsertSourceActivityRequiresIdentity(t *testing.T) {
	repo := New(nil)

	_, err := repo.UpsertSourceActivity(context.Background(), "", domain.SourceActivity{})
	if err == nil {
		t.Fatal("UpsertSourceActivity() error = nil, want missing athlete id error")
	}
}

func TestDurationMillis(t *testing.T) {
	duration := 1500 * time.Millisecond

	got := durationMillis(&duration)
	if got == nil || *got != 1500 {
		t.Fatalf("durationMillis() = %v, want 1500", got)
	}
	if durationMillis(nil) != nil {
		t.Fatal("durationMillis(nil) returned non-nil")
	}
}

func TestUpsertSourceActivityIntegration(t *testing.T) {
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
	defer pool.Close()

	if err := migrate.Run(ctx, migrate.PGXRunner{Pool: pool}, migrations.Files); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := New(pool)
	athlete, err := repo.CreateAthlete(ctx, "Repository Test")
	if err != nil {
		t.Fatalf("CreateAthlete() error = %v", err)
	}
	defer pool.Exec(context.Background(), "DELETE FROM athletes WHERE id = $1", athlete.ID)

	connection, err := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source:            domain.SourceNRC,
		ExternalAthleteID: "external-athlete-1",
		SecretKey:         "local-secret-key",
		Metadata: map[string]any{
			"connected_by": "test",
		},
	})
	if err != nil {
		t.Fatalf("UpsertSourceConnection() error = %v", err)
	}
	if connection.ID == "" {
		t.Fatal("source connection id was not returned")
	}
	if connection.Status != "active" {
		t.Fatalf("source connection status = %q, want active", connection.Status)
	}

	start := time.Date(2025, time.September, 15, 10, 0, 0, 0, time.UTC)
	elapsed := 30 * time.Minute
	distance := 5000.0
	hr := 142.0
	sampleDistance := 1000.0
	tagValue := "Morning run"

	first, err := repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
		Ref: domain.SourceActivityRef{
			SourceConnectionID: connection.ID,
			Source:             domain.SourceNRC,
			SourceActivityID:   "nrc-run-1",
			RawBlobKey:         "nrc/nrc-run-1.json",
			RawSHA256:          "abc123",
		},
		Activity: domain.Activity{
			SportType:        domain.SportRun,
			Name:             "Morning run",
			StartAt:          start,
			ElapsedDuration:  &elapsed,
			DistanceMeters:   &distance,
			AverageHeartRate: &hr,
			Samples: []domain.ActivitySample{
				{
					SampleAt:       start,
					DistanceMeters: &sampleDistance,
					HeartRate:      &hr,
				},
			},
			Tags: []domain.ActivityTag{
				{Key: "com.nike.name", Value: tagValue},
			},
		},
	})
	if err != nil {
		t.Fatalf("first UpsertSourceActivity() error = %v", err)
	}
	if first.Ref.ID == "" {
		t.Fatal("source ref id was not returned")
	}
	if first.Activity.ID == "" {
		t.Fatal("activity id was not returned")
	}
	if len(first.Activity.Samples) != 1 || first.Activity.Samples[0].ID == 0 {
		t.Fatalf("sample id was not returned: %#v", first.Activity.Samples)
	}

	updatedDistance := 6000.0
	second, err := repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
		Ref: domain.SourceActivityRef{
			SourceConnectionID: connection.ID,
			Source:             domain.SourceNRC,
			SourceActivityID:   "nrc-run-1",
			RawBlobKey:         "nrc/nrc-run-1-v2.json",
		},
		Activity: domain.Activity{
			SportType:      domain.SportRun,
			Name:           "Morning run updated",
			StartAt:        start,
			DistanceMeters: &updatedDistance,
			Samples: []domain.ActivitySample{
				{SampleAt: start.Add(time.Minute), DistanceMeters: &updatedDistance},
				{SampleAt: start.Add(2 * time.Minute), DistanceMeters: &updatedDistance},
			},
			Tags: []domain.ActivityTag{
				{Key: "com.nike.name", Value: "Morning run updated"},
			},
		},
	})
	if err != nil {
		t.Fatalf("second UpsertSourceActivity() error = %v", err)
	}
	if second.Ref.ID != first.Ref.ID {
		t.Fatalf("source ref id changed: %q -> %q", first.Ref.ID, second.Ref.ID)
	}
	if second.Activity.ID != first.Activity.ID {
		t.Fatalf("activity id changed: %q -> %q", first.Activity.ID, second.Activity.ID)
	}

	var sampleCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM activity_samples WHERE activity_id = $1", first.Activity.ID).Scan(&sampleCount); err != nil {
		t.Fatalf("query sample count: %v", err)
	}
	if sampleCount != 2 {
		t.Fatalf("sample count = %d, want 2", sampleCount)
	}

	var storedName string
	var storedDistance float64
	if err := pool.QueryRow(ctx, "SELECT name, distance_meters FROM activities WHERE id = $1", first.Activity.ID).Scan(&storedName, &storedDistance); err != nil {
		t.Fatalf("query activity: %v", err)
	}
	if storedName != "Morning run updated" {
		t.Fatalf("stored name = %q", storedName)
	}
	if storedDistance != updatedDistance {
		t.Fatalf("stored distance = %.3f, want %.3f", storedDistance, updatedDistance)
	}
}

// --- cursor unit tests ---

func TestCursorRoundTrip(t *testing.T) {
	ts := time.Date(2025, 9, 15, 10, 30, 0, 123456789, time.UTC)
	id := "550e8400-e29b-41d4-a716-446655440000"

	cursor := encodeCursor(ts, id)
	if cursor == "" {
		t.Fatal("encodeCursor returned empty string")
	}

	gotTime, gotID, err := decodeCursor(cursor)
	if err != nil {
		t.Fatalf("decodeCursor() error = %v", err)
	}
	if !gotTime.Equal(ts) {
		t.Fatalf("time = %v, want %v", gotTime, ts)
	}
	if gotID != id {
		t.Fatalf("id = %q, want %q", gotID, id)
	}
}

func TestCursorDecodeInvalid(t *testing.T) {
	cases := []string{"", "not-base64!!!", "dGVzdA"}
	for _, c := range cases {
		if _, _, err := decodeCursor(c); err == nil {
			t.Errorf("decodeCursor(%q) expected error, got nil", c)
		}
	}
}

// --- ListActivities tests ---

func TestListActivitiesRequiresAthleteID(t *testing.T) {
	repo := New(nil)
	_, _, err := repo.ListActivities(context.Background(), "", ListActivitiesFilter{})
	if err == nil {
		t.Fatal("expected error for empty athleteID, got nil")
	}
}

func TestListActivitiesIntegration(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "List Test")

	ctx := context.Background()
	t1 := time.Date(2025, 9, 15, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 9, 14, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 9, 13, 10, 0, 0, 0, time.UTC)

	upsertActivity := func(sourceID string, startAt time.Time) string {
		t.Helper()
		result, err := repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
			Ref: domain.SourceActivityRef{
				Source:           domain.SourceNRC,
				SourceActivityID: sourceID,
			},
			Activity: domain.Activity{SportType: domain.SportRun, StartAt: startAt},
		})
		if err != nil {
			t.Fatalf("UpsertSourceActivity(%q): %v", sourceID, err)
		}
		return result.Activity.ID
	}

	id1 := upsertActivity("list-run-1", t1)
	id2 := upsertActivity("list-run-2", t2)
	id3 := upsertActivity("list-run-3", t3)

	// Page 1: limit 2 — should return newest two activities.
	page1, cursor, err := repo.ListActivities(ctx, athlete.ID, ListActivitiesFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListActivities page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page 1 len = %d, want 2", len(page1))
	}
	if page1[0].ID != id1 || page1[1].ID != id2 {
		t.Fatalf("page 1 order wrong: got [%s %s], want [%s %s]",
			page1[0].ID, page1[1].ID, id1, id2)
	}
	if cursor == "" {
		t.Fatal("cursor is empty after page 1, expected next cursor")
	}

	// Page 2: should return the third activity and no further cursor.
	page2, cursor2, err := repo.ListActivities(ctx, athlete.ID, ListActivitiesFilter{Limit: 2, Cursor: cursor})
	if err != nil {
		t.Fatalf("ListActivities page 2: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("page 2 len = %d, want 1", len(page2))
	}
	if page2[0].ID != id3 {
		t.Fatalf("page 2 activity = %s, want %s", page2[0].ID, id3)
	}
	if cursor2 != "" {
		t.Fatalf("cursor after last page = %q, want empty", cursor2)
	}
}

func TestListActivitiesFilterBySportType(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "Filter Test")

	ctx := context.Background()
	start := time.Date(2025, 9, 15, 10, 0, 0, 0, time.UTC)

	upsert := func(sourceID string, sport domain.SportType) {
		t.Helper()
		if _, err := repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
			Ref: domain.SourceActivityRef{Source: domain.SourceNRC, SourceActivityID: sourceID},
			Activity: domain.Activity{SportType: sport, StartAt: start},
		}); err != nil {
			t.Fatalf("UpsertSourceActivity(%q): %v", sourceID, err)
		}
	}

	upsert("filter-run-1", domain.SportRun)
	upsert("filter-run-2", domain.SportRun)
	upsert("filter-other-1", domain.SportUnknown)

	runs, _, err := repo.ListActivities(ctx, athlete.ID, ListActivitiesFilter{SportType: string(domain.SportRun)})
	if err != nil {
		t.Fatalf("ListActivities(SportRun): %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("filtered run count = %d, want 2", len(runs))
	}
}

// --- GetActivity tests ---

func TestGetActivityRequiresIDs(t *testing.T) {
	repo := New(nil)
	if _, err := repo.GetActivity(context.Background(), "", "some-id"); err == nil {
		t.Fatal("expected error for empty athleteID")
	}
	if _, err := repo.GetActivity(context.Background(), "some-id", ""); err == nil {
		t.Fatal("expected error for empty activity id")
	}
}

func TestGetActivityIntegration(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "Get Test")

	ctx := context.Background()
	start := time.Date(2025, 9, 15, 10, 0, 0, 0, time.UTC)
	lapEnd := start.Add(10 * time.Minute)
	lapDur := 10 * time.Minute
	lapDist := 2000.0

	result, err := repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
		Ref: domain.SourceActivityRef{Source: domain.SourceNRC, SourceActivityID: "get-run-1"},
		Activity: domain.Activity{
			SportType: domain.SportRun,
			Name:      "Morning run",
			StartAt:   start,
			Laps: []domain.ActivityLap{
				{LapIndex: 0, StartAt: start, EndAt: &lapEnd, Duration: &lapDur, DistanceMeters: &lapDist},
				{LapIndex: 1, StartAt: lapEnd, Duration: &lapDur, DistanceMeters: &lapDist},
			},
			Tags: []domain.ActivityTag{
				{Key: "source", Value: "test"},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertSourceActivity: %v", err)
	}

	got, err := repo.GetActivity(ctx, athlete.ID, result.Activity.ID)
	if err != nil {
		t.Fatalf("GetActivity: %v", err)
	}
	if got.Name != "Morning run" {
		t.Fatalf("name = %q, want %q", got.Name, "Morning run")
	}
	if len(got.Laps) != 2 {
		t.Fatalf("laps len = %d, want 2", len(got.Laps))
	}
	if got.Laps[0].LapIndex != 0 || got.Laps[1].LapIndex != 1 {
		t.Fatalf("lap indexes wrong: %d %d", got.Laps[0].LapIndex, got.Laps[1].LapIndex)
	}
	if len(got.Tags) != 1 || got.Tags[0].Key != "source" {
		t.Fatalf("tags = %v, want [{source test}]", got.Tags)
	}
}

func TestGetActivityNotFound(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "NotFound Test")

	_, err := repo.GetActivity(context.Background(), athlete.ID, "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error for missing activity, got nil")
	}
}

// --- GetActivitySamples tests ---

func TestGetActivitySamplesIntegration(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "Samples Test")

	ctx := context.Background()
	start := time.Date(2025, 9, 15, 10, 0, 0, 0, time.UTC)

	// Build 10 samples 1 minute apart.
	samples := make([]domain.ActivitySample, 10)
	for i := range samples {
		lat := 52.0 + float64(i)*0.001
		lng := 21.0 + float64(i)*0.001
		samples[i] = domain.ActivitySample{
			SampleAt:  start.Add(time.Duration(i) * time.Minute),
			Latitude:  &lat,
			Longitude: &lng,
		}
	}

	result, err := repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
		Ref:      domain.SourceActivityRef{Source: domain.SourceNRC, SourceActivityID: "samples-run-1"},
		Activity: domain.Activity{SportType: domain.SportRun, StartAt: start, Samples: samples},
	})
	if err != nil {
		t.Fatalf("UpsertSourceActivity: %v", err)
	}

	all, err := repo.GetActivitySamples(ctx, result.Activity.ID, 0)
	if err != nil {
		t.Fatalf("GetActivitySamples(0): %v", err)
	}
	if len(all) != 10 {
		t.Fatalf("all samples = %d, want 10", len(all))
	}

	downsampled, err := repo.GetActivitySamples(ctx, result.Activity.ID, 4)
	if err != nil {
		t.Fatalf("GetActivitySamples(4): %v", err)
	}
	if len(downsampled) >= 10 {
		t.Fatalf("downsampled count = %d, should be < 10", len(downsampled))
	}
	// First and last samples are always included.
	if !downsampled[0].SampleAt.Equal(samples[0].SampleAt) {
		t.Fatalf("first downsampled sample = %v, want %v", downsampled[0].SampleAt, samples[0].SampleAt)
	}
	if !downsampled[len(downsampled)-1].SampleAt.Equal(samples[9].SampleAt) {
		t.Fatalf("last downsampled sample = %v, want %v", downsampled[len(downsampled)-1].SampleAt, samples[9].SampleAt)
	}
}

// --- job unit tests ---

func TestCreateJobRequiresAthleteID(t *testing.T) {
	repo := New(nil)
	if _, err := repo.CreateJob(context.Background(), "", "export", nil); err == nil {
		t.Fatal("expected error for empty athleteID")
	}
}

func TestCreateJobRequiresType(t *testing.T) {
	repo := New(nil)
	if _, err := repo.CreateJob(context.Background(), "some-id", "", nil); err == nil {
		t.Fatal("expected error for empty job type")
	}
}

func TestUpdateJobRequiresID(t *testing.T) {
	repo := New(nil)
	if err := repo.UpdateJob(context.Background(), "", JobUpdate{Status: domain.JobRunning}); err == nil {
		t.Fatal("expected error for empty job id")
	}
}

func TestUpdateJobRequiresStatus(t *testing.T) {
	repo := New(nil)
	if err := repo.UpdateJob(context.Background(), "some-id", JobUpdate{}); err == nil {
		t.Fatal("expected error for empty status")
	}
}

// --- job integration tests ---

func TestJobLifecycleIntegration(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "Job Test")
	ctx := context.Background()

	// Create in queued state.
	job, err := repo.CreateJob(ctx, athlete.ID, "export", map[string]any{"sport": "run"})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.ID == "" {
		t.Fatal("job id is empty")
	}
	if job.Status != domain.JobQueued {
		t.Fatalf("initial status = %q, want %q", job.Status, domain.JobQueued)
	}

	// GetJob returns the same record.
	got, err := repo.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Type != "export" {
		t.Fatalf("type = %q, want export", got.Type)
	}

	// Transition to running.
	now := time.Now().UTC()
	if err := repo.UpdateJob(ctx, job.ID, JobUpdate{
		Status:    domain.JobRunning,
		Phase:     "scanning",
		Message:   "scanning NRC pages",
		StartedAt: &now,
	}); err != nil {
		t.Fatalf("UpdateJob running: %v", err)
	}

	// Append an event.
	if err := repo.AppendJobEvent(ctx, job.ID, "info", "found 42 activities", map[string]any{"count": 42}); err != nil {
		t.Fatalf("AppendJobEvent: %v", err)
	}

	// Progress update.
	total := 42
	if err := repo.UpdateJob(ctx, job.ID, JobUpdate{
		Status:          domain.JobRunning,
		Phase:           "exporting",
		Message:         "converting activities",
		ProgressCurrent: 21,
		ProgressTotal:   &total,
		StartedAt:       &now,
	}); err != nil {
		t.Fatalf("UpdateJob progress: %v", err)
	}

	// Complete with result.
	completedAt := time.Now().UTC()
	if err := repo.UpdateJob(ctx, job.ID, JobUpdate{
		Status:          domain.JobSucceeded,
		Phase:           "done",
		Message:         "exported 42 activities",
		ProgressCurrent: 42,
		ProgressTotal:   &total,
		Result:          map[string]any{"exported": 42},
		StartedAt:       &now,
		CompletedAt:     &completedAt,
	}); err != nil {
		t.Fatalf("UpdateJob complete: %v", err)
	}

	// Verify final state.
	final, err := repo.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob final: %v", err)
	}
	if final.Status != domain.JobSucceeded {
		t.Fatalf("final status = %q, want succeeded", final.Status)
	}
	if final.ProgressCurrent != 42 {
		t.Fatalf("progress_current = %d, want 42", final.ProgressCurrent)
	}
	if final.ProgressTotal == nil || *final.ProgressTotal != 42 {
		t.Fatalf("progress_total = %v, want 42", final.ProgressTotal)
	}
	if v, ok := final.Result["exported"]; !ok || v == nil {
		t.Fatalf("result missing exported key: %v", final.Result)
	}
	if final.CompletedAt == nil {
		t.Fatal("completed_at is nil")
	}

	// ListJobs returns the completed job.
	jobs, err := repo.ListJobs(ctx, athlete.ID, 10)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) == 0 {
		t.Fatal("ListJobs returned no jobs")
	}
	if jobs[0].ID != job.ID {
		t.Fatalf("ListJobs first id = %q, want %q", jobs[0].ID, job.ID)
	}
}

func TestJobNotFoundIntegration(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)

	_, err := repo.GetJob(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error for missing job")
	}
}

// --- artifact tests ---

func TestCreateArtifactRequiresFields(t *testing.T) {
	repo := New(nil)
	if _, err := repo.CreateArtifact(context.Background(), domain.Artifact{Kind: "zip", StorageKey: "k"}); err == nil {
		t.Fatal("expected error for empty athlete id")
	}
	if _, err := repo.CreateArtifact(context.Background(), domain.Artifact{AthleteID: "id", StorageKey: "k"}); err == nil {
		t.Fatal("expected error for empty kind")
	}
	if _, err := repo.CreateArtifact(context.Background(), domain.Artifact{AthleteID: "id", Kind: "zip"}); err == nil {
		t.Fatal("expected error for empty storage key")
	}
}

func TestArtifactLifecycleIntegration(t *testing.T) {
	pool := testPool(t)
	repo := New(pool)
	athlete := testAthlete(t, repo, "Artifact Test")
	ctx := context.Background()

	job, err := repo.CreateJob(ctx, athlete.ID, "export", nil)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	artifact, err := repo.CreateArtifact(ctx, domain.Artifact{
		AthleteID:   athlete.ID,
		JobID:       job.ID,
		Kind:        "tcx_zip",
		ContentType: "application/zip",
		Filename:    "export-2025.zip",
		StorageKey:  "artifacts/export-2025.zip",
		ByteSize:    1024,
		Checksum:    "sha256:abc123",
		Metadata:    map[string]any{"activity_count": 10},
	})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	if artifact.ID == "" {
		t.Fatal("artifact id is empty")
	}

	got, err := repo.GetArtifact(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if got.Filename != "export-2025.zip" {
		t.Fatalf("filename = %q, want export-2025.zip", got.Filename)
	}
	if got.ByteSize != 1024 {
		t.Fatalf("byte_size = %d, want 1024", got.ByteSize)
	}
	if got.JobID != job.ID {
		t.Fatalf("job_id = %q, want %q", got.JobID, job.ID)
	}
	if v, ok := got.Metadata["activity_count"]; !ok || v == nil {
		t.Fatalf("metadata missing activity_count: %v", got.Metadata)
	}

	// Link artifact back to the job.
	if err := repo.UpdateJob(ctx, job.ID, JobUpdate{
		Status:     domain.JobSucceeded,
		ArtifactID: artifact.ID,
	}); err != nil {
		t.Fatalf("UpdateJob with artifact: %v", err)
	}

	updated, err := repo.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob after artifact link: %v", err)
	}
	if updated.ArtifactID != artifact.ID {
		t.Fatalf("artifact_id = %q, want %q", updated.ArtifactID, artifact.ID)
	}
}
