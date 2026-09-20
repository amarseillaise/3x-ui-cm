package httpapi

import (
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/domain"
)

// meResponse is the payload of GET /api/me.
type meResponse struct {
	Subscription subscriptionDTO `json:"subscription"`
	Renewal      renewalDTO      `json:"renewal"`
	Push         pushDTO         `json:"push"`
}

type subscriptionDTO struct {
	Status          domain.Status `json:"status"`
	Enabled         bool          `json:"enabled"`
	Unlimited       bool          `json:"unlimited"`
	ExpiresAt       *time.Time    `json:"expiresAt"`
	DaysLeft        int           `json:"daysLeft"`
	QuotaBytes      int64         `json:"quotaBytes"`
	UsedBytes       int64         `json:"usedBytes"`
	TrafficPercent  int           `json:"trafficPercent"`
	Online          bool          `json:"online"`
	LastOnline      *time.Time    `json:"lastOnline"`
	SubscriptionURL string        `json:"subscriptionUrl"`
	InboundCount    int           `json:"inboundCount"`
}

type renewalDTO struct {
	Enabled    bool `json:"enabled"`
	HasPending bool `json:"hasPending"`
	CanRenew   bool `json:"canRenew"`
}

type pushDTO struct {
	Enabled        bool   `json:"enabled"`
	VAPIDPublicKey string `json:"vapidPublicKey"`
	Subscribed     bool   `json:"subscribed"`
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
