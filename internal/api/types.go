package api

import (
	"time"

	"athlete-log/internal/domain"
)

// ActivityListItem is the serialised shape for a single activity in a list response.
type ActivityListItem struct {
	ID                      string   `json:"id"`
	SportType               string   `json:"sport_type"`
	Name                    string   `json:"name,omitempty"`
	StartAt                 string   `json:"start_at"`
	EndAt                   string   `json:"end_at,omitempty"`
	ElapsedMs               *int64   `json:"elapsed_ms,omitempty"`
	MovingMs                *int64   `json:"moving_ms,omitempty"`
	DistanceMeters          *float64 `json:"distance_meters,omitempty"`
	ElevationGainMeters     *float64 `json:"elevation_gain_meters,omitempty"`
	ElevationLossMeters     *float64 `json:"elevation_loss_meters,omitempty"`
	AverageHeartRate        *float64 `json:"average_heart_rate,omitempty"`
	MaxHeartRate            *float64 `json:"max_heart_rate,omitempty"`
	AverageSpeedMPS         *float64 `json:"average_speed_meters_per_second,omitempty"`
	AveragePaceSecsPerKM    *float64 `json:"average_pace_seconds_per_km,omitempty"`
	Calories                *int     `json:"calories,omitempty"`
	Indoor                  bool     `json:"indoor,omitempty"`
}

// ActivityDetailResponse is the full activity response including laps and tags.
type ActivityDetailResponse struct {
	ActivityListItem
	Notes string        `json:"notes,omitempty"`
	Laps  []LapResponse `json:"laps"`
	Tags  []TagResponse `json:"tags"`
}

// LapResponse is the serialised shape for a single lap.
type LapResponse struct {
	Index            int      `json:"index"`
	StartAt          string   `json:"start_at"`
	EndAt            string   `json:"end_at,omitempty"`
	DurationMs       *int64   `json:"duration_ms,omitempty"`
	DistanceMeters   *float64 `json:"distance_meters,omitempty"`
	Calories         *int     `json:"calories,omitempty"`
	AverageHeartRate *float64 `json:"average_heart_rate,omitempty"`
	MaxHeartRate     *float64 `json:"max_heart_rate,omitempty"`
}

// TagResponse is the serialised shape for an activity tag.
type TagResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// SamplePoint is the serialised shape for a single time-series sample.
type SamplePoint struct {
	SampleAt             string   `json:"sample_at"`
	ElapsedMs            *int64   `json:"elapsed_ms,omitempty"`
	Latitude             *float64 `json:"latitude,omitempty"`
	Longitude            *float64 `json:"longitude,omitempty"`
	DistanceMeters       *float64 `json:"distance_meters,omitempty"`
	ElevationMeters      *float64 `json:"elevation_meters,omitempty"`
	HeartRate            *float64 `json:"heart_rate,omitempty"`
	SpeedMetersPerSecond *float64 `json:"speed_meters_per_second,omitempty"`
	Cadence              *float64 `json:"cadence,omitempty"`
	PowerWatts           *float64 `json:"power_watts,omitempty"`
	TemperatureCelsius   *float64 `json:"temperature_celsius,omitempty"`
}

// V1ActivityListResponse is the paginated activity list response.
type V1ActivityListResponse struct {
	Activities []ActivityListItem `json:"activities"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// V1SourceResponse extends the source connection with last sync job info.
type V1SourceResponse struct {
	ID                string         `json:"id"`
	Source            string         `json:"source"`
	Status            string         `json:"status"`
	ExternalAthleteID string         `json:"external_athlete_id,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
	LastSyncStatus    string         `json:"last_sync_status,omitempty"`
	LastSyncAt        string         `json:"last_sync_at,omitempty"`
}

// StatsResponse is the response shape for GET /api/v1/stats.
type StatsResponse struct {
	TotalActivities     int                `json:"total_activities"`
	TotalDistanceMeters float64            `json:"total_distance_meters"`
	TotalElapsedMs      int64              `json:"total_elapsed_ms"`
	TotalCalories       int                `json:"total_calories,omitempty"`
	AvgPaceSecondsPerKM *float64           `json:"avg_pace_seconds_per_km,omitempty"`
	AvgHeartRate        *float64           `json:"avg_heart_rate,omitempty"`
	BySport             []SportStatsItem   `json:"by_sport"`
}

// SportStatsItem is one row of the by-sport breakdown inside StatsResponse.
type SportStatsItem struct {
	SportType      string  `json:"sport_type"`
	ActivityCount  int     `json:"activity_count"`
	DistanceMeters float64 `json:"distance_meters"`
}

// TimeseriesPoint is one bucket in the timeseries response.
type TimeseriesPoint struct {
	PeriodStart    string   `json:"period_start"`
	DistanceMeters float64  `json:"distance_meters"`
	ActivityCount  int      `json:"activity_count"`
	AvgPace        *float64 `json:"avg_pace_seconds_per_km,omitempty"`
	AvgHeartRate   *float64 `json:"avg_heart_rate,omitempty"`
}

// --- Mapping helpers ---

func domainActivityToListItem(a domain.Activity) ActivityListItem {
	item := ActivityListItem{
		ID:                   a.ID,
		SportType:            string(a.SportType),
		Name:                 a.Name,
		StartAt:              a.StartAt.UTC().Format(time.RFC3339),
		DistanceMeters:       a.DistanceMeters,
		ElevationGainMeters:  a.ElevationGainMeters,
		ElevationLossMeters:  a.ElevationLossMeters,
		AverageHeartRate:     a.AverageHeartRate,
		MaxHeartRate:         a.MaxHeartRate,
		AverageSpeedMPS:      a.AverageSpeedMetersPerSecond,
		AveragePaceSecsPerKM: a.AveragePaceSecondsPerKM,
		Calories:             a.Calories,
		Indoor:               a.Indoor,
	}
	if a.EndAt != nil {
		item.EndAt = a.EndAt.UTC().Format(time.RFC3339)
	}
	if a.ElapsedDuration != nil {
		ms := a.ElapsedDuration.Milliseconds()
		item.ElapsedMs = &ms
	}
	if a.MovingDuration != nil {
		ms := a.MovingDuration.Milliseconds()
		item.MovingMs = &ms
	}
	return item
}

func domainActivityToDetail(a domain.Activity) ActivityDetailResponse {
	detail := ActivityDetailResponse{
		ActivityListItem: domainActivityToListItem(a),
		Notes:            a.Notes,
		Laps:             make([]LapResponse, 0, len(a.Laps)),
		Tags:             make([]TagResponse, 0, len(a.Tags)),
	}
	for _, lap := range a.Laps {
		lr := LapResponse{
			Index:            lap.LapIndex,
			StartAt:          lap.StartAt.UTC().Format(time.RFC3339),
			DistanceMeters:   lap.DistanceMeters,
			Calories:         lap.Calories,
			AverageHeartRate: lap.AverageHeartRate,
			MaxHeartRate:     lap.MaxHeartRate,
		}
		if lap.EndAt != nil {
			lr.EndAt = lap.EndAt.UTC().Format(time.RFC3339)
		}
		if lap.Duration != nil {
			ms := lap.Duration.Milliseconds()
			lr.DurationMs = &ms
		}
		detail.Laps = append(detail.Laps, lr)
	}
	for _, tag := range a.Tags {
		detail.Tags = append(detail.Tags, TagResponse{Key: tag.Key, Value: tag.Value})
	}
	return detail
}

func domainSampleToPoint(s domain.ActivitySample) SamplePoint {
	sp := SamplePoint{
		SampleAt:             s.SampleAt.UTC().Format(time.RFC3339),
		Latitude:             s.Latitude,
		Longitude:            s.Longitude,
		DistanceMeters:       s.DistanceMeters,
		ElevationMeters:      s.ElevationMeters,
		HeartRate:            s.HeartRate,
		SpeedMetersPerSecond: s.SpeedMetersPerSecond,
		Cadence:              s.Cadence,
		PowerWatts:           s.PowerWatts,
		TemperatureCelsius:   s.TemperatureCelsius,
	}
	if s.Elapsed != nil {
		ms := s.Elapsed.Milliseconds()
		sp.ElapsedMs = &ms
	}
	return sp
}
