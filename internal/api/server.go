package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"athlete-log/internal/domain"
	"athlete-log/internal/export"
	"athlete-log/internal/job"
	"athlete-log/internal/nrc"
	"athlete-log/internal/source"
	"athlete-log/internal/source/folder"
	nrcsource "athlete-log/internal/source/nrc"
	"athlete-log/internal/source/strava"
	"athlete-log/internal/source/upload"
	"athlete-log/internal/storage"
)

type APIServer struct {
	nikeClient *nrc.NikeClient
	tokenStore *nrc.TokenStore
	jobsMu     sync.RWMutex
	exportJobs map[string]*ExportJob

	// DB-backed job system — nil when running without a repository (e.g. in tests).
	repo          storage.Repository
	runner        *job.Runner
	athleteID     string
	dataDir       string
	httpClient    *http.Client
	stravaAdapter *strava.Adapter
	// SB-7: short-lived CSRF state tokens for the Strava OAuth flow.
	stravaOAuthStates sync.Map // map[string]time.Time (expiry)

	// Heatmap GeoJSON cache — invalidated whenever any activity is written.
	heatmapMu    sync.RWMutex
	heatmapCache []byte // nil = stale, recomputed on next request

	// Folder watchers — one per active folder_watch source connection.
	folderMu       sync.Mutex
	folderWatchers map[string]*folder.Watcher // keyed by source_connection.id
	folderCancel   map[string]context.CancelFunc

	// baseURL is the externally-reachable origin used to construct OAuth callback URLs.
	// Set via WithBaseURL; falls back to deriving from the incoming request Host header.
	baseURL string

	// publicMode is set from APP_PUBLIC_MODE=true at startup. When true, GET/HEAD
	// requests are always public; POST/PUT/DELETE/PATCH always require auth.
	publicMode bool

	// Optional password auth gate. authEnabled is loaded from app_settings on every
	// settings change. authCachedPwd avoids a bcrypt comparison on every request.
	authMu           sync.RWMutex
	authEnabled      bool
	authPasswordHash string
	authCachedPwd    string // last password that passed bcrypt; avoids repeated hashing

	// Best-efforts recomputation coalescing: at most one goroutine runs at a time,
	// with at most one additional run queued. Prevents N concurrent uploads from
	// spawning N simultaneous O(n²) sample scans.
	beMu   sync.Mutex
	beBusy bool
	beNext bool
}

func NewAPIServer(nikeClient *nrc.NikeClient, tokenStore *nrc.TokenStore) *APIServer {
	return &APIServer{
		nikeClient:     nikeClient,
		tokenStore:     tokenStore,
		exportJobs:     make(map[string]*ExportJob),
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		folderWatchers: make(map[string]*folder.Watcher),
		folderCancel:   make(map[string]context.CancelFunc),
		publicMode:     os.Getenv("APP_PUBLIC_MODE") == "true",
	}
}

// WithRepository wires the storage-backed job system and data directory into the server.
func (s *APIServer) WithRepository(repo storage.Repository, runner *job.Runner, athleteID, dataDir string) *APIServer {
	s.repo = repo
	s.runner = runner
	s.athleteID = athleteID
	s.dataDir = dataDir
	s.loadAuthState(context.Background())
	s.scheduleRecomputeBestEfforts()
	go s.startExistingFolderWatchers(context.Background())
	return s
}

func (s *APIServer) startExistingFolderWatchers(ctx context.Context) {
	conns, err := s.repo.ListSourceConnectionsByType(ctx, s.athleteID, domain.SourceFolderWatch)
	if err != nil {
		slog.Error("folder_watch: load connections", "error", err)
		return
	}
	for _, conn := range conns {
		s.startFolderWatcher(conn)
	}
}

func (s *APIServer) startFolderWatcher(conn domain.SourceConnection) {
	s.folderMu.Lock()
	defer s.folderMu.Unlock()

	if cancel, ok := s.folderCancel[conn.ID]; ok {
		cancel() // stop previous instance
	}

	ctx, cancel := context.WithCancel(context.Background())
	w := folder.New(conn, s.repo, s.athleteID, s.dataDir)
	s.folderWatchers[conn.ID] = w
	s.folderCancel[conn.ID] = cancel

	go func() {
		w.Start(ctx)
	}()
}

func (s *APIServer) stopFolderWatcher(connID string) {
	s.folderMu.Lock()
	defer s.folderMu.Unlock()
	if cancel, ok := s.folderCancel[connID]; ok {
		cancel()
		delete(s.folderCancel, connID)
		delete(s.folderWatchers, connID)
	}
}

// WithBaseURL sets the externally-reachable origin (e.g. "https://vault.example.com")
// used when constructing OAuth callback URLs. Must not have a trailing slash.
func (s *APIServer) WithBaseURL(u string) *APIServer {
	s.baseURL = u
	return s
}

// WithStrava enables the Strava OAuth flow and sync adapter.
func (s *APIServer) WithStrava(adapter *strava.Adapter) *APIServer {
	s.stravaAdapter = adapter
	return s
}

func (s *APIServer) Routes() http.Handler {
	return s.RoutesWithStatic("")
}

func (s *APIServer) RoutesWithStatic(staticDir string) http.Handler {
	mux := http.NewServeMux()

	// v1 endpoints
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("POST /api/v1/auth/verify", s.handleAuthVerify)
	mux.HandleFunc("GET /api/v1/settings", s.adminOnly(s.handleV1SettingsGet))
	mux.HandleFunc("PUT /api/v1/settings", s.handleV1SettingsPut)
	mux.HandleFunc("GET /api/v1/profile", s.adminOnly(s.handleV1ProfileGet))
	mux.HandleFunc("PUT /api/v1/profile", s.handleV1ProfilePut)
	mux.HandleFunc("POST /api/v1/profile/avatar", s.handleV1ProfileAvatarUpload)
	mux.HandleFunc("GET /api/v1/profile/avatar", s.handleV1ProfileAvatarServe)
	mux.HandleFunc("GET /api/v1/gear", s.handleV1GearList)
	mux.HandleFunc("POST /api/v1/gear", s.handleV1GearCreate)
	mux.HandleFunc("PUT /api/v1/gear/{id}", s.handleV1GearUpdate)
	mux.HandleFunc("DELETE /api/v1/gear/{id}", s.handleV1GearDelete)
	mux.HandleFunc("GET /api/v1/activities/{id}/gear", s.handleV1ActivityGearGet)
	mux.HandleFunc("PUT /api/v1/activities/{id}/gear", s.handleV1ActivityGearSet)
	mux.HandleFunc("GET /api/v1/heatmap", s.handleV1Heatmap)
	mux.HandleFunc("GET /api/v1/records", s.handleV1Records)
	mux.HandleFunc("GET /api/v1/dashboards", s.handleV1DashboardsList)
	mux.HandleFunc("POST /api/v1/dashboards", s.handleV1DashboardsCreate)
	mux.HandleFunc("GET /api/v1/dashboards/{id}", s.handleV1DashboardsGet)
	mux.HandleFunc("PUT /api/v1/dashboards/{id}", s.handleV1DashboardsUpdate)
	mux.HandleFunc("DELETE /api/v1/dashboards/{id}", s.handleV1DashboardsDelete)
	mux.HandleFunc("GET /api/v1/dashboards/{id}/widgets", s.handleV1WidgetsList)
	mux.HandleFunc("POST /api/v1/dashboards/{id}/widgets", s.handleV1WidgetsCreate)
	mux.HandleFunc("PUT /api/v1/dashboards/{id}/widgets/{wid}", s.handleV1WidgetsUpdate)
	mux.HandleFunc("DELETE /api/v1/dashboards/{id}/widgets/{wid}", s.handleV1WidgetsDelete)
	mux.HandleFunc("GET /api/v1/sources", s.adminOnly(s.handleV1SourcesList))
	mux.HandleFunc("POST /api/v1/sources/nrc/connect", s.handleV1SourcesNRCConnect)
	mux.HandleFunc("GET /api/v1/sources/strava/connect", s.adminOnly(s.handleV1SourcesStravaConnect))
	mux.HandleFunc("GET /api/v1/sources/strava/callback", s.adminOnly(s.handleV1SourcesStravaCallback))
	mux.HandleFunc("POST /api/v1/sources/folder/connect", s.handleV1SourcesFolderConnect)
	mux.HandleFunc("DELETE /api/v1/sources/folder/{id}", s.handleV1SourcesFolderDelete)
	mux.HandleFunc("GET /api/v1/sources/{id}/files", s.adminOnly(s.handleV1SourcesFileLog))
	mux.HandleFunc("POST /api/v1/sources/{id}/sync", s.handleV1SourcesSync)
	mux.HandleFunc("GET /api/v1/stats/timeseries", s.handleV1StatsTimeseries)
	mux.HandleFunc("GET /api/v1/stats/streaks", s.handleV1StatsStreaks)
	mux.HandleFunc("GET /api/v1/stats/bests", s.handleV1StatsBests)
	mux.HandleFunc("GET /api/v1/stats/pmc", s.handleV1StatsPMC)
	mux.HandleFunc("GET /api/v1/stats/year-review", s.handleV1StatsYearReview)
	mux.HandleFunc("GET /api/v1/stats", s.handleV1Stats)
	mux.HandleFunc("GET /api/v1/activities", s.handleV1ActivitiesList)
	mux.HandleFunc("PUT /api/v1/activities/{id}/tags/{key}", s.handleV1ActivityTagSet)
	mux.HandleFunc("DELETE /api/v1/activities/{id}/tags/{key}", s.handleV1ActivityTagDelete)
	mux.HandleFunc("GET /api/v1/activities/{id}/samples", s.handleV1ActivitySamples)
	mux.HandleFunc("GET /api/v1/activities/{id}/similar", s.handleV1ActivitySimilar)
	mux.HandleFunc("GET /api/v1/activities/{id}/weather", s.handleV1ActivityWeather)
	mux.HandleFunc("GET /api/v1/activities/{id}/card.svg", s.handleV1ActivityCard)
	mux.HandleFunc("GET /api/v1/activities/{id}", s.handleV1ActivityDetail)
	mux.HandleFunc("PATCH /api/v1/activities/{id}", s.handleV1ActivityPatch)
	mux.HandleFunc("DELETE /api/v1/activities/{id}", s.handleV1ActivityDelete)
	mux.HandleFunc("GET /api/v1/jobs", s.adminOnly(s.handleV1JobsList))
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.adminOnly(s.handleV1Job))
	mux.HandleFunc("POST /api/v1/uploads", s.handleV1Upload)
	mux.HandleFunc("POST /api/v1/exports", s.handleV1Exports)
	mux.HandleFunc("GET /api/v1/artifacts/{id}/download", s.adminOnly(s.handleV1ArtifactDownload))
	mux.HandleFunc("POST /api/v1/mcp", s.adminOnly(s.handleMCP))
	mux.HandleFunc("GET /api/v1/mcp.mcpb", s.adminOnly(s.handleMCPBundle))

	// Legacy endpoints (kept for compatibility)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/token", s.handleToken)
	mux.HandleFunc("/api/token/status", s.handleTokenStatus)
	mux.HandleFunc("/api/activities", s.handleActivities)
	mux.HandleFunc("/api/activities/", s.handleActivity)
	mux.HandleFunc("/api/export", s.handleExportAll)
	mux.HandleFunc("/api/export/jobs", s.handleExportJobs)
	mux.HandleFunc("/api/export/jobs/", s.handleExportJob)
	mux.HandleFunc("/api/", http.NotFound)

	if staticDir != "" {
		mux.Handle("/", spaFileServer(staticDir))
	}
	return s.logMiddleware(s.withCORS(s.authMiddleware(mux)))
}

func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	s.authMu.RLock()
	authEnabled := s.authEnabled
	s.authMu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"time":         time.Now().UTC().Format(time.RFC3339),
		"auth_enabled": authEnabled,
		"public_mode":  s.publicMode,
	})
}

// adminOnly wraps a handler so it always requires auth when in public mode,
// even for GET/HEAD requests. Used for endpoints that return sensitive config
// (settings, API keys, source connections, export artifacts).
// In normal mode it's a no-op — the outer authMiddleware handles everything.
func (s *APIServer) adminOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.publicMode {
			s.authMu.RLock()
			hash := s.authPasswordHash
			cached := s.authCachedPwd
			s.authMu.RUnlock()
			if hash != "" {
				token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				if token == "" {
					writeError(w, http.StatusUnauthorized, "authentication required")
					return
				}
				if token != cached {
					if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)); err != nil {
						writeError(w, http.StatusUnauthorized, "invalid password")
						return
					}
					s.authMu.Lock()
					s.authCachedPwd = token
					s.authMu.Unlock()
				}
			}
		}
		h(w, r)
	}
}

// authMiddleware gates routes based on auth configuration.
//
// Normal mode (APP_PUBLIC_MODE unset): all routes require auth when auth_enabled=true.
// Public mode (APP_PUBLIC_MODE=true): GET/HEAD are always public; POST/PUT/DELETE/PATCH
// always require a valid password regardless of the auth_enabled setting.
func (s *APIServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/api/v1/health" || path == "/api/v1/auth/verify" {
			next.ServeHTTP(w, r)
			return
		}

		// Public mode: read operations are open to everyone.
		if s.publicMode && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			next.ServeHTTP(w, r)
			return
		}

		s.authMu.RLock()
		enabled := s.authEnabled
		hash := s.authPasswordHash
		cached := s.authCachedPwd
		s.authMu.RUnlock()

		// Normal mode: only gate when auth is explicitly enabled.
		if !s.publicMode && !enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Public mode with no password set yet: allow everything so the owner
		// can complete first-time setup (enable password protection in Settings).
		if s.publicMode && hash == "" {
			next.ServeHTTP(w, r)
			return
		}

		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Athlete Log"`)
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		// Fast path: compare against the last verified password (plain string compare).
		if token != cached {
			if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)); err != nil {
				writeError(w, http.StatusUnauthorized, "invalid password")
				return
			}
			s.authMu.Lock()
			s.authCachedPwd = token
			s.authMu.Unlock()
		}

		next.ServeHTTP(w, r)
	})
}

// handleAuthVerify checks a password without requiring an existing session.
// Used by the login form before the token is stored client-side.
func (s *APIServer) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
		writeError(w, http.StatusBadRequest, "password required")
		return
	}
	s.authMu.RLock()
	hash := s.authPasswordHash
	s.authMu.RUnlock()
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// loadAuthState re-reads auth settings from the DB into the in-memory cache.
// Called after any settings save that may have changed auth fields.
// If APP_AUTH_RESET=true is set, auth is disabled and the password cleared first —
// allowing recovery when the password is forgotten.
func (s *APIServer) loadAuthState(ctx context.Context) {
	if s.repo == nil {
		return
	}
	if os.Getenv("APP_AUTH_RESET") == "true" {
		_ = s.repo.SetSetting(ctx, "auth_enabled", "false")
		_ = s.repo.SetSetting(ctx, "auth_password_hash", "")
		slog.Warn("APP_AUTH_RESET=true: auth disabled and password cleared")
	}
	enabled, _ := s.repo.GetSetting(ctx, "auth_enabled")
	hash, _ := s.repo.GetSetting(ctx, "auth_password_hash")
	s.authMu.Lock()
	s.authEnabled = enabled == "true"
	s.authPasswordHash = hash
	s.authCachedPwd = "" // invalidate fast-path cache on any settings change
	s.authMu.Unlock()
}

func (s *APIServer) handleTokenStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"configured": s.tokenStore.HasToken()})
}

func (s *APIServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	if err := s.tokenStore.Set(req.Token); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"configured": true})
}

func (s *APIServer) handleActivities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	query := r.URL.Query()
	beforeID := firstQueryValue(query.Get("before_id"), query.Get("beforeId"), "*")
	beforeDateRaw := firstQueryValue(query.Get("before_date"), query.Get("beforeDate"))
	limit := parsePositiveInt(query.Get("limit"), 10, 10)
	pages := parsePositiveInt(query.Get("pages"), 1, 50)
	searchPages := parsePositiveInt(query.Get("search_pages"), 250, 1000)
	var beforeDate time.Time
	if beforeDateRaw != "" {
		parsed, err := parseBeforeDate(beforeDateRaw)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		beforeDate = parsed
	}

	response := ActivityListResponse{
		Activities: make([]nrc.ActivityListItem, 0, limit*pages),
		Paging:     nrc.Paging{BeforeID: beforeID},
	}
	resultLimit := limit * pages
	dateMatched := beforeDateRaw == ""
	maxFetchPages := pages
	if beforeDateRaw != "" {
		maxFetchPages = searchPages
	}
	seenBeforeIDs := map[string]struct{}{beforeID: {}}
	for pageIndex := 0; pageIndex < maxFetchPages; pageIndex++ {
		page, err := s.nikeClient.FetchActivities(r.Context(), beforeID, limit)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		if beforeDateRaw != "" {
			response.Search = &ActivitySearch{
				BeforeDate:    beforeDateRaw,
				CutoffEpochMs: beforeDate.UnixMilli(),
				PagesFetched:  pageIndex + 1,
				SearchPages:   searchPages,
				Matched:       dateMatched,
			}
		}
		updateSearchScan(response.Search, page.Activities)
		if dateMatched {
			response.Activities = append(response.Activities, activitiesBefore(page.Activities, beforeDate)...)
		} else if firstPriorIndex := firstActivityBefore(page.Activities, beforeDate); firstPriorIndex >= 0 {
			dateMatched = true
			response.Search.Matched = true
			response.Activities = append(response.Activities, activitiesBefore(page.Activities[firstPriorIndex:], beforeDate)...)
		}
		nextBeforeID := nextBeforeCursor(page, beforeID)
		response.Paging.BeforeID = nextBeforeID
		if response.Search != nil {
			response.Search.NextBeforeID = nextBeforeID
		}
		if dateMatched && len(response.Activities) >= resultLimit {
			response.Activities = response.Activities[:resultLimit]
			setSearchStop(response.Search, "result_limit")
			break
		}
		if len(page.Activities) == 0 {
			setSearchStop(response.Search, "empty_page")
			break
		}
		if nextBeforeID == "" {
			setSearchStop(response.Search, "no_before_id")
			break
		}
		if _, seen := seenBeforeIDs[nextBeforeID]; seen {
			setSearchStop(response.Search, "repeated_before_id")
			break
		}
		seenBeforeIDs[nextBeforeID] = struct{}{}
		beforeID = nextBeforeID
		if pageIndex == maxFetchPages-1 {
			setSearchStop(response.Search, "page_limit")
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *APIServer) handleActivity(w http.ResponseWriter, r *http.Request) {
	activityID, suffix, ok := parseActivityPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch {
	case suffix == "" && r.Method == http.MethodGet:
		s.handleActivityDetail(w, r, activityID)
	case suffix == "tcx" && r.Method == http.MethodGet:
		s.handleActivityTCX(w, r, activityID)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *APIServer) handleActivityDetail(w http.ResponseWriter, r *http.Request, activityID string) {
	activity, err := s.nikeClient.FetchActivity(r.Context(), activityID)
	if err != nil {
		writeAPIError(w, err)
		return
	}

	route, _ := nrc.RoutePoints(activity)
	writeJSON(w, http.StatusOK, nrcActivityDetailResponse{
		Activity: *activity,
		Route:    route,
	})
}

func (s *APIServer) handleActivityTCX(w http.ResponseWriter, r *http.Request, activityID string) {
	activity, err := s.nikeClient.FetchActivity(r.Context(), activityID)
	if err != nil {
		writeAPIError(w, err)
		return
	}

	bytes, err := nrc.ExportActivityToTCX(activity)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/vnd.garmin.tcx+xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, nrc.TCXFilename(activity)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bytes)
}

func (s *APIServer) handleExportAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	query := r.URL.Query()
	beforeDateRaw := firstQueryValue(query.Get("before_date"), query.Get("beforeDate"))
	fromDateRaw := firstQueryValue(query.Get("from_date"), query.Get("fromDate"), query.Get("start_date"), query.Get("startDate"))
	toDateRaw := firstQueryValue(query.Get("to_date"), query.Get("toDate"), query.Get("end_date"), query.Get("endDate"))
	allRaw := firstQueryValue(query.Get("all"), query.Get("allActivities"))
	allRequested := allRaw == "" || isTruthyQuery(allRaw)
	beforeID := firstQueryValue(query.Get("before_id"), query.Get("beforeId"), "*")
	searchPages := parsePositiveInt(query.Get("search_pages"), 1000, 2000)

	var (
		candidates []nrc.ActivityListItem
		summary    *ActivitySearch
		filename   string
		err        error
	)

	switch {
	case allRaw != "" && !allRequested:
		writeError(w, http.StatusBadRequest, "all must be true when provided")
		return
	case allRaw != "" && (beforeDateRaw != "" || fromDateRaw != "" || toDateRaw != ""):
		writeError(w, http.StatusBadRequest, "use either all=true or date filters")
		return
	case beforeDateRaw == "" && fromDateRaw == "" && toDateRaw == "":
		candidates, summary, err = s.activitiesAll(r.Context(), beforeID, searchPages, nil)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		if len(candidates) == 0 {
			writeError(w, http.StatusNotFound, "no activities were found")
			return
		}
		filename = fmt.Sprintf("nrc-all-activities-%s.zip", time.Now().UTC().Format("2006-01-02"))
	case beforeDateRaw != "" && (fromDateRaw != "" || toDateRaw != ""):
		writeError(w, http.StatusBadRequest, "use either before_date or from_date/to_date")
		return
	case fromDateRaw != "" || toDateRaw != "":
		if fromDateRaw == "" || toDateRaw == "" {
			writeError(w, http.StatusBadRequest, "from_date and to_date are required for range export")
			return
		}
		fromDate, err := parseQueryDate(fromDateRaw, "from_date")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		toDate, err := parseQueryDate(toDateRaw, "to_date")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		rangeEnd := toDate
		if isDateOnlyQueryDate(toDateRaw) {
			rangeEnd = toDate.AddDate(0, 0, 1)
		}
		if !fromDate.Before(rangeEnd) {
			writeError(w, http.StatusBadRequest, "to_date must be the same as or after from_date")
			return
		}
		candidates, summary, err = s.activitiesInDateRange(r.Context(), beforeID, fromDate, rangeEnd, fromDateRaw, toDateRaw, searchPages)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		if len(candidates) == 0 {
			writeError(w, http.StatusNotFound, fmt.Sprintf("no activities between %s and %s were found", fromDateRaw, toDateRaw))
			return
		}
		filename = fmt.Sprintf("nrc-range-%s-to-%s.zip", fromDate.Format("2006-01-02"), toDate.Format("2006-01-02"))
	case beforeDateRaw != "":
		beforeDate, err := parseBeforeDate(beforeDateRaw)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		candidates, summary, err = s.activitiesBeforeDate(r.Context(), beforeID, beforeDate, beforeDateRaw, searchPages)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		if len(candidates) == 0 {
			writeError(w, http.StatusNotFound, fmt.Sprintf("no activities before %s were found", beforeDateRaw))
			return
		}
		filename = fmt.Sprintf("nrc-before-%s.zip", beforeDate.UTC().Format("2006-01-02"))
	default:
		writeError(w, http.StatusBadRequest, "before_date or from_date/to_date is required")
		return
	}

	zipBytes, exported, exportErrors := s.exportActivitiesZip(r.Context(), candidates, summary, nil)
	if exported == 0 {
		writeError(w, http.StatusBadGateway, strings.Join(exportErrors, "; "))
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(zipBytes)
}

func (s *APIServer) handleExportJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	var req struct {
		BeforeID    string `json:"before_id"`
		SearchPages int    `json:"search_pages"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON request body")
			return
		}
	}

	query := r.URL.Query()
	beforeID := firstQueryValue(query.Get("before_id"), query.Get("beforeId"), req.BeforeID, "*")
	searchPages := req.SearchPages
	if searchPages <= 0 {
		searchPages = 2000
	}
	if searchPages > 2000 {
		searchPages = 2000
	}
	searchPages = parsePositiveInt(query.Get("search_pages"), searchPages, 2000)

	if s.repo != nil {
		dbJob, err := s.repo.CreateJob(r.Context(), s.athleteID, "export", map[string]any{
			"before_id":    beforeID,
			"search_pages": searchPages,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not create export job")
			return
		}
		w.Header().Set("Location", fmt.Sprintf("/api/export/jobs/%s", dbJob.ID))
		writeJSON(w, http.StatusAccepted, jobToExportResponse(dbJob))
		s.runner.Start(dbJob.ID, func(ctx context.Context, update func(storage.JobUpdate)) []byte {
			s.runDBExportJob(ctx, dbJob.ID, storage.ListActivitiesFilter{}, update)
			return nil
		})
		return
	}

	job := s.createExportJob()
	w.Header().Set("Location", fmt.Sprintf("/api/export/jobs/%s", job.ID))
	writeJSON(w, http.StatusAccepted, job.Snapshot())

	go s.runExportAllJob(job.ID, beforeID, searchPages)
}

func (s *APIServer) handleExportJob(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/export/jobs/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) > 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	jobID := parts[0]
	isDownload := len(parts) == 2
	if isDownload && parts[1] != "download" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	if s.repo != nil {
		dbJob, err := s.repo.GetJob(r.Context(), jobID)
		if err != nil {
			writeError(w, http.StatusNotFound, "export job was not found")
			return
		}
		if isDownload {
			s.handleExportJobDownloadDB(w, dbJob, jobID)
		} else {
			writeJSON(w, http.StatusOK, jobToExportResponse(dbJob))
		}
		return
	}

	memJob, ok := s.getExportJob(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "export job was not found")
		return
	}
	if isDownload {
		s.handleExportJobDownload(w, memJob)
	} else {
		writeJSON(w, http.StatusOK, memJob.Snapshot())
	}
}

func (s *APIServer) handleExportJobDownload(w http.ResponseWriter, job *ExportJob) {
	job.mu.RLock()
	status := job.Status
	filename := job.Filename
	zipBytes := job.ZipBytes
	message := job.Error
	job.mu.RUnlock()

	if status != exportJobStatusCompleted {
		if message == "" {
			message = "export is not complete"
		}
		writeError(w, http.StatusConflict, message)
		return
	}
	if filename == "" {
		filename = "nrc-all-activities.zip"
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(zipBytes)
}

func (s *APIServer) createExportJob() *ExportJob {
	now := time.Now().UTC()
	job := &ExportJob{
		ID:        newExportJobID(),
		Status:    exportJobStatusRunning,
		Phase:     "queued",
		Message:   "Queued export job",
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.jobsMu.Lock()
	for {
		if _, exists := s.exportJobs[job.ID]; !exists {
			break
		}
		job.ID = newExportJobID()
	}
	s.exportJobs[job.ID] = job
	s.jobsMu.Unlock()
	return job
}

func (s *APIServer) getExportJob(id string) (*ExportJob, bool) {
	s.jobsMu.RLock()
	defer s.jobsMu.RUnlock()
	job, ok := s.exportJobs[id]
	return job, ok
}

func (s *APIServer) runExportAllJob(jobID string, beforeID string, searchPages int) {
	job, ok := s.getExportJob(jobID)
	if !ok {
		return
	}

	job.Set("running", "scanning", "Scanning Nike Run Club pages...")
	candidates, summary, err := s.activitiesAll(context.Background(), beforeID, searchPages, func(search *ActivitySearch, found int) {
		job.UpdateScan(search, found)
	})
	if err != nil {
		job.Fail(fmt.Sprintf("Could not fetch activity list: %v", err))
		return
	}
	if len(candidates) == 0 {
		job.Fail("No activities were found.")
		return
	}

	filename := fmt.Sprintf("nrc-all-activities-%s.zip", time.Now().UTC().Format("2006-01-02"))
	job.StartExport(len(candidates), filename)
	zipBytes, exported, exportErrors := s.exportActivitiesZip(context.Background(), candidates, summary, func(processed int, exported int, total int, errors []string) {
		job.UpdateExport(processed, exported, total, len(errors))
	})
	if exported == 0 {
		message := strings.Join(exportErrors, "; ")
		if message == "" {
			message = "No activities could be exported."
		}
		job.Fail(message)
		return
	}
	job.Complete(filename, zipBytes, len(candidates), exported, exportErrors, summary)
}

func newExportJobID() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

const (
	exportJobStatusRunning   = "running"
	exportJobStatusCompleted = "completed"
	exportJobStatusFailed    = "failed"
)

type ExportJob struct {
	mu                  sync.RWMutex
	ID                  string
	Status              string
	Phase               string
	Message             string
	Error               string
	Filename            string
	DownloadURL         string
	PagesFetched        int
	SearchPages         int
	StopReason          string
	OldestActivityID    string
	OldestEpochMs       int64
	ActivitiesFound     int
	ActivitiesTotal     int
	ActivitiesProcessed int
	ActivitiesExported  int
	ExportErrorCount    int
	ExportErrors        []string
	ZipBytes            []byte
	CreatedAt           time.Time
	UpdatedAt           time.Time
	CompletedAt         time.Time
}

type ExportJobResponse struct {
	ID                  string `json:"id"`
	Status              string `json:"status"`
	Phase               string `json:"phase"`
	Message             string `json:"message"`
	Error               string `json:"error,omitempty"`
	Filename            string `json:"filename,omitempty"`
	DownloadURL         string `json:"download_url,omitempty"`
	PagesFetched        int    `json:"pages_fetched"`
	SearchPages         int    `json:"search_pages"`
	StopReason          string `json:"stop_reason,omitempty"`
	OldestActivityID    string `json:"oldest_activity_id,omitempty"`
	OldestEpochMs       int64  `json:"oldest_epoch_ms,omitempty"`
	ActivitiesFound     int    `json:"activities_found"`
	ActivitiesTotal     int    `json:"activities_total"`
	ActivitiesProcessed int    `json:"activities_processed"`
	ActivitiesExported  int    `json:"activities_exported"`
	ExportErrorCount    int    `json:"export_error_count"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
	CompletedAt         string `json:"completed_at,omitempty"`
}

func (j *ExportJob) Set(status string, phase string, message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = status
	j.Phase = phase
	j.Message = message
	j.UpdatedAt = time.Now().UTC()
}

func (j *ExportJob) UpdateScan(search *ActivitySearch, found int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = exportJobStatusRunning
	j.Phase = "scanning"
	j.ActivitiesFound = found
	if search != nil {
		j.PagesFetched = search.PagesFetched
		j.SearchPages = search.SearchPages
		j.StopReason = search.StopReason
		j.OldestActivityID = search.OldestActivityID
		j.OldestEpochMs = search.OldestEpochMs
	}
	j.Message = fmt.Sprintf("Scanned %d pages, found %d activities", j.PagesFetched, found)
	j.UpdatedAt = time.Now().UTC()
}

func (j *ExportJob) StartExport(total int, filename string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = exportJobStatusRunning
	j.Phase = "exporting"
	j.Message = fmt.Sprintf("Converting 0 of %d activities to TCX", total)
	j.Filename = filename
	j.ActivitiesTotal = total
	j.ActivitiesProcessed = 0
	j.ActivitiesExported = 0
	j.UpdatedAt = time.Now().UTC()
}

func (j *ExportJob) UpdateExport(processed int, exported int, total int, errorCount int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = exportJobStatusRunning
	j.Phase = "exporting"
	j.ActivitiesProcessed = processed
	j.ActivitiesExported = exported
	j.ActivitiesTotal = total
	j.ExportErrorCount = errorCount
	j.Message = fmt.Sprintf("Converted %d of %d activities to TCX", processed, total)
	j.UpdatedAt = time.Now().UTC()
}

func (j *ExportJob) Fail(message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = exportJobStatusFailed
	j.Phase = "failed"
	j.Message = message
	j.Error = message
	j.UpdatedAt = time.Now().UTC()
	j.CompletedAt = j.UpdatedAt
}

func (j *ExportJob) Complete(filename string, zipBytes []byte, candidates int, exported int, exportErrors []string, search *ActivitySearch) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = exportJobStatusCompleted
	j.Phase = "completed"
	j.Message = fmt.Sprintf("Exported %d of %d activities", exported, candidates)
	j.Filename = filename
	j.DownloadURL = fmt.Sprintf("/api/export/jobs/%s/download", j.ID)
	j.ActivitiesFound = candidates
	j.ActivitiesTotal = candidates
	j.ActivitiesProcessed = candidates
	j.ActivitiesExported = exported
	j.ExportErrorCount = len(exportErrors)
	j.ExportErrors = append([]string(nil), exportErrors...)
	j.ZipBytes = zipBytes
	if search != nil {
		j.PagesFetched = search.PagesFetched
		j.SearchPages = search.SearchPages
		j.StopReason = search.StopReason
		j.OldestActivityID = search.OldestActivityID
		j.OldestEpochMs = search.OldestEpochMs
	}
	j.UpdatedAt = time.Now().UTC()
	j.CompletedAt = j.UpdatedAt
}

func (j *ExportJob) Snapshot() ExportJobResponse {
	j.mu.RLock()
	defer j.mu.RUnlock()
	response := ExportJobResponse{
		ID:                  j.ID,
		Status:              j.Status,
		Phase:               j.Phase,
		Message:             j.Message,
		Error:               j.Error,
		Filename:            j.Filename,
		DownloadURL:         j.DownloadURL,
		PagesFetched:        j.PagesFetched,
		SearchPages:         j.SearchPages,
		StopReason:          j.StopReason,
		OldestActivityID:    j.OldestActivityID,
		OldestEpochMs:       j.OldestEpochMs,
		ActivitiesFound:     j.ActivitiesFound,
		ActivitiesTotal:     j.ActivitiesTotal,
		ActivitiesProcessed: j.ActivitiesProcessed,
		ActivitiesExported:  j.ActivitiesExported,
		ExportErrorCount:    j.ExportErrorCount,
		CreatedAt:           j.CreatedAt.Format(time.RFC3339),
		UpdatedAt:           j.UpdatedAt.Format(time.RFC3339),
	}
	if !j.CompletedAt.IsZero() {
		response.CompletedAt = j.CompletedAt.Format(time.RFC3339)
	}
	return response
}

type ActivityListResponse struct {
	Activities []nrc.ActivityListItem `json:"activities"`
	Paging     nrc.Paging             `json:"paging"`
	Search     *ActivitySearch        `json:"search,omitempty"`
}

type ActivitySearch struct {
	AllActivities     bool   `json:"all_activities,omitempty"`
	BeforeDate        string `json:"before_date"`
	CutoffEpochMs     int64  `json:"cutoff_epoch_ms"`
	FromDate          string `json:"from_date,omitempty"`
	ToDate            string `json:"to_date,omitempty"`
	RangeStartEpochMs int64  `json:"range_start_epoch_ms,omitempty"`
	RangeEndEpochMs   int64  `json:"range_end_epoch_ms,omitempty"`
	PagesFetched      int    `json:"pages_fetched"`
	SearchPages       int    `json:"search_pages"`
	Matched           bool   `json:"matched"`
	StopReason        string `json:"stop_reason,omitempty"`
	NextBeforeID      string `json:"next_before_id,omitempty"`
	OldestActivityID  string `json:"oldest_activity_id,omitempty"`
	OldestEpochMs     int64  `json:"oldest_epoch_ms,omitempty"`
}

type nrcActivityDetailResponse struct {
	nrc.Activity
	Route []nrc.RoutePoint `json:"route"`
}

func parseActivityPath(path string) (activityID string, suffix string, ok bool) {
	trimmed := strings.TrimPrefix(path, "/api/activities/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) > 2 || parts[0] == "" {
		return "", "", false
	}
	if len(parts) == 2 {
		return parts[0], parts[1], true
	}
	return parts[0], "", true
}

func firstQueryValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parsePositiveInt(raw string, fallback int, max int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	if max > 0 && value > max {
		return max
	}
	return value
}

func isTruthyQuery(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func parseBeforeDate(raw string) (time.Time, error) {
	return parseQueryDate(raw, "before_date")
}

func parseQueryDate(raw string, field string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("%s is required", field)
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return parsed, nil
	}
	if parsed, err := time.ParseInLocation("02.01.2006", raw, time.Local); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("%s must be YYYY-MM-DD, DD.MM.YYYY, or RFC3339", field)
}

func isDateOnlyQueryDate(raw string) bool {
	raw = strings.TrimSpace(raw)
	if _, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return true
	}
	if _, err := time.ParseInLocation("02.01.2006", raw, time.Local); err == nil {
		return true
	}
	return false
}

func nextBeforeCursor(page *nrc.Activities, currentBeforeID string) string {
	if page == nil {
		return ""
	}
	if page.Paging.BeforeID != "" && page.Paging.BeforeID != currentBeforeID {
		return page.Paging.BeforeID
	}
	if len(page.Activities) == 0 {
		return ""
	}
	lastID := page.Activities[len(page.Activities)-1].ID
	if lastID != "" && lastID != currentBeforeID {
		return lastID
	}
	return ""
}

func updateSearchScan(search *ActivitySearch, activities []nrc.ActivityListItem) {
	if search == nil {
		return
	}
	for _, activity := range activities {
		if activity.StartEpochMs == 0 {
			continue
		}
		if search.OldestEpochMs == 0 || activity.StartEpochMs < search.OldestEpochMs {
			search.OldestEpochMs = activity.StartEpochMs
			search.OldestActivityID = activity.ID
		}
	}
}

func setSearchStop(search *ActivitySearch, reason string) {
	if search == nil || search.StopReason != "" {
		return
	}
	search.StopReason = reason
}

func firstActivityBefore(activities []nrc.ActivityListItem, before time.Time) int {
	cutoff := before.UnixMilli()
	for i, activity := range activities {
		if activity.StartEpochMs < cutoff {
			return i
		}
	}
	return -1
}

func activitiesBefore(activities []nrc.ActivityListItem, before time.Time) []nrc.ActivityListItem {
	if before.IsZero() {
		return activities
	}
	cutoff := before.UnixMilli()
	filtered := make([]nrc.ActivityListItem, 0, len(activities))
	for _, activity := range activities {
		if activity.StartEpochMs < cutoff {
			filtered = append(filtered, activity)
		}
	}
	return filtered
}

func activitiesInRange(activities []nrc.ActivityListItem, start time.Time, end time.Time) []nrc.ActivityListItem {
	startMs := start.UnixMilli()
	endMs := end.UnixMilli()
	filtered := make([]nrc.ActivityListItem, 0, len(activities))
	for _, activity := range activities {
		if activity.StartEpochMs >= startMs && activity.StartEpochMs < endMs {
			filtered = append(filtered, activity)
		}
	}
	return filtered
}

func allActivitiesBefore(activities []nrc.ActivityListItem, before time.Time) bool {
	cutoff := before.UnixMilli()
	hasTimestamp := false
	for _, activity := range activities {
		if activity.StartEpochMs == 0 {
			continue
		}
		hasTimestamp = true
		if activity.StartEpochMs >= cutoff {
			return false
		}
	}
	return hasTimestamp
}

func (s *APIServer) activitiesBeforeDate(ctx context.Context, beforeID string, beforeDate time.Time, beforeDateRaw string, searchPages int) ([]nrc.ActivityListItem, *ActivitySearch, error) {
	if beforeID == "" {
		beforeID = "*"
	}
	search := &ActivitySearch{
		BeforeDate:    beforeDateRaw,
		CutoffEpochMs: beforeDate.UnixMilli(),
		SearchPages:   searchPages,
	}
	candidates := make([]nrc.ActivityListItem, 0)
	dateMatched := false
	seenBeforeIDs := map[string]struct{}{beforeID: {}}

	for pageIndex := 0; pageIndex < searchPages; pageIndex++ {
		page, err := s.nikeClient.FetchActivities(ctx, beforeID, 10)
		if err != nil {
			return nil, search, err
		}
		search.PagesFetched = pageIndex + 1
		updateSearchScan(search, page.Activities)

		if dateMatched {
			candidates = append(candidates, activitiesBefore(page.Activities, beforeDate)...)
		} else if firstPriorIndex := firstActivityBefore(page.Activities, beforeDate); firstPriorIndex >= 0 {
			dateMatched = true
			search.Matched = true
			candidates = append(candidates, activitiesBefore(page.Activities[firstPriorIndex:], beforeDate)...)
		}

		nextBeforeID := nextBeforeCursor(page, beforeID)
		search.NextBeforeID = nextBeforeID
		if len(page.Activities) == 0 {
			setSearchStop(search, "empty_page")
			break
		}
		if nextBeforeID == "" {
			setSearchStop(search, "no_before_id")
			break
		}
		if _, seen := seenBeforeIDs[nextBeforeID]; seen {
			setSearchStop(search, "repeated_before_id")
			break
		}
		seenBeforeIDs[nextBeforeID] = struct{}{}
		beforeID = nextBeforeID
		if pageIndex == searchPages-1 {
			setSearchStop(search, "page_limit")
		}
	}

	return candidates, search, nil
}

func (s *APIServer) activitiesAll(ctx context.Context, beforeID string, searchPages int, progress func(*ActivitySearch, int)) ([]nrc.ActivityListItem, *ActivitySearch, error) {
	if beforeID == "" {
		beforeID = "*"
	}
	search := &ActivitySearch{
		AllActivities: true,
		SearchPages:   searchPages,
	}
	candidates := make([]nrc.ActivityListItem, 0)
	seenBeforeIDs := map[string]struct{}{beforeID: {}}
	seenActivityIDs := make(map[string]struct{})

	for pageIndex := 0; pageIndex < searchPages; pageIndex++ {
		page, err := s.nikeClient.FetchActivities(ctx, beforeID, 10)
		if err != nil {
			return nil, search, err
		}
		search.PagesFetched = pageIndex + 1
		updateSearchScan(search, page.Activities)

		for _, activity := range page.Activities {
			if activity.ID == "" {
				continue
			}
			if _, seen := seenActivityIDs[activity.ID]; seen {
				continue
			}
			seenActivityIDs[activity.ID] = struct{}{}
			candidates = append(candidates, activity)
		}
		search.Matched = len(candidates) > 0
		if progress != nil {
			progress(search, len(candidates))
		}

		nextBeforeID := nextBeforeCursor(page, beforeID)
		search.NextBeforeID = nextBeforeID
		if len(page.Activities) == 0 {
			setSearchStop(search, "empty_page")
			break
		}
		if nextBeforeID == "" {
			setSearchStop(search, "no_before_id")
			break
		}
		if _, seen := seenBeforeIDs[nextBeforeID]; seen {
			setSearchStop(search, "repeated_before_id")
			break
		}
		seenBeforeIDs[nextBeforeID] = struct{}{}
		beforeID = nextBeforeID
		if pageIndex == searchPages-1 {
			setSearchStop(search, "page_limit")
		}
	}

	return candidates, search, nil
}

func (s *APIServer) activitiesInDateRange(ctx context.Context, beforeID string, fromDate time.Time, rangeEnd time.Time, fromDateRaw string, toDateRaw string, searchPages int) ([]nrc.ActivityListItem, *ActivitySearch, error) {
	if beforeID == "" {
		beforeID = "*"
	}
	search := &ActivitySearch{
		FromDate:          fromDateRaw,
		ToDate:            toDateRaw,
		RangeStartEpochMs: fromDate.UnixMilli(),
		RangeEndEpochMs:   rangeEnd.UnixMilli(),
		SearchPages:       searchPages,
	}
	candidates := make([]nrc.ActivityListItem, 0)
	seenBeforeIDs := map[string]struct{}{beforeID: {}}

	for pageIndex := 0; pageIndex < searchPages; pageIndex++ {
		page, err := s.nikeClient.FetchActivities(ctx, beforeID, 10)
		if err != nil {
			return nil, search, err
		}
		search.PagesFetched = pageIndex + 1
		updateSearchScan(search, page.Activities)

		matches := activitiesInRange(page.Activities, fromDate, rangeEnd)
		if len(matches) > 0 {
			search.Matched = true
			candidates = append(candidates, matches...)
		}
		if allActivitiesBefore(page.Activities, fromDate) {
			setSearchStop(search, "range_complete")
			break
		}

		nextBeforeID := nextBeforeCursor(page, beforeID)
		search.NextBeforeID = nextBeforeID
		if len(page.Activities) == 0 {
			setSearchStop(search, "empty_page")
			break
		}
		if nextBeforeID == "" {
			setSearchStop(search, "no_before_id")
			break
		}
		if _, seen := seenBeforeIDs[nextBeforeID]; seen {
			setSearchStop(search, "repeated_before_id")
			break
		}
		seenBeforeIDs[nextBeforeID] = struct{}{}
		beforeID = nextBeforeID
		if pageIndex == searchPages-1 {
			setSearchStop(search, "page_limit")
		}
	}

	return candidates, search, nil
}

func (s *APIServer) exportActivitiesZip(ctx context.Context, candidates []nrc.ActivityListItem, search *ActivitySearch, progress func(int, int, int, []string)) ([]byte, int, []string) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	usedNames := make(map[string]int)
	exportErrors := make([]string, 0)
	exported := 0
	processed := 0

	if progress != nil {
		progress(processed, exported, len(candidates), exportErrors)
	}

	for _, candidate := range candidates {
		processed++
		if candidate.ID == "" {
			if progress != nil {
				progress(processed, exported, len(candidates), exportErrors)
			}
			continue
		}
		activity, err := s.nikeClient.FetchActivity(ctx, candidate.ID)
		if err != nil {
			exportErrors = append(exportErrors, fmt.Sprintf("%s: fetch failed: %v", candidate.ID, err))
			if progress != nil {
				progress(processed, exported, len(candidates), exportErrors)
			}
			continue
		}
		tcxBytes, err := nrc.ExportActivityToTCX(activity)
		if err != nil {
			exportErrors = append(exportErrors, fmt.Sprintf("%s: tcx export failed: %v", candidate.ID, err))
			if progress != nil {
				progress(processed, exported, len(candidates), exportErrors)
			}
			continue
		}

		writer, err := archive.Create(uniqueZipName(nrc.TCXFilename(activity), usedNames))
		if err != nil {
			exportErrors = append(exportErrors, fmt.Sprintf("%s: zip entry failed: %v", candidate.ID, err))
			if progress != nil {
				progress(processed, exported, len(candidates), exportErrors)
			}
			continue
		}
		if _, err := writer.Write(tcxBytes); err != nil {
			exportErrors = append(exportErrors, fmt.Sprintf("%s: zip write failed: %v", candidate.ID, err))
			if progress != nil {
				progress(processed, exported, len(candidates), exportErrors)
			}
			continue
		}
		exported++
		if progress != nil {
			progress(processed, exported, len(candidates), exportErrors)
		}
	}

	_ = writeZipText(archive, "export-summary.txt", exportSummaryText(search, len(candidates), exported, exportErrors))
	if len(exportErrors) > 0 {
		_ = writeZipText(archive, "export-errors.txt", strings.Join(exportErrors, "\n")+"\n")
	}
	if err := archive.Close(); err != nil {
		exportErrors = append(exportErrors, fmt.Sprintf("zip close failed: %v", err))
	}
	return buffer.Bytes(), exported, exportErrors
}

func uniqueZipName(name string, used map[string]int) string {
	if name == "" {
		name = "activity.tcx"
	}
	count := used[name]
	used[name] = count + 1
	if count == 0 {
		return name
	}
	base := strings.TrimSuffix(name, ".tcx")
	return fmt.Sprintf("%s-%d.tcx", base, count+1)
}

func writeZipText(archive *zip.Writer, name string, content string) error {
	writer, err := archive.Create(name)
	if err != nil {
		return err
	}
	_, err = writer.Write([]byte(content))
	return err
}

func exportSummaryText(search *ActivitySearch, candidates int, exported int, exportErrors []string) string {
	var builder strings.Builder
	builder.WriteString("NRC to Strava export\n")
	if search != nil {
		if search.AllActivities {
			builder.WriteString("All activities: true\n")
		}
		if search.BeforeDate != "" {
			builder.WriteString(fmt.Sprintf("Before date: %s\n", search.BeforeDate))
		}
		if search.FromDate != "" || search.ToDate != "" {
			builder.WriteString(fmt.Sprintf("From date: %s\n", search.FromDate))
			builder.WriteString(fmt.Sprintf("To date: %s\n", search.ToDate))
		}
		builder.WriteString(fmt.Sprintf("Pages scanned: %d\n", search.PagesFetched))
		builder.WriteString(fmt.Sprintf("Stop reason: %s\n", search.StopReason))
		if search.OldestEpochMs > 0 {
			builder.WriteString(fmt.Sprintf("Oldest scanned: %s\n", time.UnixMilli(search.OldestEpochMs).UTC().Format(time.RFC3339)))
		}
	}
	builder.WriteString(fmt.Sprintf("Candidates: %d\n", candidates))
	builder.WriteString(fmt.Sprintf("Exported: %d\n", exported))
	builder.WriteString(fmt.Sprintf("Errors: %d\n", len(exportErrors)))
	return builder.String()
}

// --- v1 source and job handlers ---

type sourceConnectionResponse struct {
	ID                string `json:"id"`
	Source            string `json:"source"`
	Status            string `json:"status"`
	ExternalAthleteID string `json:"external_athlete_id,omitempty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type jobResponse struct {
	ID              string         `json:"id"`
	Type            string         `json:"type"`
	Status          string         `json:"status"`
	Phase           string         `json:"phase,omitempty"`
	Message         string         `json:"message,omitempty"`
	Error           string         `json:"error,omitempty"`
	ProgressCurrent int            `json:"progress_current"`
	ProgressTotal   *int           `json:"progress_total,omitempty"`
	Result          map[string]any `json:"result,omitempty"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
	CompletedAt     string         `json:"completed_at,omitempty"`
}

func domainJobToResponse(j domain.Job) jobResponse {
	resp := jobResponse{
		ID:              j.ID,
		Type:            j.Type,
		Status:          string(j.Status),
		Phase:           j.Phase,
		Message:         j.Message,
		Error:           j.LastError,
		ProgressCurrent: j.ProgressCurrent,
		ProgressTotal:   j.ProgressTotal,
		CreatedAt:       j.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       j.UpdatedAt.Format(time.RFC3339),
	}
	if j.CompletedAt != nil {
		resp.CompletedAt = j.CompletedAt.Format(time.RFC3339)
	}
	if len(j.Result) > 0 {
		resp.Result = j.Result
	}
	return resp
}

func domainConnToResponse(c domain.SourceConnection) sourceConnectionResponse {
	return sourceConnectionResponse{
		ID:                c.ID,
		Source:            string(c.Source),
		Status:            c.Status,
		ExternalAthleteID: c.ExternalAthleteID,
		CreatedAt:         c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         c.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *APIServer) handleV1SourcesList(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	conns, err := s.repo.ListSourceConnections(r.Context(), s.athleteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list sources")
		return
	}
	connIDs := make([]string, len(conns))
	for i, c := range conns {
		connIDs[i] = c.ID
	}
	lastJobs, _ := s.repo.GetLastJobsForSourceConnections(r.Context(), s.athleteID, connIDs)

	out := make([]V1SourceResponse, len(conns))
	for i, c := range conns {
		resp := V1SourceResponse{
			ID:                c.ID,
			Source:            string(c.Source),
			Status:            c.Status,
			ExternalAthleteID: c.ExternalAthleteID,
			CreatedAt:         c.CreatedAt.Format(time.RFC3339),
			UpdatedAt:         c.UpdatedAt.Format(time.RFC3339),
		}
		if c.Source == domain.SourceFolderWatch && len(c.Metadata) > 0 {
			resp.Metadata = c.Metadata
		}
		if job, ok := lastJobs[c.ID]; ok {
			resp.LastSyncStatus = string(job.Status)
			if job.CompletedAt != nil {
				resp.LastSyncAt = job.CompletedAt.Format(time.RFC3339)
			}
		}
		out[i] = resp
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *APIServer) handleV1Stats(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	filter := parseStatsFilter(r)
	stats, err := s.repo.GetStats(r.Context(), s.athleteID, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not compute stats")
		return
	}
	resp := StatsResponse{
		TotalActivities:     stats.TotalActivities,
		TotalDistanceMeters: stats.TotalDistanceMeters,
		TotalElapsedMs:      stats.TotalElapsedMs,
		TotalCalories:       stats.TotalCalories,
		AvgPaceSecondsPerKM: stats.AvgPaceSecondsPerKM,
		AvgHeartRate:        stats.AvgHeartRate,
		BySport:             make([]SportStatsItem, len(stats.BySport)),
	}
	for i, s := range stats.BySport {
		resp.BySport[i] = SportStatsItem{
			SportType:      s.SportType,
			ActivityCount:  s.ActivityCount,
			DistanceMeters: s.DistanceMeters,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *APIServer) handleV1StatsTimeseries(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	granularity := r.URL.Query().Get("granularity")
	if granularity == "" {
		granularity = "week"
	}
	filter := parseStatsFilter(r)
	points, err := s.repo.GetTimeseries(r.Context(), s.athleteID, granularity, filter)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := make([]TimeseriesPoint, len(points))
	for i, p := range points {
		out[i] = TimeseriesPoint{
			PeriodStart:    p.PeriodStart.UTC().Format(time.RFC3339),
			DistanceMeters: p.DistanceMeters,
			ActivityCount:  p.ActivityCount,
			AvgPace:        p.AvgPace,
			AvgHeartRate:   p.AvgHeartRate,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *APIServer) handleV1StatsYearReview(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	year := time.Now().UTC().Year() - 1
	if v := r.URL.Query().Get("year"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &year); n != 1 || err != nil {
			writeError(w, http.StatusBadRequest, "invalid year")
			return
		}
	}
	rev, err := s.repo.GetYearReview(r.Context(), s.athleteID, year)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not compute year review")
		return
	}

	type sportItem struct {
		SportType      string  `json:"sport_type"`
		ActivityCount  int     `json:"activity_count"`
		DistanceMeters float64 `json:"distance_meters"`
	}
	type monthItem struct {
		Month          int     `json:"month"`
		DistanceMeters float64 `json:"distance_meters"`
		ActivityCount  int     `json:"activity_count"`
	}
	type bigActivity struct {
		ID             string  `json:"id"`
		Name           string  `json:"name"`
		StartAt        string  `json:"start_at"`
		DistanceMeters float64 `json:"distance_meters"`
	}
	type gearItem struct {
		ID             string  `json:"id"`
		Name           string  `json:"name"`
		ActivityCount  int     `json:"activity_count"`
		DistanceMeters float64 `json:"distance_meters"`
	}

	resp := map[string]any{
		"year":                   rev.Year,
		"total_distance_meters":  rev.TotalDistanceMeters,
		"total_elapsed_ms":       rev.TotalElapsedMs,
		"total_elevation_meters": rev.TotalElevationMeters,
		"total_activities":       rev.TotalActivities,
		"prs_set":                rev.PRsSet,
		"by_sport":               []sportItem{},
		"by_month":               []monthItem{},
	}
	sports := make([]sportItem, len(rev.BySport))
	for i, s := range rev.BySport {
		sports[i] = sportItem{s.SportType, s.ActivityCount, s.DistanceMeters}
	}
	resp["by_sport"] = sports

	months := make([]monthItem, len(rev.ByMonth))
	for i, m := range rev.ByMonth {
		months[i] = monthItem{m.Month, m.DistanceMeters, m.ActivityCount}
	}
	resp["by_month"] = months

	if rev.BiggestActivity != nil {
		resp["biggest_activity"] = bigActivity{
			ID:             rev.BiggestActivity.ID,
			Name:           rev.BiggestActivity.Name,
			StartAt:        rev.BiggestActivity.StartAt.UTC().Format(time.RFC3339),
			DistanceMeters: rev.BiggestActivity.DistanceMeters,
		}
	}
	if rev.MostUsedGear != nil {
		resp["most_used_gear"] = gearItem{
			ID:             rev.MostUsedGear.ID,
			Name:           rev.MostUsedGear.Name,
			ActivityCount:  rev.MostUsedGear.ActivityCount,
			DistanceMeters: rev.MostUsedGear.DistanceMeters,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *APIServer) handleV1StatsPMC(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	q := r.URL.Query()
	var from, to time.Time
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t.UTC()
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t.UTC()
		}
	}
	hrMaxStr, _ := s.repo.GetSetting(r.Context(), "hr_max")
	hrMax := 0
	if n, err := fmt.Sscanf(hrMaxStr, "%d", &hrMax); n != 1 || err != nil {
		hrMax = 0
	}
	points, err := s.repo.GetPMC(r.Context(), s.athleteID, from, to, hrMax)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not compute PMC")
		return
	}
	type pmcPoint struct {
		Date  string  `json:"date"`
		TRIMP float64 `json:"trimp"`
		CTL   float64 `json:"ctl"`
		ATL   float64 `json:"atl"`
		TSB   float64 `json:"tsb"`
	}
	out := make([]pmcPoint, len(points))
	for i, p := range points {
		out[i] = pmcPoint{
			Date:  p.Date.Format("2006-01-02"),
			TRIMP: p.TRIMP,
			CTL:   p.CTL,
			ATL:   p.ATL,
			TSB:   p.TSB,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *APIServer) handleV1StatsStreaks(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	streaks, err := s.repo.GetStreaks(r.Context(), s.athleteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not compute streaks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{
		"current_days":  streaks.CurrentDays,
		"longest_days":  streaks.LongestDays,
		"current_weeks": streaks.CurrentWeeks,
		"longest_weeks": streaks.LongestWeeks,
	})
}

func (s *APIServer) handleV1StatsBests(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	bests, err := s.repo.GetBests(r.Context(), s.athleteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not compute bests")
		return
	}
	resp := map[string]any{}
	if bests.BestWeek != nil {
		resp["best_week"] = map[string]any{
			"period_start":    bests.BestWeek.PeriodStart.UTC().Format("2006-01-02"),
			"distance_meters": bests.BestWeek.DistanceMeters,
		}
	}
	if bests.BestMonth != nil {
		resp["best_month"] = map[string]any{
			"period_start":    bests.BestMonth.PeriodStart.UTC().Format("2006-01-02"),
			"distance_meters": bests.BestMonth.DistanceMeters,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func parseStatsFilter(r *http.Request) storage.StatsFilter {
	q := r.URL.Query()
	f := storage.StatsFilter{SportType: q.Get("sport_type")}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.From = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.To = t.AddDate(0, 0, 1) // to is inclusive
		}
	}
	return f
}

func (s *APIServer) handleV1ActivitiesList(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	q := r.URL.Query()
	filter := storage.ListActivitiesFilter{
		SportType: q.Get("sport_type"),
		Cursor:    q.Get("cursor"),
		Limit:     parsePositiveInt(q.Get("limit"), 20, 100),
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			filter.From = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			filter.To = t.AddDate(0, 0, 1)
		}
	}

	activities, nextCursor, err := s.repo.ListActivities(r.Context(), s.athleteID, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list activities")
		return
	}
	items := make([]ActivityListItem, len(activities))
	for i, a := range activities {
		items[i] = domainActivityToListItem(a)
	}
	writeJSON(w, http.StatusOK, V1ActivityListResponse{Activities: items, NextCursor: nextCursor})
}

func (s *APIServer) handleV1ActivityDetail(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	a, err := s.repo.GetActivity(r.Context(), s.athleteID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "activity not found")
		return
	}
	writeJSON(w, http.StatusOK, domainActivityToDetail(a))
}

func (s *APIServer) handleV1ActivityPatch(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := r.PathValue("id")
	var body struct {
		Name  *string `json:"name"`
		Notes *string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name != nil {
		if err := s.repo.UpdateActivityName(r.Context(), s.athleteID, id, *body.Name); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if body.Notes != nil {
		if err := s.repo.UpdateActivityNotes(r.Context(), s.athleteID, id, *body.Notes); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *APIServer) handleV1ActivityTagSet(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	if err := s.repo.SetActivityTag(r.Context(), s.athleteID, r.PathValue("id"), key, body.Value); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if key == "exclude_from_records" {
		s.scheduleRecomputeBestEfforts()
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "value": body.Value})
}

func (s *APIServer) handleV1ActivityTagDelete(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	key := r.PathValue("key")
	if err := s.repo.DeleteActivityTag(r.Context(), s.athleteID, r.PathValue("id"), key); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if key == "exclude_from_records" {
		s.scheduleRecomputeBestEfforts()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *APIServer) handleV1ActivityDelete(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := r.PathValue("id")
	if err := s.repo.DeleteActivity(r.Context(), s.athleteID, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.invalidateHeatmapCache()
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *APIServer) handleV1ActivityWeather(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := r.PathValue("id")

	// Return cached result if available
	cached, err := s.repo.GetActivityWeather(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "weather lookup failed")
		return
	}
	if cached != nil {
		writeJSON(w, http.StatusOK, weatherResponse(cached))
		return
	}

	// Need GPS + start time to fetch from Open-Meteo
	startAt, lat, lng, ok, err := s.repo.GetActivityStartInfo(r.Context(), id)
	if err != nil || !ok {
		writeError(w, http.StatusNotFound, "no GPS data for this activity")
		return
	}

	// Open-Meteo archive API (free, no key required)
	date := startAt.UTC().Format("2006-01-02")
	url := fmt.Sprintf(
		"https://archive-api.open-meteo.com/v1/archive?latitude=%.6f&longitude=%.6f"+
			"&start_date=%s&end_date=%s"+
			"&hourly=temperature_2m,weather_code,wind_speed_10m,precipitation&timezone=UTC",
		lat, lng, date, date,
	)
	resp, err := http.Get(url) //nolint:gosec // URL is fully controlled
	if err != nil || resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "weather service unavailable")
		return
	}
	defer resp.Body.Close()

	var omResp struct {
		Hourly struct {
			Time          []string  `json:"time"`
			Temperature2m []float64 `json:"temperature_2m"`
			WeatherCode   []int     `json:"weather_code"`
			WindSpeed10m  []float64 `json:"wind_speed_10m"`
			Precipitation []float64 `json:"precipitation"`
		} `json:"hourly"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&omResp); err != nil {
		writeError(w, http.StatusBadGateway, "weather response parse error")
		return
	}

	// Find the hour closest to activity start
	targetHour := startAt.UTC().Hour()
	bestIdx := 0
	bestDiff := 999
	for i, t := range omResp.Hourly.Time {
		if len(t) >= 13 {
			h := int(t[11]-'0')*10 + int(t[12]-'0')
			d := h - targetHour
			if d < 0 {
				d = -d
			}
			if d < bestDiff {
				bestDiff = d
				bestIdx = i
			}
		}
	}

	wx := storage.ActivityWeather{}
	if bestIdx < len(omResp.Hourly.Temperature2m) {
		v := omResp.Hourly.Temperature2m[bestIdx]
		wx.TemperatureC = &v
	}
	if bestIdx < len(omResp.Hourly.WeatherCode) {
		v := omResp.Hourly.WeatherCode[bestIdx]
		wx.WeatherCode = &v
	}
	if bestIdx < len(omResp.Hourly.WindSpeed10m) {
		v := omResp.Hourly.WindSpeed10m[bestIdx] / 3.6
		wx.WindSpeedMS = &v // km/h → m/s
	}
	if bestIdx < len(omResp.Hourly.Precipitation) {
		v := omResp.Hourly.Precipitation[bestIdx]
		wx.PrecipitationMM = &v
	}

	if err := s.repo.UpsertActivityWeather(r.Context(), id, wx); err != nil {
		// Non-fatal — return the data anyway
		_ = err
	}
	writeJSON(w, http.StatusOK, weatherResponse(&wx))
}

func weatherResponse(w *storage.ActivityWeather) map[string]any {
	return map[string]any{
		"temperature_c":    w.TemperatureC,
		"weather_code":     w.WeatherCode,
		"wind_speed_ms":    w.WindSpeedMS,
		"precipitation_mm": w.PrecipitationMM,
	}
}

func (s *APIServer) handleV1ActivitySimilar(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	similar, err := s.repo.GetSimilarActivities(r.Context(), s.athleteID, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not find similar activities")
		return
	}
	type item struct {
		ID             string   `json:"id"`
		Name           string   `json:"name"`
		StartAt        string   `json:"start_at"`
		DistanceMeters *float64 `json:"distance_meters,omitempty"`
		ElapsedMs      *int64   `json:"elapsed_ms,omitempty"`
		PaceSecsPerKM  *float64 `json:"average_pace_seconds_per_km,omitempty"`
		AvgHeartRate   *float64 `json:"average_heart_rate,omitempty"`
	}
	out := make([]item, len(similar))
	for i, s := range similar {
		out[i] = item{
			ID:             s.ID,
			Name:           s.Name,
			StartAt:        s.StartAt.UTC().Format(time.RFC3339),
			DistanceMeters: s.DistanceMeters,
			ElapsedMs:      s.ElapsedMs,
			PaceSecsPerKM:  s.PaceSecsPerKM,
			AvgHeartRate:   s.AvgHeartRate,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *APIServer) handleV1ActivityCard(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := r.PathValue("id")
	act, err := s.repo.GetActivity(r.Context(), s.athleteID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "activity not found")
		return
	}
	samples, err := s.repo.GetActivitySamples(r.Context(), id, 300)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not fetch samples")
		return
	}

	gps := make([][2]float64, 0, len(samples))
	for _, s := range samples {
		if s.Latitude != nil && s.Longitude != nil {
			gps = append(gps, [2]float64{*s.Longitude, *s.Latitude})
		}
	}

	theme := r.URL.Query().Get("theme")
	if theme == "" {
		theme = "dark"
	}
	imperial := r.URL.Query().Get("units") == "imperial"

	ca := cardActivity{
		Name:                act.Name,
		SportType:           string(act.SportType),
		StartAt:             act.StartAt.Format(time.RFC3339),
		DistanceMeters:      act.DistanceMeters,
		ElapsedMs:           durationToMs(act.ElapsedDuration),
		PaceSecsPerKM:       act.AveragePaceSecondsPerKM,
		ElevationGainMeters: act.ElevationGainMeters,
		AvgHeartRate:        act.AverageHeartRate,
		MaxHeartRate:        act.MaxHeartRate,
	}

	svg := GenerateShareCard(ca, gps, theme, imperial)
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(svg))
}

func durationToMs(d *time.Duration) *int64 {
	if d == nil {
		return nil
	}
	ms := d.Milliseconds()
	return &ms
}

func (s *APIServer) handleV1ActivitySamples(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	resolution := parsePositiveInt(r.URL.Query().Get("resolution"), 200, 5000)
	samples, err := s.repo.GetActivitySamples(r.Context(), r.PathValue("id"), resolution)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not fetch samples")
		return
	}
	points := make([]SamplePoint, len(samples))
	for i, s := range samples {
		points[i] = domainSampleToPoint(s)
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *APIServer) handleV1JobsList(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 20, 100)
	jobs, err := s.repo.ListJobs(r.Context(), s.athleteID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list jobs")
		return
	}
	out := make([]jobResponse, len(jobs))
	for i, j := range jobs {
		out[i] = domainJobToResponse(j)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *APIServer) handleV1SourcesNRCConnect(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	token := nrc.SanitizeBearerToken(req.Token)
	if token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	conn, err := s.repo.UpsertSourceConnection(r.Context(), s.athleteID, domain.SourceConnection{
		Source:    domain.SourceNRC,
		SecretKey: token,
		Status:    "active",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save NRC connection")
		return
	}
	writeJSON(w, http.StatusOK, domainConnToResponse(conn))
}

func (s *APIServer) handleV1SourcesFolderConnect(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if info, err := os.Stat(req.Path); err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("path %q does not exist or is not a directory", req.Path))
		return
	}
	conn, err := s.repo.UpsertSourceConnection(r.Context(), s.athleteID, domain.SourceConnection{
		Source:   domain.SourceFolderWatch,
		Status:   "active",
		Metadata: map[string]any{"path": req.Path},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save folder connection")
		return
	}
	s.startFolderWatcher(conn)
	writeJSON(w, http.StatusOK, domainConnToResponse(conn))
}

func (s *APIServer) handleV1SourcesFolderDelete(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	connID := r.PathValue("id")
	s.stopFolderWatcher(connID)
	if err := s.repo.DeleteSourceConnection(r.Context(), connID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete folder connection")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *APIServer) handleV1SourcesFileLog(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	connID := r.PathValue("id")
	files, err := s.repo.ListSyncedFiles(r.Context(), connID, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list synced files")
		return
	}
	type fileItem struct {
		FilePath     string `json:"file_path"`
		SHA256       string `json:"sha256"`
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message,omitempty"`
		SyncedAt     string `json:"synced_at"`
	}
	out := make([]fileItem, len(files))
	for i, f := range files {
		out[i] = fileItem{
			FilePath:     f.FilePath,
			SHA256:       f.SHA256,
			Status:       f.Status,
			ErrorMessage: f.ErrorMessage,
			SyncedAt:     f.SyncedAt.Format(time.RFC3339),
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *APIServer) handleV1SourcesSync(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	sourceID := r.PathValue("id")
	conn, err := s.repo.GetSourceConnection(r.Context(), sourceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "source connection not found")
		return
	}
	dbJob, err := s.repo.CreateJob(r.Context(), s.athleteID, string(conn.Source)+"_sync", map[string]any{
		"source_connection_id": conn.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create sync job")
		return
	}
	writeJSON(w, http.StatusAccepted, domainJobToResponse(dbJob))
	s.runner.Start(dbJob.ID, func(ctx context.Context, update func(storage.JobUpdate)) []byte {
		s.runSyncJob(ctx, dbJob.ID, conn, update)
		return nil
	})
}

// stravaAdapterForRequest returns the pre-configured adapter (from env vars) if set,
// otherwise builds one on-the-fly from credentials stored in app_settings.
func (s *APIServer) stravaAdapterForRequest(ctx context.Context) (*strava.Adapter, error) {
	if s.stravaAdapter != nil {
		return s.stravaAdapter, nil
	}
	if s.repo == nil {
		return nil, fmt.Errorf("Strava credentials not configured — add them in Settings")
	}
	clientID, err := s.repo.GetSetting(ctx, "strava_client_id")
	if err != nil || clientID == "" {
		return nil, fmt.Errorf("Strava credentials not configured — add them in Settings")
	}
	clientSecret, err := s.repo.GetSetting(ctx, "strava_client_secret")
	if err != nil || clientSecret == "" {
		return nil, fmt.Errorf("Strava credentials not configured — add them in Settings")
	}
	return strava.New(
		strava.Config{ClientID: clientID, ClientSecret: clientSecret},
		s.httpClient,
		s.dataDir,
	), nil
}

func (s *APIServer) handleV1SourcesStravaConnect(w http.ResponseWriter, r *http.Request) {
	adapter, err := s.stravaAdapterForRequest(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	// SB-7: generate a CSRF state token, store it with a 10-minute expiry.
	state, err := generateOAuthState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate OAuth state")
		return
	}
	s.stravaOAuthStates.Store(state, time.Now().Add(10*time.Minute))
	callbackURL := s.stravaCallbackURL(r)
	http.Redirect(w, r, adapter.AuthorizeURL(callbackURL, state), http.StatusFound)
}

func (s *APIServer) handleV1SourcesStravaCallback(w http.ResponseWriter, r *http.Request) {
	adapter, err := s.stravaAdapterForRequest(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}

	// SB-7: validate CSRF state token.
	state := r.URL.Query().Get("state")
	if expiry, ok := s.stravaOAuthStates.LoadAndDelete(state); !ok || time.Now().After(expiry.(time.Time)) {
		writeError(w, http.StatusBadRequest, "invalid or expired OAuth state")
		return
	}

	errParam := r.URL.Query().Get("error")
	if errParam != "" {
		writeError(w, http.StatusBadRequest, "Strava authorization denied: "+errParam)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing authorization code")
		return
	}
	meta, externalID, err := adapter.ExchangeCode(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not exchange Strava authorization code")
		return
	}
	_, err = s.repo.UpsertSourceConnection(r.Context(), s.athleteID, domain.SourceConnection{
		Source:            domain.SourceStrava,
		Status:            "active",
		ExternalAthleteID: externalID,
		Metadata:          meta,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save Strava connection")
		return
	}
	// SB-1: redirect the browser back to the Sources page instead of returning JSON.
	http.Redirect(w, r, "/sources", http.StatusFound)
}

// handleV1SettingsGet returns the current app settings (secrets masked).
func (s *APIServer) handleV1SettingsGet(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	clientID, _ := s.repo.GetSetting(r.Context(), "strava_client_id")
	secret, _ := s.repo.GetSetting(r.Context(), "strava_client_secret")
	hrMax, _ := s.repo.GetSetting(r.Context(), "hr_max")
	protoKey, _ := s.repo.GetSetting(r.Context(), "protomaps_key")
	authEnabledRaw, _ := s.repo.GetSetting(r.Context(), "auth_enabled")
	authPasswordSet, _ := s.repo.GetSetting(r.Context(), "auth_password_hash")
	masked := ""
	if secret != "" {
		masked = "••••••••"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"strava_client_id":            clientID,
		"strava_client_secret_set":    secret != "",
		"strava_client_secret_masked": masked,
		"hr_max":                      hrMax,
		"protomaps_key":               protoKey,
		"auth_enabled":                authEnabledRaw == "true",
		"auth_password_set":           authPasswordSet != "",
	})
}

// handleV1SettingsPut saves app settings.
func (s *APIServer) handleV1SettingsPut(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var body struct {
		StravaClientID     string `json:"strava_client_id"`
		StravaClientSecret string `json:"strava_client_secret"`
		HRMax              string `json:"hr_max"`
		ProtomapsKey       string `json:"protomaps_key"`
		AuthEnabled        *bool  `json:"auth_enabled"`
		AuthPassword       string `json:"auth_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.StravaClientID != "" {
		if err := s.repo.SetSetting(r.Context(), "strava_client_id", body.StravaClientID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save setting")
			return
		}
	}
	// Only overwrite the secret if a new value is supplied (empty = keep existing).
	if body.StravaClientSecret != "" {
		if err := s.repo.SetSetting(r.Context(), "strava_client_secret", body.StravaClientSecret); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save setting")
			return
		}
	}
	if body.HRMax != "" {
		if err := s.repo.SetSetting(r.Context(), "hr_max", body.HRMax); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save setting")
			return
		}
	}
	if body.ProtomapsKey != "" {
		if err := s.repo.SetSetting(r.Context(), "protomaps_key", body.ProtomapsKey); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save setting")
			return
		}
	}
	if body.AuthPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(body.AuthPassword), bcrypt.DefaultCost)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not hash password")
			return
		}
		if err := s.repo.SetSetting(r.Context(), "auth_password_hash", string(hash)); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save setting")
			return
		}
	}
	if body.AuthEnabled != nil {
		val := "false"
		if *body.AuthEnabled {
			// Don't allow enabling auth without a password being set.
			existing, _ := s.repo.GetSetting(r.Context(), "auth_password_hash")
			if existing == "" && body.AuthPassword == "" {
				writeError(w, http.StatusBadRequest, "set a password before enabling authentication")
				return
			}
			val = "true"
		}
		if err := s.repo.SetSetting(r.Context(), "auth_enabled", val); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save setting")
			return
		}
	}
	s.loadAuthState(r.Context())
	s.handleV1SettingsGet(w, r)
}

func (s *APIServer) handleV1Job(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	j, err := s.repo.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, domainJobToResponse(j))
}

func (s *APIServer) runSyncJob(ctx context.Context, jobID string, conn domain.SourceConnection, update func(storage.JobUpdate)) {
	switch conn.Source {
	case domain.SourceNRC:
		s.runNRCSyncJob(ctx, jobID, conn, update)
	case domain.SourceStrava:
		s.runStravaSyncJob(ctx, jobID, conn, update)
	case domain.SourceFolderWatch:
		s.runFolderSyncJob(ctx, conn, update)
	default:
		completedAt := time.Now().UTC()
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   fmt.Sprintf("unknown source type: %s", conn.Source),
			CompletedAt: &completedAt,
		})
	}
}

func (s *APIServer) runFolderSyncJob(ctx context.Context, conn domain.SourceConnection, update func(storage.JobUpdate)) {
	now := time.Now().UTC()
	update(storage.JobUpdate{
		Status:    domain.JobRunning,
		Phase:     "scanning",
		Message:   "Scanning folder…",
		StartedAt: &now,
	})

	w := folder.New(conn, s.repo, s.athleteID, s.dataDir)
	imported := w.Scan(ctx)

	completedAt := time.Now().UTC()
	s.invalidateHeatmapCache()
	s.scheduleRecomputeBestEfforts()
	update(storage.JobUpdate{
		Status:          domain.JobSucceeded,
		Phase:           "done",
		Message:         fmt.Sprintf("Imported %d new file(s)", imported),
		ProgressCurrent: imported,
		CompletedAt:     &completedAt,
	})
}

func (s *APIServer) runNRCSyncJob(ctx context.Context, jobID string, conn domain.SourceConnection, update func(storage.JobUpdate)) {
	slog.Info("sync job started", "job_id", jobID, "source", conn.Source)
	now := time.Now().UTC()
	synced := 0

	update(storage.JobUpdate{
		Status:    domain.JobRunning,
		Phase:     "syncing",
		Message:   "Connecting to " + string(conn.Source) + "...",
		StartedAt: &now,
	})

	adapter := nrcsource.New(s.httpClient, s.dataDir)

	err := adapter.Sync(ctx, conn, func(activity domain.SourceActivity) error {
		if !activity.Activity.StartAt.IsZero() {
			if dup := s.repo.FindActivityNearTime(ctx, s.athleteID, activity.Activity.StartAt, string(activity.Activity.SportType), ""); dup != nil {
				return nil
			}
		}
		if _, upsertErr := s.repo.UpsertSourceActivity(ctx, s.athleteID, activity); upsertErr != nil {
			return upsertErr
		}
		synced++
		update(storage.JobUpdate{
			Status:          domain.JobRunning,
			Phase:           "syncing",
			Message:         fmt.Sprintf("Synced %d activities", synced),
			ProgressCurrent: synced,
		})
		return nil
	})

	completedAt := time.Now().UTC()
	if err != nil {
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   err.Error(),
			CompletedAt: &completedAt,
		})
		return
	}
	s.invalidateHeatmapCache()
	s.scheduleRecomputeBestEfforts()
	slog.Info("sync job completed", "job_id", jobID, "source", conn.Source, "synced", synced)
	update(storage.JobUpdate{
		Status:          domain.JobSucceeded,
		Phase:           "done",
		Message:         fmt.Sprintf("Synced %d activities", synced),
		ProgressCurrent: synced,
		CompletedAt:     &completedAt,
	})
}

func (s *APIServer) runStravaSyncJob(ctx context.Context, jobID string, conn domain.SourceConnection, update func(storage.JobUpdate)) {
	slog.Info("sync job started", "job_id", jobID, "source", conn.Source)
	now := time.Now().UTC()
	synced := 0

	update(storage.JobUpdate{
		Status:    domain.JobRunning,
		Phase:     "syncing",
		Message:   "Connecting to Strava...",
		StartedAt: &now,
	})

	adapter, err := s.stravaAdapterForRequest(ctx)
	if err != nil {
		completedAt := time.Now().UTC()
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   err.Error(),
			CompletedAt: &completedAt,
		})
		return
	}

	// SB-3: Refresh access token if it expires soon; abort the job on failure.
	if strava.NeedsRefresh(conn) {
		refreshed, refreshErr := adapter.RefreshToken(ctx, conn)
		if refreshErr != nil {
			completedAt := time.Now().UTC()
			update(storage.JobUpdate{
				Status:      domain.JobFailed,
				Phase:       "failed",
				LastError:   "Strava token refresh failed — please reconnect the Strava integration",
				CompletedAt: &completedAt,
			})
			return
		}
		if updated, dbErr := s.repo.UpsertSourceConnection(ctx, s.athleteID, refreshed); dbErr == nil {
			conn = updated
		}
	}

	// SB-2: Sync returns updated conn with last_synced_at on success.
	updatedConn, err := adapter.Sync(ctx, conn, func(activity domain.SourceActivity) error {
		if !activity.Activity.StartAt.IsZero() {
			if dup := s.repo.FindActivityNearTime(ctx, s.athleteID, activity.Activity.StartAt, string(activity.Activity.SportType), ""); dup != nil {
				return nil
			}
		}
		if _, upsertErr := s.repo.UpsertSourceActivity(ctx, s.athleteID, activity); upsertErr != nil {
			return upsertErr
		}
		synced++
		update(storage.JobUpdate{
			Status:          domain.JobRunning,
			Phase:           "syncing",
			Message:         fmt.Sprintf("Synced %d activities", synced),
			ProgressCurrent: synced,
		})
		return nil
	})

	completedAt := time.Now().UTC()
	if err != nil {
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   err.Error(),
			CompletedAt: &completedAt,
		})
		return
	}

	// SB-2: Persist last_synced_at so the next sync is incremental.
	s.repo.UpsertSourceConnection(ctx, s.athleteID, updatedConn) //nolint:errcheck

	s.invalidateHeatmapCache()
	s.scheduleRecomputeBestEfforts()
	slog.Info("sync job completed", "job_id", jobID, "source", conn.Source, "synced", synced)
	update(storage.JobUpdate{
		Status:          domain.JobSucceeded,
		Phase:           "done",
		Message:         fmt.Sprintf("Synced %d activities", synced),
		ProgressCurrent: synced,
		CompletedAt:     &completedAt,
	})
}

type uploadResponse struct {
	ID               string   `json:"id"`
	SourceActivityID string   `json:"source_activity_id"`
	SourceType       string   `json:"source_type"`
	Name             string   `json:"name,omitempty"`
	SportType        string   `json:"sport_type"`
	StartAt          string   `json:"start_at"`
	DistanceMeters   *float64 `json:"distance_meters,omitempty"`
	Created          bool     `json:"created"`
	// Cross-source duplicate warning: a different activity with the same start time already exists.
	DuplicateOfID   string `json:"duplicate_of_id,omitempty"`
	DuplicateOfName string `json:"duplicate_of_name,omitempty"`
}

func (s *APIServer) handleV1Upload(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "could not parse multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read uploaded file")
		return
	}

	sourceType, err := upload.SourceTypeForFilename(header.Filename)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	sum := sha256.Sum256(data)
	sha256hex := hex.EncodeToString(sum[:])

	conn, err := s.ensureUploadConnection(r.Context(), sourceType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not prepare upload source connection")
		return
	}

	sa, err := upload.Parse(r.Context(), data, header.Filename)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("could not parse file: %v", err))
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	blobKey, _, _ := source.WriteBlobRaw(s.dataDir, data, ext)

	if sa.Activity.Name == "" {
		sa.Activity.Name = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}

	sa.Ref.Source = sourceType
	sa.Ref.SourceConnectionID = conn.ID
	sa.Ref.SourceActivityID = sha256hex
	sa.Ref.RawBlobKey = blobKey
	sa.Ref.RawSHA256 = sha256hex

	result, err := s.repo.UpsertSourceActivity(r.Context(), s.athleteID, sa)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("could not save activity: %v", err))
		return
	}
	s.invalidateHeatmapCache()
	s.scheduleRecomputeBestEfforts()

	created := result.Ref.CreatedAt.Equal(result.Ref.UpdatedAt)
	resp := uploadResponse{
		ID:               result.Activity.ID,
		SourceActivityID: sha256hex,
		SourceType:       string(sourceType),
		Name:             result.Activity.Name,
		SportType:        string(result.Activity.SportType),
		StartAt:          result.Activity.StartAt.Format(time.RFC3339),
		DistanceMeters:   result.Activity.DistanceMeters,
		Created:          created,
	}

	// Cross-source duplicate check: warn if another activity with the same start
	// time already exists (e.g. Strava-synced run + uploaded FIT for the same workout).
	if created && !sa.Activity.StartAt.IsZero() {
		if dup := s.repo.FindActivityNearTime(r.Context(), s.athleteID,
			sa.Activity.StartAt, string(sa.Activity.SportType), result.Activity.ID); dup != nil {
			resp.DuplicateOfID = dup.ID
			resp.DuplicateOfName = dup.Name
			if resp.DuplicateOfName == "" {
				resp.DuplicateOfName = string(dup.SportType)
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *APIServer) handleV1Exports(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var req struct {
		From      string `json:"from"`
		To        string `json:"to"`
		SportType string `json:"sport_type"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	filter := storage.ListActivitiesFilter{SportType: req.SportType}
	if req.From != "" {
		if t, err := time.Parse("2006-01-02", req.From); err == nil {
			filter.From = t
		}
	}
	if req.To != "" {
		if t, err := time.Parse("2006-01-02", req.To); err == nil {
			filter.To = t.AddDate(0, 0, 1) // to is inclusive
		}
	}

	params := map[string]any{"sport_type": req.SportType, "from": req.From, "to": req.To}
	dbJob, err := s.repo.CreateJob(r.Context(), s.athleteID, "export", params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create export job")
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/jobs/%s", dbJob.ID))
	writeJSON(w, http.StatusAccepted, domainJobToResponse(dbJob))
	s.runner.Start(dbJob.ID, func(ctx context.Context, update func(storage.JobUpdate)) []byte {
		s.runDBExportJob(ctx, dbJob.ID, filter, update)
		return nil
	})
}

func (s *APIServer) handleV1ArtifactDownload(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	artifact, err := s.repo.GetArtifact(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	fullPath := filepath.Join(s.dataDir, artifact.StorageKey)
	f, err := os.Open(fullPath)
	if err != nil {
		writeError(w, http.StatusConflict, "artifact file is not available; re-run the export")
		return
	}
	defer f.Close()
	filename := artifact.Filename
	if filename == "" {
		filename = "export.zip"
	}
	ct := artifact.ContentType
	if ct == "" {
		ct = "application/zip"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.FormatInt(artifact.ByteSize, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

func (s *APIServer) runDBExportJob(ctx context.Context, jobID string, filter storage.ListActivitiesFilter, update func(storage.JobUpdate)) {
	now := time.Now().UTC()
	update(storage.JobUpdate{
		Status:    domain.JobRunning,
		Phase:     "collecting",
		Message:   "Collecting activities from database...",
		StartedAt: &now,
	})

	// Page through all matching activities.
	var activities []domain.Activity
	cursor := ""
	for {
		page, next, err := s.repo.ListActivities(ctx, s.athleteID, storage.ListActivitiesFilter{
			SportType: filter.SportType,
			From:      filter.From,
			To:        filter.To,
			Cursor:    cursor,
			Limit:     200,
		})
		if err != nil {
			completedAt := time.Now().UTC()
			update(storage.JobUpdate{
				Status:      domain.JobFailed,
				Phase:       "failed",
				LastError:   fmt.Sprintf("could not list activities: %v", err),
				CompletedAt: &completedAt,
			})
			return
		}
		activities = append(activities, page...)
		if next == "" {
			break
		}
		cursor = next
	}

	total := len(activities)
	if total == 0 {
		completedAt := time.Now().UTC()
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   "no activities found matching the filter",
			CompletedAt: &completedAt,
		})
		return
	}

	update(storage.JobUpdate{
		Status:        domain.JobRunning,
		Phase:         "exporting",
		Message:       fmt.Sprintf("Exporting 0 of %d activities to TCX...", total),
		ProgressTotal: &total,
	})

	// Write the ZIP to data/artifacts/<job-id>.zip.
	artifactDir := filepath.Join(s.dataDir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		completedAt := time.Now().UTC()
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   fmt.Sprintf("could not create artifacts directory: %v", err),
			CompletedAt: &completedAt,
		})
		return
	}

	zipPath := filepath.Join(artifactDir, jobID+".zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		completedAt := time.Now().UTC()
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   fmt.Sprintf("could not create artifact file: %v", err),
			CompletedAt: &completedAt,
		})
		return
	}

	archive := zip.NewWriter(zipFile)
	usedNames := make(map[string]int)
	exported := 0
	var exportErrors []string

	for i, act := range activities {
		samples, err := s.repo.GetActivitySamples(ctx, act.ID, 0)
		if err == nil {
			act.Samples = samples
		}

		tcxBytes, err := export.ExportToTCX(act)
		if err != nil {
			exportErrors = append(exportErrors, fmt.Sprintf("%s: tcx export failed: %v", act.ID, err))
		} else {
			w, zipErr := archive.Create(uniqueZipName(export.TCXFilename(act), usedNames))
			if zipErr == nil {
				_, _ = w.Write(tcxBytes)
				exported++
			}
		}

		processed := i + 1
		update(storage.JobUpdate{
			Status:          domain.JobRunning,
			Phase:           "exporting",
			Message:         fmt.Sprintf("Exported %d of %d activities", exported, total),
			ProgressCurrent: processed,
			ProgressTotal:   &total,
		})
	}

	if len(exportErrors) > 0 {
		_ = writeZipText(archive, "export-errors.txt", strings.Join(exportErrors, "\n")+"\n")
	}
	_ = archive.Close()
	_ = zipFile.Close()

	completedAt := time.Now().UTC()

	if exported == 0 {
		_ = os.Remove(zipPath)
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   "no activities could be exported",
			CompletedAt: &completedAt,
		})
		return
	}

	stat, _ := os.Stat(zipPath)
	var byteSize int64
	if stat != nil {
		byteSize = stat.Size()
	}
	storageKey := filepath.Join("artifacts", jobID+".zip")
	filename := fmt.Sprintf("athlete-log-export-%s.zip", now.Format("2006-01-02"))

	artifact, err := s.repo.CreateArtifact(ctx, domain.Artifact{
		AthleteID:   s.athleteID,
		JobID:       jobID,
		Kind:        "export_zip",
		ContentType: "application/zip",
		Filename:    filename,
		StorageKey:  storageKey,
		ByteSize:    byteSize,
	})
	if err != nil {
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   fmt.Sprintf("could not register artifact: %v", err),
			CompletedAt: &completedAt,
		})
		return
	}

	update(storage.JobUpdate{
		Status:          domain.JobSucceeded,
		Phase:           "done",
		Message:         fmt.Sprintf("Exported %d of %d activities", exported, total),
		ProgressCurrent: total,
		ProgressTotal:   &total,
		ArtifactID:      artifact.ID,
		Result: map[string]any{
			"exported":     exported,
			"total":        total,
			"error_count":  len(exportErrors),
			"artifact_id":  artifact.ID,
			"download_url": fmt.Sprintf("/api/v1/artifacts/%s/download", artifact.ID),
			"filename":     filename,
		},
		CompletedAt: &completedAt,
	})
}

func (s *APIServer) ensureUploadConnection(ctx context.Context, sourceType domain.SourceType) (domain.SourceConnection, error) {
	return s.repo.UpsertSourceConnection(ctx, s.athleteID, domain.SourceConnection{
		Source: sourceType,
		Status: "active",
	})
}

// ─── Heatmap ─────────────────────────────────────────────────────────────────

func (s *APIServer) invalidateHeatmapCache() {
	s.heatmapMu.Lock()
	s.heatmapCache = nil
	s.heatmapMu.Unlock()
}

func (s *APIServer) handleV1Heatmap(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}

	sportType := r.URL.Query().Get("sport_type")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	filtered := sportType != "" || from != "" || to != ""

	// Serve cached payload when no filters are applied.
	if !filtered {
		s.heatmapMu.RLock()
		cached := s.heatmapCache
		s.heatmapMu.RUnlock()
		if cached != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write(cached) //nolint:errcheck
			return
		}
	}

	geojson, err := s.repo.GetHeatmapGeoJSON(r.Context(), s.athleteID, sportType, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build heatmap")
		return
	}

	// Store in cache only for the unfiltered case.
	if !filtered {
		s.heatmapMu.Lock()
		s.heatmapCache = geojson
		s.heatmapMu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(geojson) //nolint:errcheck
}

// generateOAuthState returns a cryptographically random hex token for CSRF protection.
func generateOAuthState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:16]), nil
}

func (s *APIServer) stravaCallbackURL(r *http.Request) string {
	if s.baseURL != "" {
		return s.baseURL + "/api/v1/sources/strava/callback"
	}
	// Fallback: derive from request when BASE_URL is not configured.
	// Acceptable for localhost-only deployments; set BASE_URL for any public exposure.
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/api/v1/sources/strava/callback"
}

// --- DB-backed export job helpers ---

// exportJobResult holds export-specific metadata stored in domain.Job.Result jsonb.
type exportJobResult struct {
	PagesFetched       int    `json:"pages_fetched"`
	SearchPages        int    `json:"search_pages"`
	StopReason         string `json:"stop_reason"`
	OldestActivityID   string `json:"oldest_activity_id"`
	OldestEpochMs      int64  `json:"oldest_epoch_ms"`
	ActivitiesFound    int    `json:"activities_found"`
	ActivitiesExported int    `json:"activities_exported"`
	ExportErrorCount   int    `json:"export_error_count"`
	Filename           string `json:"filename"`
	DownloadURL        string `json:"download_url"`
}

func (r exportJobResult) toMap() map[string]any {
	b, _ := json.Marshal(r)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func mapToExportResult(m map[string]any) exportJobResult {
	b, _ := json.Marshal(m)
	var r exportJobResult
	_ = json.Unmarshal(b, &r)
	return r
}

func jobToExportResponse(j domain.Job) ExportJobResponse {
	result := mapToExportResult(j.Result)
	var progressTotal int
	if j.ProgressTotal != nil {
		progressTotal = *j.ProgressTotal
	}
	resp := ExportJobResponse{
		ID:                  j.ID,
		Status:              string(j.Status),
		Phase:               j.Phase,
		Message:             j.Message,
		Error:               j.LastError,
		Filename:            result.Filename,
		DownloadURL:         result.DownloadURL,
		PagesFetched:        result.PagesFetched,
		SearchPages:         result.SearchPages,
		StopReason:          result.StopReason,
		OldestActivityID:    result.OldestActivityID,
		OldestEpochMs:       result.OldestEpochMs,
		ActivitiesFound:     result.ActivitiesFound,
		ActivitiesTotal:     progressTotal,
		ActivitiesProcessed: j.ProgressCurrent,
		ActivitiesExported:  result.ActivitiesExported,
		ExportErrorCount:    result.ExportErrorCount,
		CreatedAt:           j.CreatedAt.Format(time.RFC3339),
		UpdatedAt:           j.UpdatedAt.Format(time.RFC3339),
	}
	if j.CompletedAt != nil {
		resp.CompletedAt = j.CompletedAt.Format(time.RFC3339)
	}
	return resp
}

func (s *APIServer) handleExportJobDownloadDB(w http.ResponseWriter, j domain.Job, jobID string) {
	if j.Status != domain.JobSucceeded {
		message := j.LastError
		if message == "" {
			message = "export is not complete"
		}
		writeError(w, http.StatusConflict, message)
		return
	}

	// Try in-memory first (fast path for the same server process).
	if zipBytes, ok := s.runner.ZipBytes(jobID); ok {
		result := mapToExportResult(j.Result)
		filename := result.Filename
		if filename == "" {
			filename = "export.zip"
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(zipBytes)
		return
	}

	// Fall back to disk via the artifact record (survives restarts).
	if j.ArtifactID != "" {
		artifact, err := s.repo.GetArtifact(context.Background(), j.ArtifactID)
		if err == nil {
			fullPath := filepath.Join(s.dataDir, artifact.StorageKey)
			if f, err := os.Open(fullPath); err == nil {
				defer f.Close()
				filename := artifact.Filename
				if filename == "" {
					filename = "export.zip"
				}
				w.Header().Set("Content-Type", "application/zip")
				w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
				w.Header().Set("Content-Length", strconv.FormatInt(artifact.ByteSize, 10))
				w.WriteHeader(http.StatusOK)
				_, _ = io.Copy(w, f)
				return
			}
		}
	}

	writeError(w, http.StatusConflict, "export data is no longer available — re-run the export")
}

func (s *APIServer) runExportAllJobDB(ctx context.Context, jobID, beforeID string, searchPages int, update func(storage.JobUpdate)) []byte {
	now := time.Now().UTC()
	result := exportJobResult{SearchPages: searchPages}

	update(storage.JobUpdate{
		Status:    domain.JobRunning,
		Phase:     "scanning",
		Message:   "Scanning Nike Run Club pages...",
		Result:    result.toMap(),
		StartedAt: &now,
	})

	candidates, summary, err := s.activitiesAll(ctx, beforeID, searchPages, func(search *ActivitySearch, found int) {
		result.PagesFetched = search.PagesFetched
		result.ActivitiesFound = found
		result.StopReason = search.StopReason
		result.OldestActivityID = search.OldestActivityID
		result.OldestEpochMs = search.OldestEpochMs
		update(storage.JobUpdate{
			Status:  domain.JobRunning,
			Phase:   "scanning",
			Message: fmt.Sprintf("Scanned %d pages, found %d activities", search.PagesFetched, found),
			Result:  result.toMap(),
		})
	})

	completedAt := time.Now().UTC()

	if err != nil {
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   fmt.Sprintf("Could not fetch activity list: %v", err),
			Result:      result.toMap(),
			CompletedAt: &completedAt,
		})
		return nil
	}
	if len(candidates) == 0 {
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   "No activities were found.",
			Result:      result.toMap(),
			CompletedAt: &completedAt,
		})
		return nil
	}

	filename := fmt.Sprintf("nrc-all-activities-%s.zip", time.Now().UTC().Format("2006-01-02"))
	result.Filename = filename
	result.ActivitiesFound = len(candidates)
	total := len(candidates)

	update(storage.JobUpdate{
		Status:        domain.JobRunning,
		Phase:         "exporting",
		Message:       fmt.Sprintf("Converting 0 of %d activities to TCX", total),
		ProgressTotal: &total,
		Result:        result.toMap(),
	})

	zipBytes, exported, exportErrors := s.exportActivitiesZip(ctx, candidates, summary, func(processed, exported, total int, errs []string) {
		result.ExportErrorCount = len(errs)
		update(storage.JobUpdate{
			Status:          domain.JobRunning,
			Phase:           "exporting",
			Message:         fmt.Sprintf("Converted %d of %d activities to TCX", processed, total),
			ProgressCurrent: processed,
			ProgressTotal:   &total,
			Result:          result.toMap(),
		})
	})

	completedAt = time.Now().UTC()

	if exported == 0 {
		message := strings.Join(exportErrors, "; ")
		if message == "" {
			message = "No activities could be exported."
		}
		update(storage.JobUpdate{
			Status:      domain.JobFailed,
			Phase:       "failed",
			LastError:   message,
			Result:      result.toMap(),
			CompletedAt: &completedAt,
		})
		return nil
	}

	result.ActivitiesExported = exported
	result.ExportErrorCount = len(exportErrors)
	result.DownloadURL = fmt.Sprintf("/api/export/jobs/%s/download", jobID)

	update(storage.JobUpdate{
		Status:          domain.JobSucceeded,
		Phase:           "done",
		Message:         fmt.Sprintf("Exported %d of %d activities", exported, len(candidates)),
		ProgressCurrent: len(candidates),
		ProgressTotal:   &total,
		Result:          result.toMap(),
		CompletedAt:     &completedAt,
	})

	return zipBytes
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeAPIError(w http.ResponseWriter, err error) {
	if errors.Is(err, nrc.ErrMissingToken) {
		writeError(w, http.StatusUnauthorized, "Nike bearer token is not configured")
		return
	}

	var nikeErr *nrc.NikeError
	if errors.As(err, &nikeErr) {
		writeError(w, nikeErr.StatusCode, nikeErr.Error())
		return
	}

	writeError(w, http.StatusBadGateway, err.Error())
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (s *APIServer) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sr, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sr.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func (s *APIServer) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !s.isAllowedOrigin(r, origin) {
				writeError(w, http.StatusForbidden, "origin not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *APIServer) isAllowedOrigin(r *http.Request, origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// Always allow localhost (any scheme).
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	// Allow same-origin requests: origin host matches the incoming Host header.
	requestHost := r.Host
	if h, _, err := net.SplitHostPort(requestHost); err == nil {
		requestHost = h
	}
	if strings.EqualFold(parsed.Hostname(), requestHost) {
		return true
	}
	// Allow the explicitly configured BASE_URL origin.
	if s.baseURL != "" {
		base, err := url.Parse(s.baseURL)
		if err == nil && strings.EqualFold(parsed.Host, base.Host) {
			return true
		}
	}
	return false
}

// ─── Best Efforts / Records ───────────────────────────────────────────────────

// scheduleRecomputeBestEfforts coalesces best-effort recomputation so that at most
// one goroutine is running and at most one additional run is queued. Any number of
// concurrent uploads or syncs that call this will result in at most two total scans.
func (s *APIServer) scheduleRecomputeBestEfforts() {
	if s.repo == nil {
		return
	}
	s.beMu.Lock()
	if s.beBusy {
		s.beNext = true // another run will follow the current one
		s.beMu.Unlock()
		return
	}
	s.beBusy = true
	s.beMu.Unlock()

	go func() {
		for {
			s.recomputeBestEfforts(context.Background())

			s.beMu.Lock()
			if !s.beNext {
				s.beBusy = false
				s.beMu.Unlock()
				return
			}
			s.beNext = false
			s.beMu.Unlock()
			// loop: run once more for the queued request
		}
	}()
}

// recomputeBestEfforts runs best-effort computation for all sports that have activities.
// Call scheduleRecomputeBestEfforts instead — never call this directly from handlers.
func (s *APIServer) recomputeBestEfforts(ctx context.Context) {
	for _, sport := range []string{"run", "ride", "walk", "hike"} {
		if err := s.repo.ComputeAndStoreBestEfforts(ctx, s.athleteID, sport); err != nil {
			slog.Error("best efforts recompute", "sport", sport, "error", err)
		}
	}
}

func (s *APIServer) handleV1Records(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}

	sportType := r.URL.Query().Get("sport_type")

	efforts, err := s.repo.GetBestEfforts(r.Context(), s.athleteID, sportType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not fetch records")
		return
	}

	type distGroup struct {
		DistanceMeters int                  `json:"distance_meters"`
		Efforts        []storage.BestEffort `json:"efforts"`
	}

	// Group by distance, preserving order (already sorted by dist ASC, elapsed ASC).
	var groups []distGroup
	distIdx := map[int]int{}
	for _, e := range efforts {
		idx, ok := distIdx[e.DistanceMeters]
		if !ok {
			idx = len(groups)
			distIdx[e.DistanceMeters] = idx
			groups = append(groups, distGroup{DistanceMeters: e.DistanceMeters})
		}
		groups[idx].Efforts = append(groups[idx].Efforts, e)
	}

	writeJSON(w, http.StatusOK, groups)
}

// ─── Dashboards ───────────────────────────────────────────────────────────────

func (s *APIServer) handleV1DashboardsList(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	dashboards, err := s.repo.ListDashboards(r.Context(), s.athleteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list dashboards")
		return
	}
	if dashboards == nil {
		dashboards = []storage.Dashboard{}
	}
	writeJSON(w, http.StatusOK, dashboards)
}

func (s *APIServer) handleV1DashboardsCreate(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	d, err := s.repo.CreateDashboard(r.Context(), s.athleteID, body.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create dashboard")
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (s *APIServer) handleV1DashboardsGet(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := r.PathValue("id")
	d, err := s.repo.GetDashboard(r.Context(), s.athleteID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *APIServer) handleV1DashboardsUpdate(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := r.PathValue("id")
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	d, err := s.repo.UpdateDashboard(r.Context(), s.athleteID, id, body.Name)
	if err != nil {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *APIServer) handleV1DashboardsDelete(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := r.PathValue("id")
	if err := s.repo.DeleteDashboard(r.Context(), s.athleteID, id); err != nil {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Widgets ──────────────────────────────────────────────────────────────────

func (s *APIServer) handleV1WidgetsList(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := r.PathValue("id")
	widgets, err := s.repo.ListWidgets(r.Context(), id, s.athleteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list widgets")
		return
	}
	if widgets == nil {
		widgets = []storage.Widget{}
	}
	writeJSON(w, http.StatusOK, widgets)
}

func (s *APIServer) handleV1WidgetsCreate(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := r.PathValue("id")
	var body struct {
		Type     string          `json:"type"`
		Config   json.RawMessage `json:"config"`
		Position json.RawMessage `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	if len(body.Config) == 0 {
		body.Config = json.RawMessage(`{}`)
	}
	if len(body.Position) == 0 {
		body.Position = json.RawMessage(`{"x":0,"y":0,"w":4,"h":3}`)
	}
	widget, err := s.repo.CreateWidget(r.Context(), id, s.athleteID, body.Type, body.Config, body.Position)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create widget")
		return
	}
	writeJSON(w, http.StatusCreated, widget)
}

func (s *APIServer) handleV1WidgetsUpdate(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	dashID := r.PathValue("id")
	widID := r.PathValue("wid")
	var body struct {
		Config   json.RawMessage `json:"config"`
		Position json.RawMessage `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	widget, err := s.repo.UpdateWidget(r.Context(), widID, dashID, s.athleteID, body.Config, body.Position)
	if err != nil {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}
	writeJSON(w, http.StatusOK, widget)
}

func (s *APIServer) handleV1WidgetsDelete(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	dashID := r.PathValue("id")
	widID := r.PathValue("wid")
	if err := s.repo.DeleteWidget(r.Context(), widID, dashID, s.athleteID); err != nil {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Profile ──────────────────────────────────────────────────────────────────

func (s *APIServer) handleV1ProfileGet(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	a, err := s.repo.GetAthlete(r.Context(), s.athleteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not fetch profile")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *APIServer) handleV1ProfilePut(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		StartedAt   string `json:"started_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	a, err := s.repo.UpdateAthlete(r.Context(), s.athleteID, body.DisplayName, body.Email, body.StartedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update profile")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *APIServer) handleV1ProfileAvatarUpload(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil || s.dataDir == "" {
		writeError(w, http.StatusServiceUnavailable, "storage not configured")
		return
	}
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "file too large (max 5 MB)")
		return
	}
	file, header, err := r.FormFile("avatar")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing avatar file")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		writeError(w, http.StatusBadRequest, "unsupported image type (jpg, png, webp only)")
		return
	}

	dest := filepath.Join(s.dataDir, "avatar"+ext)
	// Remove old avatars with other extensions
	for _, old := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		if old != ext {
			os.Remove(filepath.Join(s.dataDir, "avatar"+old)) //nolint:errcheck
		}
	}

	out, err := os.Create(dest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save avatar")
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		writeError(w, http.StatusInternalServerError, "could not write avatar")
		return
	}

	if err := s.repo.SetAthleteAvatar(r.Context(), s.athleteID, "avatar"+ext); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update profile")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatar_path": "avatar" + ext})
}

func (s *APIServer) handleV1ProfileAvatarServe(w http.ResponseWriter, r *http.Request) {
	if s.dataDir == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		p := filepath.Join(s.dataDir, "avatar"+ext)
		if _, err := os.Stat(p); err == nil {
			http.ServeFile(w, r, p)
			return
		}
	}
	writeError(w, http.StatusNotFound, "no avatar set")
}

// ─── Gear ─────────────────────────────────────────────────────────────────────

type gearBody struct {
	Name                string  `json:"name"`
	Type                string  `json:"type"`
	BrandModel          string  `json:"brand_model"`
	PurchaseDate        *string `json:"purchase_date"`
	DistanceAlertMeters *int    `json:"distance_alert_meters"`
	Retire              bool    `json:"retire"`
}

func (s *APIServer) handleV1GearList(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	includeRetired := r.URL.Query().Get("retired") == "true"
	gear, err := s.repo.ListGear(r.Context(), s.athleteID, includeRetired)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list gear")
		return
	}
	if gear == nil {
		gear = []storage.Gear{}
	}
	writeJSON(w, http.StatusOK, gear)
}

func (s *APIServer) handleV1GearCreate(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var body gearBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if body.Type == "" {
		body.Type = "other"
	}
	g, err := s.repo.CreateGear(r.Context(), s.athleteID, body.Name, body.Type, body.BrandModel, body.PurchaseDate, body.DistanceAlertMeters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create gear")
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *APIServer) handleV1GearUpdate(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := r.PathValue("id")
	var body gearBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	g, err := s.repo.UpdateGear(r.Context(), s.athleteID, id, body.Name, body.Type, body.BrandModel, body.PurchaseDate, body.DistanceAlertMeters, body.Retire)
	if err != nil {
		writeError(w, http.StatusNotFound, "gear not found")
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *APIServer) handleV1GearDelete(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	if err := s.repo.DeleteGear(r.Context(), s.athleteID, r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "gear not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *APIServer) handleV1ActivityGearGet(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	gear, err := s.repo.GetActivityGear(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not fetch gear")
		return
	}
	if gear == nil {
		gear = []storage.Gear{}
	}
	writeJSON(w, http.StatusOK, gear)
}

func (s *APIServer) handleV1ActivityGearSet(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var body struct {
		GearIDs []string `json:"gear_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.repo.SetActivityGear(r.Context(), r.PathValue("id"), body.GearIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update gear")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
