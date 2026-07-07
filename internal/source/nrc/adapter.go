package nrcsource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"athlete-log/internal/domain"
	"athlete-log/internal/nrc"
	"athlete-log/internal/source"
)

// Adapter implements source.PullAdapter for Nike Run Club.
type Adapter struct {
	httpClient *http.Client
	dataDir    string
	// BaseURL overrides the NRC API base URL — set in tests to point at a fake server.
	BaseURL string
}

func New(httpClient *http.Client, dataDir string) *Adapter {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * 1e9} // 30s
	}
	return &Adapter{httpClient: httpClient, dataDir: dataDir}
}

func (a *Adapter) Source() domain.SourceType {
	return domain.SourceNRC
}

// Sync pages through all NRC activities, saves each raw payload as a blob,
// maps to canonical form, and calls upsert for each one.
func (a *Adapter) Sync(ctx context.Context, conn domain.SourceConnection, upsert func(domain.SourceActivity) error) error {
	client := nrc.NewNikeClientWithToken(conn.SecretKey, a.httpClient)
	if a.BaseURL != "" {
		client.BaseURL = a.BaseURL
	}

	beforeID := "*"
	seen := make(map[string]struct{})

	for {
		page, err := client.FetchActivities(ctx, beforeID, 10)
		if err != nil {
			return fmt.Errorf("fetch NRC activities: %w", err)
		}

		for _, item := range page.Activities {
			if item.ID == "" {
				continue
			}
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}

			detail, err := client.FetchActivity(ctx, item.ID)
			if err != nil {
				// Skip this activity; the next sync will retry it.
				continue
			}

			raw, _ := json.Marshal(detail)
			blobKey, sha256hex, _ := source.WriteBlob(a.dataDir, raw)

			sourceActivity, err := nrc.MapNRCActivity(detail)
			if err != nil {
				continue
			}

			sourceActivity.Ref.SourceConnectionID = conn.ID
			if blobKey != "" {
				sourceActivity.Ref.RawBlobKey = blobKey
				sourceActivity.Ref.RawSHA256 = sha256hex
			}

			if err := upsert(*sourceActivity); err != nil {
				return fmt.Errorf("upsert NRC activity %s: %w", item.ID, err)
			}
		}

		nextID := nextCursor(page, beforeID)
		if nextID == "" || len(page.Activities) == 0 {
			break
		}
		beforeID = nextID
	}

	return nil
}

func nextCursor(page *nrc.Activities, currentBeforeID string) string {
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
