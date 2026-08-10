package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
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
// settingsValidators maps setting keys to validation functions that are
// called before the value is persisted via the generic settings API.
var settingsValidators = map[string]func(string) error{
	"dns_tld":              ValidateTLD,
	"dns_resolution_mode":  ValidateDNSResolutionMode,
	"dns_local_forwarders": ValidateBool,
}

// ValidateBool accepts what strconv.ParseBool accepts, which is what every
// consumer of a boolean setting uses to read it back.
func ValidateBool(v string) error {
	if _, err := strconv.ParseBool(strings.TrimSpace(v)); err != nil {
		return errors.New("must be true or false")
	}
	return nil
}

// ValidateDNSResolutionMode accepts only the modes rolodex understands.
// Anything else would be written into rolodex.yml and refused at startup,
// taking DNS down for the whole box.
func ValidateDNSResolutionMode(v string) error {
	if !rolodex.ValidResolutionMode(v) {
		return fmt.Errorf("must be %q, %q, or %q",
			rolodex.ResolutionModeAuto,
			rolodex.ResolutionModeRecursive,
			rolodex.ResolutionModeForward)
	}
	return nil
}

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

	if validator, ok := settingsValidators[req.Key]; ok {
		if err := validator(value); err != nil {
			return echo.NewHTTPError(400, fmt.Sprintf("invalid value for %q: %v", req.Key, err))
		}
	}

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

	// When the monitoring backend changes, restart the monitoring UI
	// service immediately so the switch takes effect without a reboot.
	if req.Key == "monitoring_backend" {
		if err := s.Controller.RefreshMonitoringBackend(c.Request().Context(), value); err != nil {
			return echo.NewHTTPError(500, fmt.Sprintf("failed to refresh monitoring backend: %v", err))
		}
	}

	// Rewrite rolodex.yml and restart rolodex so a resolution-mode change takes
	// effect immediately rather than at the next boot.
	if req.Key == "dns_resolution_mode" {
		if err := s.Controller.RefreshDNSResolutionMode(c.Request().Context(), value); err != nil {
			return echo.NewHTTPError(500, fmt.Sprintf("failed to apply dns resolution mode: %v", err))
		}
	}

	// Same for the forwarder list: repoint the local tier at the host's own
	// resolvers (or back at the public defaults) and restart rolodex now. The
	// value is already known parseable — ValidateBool ran above.
	if req.Key == "dns_local_forwarders" {
		enabled, parseErr := strconv.ParseBool(strings.TrimSpace(value))
		if parseErr != nil {
			return echo.NewHTTPError(400, fmt.Sprintf("invalid value for %q: must be true or false", req.Key))
		}
		if err := s.Controller.RefreshDNSLocalForwarders(c.Request().Context(), enabled); err != nil {
			return echo.NewHTTPError(500, fmt.Sprintf("failed to apply dns local forwarders: %v", err))
		}
	}

	c.Response().WriteHeader(200)
	return nil
}
