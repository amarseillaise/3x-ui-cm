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

// Sender delivers messages. A Sender with empty keys is disabled and reports
// every message as failed without contacting anything.
type Sender struct {
	store      *store.Store
	log        *slog.Logger
	publicKey  string
	privateKey string
	subject    string
	http       *http.Client
	now        func() time.Time
}

// New builds a sender. Pass empty keys to get a disabled sender.
func New(st *store.Store, publicKey, privateKey, subject string, log *slog.Logger) *Sender {
	if log == nil {
		log = slog.Default()
	}
	return &Sender{
		store:      st,
		log:        log,
		publicKey:  publicKey,
		privateKey: privateKey,
		subject:    subject,
		http:       &http.Client{Timeout: sendTimeout},
		now:        time.Now,
	}
}

// Enabled reports whether VAPID keys are configured.
func (s *Sender) Enabled() bool { return s.publicKey != "" && s.privateKey != "" }

// PublicKey returns the VAPID public key for the browser.
func (s *Sender) PublicKey() string { return s.publicKey }

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
		if err := s.store.MarkPushResult(ctx, sub.Endpoint, true, s.now().UnixMilli()); err != nil {
			s.log.Error("push: mark ok", "err", err)
		}
		return outcomeSent
	default:
		s.log.Warn("push: unexpected status", "status", resp.StatusCode)
		return s.recordFailure(ctx, sub)
	}
}

func (s *Sender) recordFailure(ctx context.Context, sub store.PushSubscription) outcome {
	if sub.FailCount+1 >= maxFailures {
		if _, err := s.store.DeletePushSubscription(ctx, sub.Endpoint); err == nil {
			return outcomeRemoved
		}
	}
	if err := s.store.MarkPushResult(ctx, sub.Endpoint, false, 0); err != nil && !errors.Is(err, context.Canceled) {
		s.log.Error("push: mark failure", "err", err)
	}
	return outcomeFailed
}
