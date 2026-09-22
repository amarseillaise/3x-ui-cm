// Package nginxconf renders the nginx server block that puts TLS in front of
// the cabinet. Everything it needs is already in .env, so the port in nginx
// and the port in APP_BASE_URL cannot drift apart.
package nginxconf

import (
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"text/template"
)

//go:embed vhost.conf.tmpl
var source string

var tmpl = template.Must(template.New("vhost").Parse(source))

// Config is the rendered block's input.
type Config struct {
	ServerName   string
	Port         int
	CertFile     string
	KeyFile      string
	Upstream     string
	RedirectHTTP bool
}

// DefaultUpstream is where docker-compose publishes the app.
const DefaultUpstream = "127.0.0.1:8080"

// FromAppBaseURL derives the host and port from APP_BASE_URL. Certificate
// paths cannot be guessed, so the caller must supply them.
func FromAppBaseURL(appBaseURL, certFile, keyFile string) (Config, error) {
	var cfg Config
	raw := strings.TrimSpace(appBaseURL)
	if raw == "" {
		return cfg, errors.New("APP_BASE_URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return cfg, fmt.Errorf("APP_BASE_URL is not a URL: %w", err)
	}
	if u.Scheme != "https" {
		return cfg, fmt.Errorf("APP_BASE_URL must be https to terminate TLS, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return cfg, fmt.Errorf("APP_BASE_URL has no host: %q", raw)
	}
	port := 443
	if p := u.Port(); p != "" {
		if port, err = strconv.Atoi(p); err != nil || port < 1 || port > 65535 {
			return cfg, fmt.Errorf("APP_BASE_URL has a bad port: %q", p)
		}
	}
	if u.Path != "" && u.Path != "/" {
		return cfg, fmt.Errorf("APP_BASE_URL must not have a path, got %q", u.Path)
	}
	if certFile == "" || keyFile == "" {
		return cfg, fmt.Errorf("certificate and key paths are required, e.g. "+
			"/etc/letsencrypt/live/%s/fullchain.pem or /root/cert/%s/fullchain.pem", host, host)
	}
	return Config{
		ServerName: host,
		Port:       port,
		CertFile:   certFile,
		KeyFile:    keyFile,
		Upstream:   DefaultUpstream,
	}, nil
}

// Render writes the server block.
func Render(cfg Config) (string, error) {
	switch {
	case cfg.ServerName == "":
		return "", errors.New("server name is required")
	case cfg.Port < 1 || cfg.Port > 65535:
		return "", fmt.Errorf("port %d is out of range", cfg.Port)
	case cfg.CertFile == "" || cfg.KeyFile == "":
		return "", errors.New("certificate and key paths are required")
	}
	if cfg.Upstream == "" {
		cfg.Upstream = DefaultUpstream
	}
	// A stray quote or newline here would end up as an nginx directive.
	for field, value := range map[string]string{
		"server name": cfg.ServerName,
		"certificate": cfg.CertFile,
		"key":         cfg.KeyFile,
		"upstream":    cfg.Upstream,
	} {
		if strings.ContainsAny(value, " \t\n\r;{}\"'#") {
			return "", fmt.Errorf("%s %q contains characters nginx would read as syntax", field, value)
		}
	}
	var out strings.Builder
	if err := tmpl.Execute(&out, cfg); err != nil {
		return "", err
	}
	return strings.TrimLeft(out.String(), "\n"), nil
}
