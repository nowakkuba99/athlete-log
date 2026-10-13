package upload_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"athlete-log/internal/domain"
	"athlete-log/internal/source/upload"
)

func TestParseFIT(t *testing.T) {
	data, err := os.ReadFile("testdata/Activity.fit")
	if err != nil {
		t.Fatal(err)
	}
	sa, err := upload.Parse(context.Background(), data, "Activity.fit")
	if err != nil {
		t.Fatalf("ParseFIT: %v", err)
	}
	if sa.Activity.StartAt.IsZero() {
		t.Error("StartAt is zero")
	}
	if len(sa.Activity.Samples) == 0 {
		t.Error("expected samples, got none")
	}
	if sa.Activity.SportType == "" {
		t.Error("SportType is empty")
	}
	t.Logf("FIT: sport=%s start=%s samples=%d laps=%d",
		sa.Activity.SportType, sa.Activity.StartAt, len(sa.Activity.Samples), len(sa.Activity.Laps))
}

func TestParseGPX(t *testing.T) {
	gpxData := `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" xmlns="http://www.topografix.com/GPX/1/1"
     xmlns:gpxtpx="http://www.garmin.com/xmlschemas/TrackPointExtension/v1">
  <metadata><name>Test Run</name><time>2024-03-01T08:00:00Z</time></metadata>
  <trk>
    <name>Morning Run</name>
    <type>running</type>
    <trkseg>
      <trkpt lat="52.2297" lon="21.0122">
        <ele>100.0</ele>
        <time>2024-03-01T08:00:00Z</time>
        <extensions>
          <gpxtpx:TrackPointExtension>
            <gpxtpx:hr>140</gpxtpx:hr>
            <gpxtpx:cad>80</gpxtpx:cad>
          </gpxtpx:TrackPointExtension>
        </extensions>
      </trkpt>
      <trkpt lat="52.2300" lon="21.0130">
        <ele>101.0</ele>
        <time>2024-03-01T08:01:00Z</time>
        <extensions>
          <gpxtpx:TrackPointExtension>
            <gpxtpx:hr>145</gpxtpx:hr>
            <gpxtpx:cad>82</gpxtpx:cad>
          </gpxtpx:TrackPointExtension>
        </extensions>
      </trkpt>
      <trkpt lat="52.2310" lon="21.0140">
        <ele>102.0</ele>
        <time>2024-03-01T08:02:00Z</time>
        <extensions>
          <gpxtpx:TrackPointExtension>
            <gpxtpx:hr>150</gpxtpx:hr>
          </gpxtpx:TrackPointExtension>
        </extensions>
      </trkpt>
    </trkseg>
  </trk>
</gpx>`

	sa, err := upload.Parse(context.Background(), []byte(gpxData), "run.gpx")
	if err != nil {
		t.Fatalf("ParseGPX: %v", err)
	}
	if sa.Activity.Name != "Morning Run" {
		t.Errorf("Name: got %q, want %q", sa.Activity.Name, "Morning Run")
	}
	if sa.Activity.SportType != domain.SportRun {
		t.Errorf("SportType: got %q, want %q", sa.Activity.SportType, domain.SportRun)
	}
	if sa.Activity.StartAt.IsZero() {
		t.Error("StartAt is zero")
	}
	if len(sa.Activity.Samples) != 3 {
		t.Errorf("samples: got %d, want 3", len(sa.Activity.Samples))
	}
	if sa.Activity.AverageHeartRate == nil {
		t.Error("AverageHeartRate is nil")
	} else if *sa.Activity.AverageHeartRate < 140 || *sa.Activity.AverageHeartRate > 150 {
		t.Errorf("AverageHeartRate out of range: %v", *sa.Activity.AverageHeartRate)
	}
	if sa.Activity.DistanceMeters == nil || *sa.Activity.DistanceMeters <= 0 {
		t.Error("DistanceMeters not computed")
	}
	t.Logf("GPX: sport=%s start=%s dist=%.1fm avgHR=%.1f",
		sa.Activity.SportType, sa.Activity.StartAt,
		derefF(sa.Activity.DistanceMeters), derefF(sa.Activity.AverageHeartRate))
}

func TestParseTCX(t *testing.T) {
	tcxData := `<?xml version="1.0" encoding="UTF-8"?>
<TrainingCenterDatabase xmlns="http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2">
  <Activities>
    <Activity Sport="Running">
      <Id>2024-03-01T09:00:00Z</Id>
      <Lap StartTime="2024-03-01T09:00:00Z">
        <TotalTimeSeconds>600</TotalTimeSeconds>
        <DistanceMeters>1500</DistanceMeters>
        <Calories>80</Calories>
        <AverageHeartRateBpm><Value>155</Value></AverageHeartRateBpm>
        <MaximumHeartRateBpm><Value>170</Value></MaximumHeartRateBpm>
        <Intensity>Active</Intensity>
        <TriggerMethod>Manual</TriggerMethod>
        <Track>
          <Trackpoint>
            <Time>2024-03-01T09:00:00Z</Time>
            <Position><LatitudeDegrees>52.2297</LatitudeDegrees><LongitudeDegrees>21.0122</LongitudeDegrees></Position>
            <AltitudeMeters>100</AltitudeMeters>
            <DistanceMeters>0</DistanceMeters>
            <HeartRateBpm><Value>150</Value></HeartRateBpm>
          </Trackpoint>
          <Trackpoint>
            <Time>2024-03-01T09:10:00Z</Time>
            <Position><LatitudeDegrees>52.2310</LatitudeDegrees><LongitudeDegrees>21.0140</LongitudeDegrees></Position>
            <AltitudeMeters>110</AltitudeMeters>
            <DistanceMeters>1500</DistanceMeters>
            <HeartRateBpm><Value>160</Value></HeartRateBpm>
          </Trackpoint>
        </Track>
      </Lap>
    </Activity>
  </Activities>
</TrainingCenterDatabase>`

	sa, err := upload.Parse(context.Background(), []byte(tcxData), "activity.tcx")
	if err != nil {
		t.Fatalf("ParseTCX: %v", err)
	}
	if sa.Activity.SportType != domain.SportRun {
		t.Errorf("SportType: got %q, want %q", sa.Activity.SportType, domain.SportRun)
	}
	if sa.Activity.DistanceMeters == nil || *sa.Activity.DistanceMeters != 1500 {
		t.Errorf("DistanceMeters: got %v, want 1500", sa.Activity.DistanceMeters)
	}
	if len(sa.Activity.Samples) != 2 {
		t.Errorf("samples: got %d, want 2", len(sa.Activity.Samples))
	}
	if len(sa.Activity.Laps) != 1 {
		t.Errorf("laps: got %d, want 1", len(sa.Activity.Laps))
	}
	t.Logf("TCX: sport=%s start=%s dist=%.1fm samples=%d",
		sa.Activity.SportType, sa.Activity.StartAt,
		derefF(sa.Activity.DistanceMeters), len(sa.Activity.Samples))
}

func TestParseUnsupportedExtension(t *testing.T) {
	_, err := upload.Parse(context.Background(), []byte("data"), "activity.kml")
	if err == nil {
		t.Error("expected error for unsupported extension")
	}
	if !strings.Contains(err.Error(), ".kml") {
		t.Errorf("error should mention extension, got: %v", err)
	}
}

func TestSourceTypeForFilename(t *testing.T) {
	cases := []struct {
		filename string
		want     domain.SourceType
		wantErr  bool
	}{
		{"run.fit", domain.SourceFITUpload, false},
		{"RUN.FIT", domain.SourceFITUpload, false},
		{"track.gpx", domain.SourceGPXUpload, false},
		{"export.tcx", domain.SourceTCXUpload, false},
		{"data.csv", "", true},
	}
	for _, tc := range cases {
		got, err := upload.SourceTypeForFilename(tc.filename)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected error", tc.filename)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.filename, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.filename, got, tc.want)
		}
	}
}

func derefF(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
