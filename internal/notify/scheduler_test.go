package notify

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/push"
	"github.com/amarseillaise/3x-ui-cm/internal/store"
	"github.com/amarseillaise/3x-ui-cm/internal/xui"
)

type harness struct {
	sched   *Scheduler
	store   *store.Store
	now     time.Time
	expiry  *int64
	quota   *int64
	used    *int64
	mu      sync.Mutex
	pushes  []string // titles received by the fake push service
	panelMu sync.Mutex
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{now: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}
	exp, quota, used := h.now.Add(2*24*time.Hour).UnixMilli(), int64(0), int64(0)
	h.expiry, h.quota, h.used = &exp, &quota, &used

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.store = st

	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.panelMu.Lock()
		defer h.panelMu.Unlock()
		rec := map[string]any{"email": "alice", "subId": "sub1", "enable": true, "expiryTime": *h.expiry, "totalGB": *h.quota, "inboundIds": []int{1},
			"traffic": map[string]any{"up": *h.used, "down": 0, "expiryTime": *h.expiry}}
		body, _ := json.Marshal(map[string]any{"success": true, "msg": "", "obj": []any{rec}})
		_, _ = w.Write(body)
	}))
	t.Cleanup(panel.Close)

	pushSvc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		h.mu.Lock()
		h.pushes = append(h.pushes, r.URL.Path)
		h.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushSvc.Close)

	priv, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	keys := store.PushSubscription{P256dh: base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()), Auth: base64.RawURLEncoding.EncodeToString(auth), CreatedAt: 1}
	client := keys
	client.Endpoint, client.Role, client.SubID = pushSvc.URL+"/client", store.RoleClient, "sub1"
	admin := keys
	admin.Endpoint, admin.Role = pushSvc.URL+"/admin", store.RoleAdmin
	for _, p := range []store.PushSubscription{client, admin} {
		if err := st.UpsertPushSubscription(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}

	pub, privKey, _ := push.GenerateVAPID()
	sender := push.New(st, pub, privKey, "mailto:t@example.com", nil)
	x, _ := xui.New(panel.URL, "tok", xui.WithCacheTTL(0))
	h.sched = New(st, x, sender, time.Minute, []int{7, 3, 1}, 90, nil)
	h.sched.now = func() time.Time { return h.now }
	return h
}

func (h *harness) run(t *testing.T) Stats {
	t.Helper()
	st, err := h.sched.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func (h *harness) pushCount(path string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, p := range h.pushes {
		if p == path {
			n++
		}
	}
	return n
}

func TestExpiryThresholdsFireOnceEach(t *testing.T) {
	h := newHarness(t)
	// 2 days left with thresholds 7,3,1 → only the 3-day reminder, once.
	if st := h.run(t); st.Sent != 1 {
		t.Fatalf("first pass sent %d", st.Sent)
	}
	if st := h.run(t); st.Sent != 0 {
		t.Fatalf("second pass must be deduplicated, sent %d", st.Sent)
	}
	if h.pushCount("/client") != 1 {
		t.Fatalf("client pushes = %d", h.pushCount("/client"))
	}
	// Next day: 1 day left → 1-day reminder.
	h.now = h.now.Add(24 * time.Hour)
	if st := h.run(t); st.Sent != 1 {
		t.Fatalf("1-day reminder sent %d", st.Sent)
	}
	// Expired → one more.
	h.now = h.now.Add(48 * time.Hour)
	if st := h.run(t); st.Sent != 1 {
		t.Fatalf("expired notice sent %d", st.Sent)
	}
	h.run(t)
	if h.pushCount("/client") != 3 {
		t.Fatalf("client pushes = %d, want 3", h.pushCount("/client"))
	}
	// Renewal changes expiry → the cycle starts over with a new ref.
	h.panelMu.Lock()
	*h.expiry = h.now.Add(2 * 24 * time.Hour).UnixMilli()
	h.panelMu.Unlock()
	if st := h.run(t); st.Sent != 1 {
		t.Fatalf("after renewal sent %d", st.Sent)
	}
}

func TestUnlimitedAndNoPushSubscriptionAreSkipped(t *testing.T) {
	h := newHarness(t)
	h.panelMu.Lock()
	*h.expiry = 0
	h.panelMu.Unlock()
	if st := h.run(t); st.Sent != 0 || st.Subscriptions != 1 {
		t.Fatalf("unlimited: %+v", st)
	}
	h.panelMu.Lock()
	*h.expiry = h.now.Add(-time.Hour).UnixMilli()
	h.panelMu.Unlock()
	if _, err := h.store.DeletePushSubscription(context.Background(), h.pushEndpoint(t, store.RoleClient)); err != nil {
		t.Fatal(err)
	}
	if st := h.run(t); st.Sent != 0 {
		t.Fatalf("no subscription: sent %d", st.Sent)
	}
	if sent, _ := h.store.NotificationSent(context.Background(), "sub1", "expired", "0"); sent {
		t.Error("nothing must be recorded without a push subscription")
	}
}

func (h *harness) pushEndpoint(t *testing.T, role string) string {
	t.Helper()
	subs, err := h.store.ListPushSubscriptions(context.Background(), role, nil)
	if err != nil || len(subs) == 0 {
		t.Fatalf("no %s subscription: %v", role, err)
	}
	return subs[0].Endpoint
}

func TestTrafficWarnings(t *testing.T) {
	h := newHarness(t)
	h.panelMu.Lock()
	*h.expiry = 0
	*h.quota = 1000
	*h.used = 950
	h.panelMu.Unlock()
	if st := h.run(t); st.Sent != 1 {
		t.Fatalf("traffic_high sent %d", st.Sent)
	}
	h.panelMu.Lock()
	*h.used = 1000
	h.panelMu.Unlock()
	if st := h.run(t); st.Sent != 1 {
		t.Fatalf("depleted sent %d", st.Sent)
	}
	if st := h.run(t); st.Sent != 0 {
		t.Fatalf("dedup: sent %d", st.Sent)
	}
	// A new month (after a monthly reset) allows the warning again.
	h.now = h.now.AddDate(0, 1, 0)
	if st := h.run(t); st.Sent != 1 {
		t.Fatalf("next month sent %d", st.Sent)
	}
}

func TestAdminReminderOncePerDay(t *testing.T) {
	h := newHarness(t)
	h.panelMu.Lock()
	*h.expiry = 0
	h.panelMu.Unlock()
	ctx := context.Background()
	req := &store.RenewalRequest{SubID: "sub1", Email: "alice", PlanID: "p30", Days: 30, Amount: 180, Currency: "RUB", Status: store.RenewalPending, CreatedAt: h.now.Add(-2 * time.Hour).UnixMilli()}
	if err := h.store.CreateRenewal(ctx, req); err != nil {
		t.Fatal(err)
	}
	if st := h.run(t); st.Reminders != 0 {
		t.Fatalf("too early for a reminder: %+v", st)
	}
	h.now = h.now.Add(25 * time.Hour)
	if st := h.run(t); st.Reminders != 1 {
		t.Fatalf("reminder expected: %+v", st)
	}
	if st := h.run(t); st.Reminders != 0 {
		t.Fatalf("same day dedup: %+v", st)
	}
	h.now = h.now.Add(24 * time.Hour)
	if st := h.run(t); st.Reminders != 1 {
		t.Fatalf("next day reminder: %+v", st)
	}
	if h.pushCount("/admin") != 2 {
		t.Errorf("admin pushes = %d", h.pushCount("/admin"))
	}
	if err := h.store.ResolveRenewal(ctx, req.ID, store.RenewalConfirmed, "admin", h.now.UnixMilli(), 0); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(24 * time.Hour)
	if st := h.run(t); st.Reminders != 0 {
		t.Fatalf("resolved request must not remind: %+v", st)
	}
}
