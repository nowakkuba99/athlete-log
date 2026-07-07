package upload

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	fitlib "github.com/tormoder/fit"

	"athlete-log/internal/domain"
)

// fitEpoch is the FIT protocol epoch used as a sentinel for "not set" timestamps.
var fitEpoch = time.Date(1989, time.December, 31, 0, 0, 0, 0, time.UTC)

func isFITEpoch(t time.Time) bool {
	return t.IsZero() || t.Equal(fitEpoch)
}

func ParseFIT(_ context.Context, r io.Reader) (domain.SourceActivity, error) {
	f, err := fitlib.Decode(r)
	if err != nil {
		return domain.SourceActivity{}, fmt.Errorf("decode FIT: %w", err)
	}
	activity, err := f.Activity()
	if err != nil {
		return domain.SourceActivity{}, fmt.Errorf("not an activity FIT file: %w", err)
	}
	return domain.SourceActivity{Activity: activityFromFIT(activity)}, nil
}

func activityFromFIT(a *fitlib.ActivityFile) domain.Activity {
	act := domain.Activity{SportType: domain.SportUnknown}

	if len(a.Sessions) > 0 {
		s := a.Sessions[0]
		act.StartAt = s.StartTime
		act.SportType = fitSportToDomain(s.Sport)

		if v := s.GetTotalElapsedTimeScaled(); !math.IsNaN(v) {
			d := time.Duration(v * float64(time.Second))
			act.ElapsedDuration = &d
		}
		if v := s.GetTotalTimerTimeScaled(); !math.IsNaN(v) {
			d := time.Duration(v * float64(time.Second))
			act.MovingDuration = &d
		}
		if v := s.GetTotalDistanceScaled(); !math.IsNaN(v) {
			act.DistanceMeters = &v
		}
		if s.AvgHeartRate != 0xFF {
			v := float64(s.AvgHeartRate)
			act.AverageHeartRate = &v
		}
		if s.MaxHeartRate != 0xFF {
			v := float64(s.MaxHeartRate)
			act.MaxHeartRate = &v
		}
		if v := s.GetAvgSpeedScaled(); !math.IsNaN(v) && v > 0 {
			act.AverageSpeedMetersPerSecond = &v
			pace := 1000.0 / v
			act.AveragePaceSecondsPerKM = &pace
		}
		if s.TotalCalories != 0xFFFF {
			cal := int(s.TotalCalories)
			act.Calories = &cal
		}
		if s.TotalAscent != 0xFFFF {
			gain := float64(s.TotalAscent)
			act.ElevationGainMeters = &gain
		}
		if s.TotalDescent != 0xFFFF {
			loss := float64(s.TotalDescent)
			act.ElevationLossMeters = &loss
		}
		if act.ElapsedDuration != nil {
			end := act.StartAt.Add(*act.ElapsedDuration)
			act.EndAt = &end
		}
	}

	act.Samples = make([]domain.ActivitySample, 0, len(a.Records))
	for _, rec := range a.Records {
		act.Samples = append(act.Samples, fitRecordToSample(rec, act.StartAt))
	}

	// Fallback: compute MaxHR from per-record HR values if the session didn't set it.
	if act.MaxHeartRate == nil {
		var maxHR float64
		for _, rec := range a.Records {
			if rec.HeartRate != 0xFF && float64(rec.HeartRate) > maxHR {
				maxHR = float64(rec.HeartRate)
			}
		}
		if maxHR > 0 {
			act.MaxHeartRate = &maxHR
		}
	}

	act.Laps = make([]domain.ActivityLap, 0, len(a.Laps))
	for i, lap := range a.Laps {
		if l, ok := fitLapToDomain(lap, i, act.StartAt); ok {
			act.Laps = append(act.Laps, l)
		}
	}

	return act
}

func fitRecordToSample(rec *fitlib.RecordMsg, actStart time.Time) domain.ActivitySample {
	s := domain.ActivitySample{SampleAt: rec.Timestamp}
	if !actStart.IsZero() && !rec.Timestamp.IsZero() {
		e := rec.Timestamp.Sub(actStart)
		s.Elapsed = &e
	}
	if !rec.PositionLat.Invalid() && !rec.PositionLong.Invalid() {
		lat := rec.PositionLat.Degrees()
		lng := rec.PositionLong.Degrees()
		s.Latitude = &lat
		s.Longitude = &lng
	}
	if v := rec.GetAltitudeScaled(); !math.IsNaN(v) {
		s.ElevationMeters = &v
	}
	if v := rec.GetDistanceScaled(); !math.IsNaN(v) {
		s.DistanceMeters = &v
	}
	if v := rec.GetSpeedScaled(); !math.IsNaN(v) {
		s.SpeedMetersPerSecond = &v
	}
	if rec.HeartRate != 0xFF {
		v := float64(rec.HeartRate)
		s.HeartRate = &v
	}
	if rec.Cadence != 0xFF {
		v := float64(rec.Cadence)
		s.Cadence = &v
	}
	if rec.Power != 0xFFFF {
		v := float64(rec.Power)
		s.PowerWatts = &v
	}
	if rec.Temperature != 0x7F {
		v := float64(rec.Temperature)
		s.TemperatureCelsius = &v
	}
	return s
}

// fitLapToDomain converts a FIT LapMsg. Returns (lap, false) if the lap has no
// usable start time (e.g. device didn't record it — treat as no lap data).
func fitLapToDomain(lap *fitlib.LapMsg, idx int, actStart time.Time) (domain.ActivityLap, bool) {
	startAt := lap.StartTime
	if isFITEpoch(startAt) {
		// StartTime not set by device — fall back to activity start + elapsed offset.
		if isFITEpoch(actStart) {
			return domain.ActivityLap{}, false
		}
		startAt = actStart
	}

	l := domain.ActivityLap{
		LapIndex: idx,
		StartAt:  startAt,
	}
	if v := lap.GetTotalElapsedTimeScaled(); !math.IsNaN(v) {
		d := time.Duration(v * float64(time.Second))
		l.Duration = &d
		end := startAt.Add(d)
		l.EndAt = &end
	}
	if v := lap.GetTotalDistanceScaled(); !math.IsNaN(v) {
		l.DistanceMeters = &v
	}
	if lap.TotalCalories != 0xFFFF {
		cal := int(lap.TotalCalories)
		l.Calories = &cal
	}
	if lap.AvgHeartRate != 0xFF {
		v := float64(lap.AvgHeartRate)
		l.AverageHeartRate = &v
	}
	if lap.MaxHeartRate != 0xFF {
		v := float64(lap.MaxHeartRate)
		l.MaxHeartRate = &v
	}
	return l, true
}

func fitSportToDomain(s fitlib.Sport) domain.SportType {
	switch s {
	case fitlib.SportRunning:
		return domain.SportRun
	case fitlib.SportCycling:
		return domain.SportRide
	case fitlib.SportSwimming:
		return domain.SportSwim
	case fitlib.SportWalking:
		return domain.SportWalk
	case fitlib.SportHiking:
		return domain.SportHike
	default:
		return domain.SportUnknown
	}
}
