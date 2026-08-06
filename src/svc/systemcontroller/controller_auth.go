package systemcontroller

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/i18n"
	"github.com/labstack/echo/v5"
)

type AuthenticateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"` //nolint:gosec // G117 -- request field, not hardcoded
}

type AuthenticateResponse struct {
	Token   string           `json:"token"`
	Account *account.Account `json:"account"`
}

type RevokeSessionRequest struct {
	SessionID string `json:"session_id"`
}

type SessionUsernameResponse struct {
	Username string `json:"username"`
}

func (s *SystemControllerHandlers) authenticateAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := AuthenticateRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	// Throttle before hashing, not after: the cost this protects against is
	// the argon2 call itself (64 MiB per attempt), so a refusal that still
	// hashed would have paid for the attack it was refusing.
	limiter, gate := s.loginThrottle()
	source := loginSourceKey(c.Request())
	if !limiter.allow(source) {
		return echo.NewHTTPError(http.StatusTooManyRequests, i18n.T(s.getLocale(), i18n.MsgAuthInvalidCredentials))
	}
	if !gate.acquire() {
		return echo.NewHTTPError(http.StatusServiceUnavailable, i18n.T(s.getLocale(), i18n.MsgAuthInvalidCredentials))
	}
	// Released through a defer inside the closure rather than after the call:
	// a slot leaked by a panic would be gone for the life of the process, and
	// four of them would wedge every login on the box until a restart.
	acct, err := func() (*account.Account, error) {
		defer gate.release()
		return s.Controller.GetAccountManager().Authenticate(req.Username, req.Password)
	}()
	if err != nil {
		return echo.NewHTTPError(401, i18n.T(s.getLocale(), i18n.MsgAuthInvalidCredentials))
	}

	// A proved-good password is not guessing. Clearing the window keeps a
	// household behind one NAT address from walking into a lockout through
	// ordinary use.
	limiter.reset(source)

	token, err := s.Controller.GetSessionManager().Create(req.Username)
	if err != nil {
		return err
	}

	return c.JSON(200, AuthenticateResponse{Token: token, Account: acct})
}

func (s *SystemControllerHandlers) revokeSession(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RevokeSessionRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.Controller.GetSessionManager().Revoke(req.SessionID); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listSessions(c *echo.Context) error {
	locale := s.getLocale()
	token := extractBearerToken(c.Request())
	if token == "" {
		return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthMissingToken))
	}

	sess, _, err := s.Controller.GetSessionManager().Validate(token)
	if err != nil {
		return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthInvalidSession))
	}

	sessions, err := s.Controller.GetSessionManager().List(sess.Username)
	if err != nil {
		return err
	}

	return c.JSON(200, sessions)
}

func (s *SystemControllerHandlers) sessionUsername(c *echo.Context) error {
	locale := s.getLocale()
	token := extractBearerToken(c.Request())
	if token == "" {
		return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthMissingToken))
	}

	sess, _, err := s.Controller.GetSessionManager().Validate(token)
	if err != nil {
		return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthInvalidSession))
	}

	return c.JSON(200, SessionUsernameResponse{Username: sess.Username})
}

// --- Admin middleware ---

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

func (s *SystemControllerHandlers) requireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.Controller.GetSessionManager() == nil {
			return next(c)
		}

		locale := s.getLocale()
		token := extractBearerToken(c.Request())
		if token == "" {
			return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthMissingToken))
		}

		_, acct, err := s.Controller.GetSessionManager().Validate(token)
		if err != nil {
			return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthInvalidSession))
		}

		if !acct.Admin {
			return echo.NewHTTPError(403, i18n.T(locale, i18n.MsgAuthAdminRequired))
		}

		return next(c)
	}
}

// isLocalhost returns true when the request originates from a loopback address.
func isLocalhost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// localhostOrAuth allows unauthenticated access from localhost while
// requiring a valid session token for all other origins.
func (s *SystemControllerHandlers) localhostOrAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if isLocalhost(c.Request()) {
			return next(c)
		}
		return s.requireAuth(next)(c)
	}
}

// localhostOrAdmin is localhostOrAuth for data that an ordinary account has no
// business reading: the systemd journal and the audit log.
//
// The journal carries every unit's ExecStart line, which for a package is a
// `podman run` with its environment on it — database passwords, API keys, the
// generated answers to `type: secret` questions. The audit log carries the
// request bodies those answers arrived in. Both were readable by any
// authenticated account, which made "has a dashboard login" equivalent to
// "holds every credential on the box".
//
// Localhost keeps its pass, unchanged: it is how the controller's own tooling
// reads these, and reaching loopback already means being on the box.
func (s *SystemControllerHandlers) localhostOrAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if isLocalhost(c.Request()) {
			return next(c)
		}
		return s.requireAdmin(next)(c)
	}
}

func (s *SystemControllerHandlers) requireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.Controller.GetSessionManager() == nil {
			return next(c)
		}

		locale := s.getLocale()
		token := extractBearerToken(c.Request())
		if token == "" {
			return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthMissingToken))
		}

		_, _, err := s.Controller.GetSessionManager().Validate(token)
		if err != nil {
			return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthInvalidSession))
		}

		return next(c)
	}
}

// callingAccount resolves the account behind the request's bearer token, or nil
// when there is no session manager, no token, or the token is invalid. It never
// errors — callers that need to *reject* an unauthenticated request rely on the
// route's own auth middleware; callingAccount only answers "who, if anyone".
func (s *SystemControllerHandlers) callingAccount(c *echo.Context) *account.Account {
	sm := s.Controller.GetSessionManager()
	if sm == nil {
		return nil
	}
	token := extractBearerToken(c.Request())
	if token == "" {
		return nil
	}
	_, acct, err := sm.Validate(token)
	if err != nil {
		return nil
	}
	return acct
}

// callerIsAdmin answers whether the request carries an administrator's token.
//
// It exists for handlers that serve everybody but must serve an administrator
// more — where the alternative is two routes returning two shapes of the same
// thing. A nil session manager means authentication is not configured at all,
// the same condition every auth middleware treats as "let it through", so it
// answers true there: a handler must not redact its way to being useless on a
// box that has no accounts.
func (s *SystemControllerHandlers) callerIsAdmin(c *echo.Context) bool {
	if s.Controller.GetSessionManager() == nil {
		return true
	}
	acct := s.callingAccount(c)
	return acct != nil && acct.Admin
}

// grantRoutes maps a route to the grant that unlocks it, keyed by
// "METHOD PATH".
//
// This is the whole of what a grant buys. Adding a grant is an entry in
// account.AllGrants plus its routes here — no new middleware, no new field.
var grantRoutes = map[string]string{
	"GET /networks/peers":          account.GrantWireGuard,
	"POST /networks/peers/add":     account.GrantWireGuard,
	"POST /networks/peers/refresh": account.GrantWireGuard,

	"GET /gfeh":                     account.GrantGfeh,
	"GET /gfeh/principals":          account.GrantGfeh,
	"POST /gfeh/principals/add":     account.GrantGfeh,
	"POST /gfeh/principals/remove":  account.GrantGfeh,
	"GET /gfeh/grants":              account.GrantGfeh,
	"POST /gfeh/grants/add":         account.GrantGfeh,
	"POST /gfeh/grants/revoke":      account.GrantGfeh,
	"GET /gfeh/exposures":           account.GrantGfeh,
	"POST /gfeh/exposures/withdraw": account.GrantGfeh,
}

// grantCommonRoutes are reachable by any grant-holding account regardless of
// which grants it holds: authenticate, find out who you are, discover the
// networks you are scoped to, and fetch the CA so you can trust the box.
//
// Without these a grant is unusable — you cannot exercise one without first
// signing in — so they are common rather than duplicated into every grant.
var grantCommonRoutes = map[string]bool{
	"POST /account/authenticate": true,
	"GET /account/me":            true,
	"GET /networks":              true,
	"GET /dns/services":          true,
	"GET /tls/ca.crt":            true,
}

// Notably absent from grantRoutes: /gfeh/partitions/*, which stays admin-only.
// Provisioning a partition creates the root of a permission tree and allocates
// a btrfs subvolume with a quota — TOWNOS_CONTRACT.md reserves it for
// administrators, and gfeh's client branches on the 403 a non-admin gets. Also
// absent is GET /networks/peers/connected, which aggregates every account's
// peers across every network.

// grantAllows reports whether an account's grants admit a route.
func grantAllows(acct *account.Account, method, path string) bool {
	route := method + " " + path
	if grantCommonRoutes[route] {
		return true
	}
	needed, known := grantRoutes[route]
	return known && acct.HasGrant(needed)
}

// grantAllowlist is the fail-closed gate for an account that holds grants.
//
// A global middleware, so a route added tomorrow is denied to such an account
// by default — the safe direction — until somebody lists it in grantRoutes
// deliberately. Requests with no valid token, from an administrator, or from an
// ordinary account holding no grants pass straight through to the route's own
// auth middleware: a grant is additive authority for an account that exists to
// exercise it, and this confines only those.
func (s *SystemControllerHandlers) grantAllowlist(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		acct := s.callingAccount(c)
		if acct == nil || !acct.Restricted() {
			return next(c)
		}
		if !grantAllows(acct, c.Request().Method, c.Request().URL.Path) {
			return echo.NewHTTPError(403, i18n.T(s.getLocale(), i18n.MsgAuthNetworkOnlyRestricted))
		}
		return next(c)
	}
}

// requireGrant builds a middleware admitting administrators — who hold every
// grant — and accounts carrying the named one.
//
// The grant answers "may this caller do this at all", never "on which network":
// that needs the network named in the request body or query, which only the
// handler has parsed by then. Splitting it that way is deliberate — a
// middleware guessing where the network lives in each request shape would
// silently pass the one it could not find.
func (s *SystemControllerHandlers) requireGrant(grant, message string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if s.Controller.GetSessionManager() == nil {
				return next(c)
			}
			locale := s.getLocale()
			token := extractBearerToken(c.Request())
			if token == "" {
				return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthMissingToken))
			}
			_, acct, err := s.Controller.GetSessionManager().Validate(token)
			if err != nil {
				return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthInvalidSession))
			}
			if !acct.HasGrant(grant) {
				return echo.NewHTTPError(403, i18n.T(locale, message))
			}
			return next(c)
		}
	}
}

// requirePeerEnroll guards the peer enrollment endpoints. The wireguard grant
// is what admits a non-admin; per-network scope and per-peer ownership are
// enforced in the handler.
func (s *SystemControllerHandlers) requirePeerEnroll(next echo.HandlerFunc) echo.HandlerFunc {
	return s.requireGrant(account.GrantWireGuard, i18n.MsgAuthAdminRequired)(next)
}

// requireObjectStorage guards the mutating object-storage routes: a partition's
// principal forest, its grants, and the links published out of it.
//
// The grant deliberately stops there. It does NOT extend to
// /gfeh/partitions/*, which stays requireAdmin: provisioning a partition
// creates the root of a permission tree and allocates a btrfs subvolume with a
// quota, TOWNOS_CONTRACT.md reserves it for administrators, and gfeh's client
// branches on the 403 a non-admin gets. Running the users inside a partition
// that already exists is a smaller thing than deciding that it should.
func (s *SystemControllerHandlers) requireObjectStorage(next echo.HandlerFunc) echo.HandlerFunc {
	return s.requireGrant(account.GrantGfeh, i18n.MsgAuthObjectStorageRequired)(next)
}

func (s *SystemControllerHandlers) auditMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		am := s.Controller.GetAuditManager()
		if am == nil {
			return next(c)
		}

		path := c.Request().URL.Path

		excluded := map[string]bool{
			"/":                 true,
			"/account/sessions": true,
			"/account/me":       true,
			"/status/ping":      true,
			// The boot stub serves /boot-status; the full router does not.
			// A UI watching a self-update keeps the SSE stream open across
			// the handler swap, so the first request after the swap lands
			// on the full router and 404s. That is the expected end of the
			// stream, not an operator action — auditing it would file a
			// failed-action row on every successful refresh and inflate the
			// "recent audit errors" count the dashboard renders as a red
			// failure pill.
			"/boot-status":                  true,
			"/audit/log":                    true,
			"/storage":                      true,
			"/repository":                   true,
			"/packages":                     true,
			"/packages/by-repo":             true,
			"/packages/featured":            true,
			"/packages/installed":           true,
			"/packages/installed/info":      true,
			"/packages/install-preview":     true,
			"/packages/last-responses":      true,
			"/packages/responses":           true,
			"/packages/manifest":            true,
			"/packages/versions":            true,
			"/packages/questions":           true,
			"/packages/questions/identity":  true,
			"/packages/children":            true,
			"/packages/uninstalled-volumes": true,
			"/packages/upgrades":            true,
			"/systemd/units":                true,
			"/systemd/units-tree":           true,
			"/systemd/logs":                 true,
			"/systemd/logs/tail":            true,
			"/systemd/logs/tree":            true,
			"/systemd/logs/tree/tail":       true,
			"/account":                      true,
			"/settings":                     true,
			"/settings/get":                 true,
			"/pages":                        true,
			"/locales":                      true,
			"/vm-images":                    true,
			"/monitoring/status":             true,
			"/system-services":              true,
			"/dns/status":                   true,
			"/dns/records":                  true,
			"/dns/rbl/local":                true,
			"/dns/services":                 true,
			"/networks":                     true,
			"/networks/peers":               true,
			"/networks/peers/connected":     true,
			"/storage/package-volumes":      true,
			"/tls/ca.crt":                   true,
			// Object storage reads. /gfeh/partitions is a POST only because
			// gfeh's client sends it that way; it is a listing, not an action.
			"/gfeh/partitions": true,
			"/gfeh":            true,
			"/gfeh/principals": true,
			"/gfeh/grants":     true,
			"/gfeh/exposures":  true,
		}

		if excluded[path] {
			return next(c)
		}

		// Exclude read-only endpoints that share a path with write endpoints.
		if c.Request().Method == http.MethodGet && (path == "/dns/tld" || path == "/dns/rbl" || path == "/dns/dnsbl") {
			return next(c)
		}

		// Buffer the request body so we can capture it for audit detail
		// while still allowing the handler to read it.
		var detail string
		if c.Request().Body != nil {
			bodyBytes, err := io.ReadAll(c.Request().Body)
			if closeErr := c.Request().Body.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
			if err == nil && len(bodyBytes) > 0 {
				detail = sanitizeAuditDetail(bodyBytes)
				c.Request().Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
		}

		handlerErr := next(c)

		var acctName string
		if path == "/account/authenticate" {
			acctName = ""
		} else {
			token := extractBearerToken(c.Request())
			if token != "" && s.Controller.GetSessionManager() != nil {
				_, acct, err := s.Controller.GetSessionManager().Validate(token)
				if err == nil {
					acctName = acct.Username
				}
			}
		}

		action := account.RouteActions[path]

		entry := account.AuditEntry{
			Account:   acctName,
			Action:    action,
			Path:      path,
			Detail:    detail,
			Success:   handlerErr == nil,
			CreatedAt: time.Now().UTC(),
		}
		if handlerErr != nil {
			entry.Error = handlerErr.Error()
		}

		// Best-effort audit logging; don't fail the request if logging fails.
		if err := am.LogEntry(entry); err != nil {
			slog.Debug("audit log entry", "error", err)
		}

		return handlerErr
	}
}

// sanitizeAuditDetail parses a JSON request body, redacts sensitive fields,
// and returns a compact JSON string for audit logging.
func sanitizeAuditDetail(body []byte) string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	// A literal `null` unmarshals into a nil map WITHOUT an error, and
	// json.Marshal of a nil map renders the string "null" — so the one JSON
	// scalar that decodes into this type would otherwise reach the audit log as
	// a detail of "null" rather than no detail at all. Every other non-object
	// body (a bare array, a string, a number) fails the unmarshal above and is
	// already handled.
	if m == nil {
		return ""
	}

	redactSensitive(m)

	out, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(out)
}

// auditRedactedKeys are request-body fields whose value is a credential
// wherever it appears. Matched case-insensitively against the whole key, and
// against the key's suffix after the last underscore, so `smtp_password` and
// `registration_secret` are caught without a substring match that would also
// swallow innocuous names.
var auditRedactedKeys = map[string]bool{
	"password":     true,
	"passwd":       true,
	"secret":       true,
	"token":        true,
	"credential":   true,
	"credentials":  true,
	"private_key":  true,
	"privatekey":   true,
	"passphrase":   true,
	"apikey":       true,
	"api_key":      true,
	"access_token": true,
	"auth":         true,
}

// Deliberately NOT in that list: a bare "key". The suffix rule would then catch
// `public_key`, which POST /networks/peers/add carries — and a WireGuard public
// key is public by construction, while being the one field that says WHICH
// device was enrolled. Redacting it would hide the answer to the question an
// auditor opens the peer-enrollment log to ask, in exchange for protecting
// something that is not a secret. The compound spellings that ARE credentials
// (`private_key`, `api_key`, `apikey`) are listed explicitly instead.

// auditOpaqueKeys are fields whose *entire subtree* is redacted rather than
// walked.
//
// `responses` is a package's question answers keyed by the package author's
// names, so there is no key vocabulary to match on: a Postgres package calls it
// `dbpass`, a Synapse one `registration_secret`, a Plex one `plextoken`. The
// values are exactly the credentials the audit log must not become a copy of,
// and the audit entry's job is to record that an install happened and with
// which package — not to reproduce its inputs.
// Only `responses`. An account update's `fields` is deliberately NOT here: its
// keys are a known, fixed vocabulary, so `password` inside it is caught by name
// while `grants`, `networks`, and `real_name` survive — and a grant change is
// exactly the thing an auditor is reading this log to find.
var auditOpaqueKeys = map[string]bool{
	"responses": true,
}

const auditRedacted = "[REDACTED]"

// redactSensitive walks a decoded JSON body and masks credential-bearing
// values in place.
//
// The previous version deleted a key literally named "password" from the
// top-level map and from nested maps. That left every other spelling
// (`smtp_password`, `token`), every credential inside an array, and — most
// of all — the `responses` map of a package install, which is where the
// generated `type: secret` answers live. Masking rather than deleting is
// deliberate: an audit reader should see that a field was present and withheld,
// not be unable to tell it from a request that never carried one.
func redactSensitive(m map[string]any) {
	for k, v := range m {
		lower := strings.ToLower(k)
		if auditOpaqueKeys[lower] {
			m[k] = auditRedacted
			continue
		}
		if auditRedactedKeys[lower] || auditRedactedKeys[keySuffix(lower)] {
			m[k] = auditRedacted
			continue
		}
		redactSensitiveValue(v)
	}
}

// keySuffix returns the part of a key after its last underscore, so
// `smtp_password` matches `password`. A key with no underscore returns itself.
func keySuffix(key string) string {
	if idx := strings.LastIndex(key, "_"); idx >= 0 && idx+1 < len(key) {
		return key[idx+1:]
	}
	return key
}

// redactSensitiveValue recurses into maps and arrays. Arrays matter: a body
// carrying a list of objects hid every credential in them from the old walker.
func redactSensitiveValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		redactSensitive(t)
	case []any:
		for _, item := range t {
			redactSensitiveValue(item)
		}
	}
}
