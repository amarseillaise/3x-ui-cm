package xui

import "encoding/json"

// ClientTraffic mirrors the panel's ClientTraffic object. Counters are bytes,
// timestamps are Unix milliseconds, 0 means unlimited / never.
type ClientTraffic struct {
	InboundID    int64  `json:"inboundId"`
	Enable       bool   `json:"enable"`
	Email        string `json:"email"`
	SubID        string `json:"subId"`
	Up           int64  `json:"up"`
	Down         int64  `json:"down"`
	ExpiryTime   int64  `json:"expiryTime"`
	Total        int64  `json:"total"`
	Reset        int    `json:"reset"`
	LastOnline   int64  `json:"lastOnline"`
	LastSubFetch int64  `json:"lastSubFetch"`
}

// ClientRecord is a client as returned by /clients/list (full) and
// /clients/list/paged (slim). Secret fields (uuid, password, keys) are
// deliberately not decoded.
type ClientRecord struct {
	Email      string         `json:"email"`
	SubID      string         `json:"subId"`
	Enable     bool           `json:"enable"`
	ExpiryTime int64          `json:"expiryTime"` // Unix ms; 0 = unlimited; <0 = starts on first use
	TotalGB    int64          `json:"totalGB"`    // bytes despite the name; 0 = unlimited
	LimitIP    int            `json:"limitIp"`
	TgID       int64          `json:"tgId"`
	Group      string         `json:"group"`
	Comment    string         `json:"comment"`
	Reset      int            `json:"reset"`
	CreatedAt  int64          `json:"createdAt"`
	UpdatedAt  int64          `json:"updatedAt"`
	InboundIDs []int64        `json:"inboundIds"`
	Traffic    *ClientTraffic `json:"traffic"`
}

// Used returns up+down bytes.
func (r ClientRecord) Used() int64 {
	if r.Traffic == nil {
		return 0
	}
	return r.Traffic.Up + r.Traffic.Down
}

// ClientPage is the /clients/list/paged response.
type ClientPage struct {
	Items    []ClientRecord `json:"items"`
	Total    int            `json:"total"`
	Filtered int            `json:"filtered"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

// ClientDetail is the /clients/get/{email} response.
type ClientDetail struct {
	Client      ClientRecord `json:"client"`
	InboundIDs  []int64      `json:"inboundIds"`
	UsedTraffic int64        `json:"usedTraffic"`
}

// ServerStatus is the subset of /server/status the app uses.
type ServerStatus struct {
	PanelVersion string `json:"panelVersion"`
	Uptime       int64  `json:"uptime"`
	Xray         struct {
		State    string `json:"state"`
		Version  string `json:"version"`
		ErrorMsg string `json:"errorMsg"`
	} `json:"xray"`
}

// BulkAdjustRequest shifts expiry and/or quota of the given clients.
type BulkAdjustRequest struct {
	Emails   []string `json:"emails"`
	AddDays  int      `json:"addDays,omitempty"`
	AddBytes int64    `json:"addBytes,omitempty"`
}

// BulkAdjustResult is decoded best-effort; Raw always holds the panel's obj.
type BulkAdjustResult struct {
	Adjusted int               `json:"adjusted"`
	Skipped  map[string]string `json:"skipped"`
	Raw      json.RawMessage   `json:"-"`
}

// Settings is the subset of /setting/all needed to build subscription URLs.
type Settings struct {
	SubEnable   bool   `json:"subEnable"`
	SubListen   string `json:"subListen"`
	SubPort     int    `json:"subPort"`
	SubPath     string `json:"subPath"`
	SubDomain   string `json:"subDomain"`
	SubURI      string `json:"subURI"`
	SubCertFile string `json:"subCertFile"`
	SubJSONPath string `json:"subJsonPath"`
	SubJSONURI  string `json:"subJsonURI"`
}
