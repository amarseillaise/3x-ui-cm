// Package config loads process configuration: secrets and runtime settings from
// environment variables (Env) and product settings such as plans and payment
// requisites from a YAML file (AppConfig).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const minSecretLen = 16

// Env holds configuration read from environment variables. It contains secrets:
// never log the whole struct.
type Env struct {
	NodeURL         string
	NodeAPIToken    string
	NodeTLSInsecure bool

	AppBaseURL string
	SubBaseURL string

	AdminToken      string
	AdminLinkSecret string
	SessionSecret   string

	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string

	DBPath     string
	ConfigPath string
	ListenAddr string
	TrustProxy bool // take the client IP from X-Forwarded-For / X-Real-IP

	PollInterval     time.Duration
	NotifyDays       []int
	NotifyTrafficPct int
	LogLevel         string
}

// PushEnabled reports whether Web Push can be used (VAPID keys configured).
func (e Env) PushEnabled() bool {
	return e.VAPIDPublicKey != "" && e.VAPIDPrivateKey != ""
}

// LoadEnv reads and validates configuration from the process environment.
func LoadEnv() (Env, error) {
	return loadEnv(os.LookupEnv)
}

func loadEnv(lookup func(string) (string, bool)) (Env, error) {
	get := func(key, def string) string {
		if v, ok := lookup(key); ok {
			if t := strings.TrimSpace(v); t != "" {
				return t
			}
		}
		return def
	}

	e := Env{
		NodeURL:         get("NODE_URL", ""),
		NodeAPIToken:    get("NODE_API_TOKEN", ""),
		NodeTLSInsecure: get("NODE_TLS_INSECURE", "0") == "1",
		AppBaseURL:      strings.TrimRight(get("APP_BASE_URL", ""), "/"),
		SubBaseURL:      get("SUB_BASE_URL", ""),
		AdminToken:      get("ADMIN_TOKEN", ""),
		AdminLinkSecret: get("ADMIN_LINK_SECRET", ""),
		SessionSecret:   get("SESSION_SECRET", ""),
		VAPIDPublicKey:  get("VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey: get("VAPID_PRIVATE_KEY", ""),
		VAPIDSubject:    get("VAPID_SUBJECT", ""),
		DBPath:          get("DB_PATH", "/data/app.db"),
		ConfigPath:      get("CONFIG_PATH", "/data/config.yaml"),
		ListenAddr:      get("LISTEN_ADDR", ":8080"),
		TrustProxy:      get("TRUST_PROXY", "1") == "1",
		LogLevel:        strings.ToLower(get("LOG_LEVEL", "info")),
	}

	var errs []error
	for _, r := range []struct{ key, val string }{
		{"NODE_URL", e.NodeURL},
		{"NODE_API_TOKEN", e.NodeAPIToken},
		{"APP_BASE_URL", e.AppBaseURL},
	} {
		if r.val == "" {
			errs = append(errs, fmt.Errorf("%s is required", r.key))
		}
	}
	for _, r := range []struct{ key, val string }{
		{"ADMIN_TOKEN", e.AdminToken},
		{"ADMIN_LINK_SECRET", e.AdminLinkSecret},
		{"SESSION_SECRET", e.SessionSecret},
	} {
		switch {
		case r.val == "":
			errs = append(errs, fmt.Errorf("%s is required", r.key))
		case len(r.val) < minSecretLen:
			errs = append(errs, fmt.Errorf("%s must be at least %d characters", r.key, minSecretLen))
		}
	}
	if e.AdminToken != "" && e.AdminToken == e.AdminLinkSecret {
		errs = append(errs, errors.New("ADMIN_LINK_SECRET must differ from ADMIN_TOKEN"))
	}
	if e.NodeURL != "" && !isHTTPURL(e.NodeURL) {
		errs = append(errs, errors.New("NODE_URL must be an absolute http(s) URL"))
	}
	if e.AppBaseURL != "" && !isHTTPURL(e.AppBaseURL) {
		errs = append(errs, errors.New("APP_BASE_URL must be an absolute http(s) URL"))
	}
	if e.SubBaseURL != "" && !isHTTPURL(e.SubBaseURL) {
		errs = append(errs, errors.New("SUB_BASE_URL must be an absolute http(s) URL"))
	}
	if (e.VAPIDPublicKey == "") != (e.VAPIDPrivateKey == "") {
		errs = append(errs, errors.New("VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY must be set together"))
	}
	if e.PushEnabled() && e.VAPIDSubject == "" {
		errs = append(errs, errors.New("VAPID_SUBJECT is required when VAPID keys are set (mailto:you@example.com)"))
	}

	var err error
	if e.PollInterval, err = time.ParseDuration(get("POLL_INTERVAL", "10m")); err != nil || e.PollInterval < time.Minute {
		errs = append(errs, errors.New("POLL_INTERVAL must be a duration of at least 1m"))
	}
	if e.NotifyDays, err = parseDays(get("NOTIFY_DAYS", "7,3,1")); err != nil {
		errs = append(errs, fmt.Errorf("NOTIFY_DAYS: %w", err))
	}
	if e.NotifyTrafficPct, err = strconv.Atoi(get("NOTIFY_TRAFFIC_PCT", "90")); err != nil || e.NotifyTrafficPct < 1 || e.NotifyTrafficPct > 100 {
		errs = append(errs, errors.New("NOTIFY_TRAFFIC_PCT must be an integer in 1..100"))
	}
	switch e.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("LOG_LEVEL must be one of debug, info, warn, error (got %q)", e.LogLevel))
	}

	return e, errors.Join(errs...)
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// parseDays parses "7,3,1" into a descending, de-duplicated list of positive days.
func parseDays(s string) ([]int, error) {
	seen := map[int]bool{}
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid value %q (positive integers expected)", part)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("at least one value required")
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out, nil
}
