package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"athlete-log/internal/domain"
	"athlete-log/internal/storage"
)

func TestRepositorySettingsAthletesAndSourceConnections(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)

	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		t.Fatalf("EnsureDefaultAthlete() error = %v", err)
	}
	if athlete.ID == "" || athlete.DisplayName != "Local Athlete" {
		t.Fatalf("EnsureDefaultAthlete() = %#v", athlete)
	}

	if err := repo.SetSetting(ctx, "hr_max", "190"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}
	if got, err := repo.GetSetting(ctx, "hr_max"); err != nil || got != "190" {
		t.Fatalf("GetSetting() = %q, %v; want 190, nil", got, err)
	}

	conn, err := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source:            domain.SourceNRC,
		ExternalAthleteID: "nike-user",
		SecretKey:         "token",
		Metadata:          map[string]any{"scope": "run"},
	})
	if err != nil {
		t.Fatalf("UpsertSourceConnection() error = %v", err)
	}
	if conn.ID == "" || conn.Status != "active" {
		t.Fatalf("UpsertSourceConnection() = %#v", conn)
	}

	byType, err := repo.ListSourceConnectionsByType(ctx, athlete.ID, domain.SourceNRC)
	if err != nil {
		t.Fatalf("ListSourceConnectionsByType() error = %v", err)
	}
	if len(byType) != 1 || byType[0].ExternalAthleteID != "nike-user" {
		t.Fatalf("ListSourceConnectionsByType() = %#v", byType)
	}

	conn, err = repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{
		Source:            domain.SourceNRC,
		ExternalAthleteID: "nike-user-2",
		SecretKey:         "token-2",
	})
	if err != nil {
		t.Fatalf("second UpsertSourceConnection() error = %v", err)
	}
	list, err := repo.ListSourceConnections(ctx, athlete.ID)
	if err != nil {
		t.Fatalf("ListSourceConnections() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != conn.ID || list[0].ExternalAthleteID != "nike-user-2" {
		t.Fatalf("ListSourceConnections() = %#v", list)
	}
}

func TestRepositoryActivitiesStatsWeatherAndDelete(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		t.Fatalf("EnsureDefaultAthlete() error = %v", err)
	}
	conn, err := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{Source: domain.SourceFITUpload})
	if err != nil {
		t.Fatalf("UpsertSourceConnection() error = %v", err)
	}

	start := time.Date(2026, 1, 2, 8, 30, 0, 0, time.UTC)
	elapsed := 30 * time.Minute
	distance := 5000.0
	pace := 360.0
	hr := 150.0
	calories := 400
	lat1, lng1 := 52.2297, 21.0122
	lat2, lng2 := 52.2302, 21.015

	inserted, err := repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
		Ref: domain.SourceActivityRef{
			SourceConnectionID: conn.ID,
			Source:             domain.SourceFITUpload,
			SourceActivityID:   "fit-1",
		},
		Activity: domain.Activity{
			SportType:               domain.SportRun,
			Name:                    "Morning Run",
			Status:                  "synced",
			StartAt:                 start,
			ElapsedDuration:         &elapsed,
			DistanceMeters:          &distance,
			AveragePaceSecondsPerKM: &pace,
			AverageHeartRate:        &hr,
			Calories:                &calories,
			Samples: []domain.ActivitySample{
				{SampleAt: start, Latitude: &lat1, Longitude: &lng1},
				{SampleAt: start.Add(15 * time.Minute), Latitude: &lat2, Longitude: &lng2, DistanceMeters: &distance, Elapsed: &elapsed},
			},
			Laps: []domain.ActivityLap{
				{LapIndex: 1, StartAt: start, Duration: &elapsed, DistanceMeters: &distance},
			},
			Tags: []domain.ActivityTag{{Key: "surface", Value: "road"}},
		},
	})
	if err != nil {
		t.Fatalf("UpsertSourceActivity() error = %v", err)
	}
	if inserted.Activity.ID == "" || inserted.Ref.ID == "" {
		t.Fatalf("UpsertSourceActivity() = %#v", inserted)
	}

	page, next, err := repo.ListActivities(ctx, athlete.ID, storage.ListActivitiesFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListActivities() error = %v", err)
	}
	if next != "" || len(page) != 1 || page[0].Name != "Morning Run" {
		t.Fatalf("ListActivities() = %#v, next %q", page, next)
	}

	got, err := repo.GetActivity(ctx, athlete.ID, inserted.Activity.ID)
	if err != nil {
		t.Fatalf("GetActivity() error = %v", err)
	}
	if len(got.Laps) != 1 || len(got.Tags) != 1 || got.Tags[0].Value != "road" {
		t.Fatalf("GetActivity() = %#v", got)
	}

	samples, err := repo.GetActivitySamples(ctx, inserted.Activity.ID, 1)
	if err != nil {
		t.Fatalf("GetActivitySamples() error = %v", err)
	}
	if len(samples) != 1 || !samples[0].SampleAt.Equal(start) {
		t.Fatalf("GetActivitySamples() = %#v", samples)
	}

	near := repo.FindActivityNearTime(ctx, athlete.ID, start.Add(30*time.Second), string(domain.SportRun), "")
	if near == nil || near.ID != inserted.Activity.ID {
		t.Fatalf("FindActivityNearTime() = %#v", near)
	}

	stats, err := repo.GetStats(ctx, athlete.ID, storage.StatsFilter{})
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}
	if stats.TotalActivities != 1 || stats.TotalDistanceMeters != distance || stats.TotalCalories != calories {
		t.Fatalf("GetStats() = %#v", stats)
	}

	okStart, gotLat, gotLng, ok, err := repo.GetActivityStartInfo(ctx, inserted.Activity.ID)
	if err != nil || !ok || !okStart.Equal(start) || gotLat == 0 || gotLng == 0 {
		t.Fatalf("GetActivityStartInfo() = %v %v %v %v %v", okStart, gotLat, gotLng, ok, err)
	}
	temp := 8.5
	if err := repo.UpsertActivityWeather(ctx, inserted.Activity.ID, storage.ActivityWeather{TemperatureC: &temp}); err != nil {
		t.Fatalf("UpsertActivityWeather() error = %v", err)
	}
	weather, err := repo.GetActivityWeather(ctx, inserted.Activity.ID)
	if err != nil || weather == nil || weather.TemperatureC == nil || *weather.TemperatureC != temp {
		t.Fatalf("GetActivityWeather() = %#v, %v", weather, err)
	}

	if err := repo.DeleteActivity(ctx, athlete.ID, inserted.Activity.ID); err != nil {
		t.Fatalf("DeleteActivity() error = %v", err)
	}
	if _, err := repo.GetActivity(ctx, athlete.ID, inserted.Activity.ID); err == nil {
		t.Fatal("GetActivity() after delete succeeded; want error")
	}
}

func TestRepositoryJobsArtifactsAndSyncedFiles(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		t.Fatalf("EnsureDefaultAthlete() error = %v", err)
	}
	conn, err := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{Source: domain.SourceFolderWatch})
	if err != nil {
		t.Fatalf("UpsertSourceConnection() error = %v", err)
	}

	job, err := repo.CreateJob(ctx, athlete.ID, "folder_watch_sync", map[string]any{"source_connection_id": conn.ID})
	if err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	now := time.Now().UTC()
	total := 3
	if err := repo.UpdateJob(ctx, job.ID, storage.JobUpdate{
		Status:          domain.JobSucceeded,
		Phase:           "done",
		Message:         "imported",
		ProgressCurrent: 3,
		ProgressTotal:   &total,
		Result:          map[string]any{"imported": 1},
		StartedAt:       &now,
		CompletedAt:     &now,
	}); err != nil {
		t.Fatalf("UpdateJob() error = %v", err)
	}
	gotJob, err := repo.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if gotJob.Status != domain.JobSucceeded || gotJob.ProgressTotal == nil || *gotJob.ProgressTotal != total {
		t.Fatalf("GetJob() = %#v", gotJob)
	}
	last, ok, err := repo.GetLastJobForSourceConnection(ctx, athlete.ID, conn.ID)
	if err != nil || !ok || last.ID != job.ID {
		t.Fatalf("GetLastJobForSourceConnection() = %#v, %v, %v", last, ok, err)
	}

	artifact, err := repo.CreateArtifact(ctx, domain.Artifact{
		AthleteID:   athlete.ID,
		JobID:       job.ID,
		Kind:        "zip",
		ContentType: "application/zip",
		StorageKey:  "exports/a.zip",
		ByteSize:    10,
		Metadata:    map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatalf("CreateArtifact() error = %v", err)
	}
	gotArtifact, err := repo.GetArtifact(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact() error = %v", err)
	}
	if gotArtifact.StorageKey != artifact.StorageKey || gotArtifact.JobID != job.ID {
		t.Fatalf("GetArtifact() = %#v", gotArtifact)
	}

	if err := repo.UpsertSyncedFile(ctx, storage.SyncedFile{
		SourceConnectionID: conn.ID,
		FilePath:           "/tmp/run.fit",
		SHA256:             "abc123",
		Status:             "imported",
	}); err != nil {
		t.Fatalf("UpsertSyncedFile() error = %v", err)
	}
	synced, err := repo.IsSyncedBySHA256(ctx, conn.ID, "abc123")
	if err != nil || !synced {
		t.Fatalf("IsSyncedBySHA256() = %v, %v", synced, err)
	}
	files, err := repo.ListSyncedFiles(ctx, conn.ID, 10)
	if err != nil {
		t.Fatalf("ListSyncedFiles() error = %v", err)
	}
	if len(files) != 1 || files[0].FilePath != "/tmp/run.fit" {
		t.Fatalf("ListSyncedFiles() = %#v", files)
	}
}

func TestRepositoryDashboardsAndWidgets(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		t.Fatalf("EnsureDefaultAthlete() error = %v", err)
	}

	dashboard, err := repo.CreateDashboard(ctx, athlete.ID, "Home")
	if err != nil {
		t.Fatalf("CreateDashboard() error = %v", err)
	}
	widget, err := repo.CreateWidget(
		ctx,
		dashboard.ID,
		athlete.ID,
		"weekly_distance",
		json.RawMessage(`{"sport_type":"run"}`),
		json.RawMessage(`{"x":0,"y":0,"w":4,"h":3}`),
	)
	if err != nil {
		t.Fatalf("CreateWidget() error = %v", err)
	}

	dashboard, err = repo.GetDashboard(ctx, athlete.ID, dashboard.ID)
	if err != nil {
		t.Fatalf("GetDashboard() error = %v", err)
	}
	if dashboard.WidgetCount != 1 {
		t.Fatalf("GetDashboard().WidgetCount = %d; want 1", dashboard.WidgetCount)
	}

	widgets, err := repo.ListWidgets(ctx, dashboard.ID, athlete.ID)
	if err != nil {
		t.Fatalf("ListWidgets() error = %v", err)
	}
	if len(widgets) != 1 || string(widgets[0].Config) != `{"sport_type":"run"}` {
		t.Fatalf("ListWidgets() = %#v", widgets)
	}

	updated, err := repo.UpdateWidget(ctx, widget.ID, dashboard.ID, athlete.ID, json.RawMessage(`{"sport_type":"ride"}`), nil)
	if err != nil {
		t.Fatalf("UpdateWidget() error = %v", err)
	}
	if string(updated.Config) != `{"sport_type":"ride"}` || string(updated.Position) != `{"x":0,"y":0,"w":4,"h":3}` {
		t.Fatalf("UpdateWidget() = %#v", updated)
	}

	dashboard, err = repo.UpdateDashboard(ctx, athlete.ID, dashboard.ID, "Training")
	if err != nil {
		t.Fatalf("UpdateDashboard() error = %v", err)
	}
	if dashboard.Name != "Training" {
		t.Fatalf("UpdateDashboard().Name = %q; want Training", dashboard.Name)
	}

	if err := repo.DeleteWidget(ctx, widget.ID, dashboard.ID, athlete.ID); err != nil {
		t.Fatalf("DeleteWidget() error = %v", err)
	}
	if err := repo.DeleteDashboard(ctx, athlete.ID, dashboard.ID); err != nil {
		t.Fatalf("DeleteDashboard() error = %v", err)
	}
}

func TestRepositoryGearBestEffortsAndYearReview(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		t.Fatalf("EnsureDefaultAthlete() error = %v", err)
	}
	conn, err := repo.UpsertSourceConnection(ctx, athlete.ID, domain.SourceConnection{Source: domain.SourceFITUpload})
	if err != nil {
		t.Fatalf("UpsertSourceConnection() error = %v", err)
	}

	start := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	zeroDuration := time.Duration(0)
	fiveMinutes := 5 * time.Minute
	twentyMinutes := 20 * time.Minute
	zeroDistance := 0.0
	oneKM := 1000.0
	fiveKM := 5000.0
	elevation := 42.0
	inserted, err := repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
		Ref: domain.SourceActivityRef{
			SourceConnectionID: conn.ID,
			Source:             domain.SourceFITUpload,
			SourceActivityID:   "fit-records",
		},
		Activity: domain.Activity{
			SportType:           domain.SportRun,
			Name:                "Record Run",
			Status:              "synced",
			StartAt:             start,
			ElapsedDuration:     &twentyMinutes,
			DistanceMeters:      &fiveKM,
			ElevationGainMeters: &elevation,
			Samples: []domain.ActivitySample{
				{SampleAt: start, Elapsed: &zeroDuration, DistanceMeters: &zeroDistance},
				{SampleAt: start.Add(fiveMinutes), Elapsed: &fiveMinutes, DistanceMeters: &oneKM},
				{SampleAt: start.Add(twentyMinutes), Elapsed: &twentyMinutes, DistanceMeters: &fiveKM},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpsertSourceActivity() error = %v", err)
	}

	alert := 600000
	purchaseDate := "2026-01-01"
	gear, err := repo.CreateGear(ctx, athlete.ID, "Daily Trainer", "shoe", "Fast 1", &purchaseDate, &alert)
	if err != nil {
		t.Fatalf("CreateGear() error = %v", err)
	}
	if err := repo.SetActivityGear(ctx, inserted.Activity.ID, []string{gear.ID}); err != nil {
		t.Fatalf("SetActivityGear() error = %v", err)
	}

	gearList, err := repo.ListGear(ctx, athlete.ID, false)
	if err != nil {
		t.Fatalf("ListGear() error = %v", err)
	}
	if len(gearList) != 1 || gearList[0].TotalDistanceMeters != fiveKM || gearList[0].ActivityCount != 1 {
		t.Fatalf("ListGear() = %#v", gearList)
	}
	activityGear, err := repo.GetActivityGear(ctx, inserted.Activity.ID)
	if err != nil {
		t.Fatalf("GetActivityGear() error = %v", err)
	}
	if len(activityGear) != 1 || activityGear[0].ID != gear.ID {
		t.Fatalf("GetActivityGear() = %#v", activityGear)
	}

	if err := repo.ComputeAndStoreBestEfforts(ctx, athlete.ID, string(domain.SportRun)); err != nil {
		t.Fatalf("ComputeAndStoreBestEfforts() error = %v", err)
	}
	efforts, err := repo.GetBestEfforts(ctx, athlete.ID, string(domain.SportRun))
	if err != nil {
		t.Fatalf("GetBestEfforts() error = %v", err)
	}
	if len(efforts) == 0 || efforts[0].ActivityID != inserted.Activity.ID {
		t.Fatalf("GetBestEfforts() = %#v", efforts)
	}

	review, err := repo.GetYearReview(ctx, athlete.ID, 2026)
	if err != nil {
		t.Fatalf("GetYearReview() error = %v", err)
	}
	if review.MostUsedGear == nil || review.MostUsedGear.ID != gear.ID || review.PRsSet == 0 {
		t.Fatalf("GetYearReview() = %#v", review)
	}

	updated, err := repo.UpdateGear(ctx, athlete.ID, gear.ID, "Daily Trainer 2", "shoe", "Fast 2", &purchaseDate, &alert, true)
	if err != nil {
		t.Fatalf("UpdateGear() error = %v", err)
	}
	if updated.RetiredAt == nil {
		t.Fatalf("UpdateGear().RetiredAt = nil; want timestamp")
	}
	activeGear, err := repo.ListGear(ctx, athlete.ID, false)
	if err != nil {
		t.Fatalf("ListGear(active) error = %v", err)
	}
	if len(activeGear) != 0 {
		t.Fatalf("ListGear(active) = %#v; want no active gear", activeGear)
	}
}

func newTestRepository(t *testing.T) *Repository {
	t.Helper()
	repo, err := New(filepath.Join(t.TempDir(), "athlete-log.db"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := repo.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return repo
}

func TestLegacyDatabaseActivitiesSurviveRestart(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	legacyPath := filepath.Join(dataDir, "athlete-vault.db")

	repo, err := New(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	athlete, err := repo.EnsureDefaultAthlete(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.UpsertSourceActivity(ctx, athlete.ID, domain.SourceActivity{
		Ref: domain.SourceActivityRef{
			Source:           domain.SourceFITUpload,
			SourceActivityID: "run-before-restart",
		},
		Activity: domain.Activity{
			SportType: domain.SportRun,
			Name:      "Saved run",
			StartAt:   time.Date(2026, 1, 2, 8, 30, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	currentRepo, err := New(filepath.Join(dataDir, "athlete-log.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := currentRepo.Close(); err != nil {
		t.Fatal(err)
	}

	cfg, err := storage.ConfigFromEnv(func(string) string { return "" }, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	restartedRepo, err := New(cfg.SQLitePath)
	if err != nil {
		t.Fatal(err)
	}
	defer restartedRepo.Close()
	restartedAthlete, err := restartedRepo.EnsureDefaultAthlete(ctx)
	if err != nil {
		t.Fatal(err)
	}
	activities, _, err := restartedRepo.ListActivities(ctx, restartedAthlete.ID, storage.ListActivitiesFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(activities) != 1 || activities[0].Name != "Saved run" {
		t.Fatalf("activities after restart = %#v, want saved run", activities)
	}
}
