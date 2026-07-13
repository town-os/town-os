package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/labstack/echo/v5"

	"gitea.com/town-os/town-os/src/packages"
)

// mockProvider is a stand-in for plex.tv: it mints a pin, and answers the poll
// with a null token until it is approved -- which is exactly how a device flow
// signals "the user has not said yes yet".
type mockProvider struct {
	server   *httptest.Server
	approved atomic.Bool
	// clientID records the identifier the controller sent, so a test can prove
	// the same one reaches both calls (Plex ties the pin to it).
	startClientID atomic.Value
	pollClientID  atomic.Value
	polls         atomic.Int32
}

func newMockProvider(t *testing.T) *mockProvider {
	t.Helper()
	p := &mockProvider{}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v2/pins", func(w http.ResponseWriter, r *http.Request) {
		p.startClientID.Store(r.Header.Get("X-Plex-Client-Identifier"))
		w.Header().Set("Content-Type", "application/json")
		// id is a NUMBER here, as it is at plex.tv: it has to survive into the
		// poll URL as text, not as 1.234e+06.
		_, _ = w.Write([]byte(`{"id":1234567,"code":"abcd","location":{"code":"US"}}`))
	})

	mux.HandleFunc("/api/v2/pins/1234567", func(w http.ResponseWriter, r *http.Request) {
		p.polls.Add(1)
		p.pollClientID.Store(r.Header.Get("X-Plex-Client-Identifier"))
		w.Header().Set("Content-Type", "application/json")
		if !p.approved.Load() {
			_, _ = w.Write([]byte(`{"id":1234567,"authToken":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":1234567,"authToken":"plex-auth-token"}`))
	})

	p.server = httptest.NewServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

// oauthTestHandlers builds a controller over a local repository holding one
// package whose oauth question points at the mock provider.
func oauthTestHandlers(t *testing.T, base string, allowPrivate bool) *SystemControllerHandlers {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "local"}}

	// The package names the provider's URLs, which is the whole point: no
	// provider registry lives in the controller.
	pkgYAML := `image: alpine:3.20
description: "oauth question test"
environment:
  TOKEN: "@token@"
questions:
  token:
    query: "Provider account"
    type: oauth
    oauth:
      start:
        method: POST
        url: "` + base + `/api/v2/pins?strong=true"
        headers:
          X-Plex-Client-Identifier: "{{client_id}}"
      extract:
        id: id
        code: code
      approve: "https://app.plex.tv/auth#?clientID={{client_id}}&code={{code}}"
      poll:
        url: "` + base + `/api/v2/pins/{{id}}"
        headers:
          X-Plex-Client-Identifier: "{{client_id}}"
      token: authToken
      interval: 1s
      timeout: 5m
`
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "oauthpkg")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile package: %v", err)
	}

	sb := &serverBase{ServerConfig: ServerConfig{
		RepositoryRoot:    rr,
		Installer:         packages.InitMockInstallManager(),
		OAuthAllowPrivate: allowPrivate,
	}}
	return &SystemControllerHandlers{Controller: sb, ctx: context.Background()}
}

func startFlow(t *testing.T, s *SystemControllerHandlers) OAuthStartResponse {
	t.Helper()
	body := `{"repo":"local","name":"oauthpkg","version":"1.0","question":"token"}`
	c, rec := postJSONContext(echo.New(), body)
	if err := s.startOAuth(c); err != nil {
		t.Fatalf("startOAuth: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("startOAuth status = %d: %s", rec.Code, rec.Body.String())
	}
	var out OAuthStartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func pollFlow(t *testing.T, s *SystemControllerHandlers, flowID string) OAuthPollResponse {
	t.Helper()
	c, rec := postJSONContext(echo.New(), `{"flow_id":"`+flowID+`"}`)
	if err := s.pollOAuth(c); err != nil {
		t.Fatalf("pollOAuth: %v", err)
	}
	var out OAuthPollResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// The flow a package describes has to survive the round trip: the start call
// carries the generated client id, the pin id comes back as a number and reaches
// the poll URL as text, and the approval link is built from both.
func TestOAuthStartBuildsApprovalURL(t *testing.T) {
	t.Parallel()
	p := newMockProvider(t)
	s := oauthTestHandlers(t, p.server.URL, true)

	out := startFlow(t, s)

	if out.FlowID == "" {
		t.Fatal("no flow id returned")
	}
	clientID, ok := p.startClientID.Load().(string)
	if !ok || clientID == "" {
		t.Fatal("the provider was not sent a client identifier")
	}
	if !strings.Contains(out.ApproveURL, "code=abcd") {
		t.Fatalf("approve URL %q does not carry the code from the start response", out.ApproveURL)
	}
	if !strings.Contains(out.ApproveURL, "clientID="+clientID) {
		t.Fatalf("approve URL %q does not carry the client id the provider saw", out.ApproveURL)
	}
	if out.IntervalMS != 1000 {
		t.Fatalf("interval = %dms, want the package's 1s", out.IntervalMS)
	}
}

// Until the operator approves, the provider answers with a null token. That is
// "pending", not a failure -- polling has to keep going.
func TestOAuthPollPendingThenApproved(t *testing.T) {
	t.Parallel()
	p := newMockProvider(t)
	s := oauthTestHandlers(t, p.server.URL, true)

	out := startFlow(t, s)

	if got := pollFlow(t, s, out.FlowID); got.Status != "pending" || got.Token != "" {
		t.Fatalf("poll before approval = %+v, want pending with no token", got)
	}

	p.approved.Store(true)

	got := pollFlow(t, s, out.FlowID)
	if got.Status != "approved" {
		t.Fatalf("poll after approval = %+v, want approved", got)
	}
	if got.Token != "plex-auth-token" {
		t.Fatalf("token = %q, want the provider's token", got.Token)
	}

	// The pin id (a JSON number) must have reached the poll URL as "1234567";
	// had it been formatted as a float the mock's route would never have matched
	// and the poll would have 404'd into a permanent "pending".
	if p.polls.Load() < 2 {
		t.Fatalf("provider saw %d polls; the poll URL was not built from the pin id", p.polls.Load())
	}
	pollID, pollOK := p.pollClientID.Load().(string)
	startID, startOK := p.startClientID.Load().(string)
	if !pollOK || !startOK {
		t.Fatal("the provider did not record a client identifier on both calls")
	}
	if pollID != startID {
		t.Fatalf("poll used client id %q but start used %q; Plex ties the pin to one identifier", pollID, startID)
	}
}

// A redeemed flow is gone: the token was handed to the browser, and keeping it
// server-side would only add a second place for it to leak from.
func TestOAuthFlowIsSingleUse(t *testing.T) {
	t.Parallel()
	p := newMockProvider(t)
	s := oauthTestHandlers(t, p.server.URL, true)
	p.approved.Store(true)

	out := startFlow(t, s)
	if got := pollFlow(t, s, out.FlowID); got.Status != "approved" {
		t.Fatalf("first poll = %+v, want approved", got)
	}
	if got := pollFlow(t, s, out.FlowID); got.Status != "expired" {
		t.Fatalf("second poll = %+v, want expired", got)
	}
}

func TestOAuthPollUnknownFlowIsExpired(t *testing.T) {
	t.Parallel()
	p := newMockProvider(t)
	s := oauthTestHandlers(t, p.server.URL, true)

	if got := pollFlow(t, s, "no-such-flow"); got.Status != "expired" {
		t.Fatalf("poll of an unknown flow = %+v, want expired", got)
	}
}

// The guard that matters. The mock provider is on 127.0.0.1, which is exactly
// what a hostile package would aim the controller at -- with the production
// setting, the flow must refuse to call it at all.
func TestOAuthRefusesPrivateAddress(t *testing.T) {
	t.Parallel()
	p := newMockProvider(t)
	s := oauthTestHandlers(t, p.server.URL, false)

	body := `{"repo":"local","name":"oauthpkg","version":"1.0","question":"token"}`
	c, rec := postJSONContext(echo.New(), body)
	err := s.startOAuth(c)

	if err == nil && rec.Code == http.StatusOK {
		t.Fatal("a flow pointed at 127.0.0.1 was allowed to run")
	}
	if p.polls.Load() != 0 {
		t.Fatal("the provider was reached despite the address guard")
	}
}

// The guard has to resolve a name before judging it. Transport.DialContext is
// handed the URL's host verbatim -- "plex.tv:443" -- so a check there sees a
// hostname, cannot parse it as an IP, and rejects it: every real provider is
// refused with "plex.tv is not an IP address". Dialer.Control runs after
// resolution with the concrete address, which is both correct for names and the
// only place a DNS answer of 127.0.0.1 can be caught.
func TestOAuthClientResolvesNamesBeforeJudgingThem(t *testing.T) {
	t.Parallel()
	p := newMockProvider(t)
	// The same server, named rather than numbered: "localhost" resolves to the
	// loopback address the provider is actually listening on.
	byName := strings.Replace(p.server.URL, "127.0.0.1", "localhost", 1)

	resp, err := oauthClient(false).Get(byName + "/api/v2/pins") //nolint:noctx // the client under test
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("a name resolving to loopback was dialed anyway")
	}
	if strings.Contains(err.Error(), "is not an IP address") {
		t.Fatalf("the guard judged the hostname instead of resolving it: %v", err)
	}
	if !errors.Is(err, packages.ErrOAuthURLNotAllowed) {
		t.Fatalf("err = %v, want ErrOAuthURLNotAllowed", err)
	}

	// And with the rule relaxed, the same name connects: nothing about a hostname
	// is objectionable in itself.
	resp, err = oauthClient(true).Get(byName + "/api/v2/pins") //nolint:noctx // the client under test
	if err != nil {
		t.Fatalf("a hostname could not be dialed at all: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
