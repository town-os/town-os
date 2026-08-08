package systemcontroller

import (
	"encoding/json"
	"fmt"
	"strings"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"github.com/labstack/echo/v5"
)

// RblProviderDTO is the JSON shape of a single RBL/DNSBL provider.
type RblProviderDTO struct {
	Zone    string `json:"zone"`
	Enabled bool   `json:"enabled"`
}

// RblConfigRequest is the request body for POST /dns/rbl and POST /dns/dnsbl.
type RblConfigRequest struct {
	Enabled   bool             `json:"enabled"`
	Providers []RblProviderDTO `json:"providers"`
}

// RblConfigResponse is the response for GET /dns/rbl and GET /dns/dnsbl.
type RblConfigResponse struct {
	Enabled   bool             `json:"enabled"`
	Providers []RblProviderDTO `json:"providers"`
}

// LocalRblEntryDTO is the JSON shape of a single local RBL blocklist entry.
type LocalRblEntryDTO struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// AddLocalRblEntryRequest is the request body for POST /dns/rbl/local/add.
type AddLocalRblEntryRequest struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// RemoveLocalRblEntryRequest is the request body for POST /dns/rbl/local/remove.
type RemoveLocalRblEntryRequest struct {
	Name string `json:"name"`
}

// DnsblAllowlistEntryDTO is the JSON shape of a single DNSBL allowlist entry —
// a name exempted from the name-based blocklist check.
type DnsblAllowlistEntryDTO struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// AddDnsblAllowlistEntryRequest is the request body for
// POST /dns/dnsbl/allowlist/add.
type AddDnsblAllowlistEntryRequest struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// RemoveDnsblAllowlistEntryRequest is the request body for
// POST /dns/dnsbl/allowlist/remove.
type RemoveDnsblAllowlistEntryRequest struct {
	Name string `json:"name"`
}

func rblProvidersToDTO(providers []*upstream.RblConfig) []RblProviderDTO {
	out := make([]RblProviderDTO, 0, len(providers))
	for _, p := range providers {
		if p == nil {
			continue
		}
		out = append(out, RblProviderDTO{Zone: p.Zone, Enabled: p.Enabled})
	}
	return out
}

func dnsblProvidersToDTO(providers []*upstream.DnsblConfig) []RblProviderDTO {
	out := make([]RblProviderDTO, 0, len(providers))
	for _, p := range providers {
		if p == nil {
			continue
		}
		out = append(out, RblProviderDTO{Zone: p.Zone, Enabled: p.Enabled})
	}
	return out
}

// validateProviderDTOs validates and normalizes (trim + lowercase) a list of
// provider zones, returning the cleaned zones.
func validateProviderDTOs(providers []RblProviderDTO) ([]RblProviderDTO, error) {
	cleaned := make([]RblProviderDTO, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		zone := strings.ToLower(strings.TrimSpace(p.Zone))
		if err := ValidateRblZone(zone); err != nil {
			return nil, err
		}
		if _, dup := seen[zone]; dup {
			return nil, fmt.Errorf("duplicate RBL zone %q", zone)
		}
		seen[zone] = struct{}{}
		cleaned = append(cleaned, RblProviderDTO{Zone: zone, Enabled: p.Enabled})
	}
	return cleaned, nil
}

// getRblConfig handles GET /dns/rbl.
func (s *SystemControllerHandlers) getRblConfig(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}
	status, err := rc.GetRblConfig(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("get rbl config: %v", err))
	}
	return c.JSON(200, RblConfigResponse{
		Enabled:   status.Enabled,
		Providers: rblProvidersToDTO(status.Providers),
	})
}

// setRblConfig handles POST /dns/rbl.
func (s *SystemControllerHandlers) setRblConfig(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req RblConfigRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	cleaned, err := validateProviderDTOs(req.Providers)
	if err != nil {
		return echo.NewHTTPError(400, err.Error())
	}

	providers := make([]*upstream.RblConfig, 0, len(cleaned))
	for _, p := range cleaned {
		providers = append(providers, &upstream.RblConfig{Zone: p.Zone, Enabled: p.Enabled})
	}

	if err := rc.SetRblConfig(c.Request().Context(), req.Enabled, providers); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("set rbl config: %v", err))
	}
	return c.JSON(200, RblConfigResponse{Enabled: req.Enabled, Providers: cleaned})
}

// getDnsblConfig handles GET /dns/dnsbl.
func (s *SystemControllerHandlers) getDnsblConfig(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}
	status, err := rc.GetDnsblConfig(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("get dnsbl config: %v", err))
	}
	return c.JSON(200, RblConfigResponse{
		Enabled:   status.Enabled,
		Providers: dnsblProvidersToDTO(status.Providers),
	})
}

// setDnsblConfig handles POST /dns/dnsbl.
func (s *SystemControllerHandlers) setDnsblConfig(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req RblConfigRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	cleaned, err := validateProviderDTOs(req.Providers)
	if err != nil {
		return echo.NewHTTPError(400, err.Error())
	}

	providers := make([]*upstream.DnsblConfig, 0, len(cleaned))
	for _, p := range cleaned {
		providers = append(providers, &upstream.DnsblConfig{Zone: p.Zone, Enabled: p.Enabled})
	}

	if err := rc.SetDnsblConfig(c.Request().Context(), req.Enabled, providers); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("set dnsbl config: %v", err))
	}
	return c.JSON(200, RblConfigResponse{Enabled: req.Enabled, Providers: cleaned})
}

// listLocalRblEntries handles GET /dns/rbl/local.
func (s *SystemControllerHandlers) listLocalRblEntries(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}
	entries, err := rc.ListLocalRblEntries(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("list local rbl entries: %v", err))
	}
	out := make([]LocalRblEntryDTO, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		out = append(out, LocalRblEntryDTO{Name: e.Name, Reason: e.Reason})
	}
	return c.JSON(200, out)
}

// addLocalRblEntry handles POST /dns/rbl/local/add.
func (s *SystemControllerHandlers) addLocalRblEntry(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req AddLocalRblEntryRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if err := ValidateLocalRblName(name); err != nil {
		return echo.NewHTTPError(400, err.Error())
	}

	if err := rc.AddLocalRblEntry(c.Request().Context(), &upstream.LocalRblEntry{
		Name:   name,
		Reason: strings.TrimSpace(req.Reason),
	}); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("add local rbl entry: %v", err))
	}
	return c.JSON(200, map[string]string{"status": "ok", "name": name})
}

// removeLocalRblEntry handles POST /dns/rbl/local/remove.
func (s *SystemControllerHandlers) removeLocalRblEntry(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req RemoveLocalRblEntryRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if name == "" {
		return echo.NewHTTPError(400, "name must not be empty")
	}

	if err := rc.RemoveLocalRblEntry(c.Request().Context(), name); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("remove local rbl entry: %v", err))
	}
	return c.JSON(200, map[string]string{"status": "ok", "name": name})
}

// trimDNSRoot removes a single trailing root dot.
//
// Rolodex normalizes an allowlist name into fully-qualified form on the way in
// and hands it back that way ("cdn.example.com."), which is a storage detail of
// its zone-suffix matching. The local blocklist it sits beside stores names
// verbatim, so without this the Allow Lists table would render a trailing dot
// that the Blocklists table does not, on names the operator typed identically —
// which reads as a bug in whichever tab you looked at second. Purely
// presentational: rolodex normalizes whatever name it is given, so a removal
// matches with or without the dot.
func trimDNSRoot(name string) string {
	if name == "." {
		return name
	}
	return strings.TrimSuffix(name, ".")
}

// listDnsblAllowlistEntries handles GET /dns/dnsbl/allowlist.
func (s *SystemControllerHandlers) listDnsblAllowlistEntries(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}
	entries, err := rc.ListDnsblAllowlistEntries(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("list dnsbl allowlist entries: %v", err))
	}
	out := make([]DnsblAllowlistEntryDTO, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		out = append(out, DnsblAllowlistEntryDTO{Name: trimDNSRoot(e.Name), Reason: e.Reason})
	}
	return c.JSON(200, out)
}

// addDnsblAllowlistEntry handles POST /dns/dnsbl/allowlist/add.
func (s *SystemControllerHandlers) addDnsblAllowlistEntry(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req AddDnsblAllowlistEntryRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	name := trimDNSRoot(strings.ToLower(strings.TrimSpace(req.Name)))
	if err := ValidateDnsblAllowlistName(name); err != nil {
		return echo.NewHTTPError(400, err.Error())
	}

	if err := rc.AddDnsblAllowlistEntry(c.Request().Context(), &upstream.DnsblAllowlistEntry{
		Name:   name,
		Reason: strings.TrimSpace(req.Reason),
	}); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("add dnsbl allowlist entry: %v", err))
	}
	return c.JSON(200, map[string]string{"status": "ok", "name": name})
}

// removeDnsblAllowlistEntry handles POST /dns/dnsbl/allowlist/remove.
//
// The name is only normalized, never re-validated: a row that predates a
// validation change must still be removable.
func (s *SystemControllerHandlers) removeDnsblAllowlistEntry(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req RemoveDnsblAllowlistEntryRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	name := trimDNSRoot(strings.ToLower(strings.TrimSpace(req.Name)))
	if name == "" {
		return echo.NewHTTPError(400, "name must not be empty")
	}

	if err := rc.RemoveDnsblAllowlistEntry(c.Request().Context(), name); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("remove dnsbl allowlist entry: %v", err))
	}
	return c.JSON(200, map[string]string{"status": "ok", "name": name})
}
