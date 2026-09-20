// Package notify runs the background scheduler: expiry and traffic reminders
// for clients and daily reminders to admins about unconfirmed renewals.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/domain"
	"github.com/amarseillaise/3x-ui-cm/internal/push"
	"github.com/amarseillaise/3x-ui-cm/internal/store"
	"github.com/amarseillaise/3x-ui-cm/internal/xui"
)

const (
	firstRunDelay   = 30 * time.Second
	adminRemindAge  = 24 * time.Hour
	dateLayout      = "2 January 2006"
	monthRefLayout  = "2006-01"
	adminRemindKind = "admin_pending_reminder"
)

// Stats summarises one scheduler pass.
type Stats struct {
	Subscriptions int
	Sent          int
	Reminders     int
}

// Scheduler is the periodic checker.
type Scheduler struct {
	store      *store.Store
	panel      *xui.Client
	push       *push.Sender
	log        *slog.Logger
	interval   time.Duration
	notifyDays []int // descending, e.g. 7,3,1
	trafficPct int
	now        func() time.Time
}

// New builds a scheduler.
func New(st *store.Store, panel *xui.Client, sender *push.Sender, interval time.Duration, notifyDays []int, trafficPct int, log *slog.Logger) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{store: st, panel: panel, push: sender, log: log, interval: interval, notifyDays: notifyDays, trafficPct: trafficPct, now: time.Now}
}

// Run executes passes every interval until ctx is done.
func (s *Scheduler) Run(ctx context.Context) {
	if !s.push.Enabled() {
		s.log.Warn("scheduler: push disabled, reminders will not be delivered")
	}
	timer := time.NewTimer(firstRunDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if st, err := s.RunOnce(ctx); err != nil {
			s.log.Warn("scheduler: pass failed", "err", err)
		} else if st.Sent > 0 || st.Reminders > 0 {
			s.log.Info("scheduler: pass done", "subscriptions", st.Subscriptions, "sent", st.Sent, "reminders", st.Reminders)
		}
		timer.Reset(s.interval)
	}
}

// RunOnce performs a single pass.
func (s *Scheduler) RunOnce(ctx context.Context) (Stats, error) {
	var st Stats
	list, err := s.panel.ListClients(ctx)
	if err != nil {
		return st, fmt.Errorf("list clients: %w", err)
	}
	groups := map[string][]xui.ClientRecord{}
	var order []string
	for _, r := range list {
		if r.SubID == "" {
			continue
		}
		if _, ok := groups[r.SubID]; !ok {
			order = append(order, r.SubID)
		}
		groups[r.SubID] = append(groups[r.SubID], r)
	}
	now := s.now()
	for _, subID := range order {
		if ctx.Err() != nil {
			return st, ctx.Err()
		}
		sub, _ := domain.FromRecords(groups[subID])
		st.Subscriptions++
		has, err := s.store.HasPushSubscription(ctx, subID)
		if err != nil {
			s.log.Error("scheduler: push lookup", "err", err)
			continue
		}
		if !has {
			continue
		}
		if kind, ref, msg, ok := s.expiryMessage(sub, now); ok {
			st.Sent += s.deliver(ctx, subID, kind, ref, msg)
		}
		if kind, ref, msg, ok := s.trafficMessage(sub, now); ok {
			st.Sent += s.deliver(ctx, subID, kind, ref, msg)
		}
	}
	st.Reminders = s.remindAdmins(ctx, now)
	return st, nil
}

// expiryMessage picks the most urgent unsent threshold for the subscription.
func (s *Scheduler) expiryMessage(sub domain.Subscription, now time.Time) (kind, ref string, msg push.Message, ok bool) {
	if sub.Unlimited() {
		return "", "", push.Message{}, false
	}
	ref = strconv.FormatInt(domain.TimeToMs(sub.ExpiresAt), 10)
	until := sub.ExpiresAt.Local().Format(dateLayout)
	if sub.Expired(now) {
		return "expired", ref, push.Message{
			Title: "Подписка истекла",
			Body:  "Доступ приостановлен. Продлите подписку в кабинете.",
			URL:   "/renew",
			Tag:   "expiry",
		}, true
	}
	daysLeft := sub.DaysLeft(now)
	chosen := 0
	for _, d := range s.notifyDays { // descending: the last match is the most urgent
		if daysLeft <= d {
			chosen = d
		}
	}
	if chosen == 0 {
		return "", "", push.Message{}, false
	}
	return fmt.Sprintf("expiry_%dd", chosen), ref, push.Message{
		Title: fmt.Sprintf("Подписка истекает через %s", daysWord(daysLeft)),
		Body:  fmt.Sprintf("Действует до %s. Продлите заранее, чтобы не потерять доступ.", until),
		URL:   "/renew",
		Tag:   "expiry",
	}, true
}

// trafficMessage warns when the quota is nearly or fully used. The reference
// includes the month so a warning repeats after a monthly reset.
func (s *Scheduler) trafficMessage(sub domain.Subscription, now time.Time) (kind, ref string, msg push.Message, ok bool) {
	if sub.QuotaBytes <= 0 {
		return "", "", push.Message{}, false
	}
	ref = fmt.Sprintf("%d:%s", sub.QuotaBytes, now.Format(monthRefLayout))
	switch {
	case sub.Depleted():
		return "depleted", ref, push.Message{Title: "Трафик исчерпан", Body: "Лимит трафика использован полностью.", URL: "/", Tag: "traffic"}, true
	case sub.TrafficPercent() >= s.trafficPct:
		return "traffic_high", ref, push.Message{
			Title: "Трафик почти исчерпан",
			Body:  fmt.Sprintf("Использовано %d%% лимита.", sub.TrafficPercent()),
			URL:   "/",
			Tag:   "traffic",
		}, true
	}
	return "", "", push.Message{}, false
}

// deliver sends once per (subID, kind, ref): the log row is written only after
// at least one endpoint accepted the message, so transient failures retry.
func (s *Scheduler) deliver(ctx context.Context, subID, kind, ref string, msg push.Message) int {
	if s.alreadySent(ctx, subID, kind, ref) {
		return 0
	}
	res := s.push.SendToSubID(ctx, subID, msg)
	if res.Sent == 0 {
		return 0
	}
	if _, err := s.store.RecordNotification(ctx, store.Notification{SubID: subID, Kind: kind, Ref: ref, Title: msg.Title, SentAt: s.now().UnixMilli()}); err != nil {
		s.log.Error("scheduler: record notification", "err", err)
	}
	return res.Sent
}

func (s *Scheduler) alreadySent(ctx context.Context, subID, kind, ref string) bool {
	sent, err := s.store.NotificationSent(ctx, subID, kind, ref)
	if err != nil {
		s.log.Error("scheduler: dedup lookup", "err", err)
		return true // fail closed: never spam on DB trouble
	}
	return sent
}

// remindAdmins pings admins once a day about requests pending longer than 24h.
func (s *Scheduler) remindAdmins(ctx context.Context, now time.Time) int {
	pending, err := s.store.ListPendingRenewalsBefore(ctx, now.Add(-adminRemindAge).UnixMilli())
	if err != nil {
		s.log.Error("scheduler: pending renewals", "err", err)
		return 0
	}
	sent := 0
	for _, r := range pending {
		ref := fmt.Sprintf("%d:%s", r.ID, now.Format("2006-01-02"))
		if s.alreadySent(ctx, "", adminRemindKind, ref) {
			continue
		}
		msg := push.Message{
			Title: "Заявка ждёт подтверждения",
			Body:  fmt.Sprintf("%s: %d дн., %d %s — с %s", r.Email, r.Days, r.Amount, r.Currency, time.UnixMilli(r.CreatedAt).Local().Format(dateLayout)),
			URL:   "/admin",
			Tag:   fmt.Sprintf("renewal-%d", r.ID),
		}
		res := s.push.SendToAdmins(ctx, msg)
		if res.Sent == 0 {
			continue
		}
		if _, err := s.store.RecordNotification(ctx, store.Notification{Kind: adminRemindKind, Ref: ref, Title: msg.Title, SentAt: s.now().UnixMilli()}); err != nil {
			s.log.Error("scheduler: record reminder", "err", err)
		}
		sent++
	}
	return sent
}

func daysWord(n int) string {
	abs := n % 100
	last := abs % 10
	word := "дней"
	switch {
	case abs > 10 && abs < 20:
	case last == 1:
		word = "день"
	case last > 1 && last < 5:
		word = "дня"
	}
	return fmt.Sprintf("%d %s", n, word)
}
