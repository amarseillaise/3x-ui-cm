package httpapi

import (
	"net/http"
	"net/url"

	"github.com/amarseillaise/3x-ui-cm/internal/store"
)

// pushSubscribeRequest is PushSubscription.toJSON() as sent by the browser.
type pushSubscribeRequest struct {
	Endpoint       string `json:"endpoint"`
	ExpirationTime *int64 `json:"expirationTime"`
	Keys           struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func validEndpoint(s string) bool {
	if len(s) == 0 || len(s) > 2048 {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// handlePushSubscribe is POST /api/push/subscribe.
func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if !s.push.Enabled() {
		writeError(w, http.StatusConflict, "push_disabled", "Уведомления не настроены на сервере")
		return
	}
	var req pushSubscribeRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса")
		return
	}
	if !validEndpoint(req.Endpoint) || req.Keys.P256dh == "" || req.Keys.Auth == "" || len(req.Keys.P256dh) > 512 || len(req.Keys.Auth) > 512 {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректная push-подписка")
		return
	}
	sess := sessionFrom(r.Context())
	err := s.store.UpsertPushSubscription(r.Context(), store.PushSubscription{
		Endpoint:  req.Endpoint,
		Role:      sess.Role,
		SubID:     sess.SubID,
		P256dh:    req.Keys.P256dh,
		Auth:      req.Keys.Auth,
		UserAgent: r.UserAgent(),
		CreatedAt: s.now().UnixMilli(),
	})
	if err != nil {
		s.log.Error("push subscribe", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить подписку")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePushUnsubscribe is DELETE /api/push/subscribe.
func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := readJSON(r, &req); err != nil || req.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Укажите endpoint")
		return
	}
	if _, err := s.store.DeletePushSubscription(r.Context(), req.Endpoint); err != nil {
		s.log.Error("push unsubscribe", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить подписку")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
