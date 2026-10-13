package nrc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestTokenStoreSanitizesAndPersistsBearerToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bearer.token")
	store, err := NewTokenStore(path)
	if err != nil {
		t.Fatalf("NewTokenStore() error = %v", err)
	}
	if store.HasToken() {
		t.Fatal("new token store unexpectedly has a token")
	}
	if err := store.Set("Bearer abc123\n"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	token, err := store.Token()
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "abc123" {
		t.Fatalf("Token() = %q, want %q", token, "abc123")
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(bytes) != "abc123\n" {
		t.Fatalf("persisted token = %q, want sanitized token", string(bytes))
	}
}

func TestTokenStoreRejectsEmptyToken(t *testing.T) {
	store, err := NewTokenStore(filepath.Join(t.TempDir(), "bearer.token"))
	if err != nil {
		t.Fatalf("NewTokenStore() error = %v", err)
	}
	if err := store.Set("Bearer  "); !errors.Is(err, ErrMissingToken) {
		t.Fatalf("Set(empty) error = %v, want ErrMissingToken", err)
	}
}

func TestNikeClientFetchActivitiesRequest(t *testing.T) {
	tokenStore := testTokenStore(t, "abc123")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer abc123" {
			t.Fatalf("Authorization header = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/plus/v3/activities/before_id/v3/*" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("limit") != "10" {
			t.Fatalf("limit = %q", query.Get("limit"))
		}
		if query.Get("types") != "run,jogging" {
			t.Fatalf("types = %q", query.Get("types"))
		}
		if query.Get("include_deleted") != "false" {
			t.Fatalf("include_deleted = %q", query.Get("include_deleted"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"activities":[{"id":"run-1","type":"run"}],"paging":{"before_id":"next"}}`))
	}))
	defer server.Close()

	client := NewNikeClient(tokenStore, server.Client())
	client.BaseURL = server.URL

	activities, err := client.FetchActivities(context.Background(), "*", 15)
	if err != nil {
		t.Fatalf("FetchActivities() error = %v", err)
	}
	if len(activities.Activities) != 1 || activities.Activities[0].ID != "run-1" {
		t.Fatalf("activities = %+v", activities.Activities)
	}
	if activities.Paging.BeforeID != "next" {
		t.Fatalf("BeforeID = %q", activities.Paging.BeforeID)
	}
}

func TestNikeClientFetchActivityRequestAndError(t *testing.T) {
	tokenStore := testTokenStore(t, "abc123")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sport/v3/me/activity/fail" {
			http.Error(w, "nope", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/sport/v3/me/activity/run-1" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("metrics") != "ALL" {
			t.Fatalf("metrics = %q", r.URL.Query().Get("metrics"))
		}
		if r.Header.Get("Authorization") != "Bearer abc123" {
			t.Fatalf("Authorization header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"run-1","type":"run"}`))
	}))
	defer server.Close()

	client := NewNikeClient(tokenStore, server.Client())
	client.BaseURL = server.URL

	activity, err := client.FetchActivity(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("FetchActivity() error = %v", err)
	}
	if activity.ID != "run-1" {
		t.Fatalf("activity ID = %q", activity.ID)
	}

	_, err = client.FetchActivity(context.Background(), "fail")
	var nikeErr *NikeError
	if !errors.As(err, &nikeErr) || nikeErr.StatusCode != http.StatusForbidden {
		t.Fatalf("FetchActivity(fail) error = %v, want NikeError 403", err)
	}
}

func testTokenStore(t *testing.T, token string) *TokenStore {
	t.Helper()
	store, err := NewTokenStore(filepath.Join(t.TempDir(), "bearer.token"))
	if err != nil {
		t.Fatalf("NewTokenStore() error = %v", err)
	}
	if err := store.Set(token); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	return store
}
