package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/i18n"
	"github.com/labstack/echo/v5"
)

type CreateAccountRequest struct {
	Username string `json:"username"`
	Password string `json:"password"` //nolint:gosec // G117 -- request field, not hardcoded
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	RealName string `json:"real_name"`
	Admin    bool   `json:"admin"`
	// Grants are the named capabilities the new account holds, from
	// account.AllGrants. Non-empty means a non-admin account confined to the
	// routes those grants unlock, scoped to Networks; Admin is ignored then,
	// since an administrator holds every grant already.
	Grants   []string `json:"grants"`
	Networks []string `json:"networks"`
}

type GetAccountRequest struct {
	Username string `json:"username"`
}

type UpdateAccountRequest struct {
	Username string               `json:"username"`
	Fields   account.UpdateFields `json:"fields"`
}

type DisableAccountRequest struct {
	Username string `json:"username"`
}

type EnableAccountRequest struct {
	Username string `json:"username"`
}

// --- Account handlers ---

func (s *SystemControllerHandlers) createAccount(c *echo.Context) error {
	locale := s.getLocale()
	sessMgr := s.Controller.GetSessionManager()
	if sessMgr != nil {
		accounts, err := s.Controller.GetAccountManager().List()
		if err != nil {
			return fmt.Errorf("%s: %w", i18n.T(locale, i18n.MsgAccountListError), err)
		}

		// Bootstrap: with no administrator on the box yet, the first one is
		// created without a token, because there is no account to obtain one
		// from. Shares hasEnabledAdmin with the setup flag in /status/ping so
		// the two can never disagree about whether that window is open.
		if hasEnabledAdmin(accounts) {
			token := extractBearerToken(c.Request())
			if token == "" {
				return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthMissingToken))
			}
			_, acct, err := sessMgr.Validate(token)
			if err != nil {
				return echo.NewHTTPError(401, i18n.T(locale, i18n.MsgAuthInvalidSession))
			}
			if !acct.Admin {
				return echo.NewHTTPError(403, i18n.T(locale, i18n.MsgAuthAdminRequired))
			}
		}
	}

	de := json.NewDecoder(c.Request().Body)
	req := CreateAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	var acct *account.Account
	var err error
	if len(req.Grants) > 0 {
		acct, err = s.Controller.GetAccountManager().CreateGranted(req.Username, req.Password, req.Email, req.Phone, req.RealName, req.Grants, req.Networks)
	} else {
		acct, err = s.Controller.GetAccountManager().Create(req.Username, req.Password, req.Email, req.Phone, req.RealName, req.Admin)
	}
	if err != nil {
		if errors.Is(err, account.ErrDuplicateUsername) {
			return echo.NewHTTPError(400, i18n.T(locale, i18n.MsgAccountCreateFailed))
		}
		if errors.Is(err, account.ErrGrantsNoNetworks) || errors.Is(err, account.ErrInvalidNetworkName) ||
			errors.Is(err, account.ErrInvalidGrant) {
			return echo.NewHTTPError(400, err.Error())
		}
		return err
	}

	return c.JSON(200, acct)
}

func (s *SystemControllerHandlers) getAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := GetAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	acct, err := s.Controller.GetAccountManager().Get(req.Username)
	if err != nil {
		return err
	}

	return c.JSON(200, acct)
}

func (s *SystemControllerHandlers) updateAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := UpdateAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Fields.Admin != nil {
		return echo.NewHTTPError(403, i18n.T(s.getLocale(), i18n.MsgAccountAdminStatusImmutable))
	}

	// This route is requireAuth, not requireAdmin -- any authenticated account
	// may edit an account. That is fine for contact details and a password, and
	// would be a privilege escalation for the account kind: without this check a
	// normal user could POST /account/update on itself with
	// {"network_only":true,"networks":["home"]} and walk into that network's
	// partition user database and its peer enrollment. Which kind an account is
	// stays an administrator's decision.
	//
	// A nil session manager means authentication is not configured at all (the
	// same condition every auth middleware treats as "let it through"), so the
	// check applies only where there is a caller to identify.
	if (req.Fields.Grants != nil || req.Fields.Networks != nil) && s.Controller.GetSessionManager() != nil {
		caller := s.callingAccount(c)
		if caller == nil || !caller.Admin {
			return echo.NewHTTPError(403, i18n.T(s.getLocale(), i18n.MsgAuthAdminRequired))
		}
	}

	acct, err := s.Controller.GetAccountManager().Update(req.Username, req.Fields)
	if err != nil {
		return err
	}

	return c.JSON(200, acct)
}

func (s *SystemControllerHandlers) disableAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := DisableAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.Controller.GetAccountManager().Disable(req.Username); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) enableAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := EnableAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if err := s.Controller.GetAccountManager().Enable(req.Username); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listAccounts(c *echo.Context) error {
	accounts, err := s.Controller.GetAccountManager().List()
	if err != nil {
		return err
	}

	p := readListParams(c)
	accounts = filterSearch(accounts, p.Search)
	sortSlice(accounts, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(accounts, p.Limit, p.Offset))
}
