package nrc

type Summary struct {
	Metric  string  `json:"metric"`
	Summary string  `json:"summary"`
	Source  string  `json:"source"`
	AppID   string  `json:"app_id"`
	Value   float64 `json:"value"`
}

type ActivityTags struct {
	ComNikeRunningRecordingappversion string `json:"com.nike.running.recordingappversion"`
	Location                          string `json:"location"`
	ComNikeRunningSyncsource          string `json:"com.nike.running.syncsource"`
	ComNikeWeather                    string `json:"com.nike.weather"`
	ComNikeRunningGoaltype            string `json:"com.nike.running.goaltype"`
	ComNikeRunningSyncappversion      string `json:"com.nike.running.syncappversion"`
	ShoeID                            string `json:"shoe_id"`
	ComNikeWeathersource              string `json:"com.nike.weathersource"`
	ComNikeName                       string `json:"com.nike.name"`
	ComNikeRunningRecordingsource     string `json:"com.nike.running.recordingsource"`
	ComNikeTemperature                string `json:"com.nike.temperature"`
	ComNikeRunningOriginalactivityid  string `json:"com.nike.running.originalactivityid"`
	Rpe                               string `json:"rpe"`
	Terrain                           string `json:"terrain"`
}

type Moment struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Timestamp int64  `json:"timestamp"`
	AppID     string `json:"app_id"`
	Source    string `json:"source"`
}

type Polyline struct {
	Source   string `json:"source"`
	AppID    string `json:"app_id"`
	Type     string `json:"type"`
	Polyline string `json:"polyline"`
}

type ActivityListItem struct {
	ID               string        `json:"id"`
	Type             string        `json:"type"`
	AppID            string        `json:"app_id"`
	StartEpochMs     int64         `json:"start_epoch_ms"`
	EndEpochMs       int64         `json:"end_epoch_ms"`
	LastModified     int64         `json:"last_modified"`
	ActiveDurationMs int           `json:"active_duration_ms"`
	Status           string        `json:"status"`
	Session          bool          `json:"session"`
	Summaries        []Summary     `json:"summaries"`
	Sources          []string      `json:"sources"`
	Tags             ActivityTags  `json:"tags"`
	ChangeTokens     []string      `json:"change_tokens"`
	MetricTypes      []string      `json:"metric_types"`
	Metrics          []interface{} `json:"metrics"`
	Moments          []Moment      `json:"moments"`
	Polylines        []Polyline    `json:"polylines"`
	DeleteIndicator  bool          `json:"delete_indicator"`
}

type Paging struct {
	BeforeID string `json:"before_id"`
}

type Activities struct {
	Activities []ActivityListItem `json:"activities"`
	Paging     Paging             `json:"paging"`
}

type MetricValue struct {
	StartEpochMs int64   `json:"start_epoch_ms"`
	EndEpochMs   int64   `json:"end_epoch_ms"`
	Value        float64 `json:"value"`
}

type Metric struct {
	Type   string        `json:"type"`
	Unit   string        `json:"unit"`
	Source string        `json:"source"`
	AppID  string        `json:"appId"`
	Values []MetricValue `json:"values"`
}

type Activity struct {
	ID               string       `json:"id"`
	Type             string       `json:"type"`
	AppID            string       `json:"app_id"`
	StartEpochMs     int64        `json:"start_epoch_ms"`
	EndEpochMs       int64        `json:"end_epoch_ms"`
	LastModified     int64        `json:"last_modified"`
	ActiveDurationMs int          `json:"active_duration_ms"`
	Session          bool         `json:"session"`
	DeleteIndicator  bool         `json:"delete_indicator"`
	Summaries        []Summary    `json:"summaries"`
	Sources          []string     `json:"sources"`
	Tags             ActivityTags `json:"tags"`
	ChangeTokens     []string     `json:"change_tokens"`
	MetricTypes      []string     `json:"metric_types"`
	Metrics          []Metric     `json:"metrics"`
	Moments          []Moment     `json:"moments"`
	Polylines        []Polyline   `json:"polylines"`
}
