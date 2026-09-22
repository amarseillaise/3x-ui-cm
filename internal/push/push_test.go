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
	s := New(st, pub, priv, "mailto:test@example.com", nil)
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
	s := New(st, "", "", "", nil)
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
	s := New(st, pub, priv, "mailto:t@example.com", nil)
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
	s := New(nil, "pub", "priv", "mailto:admin@example.com", nil)
	if s.subject != "admin@example.com" {
		t.Errorf("subject = %q, want %q", s.subject, "admin@example.com")
	}
	s = New(nil, "pub", "priv", "https://cab.example.com", nil)
	if s.subject != "https://cab.example.com" {
		t.Errorf("https subject must pass through, got %q", s.subject)
	}
}
