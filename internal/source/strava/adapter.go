package strava

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"athlete-log/internal/domain"
	"athlete-log/internal/source"
)

const (
	defaultAPIURL  = "https://www.strava.com"
	authorizePath  = "/oauth/authorize"
	tokenPath      = "/oauth/token"
	activitiesPath = "/api/v3/athlete/activities"
	activityPath   = "/api/v3/activities/%d"
	streamsPath    = "/api/v3/activities/%d/streams"
)

// Config holds Strava OAuth2 credentials.
type Config struct {
	ClientID     string
	ClientSecret string
}

// Adapter implements source.PullAdapter for Strava.
type Adapter struct {
	config     Config
	httpClient *http.Client
	dataDir    string
	// BaseURL overrides the Strava base URL — set in tests to point at a fake server.
	BaseURL string
}

func New(config Config, httpClient *http.Client, dataDir string) *Adapter {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Adapter{config: config, httpClient: httpClient, dataDir: dataDir}
}

func (a *Adapter) Source() domain.SourceType { return domain.SourceStrava }

func (a *Adapter) apiBase() string {
	if a.BaseURL != "" {
		return a.BaseURL
	}
	return defaultAPIURL
}

// AuthorizeURL returns the Strava OAuth2 authorization URL.
// state must be a caller-generated CSRF token that is verified in the callback.
func (a *Adapter) AuthorizeURL(callbackURL, state string) string {
	q := url.Values{
		"client_id":       {a.config.ClientID},
		"redirect_uri":    {callbackURL},
		"response_type":   {"code"},
		"scope":           {"activity:read_all"},
		"approval_prompt": {"auto"},
		"state":           {state},
	}
	return a.apiBase() + authorizePath + "?" + q.Encode()
}

// ExchangeCode exchanges an OAuth authorization code for tokens and returns the
// metadata to store in source_connections, plus the athlete's external ID.
func (a *Adapter) ExchangeCode(ctx context.Context, code string) (meta map[string]any, externalAthleteID string, err error) {
	return a.doTokenRequest(ctx, url.Values{
		"client_id":     {a.config.ClientID},
		"client_secret": {a.config.ClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
	})
}

// RefreshToken obtains a new access token using the stored refresh token.
func (a *Adapter) RefreshToken(ctx context.Context, conn domain.SourceConnection) (domain.SourceConnection, error) {
	rt, _ := conn.Metadata["refresh_token"].(string)
	if rt == "" {
		return conn, errors.New("no refresh token in source connection metadata")
	}
	meta, externalID, err := a.doTokenRequest(ctx, url.Values{
		"client_id":     {a.config.ClientID},
		"client_secret": {a.config.ClientSecret},
		"refresh_token": {rt},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return conn, fmt.Errorf("strava token refresh: %w", err)
	}
	conn.Metadata = meta
	if externalID != "" {
		conn.ExternalAthleteID = externalID
	}
	return conn, nil
}

// NeedsRefresh returns true when the access token expires within 5 minutes.
func NeedsRefresh(conn domain.SourceConnection) bool {
	expiresAt, ok := conn.Metadata["token_expires_at"].(float64)
	if !ok {
		return false
	}
	return time.Now().Add(5 * time.Minute).Unix() >= int64(expiresAt)
}

// Sync pages through Strava activities since the last sync and calls upsert for each.
// On the first run (no last_synced_at in metadata) it fetches all history.
// Returns an updated copy of conn with last_synced_at set to now on success.
func (a *Adapter) Sync(ctx context.Context, conn domain.SourceConnection, upsert func(domain.SourceActivity) error) (domain.SourceConnection, error) {
	token, _ := conn.Metadata["access_token"].(string)
	if token == "" {
		return conn, errors.New("no Strava access token in source connection")
	}

	// SB-2: Use the last sync timestamp for incremental fetches.
	var after int64
	if v, ok := conn.Metadata["last_synced_at"].(float64); ok && v > 0 {
		after = int64(v)
	}

	syncedAt := time.Now().UTC()

	for page := 1; ; page++ {
		activities, err := a.fetchActivities(ctx, token, page, after)
		if err != nil {
			return conn, fmt.Errorf("fetch Strava activities page %d: %w", page, err)
		}
		if len(activities) == 0 {
			break
		}
		for _, sa := range activities {
			// SB-6: Fetch laps from the detail endpoint.
			detail, _ := a.fetchActivityDetail(ctx, token, sa.ID)

			// SB-4: Capture stream errors; a 429 aborts the sync.
			streams, streamErr := a.fetchStreams(ctx, token, sa.ID)
			if streamErr != nil {
				return conn, fmt.Errorf("fetch streams for activity %d: %w", sa.ID, streamErr)
			}

			raw, _ := json.Marshal(sa)
			blobKey, sha256hex, _ := source.WriteBlob(a.dataDir, raw)

			activity, err := mapActivity(sa, detail, streams, conn.ID)
			if err != nil {
				continue
			}
			if blobKey != "" {
				activity.Ref.RawBlobKey = blobKey
				activity.Ref.RawSHA256 = sha256hex
			}
			if err := upsert(*activity); err != nil {
				return conn, fmt.Errorf("upsert Strava activity %d: %w", sa.ID, err)
			}
		}
	}

	// SB-2: Record when this sync completed so the next run is incremental.
	if conn.Metadata == nil {
		conn.Metadata = make(map[string]any)
	}
	conn.Metadata["last_synced_at"] = float64(syncedAt.Unix())
	return conn, nil
}

// --- Strava API types ---

type stravaActivity struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	SportType        string  `json:"sport_type"`
	Type             string  `json:"type"`
	StartDate        string  `json:"start_date"`
	ElapsedTime      int     `json:"elapsed_time"`
	MovingTime       int     `json:"moving_time"`
	Distance         float64 `json:"distance"`
	TotalElevGain    float64 `json:"total_elevation_gain"`
	AverageSpeed     float64 `json:"average_speed"`
	AverageHeartrate float64 `json:"average_heartrate"`
	MaxHeartrate     float64 `json:"max_heartrate"`
	Calories         float64 `json:"calories"`
	Trainer          bool    `json:"trainer"`
	Manual           bool    `json:"manual"`
}

// SB-6: Detail endpoint response (used for laps).
type stravaActivityDetail struct {
	Laps []stravaLap `json:"laps"`
}

type stravaLap struct {
	LapIndex         int     `json:"lap_index"`
	StartDate        string  `json:"start_date"`
	ElapsedTime      int     `json:"elapsed_time"`
	Distance         float64 `json:"distance"`
	Calories         float64 `json:"calories"`
	AverageHeartrate float64 `json:"average_heartrate"`
	MaxHeartrate     float64 `json:"max_heartrate"`
}

type streamsResponse struct {
	Time           *struct{ Data []int        `json:"data"` } `json:"time"`
	Latlng         *struct{ Data [][2]float64 `json:"data"` } `json:"latlng"`
	Altitude       *struct{ Data []float64    `json:"data"` } `json:"altitude"`
	Heartrate      *struct{ Data []float64    `json:"data"` } `json:"heartrate"`
	VelocitySmooth *struct{ Data []float64    `json:"data"` } `json:"velocity_smooth"`
	Cadence        *struct{ Data []float64    `json:"data"` } `json:"cadence"`
}

type tokenResponse struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresAt    int64   `json:"expires_at"`
	Athlete      *struct{ ID int64 `json:"id"` } `json:"athlete"`
}

// --- HTTP helpers ---

func (a *Adapter) doTokenRequest(ctx context.Context, params url.Values) (map[string]any, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.apiBase()+tokenPath, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("strava token endpoint returned %s", resp.Status)
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, "", fmt.Errorf("decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, "", errors.New("strava returned empty access token")
	}

	meta := map[string]any{
		"access_token":     tr.AccessToken,
		"refresh_token":    tr.RefreshToken,
		"token_expires_at": float64(tr.ExpiresAt),
	}
	externalID := ""
	if tr.Athlete != nil && tr.Athlete.ID != 0 {
		externalID = strconv.FormatInt(tr.Athlete.ID, 10)
	}
	return meta, externalID, nil
}

// SB-2: after is a Unix timestamp; pass 0 for a full historical sync.
func (a *Adapter) fetchActivities(ctx context.Context, token string, page int, after int64) ([]stravaActivity, error) {
	endpoint := fmt.Sprintf("%s%s?page=%d&per_page=200", a.apiBase(), activitiesPath, page)
	if after > 0 {
		endpoint += fmt.Sprintf("&after=%d", after)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// SB-5: Respect rate limits.
	if err := checkRateLimit(resp); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("strava activities returned %s", resp.Status)
	}
	var activities []stravaActivity
	if err := json.NewDecoder(resp.Body).Decode(&activities); err != nil {
		return nil, fmt.Errorf("decode activities: %w", err)
	}
	return activities, nil
}

// SB-6: fetchActivityDetail retrieves laps from the activity detail endpoint.
// Returns nil detail (not an error) for manual/indoor activities with no laps.
func (a *Adapter) fetchActivityDetail(ctx context.Context, token string, activityID int64) (*stravaActivityDetail, error) {
	endpoint := fmt.Sprintf("%s%s", a.apiBase(), fmt.Sprintf(activityPath, activityID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkRateLimit(resp); err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("strava activity detail returned %s", resp.Status)
	}
	var detail stravaActivityDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("decode activity detail: %w", err)
	}
	return &detail, nil
}

func (a *Adapter) fetchStreams(ctx context.Context, token string, activityID int64) (*streamsResponse, error) {
	endpoint := fmt.Sprintf("%s%s?keys=time,latlng,altitude,heartrate,velocity_smooth,cadence&key_by_type=true",
		a.apiBase(), fmt.Sprintf(streamsPath, activityID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkRateLimit(resp); err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // manual entry — no streams
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("strava streams returned %s", resp.Status)
	}
	var streams streamsResponse
	if err := json.NewDecoder(resp.Body).Decode(&streams); err != nil {
		return nil, fmt.Errorf("decode streams: %w", err)
	}
	return &streams, nil
}

// SB-5: checkRateLimit returns an error if Strava signals the 15-min bucket is
// exhausted (429) or usage has reached 80 % of the limit (sleep + warn only).
func checkRateLimit(resp *http.Response) error {
	if resp.StatusCode == http.StatusTooManyRequests {
		return errors.New("strava rate limit exceeded (429) — sync will retry on the next run")
	}

	limitHdr := resp.Header.Get("X-ReadRateLimit-Limit")
	usageHdr := resp.Header.Get("X-ReadRateLimit-Usage")
	if limitHdr == "" || usageHdr == "" {
		return nil
	}

	// Headers are "per15min,daily" format.
	limitParts := strings.SplitN(limitHdr, ",", 2)
	usageParts := strings.SplitN(usageHdr, ",", 2)
	if len(limitParts) < 1 || len(usageParts) < 1 {
		return nil
	}
	limit, _ := strconv.Atoi(limitParts[0])
	usage, _ := strconv.Atoi(usageParts[0])
	if limit > 0 && usage >= int(float64(limit)*0.8) {
		// Approaching limit: pause to let the 15-min window roll over.
		time.Sleep(15 * time.Minute)
	}
	return nil
}

// --- Mapping ---

func mapActivity(sa stravaActivity, detail *stravaActivityDetail, streams *streamsResponse, connID string) (*domain.SourceActivity, error) {
	startAt, err := time.Parse(time.RFC3339, sa.StartDate)
	if err != nil {
		return nil, fmt.Errorf("parse start_date %q: %w", sa.StartDate, err)
	}

	elapsed := time.Duration(sa.ElapsedTime) * time.Second
	moving := time.Duration(sa.MovingTime) * time.Second
	endAt := startAt.Add(elapsed)

	activity := domain.Activity{
		SportType:       mapSportType(sa.SportType, sa.Type),
		Name:            sa.Name,
		StartAt:         startAt,
		EndAt:           &endAt,
		ElapsedDuration: &elapsed,
		MovingDuration:  &moving,
		Indoor:          sa.Trainer || sa.Manual,
	}
	if sa.Distance > 0 {
		v := sa.Distance
		activity.DistanceMeters = &v
	}
	if sa.TotalElevGain > 0 {
		v := sa.TotalElevGain
		activity.ElevationGainMeters = &v
	}
	if sa.AverageSpeed > 0 {
		activity.AverageSpeedMetersPerSecond = &sa.AverageSpeed
		pace := 1000 / sa.AverageSpeed
		activity.AveragePaceSecondsPerKM = &pace
	}
	if sa.AverageHeartrate > 0 {
		activity.AverageHeartRate = &sa.AverageHeartrate
	}
	if sa.MaxHeartrate > 0 {
		activity.MaxHeartRate = &sa.MaxHeartrate
	}
	if sa.Calories > 0 {
		c := int(math.Round(sa.Calories))
		activity.Calories = &c
	}
	if streams != nil {
		activity.Samples = mapStreams(streams, startAt)
	}
	// SB-6: Map laps from the detail endpoint.
	if detail != nil {
		activity.Laps = mapLaps(detail.Laps, startAt)
	}

	return &domain.SourceActivity{
		Ref: domain.SourceActivityRef{
			Source:             domain.SourceStrava,
			SourceActivityID:   strconv.FormatInt(sa.ID, 10),
			SourceConnectionID: connID,
		},
		Activity: activity,
	}, nil
}

func mapLaps(laps []stravaLap, activityStart time.Time) []domain.ActivityLap {
	result := make([]domain.ActivityLap, 0, len(laps))
	for _, l := range laps {
		startAt, err := time.Parse(time.RFC3339, l.StartDate)
		if err != nil {
			startAt = activityStart
		}
		dur := time.Duration(l.ElapsedTime) * time.Second
		endAt := startAt.Add(dur)

		lap := domain.ActivityLap{
			LapIndex: l.LapIndex,
			StartAt:  startAt,
			EndAt:    &endAt,
			Duration: &dur,
		}
		if l.Distance > 0 {
			v := l.Distance
			lap.DistanceMeters = &v
		}
		if l.Calories > 0 {
			c := int(math.Round(l.Calories))
			lap.Calories = &c
		}
		if l.AverageHeartrate > 0 {
			lap.AverageHeartRate = &l.AverageHeartrate
		}
		if l.MaxHeartrate > 0 {
			lap.MaxHeartRate = &l.MaxHeartrate
		}
		result = append(result, lap)
	}
	return result
}

func mapStreams(streams *streamsResponse, startAt time.Time) []domain.ActivitySample {
	if streams == nil || streams.Time == nil || len(streams.Time.Data) == 0 {
		return nil
	}
	samples := make([]domain.ActivitySample, 0, len(streams.Time.Data))
	for i, t := range streams.Time.Data {
		elapsed := time.Duration(t) * time.Second
		sampleAt := startAt.Add(elapsed)
		s := domain.ActivitySample{SampleAt: sampleAt, Elapsed: &elapsed}
		if streams.Latlng != nil && i < len(streams.Latlng.Data) {
			lat, lng := streams.Latlng.Data[i][0], streams.Latlng.Data[i][1]
			s.Latitude, s.Longitude = &lat, &lng
		}
		if streams.Altitude != nil && i < len(streams.Altitude.Data) {
			v := streams.Altitude.Data[i]
			s.ElevationMeters = &v
		}
		if streams.Heartrate != nil && i < len(streams.Heartrate.Data) {
			v := streams.Heartrate.Data[i]
			s.HeartRate = &v
		}
		if streams.VelocitySmooth != nil && i < len(streams.VelocitySmooth.Data) {
			v := streams.VelocitySmooth.Data[i]
			s.SpeedMetersPerSecond = &v
		}
		if streams.Cadence != nil && i < len(streams.Cadence.Data) {
			v := streams.Cadence.Data[i]
			s.Cadence = &v
		}
		samples = append(samples, s)
	}
	return samples
}

func mapSportType(sportType, legacyType string) domain.SportType {
	t := sportType
	if t == "" {
		t = legacyType
	}
	switch strings.ToLower(t) {
	case "run", "virtualrun", "trailrun":
		return domain.SportRun
	case "ride", "virtualride", "ebikeride", "emtbride", "mountainbikeride":
		return domain.SportRide
	case "swim":
		return domain.SportSwim
	case "walk":
		return domain.SportWalk
	case "hike":
		return domain.SportHike
	case "":
		return domain.SportUnknown
	default:
		return domain.SportType(strings.ToLower(t))
	}
}
