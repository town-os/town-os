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

	acct, err := s.Controller.GetAccountManager().Authenticate(req.Username, req.Password)
	if err != nil {
		return echo.NewHTTPError(401, i18n.T(s.getLocale(), i18n.MsgAuthInvalidCredentials))
	}

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

// wireGuardAllowedRoutes is the fail-closed allowlist for WireGuard-only
// accounts, keyed by "METHOD PATH". Such an account may reach ONLY these
// endpoints — the ones needed to authenticate, discover the network, fetch the
// CA, and enroll/refresh a peer. Everything else is denied. Keep this list
// minimal: it is the entire attack surface a scoped account has against the
// control plane, and the portal holds a live tunnel into the overlay.
var wireGuardAllowedRoutes = map[string]bool{
	"POST /account/authenticate":   true,
	"GET /account/me":              true,
	"GET /networks":                true,
	"GET /networks/peers":          true,
	"POST /networks/peers/add":     true,
	"POST /networks/peers/refresh": true,
	"GET /dns/services":            true,
	"GET /tls/ca.crt":              true,
}

func wireGuardAllowedPath(method, path string) bool {
	return wireGuardAllowedRoutes[method+" "+path]
}

// wireGuardAllowlist is the fail-closed gate for WireGuard-only accounts. It is
// a global middleware so that a newly added route is denied to WireGuard
// accounts by default — the safe direction — until it is explicitly added to
// wireGuardAllowedRoutes. Requests with no valid token, and requests from normal
// or admin accounts, pass straight through to the route's own auth middleware;
// only an authenticated WireGuard account is constrained here.
func (s *SystemControllerHandlers) wireGuardAllowlist(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		acct := s.callingAccount(c)
		if acct == nil || !acct.WireGuard {
			return next(c)
		}
		if !wireGuardAllowedPath(c.Request().Method, c.Request().URL.Path) {
			return echo.NewHTTPError(403, i18n.T(s.getLocale(), i18n.MsgAuthWireGuardRestricted))
		}
		return next(c)
	}
}

// requirePeerEnroll guards the peer enrollment endpoints (peers/add,
// peers/refresh). It admits admins and WireGuard-only accounts — whose
// per-network scope and per-peer ownership are enforced in the handler — but
// rejects a plain non-admin account, so peer management stays privileged. The
// global wireGuardAllowlist has already confined a WireGuard account to these
// routes by the time this runs.
func (s *SystemControllerHandlers) requirePeerEnroll(next echo.HandlerFunc) echo.HandlerFunc {
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
		if !acct.Admin && !acct.WireGuard {
			return echo.NewHTTPError(403, i18n.T(locale, i18n.MsgAuthAdminRequired))
		}
		return next(c)
	}
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
			"/storage/package-volumes":      true,
			"/tls/ca.crt":                   true,
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

	redactSensitive(m)

	out, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(out)
}

// redactSensitive recursively removes keys named "password" from a map.
func redactSensitive(m map[string]any) {
	delete(m, "password")
	for _, v := range m {
		if nested, ok := v.(map[string]any); ok {
			redactSensitive(nested)
		}
	}
}
