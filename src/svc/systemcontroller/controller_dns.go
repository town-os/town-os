package systemcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

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
	ipv6 := s.Controller.GetInternalIPv6()
	if err := rolodex.ChangeTLD(ctx, rc, oldTLD, req.TLD, ipv4, ipv6, pkgs); err != nil {
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
	ipv6 := s.Controller.GetInternalIPv6()

	if err := rolodex.SetupTLD(ctx, rc, tld, ipv4, ipv6); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("setup TLD: %v", err))
	}

	pkgs := s.collectInstalledPackageDNSInfo()
	registered := 0
	for _, pkg := range pkgs {
		if err := rolodex.RegisterPackageDNS(ctx, rc, pkg.Repo, pkg.Name, tld, ipv4, ipv6, pkg.Domains); err != nil {
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
	ipv6 := s.Controller.GetInternalIPv6()
	if err := rolodex.RegisterPackageDNS(ctx, rc, repoName, effectiveName, tld, ipv4, ipv6, internalDomains(domains, tld)); err != nil {
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

	if err := rolodex.UnregisterPackageDNS(ctx, rc, repoName, effectiveName, tld, internalDomains(domains, tld)); err != nil {
		slog.Debug(fmt.Sprintf("unregister DNS %s/%s: %v", repoName, effectiveName, err))
	}
}

// publishPackageTLSA publishes DANE TLSA records pinning the package's leaf
// for each terminated (non-passthrough) port at the package's internal FQDNs.
// It is a best-effort no-op when rolodex is unavailable, the TLD is empty, or
// the package terminates no TLS ports. Must be called after the network state
// file and leaf cert have been written.
func (s *SystemControllerHandlers) publishPackageTLSA(ctx context.Context, repoName, effectiveName, version string, domains []string) {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return
	}
	tld := s.getDNSTLDValue()
	if tld == "" {
		return
	}
	entries, err := buildTLSAEntries(
		s.Controller.GetNetworkStatePath(), s.Controller.GetBtrfsBasePath(),
		repoName, effectiveName, version, tld, domains,
	)
	if err != nil {
		slog.Debug(fmt.Sprintf("build TLSA %s/%s: %v", repoName, effectiveName, err))
		return
	}
	if err := rolodex.RegisterPackageTLSA(ctx, rc, entries); err != nil {
		slog.Debug(fmt.Sprintf("publish TLSA %s/%s: %v", repoName, effectiveName, err))
	}
}

// unpublishPackageTLSA removes every TLSA record under the package's base name
// (primary + subdomains, all ports). It lists records and filters by owner so
// it works even though the package's network state file is removed before this
// runs during uninstall.
func (s *SystemControllerHandlers) unpublishPackageTLSA(ctx context.Context, repoName, effectiveName string) {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return
	}
	tld := s.getDNSTLDValue()
	if tld == "" {
		return
	}
	records, err := rc.ListRecords(ctx, nil)
	if err != nil {
		slog.Debug(fmt.Sprintf("list records for TLSA cleanup %s/%s: %v", repoName, effectiveName, err))
		return
	}
	baseSuffix := effectiveName + "." + repoName + "." + tld + "."
	tlsaType := upstream.RecordTypeTLSA
	for _, r := range records {
		if r.RecordType != upstream.RecordTypeTLSA || !strings.Contains(r.Name, "._tcp.") {
			continue
		}
		if !strings.HasSuffix(r.Name, baseSuffix) {
			continue
		}
		if _, err := rc.RemoveRecord(ctx, r.Name, &upstream.RemoveRecordOptions{RecordType: &tlsaType}); err != nil {
			slog.Debug(fmt.Sprintf("remove TLSA %s: %v", r.Name, err))
		}
	}
}

// collectInstalledPackageDNSInfo returns DNS info for all installed packages.
func (s *SystemControllerHandlers) collectInstalledPackageDNSInfo() []rolodex.PackageDNSInfo {
	return collectInstalledDNSInfo(s.Controller.GetInstaller(), s.Controller.GetRepositoryRoot(), s.getDNSTLDValue())
}
