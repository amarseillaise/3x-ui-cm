// Package domain holds pure business rules: subscription status, renewal math
// and notification thresholds. No I/O.
package domain

import (
	"errors"
	"math"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/xui"
)

// Status of a subscription as shown to the client.
type Status string

// Status values.
const (
	StatusUnlimited Status = "unlimited"
	StatusActive    Status = "active"
	StatusExpiring  Status = "expiring"
	StatusExpired   Status = "expired"
	StatusDepleted  Status = "depleted"
	StatusDisabled  Status = "disabled"
)

// Day is the unit the panel uses for bulkAdjust.
const Day = 24 * time.Hour

// ErrUnlimited is returned when renewal math is applied to a subscription without expiry.
var ErrUnlimited = errors.New("domain: subscription has no expiry")

// Subscription aggregates every panel client sharing one subId.
type Subscription struct {
	SubID        string
	Emails       []string
	Enabled      bool
	ExpiresAt    time.Time // zero = unlimited (or not started yet)
	QuotaBytes   int64     // 0 = unlimited
	UsedBytes    int64
	LastOnline   time.Time
	InboundCount int
}

// MsToTime converts panel Unix milliseconds; non-positive values map to zero time.
func MsToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// TimeToMs converts to panel Unix milliseconds; zero time maps to 0.
func TimeToMs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// FromRecords aggregates panel records: earliest expiry, summed traffic,
// enabled if any record is enabled.
func FromRecords(records []xui.ClientRecord) (Subscription, bool) {
	if len(records) == 0 {
		return Subscription{}, false
	}
	s := Subscription{SubID: records[0].SubID}
	for _, r := range records {
		s.Emails = append(s.Emails, r.Email)
		if r.Enable {
			s.Enabled = true
		}
		if t := MsToTime(r.ExpiryTime); !t.IsZero() && (s.ExpiresAt.IsZero() || t.Before(s.ExpiresAt)) {
			s.ExpiresAt = t
		}
		if r.TotalGB > 0 {
			s.QuotaBytes += r.TotalGB
		}
		s.UsedBytes += r.Used()
		if r.Traffic != nil {
			if t := MsToTime(r.Traffic.LastOnline); t.After(s.LastOnline) {
				s.LastOnline = t
			}
		}
		s.InboundCount += len(r.InboundIDs)
	}
	return s, true
}

// Unlimited reports whether the subscription has no expiry date.
func (s Subscription) Unlimited() bool { return s.ExpiresAt.IsZero() }

// Expired reports whether the expiry date has passed.
func (s Subscription) Expired(now time.Time) bool {
	return !s.ExpiresAt.IsZero() && !s.ExpiresAt.After(now)
}

// Depleted reports whether the traffic quota is used up.
func (s Subscription) Depleted() bool {
	return s.QuotaBytes > 0 && s.UsedBytes >= s.QuotaBytes
}

// DaysLeft returns whole days until expiry rounded up, 0 when expired, -1 when unlimited.
func (s Subscription) DaysLeft(now time.Time) int {
	if s.ExpiresAt.IsZero() {
		return -1
	}
	d := s.ExpiresAt.Sub(now)
	if d <= 0 {
		return 0
	}
	return int(math.Ceil(float64(d) / float64(Day)))
}

// Status derives the display status. expiringWithin is the "soon" window.
func (s Subscription) Status(now time.Time, expiringWithin time.Duration) Status {
	switch {
	case s.Expired(now):
		return StatusExpired
	case s.Depleted():
		return StatusDepleted
	case !s.Enabled:
		return StatusDisabled
	case s.ExpiresAt.IsZero() && s.QuotaBytes == 0:
		return StatusUnlimited
	case !s.ExpiresAt.IsZero() && s.ExpiresAt.Sub(now) <= expiringWithin:
		return StatusExpiring
	default:
		return StatusActive
	}
}

// DaysToAdd returns the addDays value for the panel so that the subscription
// gains planDays counted from max(now, expiresAt). The panel shifts expiryTime
// by whole days from its current value, so an overdue subscription gets the
// overdue part added and rounded up: the client never receives less than paid.
func DaysToAdd(planDays int, expiresAt, now time.Time) (int, error) {
	if planDays <= 0 {
		return 0, errors.New("domain: planDays must be positive")
	}
	if expiresAt.IsZero() {
		return 0, ErrUnlimited
	}
	if expiresAt.After(now) {
		return planDays, nil
	}
	overdue := now.Sub(expiresAt)
	return planDays + int(math.Ceil(float64(overdue)/float64(Day))), nil
}

// TrafficPercent returns used quota in percent (0 when unlimited).
func (s Subscription) TrafficPercent() int {
	if s.QuotaBytes <= 0 {
		return 0
	}
	p := int(math.Round(float64(s.UsedBytes) * 100 / float64(s.QuotaBytes)))
	if p > 100 {
		p = 100
	}
	return p
}
