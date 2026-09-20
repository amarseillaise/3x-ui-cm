package config

import (
	"strings"
	"testing"
	"time"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"NODE_URL":          "https://panel.example.com:2053/base/",
		"NODE_API_TOKEN":    "token",
		"APP_BASE_URL":      "https://cab.example.com/",
		"ADMIN_TOKEN":       strings.Repeat("a", 32),
		"ADMIN_LINK_SECRET": strings.Repeat("b", 32),
		"SESSION_SECRET":    strings.Repeat("c", 32),
	}
}

func TestLoadEnvDefaults(t *testing.T) {
	e, err := loadEnv(lookupFrom(validEnv()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.AppBaseURL != "https://cab.example.com" {
		t.Errorf("AppBaseURL trailing slash not trimmed: %q", e.AppBaseURL)
	}
	if e.PollInterval != 10*time.Minute {
		t.Errorf("PollInterval = %v", e.PollInterval)
	}
	if got, want := e.NotifyDays, []int{7, 3, 1}; len(got) != 3 || got[0] != want[0] || got[2] != want[2] {
		t.Errorf("NotifyDays = %v", got)
	}
	if e.NotifyTrafficPct != 90 || e.DBPath != "/data/app.db" || e.ListenAddr != ":8080" {
		t.Errorf("defaults wrong: %+v", e)
	}
	if e.PushEnabled() {
		t.Error("push must be disabled without VAPID keys")
	}
}

func TestLoadEnvErrors(t *testing.T) {
	cases := map[string]func(m map[string]string){
		"missing NODE_URL":      func(m map[string]string) { delete(m, "NODE_URL") },
		"bad NODE_URL":          func(m map[string]string) { m["NODE_URL"] = "panel.example.com" },
		"short secret":          func(m map[string]string) { m["SESSION_SECRET"] = "short" },
		"same admin secrets":    func(m map[string]string) { m["ADMIN_LINK_SECRET"] = m["ADMIN_TOKEN"] },
		"half VAPID":            func(m map[string]string) { m["VAPID_PUBLIC_KEY"] = "x" },
		"VAPID without subject": func(m map[string]string) { m["VAPID_PUBLIC_KEY"] = "x"; m["VAPID_PRIVATE_KEY"] = "y" },
		"bad NOTIFY_DAYS":       func(m map[string]string) { m["NOTIFY_DAYS"] = "7,0" },
		"short POLL_INTERVAL":   func(m map[string]string) { m["POLL_INTERVAL"] = "10s" },
		"bad LOG_LEVEL":         func(m map[string]string) { m["LOG_LEVEL"] = "verbose" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := validEnv()
			mutate(m)
			if _, err := loadEnv(lookupFrom(m)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseDays(t *testing.T) {
	got, err := parseDays(" 1, 7 ,3,7 ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 7 || got[1] != 3 || got[2] != 1 {
		t.Errorf("got %v, want [7 3 1]", got)
	}
}
