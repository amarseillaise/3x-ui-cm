package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/config"
	"github.com/amarseillaise/3x-ui-cm/internal/domain"
	"github.com/amarseillaise/3x-ui-cm/internal/renewal"
	"github.com/amarseillaise/3x-ui-cm/internal/store"
)

type renewalDTO2 struct {
	ID           int64      `json:"id"`
	Email        string     `json:"email,omitempty"`
	PlanID       string     `json:"planId"`
	PlanTitle    string     `json:"planTitle"`
	Days         int        `json:"days"`
	Amount       int        `json:"amount"`
	Currency     string     `json:"currency"`
	Status       string     `json:"status"`
	AppliedDays  int        `json:"appliedDays"`
	ExpiryBefore *time.Time `json:"expiryBefore"`
	ExpiryAfter  *time.Time `json:"expiryAfter"`
	CreatedAt    time.Time  `json:"createdAt"`
	ResolvedAt   *time.Time `json:"resolvedAt"`
	Error        string     `json:"error,omitempty"`
}

func (s *Server) renewalToDTO(r store.RenewalRequest, withEmail bool) renewalDTO2 {
	d := renewalDTO2{
		ID: r.ID, PlanID: r.PlanID, PlanTitle: s.renewal.PlanTitle(r.PlanID), Days: r.Days, Amount: r.Amount, Currency: r.Currency,
		Status: r.Status, AppliedDays: r.AppliedDays,
		ExpiryBefore: timePtr(domain.MsToTime(r.ExpiryBefore)),
		ExpiryAfter:  timePtr(domain.MsToTime(r.ExpiryAfter)),
		CreatedAt:    domain.MsToTime(r.CreatedAt),
		ResolvedAt:   timePtr(domain.MsToTime(r.ResolvedAt)),
	}
	if withEmail {
		d.Email = r.Email
		d.Error = r.Error
	}
	return d
}

func (s *Server) renewalsToDTO(list []store.RenewalRequest, withEmail bool) []renewalDTO2 {
	out := make([]renewalDTO2, 0, len(list))
	for _, r := range list {
		out = append(out, s.renewalToDTO(r, withEmail))
	}
	return out
}

// handlePlans is GET /api/plans.
func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	hasPending, err := s.store.HasPendingRenewal(r.Context(), sess.SubID)
	if err != nil {
		s.log.Error("plans: pending lookup", "err", err)
	}
	plans := s.renewal.Plans()
	if plans == nil {
		plans = []config.Plan{}
	}
	requisites := s.app.Requisites
	if requisites == nil {
		requisites = []config.Requisite{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":     s.renewal.Enabled(),
		"currency":    s.app.Currency,
		"plans":       plans,
		"requisites":  requisites,
		"paymentNote": s.app.PaymentNote,
		"hasPending":  hasPending,
	})
}

// handleRenew is POST /api/renew: the client claims a payment.
func (s *Server) handleRenew(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlanID string `json:"planId"`
	}
	if err := readJSON(r, &req); err != nil || req.PlanID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Укажите planId")
		return
	}
	sess := sessionFrom(r.Context())
	result, err := s.renewal.Request(r.Context(), sess.SubID, req.PlanID)
	if err != nil {
		s.writeRenewalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request":   s.renewalToDTO(*result, false),
		"expiresAt": timePtr(domain.MsToTime(result.ExpiryAfter)),
	})
}

// handleMyRenewals is GET /api/renewals.
func (s *Server) handleMyRenewals(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	list, err := s.store.ListRenewalsBySubID(r.Context(), sess.SubID, 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить заявки")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": s.renewalsToDTO(list, false)})
}

// handleAdminRenewals is GET /api/admin/renewals?status=pending&limit=50.
func (s *Server) handleAdminRenewals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "", store.RenewalPending, store.RenewalConfirmed, store.RenewalRejected, store.RenewalFailed:
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "Неизвестный статус")
		return
	}
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	list, err := s.store.ListRenewals(r.Context(), status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить заявки")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": s.renewalsToDTO(list, true)})
}

// handleAdminResolve is POST /api/admin/renewals/{id}/{action}.
func (s *Server) handleAdminResolve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный id")
		return
	}
	by := "admin"
	if sess := sessionFrom(r.Context()); sess != nil && sess.ID == "bearer" {
		by = "cli"
	}
	var result *store.RenewalRequest
	switch r.PathValue("action") {
	case "confirm":
		result, err = s.renewal.Confirm(r.Context(), id, by)
	case "reject":
		result, err = s.renewal.Reject(r.Context(), id, by)
	default:
		writeError(w, http.StatusNotFound, "not_found", "Неизвестное действие")
		return
	}
	if err != nil {
		s.writeRenewalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": s.renewalToDTO(*result, true)})
}

func (s *Server) writeRenewalError(w http.ResponseWriter, err error) {
	var panelErr *renewal.PanelError
	switch {
	case errors.Is(err, renewal.ErrDisabled):
		writeError(w, http.StatusServiceUnavailable, "renewal_disabled", "Продление пока недоступно")
	case errors.Is(err, renewal.ErrPlanNotFound):
		writeError(w, http.StatusNotFound, "plan_not_found", "Тариф не найден")
	case errors.Is(err, renewal.ErrUnlimited):
		writeError(w, http.StatusConflict, "unlimited", "У подписки нет срока действия, продление не требуется")
	case errors.Is(err, renewal.ErrPendingExists):
		writeError(w, http.StatusConflict, "pending_exists", "Предыдущая заявка ещё не подтверждена администратором")
	case errors.Is(err, renewal.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Заявка не найдена")
	case errors.Is(err, renewal.ErrNotPending):
		writeError(w, http.StatusConflict, "not_pending", "Заявка уже обработана")
	case errors.Is(err, renewal.ErrSubscription):
		writeError(w, http.StatusNotFound, "subscription_not_found", "Подписка не найдена в панели")
	case errors.As(err, &panelErr):
		s.log.Warn("renewal: panel error", "err", err)
		writeError(w, http.StatusBadGateway, "panel_unavailable", "Панель временно недоступна, попробуйте позже")
	default:
		s.log.Error("renewal: internal error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
	}
}
