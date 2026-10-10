package routes

import (
	"os"
	"regexp"
	"testing"
)

// The frontend routes the admin screen by its own constant. If the two drift
// apart, the server would hand the admin manifest to a page the router does
// not know, and admin notifications would open the client screen.
func TestAdminPathMatchesFrontend(t *testing.T) {
	src, err := os.ReadFile("../../web/src/routes.ts")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`ADMIN_PATH\s*=\s*'([^']+)'`).FindSubmatch(src)
	if m == nil {
		t.Fatal("ADMIN_PATH not found in web/src/routes.ts")
	}
	if got := string(m[1]); got != Admin {
		t.Errorf("web/src/routes.ts has ADMIN_PATH %q, server has %q", got, Admin)
	}
}
