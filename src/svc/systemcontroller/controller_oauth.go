package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"gitea.com/town-os/town-os/src/packages"
)

// An oauth question is answered by running an OAuth device flow from the
// install dialog: the browser asks the controller to start the flow, the
// operator approves it at the provider, and the browser polls the controller
// until a token comes back. The token becomes the question's response.
//
// The controller -- not the browser -- makes the calls to the provider, because
// a provider's device endpoints are not obliged to send CORS headers and most
// do not. That makes these requests server-side fetches of a URL a package
// named, so every one of them goes through oauthClient, whose dialer refuses any
// address that is not public. See packages.CheckOAuthAddr.

const (
	oauthFlowTTL          = 15 * time.Minute
	oauthDefaultInterval  = 5 * time.Second
	oauthDefaultTimeout   = 5 * time.Minute
	oauthRequestTimeout   = 30 * time.Second
	oauthMaxResponseBytes = 1 << 20 // 1 MiB; a device-flow response is a few hundred bytes
	// A flow is discarded after oauthFlowTTL regardless, so no package-declared
	// interval or timeout has any business being longer than that.
	oauthMaxDuration = oauthFlowTTL
)

type oauthFlow struct {
	question packages.Question
	// values holds what Extract pulled out of the start response, plus
	// client_id, and is what the poll URL and headers template against.
	values   map[string]string
	deadline time.Time
}

// oauthFlows is the set of device flows currently awaiting approval. A flow is
// short-lived and worthless once used, so it lives in memory and dies with the
// process: a restart mid-approval just means starting the flow again.
type oauthFlows struct {
	mu    sync.Mutex
	flows map[string]*oauthFlow
}

func newOAuthFlows() *oauthFlows {
	return &oauthFlows{flows: map[string]*oauthFlow{}}
}

func (o *oauthFlows) put(id string, f *oauthFlow) {
	o.mu.Lock()
	defer o.mu.Unlock()
	// Opportunistically drop anything expired, so an abandoned flow (the operator
	// closed the dialog) cannot accumulate.
	for k, v := range o.flows {
		if time.Now().After(v.deadline) {
			delete(o.flows, k)
		}
	}
	o.flows[id] = f
}

func (o *oauthFlows) get(id string) (*oauthFlow, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	f, ok := o.flows[id]
	if !ok {
		return nil, false
	}
	if time.Now().After(f.deadline) {
		delete(o.flows, id)
		return nil, false
	}
	return f, true
}

func (o *oauthFlows) del(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.flows, id)
}

// oauthClient is the only HTTP client used to call a package-named URL. Its
// dialer runs every resolved address past packages.CheckOAuthAddr, which is what
// makes the guard real: validating the URL's host at load time cannot stop a
// hostname that resolves to 127.0.0.1, and CheckRedirect cannot stop a redirect
// chain whose final hop lands somewhere private. Both are covered here.
//
// allowPrivate exists for tests, whose provider is an httptest server on
// loopback. It is never set from a package or a request.
func oauthClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		// Control, NOT Transport.DialContext. DialContext is handed the URL's host
		// verbatim -- "plex.tv:443" -- because the name is resolved inside the
		// dialer, below it. A check there sees a hostname, not an address, so it
		// can only reject what it cannot parse: every real provider. Control runs
		// once per connection attempt, after resolution, with the concrete IP:port
		// about to be connected -- the one place the check is both correct and
		// proof against a name that answers with 127.0.0.1.
		Control: func(network, address string, _ syscall.RawConn) error {
			if allowPrivate {
				return nil
			}
			return packages.CheckOAuthAddr(network, address)
		},
	}
	return &http.Client{
		Timeout: oauthRequestTimeout,
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
		},
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if req.URL.Scheme != "https" && !allowPrivate {
				return fmt.Errorf("%w: redirect to %q", packages.ErrOAuthURLNotAllowed, req.URL.Scheme)
			}
			return nil
		},
	}
}

// applyOAuthTemplate substitutes {{name}} from values. An unknown name resolves
// to the empty string rather than being left as a literal, so a typo cannot ship
// "{{cod}}" to the provider as if it were a value.
func applyOAuthTemplate(s string, values map[string]string) string {
	var b strings.Builder
	for {
		start := strings.Index(s, "{{")
		if start < 0 {
			b.WriteString(s)
			return b.String()
		}
		name, rest, found := strings.Cut(s[start+2:], "}}")
		if !found {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:start])
		b.WriteString(values[strings.TrimSpace(name)])
		s = rest
	}
}

// doOAuthStep runs one call of a flow and returns the decoded JSON body.
func doOAuthStep(ctx context.Context, client *http.Client, step packages.OAuthStep, values map[string]string, allowPrivate bool) (map[string]any, error) {
	rawURL := applyOAuthTemplate(step.URL, values)
	if err := packages.ValidateOAuthURLAllowing(rawURL, allowPrivate); err != nil {
		return nil, err
	}

	method := strings.ToUpper(step.Method)
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if len(step.Form) > 0 {
		form := url.Values{}
		for k, v := range step.Form {
			form.Set(k, applyOAuthTemplate(v, values))
		}
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	// Every device flow in sight speaks JSON, and several (Plex, GitHub) return
	// XML or a form-encoded body unless told otherwise.
	req.Header.Set("Accept", "application/json")
	if len(step.Form) > 0 {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range step.Headers {
		req.Header.Set(k, applyOAuthTemplate(v, values))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // response body close

	raw, err := io.ReadAll(io.LimitReader(resp.Body, oauthMaxResponseBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("oauth: provider returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("oauth: provider response is not a JSON object: %w", err)
	}
	return decoded, nil
}

// oauthString reads a field as a string. A device flow's pending marker is a
// null or absent token, and providers are casual about types -- Plex's pin id is
// a number that has to reach the poll URL as text.
func oauthString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// A pin id is an integer; %v on a float64 would render it as 1.234567e+06.
		return strconv.FormatInt(int64(t), 10)
	case bool:
		return strconv.FormatBool(t)
	default:
		return ""
	}
}

// oauthSeconds turns a package's duration string into a time.Duration, falling
// back to def when it is absent, unparseable, or so large it would overflow --
// a package saying "timeout: 9999999999h" gets the default, not a negative
// deadline that expires the flow before it starts.
func oauthSeconds(spec string, def time.Duration) time.Duration {
	if spec == "" {
		return def
	}
	secs, err := packages.ParseDuration(spec)
	if err != nil || secs == 0 || secs > uint64(oauthMaxDuration/time.Second) {
		return def
	}
	return time.Duration(secs) * time.Second
}

// oauthAllowPrivate reports whether this server may aim a device flow at a
// private address. It is read through an optional interface rather than added to
// systemControllerBackend so the many test backends that implement that
// interface do not all have to grow a method about OAuth.
func (s *SystemControllerHandlers) oauthAllowPrivate() bool {
	b, ok := s.Controller.(interface{ GetOAuthAllowPrivate() bool })
	if !ok {
		return false
	}
	return b.GetOAuthAllowPrivate()
}

// responseForgetter is the part of the installer that can drop stored answers.
// Read through an optional interface rather than added to packages.Installer so
// the many test doubles implementing that interface do not all have to grow a
// method about OAuth -- the same reason oauthAllowPrivate is shaped this way.
type responseForgetter interface {
	ForgetResponseKeys(repoName, pkgName string, keys []string) error
}

// forgetOAuthResponses drops a package's device-flow answers from every stored
// response, so the next install has to run the flow again instead of silently
// re-using a credential minted for an instance that no longer exists.
//
// Best-effort and never fatal: this runs after the volumes are already gone, and
// failing the uninstall at that point would leave the operator with a
// half-removed package. It is logged at Warn rather than Debug because the
// consequence of skipping it is an install that looks fine and comes up
// unauthorized.
//
// The questions are read from the package definition in the repository, not from
// the install record, because the record has already been removed by this point.
// parentName rather than effectiveName: a dependency instance shares its
// parent's definition, and only the definition carries the question types.
func (s *SystemControllerHandlers) forgetOAuthResponses(repoName, parentName, effectiveName, version string) {
	inst, ok := s.Controller.GetInstaller().(responseForgetter)
	if !ok {
		return
	}

	rr := s.Controller.GetRepositoryRoot()
	if rr == nil {
		return
	}

	ip, err := rr.LoadPackage(repoName, parentName, version)
	if err != nil {
		slog.Warn(fmt.Sprintf("forget oauth responses %s/%s@%s: load package: %v", repoName, parentName, version, err))
		return
	}

	names := packages.OAuthQuestionNames(ip.Questions)
	if len(names) == 0 {
		return
	}

	if err := inst.ForgetResponseKeys(repoName, effectiveName, names); err != nil {
		slog.Warn(fmt.Sprintf("forget oauth responses %s/%s: %v", repoName, effectiveName, err))
	}
}

type OAuthStartRequest struct {
	Repo     string `json:"repo"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Question string `json:"question"`
}

type OAuthStartResponse struct {
	FlowID     string `json:"flow_id"`
	ApproveURL string `json:"approve_url"`
	UserCode   string `json:"user_code,omitempty"`
	IntervalMS int    `json:"interval_ms"`
}

type OAuthPollRequest struct {
	FlowID string `json:"flow_id"`
}

type OAuthPollResponse struct {
	Status string `json:"status"` // "pending" | "approved" | "expired"
	Token  string `json:"token,omitempty"`
}

// startOAuth creates the pending authorization and hands back the URL the
// operator has to approve it at.
func (s *SystemControllerHandlers) startOAuth(c *echo.Context) error {
	req := OAuthStartRequest{}
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return err
	}

	ip, err := s.Controller.GetRepositoryRoot().LoadPackage(req.Repo, req.Name, req.Version)
	if err != nil {
		return err
	}
	q, ok := ip.Questions[req.Question]
	if !ok {
		return echo.NewHTTPError(400, fmt.Sprintf("no question %q in %s@%s", req.Question, req.Name, req.Version))
	}
	// Validate here as well as at load: this is the point where a URL is about to
	// be fetched, and it is the only check a package cannot route around.
	if err := packages.ValidateOAuthFlowAllowing(req.Question, q, s.oauthAllowPrivate()); err != nil {
		return echo.NewHTTPError(400, err.Error())
	}
	if q.Type != packages.Oauth {
		return echo.NewHTTPError(400, fmt.Sprintf("question %q is not an oauth question", req.Question))
	}
	flow := q.OAuth

	// Plex requires the same client identifier on the create and the poll, and
	// uses it to name the device in the user's account.
	values := map[string]string{"client_id": uuid.NewString()}

	allowPrivate := s.oauthAllowPrivate()
	client := oauthClient(allowPrivate)
	started, err := doOAuthStep(c.Request().Context(), client, flow.Start, values, allowPrivate)
	if err != nil {
		return echo.NewHTTPError(502, err.Error())
	}
	for name, key := range flow.Extract {
		values[name] = oauthString(started, key)
	}

	interval := oauthSeconds(flow.Interval, oauthDefaultInterval)
	timeout := oauthSeconds(flow.Timeout, oauthDefaultTimeout)

	approve := applyOAuthTemplate(flow.Approve, values)
	if err := packages.ValidateOAuthURLAllowing(approve, allowPrivate); err != nil {
		return echo.NewHTTPError(400, err.Error())
	}

	id := uuid.NewString()
	s.oauthFlows().put(id, &oauthFlow{
		question: q,
		values:   values,
		deadline: time.Now().Add(min(timeout, oauthFlowTTL)),
	})

	return c.JSON(200, OAuthStartResponse{
		FlowID:     id,
		ApproveURL: approve,
		UserCode:   applyOAuthTemplate(flow.UserCode, values),
		IntervalMS: int(interval / time.Millisecond),
	})
}

// pollOAuth asks the provider whether the operator has approved yet. The browser
// drives the polling loop; each call here is a single check, so a hung provider
// cannot tie up a request for minutes.
func (s *SystemControllerHandlers) pollOAuth(c *echo.Context) error {
	req := OAuthPollRequest{}
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return err
	}

	f, ok := s.oauthFlows().get(req.FlowID)
	if !ok {
		return c.JSON(200, OAuthPollResponse{Status: "expired"})
	}

	allowPrivate := s.oauthAllowPrivate()
	client := oauthClient(allowPrivate)
	polled, err := doOAuthStep(c.Request().Context(), client, f.question.OAuth.Poll, f.values, allowPrivate)
	if err != nil {
		// A provider answers an unapproved device flow with an error as often as
		// with a null token (GitHub returns authorization_pending with a 4xx), so
		// an error here is reported as still-pending rather than as a failure. The
		// flow's own deadline is what ends it.
		return c.JSON(200, OAuthPollResponse{Status: "pending"})
	}

	token := oauthString(polled, f.question.OAuth.Token)
	if token == "" {
		return c.JSON(200, OAuthPollResponse{Status: "pending"})
	}

	// The token is handed to the browser once and the flow is done; keeping it
	// server-side would only make a second place for it to leak from.
	s.oauthFlows().del(req.FlowID)
	return c.JSON(200, OAuthPollResponse{Status: "approved", Token: token})
}
