package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"athlete-log/internal/nrc"
)

func TestCORSAllowsOnlyLocalOrigins(t *testing.T) {
	s := &APIServer{}
	handler := s.withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	blocked := httptest.NewRecorder()
	blockedReq := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	blockedReq.Host = "myapp.internal"
	blockedReq.Header.Set("Origin", "https://example.com")
	handler.ServeHTTP(blocked, blockedReq)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("blocked status = %d, want %d", blocked.Code, http.StatusForbidden)
	}

	allowed := httptest.NewRecorder()
	allowedReq := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	allowedReq.Header.Set("Origin", "http://localhost:5173")
	handler.ServeHTTP(allowed, allowedReq)
	if allowed.Code != http.StatusNoContent {
		t.Fatalf("allowed status = %d, want %d", allowed.Code, http.StatusNoContent)
	}
	if allowed.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q", allowed.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestActivitiesBeforeDateSearchFetchesUntilFirstPriorActivity(t *testing.T) {
	paths := make([]string, 0)
	nikeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Query().Get("limit") != "2" {
			t.Fatalf("limit = %q", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/plus/v3/activities/before_id/v3/*":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "may-03", StartEpochMs: mustEpochMs(t, "2025-05-03T10:00:00Z")},
					{ID: "may-02", StartEpochMs: mustEpochMs(t, "2025-05-02T10:00:00Z")},
				},
				Paging: nrc.Paging{BeforeID: "older"},
			})
		case "/plus/v3/activities/before_id/v3/older":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "apr-30", StartEpochMs: mustEpochMs(t, "2025-04-30T10:00:00Z")},
					{ID: "apr-29", StartEpochMs: mustEpochMs(t, "2025-04-29T10:00:00Z")},
					{ID: "may-04-out-of-order", StartEpochMs: mustEpochMs(t, "2025-05-04T10:00:00Z")},
				},
				Paging: nrc.Paging{},
			})
		default:
			t.Fatalf("unexpected Nike path %q", r.URL.Path)
		}
	}))
	defer nikeServer.Close()

	tokenStore := tokenStoreWithToken(t, "abc123")
	nikeClient := nrc.NewNikeClient(tokenStore, nikeServer.Client())
	nikeClient.BaseURL = nikeServer.URL
	apiServer := NewAPIServer(nikeClient, tokenStore)

	req := httptest.NewRequest(http.MethodGet, "/api/activities?limit=2&pages=1&before_date=2025-05-01&search_pages=3", nil)
	rec := httptest.NewRecorder()
	apiServer.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response ActivityListResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(response.Activities) != 2 {
		t.Fatalf("len(activities) = %d, want 2", len(response.Activities))
	}
	if response.Activities[0].ID != "apr-30" {
		t.Fatalf("first activity = %q, want latest prior date activity", response.Activities[0].ID)
	}
	if response.Search == nil || !response.Search.Matched || response.Search.PagesFetched != 2 {
		t.Fatalf("search = %+v, want matched after 2 pages", response.Search)
	}
	if len(paths) != 2 {
		t.Fatalf("fetched paths = %v, want two pages despite pages=1", paths)
	}
	if response.Search.StopReason != "result_limit" {
		t.Fatalf("StopReason = %q, want result_limit", response.Search.StopReason)
	}
}

func TestActivitiesPaginationFallsBackToOldestActivityID(t *testing.T) {
	paths := make([]string, 0)
	nikeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/plus/v3/activities/before_id/v3/*":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "new", StartEpochMs: mustEpochMs(t, "2026-03-02T10:00:00Z")},
					{ID: "oldest-on-first-page", StartEpochMs: mustEpochMs(t, "2026-03-01T10:00:00Z")},
				},
				Paging: nrc.Paging{},
			})
		case "/plus/v3/activities/before_id/v3/oldest-on-first-page":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "older", StartEpochMs: mustEpochMs(t, "2026-02-28T10:00:00Z")},
				},
				Paging: nrc.Paging{},
			})
		default:
			t.Fatalf("unexpected Nike path %q", r.URL.Path)
		}
	}))
	defer nikeServer.Close()

	tokenStore := tokenStoreWithToken(t, "abc123")
	nikeClient := nrc.NewNikeClient(tokenStore, nikeServer.Client())
	nikeClient.BaseURL = nikeServer.URL
	apiServer := NewAPIServer(nikeClient, tokenStore)

	req := httptest.NewRequest(http.MethodGet, "/api/activities?limit=2&pages=2", nil)
	rec := httptest.NewRecorder()
	apiServer.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response ActivityListResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(response.Activities) != 3 {
		t.Fatalf("len(activities) = %d, want 3", len(response.Activities))
	}
	if len(paths) != 2 || paths[1] != "/plus/v3/activities/before_id/v3/oldest-on-first-page" {
		t.Fatalf("paths = %v, want fallback cursor from oldest activity ID", paths)
	}
}

func TestExportAllBeforeDateReturnsZip(t *testing.T) {
	nikeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/plus/v3/activities/before_id/v3/*":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "newer", StartEpochMs: mustEpochMs(t, "2025-05-03T10:00:00Z")},
					{ID: "run-a", StartEpochMs: mustEpochMs(t, "2025-04-30T10:00:00Z")},
					{ID: "run-b", StartEpochMs: mustEpochMs(t, "2025-04-29T10:00:00Z")},
				},
				Paging: nrc.Paging{},
			})
		case "/plus/v3/activities/before_id/v3/run-b":
			_ = json.NewEncoder(w).Encode(nrc.Activities{Activities: []nrc.ActivityListItem{}, Paging: nrc.Paging{}})
		case "/sport/v3/me/activity/run-a":
			_ = json.NewEncoder(w).Encode(testExportActivity("run-a", "2025-04-30T10:00:00Z"))
		case "/sport/v3/me/activity/run-b":
			_ = json.NewEncoder(w).Encode(testExportActivity("run-b", "2025-04-29T10:00:00Z"))
		default:
			t.Fatalf("unexpected Nike path %q", r.URL.Path)
		}
	}))
	defer nikeServer.Close()

	tokenStore := tokenStoreWithToken(t, "abc123")
	nikeClient := nrc.NewNikeClient(tokenStore, nikeServer.Client())
	nikeClient.BaseURL = nikeServer.URL
	apiServer := NewAPIServer(nikeClient, tokenStore)

	req := httptest.NewRequest(http.MethodGet, "/api/export?before_date=2025-05-01&search_pages=3", nil)
	rec := httptest.NewRecorder()
	apiServer.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}

	reader, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	tcxCount := 0
	hasSummary := false
	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, ".tcx") {
			tcxCount++
		}
		if file.Name == "export-summary.txt" {
			hasSummary = true
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("summary open error = %v", err)
			}
			body, _ := io.ReadAll(rc)
			_ = rc.Close()
			if !strings.Contains(string(body), "Exported: 2") {
				t.Fatalf("summary = %q, want exported count", string(body))
			}
		}
	}
	if tcxCount != 2 {
		t.Fatalf("tcxCount = %d, want 2", tcxCount)
	}
	if !hasSummary {
		t.Fatal("zip does not contain export-summary.txt")
	}
}

func TestExportAllActivitiesReturnsZip(t *testing.T) {
	nikeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/plus/v3/activities/before_id/v3/*":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "run-a", StartEpochMs: mustEpochMs(t, "2025-07-03T10:00:00Z")},
					{ID: "run-b", StartEpochMs: mustEpochMs(t, "2025-07-02T10:00:00Z")},
				},
				Paging: nrc.Paging{BeforeID: "page-2"},
			})
		case "/plus/v3/activities/before_id/v3/page-2":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "run-b", StartEpochMs: mustEpochMs(t, "2025-07-02T10:00:00Z")},
					{ID: "run-c", StartEpochMs: mustEpochMs(t, "2025-07-01T10:00:00Z")},
				},
				Paging: nrc.Paging{BeforeID: "done"},
			})
		case "/plus/v3/activities/before_id/v3/done":
			_ = json.NewEncoder(w).Encode(nrc.Activities{Activities: []nrc.ActivityListItem{}, Paging: nrc.Paging{}})
		case "/sport/v3/me/activity/run-a":
			_ = json.NewEncoder(w).Encode(testExportActivity("run-a", "2025-07-03T10:00:00Z"))
		case "/sport/v3/me/activity/run-b":
			_ = json.NewEncoder(w).Encode(testExportActivity("run-b", "2025-07-02T10:00:00Z"))
		case "/sport/v3/me/activity/run-c":
			_ = json.NewEncoder(w).Encode(testExportActivity("run-c", "2025-07-01T10:00:00Z"))
		default:
			t.Fatalf("unexpected Nike path %q", r.URL.Path)
		}
	}))
	defer nikeServer.Close()

	tokenStore := tokenStoreWithToken(t, "abc123")
	nikeClient := nrc.NewNikeClient(tokenStore, nikeServer.Client())
	nikeClient.BaseURL = nikeServer.URL
	apiServer := NewAPIServer(nikeClient, tokenStore)

	req := httptest.NewRequest(http.MethodGet, "/api/export?search_pages=4", nil)
	rec := httptest.NewRecorder()
	apiServer.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "nrc-all-activities-") {
		t.Fatalf("Content-Disposition = %q", rec.Header().Get("Content-Disposition"))
	}

	reader, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	tcxCount := 0
	hasSummary := false
	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, ".tcx") {
			tcxCount++
		}
		if file.Name == "export-summary.txt" {
			hasSummary = true
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("summary open error = %v", err)
			}
			body, _ := io.ReadAll(rc)
			_ = rc.Close()
			summary := string(body)
			if !strings.Contains(summary, "All activities: true") ||
				!strings.Contains(summary, "Candidates: 3") ||
				!strings.Contains(summary, "Exported: 3") {
				t.Fatalf("summary = %q, want all activity export summary", summary)
			}
		}
	}
	if tcxCount != 3 {
		t.Fatalf("tcxCount = %d, want 3", tcxCount)
	}
	if !hasSummary {
		t.Fatal("zip does not contain export-summary.txt")
	}
}

func TestExportAllActivitiesJobReportsProgressAndDownloads(t *testing.T) {
	nikeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/plus/v3/activities/before_id/v3/*":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "run-a", StartEpochMs: mustEpochMs(t, "2025-07-03T10:00:00Z")},
					{ID: "run-b", StartEpochMs: mustEpochMs(t, "2025-07-02T10:00:00Z")},
				},
				Paging: nrc.Paging{BeforeID: "done"},
			})
		case "/plus/v3/activities/before_id/v3/done":
			_ = json.NewEncoder(w).Encode(nrc.Activities{Activities: []nrc.ActivityListItem{}, Paging: nrc.Paging{}})
		case "/sport/v3/me/activity/run-a":
			_ = json.NewEncoder(w).Encode(testExportActivity("run-a", "2025-07-03T10:00:00Z"))
		case "/sport/v3/me/activity/run-b":
			_ = json.NewEncoder(w).Encode(testExportActivity("run-b", "2025-07-02T10:00:00Z"))
		default:
			t.Fatalf("unexpected Nike path %q", r.URL.Path)
		}
	}))
	defer nikeServer.Close()

	tokenStore := tokenStoreWithToken(t, "abc123")
	nikeClient := nrc.NewNikeClient(tokenStore, nikeServer.Client())
	nikeClient.BaseURL = nikeServer.URL
	apiServer := NewAPIServer(nikeClient, tokenStore)
	handler := apiServer.Routes()

	startReq := httptest.NewRequest(http.MethodPost, "/api/export/jobs", strings.NewReader(`{"search_pages":4}`))
	startRec := httptest.NewRecorder()
	handler.ServeHTTP(startRec, startReq)
	if startRec.Code != http.StatusAccepted {
		t.Fatalf("start status = %d, body = %s", startRec.Code, startRec.Body.String())
	}
	var started ExportJobResponse
	if err := json.NewDecoder(startRec.Body).Decode(&started); err != nil {
		t.Fatalf("Decode(started) error = %v", err)
	}
	if started.ID == "" {
		t.Fatal("started.ID is empty")
	}

	var job ExportJobResponse
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		statusReq := httptest.NewRequest(http.MethodGet, "/api/export/jobs/"+started.ID, nil)
		statusRec := httptest.NewRecorder()
		handler.ServeHTTP(statusRec, statusReq)
		if statusRec.Code != http.StatusOK {
			t.Fatalf("status code = %d, body = %s", statusRec.Code, statusRec.Body.String())
		}
		if err := json.NewDecoder(statusRec.Body).Decode(&job); err != nil {
			t.Fatalf("Decode(job) error = %v", err)
		}
		if job.Status == exportJobStatusCompleted {
			break
		}
		if job.Status == exportJobStatusFailed {
			t.Fatalf("job failed: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status != exportJobStatusCompleted {
		t.Fatalf("job status = %+v, want completed", job)
	}
	if job.ActivitiesFound != 2 || job.ActivitiesExported != 2 || job.DownloadURL == "" {
		t.Fatalf("job = %+v, want progress and download URL", job)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, job.DownloadURL, nil)
	downloadRec := httptest.NewRecorder()
	handler.ServeHTTP(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download status = %d, body = %s", downloadRec.Code, downloadRec.Body.String())
	}

	reader, err := zip.NewReader(bytes.NewReader(downloadRec.Body.Bytes()), int64(downloadRec.Body.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	tcxCount := 0
	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, ".tcx") {
			tcxCount++
		}
	}
	if tcxCount != 2 {
		t.Fatalf("tcxCount = %d, want 2", tcxCount)
	}
}

func TestExportDateRangeReturnsZip(t *testing.T) {
	nikeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/plus/v3/activities/before_id/v3/*":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "newer", StartEpochMs: mustEpochMs(t, "2025-07-01T10:00:00Z")},
					{ID: "run-a", StartEpochMs: mustEpochMs(t, "2025-06-30T10:00:00Z")},
					{ID: "run-b", StartEpochMs: mustEpochMs(t, "2025-06-01T10:00:00Z")},
					{ID: "older", StartEpochMs: mustEpochMs(t, "2025-05-31T10:00:00Z")},
				},
				Paging: nrc.Paging{BeforeID: "older"},
			})
		case "/plus/v3/activities/before_id/v3/older":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "oldest", StartEpochMs: mustEpochMs(t, "2025-05-30T10:00:00Z")},
				},
				Paging: nrc.Paging{},
			})
		case "/sport/v3/me/activity/run-a":
			_ = json.NewEncoder(w).Encode(testExportActivity("run-a", "2025-06-30T10:00:00Z"))
		case "/sport/v3/me/activity/run-b":
			_ = json.NewEncoder(w).Encode(testExportActivity("run-b", "2025-06-01T10:00:00Z"))
		default:
			t.Fatalf("unexpected Nike path %q", r.URL.Path)
		}
	}))
	defer nikeServer.Close()

	tokenStore := tokenStoreWithToken(t, "abc123")
	nikeClient := nrc.NewNikeClient(tokenStore, nikeServer.Client())
	nikeClient.BaseURL = nikeServer.URL
	apiServer := NewAPIServer(nikeClient, tokenStore)

	req := httptest.NewRequest(http.MethodGet, "/api/export?from_date=2025-06-01&to_date=2025-06-30&search_pages=3", nil)
	rec := httptest.NewRecorder()
	apiServer.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "nrc-range-2025-06-01-to-2025-06-30.zip") {
		t.Fatalf("Content-Disposition = %q", rec.Header().Get("Content-Disposition"))
	}

	reader, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	tcxCount := 0
	hasSummary := false
	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, ".tcx") {
			tcxCount++
		}
		if file.Name == "export-summary.txt" {
			hasSummary = true
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("summary open error = %v", err)
			}
			body, _ := io.ReadAll(rc)
			_ = rc.Close()
			summary := string(body)
			if !strings.Contains(summary, "From date: 2025-06-01") ||
				!strings.Contains(summary, "To date: 2025-06-30") ||
				!strings.Contains(summary, "Exported: 2") {
				t.Fatalf("summary = %q, want range and exported count", summary)
			}
		}
	}
	if tcxCount != 2 {
		t.Fatalf("tcxCount = %d, want 2", tcxCount)
	}
	if !hasSummary {
		t.Fatal("zip does not contain export-summary.txt")
	}
}

func TestExportDateRangeContinuesPastOutOfOrderOlderActivity(t *testing.T) {
	nikeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/plus/v3/activities/before_id/v3/*":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "newer", StartEpochMs: mustEpochMs(t, "2025-07-01T10:00:00Z")},
					{ID: "stale-page-item", StartEpochMs: mustEpochMs(t, "2025-05-15T10:00:00Z")},
				},
				Paging: nrc.Paging{BeforeID: "page-2"},
			})
		case "/plus/v3/activities/before_id/v3/page-2":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "run-a", StartEpochMs: mustEpochMs(t, "2025-06-15T10:00:00Z")},
				},
				Paging: nrc.Paging{BeforeID: "page-3"},
			})
		case "/plus/v3/activities/before_id/v3/page-3":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "older", StartEpochMs: mustEpochMs(t, "2025-05-31T10:00:00Z")},
				},
				Paging: nrc.Paging{},
			})
		case "/sport/v3/me/activity/run-a":
			_ = json.NewEncoder(w).Encode(testExportActivity("run-a", "2025-06-15T10:00:00Z"))
		default:
			t.Fatalf("unexpected Nike path %q", r.URL.Path)
		}
	}))
	defer nikeServer.Close()

	tokenStore := tokenStoreWithToken(t, "abc123")
	nikeClient := nrc.NewNikeClient(tokenStore, nikeServer.Client())
	nikeClient.BaseURL = nikeServer.URL
	apiServer := NewAPIServer(nikeClient, tokenStore)

	req := httptest.NewRequest(http.MethodGet, "/api/export?from_date=2025-06-01&to_date=2025-06-30&search_pages=4", nil)
	rec := httptest.NewRecorder()
	apiServer.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reader, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	tcxCount := 0
	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, ".tcx") {
			tcxCount++
		}
	}
	if tcxCount != 1 {
		t.Fatalf("tcxCount = %d, want 1", tcxCount)
	}
}

func TestExportDateRangeRFC3339ToDateIsExact(t *testing.T) {
	nikeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/plus/v3/activities/before_id/v3/*":
			_ = json.NewEncoder(w).Encode(nrc.Activities{
				Activities: []nrc.ActivityListItem{
					{ID: "after-range", StartEpochMs: mustEpochMs(t, "2025-06-30T13:00:00Z")},
					{ID: "run-a", StartEpochMs: mustEpochMs(t, "2025-06-30T10:00:00Z")},
					{ID: "older", StartEpochMs: mustEpochMs(t, "2025-06-29T23:00:00Z")},
				},
				Paging: nrc.Paging{},
			})
		case "/plus/v3/activities/before_id/v3/older":
			_ = json.NewEncoder(w).Encode(nrc.Activities{Activities: []nrc.ActivityListItem{}, Paging: nrc.Paging{}})
		case "/sport/v3/me/activity/run-a":
			_ = json.NewEncoder(w).Encode(testExportActivity("run-a", "2025-06-30T10:00:00Z"))
		default:
			t.Fatalf("unexpected Nike path %q", r.URL.Path)
		}
	}))
	defer nikeServer.Close()

	tokenStore := tokenStoreWithToken(t, "abc123")
	nikeClient := nrc.NewNikeClient(tokenStore, nikeServer.Client())
	nikeClient.BaseURL = nikeServer.URL
	apiServer := NewAPIServer(nikeClient, tokenStore)

	req := httptest.NewRequest(http.MethodGet, "/api/export?from_date=2025-06-30T00:00:00Z&to_date=2025-06-30T12:00:00Z&search_pages=3", nil)
	rec := httptest.NewRecorder()
	apiServer.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reader, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	tcxCount := 0
	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, ".tcx") {
			tcxCount++
		}
	}
	if tcxCount != 1 {
		t.Fatalf("tcxCount = %d, want 1", tcxCount)
	}
}

func TestBeforeDateParserAcceptsPolishDateFormat(t *testing.T) {
	parsed, err := parseBeforeDate("22.11.2025")
	if err != nil {
		t.Fatalf("parseBeforeDate() error = %v", err)
	}
	if parsed.Year() != 2025 || parsed.Month() != time.November || parsed.Day() != 22 {
		t.Fatalf("parsed date = %s", parsed)
	}
}

func testExportActivity(id string, start string) nrc.Activity {
	startTime, _ := time.Parse(time.RFC3339, start)
	return nrc.Activity{
		ID:               id,
		Type:             "run",
		StartEpochMs:     startTime.UnixMilli(),
		EndEpochMs:       startTime.Add(time.Hour).UnixMilli(),
		ActiveDurationMs: int(time.Hour / time.Millisecond),
		Summaries: []nrc.Summary{
			{Metric: "distance", Summary: "total", Value: 10},
			{Metric: "heart_rate", Summary: "mean", Value: 140},
			{Metric: "calories", Summary: "total", Value: 600},
		},
	}
}

func tokenStoreWithToken(t *testing.T, token string) *nrc.TokenStore {
	t.Helper()
	store, err := nrc.NewTokenStore(filepath.Join(t.TempDir(), "bearer.token"))
	if err != nil {
		t.Fatalf("NewTokenStore() error = %v", err)
	}
	if err := store.Set(token); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	return store
}

func mustEpochMs(t *testing.T, value string) int64 {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time.Parse(%q) error = %v", value, err)
	}
	return parsed.UnixMilli()
}
