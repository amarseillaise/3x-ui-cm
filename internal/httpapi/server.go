// Package httpapi wires HTTP routes: client and admin JSON API, magic links,
// health check and the embedded frontend.
package httpapi

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/auth"
	"github.com/amarseillaise/3x-ui-cm/internal/config"
	"github.com/amarseillaise/3x-ui-cm/internal/push"
	"github.com/amarseillaise/3x-ui-cm/internal/renewal"
	"github.com/amarseillaise/3x-ui-cm/internal/store"
	"github.com/amarseillaise/3x-ui-cm/internal/xui"
)

// Deps are the collaborators the server needs.
type Deps struct {
	Env    config.Env
	App    *config.AppConfig
	Store  *store.Store
	XUI    *xui.Client
	Push   *push.Sender // nil = disabled
	Log    *slog.Logger
	Static fs.FS
}

// Server holds routes and shared state.
type Server struct {
	env      config.Env
	app      *config.AppConfig
	store    *store.Store
	xui      *xui.Client
	push     *push.Sender
	renewal  *renewal.Service
	log      *slog.Logger
	mux      *http.ServeMux
	sessions *auth.Manager
	links    *auth.Limiter
	now      func() time.Time

	// healthMu guards the cached panel health only. The probe itself runs
	// outside the lock: holding a mutex across a panel call would make every
	// request wait for the slowest one.
	healthMu    sync.Mutex
	healthAt    time.Time
	healthOK    bool
	healthProbe chan struct{} // non-nil while a probe is in flight

	// subMu guards the cached subscription base URL, with the same rule.
	subMu     sync.Mutex
	subBase   string
	subBaseAt time.Time
	subLoad   chan struct{} // non-nil while panel settings are being read
}

// New builds the server and registers routes.
func New(d Deps) *Server {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Push == nil {
		d.Push = push.New(d.Store, "", "", "", d.Log)
	}
	s := &Server{
		env:      d.Env,
		app:      d.App,
		store:    d.Store,
		xui:      d.XUI,
		push:     d.Push,
		renewal:  renewal.New(d.Store, d.XUI, d.Push, d.App, d.Log),
		log:      d.Log,
		mux:      http.NewServeMux(),
		sessions: auth.NewManager(d.Store, d.Env.SessionSecret, strings.HasPrefix(d.Env.AppBaseURL, "https://")),
		links:    auth.NewLimiter(10, 10),
		now:      time.Now,
	}
	s.routes(d.Static)
	return s
}

func (s *Server) routes(static fs.FS) {
	m := s.mux
	m.HandleFunc("GET /healthz", s.handleHealthz)

	// Magic links.
	m.HandleFunc("GET /s/{subId}", s.handleClientLink)
	m.HandleFunc("GET /a/{secret}", s.handleAdminLink)

	// Client API.
	client := s.requireRole(store.RoleClient)
	m.Handle("GET /api/me", client(http.HandlerFunc(s.handleMe)))
	m.Handle("GET /api/plans", client(http.HandlerFunc(s.handlePlans)))
	m.Handle("POST /api/renew", client(http.HandlerFunc(s.handleRenew)))
	m.Handle("GET /api/renewals", client(http.HandlerFunc(s.handleMyRenewals)))
	m.Handle("POST /api/logout", s.requireAnyRole()(http.HandlerFunc(s.handleLogout)))
	m.Handle("POST /api/push/subscribe", s.requireAnyRole()(http.HandlerFunc(s.handlePushSubscribe)))
	m.Handle("DELETE /api/push/subscribe", s.requireAnyRole()(http.HandlerFunc(s.handlePushUnsubscribe)))

	// Admin API (admin session cookie or Bearer ADMIN_TOKEN).
	admin := s.requireRole(store.RoleAdmin)
	m.Handle("GET /api/admin/link", admin(http.HandlerFunc(s.handleAdminMakeLink)))
	m.Handle("GET /api/admin/stats", admin(http.HandlerFunc(s.handleAdminStats)))
	m.Handle("POST /api/admin/push", admin(http.HandlerFunc(s.handleAdminPush)))
	m.Handle("GET /api/admin/renewals", admin(http.HandlerFunc(s.handleAdminRenewals)))
	m.Handle("POST /api/admin/renewals/{id}/{action}", admin(http.HandlerFunc(s.handleAdminResolve)))

	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Неизвестный маршрут")
	})
	m.Handle("/", spaHandler(static))
}

// Handler returns the root handler with middleware applied.
func (s *Server) Handler() http.Handler {
	return chain(s.mux, s.recoverer, s.logging, securityHeaders)
}

// StartBackground launches maintenance goroutines that stop with ctx.
func (s *Server) StartBackground(ctx context.Context) {
	go s.sessions.CleanupLoop(ctx, s.log)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	panel := "down"
	if s.panelHealthy(r.Context()) {
		panel = "ok"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "panel": panel})
}

// panelHealthy probes the panel at most every 30 seconds. Only one probe runs
// at a time; concurrent callers get the last known answer instead of queueing
// behind a network call.
func (s *Server) panelHealthy(ctx context.Context) bool {
	s.healthMu.Lock()
	if time.Since(s.healthAt) < 30*time.Second || s.healthProbe != nil {
		ok := s.healthOK
		s.healthMu.Unlock()
		return ok
	}
	done := make(chan struct{})
	s.healthProbe = done
	s.healthMu.Unlock()

	// Detached from the request: a client that walks away must not cancel the
	// probe and poison the cached verdict for everyone else.
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	_, err := s.xui.ServerStatus(probeCtx)
	cancel()

	s.healthMu.Lock()
	s.healthOK = err == nil
	s.healthAt = s.now()
	s.healthProbe = nil
	s.healthMu.Unlock()
	close(done)

	if err != nil {
		s.log.Warn("panel health check failed", "err", err)
	}
	return err == nil
}

// subscriptionURL builds the client's subscription link: SUB_BASE_URL wins,
// otherwise panel settings are read and cached for an hour. At most one read
// is in flight; while it runs, callers are served the previous value and only
// a cold cache waits for it.
func (s *Server) subscriptionURL(ctx context.Context, subID string) string {
	if s.env.SubBaseURL != "" {
		return strings.TrimRight(s.env.SubBaseURL, "/") + "/" + subID
	}

	s.subMu.Lock()
	if s.subBase != "" && time.Since(s.subBaseAt) <= time.Hour {
		base := s.subBase
		s.subMu.Unlock()
		return base + subID
	}
	if wait := s.subLoad; wait != nil {
		stale := s.subBase
		s.subMu.Unlock()
		if stale != "" {
			return stale + subID // refresh in flight: stale beats blocking
		}
		select {
		case <-wait:
		case <-ctx.Done():
			return ""
		}
		s.subMu.Lock()
		base := s.subBase
		s.subMu.Unlock()
		return appendSubID(base, subID)
	}
	done := make(chan struct{})
	s.subLoad = done
	s.subMu.Unlock()

	settings, err := s.xui.Settings(context.WithoutCancel(ctx))

	s.subMu.Lock()
	if err == nil {
		s.subBase = settings.SubscriptionURL("")
		s.subBaseAt = s.now()
	}
	base := s.subBase
	s.subLoad = nil
	s.subMu.Unlock()
	close(done)

	if err != nil {
		s.log.Warn("cannot read panel settings for subscription URL", "err", err)
	}
	return appendSubID(base, subID)
}

// appendSubID keeps an unknown base URL reported as "no link" rather than as a
// bare subscription id.
func appendSubID(base, subID string) string {
	if base == "" {
		return ""
	}
	return base + subID
}
