package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

// browserKeys mimics PushSubscription.getKey('p256dh') / getKey('auth').
func browserKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := make([]byte, 16)
	_, _ = rand.Read(a)
	return base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(a)
}

func TestSendToPrunesAndCounts(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	var calls int32
	pushSvc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("Authorization") == "" {
			t.Errorf("missing web push headers: %v", r.Header)
		}
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusCreated)
		case "/gone":
			w.WriteHeader(http.StatusGone)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(pushSvc.Close)

	pub, priv, err := GenerateVAPID()
	if err != nil {
		t.Fatal(err)
	}
	p256dh, auth := browserKeys(t)
	for _, ep := range []string{"/ok", "/gone", "/err"} {
		if err := st.UpsertPushSubscription(ctx, store.PushSubscription{Endpoint: pushSvc.URL + ep, Role: store.RoleClient, SubID: "sub1", P256dh: p256dh, Auth: auth, CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	s := New(st, Config{PublicKey: pub, PrivateKey: priv, Subject: "mailto:test@example.com"}, nil)
	res := s.SendToSubID(ctx, "sub1", Message{Title: "Hi", Body: "There", URL: "/"})
	if res.Sent != 1 || res.Removed != 1 || res.Failed != 1 {
		t.Fatalf("result = %+v", res)
	}
	if calls != 3 {
		t.Errorf("calls = %d", calls)
	}
	left, _ := st.ListPushSubscriptions(ctx, "", []string{"sub1"})
	if len(left) != 2 {
		t.Fatalf("gone endpoint must be deleted, left %d", len(left))
	}
	for _, sub := range left {
		switch sub.Endpoint {
		case pushSvc.URL + "/ok":
			if sub.FailCount != 0 || sub.LastOkAt == 0 {
				t.Errorf("ok endpoint not marked: %+v", sub)
			}
		case pushSvc.URL + "/err":
			if sub.FailCount != 1 {
				t.Errorf("failed endpoint fail_count = %d", sub.FailCount)
			}
		}
	}
}

func TestDisabledSenderDropsMessages(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	_ = st.UpsertPushSubscription(ctx, store.PushSubscription{Endpoint: "http://127.0.0.1:1/x", Role: store.RoleAdmin, P256dh: "k", Auth: "a", CreatedAt: 1})
	s := New(st, Config{}, nil)
	if s.Enabled() {
		t.Fatal("must be disabled")
	}
	if res := s.SendToAdmins(ctx, Message{Title: "x"}); res.Failed != 1 || res.Sent != 0 {
		t.Errorf("result = %+v", res)
	}
}

func TestFailureLimitRemovesEndpoint(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	pushSvc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	t.Cleanup(pushSvc.Close)
	pub, priv, _ := GenerateVAPID()
	p256dh, auth := browserKeys(t)
	_ = st.UpsertPushSubscription(ctx, store.PushSubscription{Endpoint: pushSvc.URL + "/x", Role: store.RoleClient, SubID: "s", P256dh: p256dh, Auth: auth, CreatedAt: 1})
	s := New(st, Config{PublicKey: pub, PrivateKey: priv, Subject: "mailto:t@example.com"}, nil)
	var last Result
	for i := 0; i < maxFailures; i++ {
		last = s.SendToSubID(ctx, "s", Message{Title: "x"})
	}
	if last.Removed != 1 {
		t.Errorf("endpoint must be removed after %d failures: %+v", maxFailures, last)
	}
	if left, _ := st.ListPushSubscriptions(ctx, "", []string{"s"}); len(left) != 0 {
		t.Errorf("still stored: %d", len(left))
	}
}

func TestNormalizeSubject(t *testing.T) {
	cases := map[string]string{
		"mailto:a@b.test":      "a@b.test",
		"MAILTO:a@b.test":      "a@b.test",
		"  mailto: a@b.test  ": "a@b.test",
		"a@b.test":             "a@b.test",
		"https://cab.test":     "https://cab.test",
		"":                     "",
	}
	for in, want := range cases {
		if got := normalizeSubject(in); got != want {
			t.Errorf("normalizeSubject(%q) = %q, want %q", in, got, want)
		}
	}
}

// The library prepends "mailto:" to anything that is not an https URL, so the
// stored subject must never carry the prefix itself.
func TestSenderSubjectHasNoMailtoPrefix(t *testing.T) {
	s := New(nil, Config{PublicKey: "pub", PrivateKey: "priv", Subject: "mailto:admin@example.com"}, nil)
	if s.subject != "admin@example.com" {
		t.Errorf("subject = %q, want %q", s.subject, "admin@example.com")
	}
	s = New(nil, Config{PublicKey: "pub", PrivateKey: "priv", Subject: "https://cab.example.com"}, nil)
	if s.subject != "https://cab.example.com" {
		t.Errorf("https subject must pass through, got %q", s.subject)
	}
}

func TestAbsoluteURL(t *testing.T) {
	s := New(nil, Config{BaseURL: "https://cab.test:13671/"}, nil)
	cases := map[string]string{
		"/renew":                 "https://cab.test:13671/renew",
		"":                       "https://cab.test:13671/",
		"/":                      "https://cab.test:13671/",
		"admin":                  "https://cab.test:13671/admin",
		"https://other.test/x?y": "https://other.test/x?y",
		"http://legacy.test/":    "http://legacy.test/",
	}
	for in, want := range cases {
		if got := s.absoluteURL(in); got != want {
			t.Errorf("absoluteURL(%q) = %q, want %q", in, got, want)
		}
	}
	if got := New(nil, Config{}, nil).absoluteURL("/renew"); got != "/renew" {
		t.Errorf("without a base URL links must stay as they are, got %q", got)
	}
}

// silenceRig is a push service that accepts everything and a sender whose
// clock the test moves, so two weeks of silence take no time at all.
type silenceRig struct {
	st    *store.Store
	s     *Sender
	clock time.Time
	ep    string
}

func newSilenceRig(t *testing.T) *silenceRig {
	t.Helper()
	st := newStore(t)
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusCreated) }))
	t.Cleanup(svc.Close)
	pub, priv, _ := GenerateVAPID()
	p256dh, auth := browserKeys(t)
	r := &silenceRig{st: st, clock: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), ep: svc.URL + "/web-app"}
	if err := st.UpsertPushSubscription(context.Background(), store.PushSubscription{Endpoint: r.ep, Role: store.RoleClient, SubID: "s", P256dh: p256dh, Auth: auth, CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	r.s = New(st, Config{PublicKey: pub, PrivateKey: priv, Subject: "t@example.com"}, nil)
	r.s.now = func() time.Time { return r.clock }
	return r
}

func (r *silenceRig) sendAfter(d time.Duration) Result {
	r.clock = r.clock.Add(d)
	return r.s.SendToSubID(context.Background(), "s", Message{Title: "x"})
}

func (r *silenceRig) stored() bool {
	left, _ := r.st.ListPushSubscriptions(context.Background(), "", []string{"s"})
	return len(left) == 1
}

// The iPhone case: Apple keeps answering 2xx for a removed web app, so only
// the missing receipts reveal it.
func TestSilentEndpointIsDropped(t *testing.T) {
	r := newSilenceRig(t)
	r.sendAfter(0)
	r.sendAfter(24 * time.Hour)
	res := r.sendAfter(14 * 24 * time.Hour)
	if res.Removed != 1 || res.Sent != 0 {
		t.Errorf("third unconfirmed message after two weeks must drop the endpoint: %+v", res)
	}
	if r.stored() {
		t.Error("silent endpoint is still stored")
	}
}

// A phone that is off for a few days must keep its subscription, however many
// messages it missed.
func TestShortSilenceKeepsEndpoint(t *testing.T) {
	r := newSilenceRig(t)
	for i := 0; i < 10; i++ {
		if res := r.sendAfter(30 * time.Hour); res.Removed != 0 {
			t.Fatalf("dropped after %d messages over %v", i+1, time.Duration(i+1)*30*time.Hour)
		}
	}
	if !r.stored() {
		t.Error("endpoint lost during a silence shorter than the grace period")
	}
}

// Two long gaps with a single message each are not enough on their own.
func TestFewMessagesKeepEndpoint(t *testing.T) {
	r := newSilenceRig(t)
	r.sendAfter(0)
	if res := r.sendAfter(30 * 24 * time.Hour); res.Removed != 0 {
		t.Errorf("two unconfirmed messages must not drop an endpoint: %+v", res)
	}
	if !r.stored() {
		t.Error("endpoint dropped below the message limit")
	}
}

// A device that confirms keeps its subscription indefinitely.
func TestReceiptsKeepEndpoint(t *testing.T) {
	r := newSilenceRig(t)
	for i := 0; i < 6; i++ {
		if res := r.sendAfter(10 * 24 * time.Hour); res.Removed != 0 {
			t.Fatalf("confirmed endpoint dropped at message %d", i+1)
		}
		if known, err := r.s.Receipt(context.Background(), r.ep); err != nil || !known {
			t.Fatalf("receipt: known=%v err=%v", known, err)
		}
	}
	if !r.stored() {
		t.Error("a device that confirms every message must stay")
	}
}
