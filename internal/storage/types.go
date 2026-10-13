package storage

import (
	"context"
	"encoding/json"
	"time"

	"athlete-log/internal/domain"
	"athlete-log/internal/postgres"
)

type (
	ListActivitiesFilter = postgres.ListActivitiesFilter
	StatsFilter          = postgres.StatsFilter
	Stats                = postgres.Stats
	SportStats           = postgres.SportStats
	TimeseriesPoint      = postgres.TimeseriesPoint
	Streaks              = postgres.Streaks
	Bests                = postgres.Bests
	BestPeriod           = postgres.BestPeriod
	PMCPoint             = postgres.PMCPoint
	YearReview           = postgres.YearReview
	MonthStats           = postgres.MonthStats
	ActivitySummary      = postgres.ActivitySummary
	GearSummary          = postgres.GearSummary
	ActivityWeather      = postgres.ActivityWeather
	SimilarActivity      = postgres.SimilarActivity
	SyncedFile           = postgres.SyncedFile
	JobUpdate            = postgres.JobUpdate
	Dashboard            = postgres.Dashboard
	Widget               = postgres.Widget
	BestEffort           = postgres.BestEffort
	Gear                 = postgres.Gear
)

type JobRepository interface {
	UpdateJob(ctx context.Context, id string, update JobUpdate) error
}

type FolderRepository interface {
	IsSyncedBySHA256(ctx context.Context, connID, sha256 string) (bool, error)
	UpsertSyncedFile(ctx context.Context, f SyncedFile) error
	UpsertSourceActivity(ctx context.Context, athleteID string, value domain.SourceActivity) (domain.SourceActivity, error)
}

type Repository interface {
	JobRepository
	FolderRepository

	EnsureDefaultAthlete(ctx context.Context) (domain.Athlete, error)
	CreateAthlete(ctx context.Context, displayName string) (domain.Athlete, error)
	GetAthlete(ctx context.Context, athleteID string) (domain.Athlete, error)
	UpdateAthlete(ctx context.Context, athleteID, displayName, email, startedAt string) (domain.Athlete, error)
	SetAthleteAvatar(ctx context.Context, athleteID, path string) error

	UpsertSourceConnection(ctx context.Context, athleteID string, value domain.SourceConnection) (domain.SourceConnection, error)
	GetSourceConnection(ctx context.Context, id string) (domain.SourceConnection, error)
	ListSourceConnections(ctx context.Context, athleteID string) ([]domain.SourceConnection, error)
	ListSourceConnectionsByType(ctx context.Context, athleteID string, sourceType domain.SourceType) ([]domain.SourceConnection, error)
	DeleteSourceConnection(ctx context.Context, id string) error

	ListActivities(ctx context.Context, athleteID string, filter ListActivitiesFilter) ([]domain.Activity, string, error)
	GetActivity(ctx context.Context, athleteID, id string) (domain.Activity, error)
	GetActivitySamples(ctx context.Context, activityID string, maxPoints int) ([]domain.ActivitySample, error)
	GetActivityLaps(ctx context.Context, activityID string) ([]domain.ActivityLap, error)
	FindActivityNearTime(ctx context.Context, athleteID string, startAt time.Time, sportType, excludeID string) *domain.Activity
	DeleteActivity(ctx context.Context, athleteID, id string) error
	UpdateActivityName(ctx context.Context, athleteID, id, name string) error
	UpdateActivityNotes(ctx context.Context, athleteID, id, notes string) error
	SetActivityTag(ctx context.Context, athleteID, activityID, key, value string) error
	DeleteActivityTag(ctx context.Context, athleteID, activityID, key string) error

	GetActivityWeather(ctx context.Context, activityID string) (*ActivityWeather, error)
	UpsertActivityWeather(ctx context.Context, activityID string, w ActivityWeather) error
	GetActivityStartInfo(ctx context.Context, activityID string) (startAt time.Time, lat, lng float64, ok bool, err error)
	GetSimilarActivities(ctx context.Context, athleteID, activityID string) ([]SimilarActivity, error)
	GetHeatmapGeoJSON(ctx context.Context, athleteID, sportType, from, to string) ([]byte, error)

	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error

	CreateJob(ctx context.Context, athleteID, jobType string, params map[string]any) (domain.Job, error)
	GetJob(ctx context.Context, id string) (domain.Job, error)
	ListJobs(ctx context.Context, athleteID string, limit int) ([]domain.Job, error)
	GetLastJobForSourceConnection(ctx context.Context, athleteID, connID string) (domain.Job, bool, error)
	GetLastJobsForSourceConnections(ctx context.Context, athleteID string, connIDs []string) (map[string]domain.Job, error)
	AppendJobEvent(ctx context.Context, jobID, level, message string, data map[string]any) error
	CreateArtifact(ctx context.Context, artifact domain.Artifact) (domain.Artifact, error)
	GetArtifact(ctx context.Context, id string) (domain.Artifact, error)

	GetStats(ctx context.Context, athleteID string, filter StatsFilter) (Stats, error)
	GetTimeseries(ctx context.Context, athleteID, granularity string, filter StatsFilter) ([]TimeseriesPoint, error)
	GetStreaks(ctx context.Context, athleteID string) (Streaks, error)
	GetBests(ctx context.Context, athleteID string) (Bests, error)
	GetPMC(ctx context.Context, athleteID string, from, to time.Time, hrMax int) ([]PMCPoint, error)
	GetYearReview(ctx context.Context, athleteID string, year int) (YearReview, error)

	ComputeAndStoreBestEfforts(ctx context.Context, athleteID, sportType string) error
	GetBestEfforts(ctx context.Context, athleteID, sportType string) ([]BestEffort, error)

	CreateDashboard(ctx context.Context, athleteID, name string) (Dashboard, error)
	ListDashboards(ctx context.Context, athleteID string) ([]Dashboard, error)
	GetDashboard(ctx context.Context, athleteID, id string) (Dashboard, error)
	UpdateDashboard(ctx context.Context, athleteID, id, name string) (Dashboard, error)
	DeleteDashboard(ctx context.Context, athleteID, id string) error
	CreateWidget(ctx context.Context, dashboardID, athleteID, widgetType string, config, position json.RawMessage) (Widget, error)
	ListWidgets(ctx context.Context, dashboardID, athleteID string) ([]Widget, error)
	UpdateWidget(ctx context.Context, widgetID, dashboardID, athleteID string, config, position json.RawMessage) (Widget, error)
	DeleteWidget(ctx context.Context, widgetID, dashboardID, athleteID string) error

	CreateGear(ctx context.Context, athleteID, name, gearType, brandModel string, purchaseDate *string, alertMeters *int) (Gear, error)
	ListGear(ctx context.Context, athleteID string, includeRetired bool) ([]Gear, error)
	UpdateGear(ctx context.Context, athleteID, id, name, gearType, brandModel string, purchaseDate *string, alertMeters *int, retire bool) (Gear, error)
	DeleteGear(ctx context.Context, athleteID, id string) error
	GetActivityGear(ctx context.Context, activityID string) ([]Gear, error)
	SetActivityGear(ctx context.Context, activityID string, gearIDs []string) error

	ListSyncedFiles(ctx context.Context, connID string, limit int) ([]SyncedFile, error)
}
