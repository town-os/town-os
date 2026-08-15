package systemcontroller

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"github.com/labstack/echo/v5"
)

// BlocklistProviderDTO is the JSON shape of a single blocklist provider.
type BlocklistProviderDTO struct {
	Zone    string `json:"zone"`
	Enabled bool   `json:"enabled"`
	// RefusalCodes are the answers this provider gives to mean "I refused your
	// query" rather than "this is listed" — see [ValidateRefusalCodes]. Empty
	// means rolodex's built-in set; the single entry "none" switches detection
	// off. On the way out this is what is actually in effect, so an empty
	// configured list reads back as the built-in codes rather than as empty:
	// an operator has to be able to see what the box is really matching on.
	RefusalCodes []string `json:"refusal_codes,omitempty"`
	// RefusalCooldownSecs is how long this provider is taken out of the lookup
	// rotation after it refuses a query. 0 defers to the list-wide value.
	RefusalCooldownSecs uint32 `json:"refusal_cooldown_secs,omitempty"`
}

// RotatedProviderDTO reports a provider currently out of the lookup rotation
// because it refused a query — the operator-visible half of refusal handling,
// and the answer to "why did my blocklist go quiet".
type RotatedProviderDTO struct {
	Zone             string `json:"zone"`
	Code             string `json:"code"`
	SecondsRemaining uint32 `json:"seconds_remaining"`
}

// BlocklistConfigRequest is the request body for POST /dns/dnsbl.
type BlocklistConfigRequest struct {
	Enabled   bool                   `json:"enabled"`
	Providers []BlocklistProviderDTO `json:"providers"`
	// RefusalCooldownSecs is the default rotate-out duration for providers that
	// set none of their own. 0 uses rolodex's built-in default (3600).
	RefusalCooldownSecs uint32 `json:"refusal_cooldown_secs,omitempty"`
}

// BlocklistConfigResponse is the response for GET /dns/dnsbl.
type BlocklistConfigResponse struct {
	Enabled             bool                   `json:"enabled"`
	Providers           []BlocklistProviderDTO `json:"providers"`
	RefusalCooldownSecs uint32                 `json:"refusal_cooldown_secs,omitempty"`
	RotatedOut          []RotatedProviderDTO   `json:"rotated_out,omitempty"`
}

// LocalBlocklistEntryDTO is the JSON shape of a single local blocklist entry.
type LocalBlocklistEntryDTO struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// AddLocalBlocklistEntryRequest is the request body for POST /dns/rbl/local/add.
// The path keeps its historical spelling: it is a published HTTP contract with
// the UI, and renaming it would break every client that already speaks it.
type AddLocalBlocklistEntryRequest struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// RemoveLocalBlocklistEntryRequest is the request body for POST /dns/rbl/local/remove.
type RemoveLocalBlocklistEntryRequest struct {
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

func dnsblProvidersToDTO(providers []*upstream.DnsblConfig) []BlocklistProviderDTO {
	out := make([]BlocklistProviderDTO, 0, len(providers))
	for _, p := range providers {
		if p == nil {
			continue
		}
		out = append(out, BlocklistProviderDTO{
			Zone:                p.Zone,
			Enabled:             p.Enabled,
			RefusalCodes:        slices.Clone(p.RefusalCodes),
			RefusalCooldownSecs: p.RefusalCooldownSecs,
		})
	}
	return out
}

// rotatedOutToDTO converts rolodex's rotated-out report. It is the same shape
// for both lists, so both handlers share it.
func rotatedOutToDTO(rotated []*upstream.RotatedProvider) []RotatedProviderDTO {
	out := make([]RotatedProviderDTO, 0, len(rotated))
	for _, r := range rotated {
		if r == nil {
			continue
		}
		out = append(out, RotatedProviderDTO{
			Zone:             r.Zone,
			Code:             r.Code,
			SecondsRemaining: r.SecondsRemaining,
		})
	}
	return out
}

// validateProviderDTOs validates and normalizes (trim + lowercase) a list of
// provider zones and their refusal-code settings, returning the cleaned
// providers.
func validateProviderDTOs(providers []BlocklistProviderDTO) ([]BlocklistProviderDTO, error) {
	cleaned := make([]BlocklistProviderDTO, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		zone := strings.ToLower(strings.TrimSpace(p.Zone))
		if err := ValidateBlocklistZone(zone); err != nil {
			return nil, err
		}
		if _, dup := seen[zone]; dup {
			return nil, fmt.Errorf("duplicate blocklist zone %q", zone)
		}
		seen[zone] = struct{}{}

		codes, err := ValidateRefusalCodes(p.RefusalCodes)
		if err != nil {
			return nil, fmt.Errorf("blocklist zone %q: %w", zone, err)
		}

		cleaned = append(cleaned, BlocklistProviderDTO{
			Zone:                zone,
			Enabled:             p.Enabled,
			RefusalCodes:        codes,
			RefusalCooldownSecs: p.RefusalCooldownSecs,
		})
	}
	return cleaned, nil
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
	return c.JSON(200, BlocklistConfigResponse{
		Enabled:             status.Enabled,
		Providers:           dnsblProvidersToDTO(status.Providers),
		RefusalCooldownSecs: status.RefusalCooldownSecs,
		RotatedOut:          rotatedOutToDTO(status.RotatedOut),
	})
}

// setDnsblConfig handles POST /dns/dnsbl.
func (s *SystemControllerHandlers) setDnsblConfig(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req BlocklistConfigRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	cleaned, err := validateProviderDTOs(req.Providers)
	if err != nil {
		return echo.NewHTTPError(400, err.Error())
	}

	providers := make([]*upstream.DnsblConfig, 0, len(cleaned))
	for _, p := range cleaned {
		providers = append(providers, &upstream.DnsblConfig{
			Zone:                p.Zone,
			Enabled:             p.Enabled,
			RefusalCodes:        p.RefusalCodes,
			RefusalCooldownSecs: p.RefusalCooldownSecs,
		})
	}

	// Persisted first: rolodex forgets
	// this on every restart, so the stored setting is what makes the toggle
	// stay where the operator put it.
	stored := BlocklistConfigRequest{Enabled: req.Enabled, Providers: cleaned, RefusalCooldownSecs: req.RefusalCooldownSecs}
	if err := saveStoredBlocklist(c.Request().Context(), s.Controller.GetSettingsManager(), stored); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("save dnsbl config: %v", err))
	}
	s.syncRolodexBlocklistConfig(c.Request().Context())

	if err := rc.SetDnsblConfig(c.Request().Context(), req.Enabled, providers, req.RefusalCooldownSecs); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("set dnsbl config: %v", err))
	}
	return c.JSON(200, BlocklistConfigResponse{
		Enabled:             req.Enabled,
		Providers:           cleaned,
		RefusalCooldownSecs: req.RefusalCooldownSecs,
	})
}

// listLocalBlocklistEntries handles GET /dns/rbl/local.
func (s *SystemControllerHandlers) listLocalBlocklistEntries(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}
	entries, err := rc.ListLocalBlocklistEntries(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("list local blocklist entries: %v", err))
	}
	out := make([]LocalBlocklistEntryDTO, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		out = append(out, LocalBlocklistEntryDTO{Name: e.Name, Reason: e.Reason})
	}
	return c.JSON(200, out)
}

// addLocalBlocklistEntry handles POST /dns/rbl/local/add.
func (s *SystemControllerHandlers) addLocalBlocklistEntry(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req AddLocalBlocklistEntryRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if err := ValidateLocalBlocklistName(name); err != nil {
		return echo.NewHTTPError(400, err.Error())
	}

	if err := rc.AddLocalBlocklistEntry(c.Request().Context(), &upstream.LocalBlocklistEntry{
		Name:   name,
		Reason: strings.TrimSpace(req.Reason),
	}); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("add local blocklist entry: %v", err))
	}
	return c.JSON(200, map[string]string{"status": "ok", "name": name})
}

// removeLocalBlocklistEntry handles POST /dns/rbl/local/remove.
func (s *SystemControllerHandlers) removeLocalBlocklistEntry(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req RemoveLocalBlocklistEntryRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	if name == "" {
		return echo.NewHTTPError(400, "name must not be empty")
	}

	if err := rc.RemoveLocalBlocklistEntry(c.Request().Context(), name); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("remove local blocklist entry: %v", err))
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
