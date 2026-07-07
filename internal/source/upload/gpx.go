package upload

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"time"

	"athlete-log/internal/domain"
)

type gpxFile struct {
	XMLName  xml.Name    `xml:"gpx"`
	Metadata gpxMetadata `xml:"metadata"`
	Tracks   []gpxTrack  `xml:"trk"`
}

type gpxMetadata struct {
	Name string    `xml:"name"`
	Time time.Time `xml:"time"`
}

type gpxTrack struct {
	Name     string       `xml:"name"`
	Type     string       `xml:"type"`
	Segments []gpxSegment `xml:"trkseg"`
}

type gpxSegment struct {
	Points []gpxPoint `xml:"trkpt"`
}

type gpxPoint struct {
	Lat        float64       `xml:"lat,attr"`
	Lon        float64       `xml:"lon,attr"`
	Ele        float64       `xml:"ele"`
	Time       time.Time     `xml:"time"`
	Extensions gpxExtensions `xml:"extensions"`
}

// gpxExtensions captures heart rate and cadence from common GPX extension formats
// (Garmin TrackPointExtension and others). Go's xml decoder matches local names
// ignoring namespace prefixes, so gpxtpx:hr matches the hr path.
type gpxExtensions struct {
	HR  float64 `xml:"TrackPointExtension>hr"`
	Cad float64 `xml:"TrackPointExtension>cad"`
}

func ParseGPX(_ context.Context, r io.Reader) (domain.SourceActivity, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return domain.SourceActivity{}, fmt.Errorf("read GPX: %w", err)
	}

	var gpx gpxFile
	if err := xml.Unmarshal(data, &gpx); err != nil {
		return domain.SourceActivity{}, fmt.Errorf("parse GPX: %w", err)
	}

	var allPoints []gpxPoint
	var trackName, trackType string
	for _, trk := range gpx.Tracks {
		if trackName == "" {
			trackName = trk.Name
		}
		if trackType == "" {
			trackType = trk.Type
		}
		for _, seg := range trk.Segments {
			allPoints = append(allPoints, seg.Points...)
		}
	}
	if len(allPoints) == 0 {
		return domain.SourceActivity{}, fmt.Errorf("GPX file contains no track points")
	}

	name := trackName
	if name == "" {
		name = gpx.Metadata.Name
	}

	act := domain.Activity{
		Name:      name,
		SportType: gpxTypeToDomain(trackType),
	}

	first := allPoints[0]
	last := allPoints[len(allPoints)-1]
	act.StartAt = first.Time
	if !last.Time.IsZero() && last.Time.After(first.Time) {
		end := last.Time
		act.EndAt = &end
		elapsed := end.Sub(first.Time)
		act.ElapsedDuration = &elapsed
	}

	// Compute cumulative distance and elevation from track points.
	var totalDist, elevGain, elevLoss float64
	for i := 1; i < len(allPoints); i++ {
		totalDist += haversineMeters(allPoints[i-1].Lat, allPoints[i-1].Lon, allPoints[i].Lat, allPoints[i].Lon)
		diff := allPoints[i].Ele - allPoints[i-1].Ele
		if diff > 0 {
			elevGain += diff
		} else {
			elevLoss -= diff
		}
	}
	if totalDist > 0 {
		act.DistanceMeters = &totalDist
	}
	if elevGain > 0 {
		act.ElevationGainMeters = &elevGain
	}
	if elevLoss > 0 {
		act.ElevationLossMeters = &elevLoss
	}

	var hrSum float64
	hrCount := 0
	runningDist := 0.0
	act.Samples = make([]domain.ActivitySample, 0, len(allPoints))
	for i, pt := range allPoints {
		s := domain.ActivitySample{SampleAt: pt.Time}
		if !act.StartAt.IsZero() && !pt.Time.IsZero() {
			e := pt.Time.Sub(act.StartAt)
			s.Elapsed = &e
		}
		lat := pt.Lat
		lon := pt.Lon
		s.Latitude = &lat
		s.Longitude = &lon
		if pt.Ele != 0 {
			ele := pt.Ele
			s.ElevationMeters = &ele
		}
		if i > 0 {
			runningDist += haversineMeters(allPoints[i-1].Lat, allPoints[i-1].Lon, pt.Lat, pt.Lon)
			dist := runningDist
			s.DistanceMeters = &dist
		}
		if pt.Extensions.HR > 0 {
			hr := pt.Extensions.HR
			s.HeartRate = &hr
			hrSum += hr
			hrCount++
		}
		if pt.Extensions.Cad > 0 {
			cad := pt.Extensions.Cad
			s.Cadence = &cad
		}
		act.Samples = append(act.Samples, s)
	}

	// Derive per-sample speed using a ±3-point centered-difference window for smoothing.
	const spdW = 3
	for i := range act.Samples {
		lo, hi := i-spdW, i+spdW
		if lo < 0 {
			lo = 0
		}
		if hi >= len(allPoints) {
			hi = len(allPoints) - 1
		}
		if lo >= hi {
			continue
		}
		var segDist float64
		for j := lo + 1; j <= hi; j++ {
			segDist += haversineMeters(allPoints[j-1].Lat, allPoints[j-1].Lon, allPoints[j].Lat, allPoints[j].Lon)
		}
		dtSec := allPoints[hi].Time.Sub(allPoints[lo].Time).Seconds()
		if dtSec > 0 && segDist > 0 {
			speed := segDist / dtSec
			act.Samples[i].SpeedMetersPerSecond = &speed
		}
	}

	if hrCount > 0 {
		avg := hrSum / float64(hrCount)
		act.AverageHeartRate = &avg
	}
	if act.ElapsedDuration != nil && act.DistanceMeters != nil {
		secs := act.ElapsedDuration.Seconds()
		if secs > 0 {
			speed := *act.DistanceMeters / secs
			act.AverageSpeedMetersPerSecond = &speed
			if speed > 0 {
				pace := 1000.0 / speed
				act.AveragePaceSecondsPerKM = &pace
			}
		}
	}

	return domain.SourceActivity{Activity: act}, nil
}

func gpxTypeToDomain(t string) domain.SportType {
	switch t {
	case "running", "9": // Garmin type 9 = running
		return domain.SportRun
	case "cycling", "biking", "1": // Garmin type 1 = cycling
		return domain.SportRide
	case "swimming":
		return domain.SportSwim
	case "walking":
		return domain.SportWalk
	case "hiking":
		return domain.SportHike
	default:
		return domain.SportUnknown
	}
}

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371000.0
	lat1R := lat1 * math.Pi / 180
	lat2R := lat2 * math.Pi / 180
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1R)*math.Cos(lat2R)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadius * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
