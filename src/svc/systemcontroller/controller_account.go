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
	// WireGuard, when true, creates a WireGuard-only account scoped to Networks
	// instead of a normal account. Admin is ignored in that case — the two are
	// mutually exclusive and CreateWireGuard never grants admin.
	WireGuard bool     `json:"wireguard"`
	Networks  []string `json:"networks"`
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

		// Service accounts excluded: see isServiceAccount. Counting the gfeh
		// daemon's account here means the first human account can never be
		// created, because creating it is what would produce the token this
		// branch then demands.
		if hasHumanAdmin(accounts) {
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
	if req.WireGuard {
		acct, err = s.Controller.GetAccountManager().CreateWireGuard(req.Username, req.Password, req.Email, req.Phone, req.RealName, req.Networks)
	} else {
		acct, err = s.Controller.GetAccountManager().Create(req.Username, req.Password, req.Email, req.Phone, req.RealName, req.Admin)
	}
	if err != nil {
		if errors.Is(err, account.ErrDuplicateUsername) {
			return echo.NewHTTPError(400, i18n.T(locale, i18n.MsgAccountCreateFailed))
		}
		if errors.Is(err, account.ErrWireGuardNoNetworks) || errors.Is(err, account.ErrInvalidNetworkName) {
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

	acct, err := s.Controller.GetAccountManager().Update(req.Username, req.Fields)
	if err != nil {
		return err
	}

	// An SMB credential is rendered into every partition's gfehd.yaml, so
	// enrolling or withdrawing one has to reach the daemons. Without this the
	// operator sets a password, nothing happens, and the change appears to
	// have been lost until the next reboot.
	if req.Fields.SMBPassword != nil {
		s.reconcileGfeh(c.Request().Context())
	}

	return c.JSON(200, acct)
}

func (s *SystemControllerHandlers) disableAccount(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := DisableAccountRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	// Refused rather than silently undone. The object-storage daemons
	// authenticate as this account, so disabling it breaks provisioning on
	// every partition — and the next reconcile puts it back, which would make
	// the button appear to do nothing at all. Saying so is better than either.
	if req.Username == GfehServiceAccount {
		return echo.NewHTTPError(400, i18n.T(s.getLocale(), i18n.MsgGfehServiceAccountProtected))
	}

	if err := s.Controller.GetAccountManager().Disable(req.Username); err != nil {
		return err
	}

	// A disabled account must not be able to mount a share either, and the
	// credential table is rendered per partition rather than checked per
	// request -- so the daemons have to be told.
	s.reconcileGfeh(c.Request().Context())

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

	// The other half of disable: a re-enabled account gets its SMB credential
	// back in the partitions it is scoped to.
	s.reconcileGfeh(c.Request().Context())

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
