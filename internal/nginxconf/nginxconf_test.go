package nginxconf

import (
	"strings"
	"testing"
)

func TestFromAppBaseURLTakesHostAndPort(t *testing.T) {
	cfg, err := FromAppBaseURL("https://cab.example.com:8443", "/c/full.pem", "/c/priv.pem")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "cab.example.com" || cfg.Port != 8443 {
		t.Errorf("got %+v", cfg)
	}
	if cfg.Upstream != DefaultUpstream {
		t.Errorf("upstream = %q", cfg.Upstream)
	}
}

func TestFromAppBaseURLDefaultsTo443(t *testing.T) {
	cfg, err := FromAppBaseURL("https://cab.example.com", "/c/full.pem", "/c/priv.pem")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 443 {
		t.Errorf("port = %d, want 443", cfg.Port)
	}
}

func TestFromAppBaseURLRejectsBadInput(t *testing.T) {
	cases := map[string][3]string{
		"empty":      {"", "/c", "/k"},
		"plain http": {"http://cab.example.com", "/c", "/k"},
		"no host":    {"https://", "/c", "/k"},
		"has a path": {"https://cab.example.com/cabinet", "/c", "/k"},
		"bad port":   {"https://cab.example.com:port", "/c", "/k"},
		"no cert":    {"https://cab.example.com", "", "/k"},
		"no key":     {"https://cab.example.com", "/c", ""},
	}
	for title, in := range cases {
		t.Run(title, func(t *testing.T) {
			if _, err := FromAppBaseURL(in[0], in[1], in[2]); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// The generated port must be the one the app advertises, which is the drift
// this generator exists to prevent.
func TestRenderUsesTheSamePortAsAppBaseURL(t *testing.T) {
	cfg, err := FromAppBaseURL("https://cab.example.com:8443", "/c/full.pem", "/c/priv.pem")
	if err != nil {
		t.Fatal(err)
	}
	out, err := Render(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"listen 8443 ssl http2;",
		"listen [::]:8443 ssl http2;",
		"server_name cab.example.com;",
		"ssl_certificate     /c/full.pem;",
		"ssl_certificate_key /c/priv.pem;",
		"proxy_pass         http://127.0.0.1:8080;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "listen 80;") {
		t.Error("the redirect block must be opt-in")
	}
}

func TestRenderRedirectKeepsTheNonStandardPort(t *testing.T) {
	cfg, _ := FromAppBaseURL("https://cab.example.com:8443", "/c", "/k")
	cfg.RedirectHTTP = true
	out, _ := Render(cfg)
	if !strings.Contains(out, "return 301 https://$host:8443$request_uri;") {
		t.Errorf("redirect lost the port:\n%s", out)
	}

	cfg, _ = FromAppBaseURL("https://cab.example.com", "/c", "/k")
	cfg.RedirectHTTP = true
	out, _ = Render(cfg)
	if !strings.Contains(out, "return 301 https://$host$request_uri;") {
		t.Errorf("443 should not be written out:\n%s", out)
	}
}

// Values reach nginx as directives, so anything that could end one is refused.
func TestRenderRejectsInjection(t *testing.T) {
	base, _ := FromAppBaseURL("https://cab.example.com", "/c", "/k")
	for _, bad := range []string{"a; root /etc", "a b", "a\nlisten 80", `a"b`, "a{b}"} {
		cfg := base
		cfg.ServerName = bad
		if _, err := Render(cfg); err == nil {
			t.Errorf("Render accepted server name %q", bad)
		}
		cfg = base
		cfg.CertFile = bad
		if _, err := Render(cfg); err == nil {
			t.Errorf("Render accepted certificate %q", bad)
		}
	}
}
