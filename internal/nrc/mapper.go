package nrc

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"athlete-log/internal/domain"
)

var errNRCActivityRequired = errors.New("nrc activity is required")

func MapNRCActivity(activity *Activity) (*domain.SourceActivity, error) {
	if activity == nil {
		return nil, errNRCActivityRequired
	}
	if strings.TrimSpace(activity.ID) == "" {
		return nil, errors.New("nrc activity id is required")
	}
	if activity.StartEpochMs <= 0 {
		return nil, errors.New("nrc activity start time is required")
	}

	start := time.UnixMilli(activity.StartEpochMs).UTC()
	end := timePtrFromEpochMillis(activity.EndEpochMs)
	lastModified := timePtrFromEpochMillis(activity.LastModified)
	samples := mapNRCSamples(activity, start)
	tags := mapNRCTags(activity.Tags)

	mapped := &domain.SourceActivity{
		Ref: domain.SourceActivityRef{
			Source:               domain.SourceNRC,
			SourceActivityID:     activity.ID,
			SourceLastModifiedAt: lastModified,
			Tombstone:            activity.DeleteIndicator,
		},
		Activity: domain.Activity{
			SportType:       mapNRCSportType(activity.Type),
			Name:            strings.TrimSpace(activity.Tags.ComNikeName),
			AppID:           strings.TrimSpace(activity.AppID),
			StartAt:         start,
			EndAt:           end,
			LastModifiedAt:  lastModified,
			ElapsedDuration: elapsedDuration(start, end),
			MovingDuration:  durationPtrFromMillis(activity.ActiveDurationMs),
			Indoor:          isNRCIndoor(activity, samples),
			DeleteIndicator: activity.DeleteIndicator,
			Samples:         samples,
			Tags:            tags,
		},
	}

	applyNRCSummaries(activity, &mapped.Activity)
	return mapped, nil
}

func applyNRCSummaries(source *Activity, target *domain.Activity) {
	if value, ok := nrcSummaryValue(source, "distance", "total"); ok {
		target.DistanceMeters = floatPtr(value * 1000)
	}
	if value, ok := nrcSummaryValue(source, "ascent", "total"); ok {
		target.ElevationGainMeters = floatPtr(value)
	}
	if value, ok := nrcSummaryValue(source, "elevation_gain", "total"); ok && target.ElevationGainMeters == nil {
		target.ElevationGainMeters = floatPtr(value)
	}
	if value, ok := nrcSummaryValue(source, "descent", "total"); ok {
		target.ElevationLossMeters = floatPtr(value)
	}
	if value, ok := nrcSummaryValue(source, "elevation_loss", "total"); ok && target.ElevationLossMeters == nil {
		target.ElevationLossMeters = floatPtr(value)
	}
	if value, ok := nrcSummaryValue(source, "calories", "total"); ok {
		target.Calories = intPtr(int(math.Round(value)))
	}
	if value, ok := nrcSummaryValue(source, "heart_rate", "mean"); ok {
		target.AverageHeartRate = floatPtr(value)
	}
	if value, ok := nrcSummaryValue(source, "heart_rate", "max"); ok {
		target.MaxHeartRate = floatPtr(value)
	}
	if value, ok := nrcSummaryValue(source, "speed", "mean"); ok {
		target.AverageSpeedMetersPerSecond = floatPtr(nrcSpeedToMetersPerSecond(value, "KMH"))
	}
	if value, ok := nrcSummaryValue(source, "pace", "mean"); ok {
		target.AveragePaceSecondsPerKM = floatPtr(value * 60)
	} else if target.AverageSpeedMetersPerSecond != nil && *target.AverageSpeedMetersPerSecond > 0 {
		target.AveragePaceSecondsPerKM = floatPtr(1000 / *target.AverageSpeedMetersPerSecond)
	}
}

func mapNRCSamples(activity *Activity, start time.Time) []domain.ActivitySample {
	if activity == nil {
		return nil
	}

	latitude := metricValuesByTime(activity, "latitude", identityUnit)
	longitude := metricValuesByTime(activity, "longitude", identityUnit)
	distance := nrcDistanceValuesByTime(activity)
	elevation := metricValuesByTime(activity, "elevation", nrcElevationToMeters)
	heartRate := metricValuesByTime(activity, "heart_rate", identityUnit)
	speed := metricValuesByTime(activity, "speed", func(value float64, unit string) float64 {
		return nrcSpeedToMetersPerSecond(value, unit)
	})
	cadence := metricValuesByTime(activity, "cadence", identityUnit)
	power := metricValuesByTime(activity, "power", identityUnit)
	temperature := metricValuesByTime(activity, "temperature", nrcTemperatureToCelsius)

	timestamps := uniqueMetricTimestamps(latitude, longitude, distance, elevation, heartRate, speed, cadence, power, temperature)
	samples := make([]domain.ActivitySample, 0, len(timestamps))
	for _, epochMs := range timestamps {
		sampleAt := time.UnixMilli(epochMs).UTC()
		sample := domain.ActivitySample{
			SampleAt: sampleAt,
			Elapsed:  elapsedDuration(start, &sampleAt),
		}
		if value, ok := latitude[epochMs]; ok {
			sample.Latitude = floatPtr(value)
		}
		if value, ok := longitude[epochMs]; ok {
			sample.Longitude = floatPtr(value)
		}
		if value, ok := distance[epochMs]; ok {
			sample.DistanceMeters = floatPtr(value)
		}
		if value, ok := elevation[epochMs]; ok {
			sample.ElevationMeters = floatPtr(value)
		}
		if value, ok := heartRate[epochMs]; ok {
			sample.HeartRate = floatPtr(value)
		}
		if value, ok := speed[epochMs]; ok {
			sample.SpeedMetersPerSecond = floatPtr(value)
		}
		if value, ok := cadence[epochMs]; ok {
			sample.Cadence = floatPtr(value)
		}
		if value, ok := power[epochMs]; ok {
			sample.PowerWatts = floatPtr(value)
		}
		if value, ok := temperature[epochMs]; ok {
			sample.TemperatureCelsius = floatPtr(value)
		}
		samples = append(samples, sample)
	}
	return samples
}

func metricValuesByTime(activity *Activity, metricType string, convert func(float64, string) float64) map[int64]float64 {
	metric, ok := metricByType(activity, metricType)
	if !ok {
		return nil
	}
	values := make(map[int64]float64, len(metric.Values))
	for _, value := range metric.Values {
		if value.StartEpochMs == 0 {
			continue
		}
		values[value.StartEpochMs] = convert(value.Value, metric.Unit)
	}
	return values
}

func nrcDistanceValuesByTime(activity *Activity) map[int64]float64 {
	metric, ok := metricByType(activity, "distance")
	if !ok || len(metric.Values) == 0 {
		return nil
	}

	values := append([]MetricValue(nil), metric.Values...)
	sort.Slice(values, func(i, j int) bool {
		return values[i].StartEpochMs < values[j].StartEpochMs
	})

	summaryMeters := 0.0
	if value, ok := nrcSummaryValue(activity, "distance", "total"); ok {
		summaryMeters = value * 1000
	}

	converted := make([]MetricValue, 0, len(values))
	for _, value := range values {
		if value.StartEpochMs == 0 {
			continue
		}
		converted = append(converted, MetricValue{
			StartEpochMs: value.StartEpochMs,
			EndEpochMs:   value.EndEpochMs,
			Value:        nrcDistanceToMeters(value.Value, metric.Unit),
		})
	}
	if len(converted) == 0 {
		return nil
	}

	cumulative := true
	if summaryMeters > 0 {
		last := converted[len(converted)-1].Value
		cumulative = math.Abs(last-summaryMeters)/summaryMeters < 0.05
	}

	out := make(map[int64]float64, len(converted))
	running := 0.0
	for _, value := range converted {
		if cumulative {
			out[value.StartEpochMs] = value.Value
			continue
		}
		running += value.Value
		out[value.StartEpochMs] = running
	}
	return out
}

func uniqueMetricTimestamps(metricMaps ...map[int64]float64) []int64 {
	seen := make(map[int64]struct{})
	for _, metricMap := range metricMaps {
		for epochMs := range metricMap {
			if epochMs == 0 {
				continue
			}
			seen[epochMs] = struct{}{}
		}
	}
	timestamps := make([]int64, 0, len(seen))
	for epochMs := range seen {
		timestamps = append(timestamps, epochMs)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })
	return timestamps
}

func mapNRCTags(tags ActivityTags) []domain.ActivityTag {
	candidates := []struct {
		key   string
		value string
	}{
		{"com.nike.running.recordingappversion", tags.ComNikeRunningRecordingappversion},
		{"location", tags.Location},
		{"com.nike.running.syncsource", tags.ComNikeRunningSyncsource},
		{"com.nike.weather", tags.ComNikeWeather},
		{"com.nike.running.goaltype", tags.ComNikeRunningGoaltype},
		{"com.nike.running.syncappversion", tags.ComNikeRunningSyncappversion},
		{"shoe_id", tags.ShoeID},
		{"com.nike.weathersource", tags.ComNikeWeathersource},
		{"com.nike.name", tags.ComNikeName},
		{"com.nike.running.recordingsource", tags.ComNikeRunningRecordingsource},
		{"com.nike.temperature", tags.ComNikeTemperature},
		{"com.nike.running.originalactivityid", tags.ComNikeRunningOriginalactivityid},
		{"rpe", tags.Rpe},
		{"terrain", tags.Terrain},
	}

	mapped := make([]domain.ActivityTag, 0, len(candidates))
	for _, candidate := range candidates {
		value := strings.TrimSpace(candidate.value)
		if value == "" {
			continue
		}
		mapped = append(mapped, domain.ActivityTag{Key: candidate.key, Value: value})
	}
	return mapped
}

func mapNRCSportType(raw string) domain.SportType {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "run", "jogging":
		return domain.SportRun
	case "":
		return domain.SportUnknown
	default:
		return domain.SportType(normalized)
	}
}

func isNRCIndoor(activity *Activity, samples []domain.ActivitySample) bool {
	location := strings.ToLower(strings.TrimSpace(activity.Tags.Location))
	if strings.Contains(location, "indoor") || strings.Contains(location, "treadmill") {
		return true
	}
	for _, sample := range samples {
		if sample.HasLocation() {
			return false
		}
	}
	return true
}

func nrcSummaryValue(activity *Activity, metric string, summary string) (float64, bool) {
	if activity == nil {
		return 0, false
	}
	for _, item := range activity.Summaries {
		if item.Metric == metric && item.Summary == summary {
			return item.Value, true
		}
	}
	return 0, false
}

func timePtrFromEpochMillis(epochMs int64) *time.Time {
	if epochMs <= 0 {
		return nil
	}
	value := time.UnixMilli(epochMs).UTC()
	return &value
}

func durationPtrFromMillis(ms int) *time.Duration {
	if ms <= 0 {
		return nil
	}
	value := time.Duration(ms) * time.Millisecond
	return &value
}

func elapsedDuration(start time.Time, end *time.Time) *time.Duration {
	if start.IsZero() || end == nil || end.Before(start) {
		return nil
	}
	value := end.Sub(start)
	return &value
}

func identityUnit(value float64, _ string) float64 { return value }

func nrcDistanceToMeters(value float64, unit string) float64 {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "KM":
		return value * 1000
	case "M", "":
		return value
	case "MI":
		return value * 1609.344
	default:
		return value
	}
}

func nrcElevationToMeters(value float64, unit string) float64 {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "FT":
		return value * 0.3048
	default:
		return nrcDistanceToMeters(value, unit)
	}
}

func nrcSpeedToMetersPerSecond(value float64, unit string) float64 {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "KMH", "":
		return value / 3.6
	case "MPH":
		return value * 0.44704
	case "M/S", "MPS":
		return value
	default:
		return value
	}
}

func nrcTemperatureToCelsius(value float64, unit string) float64 {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "F":
		return (value - 32) * 5 / 9
	case "K":
		return value - 273.15
	default:
		return value
	}
}

func floatPtr(value float64) *float64 { return &value }
func intPtr(value int) *int           { return &value }
