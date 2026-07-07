package nrc

import (
	"bytes"
	"math"
	"testing"
)

func TestRouteAndTCXExportFromSampleActivity(t *testing.T) {
	activity := sampleActivity()

	route, err := RoutePoints(&activity)
	if err != nil {
		t.Fatalf("RoutePoints() error = %v", err)
	}
	if len(route) != 3 {
		t.Fatalf("len(route) = %d, want %d", len(route), 3)
	}

	distances := cumulativeDistanceValues(&activity)
	if len(distances) == 0 {
		t.Fatal("cumulativeDistanceValues() returned no points")
	}
	wantDistance := summaryDistanceMeters(&activity)
	gotDistance := distances[len(distances)-1].Value
	if math.Abs(gotDistance-wantDistance) > 0.001 {
		t.Fatalf("last cumulative distance = %.3f, want %.3f", gotDistance, wantDistance)
	}

	tcxBytes, err := ExportActivityToTCX(&activity)
	if err != nil {
		t.Fatalf("ExportActivityToTCX() error = %v", err)
	}
	for _, needle := range [][]byte{
		[]byte("<TrainingCenterDatabase>"),
		[]byte("<Trackpoint>"),
		[]byte("<DistanceMeters>"),
		[]byte("<HeartRateBpm>"),
	} {
		if !bytes.Contains(tcxBytes, needle) {
			t.Fatalf("TCX output does not contain %q", needle)
		}
	}
	if trackpoints := bytes.Count(tcxBytes, []byte("<Trackpoint>")); trackpoints <= len(route) {
		t.Fatalf("TCX trackpoints = %d, want more than route points %d", trackpoints, len(route))
	}
}

func TestTCXExportAllowsNonGPSActivity(t *testing.T) {
	activity := Activity{
		ID:               "indoor-run",
		Type:             "run",
		StartEpochMs:     1748441777960,
		EndEpochMs:       1748445377960,
		ActiveDurationMs: 3600000,
		Summaries: []Summary{
			{Metric: "distance", Summary: "total", Value: 10},
			{Metric: "heart_rate", Summary: "mean", Value: 140},
			{Metric: "calories", Summary: "total", Value: 600},
		},
	}

	tcxBytes, err := ExportActivityToTCX(&activity)
	if err != nil {
		t.Fatalf("ExportActivityToTCX(non-GPS) error = %v", err)
	}
	if !bytes.Contains(tcxBytes, []byte("<DistanceMeters>10000</DistanceMeters>")) {
		t.Fatalf("TCX output does not contain lap distance: %s", tcxBytes)
	}
	if bytes.Contains(tcxBytes, []byte("<Trackpoint>")) {
		t.Fatalf("non-GPS export should not emit zero-coordinate trackpoints: %s", tcxBytes)
	}
}

func sampleActivity() Activity {
	start := int64(1748441777960)
	return Activity{
		ID:               "sample-activity",
		Type:             "run",
		StartEpochMs:     start,
		EndEpochMs:       start + 30000,
		ActiveDurationMs: 30000,
		Summaries: []Summary{
			{Metric: "distance", Summary: "total", Value: 0.03},
			{Metric: "heart_rate", Summary: "mean", Value: 142},
			{Metric: "heart_rate", Summary: "max", Value: 151},
			{Metric: "calories", Summary: "total", Value: 12},
		},
		Metrics: []Metric{
			{Type: "latitude", Unit: "DEGREE", Values: []MetricValue{
				{StartEpochMs: start, EndEpochMs: start, Value: 52.2297},
				{StartEpochMs: start + 10000, EndEpochMs: start + 10000, Value: 52.2298},
				{StartEpochMs: start + 20000, EndEpochMs: start + 20000, Value: 52.2299},
			}},
			{Type: "longitude", Unit: "DEGREE", Values: []MetricValue{
				{StartEpochMs: start, EndEpochMs: start, Value: 21.0122},
				{StartEpochMs: start + 10000, EndEpochMs: start + 10000, Value: 21.0124},
				{StartEpochMs: start + 20000, EndEpochMs: start + 20000, Value: 21.0127},
			}},
			{Type: "distance", Unit: "KM", Values: []MetricValue{
				{StartEpochMs: start, EndEpochMs: start, Value: 0},
				{StartEpochMs: start + 10000, EndEpochMs: start + 10000, Value: 0.015},
				{StartEpochMs: start + 20000, EndEpochMs: start + 20000, Value: 0.03},
			}},
			{Type: "heart_rate", Unit: "BPM", Values: []MetricValue{
				{StartEpochMs: start + 5000, EndEpochMs: start + 5000, Value: 137},
				{StartEpochMs: start + 15000, EndEpochMs: start + 15000, Value: 145},
				{StartEpochMs: start + 25000, EndEpochMs: start + 25000, Value: 151},
			}},
			{Type: "elevation", Unit: "M", Values: []MetricValue{
				{StartEpochMs: start, EndEpochMs: start, Value: 110},
				{StartEpochMs: start + 20000, EndEpochMs: start + 20000, Value: 115},
			}},
			{Type: "speed", Unit: "KMH", Values: []MetricValue{
				{StartEpochMs: start + 5000, EndEpochMs: start + 5000, Value: 10.8},
				{StartEpochMs: start + 15000, EndEpochMs: start + 15000, Value: 11.2},
			}},
		},
	}
}
