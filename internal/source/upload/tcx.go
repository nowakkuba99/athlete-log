package upload

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	tcxlib "github.com/philhofer/tcx"

	"athlete-log/internal/domain"
)

func ParseTCX(_ context.Context, r io.Reader) (domain.SourceActivity, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return domain.SourceActivity{}, fmt.Errorf("read TCX: %w", err)
	}
	var db tcxlib.TCXDB
	if err := xml.Unmarshal(data, &db); err != nil {
		return domain.SourceActivity{}, fmt.Errorf("parse TCX: %w", err)
	}
	if db.Acts == nil || len(db.Acts.Act) == 0 {
		return domain.SourceActivity{}, fmt.Errorf("TCX file contains no activities")
	}
	return domain.SourceActivity{Activity: activityFromTCX(db.Acts.Act[0])}, nil
}

func activityFromTCX(a tcxlib.Activity) domain.Activity {
	act := domain.Activity{
		SportType: tcxSportToDomain(a.Sport),
		StartAt:   a.Id,
	}

	var totalDist, totalTime float64
	var totalCals float64
	var hrSum float64
	hrCount := 0

	for lapIdx, lap := range a.Laps {
		startTime := parseTCXTime(lap.Start)
		if act.StartAt.IsZero() && !startTime.IsZero() {
			act.StartAt = startTime
		}

		totalTime += lap.TotalTime
		totalDist += lap.Dist
		totalCals += lap.Calories

		dl := domain.ActivityLap{LapIndex: lapIdx, StartAt: startTime}
		d := time.Duration(lap.TotalTime * float64(time.Second))
		dl.Duration = &d
		dist := lap.Dist
		dl.DistanceMeters = &dist
		if lap.Calories > 0 {
			cal := int(lap.Calories)
			dl.Calories = &cal
		}
		if lap.AvgHr > 0 {
			dl.AverageHeartRate = &lap.AvgHr
		}
		if lap.MaxHr > 0 {
			dl.MaxHeartRate = &lap.MaxHr
		}
		act.Laps = append(act.Laps, dl)

		if lap.Trk == nil {
			continue
		}
		for _, pt := range lap.Trk.Pt {
			s := domain.ActivitySample{SampleAt: pt.Time}
			if !act.StartAt.IsZero() && !pt.Time.IsZero() {
				e := pt.Time.Sub(act.StartAt)
				s.Elapsed = &e
			}
			if pt.Lat != 0 || pt.Long != 0 {
				lat := pt.Lat
				lng := pt.Long
				s.Latitude = &lat
				s.Longitude = &lng
			}
			if pt.Alt != 0 {
				alt := pt.Alt
				s.ElevationMeters = &alt
			}
			if pt.Dist > 0 {
				dist := pt.Dist
				s.DistanceMeters = &dist
			}
			if pt.HR > 0 {
				hr := pt.HR
				s.HeartRate = &hr
				hrSum += hr
				hrCount++
			}
			if pt.Cad > 0 {
				cad := pt.Cad
				s.Cadence = &cad
			}
			if pt.Speed > 0 {
				spd := pt.Speed
				s.SpeedMetersPerSecond = &spd
			}
			if pt.Power > 0 {
				pwr := pt.Power
				s.PowerWatts = &pwr
			}
			act.Samples = append(act.Samples, s)
		}
	}

	if totalDist > 0 {
		act.DistanceMeters = &totalDist
	}
	if totalTime > 0 {
		d := time.Duration(totalTime * float64(time.Second))
		act.ElapsedDuration = &d
		if !act.StartAt.IsZero() {
			end := act.StartAt.Add(d)
			act.EndAt = &end
		}
	}
	if totalCals > 0 {
		cal := int(totalCals)
		act.Calories = &cal
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

	return act
}

func parseTCXTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func tcxSportToDomain(sport string) domain.SportType {
	switch strings.ToLower(sport) {
	case "running":
		return domain.SportRun
	case "biking", "cycling":
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
