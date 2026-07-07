package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPAFileServerServesAssetsAndFallsBackToIndex(t *testing.T) {
	staticDir := t.TempDir()
	assetsDir := filepath.Join(staticDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<div id=\"root\"></div>"), 0644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "app.js"), []byte("console.log('ok')"), 0644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	handler := spaFileServer(staticDir)

	assetReq := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	assetRec := httptest.NewRecorder()
	handler.ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Fatalf("asset status = %d", assetRec.Code)
	}
	if !strings.Contains(assetRec.Body.String(), "console.log") {
		t.Fatalf("asset response = %q", assetRec.Body.String())
	}

	spaReq := httptest.NewRequest(http.MethodGet, "/activities/run-123", nil)
	spaRec := httptest.NewRecorder()
	handler.ServeHTTP(spaRec, spaReq)
	if spaRec.Code != http.StatusOK {
		t.Fatalf("fallback status = %d", spaRec.Code)
	}
	if !strings.Contains(spaRec.Body.String(), `id="root"`) {
		t.Fatalf("fallback response = %q", spaRec.Body.String())
	}
}

func TestRoutesWithStaticDoNotFallbackForUnknownAPIRoutes(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<div id=\"root\"></div>"), 0644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	server := NewAPIServer(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/not-found", nil)
	rec := httptest.NewRecorder()
	server.RoutesWithStatic(staticDir).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if strings.Contains(rec.Body.String(), `id="root"`) {
		t.Fatalf("unknown API route served frontend index: %q", rec.Body.String())
	}
}
