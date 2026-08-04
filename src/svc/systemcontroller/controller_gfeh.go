// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/i18n"
	"github.com/labstack/echo/v5"
)

// The UI's view of object storage.
//
// gfehd's administrative surface is bound to a Unix socket and never to a port,
// and it checks no credential — filesystem permissions on the socket are the
// whole access control. So a browser cannot reach it, and Town OS proxies:
// these handlers authenticate the caller the way every other route does, then
// speak to the partition over its socket.
//
// Deliberately separate from controller_gfeh_partitions.go. Those four paths
// are the contract gfeh's own Rust client speaks and gfeh's
// `make check-townos-sync` verifies; nothing here may shadow or extend them.

// GfehPartitionView is one partition as the UI sees it.
type GfehPartitionView struct {
	Network string `json:"network"`
	// TLD the partition's names are published under.
	TLD string `json:"tld"`
	// Quota in bytes; zero is unlimited.
	Quota uint64 `json:"quota"`
	// Running reports whether the daemon answered its health probe. A
	// partition that exists but does not answer is a real and distinct state:
	// its data is there, its names are not being published.
	Running bool `json:"running"`
	// Names is what the partition asked Town OS to publish, resolved to the
	// fully-qualified names actually served.
	Names []GfehNameView `json:"names"`
}

// GfehNameView is one published view of a partition.
type GfehNameView struct {
	View string `json:"view"`
	FQDN string `json:"fqdn"`
	Port uint16 `json:"port"`
	// HTTP reports whether the ingress fronts this view. SMB does not: it is
	// dialled directly on Port, which is why the UI has to tell them apart.
	HTTP bool `json:"http"`
}

// GfehPrincipalView is a member of a partition's ACL forest.
type GfehPrincipalView struct {
	Name    string   `json:"name"`
	Parent  string   `json:"parent,omitempty"`
	Ceiling []string `json:"ceiling"`
	// Account reports whether a Town OS account of this name exists, so the UI
	// can distinguish a projected account from a sub-principal created inside
	// gfeh.
	Account bool `json:"account"`
}

// GfehGrantView is one principal's authority over one subtree.
type GfehGrantView struct {
	ID          int64    `json:"id"`
	Principal   string   `json:"principal"`
	Path        string   `json:"path"`
	Perm        []string `json:"perm"`
	Inheritable bool     `json:"inheritable"`
}

// GfehExposureView is a published /f/<token> link.
type GfehExposureView struct {
	Token    string `json:"token"`
	Path     string `json:"path"`
	Filename string `json:"filename,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// Request bodies.
type (
	// GfehPrincipalRequest names an account to project into a partition, or a
	// principal to remove from one.
	GfehPrincipalRequest struct {
		Network   string `json:"network"`
		Principal string `json:"principal"`
	}
	// GfehGrantRequest grants a principal authority over a subtree.
	GfehGrantRequest struct {
		Network     string   `json:"network"`
		Principal   string   `json:"principal"`
		Path        string   `json:"path"`
		Perm        []string `json:"perm"`
		Inheritable bool     `json:"inheritable"`
	}
	// GfehRevokeRequest revokes a grant by row id.
	GfehRevokeRequest struct {
		Network string `json:"network"`
		ID      int64  `json:"id"`
	}
	// GfehExposureRequest withdraws a published link.
	GfehExposureRequest struct {
		Network string `json:"network"`
		Token   string `json:"token"`
	}
)

// gfehClientFor resolves the admin client for a partition: it checks the
// request names a network, that the caller is allowed on it, and only then that
// the partition is actually there.
//
// Every /gfeh/* route funnels through here — reads included — which is what
// makes the scope check unmissable. A network-only account scoped to `office`
// listing `home`'s principals would be an information leak of exactly the kind
// the scope exists to prevent, and a check bolted onto the mutating routes
// alone would have permitted it.
//
// **The order is load-bearing: shape, then authority, then existence.**
// Authorization must not depend on the state of the thing being addressed. With
// the lookup first, a caller who had no business asking about `home` learned
// whether `home` had a partition (404 vs 200) and whether its daemon was
// configured (503) — and got that answer as a *successful* refusal of a
// different kind, so nothing recorded that a scoped account had reached outside
// its scope at all. Authorizing first makes the refusal the same 403 whether
// object storage is up, down, or absent.
//
// The empty-network 400 stays ahead of the scope check so a malformed request
// gets the same answer for everybody: "" is in nobody's scope, and 403ing it
// would tell a scoped account its own request was a permissions problem when it
// was a typo.
//
// 404 and 403 remain distinguishable for a caller who IS allowed on the
// network, which is deliberate: the set of networks on the box is already
// readable through GET /networks (on the allowlist), so hiding it from an
// in-scope caller would cost clarity and conceal nothing.
func (s *SystemControllerHandlers) gfehClientFor(c *echo.Context, network string) (gfeh.Client, error) {
	locale := s.getLocale()

	network = strings.TrimSpace(network)
	if network == "" {
		return nil, echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgGfehNetworkRequired))
	}

	if err := s.requireNetworkScope(c, network); err != nil {
		return nil, err
	}

	reg := s.Controller.GetGfehRegistry()
	if reg == nil {
		return nil, echo.NewHTTPError(http.StatusServiceUnavailable, i18n.T(locale, i18n.MsgGfehNotConfigured))
	}
	client, ok := reg.Clients()[network]
	if !ok {
		return nil, echo.NewHTTPError(http.StatusNotFound, i18n.T(locale, i18n.MsgGfehPartitionNotFound))
	}
	return client, nil
}

// requireNetworkScope confines a non-admin caller to the networks it is scoped
// to.
//
// It *confines*; it does not grant. Whether a caller may do the thing at all is
// the route's own middleware: reads stay requireAuth and writes are
// requireObjectStorage. This adds the one question neither can answer — "which
// network?" — because the answer lives in the request body or query that only
// the handler has parsed. An administrator holds every grant on every network
// and is unaffected.
//
// A nil session manager means authentication is not configured at all — the
// same condition every auth middleware treats as "let it through" — and an
// unidentifiable caller has already been rejected by the route's own auth
// middleware before reaching any handler, so both fall through here rather than
// being denied a second time on weaker information.
//
// It confines the *restricted* accounts only — a non-admin holding at least one
// grant — because a stored network scope is what a grant is exercised against,
// and only such an account has one. An ordinary dashboard account holds no
// grants and therefore no scope, and an empty scope denies every network
// (deliberately, see Account.MayAdministerNetwork): applying it here would 403
// every read for every plain account, which is not a confinement but an
// accidental tightening of routes that are requireAuth on purpose. What such an
// account may do remains the route's own question — it cannot reach any of the
// mutators, which are requireObjectStorage.
func (s *SystemControllerHandlers) requireNetworkScope(c *echo.Context, network string) error {
	if s.Controller.GetSessionManager() == nil {
		return nil
	}
	acct := s.callingAccount(c)
	if acct == nil || !acct.Restricted() {
		return nil
	}
	if !acct.MayAdministerNetwork(network) {
		return echo.NewHTTPError(http.StatusForbidden, i18n.T(s.getLocale(), i18n.MsgAuthNetworkOnlyNetworkDenied))
	}
	return nil
}

// gfehStatus maps a gfeh admin-socket error onto an HTTP status.
//
// A partition that is not answering is 503 rather than 500: it is a service
// that is down, not a request that was wrong, and a UI can say so usefully.
func (s *SystemControllerHandlers) gfehStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gfeh.ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, gfeh.ErrAlreadyExists):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, gfeh.ErrBadRequest):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, gfeh.ErrUnavailable):
		return echo.NewHTTPError(http.StatusServiceUnavailable, i18n.T(s.getLocale(), i18n.MsgGfehNotConfigured))
	}
	return err
}

// listGfeh handles GET /gfeh: every partition, with its names and live state.
func (s *SystemControllerHandlers) listGfeh(c *echo.Context) error {
	reg := s.Controller.GetGfehRegistry()
	if reg == nil {
		// An empty list rather than an error: object storage being disabled is
		// a configuration, not a failure, and the UI renders "none" for it.
		return c.JSON(http.StatusOK, []GfehPartitionView{})
	}

	ctx := c.Request().Context()
	nm := s.Controller.GetNetworkManager()
	tld := reconcileDNSTLD(s.Controller.GetSettingsManager())
	quota := gfehPartitionQuota(s.Controller.GetSettingsManager())

	// The one /gfeh/* read that names no network, so gfehClientFor's scope check
	// cannot cover it: it enumerates them. A scoped account gets the partitions
	// its networks own and no rows for the rest -- omitted rather than rendered
	// as unavailable, since a partition it may not administer is not a partition
	// that is down.
	//
	// Restricted() is the same gate requireNetworkScope uses, and for the same
	// reason: a plain account holds no grants and so no scope, and filtering it
	// against an empty scope would render every partition invisible to every
	// ordinary account rather than confining anybody.
	caller := s.callingAccount(c)
	confined := caller != nil && caller.Restricted()

	out := make([]GfehPartitionView, 0, len(reg.Clients()))
	for network, client := range reg.Clients() {
		if confined && !caller.MayAdministerNetwork(network) {
			continue
		}
		view := GfehPartitionView{
			Network: network,
			TLD:     gfehNetworkTLD(nm, network, tld),
			Quota:   quota,
			Names:   []GfehNameView{},
		}

		if _, err := client.Health(ctx); err == nil {
			view.Running = true
		}

		filter := network
		if gfeh.IsDefaultNetwork(network) {
			filter = ""
		}
		for _, site := range collectGfehSites(ctx, reg, nm, tld, filter) {
			if site.Network != network {
				continue
			}
			view.Names = append(view.Names, GfehNameView{
				View: site.View,
				FQDN: site.FQDN,
				Port: site.Port,
				HTTP: site.HTTP,
			})
		}
		out = append(out, view)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Network < out[j].Network })
	return c.JSON(http.StatusOK, out)
}

// listGfehPrincipals handles GET /gfeh/principals?network=.
func (s *SystemControllerHandlers) listGfehPrincipals(c *echo.Context) error {
	client, err := s.gfehClientFor(c, c.Request().URL.Query().Get("network"))
	if err != nil {
		return err
	}

	principals, err := client.ListPrincipals(c.Request().Context())
	if err != nil {
		return s.gfehStatus(err)
	}

	// Mark which principals correspond to a Town OS account, so the UI can
	// tell a projected account from a sub-principal created inside gfeh.
	accounts := map[string]bool{}
	if am := s.Controller.GetAccountManager(); am != nil {
		if list, listErr := am.List(); listErr == nil {
			for _, a := range list {
				accounts[a.Username] = true
			}
		}
	}

	out := make([]GfehPrincipalView, 0, len(principals))
	for _, p := range principals {
		view := GfehPrincipalView{Name: p.Name, Ceiling: p.Ceiling, Account: accounts[p.Name]}
		if p.Parent != nil {
			view.Parent = *p.Parent
		}
		out = append(out, view)
	}
	return c.JSON(http.StatusOK, out)
}

// addGfehPrincipal handles POST /gfeh/principals/add (admin).
//
// Projects a Town OS account into the partition. No password is involved:
// creating a principal over the admin socket needs none, and Town OS already
// knows whether the account is an administrator — which is the only thing the
// ceiling depends on.
func (s *SystemControllerHandlers) addGfehPrincipal(c *echo.Context) error {
	var req GfehPrincipalRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	client, err := s.gfehClientFor(c, req.Network)
	if err != nil {
		return err
	}

	locale := s.getLocale()
	name := strings.TrimSpace(req.Principal)
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgGfehPrincipalRequired))
	}

	// The principal must name a real account. Creating one for a username that
	// does not exist would put a grant in the forest that nobody can ever
	// authenticate as, and it would never be cleaned up.
	am := s.Controller.GetAccountManager()
	if am == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, i18n.T(locale, i18n.MsgGfehNotConfigured))
	}
	acct, err := am.Get(name)
	if err != nil || acct == nil {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgGfehUnknownAccount))
	}

	created, err := client.CreatePrincipal(c.Request().Context(), gfeh.Principal{
		Name: acct.Username,
		// A Town OS administrator is a gfeh superuser; an ordinary account gets
		// an ordinary ceiling and no grants, which is deliberately useless
		// until somebody grants it something. Authenticating is not
		// authorization.
		Ceiling: gfeh.CeilingForAccount(acct.Admin),
	})
	if err != nil {
		return s.gfehStatus(err)
	}

	return c.JSON(http.StatusOK, GfehPrincipalView{
		Name:    created.Name,
		Ceiling: created.Ceiling,
		Account: true,
	})
}

// removeGfehPrincipal handles POST /gfeh/principals/remove (admin).
func (s *SystemControllerHandlers) removeGfehPrincipal(c *echo.Context) error {
	var req GfehPrincipalRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	client, err := s.gfehClientFor(c, req.Network)
	if err != nil {
		return err
	}
	if strings.TrimSpace(req.Principal) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(s.getLocale(), i18n.MsgGfehPrincipalRequired))
	}

	if err := client.DeletePrincipal(c.Request().Context(), req.Principal); err != nil {
		return s.gfehStatus(err)
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "ok"})
}

// listGfehGrants handles GET /gfeh/grants?network=&principal=.
//
// principal is required, because gfehd's handler requires it — an absent one is
// a 4xx there rather than "every grant", and answering differently here would
// make the proxy disagree with what it proxies.
func (s *SystemControllerHandlers) listGfehGrants(c *echo.Context) error {
	query := c.Request().URL.Query()
	client, err := s.gfehClientFor(c, query.Get("network"))
	if err != nil {
		return err
	}

	principal := strings.TrimSpace(query.Get("principal"))
	if principal == "" {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(s.getLocale(), i18n.MsgGfehPrincipalRequired))
	}

	grants, err := client.ListGrants(c.Request().Context(), principal)
	if err != nil {
		return s.gfehStatus(err)
	}

	out := make([]GfehGrantView, 0, len(grants))
	for _, g := range grants {
		out = append(out, GfehGrantView{
			ID: g.ID, Principal: g.Principal, Path: g.Path,
			Perm: g.Perm, Inheritable: g.Inheritable,
		})
	}
	return c.JSON(http.StatusOK, out)
}

// addGfehGrant handles POST /gfeh/grants/add (admin).
func (s *SystemControllerHandlers) addGfehGrant(c *echo.Context) error {
	var req GfehGrantRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	client, err := s.gfehClientFor(c, req.Network)
	if err != nil {
		return err
	}

	locale := s.getLocale()
	if strings.TrimSpace(req.Principal) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgGfehPrincipalRequired))
	}
	if strings.TrimSpace(req.Path) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgGfehPathRequired))
	}

	granted, err := client.CreateGrant(c.Request().Context(), gfeh.Grant{
		Principal:   req.Principal,
		Path:        req.Path,
		Perm:        req.Perm,
		Inheritable: req.Inheritable,
	})
	if err != nil {
		return s.gfehStatus(err)
	}

	// Answered with what was stored, not what was asked for: gfeh clamps a
	// grant to the principal's ceiling, and an administrator has to be able to
	// see that it was narrowed or they will believe they gave access nobody
	// has.
	return c.JSON(http.StatusOK, GfehGrantView{
		ID: granted.ID, Principal: granted.Principal, Path: granted.Path,
		Perm: granted.Perm, Inheritable: granted.Inheritable,
	})
}

// revokeGfehGrant handles POST /gfeh/grants/revoke (admin).
func (s *SystemControllerHandlers) revokeGfehGrant(c *echo.Context) error {
	var req GfehRevokeRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	client, err := s.gfehClientFor(c, req.Network)
	if err != nil {
		return err
	}

	if err := client.RevokeGrant(c.Request().Context(), req.ID); err != nil {
		return s.gfehStatus(err)
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "ok"})
}

// listGfehExposures handles GET /gfeh/exposures?network=.
func (s *SystemControllerHandlers) listGfehExposures(c *echo.Context) error {
	client, err := s.gfehClientFor(c, c.Request().URL.Query().Get("network"))
	if err != nil {
		return err
	}

	exposures, err := client.ListExposures(c.Request().Context())
	if err != nil {
		return s.gfehStatus(err)
	}

	out := make([]GfehExposureView, 0, len(exposures))
	for _, e := range exposures {
		view := GfehExposureView{Token: e.Token, Path: e.Path, Enabled: e.Enabled}
		if e.Filename != nil {
			view.Filename = *e.Filename
		}
		out = append(out, view)
	}
	return c.JSON(http.StatusOK, out)
}

// withdrawGfehExposure handles POST /gfeh/exposures/withdraw (admin).
func (s *SystemControllerHandlers) withdrawGfehExposure(c *echo.Context) error {
	var req GfehExposureRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	client, err := s.gfehClientFor(c, req.Network)
	if err != nil {
		return err
	}
	if strings.TrimSpace(req.Token) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(s.getLocale(), i18n.MsgGfehNameRequired))
	}

	if err := client.WithdrawExposure(c.Request().Context(), req.Token); err != nil {
		return s.gfehStatus(err)
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "ok"})
}
