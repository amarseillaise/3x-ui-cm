package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/store"
)

func newManager(t *testing.T) *Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewManager(st, "0123456789abcdef0123456789abcdef", true)
}

func requestWithCookies(rec *httptest.ResponseRecorder) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	return r
}

func TestSessionRoundTrip(t *testing.T) {
	m := newManager(t)
	ctx := context.Background()
	rec := httptest.NewRecorder()
	created, err := m.Create(ctx, rec, store.RoleClient, "sub1", "UA")
	if err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != CookieName || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie attrs: %+v", cookies)
	}
	got, err := m.FromRequest(ctx, httptest.NewRecorder(), requestWithCookies(rec))
	if err != nil || got.ID != created.ID || got.SubID != "sub1" {
		t.Fatalf("resolve: %+v %v", got, err)
	}

	// Tampered signature is rejected without touching the store.
	bad := httptest.NewRequest(http.MethodGet, "/", nil)
	bad.AddCookie(&http.Cookie{Name: CookieName, Value: created.ID + ".AAAA"})
	if _, err := m.FromRequest(ctx, httptest.NewRecorder(), bad); !errors.Is(err, ErrNoSession) {
		t.Errorf("tampered cookie: %v", err)
	}
	none := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := m.FromRequest(ctx, httptest.NewRecorder(), none); !errors.Is(err, ErrNoSession) {
		t.Errorf("no cookie: %v", err)
	}

	// Destroy clears the cookie and the row.
	out := httptest.NewRecorder()
	if err := m.Destroy(ctx, out, requestWithCookies(rec)); err != nil {
		t.Fatal(err)
	}
	if c := out.Result().Cookies(); len(c) != 1 || c[0].MaxAge != -1 {
		t.Errorf("clear cookie: %+v", c)
	}
	if _, err := m.FromRequest(ctx, httptest.NewRecorder(), requestWithCookies(rec)); !errors.Is(err, ErrNoSession) {
		t.Errorf("after destroy: %v", err)
	}
}

func TestSessionIdleExpiryAndTouch(t *testing.T) {
	m := newManager(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return base }
	rec := httptest.NewRecorder()
	if _, err := m.Create(ctx, rec, store.RoleAdmin, "", ""); err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return base.Add(2 * time.Hour) }
	out := httptest.NewRecorder()
	sess, err := m.FromRequest(ctx, out, requestWithCookies(rec))
	if err != nil || sess.LastSeenAt != base.Add(2*time.Hour).UnixMilli() {
		t.Fatalf("touch: %+v %v", sess, err)
	}
	if len(out.Result().Cookies()) != 1 {
		t.Error("cookie must be re-issued on touch")
	}
	m.now = func() time.Time { return base.Add(2*time.Hour + SessionTTL + time.Minute) }
	if _, err := m.FromRequest(ctx, httptest.NewRecorder(), requestWithCookies(rec)); !errors.Is(err, ErrNoSession) {
		t.Errorf("idle session must expire: %v", err)
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(60, 3)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if !l.Allow("ip") {
			t.Fatalf("call %d must be allowed", i)
		}
	}
	if l.Allow("ip") {
		t.Fatal("burst exceeded must be denied")
	}
	if !l.Allow("other") {
		t.Fatal("other key must have its own bucket")
	}
	now = now.Add(time.Second)
	if !l.Allow("ip") {
		t.Fatal("one token must refill after 1s at 60/min")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.9")
	if ip := ClientIP(r, true); ip != "203.0.113.5" {
		t.Errorf("trusted: %s", ip)
	}
	if ip := ClientIP(r, false); ip != "10.0.0.1" {
		t.Errorf("untrusted: %s", ip)
	}
}
