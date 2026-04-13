package systemcontroller

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// getTLSCA serves the local CA certificate in PEM form so users can trust
// it once in their OS/browser store. The endpoint is unauthenticated — the
// CA certificate is a public object by definition (it is handed to every
// TLS client during the handshake) — and carries application/x-pem-file so
// Chrome/Firefox import dialogs recognize it. Returns 404 when no CA has
// been generated yet (e.g. btrfs not mounted, EnsureCA failed at boot).
func (s *SystemControllerHandlers) getTLSCA(c *echo.Context) error {
	ca := s.Controller.GetTLSCA()
	if ca == nil || len(ca.CertPEM) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "tls ca not configured")
	}
	c.Response().Header().Set("Content-Type", "application/x-pem-file")
	c.Response().Header().Set("Content-Disposition", `attachment; filename="town-os-ca.crt"`)
	c.Response().WriteHeader(http.StatusOK)
	if _, err := c.Response().Write(ca.CertPEM); err != nil {
		return err
	}
	return nil
}
