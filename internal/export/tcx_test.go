package export_test

import (
	"encoding/xml"
	"testing"
	"time"

	tcxlib "github.com/philhofer/tcx"

	"athlete-log/internal/domain"
	"athlete-log/internal/export"
)

func TestExportToTCX_roundtrip(t *testing.T) {
	dist := 5000.0
	elapsed := 30 * time.Minute
	avgHR := 155.0
	calories := 350

	act := domain.Activity{
		SportType:       domain.SportRun,
		Name:            "Morning Run",
		StartAt:         time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC),
		DistanceMeters:  &dist,
		ElapsedDuration: &elapsed,
		AverageHeartRate: &avgHR,
		Calories:        &calories,
		Samples: []domain.ActivitySample{
			{
				SampleAt:        time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC),
				Latitude:        pf(52.2297),
				Longitude:       pf(21.0122),
				ElevationMeters: pf(100.0),
				DistanceMeters:  pf(0.0),
				HeartRate:       pf(148.0),
			},
			{
				SampleAt:        time.Date(2024, 3, 1, 8, 15, 0, 0, time.UTC),
				Latitude:        pf(52.2350),
				Longitude:       pf(21.0180),
				ElevationMeters: pf(105.0),
				DistanceMeters:  pf(2500.0),
				HeartRate:       pf(155.0),
			},
			{
				SampleAt:        time.Date(2024, 3, 1, 8, 30, 0, 0, time.UTC),
				Latitude:        pf(52.2400),
				Longitude:       pf(21.0240),
				ElevationMeters: pf(102.0),
				DistanceMeters:  pf(5000.0),
				HeartRate:       pf(162.0),
			},
		},
	}

	tcxBytes, err := export.ExportToTCX(act)
	if err != nil {
		t.Fatalf("ExportToTCX: %v", err)
	}
	if len(tcxBytes) == 0 {
		t.Fatal("TCX output is empty")
	}

	// Parse the generated TCX back.
	var db tcxlib.TCXDB
	if err := xml.Unmarshal(tcxBytes, &db); err != nil {
		t.Fatalf("unmarshal TCX: %v", err)
	}
	if db.Acts == nil || len(db.Acts.Act) == 0 {
		t.Fatal("no activities in TCX")
	}
	parsed := db.Acts.Act[0]
	if parsed.Sport != "Running" {
		t.Errorf("sport: got %q, want %q", parsed.Sport, "Running")
	}
	if len(parsed.Laps) != 1 {
		t.Fatalf("laps: got %d, want 1", len(parsed.Laps))
	}
	lap := parsed.Laps[0]
	if lap.Dist != 5000.0 {
		t.Errorf("distance: got %.1f, want 5000.0", lap.Dist)
	}
	if lap.TotalTime != 1800.0 {
		t.Errorf("total time: got %.1f, want 1800.0", lap.TotalTime)
	}
	if lap.Trk == nil || len(lap.Trk.Pt) != 3 {
		t.Errorf("trackpoints: got %d, want 3", trackpointCount(lap.Trk))
	}
}

func TestTCXFilename(t *testing.T) {
	act := domain.Activity{
		SportType: domain.SportRun,
		Name:      "Morning Run",
		StartAt:   time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC),
	}
	name := export.TCXFilename(act)
	if name != "2024-03-01-morning-run.tcx" {
		t.Errorf("got %q, want %q", name, "2024-03-01-morning-run.tcx")
	}

	// No name falls back to sport type.
	act.Name = ""
	name = export.TCXFilename(act)
	if name != "2024-03-01-run.tcx" {
		t.Errorf("got %q, want %q", name, "2024-03-01-run.tcx")
	}
}

func TestExportToTCX_noSamples(t *testing.T) {
	dist := 10000.0
	act := domain.Activity{
		SportType:      domain.SportRide,
		StartAt:        time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC),
		DistanceMeters: &dist,
	}
	tcxBytes, err := export.ExportToTCX(act)
	if err != nil {
		t.Fatalf("ExportToTCX without samples: %v", err)
	}
	if len(tcxBytes) == 0 {
		t.Fatal("expected non-empty TCX output")
	}
}

func trackpointCount(trk *tcxlib.Track) int {
	if trk == nil {
		return 0
	}
	return len(trk.Pt)
}

func pf(v float64) *float64 { return &v }
