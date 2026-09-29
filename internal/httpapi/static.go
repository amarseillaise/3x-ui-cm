package httpapi

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"regexp"
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

// The admin screen is installed to the home screen as an app of its own. iOS
// takes the start address from the manifest the page links to, not from the
// page itself, so with one shared manifest the admin icon opened "/", the
// client screen, and the admin session got "Недостаточно прав" there.
const (
	clientManifestPath = "/manifest.webmanifest"
	adminManifestPath  = "/admin.webmanifest"
	adminAppTitle      = "Админ"
	adminPageTitle     = "Админка кабинета"
)

var (
	manifestLinkRe = regexp.MustCompile(`href="` + regexp.QuoteMeta(clientManifestPath) + `"`)
	appleTitleRe   = regexp.MustCompile(`(<meta name="apple-mobile-web-app-title" content=")[^"]*(")`)
	pageTitleRe    = regexp.MustCompile(`<title>[^<]*</title>`)
)

// isAdminRoute reports whether a client-side route belongs to the admin app.
func isAdminRoute(urlPath string) bool {
	return urlPath == "/admin" || strings.HasPrefix(urlPath, "/admin/")
}

// adminIndex points the page at the admin manifest and renames it. changed is
// false when the build no longer links the manifest the way we expect; a test
// checks the real build so that never reaches production unnoticed.
func adminIndex(index []byte) (out []byte, changed bool) {
	if !manifestLinkRe.Match(index) {
		return index, false
	}
	out = manifestLinkRe.ReplaceAll(index, []byte(`href="`+adminManifestPath+`"`))
	out = appleTitleRe.ReplaceAll(out, []byte("${1}"+adminAppTitle+"${2}"))
	out = pageTitleRe.ReplaceAll(out, []byte("<title>"+adminPageTitle+"</title>"))
	return out, true
}

// adminManifest derives the admin app's manifest from the client one, so icons
// and colours stay in step with the Vite config. A distinct id makes it a
// separate app: both can sit on the same home screen.
func adminManifest(client []byte) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(client, &m); err != nil {
		return nil, err
	}
	m["id"] = "/admin"
	m["start_url"] = "/admin"
	m["scope"] = "/"
	m["name"] = adminPageTitle
	m["short_name"] = adminAppTitle
	m["description"] = "Заявки на продление и рассылки"
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
		if "/"+p == adminManifestPath {
			serveAdminManifest(w, r, fsys)
			return
		}
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
		if isAdminRoute(r.URL.Path) {
			index, _ = adminIndex(index)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(index)
		}
	})
}

func serveAdminManifest(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	client, err := fs.ReadFile(fsys, strings.TrimPrefix(clientManifestPath, "/"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	body, err := adminManifest(client)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось собрать манифест")
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(body)
	}
}
