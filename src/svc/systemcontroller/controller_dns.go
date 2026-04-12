package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
	"github.com/labstack/echo/v5"
)

// DNSStatusResponse is the response for GET /dns/status.
type DNSStatusResponse struct {
	Enabled     bool   `json:"enabled"`
	Running     bool   `json:"running"`
	TLD         string `json:"tld"`
	RecordCount int    `json:"record_count"`
}

// SetTLDRequest is the request body for POST /dns/tld.
type SetTLDRequest struct {
	TLD string `json:"tld"`
}

// AddDNSRecordRequest is the request body for POST /dns/records/add.
type AddDNSRecordRequest struct {
	Name       string             `json:"name"`
	RecordType upstream.RecordType `json:"record_type"`
	Value      string             `json:"value"`
	TTL        uint32             `json:"ttl"`
}

// RemoveDNSRecordRequest is the request body for POST /dns/records/remove.
type RemoveDNSRecordRequest struct {
	Name       string              `json:"name"`
	RecordType *upstream.RecordType `json:"record_type,omitempty"`
}

// DNSSetupResponse is the response for POST /dns/setup.
type DNSSetupResponse struct {
	TLD              string `json:"tld"`
	PackagesRegistered int  `json:"packages_registered"`
}

func (s *SystemControllerHandlers) dnsStatus(c *echo.Context) error {
	mgr := s.Controller.GetRolodex()
	if mgr == nil {
		return c.JSON(200, DNSStatusResponse{Enabled: false})
	}

	status := mgr.Status(c.Request().Context())
	tld := s.getDNSTLDValue()

	var recordCount int
	if rc := s.Controller.GetRolodexClient(); rc != nil {
		records, err := rc.ListRecords(c.Request().Context(), nil)
		if err == nil {
			recordCount = len(records)
		}
	}

	return c.JSON(200, DNSStatusResponse{
		Enabled:     true,
		Running:     status.Running,
		TLD:         tld,
		RecordCount: recordCount,
	})
}

func (s *SystemControllerHandlers) listDNSRecords(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	records, err := rc.ListRecords(c.Request().Context(), nil)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("list records: %v", err))
	}
	if records == nil {
		records = []*upstream.DnsRecord{}
	}

	return c.JSON(200, records)
}

func (s *SystemControllerHandlers) addDNSRecord(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req AddDNSRecordRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	if err := rc.AddRecord(c.Request().Context(), &upstream.DnsRecord{
		Name:       req.Name,
		RecordType: req.RecordType,
		Value:      req.Value,
		Ttl:        req.TTL,
	}); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("add record: %v", err))
	}

	return c.JSON(200, map[string]string{"status": "ok"})
}

func (s *SystemControllerHandlers) removeDNSRecord(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req RemoveDNSRecordRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	var opts *upstream.RemoveRecordOptions
	if req.RecordType != nil {
		opts = &upstream.RemoveRecordOptions{RecordType: req.RecordType}
	}

	removed, err := rc.RemoveRecord(c.Request().Context(), req.Name, opts)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("remove record: %v", err))
	}

	return c.JSON(200, map[string]any{"status": "ok", "removed": removed})
}

func (s *SystemControllerHandlers) getDNSTLD(c *echo.Context) error {
	tld := s.getDNSTLDValue()
	return c.JSON(200, map[string]string{"tld": tld})
}

func (s *SystemControllerHandlers) setDNSTLD(c *echo.Context) error {
	var req SetTLDRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	if err := ValidateTLD(req.TLD); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid TLD: %v", err))
	}

	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	oldTLD := s.getDNSTLDValue()
	ctx := c.Request().Context()

	// Collect installed packages for re-registration.
	pkgs := s.collectInstalledPackageDNSInfo()

	ipv4 := s.Controller.GetInternalIP()
	if err := rolodex.ChangeTLD(ctx, rc, oldTLD, req.TLD, ipv4, "", pkgs); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("change TLD: %v", err))
	}

	mgr := s.Controller.GetSettingsManager()
	if mgr != nil {
		if err := mgr.Set("dns_tld", req.TLD); err != nil {
			return echo.NewHTTPError(500, fmt.Sprintf("save TLD setting: %v", err))
		}
	}

	// Update systemd-resolved routing for the new TLD so inter-package
	// DNS resolution uses rolodex for the new domain.
	if fn := s.Controller.GetResolvedConfigurator(); fn != nil {
		fn(ctx, req.TLD, rolodex.DNSLoopback)
	}

	return c.JSON(200, map[string]string{"status": "ok", "tld": req.TLD})
}

func (s *SystemControllerHandlers) setupDNS(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	tld := s.getDNSTLDValue()
	ctx := c.Request().Context()
	ipv4 := s.Controller.GetInternalIP()

	if err := rolodex.SetupTLD(ctx, rc, tld, ipv4, ""); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("setup TLD: %v", err))
	}

	pkgs := s.collectInstalledPackageDNSInfo()
	registered := 0
	for _, pkg := range pkgs {
		if err := rolodex.RegisterPackageDNS(ctx, rc, pkg.Repo, pkg.Name, tld, ipv4, "", pkg.Domains); err != nil {
			slog.Debug(fmt.Sprintf("register DNS %s/%s: %v", pkg.Repo, pkg.Name, err))
			continue
		}
		registered++
	}

	return c.JSON(200, DNSSetupResponse{TLD: tld, PackagesRegistered: registered})
}

// getDNSTLDValue returns the current TLD from settings, defaulting to "home".
func (s *SystemControllerHandlers) getDNSTLDValue() string {
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return "home"
	}

	val, err := mgr.Get("dns_tld")
	if err != nil || val == "" {
		return "home"
	}

	return val
}

// registerPackageDNS creates DNS records for a newly installed package.
// It is a no-op if rolodex is not available or the TLD is empty.
func (s *SystemControllerHandlers) registerPackageDNS(ctx context.Context, repoName, effectiveName string, domains []string) {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return
	}

	tld := s.getDNSTLDValue()
	if tld == "" {
		return
	}

	ipv4 := s.Controller.GetInternalIP()
	if err := rolodex.RegisterPackageDNS(ctx, rc, repoName, effectiveName, tld, ipv4, "", domains); err != nil {
		slog.Debug(fmt.Sprintf("register DNS %s/%s: %v", repoName, effectiveName, err))
	}
}

// unregisterPackageDNS removes DNS records for a package being uninstalled.
// It is a no-op if rolodex is not available or the TLD is empty.
func (s *SystemControllerHandlers) unregisterPackageDNS(ctx context.Context, repoName, parentName, effectiveName, version string) {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return
	}

	tld := s.getDNSTLDValue()
	if tld == "" {
		return
	}

	var domains []string
	if rr := s.Controller.GetRepositoryRoot(); rr != nil {
		ip, err := rr.LoadPackage(repoName, parentName, version)
		if err == nil {
			domains = ip.Network.Domains
		}
	}

	if err := rolodex.UnregisterPackageDNS(ctx, rc, repoName, effectiveName, tld, domains); err != nil {
		slog.Debug(fmt.Sprintf("unregister DNS %s/%s: %v", repoName, effectiveName, err))
	}
}

// collectInstalledPackageDNSInfo returns DNS info for all installed packages.
func (s *SystemControllerHandlers) collectInstalledPackageDNSInfo() []rolodex.PackageDNSInfo {
	return collectInstalledDNSInfo(s.Controller.GetInstaller(), s.Controller.GetRepositoryRoot())
}
