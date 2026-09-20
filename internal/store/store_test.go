package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if n, err := s.Migrate(context.Background()); err != nil || n < 1 {
		t.Fatalf("migrate: n=%d err=%v", n, err)
	}
	if n, err := s.Migrate(context.Background()); err != nil || n != 0 {
		t.Fatalf("second migrate must be a no-op: n=%d err=%v", n, err)
	}
	return s
}

func TestSessions(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, Session{ID: "s1", Role: RoleClient, SubID: "sub1", CreatedAt: 1, LastSeenAt: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(ctx, "s1")
	if err != nil || got.SubID != "sub1" || got.Role != RoleClient {
		t.Fatalf("get: %+v %v", got, err)
	}
	if err := s.TouchSession(ctx, "s1", 5); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSession(ctx, "s1")
	if got.LastSeenAt != 5 {
		t.Errorf("touch failed: %+v", got)
	}
	if _, err := s.GetSession(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
	if n, _ := s.DeleteSessionsIdleBefore(ctx, 10); n != 1 {
		t.Errorf("idle cleanup deleted %d", n)
	}
}

func TestRenewalPendingUniqueness(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	r := &RenewalRequest{SubID: "sub1", Email: "u1", PlanID: "p30", Days: 30, Amount: 180, Currency: "RUB", Status: RenewalPending, AppliedDays: 30, ExpiryBefore: 100, CreatedAt: 1}
	if err := s.CreateRenewal(ctx, r); err != nil || r.ID == 0 {
		t.Fatalf("create: id=%d err=%v", r.ID, err)
	}
	dup := *r
	dup.ID = 0
	if err := s.CreateRenewal(ctx, &dup); !errors.Is(err, ErrPendingExists) {
		t.Fatalf("want ErrPendingExists, got %v", err)
	}
	if ok, _ := s.HasPendingRenewal(ctx, "sub1"); !ok {
		t.Error("HasPendingRenewal should be true")
	}
	if err := s.ResolveRenewal(ctx, r.ID, RenewalConfirmed, "admin", 2, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveRenewal(ctx, r.ID, RenewalRejected, "admin", 3, 0); !errors.Is(err, ErrNotPending) {
		t.Errorf("second resolve must fail with ErrNotPending, got %v", err)
	}
	// After resolution a new pending request is allowed again.
	dup.ID = 0
	if err := s.CreateRenewal(ctx, &dup); err != nil {
		t.Fatalf("new pending after resolve: %v", err)
	}
	list, err := s.ListRenewals(ctx, RenewalPending, 10)
	if err != nil || len(list) != 1 || list[0].ID != dup.ID {
		t.Errorf("list pending: %+v %v", list, err)
	}
	counts, _ := s.CountRenewals(ctx)
	if counts[RenewalConfirmed] != 1 || counts[RenewalPending] != 1 {
		t.Errorf("counts: %v", counts)
	}
}

func TestRenewalFailedDoesNotBlock(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	r := &RenewalRequest{SubID: "sub1", Email: "u1", PlanID: "p30", Days: 30, Amount: 180, Currency: "RUB", Status: RenewalPending, CreatedAt: 1}
	if err := s.CreateRenewal(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRenewalOutcome(ctx, r.ID, RenewalFailed, 0, "panel down"); err != nil {
		t.Fatal(err)
	}
	r2 := &RenewalRequest{SubID: "sub1", Email: "u1", PlanID: "p30", Days: 30, Amount: 180, Currency: "RUB", Status: RenewalPending, CreatedAt: 2}
	if err := s.CreateRenewal(ctx, r2); err != nil {
		t.Fatalf("failed request must not block a retry: %v", err)
	}
}

func TestNotificationDedup(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	n := Notification{SubID: "sub1", Kind: "expiry_3d", Ref: "123", SentAt: 1}
	if ins, err := s.RecordNotification(ctx, n); err != nil || !ins {
		t.Fatalf("first insert: %v %v", ins, err)
	}
	if ins, err := s.RecordNotification(ctx, n); err != nil || ins {
		t.Fatalf("duplicate must be ignored: %v %v", ins, err)
	}
	n.Ref = "456"
	if ins, _ := s.RecordNotification(ctx, n); !ins {
		t.Error("different ref must insert")
	}
	// Admin/global notifications use an empty sub_id and must dedup too.
	g := Notification{Kind: "admin_pending_reminder", Ref: "1:2026-09-16", SentAt: 1}
	if ins, _ := s.RecordNotification(ctx, g); !ins {
		t.Error("global first insert")
	}
	if ins, _ := s.RecordNotification(ctx, g); ins {
		t.Error("global duplicate must be ignored")
	}
}

func TestPushSubscriptions(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	for _, p := range []PushSubscription{
		{Endpoint: "e1", Role: RoleClient, SubID: "sub1", P256dh: "k", Auth: "a", CreatedAt: 1},
		{Endpoint: "e2", Role: RoleClient, SubID: "sub2", P256dh: "k", Auth: "a", CreatedAt: 2},
		{Endpoint: "e3", Role: RoleAdmin, P256dh: "k", Auth: "a", CreatedAt: 3},
	} {
		if err := s.UpsertPushSubscription(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpsertPushSubscription(ctx, PushSubscription{Endpoint: "e1", Role: RoleClient, SubID: "sub1", P256dh: "k2", Auth: "a2", CreatedAt: 9}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListPushSubscriptions(ctx, "", nil)
	if len(all) != 3 {
		t.Fatalf("all: %d", len(all))
	}
	admins, _ := s.ListPushSubscriptions(ctx, RoleAdmin, nil)
	if len(admins) != 1 || admins[0].Endpoint != "e3" {
		t.Errorf("admins: %+v", admins)
	}
	some, _ := s.ListPushSubscriptions(ctx, RoleClient, []string{"sub1"})
	if len(some) != 1 || some[0].P256dh != "k2" {
		t.Errorf("by sub: %+v", some)
	}
	none, _ := s.ListPushSubscriptions(ctx, RoleClient, []string{})
	if len(none) != 0 {
		t.Errorf("empty id list must return nothing, got %d", len(none))
	}
	if err := s.MarkPushResult(ctx, "e1", false, 0); err != nil {
		t.Fatal(err)
	}
	some, _ = s.ListPushSubscriptions(ctx, "", []string{"sub1"})
	if some[0].FailCount != 1 {
		t.Errorf("fail_count: %d", some[0].FailCount)
	}
	if ok, _ := s.DeletePushSubscription(ctx, "e2"); !ok {
		t.Error("delete should report true")
	}
	if ok, _ := s.HasPushSubscription(ctx, "sub2"); ok {
		t.Error("sub2 should have no subscriptions")
	}
	counts, _ := s.CountPushSubscriptions(ctx)
	if counts[RoleClient] != 1 || counts[RoleAdmin] != 1 {
		t.Errorf("counts: %v", counts)
	}
}
