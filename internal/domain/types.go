package domain

import "time"

type SourceType string

const (
	SourceNRC          SourceType = "nrc"
	SourceStrava       SourceType = "strava"
	SourceFITUpload    SourceType = "fit_upload"
	SourceGPXUpload    SourceType = "gpx_upload"
	SourceTCXUpload    SourceType = "tcx_upload"
	SourceFolderWatch  SourceType = "folder_watch"
)

type SportType string

const (
	SportRun     SportType = "run"
	SportRide    SportType = "ride"
	SportSwim    SportType = "swim"
	SportWalk    SportType = "walk"
	SportHike    SportType = "hike"
	SportUnknown SportType = "unknown"
)

type Athlete struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email"`
	StartedAt   *string    `json:"started_at,omitempty"` // user-set "journey start" date YYYY-MM-DD
	AvatarPath  string     `json:"avatar_path,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type SourceConnection struct {
	ID                string
	AthleteID         string
	Source            SourceType
	ExternalAthleteID string
	Status            string
	SecretKey         string
	Metadata          map[string]any
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type SourceActivityRef struct {
	ID                   string
	AthleteID            string
	SourceConnectionID   string
	Source               SourceType
	SourceActivityID     string
	SourceLastModifiedAt *time.Time
	RawBlobKey           string
	RawSHA256            string
	Tombstone            bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type SourceActivity struct {
	Ref      SourceActivityRef
	Activity Activity
}

type Activity struct {
	ID                          string
	AthleteID                   string
	SourceActivityRefID         string
	SportType                   SportType
	Name                        string
	AppID                       string
	Status                      string
	StartAt                     time.Time
	EndAt                       *time.Time
	Timezone                    string
	LastModifiedAt              *time.Time
	ElapsedDuration             *time.Duration
	MovingDuration              *time.Duration
	DistanceMeters              *float64
	ElevationGainMeters         *float64
	ElevationLossMeters         *float64
	Calories                    *int
	AverageHeartRate            *float64
	MaxHeartRate                *float64
	AverageSpeedMetersPerSecond *float64
	AveragePaceSecondsPerKM     *float64
	Notes                       string
	Indoor                      bool
	DeleteIndicator             bool
	Samples                     []ActivitySample
	Laps                        []ActivityLap
	Tags                        []ActivityTag
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

type ActivitySample struct {
	ID                   int64
	ActivityID           string
	SampleAt             time.Time
	Elapsed              *time.Duration
	Latitude             *float64
	Longitude            *float64
	DistanceMeters       *float64
	ElevationMeters      *float64
	HeartRate            *float64
	SpeedMetersPerSecond *float64
	Cadence              *float64
	PowerWatts           *float64
	TemperatureCelsius   *float64
	Raw                  map[string]any
}

func (s ActivitySample) HasLocation() bool {
	return s.Latitude != nil && s.Longitude != nil
}

type ActivityLap struct {
	ID               string
	ActivityID       string
	LapIndex         int
	StartAt          time.Time
	EndAt            *time.Time
	Duration         *time.Duration
	DistanceMeters   *float64
	Calories         *int
	AverageHeartRate *float64
	MaxHeartRate     *float64
	Raw              map[string]any
}

type ActivityTag struct {
	ActivityID string
	Key        string
	Value      string
	CreatedAt  time.Time
}

type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
	JobCanceled  JobStatus = "canceled"
)

type Job struct {
	ID              string
	AthleteID       string
	Type            string
	Status          JobStatus
	Phase           string
	Message         string
	ProgressCurrent int
	ProgressTotal   *int
	Attempts        int
	Params          map[string]any
	Result          map[string]any
	LastError       string
	ArtifactID      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
}

type JobEvent struct {
	ID      int64
	JobID   string
	EventAt time.Time
	Level   string
	Message string
	Data    map[string]any
}

type Artifact struct {
	ID          string
	AthleteID   string
	JobID       string
	ActivityID  string
	Kind        string
	ContentType string
	Filename    string
	StorageKey  string
	ByteSize    int64
	Checksum    string
	Metadata    map[string]any
	CreatedAt   time.Time
}
