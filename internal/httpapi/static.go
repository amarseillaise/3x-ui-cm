package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func isStatic(p string) bool {
	return strings.HasPrefix(p, "/assets/") || strings.HasPrefix(p, "/icons/") || p == "/favicon.svg"
}

// maskPath hides magic-link secrets from logs.
func maskPath(p string) string {
	for _, prefix := range []string{"/s/", "/a/"} {
		if strings.HasPrefix(p, prefix) {
			return prefix + "***"
		}
	}
	return p
}

// spaHandler serves the built frontend with an index.html fallback for client-side routes.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Метод не поддерживается")
			return
		}
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if info, err := fs.Stat(fsys, p); err == nil && !info.IsDir() {
				switch {
				case strings.HasPrefix(p, "assets/"):
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				case p == "sw.js" || p == "manifest.webmanifest" || strings.HasPrefix(p, "workbox-"):
					w.Header().Set("Cache-Control", "no-cache")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, "frontend is not built: run `make web` and rebuild", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(index)
		}
	})
}
