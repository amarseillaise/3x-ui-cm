// Package subtemplate ships the 3x-ui subscription-page template that turns
// the client's subscription URL into the cabinet entry point.
//
// The panel renders the page for browser requests (Accept: text/html) with Go
// html/template and passes sId among other fields; VPN apps still receive the
// base64 config list. The template redirects to APP_BASE_URL/s/{{ .sId }},
// so the only link a client ever needs is the subscription URL.
package subtemplate

import (
	_ "embed"
	"errors"
	"strings"
)

//go:embed index.html
var source string

const placeholder = "__APP_BASE_URL__"

// Render substitutes the cabinet base URL into the template. The result is
// still a Go html/template: the panel evaluates {{ .sId }} at request time.
func Render(appBaseURL string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(appBaseURL), "/")
	if base == "" {
		return "", errors.New("subtemplate: APP_BASE_URL is required")
	}
	if strings.ContainsAny(base, "\"<>{}") {
		return "", errors.New("subtemplate: APP_BASE_URL contains characters not allowed in the template")
	}
	return strings.ReplaceAll(source, placeholder, base), nil
}
