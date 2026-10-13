package nrc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

const BaseURL = "https://api.nike.com"

// ErrMissingToken is returned when a Nike bearer token is required but not configured.
var ErrMissingToken = errors.New("nike bearer token is not configured")

// TokenProvider is satisfied by TokenStore and by any value that can supply a bearer token.
type TokenProvider interface {
	Token() (string, error)
}

type TokenStore struct {
	path  string
	mu    sync.RWMutex
	token string
}

func NewTokenStore(path string) (*TokenStore, error) {
	store := &TokenStore{path: path}
	bytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, err
	}
	store.token = sanitizeBearerToken(string(bytes))
	return store, nil
}

func (s *TokenStore) Set(token string) error {
	token = sanitizeBearerToken(token)
	if token == "" {
		return ErrMissingToken
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.WriteFile(s.path, []byte(token+"\n"), 0600); err != nil {
		return err
	}
	s.token = token
	return nil
}

func (s *TokenStore) Token() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.token == "" {
		return "", ErrMissingToken
	}
	return s.token, nil
}

func (s *TokenStore) HasToken() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token != ""
}

func sanitizeBearerToken(token string) string {
	token = strings.TrimSpace(token)
	fields := strings.Fields(token)
	if len(fields) > 0 && strings.EqualFold(fields[0], "Bearer") {
		if len(fields) == 1 {
			return ""
		}
		token = strings.Join(fields[1:], " ")
	}
	return strings.TrimSpace(token)
}

type NikeClient struct {
	httpClient    *http.Client
	tokenProvider TokenProvider
	BaseURL       string
}

func NewNikeClient(tokenStore *TokenStore, httpClient *http.Client) *NikeClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &NikeClient{
		httpClient:    httpClient,
		tokenProvider: tokenStore,
		BaseURL:       BaseURL,
	}
}

// NewNikeClientWithToken creates a NikeClient using a bearer token string directly.
func NewNikeClientWithToken(token string, httpClient *http.Client) *NikeClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &NikeClient{
		httpClient:    httpClient,
		tokenProvider: staticToken(token),
		BaseURL:       BaseURL,
	}
}

// SanitizeBearerToken strips the "Bearer " prefix and trims whitespace.
func SanitizeBearerToken(token string) string {
	return sanitizeBearerToken(token)
}

// staticToken is a TokenProvider backed by a fixed string.
type staticToken string

func (t staticToken) Token() (string, error) {
	if t == "" {
		return "", ErrMissingToken
	}
	return string(t), nil
}

func (c *NikeClient) FetchActivities(ctx context.Context, beforeID string, limit int) (*Activities, error) {
	if beforeID == "" {
		beforeID = "*"
	}
	if limit <= 0 || limit > 10 {
		limit = 10
	}

	escapedBeforeID := beforeID
	if beforeID != "*" {
		escapedBeforeID = url.PathEscape(beforeID)
	}
	endpoint := fmt.Sprintf("%s/plus/v3/activities/before_id/v3/%s?limit=%d&types=run%%2Cjogging&include_deleted=false", c.BaseURL, escapedBeforeID, limit)

	var activities Activities
	if err := c.getJSON(ctx, endpoint, &activities); err != nil {
		return nil, err
	}
	return &activities, nil
}

func (c *NikeClient) FetchActivity(ctx context.Context, id string) (*Activity, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("activity id is required")
	}

	endpoint := fmt.Sprintf("%s/sport/v3/me/activity/%s?metrics=ALL", c.BaseURL, url.PathEscape(id))

	var activity Activity
	if err := c.getJSON(ctx, endpoint, &activity); err != nil {
		return nil, err
	}
	return &activity, nil
}

func (c *NikeClient) getJSON(ctx context.Context, endpoint string, target interface{}) error {
	token, err := c.tokenProvider.Token()
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &NikeError{StatusCode: resp.StatusCode, Status: resp.Status}
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

type NikeError struct {
	StatusCode int
	Status     string
}

func (e *NikeError) Error() string {
	return fmt.Sprintf("nike api returned %s", e.Status)
}
