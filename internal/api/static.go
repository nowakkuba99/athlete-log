package api

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func spaFileServer(staticDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(staticDir))
	indexPath := filepath.Join(staticDir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeMethodNotAllowed(w)
			return
		}

		requestPath := path.Clean("/" + r.URL.Path)
		relativePath := strings.TrimPrefix(requestPath, "/")
		if relativePath == "" {
			http.ServeFile(w, r, indexPath)
			return
		}

		fullPath := filepath.Join(staticDir, filepath.FromSlash(relativePath))
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, indexPath)
	})
}
