package systemcontroller

import (
	"encoding/json"
	"strconv"

	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/packages"
	"github.com/labstack/echo/v5"
)

type SetSettingRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type GetSettingRequest struct {
	Key string `json:"key"`
}

// byteValueSettings are setting keys whose values represent byte counts
// and should be normalized through ParseBytes so that human-readable
// strings like "500GB" are stored as numeric byte values.
var byteValueSettings = map[string]bool{
	"default_quota":    true,
	"max_archive_size": true,
}

func (s *SystemControllerHandlers) getSettings(c *echo.Context) error {
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return c.JSON(200, map[string]string{})
	}

	settings, err := mgr.List()
	if err != nil {
		return err
	}

	return c.JSON(200, settings)
}

func (s *SystemControllerHandlers) getSetting(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := GetSettingRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	locale := s.getLocale()
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return echo.NewHTTPError(404, i18n.T(locale, i18n.MsgSettingNotFound, req.Key))
	}

	value, err := mgr.Get(req.Key)
	if err != nil {
		return echo.NewHTTPError(404, i18n.T(locale, i18n.MsgSettingNotFound, req.Key))
	}

	return c.JSON(200, map[string]string{"key": req.Key, "value": value})
}

func (s *SystemControllerHandlers) setSetting(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := SetSettingRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	locale := s.getLocale()
	if req.Key == "" {
		return echo.NewHTTPError(400, i18n.T(locale, i18n.MsgSettingKeyRequired))
	}

	value := req.Value

	if byteValueSettings[req.Key] {
		b, err := packages.ParseBytes(value)
		if err != nil {
			return echo.NewHTTPError(400, i18n.T(locale, i18n.MsgSettingInvalidBytes, req.Key, err))
		}
		value = strconv.FormatUint(b, 10)
	}

	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return echo.NewHTTPError(500, i18n.T(locale, i18n.MsgSettingsMgrMissing))
	}

	if err := mgr.Set(req.Key, value); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}
