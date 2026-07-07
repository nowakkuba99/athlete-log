package export

import (
	"fmt"
	"strings"
	"time"

	tcxlib "github.com/philhofer/tcx"

	"athlete-log/internal/domain"
)

// TCXFilename returns a stable filename for a TCX export of the given activity.
func TCXFilename(a domain.Activity) string {
	start := a.StartAt.UTC().Format("2006-01-02")
	name := a.Name
	if name == "" {
		name = string(a.SportType)
	}
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, name)
	if name == "" {
		name = "activity"
	}
	return fmt.Sprintf("%s-%s.tcx", start, name)
}

// ExportToTCX converts a canonical activity and its samples to TCX bytes.
// The samples slice should be pre-populated (e.g. from GetActivitySamples).
func ExportToTCX(a domain.Activity) ([]byte, error) {
	track := tcxlib.Track{}
	for _, s := range a.Samples {
		tp := tcxlib.Trackpoint{Time: s.SampleAt}
		if s.Latitude != nil {
			tp.Lat = *s.Latitude
		}
		if s.Longitude != nil {
			tp.Long = *s.Longitude
		}
		if s.ElevationMeters != nil {
			tp.Alt = *s.ElevationMeters
		}
		if s.DistanceMeters != nil {
			tp.Dist = *s.DistanceMeters
		}
		if s.HeartRate != nil {
			tp.HR = *s.HeartRate
		}
		if s.SpeedMetersPerSecond != nil {
			tp.Speed = *s.SpeedMetersPerSecond
		}
		if s.Cadence != nil {
			tp.Cad = *s.Cadence
		}
		if s.PowerWatts != nil {
			tp.Power = *s.PowerWatts
		}
		track.Pt = append(track.Pt, tp)
	}

	var totalTime float64
	if a.ElapsedDuration != nil {
		totalTime = a.ElapsedDuration.Seconds()
	} else if a.EndAt != nil {
		totalTime = a.EndAt.Sub(a.StartAt).Seconds()
	}

	var dist float64
	if a.DistanceMeters != nil {
		dist = *a.DistanceMeters
	}

	var calories, avgHR, maxHR float64
	if a.Calories != nil {
		calories = float64(*a.Calories)
	}
	if a.AverageHeartRate != nil {
		avgHR = *a.AverageHeartRate
	}
	if a.MaxHeartRate != nil {
		maxHR = *a.MaxHeartRate
	}

	lap := tcxlib.Lap{
		Start:         a.StartAt.UTC().Format(time.RFC3339),
		TotalTime:     totalTime,
		Dist:          dist,
		Calories:      calories,
		AvgHr:         avgHR,
		MaxHr:         maxHR,
		Intensity:     "Active",
		TriggerMethod: "Manual",
		Trk:           &track,
	}

	db := tcxlib.TCXDB{
		Acts: &tcxlib.Activities{
			Act: []tcxlib.Activity{{
				Sport: domainSportToTCX(a.SportType),
				Id:    a.StartAt.UTC(),
				Laps:  []tcxlib.Lap{lap},
			}},
		},
		Auth: tcxlib.Author{Name: "athlete-log"},
	}
	return tcxlib.ToBytes(db)
}

func domainSportToTCX(s domain.SportType) string {
	switch s {
	case domain.SportRun:
		return "Running"
	case domain.SportRide:
		return "Biking"
	case domain.SportSwim:
		return "Swimming"
	case domain.SportWalk:
		return "Walking"
	case domain.SportHike:
		return "Hiking"
	default:
		return "Other"
	}
}
