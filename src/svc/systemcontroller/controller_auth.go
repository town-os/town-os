package systemcontroller

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"github.com/labstack/echo/v5"
)

type AuthenticateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"` //nolint:gosec // G117: expected field name
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
		return echo.NewHTTPError(401, err.Error())
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
	token := extractBearerToken(c.Request())
	if token == "" {
		return echo.NewHTTPError(401, "missing authorization token")
	}

	sess, _, err := s.Controller.GetSessionManager().Validate(token)
	if err != nil {
		return echo.NewHTTPError(401, "invalid session")
	}

	sessions, err := s.Controller.GetSessionManager().List(sess.Username)
	if err != nil {
		return err
	}

	return c.JSON(200, sessions)
}

func (s *SystemControllerHandlers) sessionUsername(c *echo.Context) error {
	token := extractBearerToken(c.Request())
	if token == "" {
		return echo.NewHTTPError(401, "missing authorization token")
	}

	sess, _, err := s.Controller.GetSessionManager().Validate(token)
	if err != nil {
		return echo.NewHTTPError(401, "invalid session")
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

		token := extractBearerToken(c.Request())
		if token == "" {
			return echo.NewHTTPError(401, "missing authorization token")
		}

		_, acct, err := s.Controller.GetSessionManager().Validate(token)
		if err != nil {
			return echo.NewHTTPError(401, "invalid session")
		}

		if !acct.Admin {
			return echo.NewHTTPError(403, "admin access required")
		}

		return next(c)
	}
}

func (s *SystemControllerHandlers) requireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.Controller.GetSessionManager() == nil {
			return next(c)
		}

		token := extractBearerToken(c.Request())
		if token == "" {
			return echo.NewHTTPError(401, "missing authorization token")
		}

		_, _, err := s.Controller.GetSessionManager().Validate(token)
		if err != nil {
			return echo.NewHTTPError(401, "invalid session")
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
			"/account/sessions":             true,
			"/account/me":                   true,
			"/status/ping":                  true,
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
			"/packages/versions":            true,
			"/packages/questions":           true,
			"/packages/questions/identity":  true,
			"/packages/children":            true,
			"/packages/uninstalled-volumes": true,
			"/packages/upgrades":            true,
			"/systemd/units":                true,
			"/systemd/logs":                 true,
			"/systemd/logs/tail":            true,
			"/account":                      true,
			"/settings":                     true,
			"/settings/get":                 true,
			"/pages":                        true,
			"/monitoring/status":             true,
		}

		if excluded[path] || strings.HasPrefix(path, "/monitoring/grafana") {
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
