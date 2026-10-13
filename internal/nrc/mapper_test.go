package nrc

import (
	"math"
	"testing"
	"time"

	"athlete-log/internal/domain"
)

func TestMapNRCActivityMapsSummaryFieldsAndTags(t *testing.T) {
	start := mustEpochMs(t, "2025-09-15T10:00:00Z")
	activity := Activity{
		ID:               "run-123",
		Type:             "run",
		AppID:            "nrc-ios",
		StartEpochMs:     start,
		EndEpochMs:       start + int64(time.Hour/time.Millisecond),
		LastModified:     start + int64((time.Hour+time.Minute)/time.Millisecond),
		ActiveDurationMs: int((55 * time.Minute) / time.Millisecond),
		DeleteIndicator:  true,
		Tags: ActivityTags{
			ComNikeName: "Morning run",
			Location:    "Outdoor",
			ShoeID:      "pegasus",
			Terrain:     "road",
		},
		Summaries: []Summary{
			{Metric: "distance", Summary: "total", Value: 10},
			{Metric: "calories", Summary: "total", Value: 601.4},
			{Metric: "heart_rate", Summary: "mean", Value: 142.5},
			{Metric: "heart_rate", Summary: "max", Value: 166},
			{Metric: "speed", Summary: "mean", Value: 12},
			{Metric: "pace", Summary: "mean", Value: 5},
			{Metric: "ascent", Summary: "total", Value: 85},
		},
		Metrics: []Metric{
			{Type: "latitude", Values: []MetricValue{{StartEpochMs: start, Value: 52.1}}},
			{Type: "longitude", Values: []MetricValue{{StartEpochMs: start, Value: 21.1}}},
		},
	}

	mapped, err := MapNRCActivity(&activity)
	if err != nil {
		t.Fatalf("MapNRCActivity() error = %v", err)
	}

	if mapped.Ref.Source != domain.SourceNRC {
		t.Fatalf("source = %q, want %q", mapped.Ref.Source, domain.SourceNRC)
	}
	if mapped.Ref.SourceActivityID != "run-123" {
		t.Fatalf("source activity id = %q", mapped.Ref.SourceActivityID)
	}
	if !mapped.Ref.Tombstone {
		t.Fatal("ref tombstone = false, want true")
	}
	if mapped.Activity.SportType != domain.SportRun {
		t.Fatalf("sport type = %q, want %q", mapped.Activity.SportType, domain.SportRun)
	}
	if mapped.Activity.Name != "Morning run" {
		t.Fatalf("name = %q", mapped.Activity.Name)
	}
	if got := derefFloat(mapped.Activity.DistanceMeters); got != 10000 {
		t.Fatalf("distance meters = %.3f, want 10000", got)
	}
	if got := derefInt(mapped.Activity.Calories); got != 601 {
		t.Fatalf("calories = %d, want 601", got)
	}
	if got := derefFloat(mapped.Activity.AverageSpeedMetersPerSecond); math.Abs(got-3.333333) > 0.000001 {
		t.Fatalf("average speed = %.6f m/s, want 3.333333", got)
	}
	if got := derefFloat(mapped.Activity.AveragePaceSecondsPerKM); got != 300 {
		t.Fatalf("average pace = %.3f seconds/km, want 300", got)
	}
	if got := derefFloat(mapped.Activity.ElevationGainMeters); got != 85 {
		t.Fatalf("elevation gain = %.3f, want 85", got)
	}
	if mapped.Activity.Indoor {
		t.Fatal("indoor = true, want false")
	}
	assertTag(t, mapped.Activity.Tags, "com.nike.name", "Morning run")
	assertTag(t, mapped.Activity.Tags, "shoe_id", "pegasus")
}

func TestMapNRCActivityPreservesUnionMetricTimeline(t *testing.T) {
	start := mustEpochMs(t, "2025-09-15T10:00:00Z")
	activity := Activity{
		ID:           "run-with-samples",
		Type:         "run",
		StartEpochMs: start,
		EndEpochMs:   start + 20000,
		Summaries:    []Summary{{Metric: "distance", Summary: "total", Value: 0.03}},
		Metrics: []Metric{
			{Type: "latitude", Values: []MetricValue{
				{StartEpochMs: start, Value: 52.1},
				{StartEpochMs: start + 10000, Value: 52.2},
			}},
			{Type: "longitude", Values: []MetricValue{
				{StartEpochMs: start, Value: 21.1},
				{StartEpochMs: start + 10000, Value: 21.2},
			}},
			{Type: "distance", Unit: "KM", Values: []MetricValue{
				{StartEpochMs: start, Value: 0},
				{StartEpochMs: start + 10000, Value: 0.03},
			}},
			{Type: "heart_rate", Unit: "BPM", Values: []MetricValue{
				{StartEpochMs: start + 5000, Value: 140},
				{StartEpochMs: start + 15000, Value: 151},
			}},
			{Type: "speed", Unit: "KMH", Values: []MetricValue{
				{StartEpochMs: start + 5000, Value: 10.8},
			}},
		},
	}

	mapped, err := MapNRCActivity(&activity)
	if err != nil {
		t.Fatalf("MapNRCActivity() error = %v", err)
	}

	samples := mapped.Activity.Samples
	if len(samples) != 4 {
		t.Fatalf("len(samples) = %d, want 4", len(samples))
	}
	if !samples[0].HasLocation() {
		t.Fatal("first sample has no location")
	}
	if got := derefFloat(samples[0].DistanceMeters); got != 0 {
		t.Fatalf("first sample distance = %.3f, want 0", got)
	}
	if samples[1].HasLocation() {
		t.Fatal("heart-rate-only sample unexpectedly has location")
	}
	if got := derefFloat(samples[1].HeartRate); got != 140 {
		t.Fatalf("heart rate sample = %.3f, want 140", got)
	}
	if got := derefFloat(samples[1].SpeedMetersPerSecond); math.Abs(got-3) > 0.000001 {
		t.Fatalf("speed sample = %.6f m/s, want 3", got)
	}
	if got := derefFloat(samples[2].DistanceMeters); got != 30 {
		t.Fatalf("third sample distance = %.3f, want 30", got)
	}
}

func TestMapNRCActivityMarksIndoorWithoutRouteButKeepsSamples(t *testing.T) {
	start := mustEpochMs(t, "2025-09-15T10:00:00Z")
	activity := Activity{
		ID:           "indoor-run",
		Type:         "run",
		StartEpochMs: start,
		Tags:         ActivityTags{Location: "Indoor"},
		Metrics: []Metric{
			{Type: "heart_rate", Unit: "BPM", Values: []MetricValue{
				{StartEpochMs: start + 5000, Value: 135},
				{StartEpochMs: start + 10000, Value: 141},
			}},
		},
	}

	mapped, err := MapNRCActivity(&activity)
	if err != nil {
		t.Fatalf("MapNRCActivity() error = %v", err)
	}
	if !mapped.Activity.Indoor {
		t.Fatal("indoor = false, want true")
	}
	if len(mapped.Activity.Samples) != 2 {
		t.Fatalf("len(samples) = %d, want 2", len(mapped.Activity.Samples))
	}
	if got := derefFloat(mapped.Activity.Samples[0].HeartRate); got != 135 {
		t.Fatalf("first heart rate = %.3f, want 135", got)
	}
}

func TestMapNRCActivityRejectsMissingIdentity(t *testing.T) {
	_, err := MapNRCActivity(&Activity{StartEpochMs: 1})
	if err == nil {
		t.Fatal("MapNRCActivity() error = nil, want missing id error")
	}
}

func assertTag(t *testing.T, tags []domain.ActivityTag, key, value string) {
	t.Helper()
	for _, tag := range tags {
		if tag.Key == key {
			if tag.Value != value {
				t.Fatalf("tag %q = %q, want %q", key, tag.Value, value)
			}
			return
		}
	}
	t.Fatalf("tag %q not found", key)
}

func derefFloat(value *float64) float64 {
	if value == nil {
		return math.NaN()
	}
	return *value
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func mustEpochMs(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time.Parse(%q) error = %v", value, err)
	}
	return parsed.UnixMilli()
}
