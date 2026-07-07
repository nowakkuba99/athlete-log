package nrc

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	tcxlib "github.com/philhofer/tcx"
)

type RoutePoint struct {
	TimeEpochMs int64   `json:"time_epoch_ms"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

// TCXFilename returns a stable filename for a TCX export of the given activity.
func TCXFilename(activity *Activity) string {
	start := time.UnixMilli(activity.StartEpochMs).UTC().Format("2006-01-02")
	name := activity.Tags.ComNikeName
	if name == "" {
		name = "nike-run"
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
		name = "nike-run"
	}
	return fmt.Sprintf("%s-%s.tcx", start, name)
}

func ExportActivityToTCX(activity *Activity) ([]byte, error) {
	if activity == nil {
		return nil, errors.New("activity is required")
	}

	start := time.UnixMilli(activity.StartEpochMs).UTC()
	track := tcxlib.Track{}
	distanceValues := cumulativeDistanceValues(activity)
	distanceMetric, _ := metricByType(activity, "distance")
	heartRate, _ := metricByType(activity, "heart_rate")
	elevation, _ := metricByType(activity, "elevation")
	speed, _ := metricByType(activity, "speed")

	route, routeErr := RoutePoints(activity)
	if routeErr == nil {
		for _, t := range trackpointTimes(route, distanceMetric, heartRate, elevation, speed) {
			point, ok := nearestRoutePoint(route, t)
			if !ok {
				continue
			}
			trackpoint := tcxlib.Trackpoint{
				Time: time.UnixMilli(t).UTC(),
				Lat:  point.Latitude,
				Long: point.Longitude,
			}
			if meters, ok := valueAtOrBefore(distanceValues, t); ok {
				trackpoint.Dist = meters
			}
			if value, ok := nearestMetricValue(elevation, t); ok {
				trackpoint.Alt = value
			}
			if value, ok := nearestMetricValue(heartRate, t); ok {
				trackpoint.HR = math.Round(value)
			}
			if value, ok := nearestMetricValue(speed, t); ok {
				trackpoint.Speed = speedToMetersPerSecond(value, speed.Unit)
			}
			track.Pt = append(track.Pt, trackpoint)
		}
	}

	lap := tcxlib.Lap{
		Start:         start.Format(time.RFC3339),
		TotalTime:     activityDurationSeconds(activity),
		Dist:          summaryDistanceMeters(activity),
		Calories:      summaryValueOrZero(activity, "calories", "total"),
		AvgHr:         summaryValueOrZero(activity, "heart_rate", "mean"),
		MaxHr:         summaryValueOrZero(activity, "heart_rate", "max"),
		Intensity:     "Active",
		TriggerMethod: "Manual",
		Trk:           &track,
	}
	if lap.Dist == 0 && len(distanceValues) > 0 {
		lap.Dist = distanceValues[len(distanceValues)-1].Value
	}

	activities := tcxlib.Activities{
		Act: []tcxlib.Activity{{
			Sport: "Running",
			Id:    start,
			Laps:  []tcxlib.Lap{lap},
		}},
	}

	file := tcxlib.TCXDB{
		Acts: &activities,
		Auth: tcxlib.Author{Name: "athlete-log"},
	}
	return tcxlib.ToBytes(file)
}

func RoutePoints(activity *Activity) ([]RoutePoint, error) {
	latitude, ok := metricByType(activity, "latitude")
	if !ok || len(latitude.Values) == 0 {
		return nil, errors.New("activity does not contain GPS latitude metrics")
	}
	longitude, ok := metricByType(activity, "longitude")
	if !ok || len(longitude.Values) == 0 {
		return nil, errors.New("activity does not contain GPS longitude metrics")
	}

	points := make([]RoutePoint, 0, len(latitude.Values))
	for _, lat := range latitude.Values {
		lng, ok := nearestMetricValue(longitude, lat.StartEpochMs)
		if !ok {
			continue
		}
		points = append(points, RoutePoint{
			TimeEpochMs: lat.StartEpochMs,
			Latitude:    lat.Value,
			Longitude:   lng,
		})
	}
	if len(points) == 0 {
		return nil, errors.New("activity route contains no usable GPS points")
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].TimeEpochMs < points[j].TimeEpochMs
	})
	return points, nil
}

func trackpointTimes(route []RoutePoint, metrics ...*Metric) []int64 {
	seen := make(map[int64]struct{})
	times := make([]int64, 0, len(route))
	for _, point := range route {
		if point.TimeEpochMs == 0 {
			continue
		}
		if _, ok := seen[point.TimeEpochMs]; ok {
			continue
		}
		seen[point.TimeEpochMs] = struct{}{}
		times = append(times, point.TimeEpochMs)
	}
	for _, metric := range metrics {
		if metric == nil {
			continue
		}
		for _, value := range metric.Values {
			if value.StartEpochMs == 0 {
				continue
			}
			if _, ok := seen[value.StartEpochMs]; ok {
				continue
			}
			seen[value.StartEpochMs] = struct{}{}
			times = append(times, value.StartEpochMs)
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	return times
}

func nearestRoutePoint(route []RoutePoint, epochMs int64) (RoutePoint, bool) {
	if len(route) == 0 {
		return RoutePoint{}, false
	}
	idx := sort.Search(len(route), func(i int) bool { return route[i].TimeEpochMs >= epochMs })
	if idx == 0 {
		return route[0], true
	}
	if idx == len(route) {
		return route[len(route)-1], true
	}
	before := route[idx-1]
	after := route[idx]
	if epochMs-before.TimeEpochMs <= after.TimeEpochMs-epochMs {
		return before, true
	}
	return after, true
}

func metricByType(activity *Activity, metricType string) (*Metric, bool) {
	if activity == nil {
		return nil, false
	}
	for i := range activity.Metrics {
		if activity.Metrics[i].Type == metricType {
			return &activity.Metrics[i], true
		}
	}
	return nil, false
}

func nearestMetricValue(metric *Metric, epochMs int64) (float64, bool) {
	if metric == nil || len(metric.Values) == 0 {
		return 0, false
	}
	values := metric.Values
	idx := sort.Search(len(values), func(i int) bool { return values[i].StartEpochMs >= epochMs })
	if idx == 0 {
		return values[0].Value, true
	}
	if idx == len(values) {
		return values[len(values)-1].Value, true
	}
	before := values[idx-1]
	after := values[idx]
	if epochMs-before.StartEpochMs <= after.StartEpochMs-epochMs {
		return before.Value, true
	}
	return after.Value, true
}

func valueAtOrBefore(values []MetricValue, epochMs int64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	idx := sort.Search(len(values), func(i int) bool { return values[i].StartEpochMs > epochMs })
	if idx == 0 {
		return 0, false
	}
	return values[idx-1].Value, true
}

func cumulativeDistanceValues(activity *Activity) []MetricValue {
	distance, ok := metricByType(activity, "distance")
	if !ok || len(distance.Values) == 0 {
		return nil
	}

	summaryMeters := summaryDistanceMeters(activity)
	converted := make([]MetricValue, 0, len(distance.Values))
	total := 0.0
	for _, value := range distance.Values {
		meters := distanceToMeters(value.Value, distance.Unit)
		total += meters
		converted = append(converted, MetricValue{
			StartEpochMs: value.StartEpochMs,
			EndEpochMs:   value.EndEpochMs,
			Value:        meters,
		})
	}
	_ = total

	if summaryMeters > 0 {
		last := converted[len(converted)-1].Value
		if math.Abs(last-summaryMeters)/summaryMeters < 0.05 {
			return converted
		}
	}

	running := 0.0
	for i := range converted {
		running += converted[i].Value
		converted[i].Value = running
	}
	return converted
}

func activityDurationSeconds(activity *Activity) float64 {
	if activity.ActiveDurationMs > 0 {
		return float64(activity.ActiveDurationMs) / 1000
	}
	if activity.EndEpochMs > activity.StartEpochMs {
		return float64(activity.EndEpochMs-activity.StartEpochMs) / 1000
	}
	return 0
}

func summaryDistanceMeters(activity *Activity) float64 {
	return summaryValueOrZero(activity, "distance", "total") * 1000
}

func summaryValueOrZero(activity *Activity, metric string, summary string) float64 {
	value, ok := summaryValue(activity, metric, summary)
	if !ok {
		return 0
	}
	return value
}

func summaryValue(activity *Activity, metric string, summary string) (float64, bool) {
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

func distanceToMeters(value float64, unit string) float64 {
	switch strings.ToUpper(unit) {
	case "KM":
		return value * 1000
	case "M":
		return value
	default:
		return value
	}
}

func speedToMetersPerSecond(value float64, unit string) float64 {
	switch strings.ToUpper(unit) {
	case "KMH":
		return value / 3.6
	case "MPH":
		return value * 0.44704
	default:
		return value
	}
}
