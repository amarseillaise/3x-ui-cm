// Package push sends Web Push notifications (VAPID) to stored subscriptions
// and prunes endpoints the push service reports as gone.
package push

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/amarseillaise/3x-ui-cm/internal/store"
)

const (
	defaultTTL      = 24 * 3600 // seconds the push service keeps an undelivered message
	maxFailures     = 10        // consecutive failures before an endpoint is dropped
	sendConcurrency = 8
	sendTimeout     = 15 * time.Second

	// An endpoint the push service keeps accepting, but whose device never
	// confirms receipt, is dropped once both limits are passed. Apple answers
	// 2xx for a web app that was removed from the home screen, so status codes
	// alone never reveal it. Both limits are needed: a phone that is simply
	// offline for a few days must not lose its subscription.
	maxUnconfirmed   = 3
	unconfirmedGrace = 14 * 24 * time.Hour
)

// Message is the notification payload the service worker renders.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
	Tag   string `json:"tag,omitempty"`
}

// Result summarises one broadcast.
type Result struct {
	Sent    int `json:"sent"`
	Failed  int `json:"failed"`
	Removed int `json:"removed"`
}

// Config is what a Sender needs. A struct rather than positional strings: four
// of them in a row are too easy to swap.
type Config struct {
	PublicKey  string // VAPID; empty keys give a disabled sender
	PrivateKey string
	Subject    string // contact: "mailto:you@example.com", "you@example.com" or an https URL
	// BaseURL makes notification links absolute. A relative link is resolved
	// by the service worker against its own origin, so a subscription made
	// before the site moved would open the old, dead address.
	BaseURL string
}

// Sender delivers messages. A Sender with empty keys is disabled and reports
// every message as failed without contacting anything.
type Sender struct {
	store      *store.Store
	log        *slog.Logger
	publicKey  string
	privateKey string
	subject    string
	baseURL    string
	http       *http.Client
	now        func() time.Time
}

// New builds a sender.
func New(st *store.Store, cfg Config, log *slog.Logger) *Sender {
	if log == nil {
		log = slog.Default()
	}
	return &Sender{
		store:      st,
		log:        log,
		publicKey:  cfg.PublicKey,
		privateKey: cfg.PrivateKey,
		subject:    normalizeSubject(cfg.Subject),
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		http:       &http.Client{Timeout: sendTimeout},
		now:        time.Now,
	}
}

// absoluteURL turns a site path into a full link. Full links and an unset
// base pass through unchanged.
func (s *Sender) absoluteURL(link string) string {
	if s.baseURL == "" {
		return link
	}
	if link == "" {
		return s.baseURL + "/"
	}
	if u, err := url.Parse(link); err == nil && u.IsAbs() {
		return link
	}
	if !strings.HasPrefix(link, "/") {
		link = "/" + link
	}
	return s.baseURL + link
}

// Enabled reports whether VAPID keys are configured.
func (s *Sender) Enabled() bool { return s.publicKey != "" && s.privateKey != "" }

// PublicKey returns the VAPID public key for the browser.
func (s *Sender) PublicKey() string { return s.publicKey }

// normalizeSubject strips a "mailto:" prefix. webpush-go prepends "mailto:"
// to any subject that is not an https URL, so the documented and conventional
// "mailto:you@example.com" would reach the push service as
// "sub":"mailto:mailto:you@example.com". FCM tolerates that; Apple rejects the
// token with 403 and no notification is ever delivered.
func normalizeSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if strings.HasPrefix(strings.ToLower(subject), "mailto:") {
		return strings.TrimSpace(subject[len("mailto:"):])
	}
	return subject
}

// GenerateVAPID creates a new VAPID key pair (base64url, as stored in env).
func GenerateVAPID() (publicKey, privateKey string, err error) {
	privateKey, publicKey, err = webpush.GenerateVAPIDKeys()
	return publicKey, privateKey, err
}

// SendToSubID notifies every endpoint of one subscription.
func (s *Sender) SendToSubID(ctx context.Context, subID string, msg Message) Result {
	subs, err := s.store.ListPushSubscriptions(ctx, "", []string{subID})
	if err != nil {
		s.log.Error("push: list subscriptions", "err", err)
		return Result{}
	}
	return s.SendTo(ctx, subs, msg)
}

// SendToAdmins notifies every admin endpoint.
func (s *Sender) SendToAdmins(ctx context.Context, msg Message) Result {
	subs, err := s.store.ListPushSubscriptions(ctx, store.RoleAdmin, nil)
	if err != nil {
		s.log.Error("push: list admin subscriptions", "err", err)
		return Result{}
	}
	return s.SendTo(ctx, subs, msg)
}

// SendToAllClients notifies every client endpoint.
func (s *Sender) SendToAllClients(ctx context.Context, msg Message) Result {
	subs, err := s.store.ListPushSubscriptions(ctx, store.RoleClient, nil)
	if err != nil {
		s.log.Error("push: list client subscriptions", "err", err)
		return Result{}
	}
	return s.SendTo(ctx, subs, msg)
}

// SendTo delivers msg to the given endpoints with bounded concurrency.
func (s *Sender) SendTo(ctx context.Context, subs []store.PushSubscription, msg Message) Result {
	if len(subs) == 0 {
		return Result{}
	}
	if !s.Enabled() {
		s.log.Warn("push: disabled (no VAPID keys), message dropped", "recipients", len(subs))
		return Result{Failed: len(subs)}
	}
	msg.URL = s.absoluteURL(msg.URL)
	payload, err := json.Marshal(msg)
	if err != nil {
		s.log.Error("push: encode payload", "err", err)
		return Result{Failed: len(subs)}
	}

	var (
		mu  sync.Mutex
		res Result
		wg  sync.WaitGroup
		sem = make(chan struct{}, sendConcurrency)
	)
	for _, sub := range subs {
		wg.Add(1)
		sem <- struct{}{}
		go func(sub store.PushSubscription) {
			defer wg.Done()
			defer func() { <-sem }()
			outcome := s.sendOne(ctx, sub, payload)
			mu.Lock()
			switch outcome {
			case outcomeSent:
				res.Sent++
			case outcomeRemoved:
				res.Removed++
			default:
				res.Failed++
			}
			mu.Unlock()
		}(sub)
	}
	wg.Wait()
	return res
}

type outcome int

const (
	outcomeFailed outcome = iota
	outcomeSent
	outcomeRemoved
)

func (s *Sender) sendOne(ctx context.Context, sub store.PushSubscription, payload []byte) outcome {
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		HTTPClient:      s.http,
		Subscriber:      s.subject,
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
		TTL:             defaultTTL,
		Urgency:         webpush.UrgencyNormal,
	})
	if err != nil {
		s.log.Warn("push: send failed", "err", err)
		return s.recordFailure(ctx, sub)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		if _, err := s.store.DeletePushSubscription(ctx, sub.Endpoint); err != nil {
			s.log.Error("push: delete gone subscription", "err", err)
		}
		return outcomeRemoved
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return s.recordAccepted(ctx, sub)
	default:
		s.log.Warn("push: unexpected status", "status", resp.StatusCode)
		return s.recordFailure(ctx, sub)
	}
}

// recordAccepted counts a message the push service took and drops the
// endpoint when its device has been silent for too long.
func (s *Sender) recordAccepted(ctx context.Context, sub store.PushSubscription) outcome {
	now := s.now()
	silence, err := s.store.RecordPushAccepted(ctx, sub.Endpoint, now.UnixMilli())
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) && !errors.Is(err, context.Canceled) {
			s.log.Error("push: record accepted", "err", err)
		}
		return outcomeSent
	}
	if !isSilent(silence, now) {
		return outcomeSent
	}
	if _, err := s.store.DeletePushSubscription(ctx, sub.Endpoint); err != nil {
		s.log.Error("push: drop silent endpoint", "err", err)
		return outcomeSent
	}
	s.log.Info("push: dropped an endpoint that never confirms delivery",
		"service", serviceHost(sub.Endpoint), "role", sub.Role, "sub", sub.SubID,
		"unconfirmed", silence.Unconfirmed, "silent_days", int(now.Sub(time.UnixMilli(silence.Since)).Hours()/24))
	return outcomeRemoved
}

// isSilent applies both limits: enough unanswered messages, over long enough.
func isSilent(silence store.PushSilence, now time.Time) bool {
	return silence.Unconfirmed >= maxUnconfirmed &&
		silence.Since > 0 &&
		now.Sub(time.UnixMilli(silence.Since)) >= unconfirmedGrace
}

// serviceHost names the push service for logs without exposing the endpoint,
// which is a bearer capability.
func serviceHost(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil {
		return u.Host
	}
	return "unknown"
}

func (s *Sender) recordFailure(ctx context.Context, sub store.PushSubscription) outcome {
	if sub.FailCount+1 >= maxFailures {
		if _, err := s.store.DeletePushSubscription(ctx, sub.Endpoint); err == nil {
			return outcomeRemoved
		}
	}
	if err := s.store.RecordPushFailure(ctx, sub.Endpoint); err != nil && !errors.Is(err, context.Canceled) {
		s.log.Error("push: record failure", "err", err)
	}
	return outcomeFailed
}

// Receipt records that a device's service worker received a message. It
// reports whether the endpoint is known.
func (s *Sender) Receipt(ctx context.Context, endpoint string) (bool, error) {
	return s.store.RecordPushReceipt(ctx, endpoint, s.now().UnixMilli())
}
