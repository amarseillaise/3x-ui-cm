// Package renewal implements the trust-based renewal flow: the client claims a
// payment, the subscription is extended immediately on the panel, the admin is
// notified and later confirms or rejects (rolls back) the request.
package renewal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/config"
	"github.com/amarseillaise/3x-ui-cm/internal/domain"
	"github.com/amarseillaise/3x-ui-cm/internal/push"
	"github.com/amarseillaise/3x-ui-cm/internal/store"
	"github.com/amarseillaise/3x-ui-cm/internal/xui"
)

// Errors returned by the service.
var (
	ErrDisabled      = errors.New("renewal: disabled (no plans or requisites configured)")
	ErrPlanNotFound  = errors.New("renewal: plan not found")
	ErrUnlimited     = errors.New("renewal: subscription has no expiry")
	ErrPendingExists = errors.New("renewal: a pending request already exists")
	ErrNotFound      = errors.New("renewal: request not found")
	ErrNotPending    = errors.New("renewal: request is not pending")
	ErrSubscription  = errors.New("renewal: subscription not found on the panel")
)

// PanelError wraps failures talking to the panel so handlers can map them to 502.
type PanelError struct{ Err error }

func (e *PanelError) Error() string { return "renewal: panel: " + e.Err.Error() }
func (e *PanelError) Unwrap() error { return e.Err }

// Instructions tell the client how to pay for a plan.
type Instructions struct {
	Amount     int
	Currency   string
	Requisites []config.Requisite
	Note       string
}

// Payment produces payment instructions. The only implementation today is
// Manual (bank transfer by requisites); an online provider would plug in here.
type Payment interface {
	Instructions(plan config.Plan) Instructions
}

// Manual returns requisites from config.yaml.
type Manual struct{ App *config.AppConfig }

// Instructions implements Payment.
func (m Manual) Instructions(plan config.Plan) Instructions {
	return Instructions{Amount: plan.Price, Currency: m.App.Currency, Requisites: m.App.Requisites, Note: m.App.PaymentNote}
}

// Service coordinates store, panel and push.
type Service struct {
	store   *store.Store
	panel   *xui.Client
	push    *push.Sender
	app     *config.AppConfig
	payment Payment
	log     *slog.Logger
	now     func() time.Time
}

// New builds the service.
func New(st *store.Store, panel *xui.Client, sender *push.Sender, app *config.AppConfig, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: st, panel: panel, push: sender, app: app, payment: Manual{App: app}, log: log, now: time.Now}
}

// SetClock overrides the time source (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Enabled reports whether plans and requisites are configured.
func (s *Service) Enabled() bool { return s.app.RenewalEnabled() }

// Plans returns the configured plans.
func (s *Service) Plans() []config.Plan { return s.app.Plans }

// Instructions returns payment instructions for a plan.
func (s *Service) Instructions(plan config.Plan) Instructions { return s.payment.Instructions(plan) }

// PlanTitle resolves a plan title for display, falling back to the id.
func (s *Service) PlanTitle(planID string) string {
	if p, ok := s.app.Plan(planID); ok {
		return p.Title
	}
	return planID
}

// Request handles "I have paid": extends the subscription immediately and
// records a pending request for the admin.
func (s *Service) Request(ctx context.Context, subID, planID string) (*store.RenewalRequest, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	plan, ok := s.app.Plan(planID)
	if !ok {
		return nil, ErrPlanNotFound
	}
	sub, err := s.lookup(ctx, subID)
	if err != nil {
		return nil, err
	}
	if sub.Unlimited() {
		return nil, ErrUnlimited
	}
	now := s.now()
	addDays, err := domain.DaysToAdd(plan.Days, sub.ExpiresAt, now)
	if err != nil {
		return nil, err
	}

	req := &store.RenewalRequest{
		SubID:        subID,
		Email:        strings.Join(sub.Emails, ", "),
		PlanID:       plan.ID,
		Days:         plan.Days,
		Amount:       plan.Price,
		Currency:     s.app.Currency,
		Status:       store.RenewalPending,
		AppliedDays:  addDays,
		ExpiryBefore: domain.TimeToMs(sub.ExpiresAt),
		CreatedAt:    now.UnixMilli(),
	}
	if err := s.store.CreateRenewal(ctx, req); err != nil {
		if errors.Is(err, store.ErrPendingExists) {
			return nil, ErrPendingExists
		}
		return nil, err
	}

	if _, err := s.panel.BulkAdjust(ctx, xui.BulkAdjustRequest{Emails: sub.Emails, AddDays: addDays}); err != nil {
		s.log.Error("renewal: bulkAdjust failed", "request", req.ID, "err", err)
		if dbErr := s.store.SetRenewalOutcome(ctx, req.ID, store.RenewalFailed, 0, truncate(err.Error(), 500)); dbErr != nil {
			s.log.Error("renewal: mark failed", "request", req.ID, "err", dbErr)
		}
		return nil, &PanelError{Err: err}
	}
	req.ExpiryAfter = s.readExpiry(ctx, subID, req.ExpiryBefore)
	if err := s.store.SetRenewalOutcome(ctx, req.ID, store.RenewalPending, req.ExpiryAfter, ""); err != nil {
		s.log.Error("renewal: record outcome", "request", req.ID, "err", err)
	}
	s.log.Info("renewal: applied", "request", req.ID, "days", addDays)

	s.notifyAdmins(ctx, req, plan)
	return req, nil
}

// Confirm marks a pending request as paid and tells the client.
func (s *Service) Confirm(ctx context.Context, id int64, by string) (*store.RenewalRequest, error) {
	req, err := s.pending(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.store.ResolveRenewal(ctx, id, store.RenewalConfirmed, by, s.now().UnixMilli(), 0); err != nil {
		return nil, mapResolveErr(err)
	}
	s.log.Info("renewal: confirmed", "request", id, "by", by)
	s.notifyClient(ctx, req, "Оплата подтверждена", fmt.Sprintf("Продление на %d дн. подтверждено. Спасибо!", req.Days), "confirmed")
	return s.store.GetRenewal(ctx, id)
}

// Reject rolls the extension back on the panel, marks the request rejected and
// tells the client. If the panel write fails the request stays pending.
func (s *Service) Reject(ctx context.Context, id int64, by string) (*store.RenewalRequest, error) {
	req, err := s.pending(ctx, id)
	if err != nil {
		return nil, err
	}
	sub, err := s.lookup(ctx, req.SubID)
	if err != nil {
		return nil, err
	}
	if req.AppliedDays > 0 {
		if _, err := s.panel.BulkAdjust(ctx, xui.BulkAdjustRequest{Emails: sub.Emails, AddDays: -req.AppliedDays}); err != nil {
			s.log.Error("renewal: rollback failed", "request", id, "err", err)
			return nil, &PanelError{Err: err}
		}
	}
	expiryAfter := s.readExpiry(ctx, req.SubID, req.ExpiryAfter)
	if err := s.store.ResolveRenewal(ctx, id, store.RenewalRejected, by, s.now().UnixMilli(), expiryAfter); err != nil {
		return nil, mapResolveErr(err)
	}
	s.log.Info("renewal: rejected", "request", id, "by", by, "rolled_back_days", req.AppliedDays)
	s.notifyClient(ctx, req, "Платёж не найден", "Продление отменено: перевод не поступил. Проверьте реквизиты и попробуйте снова.", "rejected")
	return s.store.GetRenewal(ctx, id)
}

func (s *Service) pending(ctx context.Context, id int64) (*store.RenewalRequest, error) {
	req, err := s.store.GetRenewal(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if req.Status != store.RenewalPending {
		return nil, ErrNotPending
	}
	return req, nil
}

func (s *Service) lookup(ctx context.Context, subID string) (domain.Subscription, error) {
	recs, err := s.panel.FindBySubID(ctx, subID)
	if errors.Is(err, xui.ErrNotFound) {
		return domain.Subscription{}, ErrSubscription
	}
	if err != nil {
		return domain.Subscription{}, &PanelError{Err: err}
	}
	sub, _ := domain.FromRecords(recs)
	return sub, nil
}

// readExpiry re-reads the expiry after a write; on failure it returns fallback.
func (s *Service) readExpiry(ctx context.Context, subID string, fallback int64) int64 {
	sub, err := s.lookup(ctx, subID)
	if err != nil {
		s.log.Warn("renewal: cannot re-read expiry", "err", err)
		return fallback
	}
	return domain.TimeToMs(sub.ExpiresAt)
}

func (s *Service) notifyAdmins(ctx context.Context, req *store.RenewalRequest, plan config.Plan) {
	msg := push.Message{
		Title: "Заявка на продление",
		Body:  fmt.Sprintf("%s: %s, %d %s", req.Email, plan.Title, req.Amount, req.Currency),
		URL:   "/admin",
		Tag:   fmt.Sprintf("renewal-%d", req.ID),
	}
	res := s.push.SendToAdmins(ctx, msg)
	if _, err := s.store.RecordNotification(ctx, store.Notification{Kind: "admin_renewal_new", Ref: fmt.Sprint(req.ID), Title: msg.Title, SentAt: s.now().UnixMilli()}); err != nil {
		s.log.Error("renewal: log admin notification", "err", err)
	}
	s.log.Info("renewal: admins notified", "request", req.ID, "sent", res.Sent, "failed", res.Failed, "removed", res.Removed)
}

func (s *Service) notifyClient(ctx context.Context, req *store.RenewalRequest, title, body, kind string) {
	res := s.push.SendToSubID(ctx, req.SubID, push.Message{Title: title, Body: body, URL: "/", Tag: fmt.Sprintf("renewal-%d", req.ID)})
	if _, err := s.store.RecordNotification(ctx, store.Notification{SubID: req.SubID, Kind: "renewal_" + kind, Ref: fmt.Sprint(req.ID), Title: title, SentAt: s.now().UnixMilli()}); err != nil {
		s.log.Error("renewal: log client notification", "err", err)
	}
	s.log.Info("renewal: client notified", "request", req.ID, "kind", kind, "sent", res.Sent)
}

func mapResolveErr(err error) error {
	if errors.Is(err, store.ErrNotPending) {
		return ErrNotPending
	}
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
