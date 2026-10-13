package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"athlete-log/internal/storage"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// buildMCPServer creates an MCP server wired to the APIServer's repository.
func (s *APIServer) buildMCPServer() *server.MCPServer {
	srv := server.NewMCPServer(
		"Athlete Log",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithPromptCapabilities(false),
	)

	// ── Tools ──────────────────────────────────────────────────────────────────

	srv.AddTool(mcp.NewTool("get_summary",
		mcp.WithDescription("Get total distance, time, and activity count for a time period"),
		mcp.WithString("from", mcp.Description("Start date ISO 8601, e.g. 2024-01-01")),
		mcp.WithString("to", mcp.Description("End date ISO 8601, e.g. 2024-12-31")),
		mcp.WithString("sport_type", mcp.Description("Filter by sport: run, ride, swim, walk, hike")),
	), s.mcpGetSummary)

	srv.AddTool(mcp.NewTool("get_recent_activities",
		mcp.WithDescription("List recent activities with name, date, distance and duration"),
		mcp.WithNumber("limit", mcp.Description("Number of activities to return (default 10, max 50)")),
		mcp.WithString("sport_type", mcp.Description("Filter by sport type")),
	), s.mcpGetRecentActivities)

	srv.AddTool(mcp.NewTool("get_personal_records",
		mcp.WithDescription("Get best efforts per distance (PR at 1km, 5km, 10km, HM, FM, etc.)"),
		mcp.WithString("sport_type", mcp.Description("Sport type, default: run")),
	), s.mcpGetPersonalRecords)

	srv.AddTool(mcp.NewTool("get_fitness_trends",
		mcp.WithDescription("Get weekly distance totals over the past N weeks to show training load trends"),
		mcp.WithNumber("weeks", mcp.Description("Number of weeks to look back (default 12)")),
		mcp.WithString("sport_type", mcp.Description("Filter by sport type")),
	), s.mcpGetFitnessTrends)

	srv.AddTool(mcp.NewTool("get_training_load",
		mcp.WithDescription("Get acute (7-day) vs chronic (28-day) training load ratio to assess recovery"),
		mcp.WithString("sport_type", mcp.Description("Filter by sport type")),
	), s.mcpGetTrainingLoad)

	srv.AddTool(mcp.NewTool("get_streaks",
		mcp.WithDescription("Get current and longest activity streaks in days and weeks"),
	), s.mcpGetStreaks)

	srv.AddTool(mcp.NewTool("search_activities",
		mcp.WithDescription("Search and filter activities by date range, sport type, or minimum distance"),
		mcp.WithString("from", mcp.Description("Start date ISO 8601")),
		mcp.WithString("to", mcp.Description("End date ISO 8601")),
		mcp.WithString("sport_type", mcp.Description("Sport type filter")),
		mcp.WithNumber("min_distance_km", mcp.Description("Minimum distance in kilometres")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 20)")),
	), s.mcpSearchActivities)

	// ── Prompts ────────────────────────────────────────────────────────────────

	srv.AddPrompt(mcp.NewPrompt("create_training_plan",
		mcp.WithPromptDescription("Generate a personalised training plan based on recent history"),
		mcp.WithArgument("goal", mcp.ArgumentDescription("Target race or fitness goal, e.g. 'sub-4h marathon'"), mcp.RequiredArgument()),
		mcp.WithArgument("weeks", mcp.ArgumentDescription("Plan duration in weeks, e.g. 8")),
	), s.mcpPromptTrainingPlan)

	srv.AddPrompt(mcp.NewPrompt("analyze_fitness",
		mcp.WithPromptDescription("Review training trends and highlight strengths and areas for improvement"),
	), s.mcpPromptAnalyzeFitness)

	srv.AddPrompt(mcp.NewPrompt("predict_race",
		mcp.WithPromptDescription("Predict finish time for a target race distance based on recent training"),
		mcp.WithArgument("distance", mcp.ArgumentDescription("Race distance, e.g. '5km', 'half marathon'"), mcp.RequiredArgument()),
	), s.mcpPromptPredictRace)

	srv.AddPrompt(mcp.NewPrompt("recovery_check",
		mcp.WithPromptDescription("Assess acute:chronic load ratio and flag overtraining risk"),
	), s.mcpPromptRecoveryCheck)

	return srv
}

// handleMCP is the HTTP handler for POST /api/v1/mcp.
func (s *APIServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	mcpSrv := s.buildMCPServer()
	httpSrv := server.NewStreamableHTTPServer(mcpSrv)
	httpSrv.ServeHTTP(w, r)
}

// ── Tool handlers ──────────────────────────────────────────────────────────────

func (s *APIServer) mcpGetSummary(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	filter := storage.StatsFilter{}
	if v, ok := req.GetArguments()["from"].(string); ok && v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			filter.From = t
		}
	}
	if v, ok := req.GetArguments()["to"].(string); ok && v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			filter.To = t
		}
	}
	if v, ok := req.GetArguments()["sport_type"].(string); ok {
		filter.SportType = v
	}
	stats, err := s.repo.GetStats(ctx, s.athleteID, filter)
	if err != nil {
		return mcp.NewToolResultError("failed to get stats: " + err.Error()), nil
	}
	text := fmt.Sprintf(
		"Total activities: %d\nTotal distance: %.1f km\nTotal time: %s\nTotal calories: %d",
		stats.TotalActivities,
		stats.TotalDistanceMeters/1000,
		mcpFormatElapsedMs(stats.TotalElapsedMs),
		stats.TotalCalories,
	)
	if len(stats.BySport) > 0 {
		text += "\n\nBreakdown by sport:"
		for _, sp := range stats.BySport {
			text += fmt.Sprintf("\n  %s: %d activities, %.1f km", sp.SportType, sp.ActivityCount, sp.DistanceMeters/1000)
		}
	}
	return mcp.NewToolResultText(text), nil
}

func (s *APIServer) mcpGetRecentActivities(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limit := 10
	if v, ok := req.GetArguments()["limit"].(float64); ok && v > 0 {
		limit = int(v)
		if limit > 50 {
			limit = 50
		}
	}
	filter := storage.ListActivitiesFilter{Limit: limit}
	if v, ok := req.GetArguments()["sport_type"].(string); ok {
		filter.SportType = v
	}
	acts, _, err := s.repo.ListActivities(ctx, s.athleteID, filter)
	if err != nil {
		return mcp.NewToolResultError("failed to list activities: " + err.Error()), nil
	}
	var lines []string
	for _, a := range acts {
		dist := 0.0
		if a.DistanceMeters != nil {
			dist = *a.DistanceMeters / 1000
		}
		lines = append(lines, fmt.Sprintf("- %s | %s | %.1f km | %s",
			a.StartAt.Format("2006-01-02"),
			a.Name,
			dist,
			mcpFormatDuration(a.ElapsedDuration),
		))
	}
	if len(lines) == 0 {
		return mcp.NewToolResultText("No activities found."), nil
	}
	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}

func (s *APIServer) mcpGetPersonalRecords(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sport := "run"
	if v, ok := req.GetArguments()["sport_type"].(string); ok && v != "" {
		sport = v
	}
	efforts, err := s.repo.GetBestEfforts(ctx, s.athleteID, sport)
	if err != nil {
		return mcp.NewToolResultError("failed to get records: " + err.Error()), nil
	}
	if len(efforts) == 0 {
		return mcp.NewToolResultText("No personal records found for " + sport + "."), nil
	}
	// GetBestEfforts returns rows sorted by distance ASC, elapsed_ms ASC.
	// Take only the first (best) result per distance.
	seen := map[int]bool{}
	var lines []string
	lines = append(lines, fmt.Sprintf("Personal records for %s:", sport))
	for _, e := range efforts {
		if seen[e.DistanceMeters] {
			continue
		}
		seen[e.DistanceMeters] = true
		lines = append(lines, fmt.Sprintf("  %s: %s (on %s)",
			mcpDistLabel(e.DistanceMeters),
			mcpFormatMs(e.ElapsedMs),
			e.AchievedAt.Format("2006-01-02"),
		))
	}
	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}

func (s *APIServer) mcpGetFitnessTrends(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	weeks := 12
	if v, ok := req.GetArguments()["weeks"].(float64); ok && v > 0 {
		weeks = int(v)
	}
	filter := storage.StatsFilter{
		From: time.Now().AddDate(0, 0, -weeks*7),
	}
	if v, ok := req.GetArguments()["sport_type"].(string); ok {
		filter.SportType = v
	}
	points, err := s.repo.GetTimeseries(ctx, s.athleteID, "week", filter)
	if err != nil {
		return mcp.NewToolResultError("failed to get trends: " + err.Error()), nil
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("Weekly distance over last %d weeks:", weeks))
	for _, p := range points {
		lines = append(lines, fmt.Sprintf("  w/c %s: %.1f km (%d activities)",
			p.PeriodStart.Format("2006-01-02"), p.DistanceMeters/1000, p.ActivityCount))
	}
	if len(lines) == 1 {
		return mcp.NewToolResultText("No data found."), nil
	}
	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}

func (s *APIServer) mcpGetTrainingLoad(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sport := ""
	if v, ok := req.GetArguments()["sport_type"].(string); ok {
		sport = v
	}
	now := time.Now()
	filter7 := storage.StatsFilter{SportType: sport, From: now.AddDate(0, 0, -7)}
	filter28 := storage.StatsFilter{SportType: sport, From: now.AddDate(0, 0, -28)}

	s7, err := s.repo.GetStats(ctx, s.athleteID, filter7)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	s28, err := s.repo.GetStats(ctx, s.athleteID, filter28)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	acute := s7.TotalDistanceMeters / 1000
	chronic := s28.TotalDistanceMeters / 1000 / 4.0
	ratio := 0.0
	if chronic > 0 {
		ratio = acute / chronic
	}
	assessment := "balanced"
	if ratio > 1.5 {
		assessment = "HIGH — risk of overtraining, consider recovery"
	} else if ratio < 0.8 {
		assessment = "LOW — below baseline, consider increasing load"
	}
	text := fmt.Sprintf(
		"Acute load (7d): %.1f km/week\nChronic load (28d avg): %.1f km/week\nACWR ratio: %.2f\nAssessment: %s",
		acute, chronic, ratio, assessment,
	)
	return mcp.NewToolResultText(text), nil
}

func (s *APIServer) mcpGetStreaks(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	streaks, err := s.repo.GetStreaks(ctx, s.athleteID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	text := fmt.Sprintf(
		"Current streak: %d days / %d weeks\nLongest streak: %d days / %d weeks",
		streaks.CurrentDays, streaks.CurrentWeeks,
		streaks.LongestDays, streaks.LongestWeeks,
	)
	return mcp.NewToolResultText(text), nil
}

func (s *APIServer) mcpSearchActivities(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limit := 20
	if v, ok := req.GetArguments()["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	filter := storage.ListActivitiesFilter{Limit: limit}
	if v, ok := req.GetArguments()["sport_type"].(string); ok {
		filter.SportType = v
	}
	if v, ok := req.GetArguments()["from"].(string); ok && v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			filter.From = t
		}
	}
	if v, ok := req.GetArguments()["to"].(string); ok && v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			filter.To = t
		}
	}
	minDist := 0.0
	if v, ok := req.GetArguments()["min_distance_km"].(float64); ok {
		minDist = v * 1000
	}
	acts, _, err := s.repo.ListActivities(ctx, s.athleteID, filter)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var lines []string
	for _, a := range acts {
		dist := 0.0
		if a.DistanceMeters != nil {
			dist = *a.DistanceMeters
		}
		if minDist > 0 && dist < minDist {
			continue
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s | %s | %.1f km | %s",
			a.SportType,
			a.StartAt.Format("2006-01-02"),
			a.Name,
			dist/1000,
			mcpFormatDuration(a.ElapsedDuration),
		))
	}
	if len(lines) == 0 {
		return mcp.NewToolResultText("No activities match the filter."), nil
	}
	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}

// ── Prompt handlers ────────────────────────────────────────────────────────────

func (s *APIServer) mcpPromptTrainingPlan(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	goal := req.Params.Arguments["goal"]
	weeks := req.Params.Arguments["weeks"]
	if weeks == "" {
		weeks = "8"
	}
	summary, _ := s.mcpContextSummary(ctx)
	msg := fmt.Sprintf(`You are an expert running coach. Here is the athlete's recent training data:

%s

Based on this data, create a detailed %s-week training plan with the goal: %s.
Include weekly structure, key workouts, and progression. Be specific about distances and intensities.`, summary, weeks, goal)
	return &mcp.GetPromptResult{
		Description: "Training plan prompt",
		Messages: []mcp.PromptMessage{
			{Role: mcp.RoleUser, Content: mcp.NewTextContent(msg)},
		},
	}, nil
}

func (s *APIServer) mcpPromptAnalyzeFitness(ctx context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	summary, _ := s.mcpContextSummary(ctx)
	msg := fmt.Sprintf(`You are an expert running coach. Here is the athlete's training data:

%s

Review this training data and provide:
1. What's going well in the training
2. Areas that need improvement
3. Specific recommendations for the next 4 weeks
Be concise and actionable.`, summary)
	return &mcp.GetPromptResult{
		Description: "Fitness analysis prompt",
		Messages: []mcp.PromptMessage{
			{Role: mcp.RoleUser, Content: mcp.NewTextContent(msg)},
		},
	}, nil
}

func (s *APIServer) mcpPromptPredictRace(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	distance := req.Params.Arguments["distance"]
	records, _ := s.repo.GetBestEfforts(ctx, s.athleteID, "run")
	seen := map[int]bool{}
	var recordLines []string
	for _, e := range records {
		if seen[e.DistanceMeters] {
			continue
		}
		seen[e.DistanceMeters] = true
		recordLines = append(recordLines, fmt.Sprintf("  %s PR: %s", mcpDistLabel(e.DistanceMeters), mcpFormatMs(e.ElapsedMs)))
	}
	msg := fmt.Sprintf(`You are an expert running coach.

Current personal records:
%s

Using the Riegel formula (T2 = T1 × (D2/D1)^1.06) and these PRs, predict the athlete's finish time for a %s race.
Show your calculation steps and provide realistic best case / expected / conservative estimates.`,
		strings.Join(recordLines, "\n"), distance)
	return &mcp.GetPromptResult{
		Description: "Race prediction prompt",
		Messages: []mcp.PromptMessage{
			{Role: mcp.RoleUser, Content: mcp.NewTextContent(msg)},
		},
	}, nil
}

func (s *APIServer) mcpPromptRecoveryCheck(ctx context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	now := time.Now()
	s7, _ := s.repo.GetStats(ctx, s.athleteID, storage.StatsFilter{From: now.AddDate(0, 0, -7)})
	s28, _ := s.repo.GetStats(ctx, s.athleteID, storage.StatsFilter{From: now.AddDate(0, 0, -28)})
	acute := s7.TotalDistanceMeters / 1000
	chronic := s28.TotalDistanceMeters / 1000 / 4.0
	ratio := 0.0
	if chronic > 0 {
		ratio = acute / chronic
	}
	msg := fmt.Sprintf(`You are an expert coach reviewing an athlete's training load.

Acute load (last 7 days): %.1f km
Chronic load (28-day average): %.1f km/week
ACWR ratio: %.2f

Is this athlete at risk of overtraining or undertraining? What should they do this week to optimise recovery and performance? Be specific.`,
		acute, chronic, ratio)
	return &mcp.GetPromptResult{
		Description: "Recovery check prompt",
		Messages: []mcp.PromptMessage{
			{Role: mcp.RoleUser, Content: mcp.NewTextContent(msg)},
		},
	}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (s *APIServer) mcpContextSummary(ctx context.Context) (string, error) {
	now := time.Now()
	s12w, err := s.repo.GetStats(ctx, s.athleteID, storage.StatsFilter{From: now.AddDate(0, 0, -84)})
	if err != nil {
		return "", err
	}
	streaks, _ := s.repo.GetStreaks(ctx, s.athleteID)
	acts, _, _ := s.repo.ListActivities(ctx, s.athleteID, storage.ListActivitiesFilter{Limit: 5})
	var recentLines []string
	for _, a := range acts {
		dist := 0.0
		if a.DistanceMeters != nil {
			dist = *a.DistanceMeters / 1000
		}
		recentLines = append(recentLines, fmt.Sprintf("  - %s: %s, %.1f km, %s",
			a.StartAt.Format("2006-01-02"), a.Name, dist, mcpFormatDuration(a.ElapsedDuration)))
	}
	return fmt.Sprintf(`Last 12 weeks: %d activities, %.1f km total
Current streak: %d days
Recent activities:
%s`,
		s12w.TotalActivities,
		s12w.TotalDistanceMeters/1000,
		streaks.CurrentDays,
		strings.Join(recentLines, "\n"),
	), nil
}

// mcpFormatElapsedMs formats a millisecond count as "Xh Ym" or "Ym".
func mcpFormatElapsedMs(ms int64) string {
	if ms <= 0 {
		return "0m"
	}
	min := ms / 60000
	h := min / 60
	m := min % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// mcpFormatDuration formats a *time.Duration as "Xh Ym" or "Ym".
func mcpFormatDuration(d *time.Duration) string {
	if d == nil || *d <= 0 {
		return "—"
	}
	min := int64(d.Minutes())
	h := min / 60
	m := min % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// mcpFormatMs formats a millisecond integer as "H:MM:SS" or "M:SS".
func mcpFormatMs(ms int) string {
	if ms <= 0 {
		return "—"
	}
	sec := ms / 1000
	h := sec / 3600
	min := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, min, s)
	}
	return fmt.Sprintf("%d:%02d", min, s)
}

func mcpDistLabel(meters int) string {
	labels := map[int]string{
		400: "400m", 1000: "1km", 1609: "1 mile",
		5000: "5km", 10000: "10km", 15000: "15km",
		21097: "Half Marathon", 42195: "Marathon",
	}
	if l, ok := labels[meters]; ok {
		return l
	}
	return fmt.Sprintf("%.1f km", float64(meters)/1000)
}

// mcpJSON marshals a value to a JSON string (used in tests).
func mcpJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// handleMCPBundle serves a .mcpb bundle (ZIP) that Claude Desktop opens to install
// the MCP server via its native "Connect your agent" dialog.
func (s *APIServer) handleMCPBundle(w http.ResponseWriter, r *http.Request) {
	// Determine the externally-reachable origin.
	origin := s.baseURL
	if origin == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		origin = scheme + "://" + r.Host
	}
	endpoint := origin + "/api/v1/mcp"

	// Embed the bearer token when the caller is authenticated.
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")

	args := []string{"-y", "mcp-remote", endpoint, "--allow-http", "--transport", "http-only"}

	type mcpCfg struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env,omitempty"`
	}
	type serverCfg struct {
		Type      string `json:"type"`
		MCPConfig mcpCfg `json:"mcp_config"`
	}
	type manifestDoc struct {
		ManifestVersion string `json:"manifest_version"`
		Name            string `json:"name"`
		DisplayName     string `json:"display_name"`
		Version         string `json:"version"`
		Description     string `json:"description"`
		Author          struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"author"`
		Keywords []string  `json:"keywords"`
		Server   serverCfg `json:"server"`
	}

	var m manifestDoc
	m.ManifestVersion = "0.4"
	m.Name = "athlete-log"
	m.DisplayName = "Athlete Log"
	m.Version = "1.0.0"
	m.Description = "Access your training data, personal records, and fitness trends from Claude Desktop."
	m.Author.Name = "Athlete Log"
	m.Author.URL = "https://github.com/nowakkuba99/athlete-log"
	m.Keywords = []string{"fitness", "running", "training", "sports"}
	m.Server = serverCfg{
		Type: "node",
		MCPConfig: mcpCfg{
			Command: "npx",
			Args:    args,
		},
	}
	if token != "" {
		m.Server.MCPConfig.Env = map[string]string{"MCP_REMOTE_AUTH": "Bearer " + token}
	}

	manifestJSON, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate manifest")
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("manifest.json")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create bundle")
		return
	}
	if _, err = f.Write(manifestJSON); err != nil {
		writeError(w, http.StatusInternalServerError, "could not write manifest")
		return
	}
	if err = zw.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not finalize bundle")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="athlete-log.mcpb"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
