package systemcontroller

import (
	"encoding/json"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/i18n"
	"github.com/labstack/echo/v5"
)

func (s *SystemControllerHandlers) listAuditLog(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	var opts account.AuditListOptions

	if err := de.Decode(&opts); err != nil {
		return err
	}

	am := s.Controller.GetAuditManager()
	if am == nil {
		return echo.NewHTTPError(500, i18n.T(s.getLocale(), i18n.MsgAuditNotConfigured))
	}

	page, err := am.List(c.Request().Context(), opts)
	if err != nil {
		return err
	}

	return c.JSON(200, page)
}
