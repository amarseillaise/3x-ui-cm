package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/xui"
)

var now = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

func TestStatus(t *testing.T) {
	week := 7 * Day
	cases := []struct {
		name string
		s    Subscription
		want Status
	}{
		{"unlimited", Subscription{Enabled: true}, StatusUnlimited},
		{"active", Subscription{Enabled: true, ExpiresAt: now.Add(30 * Day)}, StatusActive},
		{"expiring", Subscription{Enabled: true, ExpiresAt: now.Add(3 * Day)}, StatusExpiring},
		{"expiring boundary", Subscription{Enabled: true, ExpiresAt: now.Add(week)}, StatusExpiring},
		{"expired", Subscription{Enabled: true, ExpiresAt: now.Add(-time.Second)}, StatusExpired},
		{"expired wins over disabled", Subscription{Enabled: false, ExpiresAt: now.Add(-Day)}, StatusExpired},
		{"depleted", Subscription{Enabled: true, QuotaBytes: 100, UsedBytes: 100}, StatusDepleted},
		{"disabled", Subscription{Enabled: false, ExpiresAt: now.Add(30 * Day)}, StatusDisabled},
		{"quota only is active", Subscription{Enabled: true, QuotaBytes: 100, UsedBytes: 10}, StatusActive},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.s.Status(now, week); got != c.want {
				t.Errorf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestDaysLeft(t *testing.T) {
	if d := (Subscription{}).DaysLeft(now); d != -1 {
		t.Errorf("unlimited: %d", d)
	}
	if d := (Subscription{ExpiresAt: now.Add(-Day)}).DaysLeft(now); d != 0 {
		t.Errorf("expired: %d", d)
	}
	if d := (Subscription{ExpiresAt: now.Add(36 * time.Hour)}).DaysLeft(now); d != 2 {
		t.Errorf("1.5 days rounds up to 2, got %d", d)
	}
}

func TestDaysToAdd(t *testing.T) {
	cases := []struct {
		name    string
		expires time.Time
		want    int
		wantErr error
	}{
		{"active adds plan days", now.Add(10 * Day), 30, nil},
		{"expires exactly now", now, 30, nil},
		{"expired 1.5 days ago rounds up", now.Add(-36 * time.Hour), 32, nil},
		{"expired exactly 1 day ago", now.Add(-Day), 31, nil},
		{"unlimited", time.Time{}, 0, ErrUnlimited},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := DaysToAdd(30, c.expires, now)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got %d want %d", got, c.want)
			}
		})
	}
	if _, err := DaysToAdd(0, now.Add(Day), now); err == nil {
		t.Error("zero plan days must error")
	}
}

func TestFromRecords(t *testing.T) {
	recs := []xui.ClientRecord{
		{Email: "a", SubID: "s", Enable: false, ExpiryTime: TimeToMs(now.Add(5 * Day)), TotalGB: 100, InboundIDs: []int64{1, 2},
			Traffic: &xui.ClientTraffic{Up: 10, Down: 20, LastOnline: TimeToMs(now.Add(-time.Hour))}},
		{Email: "b", SubID: "s", Enable: true, ExpiryTime: TimeToMs(now.Add(2 * Day)), InboundIDs: []int64{3},
			Traffic: &xui.ClientTraffic{Up: 5, Down: 5, LastOnline: TimeToMs(now.Add(-time.Minute))}},
		{Email: "c", SubID: "s", Enable: true, ExpiryTime: -86400000},
	}
	s, ok := FromRecords(recs)
	if !ok {
		t.Fatal("expected ok")
	}
	if !s.Enabled || len(s.Emails) != 3 || s.InboundCount != 3 {
		t.Errorf("aggregate: %+v", s)
	}
	if !s.ExpiresAt.Equal(now.Add(2 * Day)) {
		t.Errorf("earliest expiry expected, got %v", s.ExpiresAt)
	}
	if s.QuotaBytes != 100 || s.UsedBytes != 40 {
		t.Errorf("traffic: quota=%d used=%d", s.QuotaBytes, s.UsedBytes)
	}
	if !s.LastOnline.Equal(now.Add(-time.Minute)) {
		t.Errorf("latest lastOnline expected, got %v", s.LastOnline)
	}
	if s.TrafficPercent() != 40 {
		t.Errorf("percent = %d", s.TrafficPercent())
	}
	if _, ok := FromRecords(nil); ok {
		t.Error("empty input must not be ok")
	}
}
