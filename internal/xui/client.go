// Package xui is a minimal client for the 3x-ui master panel API (v3.x).
// Only the endpoints this project needs are implemented; destructive panel
// endpoints are intentionally absent.
package xui

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	connectTimeout  = 5 * time.Second
	requestTimeout  = 20 * time.Second
	defaultCacheTTL = 60 * time.Second
	onlinesTTL      = 30 * time.Second
	retryDelay      = 500 * time.Millisecond
	maxBodyBytes    = 64 << 20
)

// ErrNotFound is returned when a lookup matches nothing.
var ErrNotFound = errors.New("xui: not found")

// APIError is returned when the panel answers success=false or rejects auth.
type APIError struct {
	Path       string
	Msg        string
	HTTPStatus int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("xui: %s: %s (http %d)", e.Path, e.Msg, e.HTTPStatus)
}

// transportError marks failures worth one retry on idempotent calls.
type transportError struct{ err error }

func (e *transportError) Error() string { return "xui: transport: " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

type envelope struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

// Client talks to one master panel.
type Client struct {
	base     string
	token    string
	insecure bool
	http     *http.Client
	log      *slog.Logger
	cacheTTL time.Duration
	now      func() time.Time

	mu        sync.Mutex
	cache     []ClientRecord
	cacheAt   time.Time
	onlines   []string
	onlinesAt time.Time
}

// Option configures the client.
type Option func(*Client)

// WithInsecureTLS disables certificate verification (self-signed panels only).
func WithInsecureTLS(insecure bool) Option { return func(c *Client) { c.insecure = insecure } }

// WithHTTPClient replaces the HTTP client (tests).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(c *Client) { c.log = l } }

// WithCacheTTL sets how long ListClients results are cached.
func WithCacheTTL(d time.Duration) Option { return func(c *Client) { c.cacheTTL = d } }

// New creates a client for baseURL (panel root including the web base path).
func New(baseURL, token string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("xui: empty API token")
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("xui: invalid base URL %q", baseURL)
	}
	c := &Client{
		base:     strings.TrimRight(baseURL, "/"),
		token:    token,
		log:      slog.Default(),
		cacheTTL: defaultCacheTTL,
		now:      time.Now,
	}
	for _, o := range opts {
		o(c)
	}
	if c.http == nil {
		c.http = newHTTPClient(c.insecure)
	}
	return c, nil
}

func newHTTPClient(insecure bool) *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   connectTimeout,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: insecure, MinVersion: tls.VersionTLS12}, //nolint:gosec // opt-in for self-signed panels
	}
	return &http.Client{
		Transport: tr,
		Timeout:   requestTimeout,
		// A wrong token makes the panel redirect to its login page; surface that instead of following.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// ServerStatus returns panel and Xray state.
func (c *Client) ServerStatus(ctx context.Context) (*ServerStatus, error) {
	var s ServerStatus
	if err := c.get(ctx, "/panel/api/server/status", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ListClients returns every client with traffic. Results are cached for the
// configured TTL and must be treated as read-only by callers.
func (c *Client) ListClients(ctx context.Context) ([]ClientRecord, error) {
	c.mu.Lock()
	if c.cache != nil && c.now().Sub(c.cacheAt) < c.cacheTTL {
		out := c.cache
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	var list []ClientRecord
	if err := c.get(ctx, "/panel/api/clients/list", nil, &list); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cache = list
	c.cacheAt = c.now()
	c.mu.Unlock()
	return list, nil
}

// InvalidateCache drops cached lists (call after writes).
func (c *Client) InvalidateCache() {
	c.mu.Lock()
	c.cache = nil
	c.onlines = nil
	c.mu.Unlock()
}

// ClientsBySubID returns the cached clients that share a subscription id.
func (c *Client) ClientsBySubID(ctx context.Context, subID string) ([]ClientRecord, error) {
	if subID == "" {
		return nil, ErrNotFound
	}
	list, err := c.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	var out []ClientRecord
	for _, r := range list {
		if r.SubID == subID {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// FindBySubID looks a subscription id up on the panel (uncached). The panel's
// search is a substring match, so results are filtered to exact matches.
func (c *Client) FindBySubID(ctx context.Context, subID string) ([]ClientRecord, error) {
	if subID == "" {
		return nil, ErrNotFound
	}
	q := url.Values{"search": {subID}, "pageSize": {"200"}}
	var page ClientPage
	if err := c.get(ctx, "/panel/api/clients/list/paged", q, &page); err != nil {
		return nil, err
	}
	var out []ClientRecord
	for _, it := range page.Items {
		if it.SubID == subID {
			out = append(out, it)
		}
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// GetClient fetches one client by email (uncached).
func (c *Client) GetClient(ctx context.Context, email string) (*ClientDetail, error) {
	var d ClientDetail
	if err := c.get(ctx, "/panel/api/clients/get/"+url.PathEscape(email), nil, &d); err != nil {
		return nil, err
	}
	if d.Client.Email == "" {
		return nil, ErrNotFound
	}
	return &d, nil
}

// Onlines returns emails of clients seen within the heartbeat window. Cached
// for a short TTL because every /api/me call needs it.
func (c *Client) Onlines(ctx context.Context) ([]string, error) {
	c.mu.Lock()
	if c.onlines != nil && c.now().Sub(c.onlinesAt) < onlinesTTL {
		out := c.onlines
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()
	out := []string{}
	if err := c.post(ctx, "/panel/api/clients/onlines", nil, &out, true); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.onlines = out
	c.onlinesAt = c.now()
	c.mu.Unlock()
	return out, nil
}

// LastOnline maps email to last-seen Unix ms.
func (c *Client) LastOnline(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	if err := c.post(ctx, "/panel/api/clients/lastOnline", nil, &out, true); err != nil {
		return nil, err
	}
	return out, nil
}

// SubLinks returns protocol URLs (vless://...) for a subscription id.
func (c *Client) SubLinks(ctx context.Context, subID string) ([]string, error) {
	var out []string
	if err := c.get(ctx, "/panel/api/clients/subLinks/"+url.PathEscape(subID), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Settings returns panel settings (subscription URL parts).
func (c *Client) Settings(ctx context.Context) (*Settings, error) {
	var s Settings
	if err := c.post(ctx, "/panel/api/setting/all", nil, &s, true); err != nil {
		return nil, err
	}
	return &s, nil
}

// BulkAdjust shifts expiry/quota for the given emails. Never retried: a
// duplicate would extend twice.
func (c *Client) BulkAdjust(ctx context.Context, req BulkAdjustRequest) (*BulkAdjustResult, error) {
	if len(req.Emails) == 0 {
		return nil, errors.New("xui: bulkAdjust: no emails")
	}
	if req.AddDays == 0 && req.AddBytes == 0 {
		return nil, errors.New("xui: bulkAdjust: nothing to adjust")
	}
	var raw json.RawMessage
	err := c.post(ctx, "/panel/api/clients/bulkAdjust", req, &raw, false)
	c.InvalidateCache()
	if err != nil {
		return nil, err
	}
	res := &BulkAdjustResult{Raw: raw}
	_ = json.Unmarshal(raw, res) // best effort; shape is not documented
	return res, nil
}

// SubscriptionURL builds the client's subscription link from panel settings.
func (s *Settings) SubscriptionURL(subID string) string {
	if s.SubURI != "" {
		return strings.TrimRight(s.SubURI, "/") + "/" + subID
	}
	scheme := "http"
	if s.SubCertFile != "" {
		scheme = "https"
	}
	host := s.SubDomain
	if host == "" {
		host = s.SubListen
	}
	path := s.SubPath
	if path == "" {
		path = "/sub/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return fmt.Sprintf("%s://%s:%d%s%s", scheme, host, s.SubPort, path, subID)
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.withRetry(ctx, path, func() error { return c.do(ctx, http.MethodGet, path, query, nil, out) })
}

func (c *Client) post(ctx context.Context, path string, body, out any, retry bool) error {
	call := func() error { return c.do(ctx, http.MethodPost, path, nil, body, out) }
	if !retry {
		return call()
	}
	return c.withRetry(ctx, path, call)
}

func (c *Client) withRetry(ctx context.Context, path string, call func() error) error {
	err := call()
	var te *transportError
	if err == nil || !errors.As(err, &te) {
		return err
	}
	c.log.Warn("xui: transient error, retrying once", "path", path, "err", err)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(retryDelay):
	}
	return call()
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("xui: %s: encode body: %w", path, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return &transportError{err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return &transportError{err}
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &APIError{Path: path, Msg: "unauthorized: check NODE_API_TOKEN", HTTPStatus: resp.StatusCode}
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return &transportError{fmt.Errorf("http %d from %s", resp.StatusCode, path)}
	}

	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("xui: %s: unexpected response (http %d): %s", path, resp.StatusCode, snippet(data))
	}
	if !env.Success {
		return &APIError{Path: path, Msg: env.Msg, HTTPStatus: resp.StatusCode}
	}
	if out == nil || len(env.Obj) == 0 || string(env.Obj) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Obj, out); err != nil {
		return fmt.Errorf("xui: %s: decode obj: %w", path, err)
	}
	return nil
}

// snippet shortens a non-JSON body for error messages (e.g. an HTML login page).
func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	if s == "" {
		return "<empty body>"
	}
	return s
}
