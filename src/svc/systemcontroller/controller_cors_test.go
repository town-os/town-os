// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

// The rule this replaces was "the Origin's hostname equals the Host header's
// hostname", which both halves of an attacker-chosen URL satisfy: register
// box.evil.example, point it at the box's LAN address, and a browser sends
// Origin and Host that match. With AllowCredentials on, that is a drive-by page
// talking to the control plane — including the bootstrap window, where
// /account/create answers unauthenticated while no enabled admin exists.

// corsCtx builds an echo context whose request carries the given Host.
func corsCtx(t *testing.T, host string) *echo.Context {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/status/ping", nil)
	req.Host = host
	return e.NewContext(req, httptest.NewRecorder())
}

func TestOriginAllowedRejectsRebindingHost(t *testing.T) {
	t.Setenv("DEBUG", "")

	// The attacker's name resolves to the box, so Origin and Host agree — the
	// old rule's entire test.
	c := corsCtx(t, "box.evil.example:5309")
	if originAllowed(c, "http://box.evil.example", []string{"localhost", "townos"}) {
		t.Fatal("an arbitrary DNS name pointed at this box was accepted as an origin")
	}
}

func TestOriginAllowedAcceptsIPLiteralHost(t *testing.T) {
	t.Setenv("DEBUG", "")

	// The common case: the UI is reached at the box's LAN address on :80 and
	// calls the API on :5309. An address cannot be aliased by DNS, so the two
	// are the same box by construction.
	c := corsCtx(t, "192.168.1.10:5309")
	if !originAllowed(c, "http://192.168.1.10", nil) {
		t.Fatal("cross-port request from the box's own address was rejected")
	}
}

func TestOriginAllowedAcceptsConfiguredHost(t *testing.T) {
	t.Setenv("DEBUG", "")

	c := corsCtx(t, "townos.home:5309")
	if !originAllowed(c, "http://townos.home", []string{"localhost", "townos", "townos.home"}) {
		t.Fatal("a configured allowed host was rejected")
	}
}

func TestOriginAllowedRejectsMismatchedOrigin(t *testing.T) {
	t.Setenv("DEBUG", "")

	c := corsCtx(t, "192.168.1.10:5309")
	if originAllowed(c, "http://evil.example", nil) {
		t.Fatal("an unrelated origin was accepted")
	}
}

func TestOriginAllowedRejectsEmptyAndMalformed(t *testing.T) {
	t.Setenv("DEBUG", "")

	c := corsCtx(t, "192.168.1.10:5309")
	for _, origin := range []string{"", "://", "http://"} {
		if originAllowed(c, origin, nil) {
			t.Errorf("origin %q was accepted", origin)
		}
	}
}

func TestOriginAllowedDebugAllowsEverything(t *testing.T) {
	t.Setenv("DEBUG", "1")

	c := corsCtx(t, "box.evil.example:5309")
	if !originAllowed(c, "http://anything.example", nil) {
		t.Fatal("DEBUG mode should allow any origin")
	}
}

func TestHostIsSelf(t *testing.T) {
	t.Setenv("DEBUG", "")

	allowed := []string{"localhost", "townos"}
	cases := map[string]bool{
		"192.168.1.10":      true,
		"127.0.0.1":         true,
		"::1":               true,
		"[::1]":             true,
		"localhost":         true,
		"townos":            true,
		"townos.evil.test":  false,
		"evil.example":      false,
	}
	for host, want := range cases {
		if got := hostIsSelf(host, allowed); got != want {
			t.Errorf("hostIsSelf(%q) = %v, want %v", host, got, want)
		}
	}
}

// Private Network Access is the browser's extra permission for a public page to
// reach a private address. Echoing it for every origin gives away exactly what
// it exists to withhold.
func TestPrivateNetworkHeaderOnlyForAllowedOrigin(t *testing.T) {
	t.Setenv("DEBUG", "")
	env := newAuthEnv(t)

	cases := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{"rebinding origin", "box.evil.example:5309", "http://box.evil.example", false},
		{"unrelated origin", "192.168.1.10:5309", "http://evil.example", false},
		{"same address", "192.168.1.10:5309", "http://192.168.1.10", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveWithHeaders(t, env.cfg, tc.host, map[string]string{
				"Origin": tc.origin,
				"Access-Control-Request-Private-Network": "true",
			})
			got := rec.Header().Get("Access-Control-Allow-Private-Network") == "true"
			if got != tc.want {
				t.Fatalf("Access-Control-Allow-Private-Network present = %v, want %v", got, tc.want)
			}
		})
	}
}
