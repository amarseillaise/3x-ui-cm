package httpapi

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/auth"
	"github.com/amarseillaise/3x-ui-cm/internal/domain"
	"github.com/amarseillaise/3x-ui-cm/internal/store"
	"github.com/amarseillaise/3x-ui-cm/internal/xui"
)

// expiringWindow is the "expiring soon" window shown in the UI: the largest configured reminder.
func (s *Server) expiringWindow() time.Duration {
	days := 7
	if len(s.env.NotifyDays) > 0 {
		days = s.env.NotifyDays[0]
	}
	return time.Duration(days) * domain.Day
}

var subIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)

// handleClientLink is the client magic link: GET /s/{subId}.
func (s *Server) handleClientLink(w http.ResponseWriter, r *http.Request) {
	if !s.links.Allow(auth.ClientIP(r, s.env.TrustProxy)) {
		http.Error(w, "Слишком много попыток, подождите минуту", http.StatusTooManyRequests)
		return
	}
	subID := r.PathValue("subId")
	if !subIDPattern.MatchString(subID) {
		http.Redirect(w, r, "/invalid-link", http.StatusSeeOther)
		return
	}
	_, err := s.xui.FindBySubID(r.Context(), subID)
	switch {
	case errors.Is(err, xui.ErrNotFound):
		http.Redirect(w, r, "/invalid-link", http.StatusSeeOther)
		return
	case err != nil:
		s.log.Warn("magic link: panel lookup failed", "err", err)
		http.Redirect(w, r, "/invalid-link?reason=unavailable", http.StatusSeeOther)
		return
	}
	if _, err := s.sessions.Create(r.Context(), w, store.RoleClient, subID, r.UserAgent()); err != nil {
		s.log.Error("magic link: create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleAdminLink is the admin magic link: GET /a/{secret}.
func (s *Server) handleAdminLink(w http.ResponseWriter, r *http.Request) {
	if !s.links.Allow(auth.ClientIP(r, s.env.TrustProxy)) {
		http.Error(w, "Слишком много попыток, подождите минуту", http.StatusTooManyRequests)
		return
	}
	secret := r.PathValue("secret")
	if subtle.ConstantTimeCompare([]byte(secret), []byte(s.env.AdminLinkSecret)) != 1 {
		http.Redirect(w, r, "/invalid-link", http.StatusSeeOther)
		return
	}
	if _, err := s.sessions.Create(r.Context(), w, store.RoleAdmin, "", r.UserAgent()); err != nil {
		s.log.Error("admin link: create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// handleMe is GET /api/me.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := sessionFrom(ctx)
	recs, err := s.xui.ClientsBySubID(ctx, sess.SubID)
	switch {
	case errors.Is(err, xui.ErrNotFound):
		// The client was removed from the panel: the link is no longer valid.
		_ = s.sessions.Destroy(ctx, w, r)
		writeError(w, http.StatusNotFound, "subscription_not_found", "Подписка не найдена. Запросите новую ссылку у администратора")
		return
	case err != nil:
		s.log.Warn("me: panel unavailable", "err", err)
		writeError(w, http.StatusBadGateway, "panel_unavailable", "Панель временно недоступна, попробуйте позже")
		return
	}
	sub, _ := domain.FromRecords(recs)
	now := s.now()

	online := false
	if onlines, err := s.xui.Onlines(ctx); err == nil {
		set := make(map[string]struct{}, len(onlines))
		for _, e := range onlines {
			set[e] = struct{}{}
		}
		for _, e := range sub.Emails {
			if _, ok := set[e]; ok {
				online = true
				break
			}
		}
	} else {
		s.log.Debug("me: onlines unavailable", "err", err)
	}

	hasPending, err := s.store.HasPendingRenewal(ctx, sess.SubID)
	if err != nil {
		s.log.Error("me: pending lookup", "err", err)
	}
	pushSubscribed, err := s.store.HasPushSubscription(ctx, sess.SubID)
	if err != nil {
		s.log.Error("me: push lookup", "err", err)
	}
	renewalEnabled := s.app.RenewalEnabled()

	writeJSON(w, http.StatusOK, meResponse{
		Subscription: subscriptionDTO{
			Status:          sub.Status(now, s.expiringWindow()),
			Enabled:         sub.Enabled,
			Unlimited:       sub.Unlimited(),
			ExpiresAt:       timePtr(sub.ExpiresAt),
			DaysLeft:        sub.DaysLeft(now),
			QuotaBytes:      sub.QuotaBytes,
			UsedBytes:       sub.UsedBytes,
			TrafficPercent:  sub.TrafficPercent(),
			Online:          online,
			LastOnline:      timePtr(sub.LastOnline),
			SubscriptionURL: s.subscriptionURL(ctx, sess.SubID),
			InboundCount:    sub.InboundCount,
		},
		Renewal: renewalDTO{
			Enabled:    renewalEnabled,
			HasPending: hasPending,
			CanRenew:   renewalEnabled && !sub.Unlimited() && !hasPending,
		},
		Push: pushDTO{
			Enabled:        s.push.Enabled(),
			VAPIDPublicKey: s.push.PublicKey(),
			Subscribed:     pushSubscribed,
		},
	})
}

// handleLogout is POST /api/logout.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Destroy(r.Context(), w, r); err != nil {
		s.log.Error("logout", "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}
