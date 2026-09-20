package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/amarseillaise/3x-ui-cm/internal/auth"
	"github.com/amarseillaise/3x-ui-cm/internal/store"
)

type ctxKey struct{}

// sessionFrom returns the session attached by the auth middleware.
func sessionFrom(ctx context.Context) *store.Session {
	s, _ := ctx.Value(ctxKey{}).(*store.Session)
	return s
}

// bearerAdmin reports whether the request carries the admin API token.
func (s *Server) bearerAdmin(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.env.AdminToken)) == 1
}

// requireRole resolves the session and enforces the role. Admin routes also
// accept "Authorization: Bearer <ADMIN_TOKEN>" for the CLI.
func (s *Server) requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if role == store.RoleAdmin && s.bearerAdmin(r) {
				sess := &store.Session{ID: "bearer", Role: store.RoleAdmin}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
				return
			}
			sess, err := s.sessions.FromRequest(r.Context(), w, r)
			if errors.Is(err, auth.ErrNoSession) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "Откройте персональную ссылку для входа")
				return
			}
			if err != nil {
				s.log.Error("session lookup failed", "err", err)
				writeError(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
				return
			}
			if sess.Role != role {
				writeError(w, http.StatusForbidden, "forbidden", "Недостаточно прав")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
		})
	}
}

// requireAnyRole resolves any valid session.
func (s *Server) requireAnyRole() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, err := s.sessions.FromRequest(r.Context(), w, r)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "Сессия не найдена")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
		})
	}
}
