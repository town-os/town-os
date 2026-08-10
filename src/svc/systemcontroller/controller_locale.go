package systemcontroller

import (
	"gitea.com/town-os/town-os/src/i18n"
	"github.com/labstack/echo/v5"
)

// LocaleListResponse is returned by the locales endpoint.
type LocaleListResponse struct {
	// Current is the active locale code configured in system settings.
	Current string `json:"current"`

	// Populated lists locale codes that have translations available.
	Populated []string `json:"populated"`

	// CommonLanguages is a curated list of widely spoken languages
	// presented in their native scripts for language selection.
	CommonLanguages []i18n.Locale `json:"common_languages"`

	// ExtendedLocales is a comprehensive list of country-specific
	// locale codes for users whose needs are not met by the common list.
	ExtendedLocales []i18n.Locale `json:"extended_locales"`
}

// listLocales returns the list of supported locales and the current setting (GET /locales).
func (s *SystemControllerHandlers) listLocales(c *echo.Context) error {
	current := i18n.DefaultLocale
	if mgr := s.Controller.GetSettingsManager(); mgr != nil {
		if val, err := mgr.Get(c.Request().Context(), "locale"); err == nil && val != "" {
			current = val
		}
	}

	resp := LocaleListResponse{
		Current:         current,
		Populated:       i18n.PopulatedLocales(),
		CommonLanguages: i18n.CommonLanguages,
		ExtendedLocales: i18n.ExtendedLocales,
	}

	return c.JSON(200, resp)
}

// getLocale reads the system locale from settings, falling back to the default.
//
// It uses the server-scoped context rather than taking one, and that is a
// deliberate exception to the rule that a handler passes its request context
// down. getLocale is called from ~55 sites, almost all of them building an
// error message, and threading a context through every one of them would be a
// large diff for a lookup whose only failure mode is "use English".
//
// The request context would also be the wrong context to bound it with: the
// one case where it is already cancelled is a client that hung up, and then
// the message being formatted is not going to be delivered either way — so
// cancelling here converts a rendered-but-undelivered message into a
// default-locale-but-undelivered one, which is no better and loses the locale
// on any path that does still write a response.
//
// s.ctx is nil in tests that construct SystemControllerHandlers directly;
// account.queryCtx treats that as context.Background(), so those keep working.
func (s *SystemControllerHandlers) getLocale() string {
	if mgr := s.Controller.GetSettingsManager(); mgr != nil {
		if val, err := mgr.Get(s.ctx, "locale"); err == nil && val != "" {
			return val
		}
	}
	return i18n.DefaultLocale
}
