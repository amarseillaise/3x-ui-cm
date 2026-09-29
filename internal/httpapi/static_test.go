package httpapi

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/amarseillaise/3x-ui-cm/internal/webdist"
)

const sampleIndex = `<!doctype html>
<html lang="ru">
  <head>
    <meta name="apple-mobile-web-app-title" content="Подписка" />
    <title>Кабинет подписки</title>
  <link rel="manifest" href="/manifest.webmanifest"></head>
  <body><div id="root"></div></body>
</html>`

const sampleManifest = `{"name":"Кабинет подписки","short_name":"Подписка","start_url":"/","scope":"/",` +
	`"display":"standalone","icons":[{"src":"/icons/icon-192.png","sizes":"192x192","type":"image/png"}]}`

func TestAdminIndexPointsAtAdminManifest(t *testing.T) {
	out, changed := adminIndex([]byte(sampleIndex))
	if !changed {
		t.Fatal("expected the manifest link to be rewritten")
	}
	html := string(out)
	for _, want := range []string{
		`href="/admin.webmanifest"`,
		`<meta name="apple-mobile-web-app-title" content="Админ" />`,
		`<title>Админка кабинета</title>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "/manifest.webmanifest") {
		t.Error("client manifest link survived")
	}
}

// The rewrite depends on how vite-plugin-pwa writes the link. If a plugin
// update changes that, the admin icon would silently open the client screen
// again, so check the real build rather than only a hand-written sample.
func TestAdminIndexMatchesTheRealBuild(t *testing.T) {
	dist, err := webdist.FS()
	if err != nil {
		t.Fatal(err)
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		t.Skip("frontend not built; run make web")
	}
	if _, changed := adminIndex(index); !changed {
		t.Fatalf("the built index.html no longer links %s the expected way", clientManifestPath)
	}
	if _, err := fs.Stat(dist, "manifest.webmanifest"); err != nil {
		t.Fatal("the build has no manifest.webmanifest to derive the admin one from")
	}
}

func TestAdminManifestIsASeparateApp(t *testing.T) {
	body, err := adminManifest([]byte(sampleManifest))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"id": "/admin", "start_url": "/admin", "scope": "/", "short_name": "Админ",
	} {
		if m[key] != want {
			t.Errorf("%s = %v, want %q", key, m[key], want)
		}
	}
	if icons, _ := m["icons"].([]any); len(icons) != 1 {
		t.Errorf("icons must be carried over from the client manifest, got %v", m["icons"])
	}
	if !strings.Contains(string(body), "Админ") {
		t.Error("Cyrillic must be written as UTF-8, not escaped")
	}
}

func TestSPARoutesPickTheirManifest(t *testing.T) {
	h := spaHandler(fstest.MapFS{
		"index.html":           {Data: []byte(sampleIndex)},
		"manifest.webmanifest": {Data: []byte(sampleManifest)},
	})
	get := func(p string) (int, string, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		body, _ := io.ReadAll(rec.Body)
		return rec.Code, rec.Header().Get("Content-Type"), string(body)
	}

	cases := []struct {
		path      string
		wantAdmin bool
	}{
		{"/", false},
		{"/renew", false},
		{"/admin", true},
		{"/admin/", true},
		{"/admin/history", true},
		{"/administrator", false}, // a prefix match would be wrong
	}
	for _, c := range cases {
		code, _, body := get(c.path)
		if code != http.StatusOK {
			t.Errorf("%s: status %d", c.path, code)
			continue
		}
		if got := strings.Contains(body, adminManifestPath); got != c.wantAdmin {
			t.Errorf("%s: admin manifest linked = %v, want %v", c.path, got, c.wantAdmin)
		}
	}

	code, ctype, body := get(adminManifestPath)
	if code != http.StatusOK || ctype != "application/manifest+json" {
		t.Fatalf("admin manifest: %d %q", code, ctype)
	}
	if !strings.Contains(body, `"start_url":"/admin"`) {
		t.Errorf("admin manifest start_url wrong: %s", body)
	}
}

// Without a built client manifest there is nothing to derive from.
func TestAdminManifestMissingBaseIs404(t *testing.T) {
	h := spaHandler(fstest.MapFS{"index.html": {Data: []byte(sampleIndex)}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, adminManifestPath, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}
