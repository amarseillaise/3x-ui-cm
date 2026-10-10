package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/amarseillaise/3x-ui-cm/internal/auth"
	"github.com/amarseillaise/3x-ui-cm/internal/store"

	"github.com/amarseillaise/3x-ui-cm/internal/xui"
)

// handleAdminMakeLink is GET /api/admin/link?email=<email>: builds the link to
// hand to the client. "url" is the subscription URL (the same one the VPN app
// uses; opened in a browser it lands in the cabinet via the panel template),
// "cabinetUrl" is the direct magic link. When panel settings are unreachable
// "url" falls back to the direct link.
func (s *Server) handleAdminMakeLink(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if email == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Укажите email клиента: ?email=")
		return
	}
	detail, err := s.xui.GetClient(r.Context(), email)
	var apiErr *xui.APIError
	switch {
	case errors.Is(err, xui.ErrNotFound) || errors.As(err, &apiErr):
		writeError(w, http.StatusNotFound, "client_not_found", "Клиент не найден в панели")
		return
	case err != nil:
		s.log.Warn("admin link: panel unavailable", "err", err)
		writeError(w, http.StatusBadGateway, "panel_unavailable", "Панель временно недоступна")
		return
	}
	if detail.Client.SubID == "" {
		writeError(w, http.StatusConflict, "no_sub_id", "У клиента нет subId")
		return
	}
	cabinet := s.env.AppBaseURL + "/s/" + detail.Client.SubID
	url := s.subscriptionURL(r.Context(), detail.Client.SubID)
	if url == "" {
		url = cabinet
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"email":      detail.Client.Email,
		"url":        url,
		"cabinetUrl": cabinet,
	})
}

// handleAdminStats is GET /api/admin/stats.
func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sessions, err := s.store.CountSessions(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pushes, _ := s.store.CountPushSubscriptions(ctx)
	renewals, _ := s.store.CountRenewals(ctx)
	notifications, _ := s.store.CountNotifications(ctx)
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions":       sessions,
		"push":           pushes,
		"renewals":       renewals,
		"notifications":  notifications,
		"pushEnabled":    s.push.Enabled(),
		"vapidPublicKey": s.push.PublicKey(),
		"plans":          len(s.app.Plans),
	})
}

// handleAdminLogin is POST /api/admin/login: the password from ADMIN_PASSWORD
// opens an admin session. It replaced the /a/<secret> link, which iOS could not
// carry into an app installed on the home screen.
func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	ip := auth.ClientIP(r, s.env.TrustProxy)
	if !s.logins.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Слишком много попыток, подождите минуту")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса")
		return
	}
	if !passwordMatches(req.Password, s.env.AdminPassword) {
		// Logged with the address so a fail2ban rule can act on repeats.
		s.log.Warn("admin login: wrong password", "ip", ip)
		writeError(w, http.StatusUnauthorized, "wrong_password", "Неверный пароль")
		return
	}
	if _, err := s.sessions.Create(r.Context(), w, store.RoleAdmin, "", r.UserAgent()); err != nil {
		s.log.Error("admin login: create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
		return
	}
	s.log.Info("admin login", "ip", ip)
	w.WriteHeader(http.StatusNoContent)
}

// passwordMatches compares in constant time. Hashing both sides first also
// hides the length, which subtle.ConstantTimeCompare would reveal by returning
// early on a length mismatch.
func passwordMatches(given, want string) bool {
	if want == "" {
		return false
	}
	g := sha256.Sum256([]byte(given))
	w := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(g[:], w[:]) == 1
}
