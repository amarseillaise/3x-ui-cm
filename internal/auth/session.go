// Package auth implements cookie sessions created by magic links and a small
// per-IP rate limiter for the link endpoints.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/store"
)

const (
	// CookieName is the session cookie.
	CookieName = "sid"
	// SessionTTL is the idle lifetime: a session used at least this often never expires.
	SessionTTL = 90 * 24 * time.Hour
	touchEvery = time.Hour
)

// ErrNoSession is returned when the request carries no valid session.
var ErrNoSession = errors.New("auth: no valid session")

// Manager creates, resolves and destroys sessions.
type Manager struct {
	store  *store.Store
	secret []byte
	secure bool
	now    func() time.Time
}

// NewManager builds a manager. secure controls the cookie's Secure flag.
func NewManager(st *store.Store, secret string, secure bool) *Manager {
	return &Manager{store: st, secret: []byte(secret), secure: secure, now: time.Now}
}

// Create stores a new session and sets the cookie.
func (m *Manager) Create(ctx context.Context, w http.ResponseWriter, role, subID, userAgent string) (*store.Session, error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	now := m.now().UnixMilli()
	sess := store.Session{ID: id, Role: role, SubID: subID, CreatedAt: now, LastSeenAt: now, UserAgent: truncate(userAgent, 200)}
	if err := m.store.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	http.SetCookie(w, m.cookie(m.sign(id), int(SessionTTL.Seconds())))
	return &sess, nil
}

// FromRequest resolves the session cookie. It refreshes last_seen (and the
// cookie) at most once an hour and drops sessions idle longer than SessionTTL.
func (m *Manager) FromRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) (*store.Session, error) {
	id, ok := m.idFromRequest(r)
	if !ok {
		return nil, ErrNoSession
	}
	sess, err := m.store.GetSession(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	now := m.now()
	idle := now.Sub(time.UnixMilli(sess.LastSeenAt))
	if idle > SessionTTL {
		_ = m.store.DeleteSession(ctx, id)
		return nil, ErrNoSession
	}
	if idle > touchEvery {
		if err := m.store.TouchSession(ctx, id, now.UnixMilli()); err == nil {
			sess.LastSeenAt = now.UnixMilli()
			http.SetCookie(w, m.cookie(m.sign(id), int(SessionTTL.Seconds())))
		}
	}
	return sess, nil
}

// Destroy deletes the session (if any) and clears the cookie.
func (m *Manager) Destroy(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, m.cookie("", -1))
	id, ok := m.idFromRequest(r)
	if !ok {
		return nil
	}
	return m.store.DeleteSession(ctx, id)
}

// CleanupLoop deletes idle sessions once a day until ctx is done.
func (m *Manager) CleanupLoop(ctx context.Context, log *slog.Logger) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		n, err := m.store.DeleteSessionsIdleBefore(ctx, m.now().Add(-SessionTTL).UnixMilli())
		if err != nil {
			log.Warn("session cleanup failed", "err", err)
		} else if n > 0 {
			log.Info("idle sessions removed", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) idFromRequest(r *http.Request) (string, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return m.verify(c.Value)
}

func (m *Manager) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

// sign returns "<id>.<hmac>" so tampered cookies are rejected before any DB lookup.
func (m *Manager) sign(id string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(id))
	return id + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (m *Manager) verify(value string) (string, bool) {
	i := strings.LastIndexByte(value, '.')
	if i <= 0 {
		return "", false
	}
	id, sig := value[:i], value[i+1:]
	want, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(id))
	if !hmac.Equal(mac.Sum(nil), want) {
		return "", false
	}
	return id, true
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
