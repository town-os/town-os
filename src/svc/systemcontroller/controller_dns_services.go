package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"github.com/labstack/echo/v5"
)

// settingDNSExcludedServices stores the JSON array of "repo/name" keys for
// installed package services that should NOT be published in the DNS zone.
// Publishing is opt-out: a service is published unless its key is listed here.
const settingDNSExcludedServices = "dns_excluded_services"

func dnsServiceKey(repo, name string) string { return repo + "/" + name }

// loadDNSExcludedServices reads the excluded-service set from settings. A nil
// manager or unset/blank/invalid value yields an empty set (publish all).
func loadDNSExcludedServices(mgr account.SettingsManager) map[string]bool {
	out := map[string]bool{}
	if mgr == nil {
		return out
	}
	val, err := mgr.Get(settingDNSExcludedServices)
	if err != nil || val == "" {
		return out
	}
	var keys []string
	if err := json.Unmarshal([]byte(val), &keys); err != nil {
		return out
	}
	for _, k := range keys {
		out[k] = true
	}
	return out
}

// saveDNSExcludedServices persists the excluded-service set as a sorted JSON
// array (sorted for deterministic, diff-friendly storage).
func saveDNSExcludedServices(mgr account.SettingsManager, set map[string]bool) error {
	if mgr == nil {
		return errors.New("settings manager not available")
	}
	keys := make([]string, 0, len(set))
	for k, v := range set {
		if v {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	data, err := json.Marshal(keys)
	if err != nil {
		return fmt.Errorf("marshal excluded services: %w", err)
	}
	return mgr.Set(settingDNSExcludedServices, string(data))
}

// DNSServiceEntry describes an installed package service and whether it is
// published in the DNS zone.
type DNSServiceEntry struct {
	Repo      string   `json:"repo"`
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	FQDN      string   `json:"fqdn"`
	Domains   []string `json:"domains"`
	Published bool     `json:"published"`
}

// SetDNSServiceRequest is the request body for POST /dns/services/set.
type SetDNSServiceRequest struct {
	Repo      string `json:"repo"`
	Name      string `json:"name"`
	Published bool   `json:"published"`
}

// listDNSServices handles GET /dns/services. It lists installed package
// services (deduplicated by repo/name) with their published state.
func (s *SystemControllerHandlers) listDNSServices(c *echo.Context) error {
	inst := s.Controller.GetInstaller()
	if inst == nil {
		return c.JSON(200, []DNSServiceEntry{})
	}

	installed, err := inst.ListInstalled()
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("list installed: %v", err))
	}

	tld := s.getDNSTLDValue()
	excluded := loadDNSExcludedServices(s.Controller.GetSettingsManager())
	rr := s.Controller.GetRepositoryRoot()

	seen := map[string]bool{}
	entries := make([]DNSServiceEntry, 0, len(installed))
	for _, pkg := range installed {
		pi, err := packages.ParsePackageIdentity(pkg)
		if err != nil {
			continue
		}
		key := dnsServiceKey(pi.Repo, pi.Name)
		if seen[key] {
			continue
		}
		seen[key] = true

		var domains []string
		if rr != nil {
			if ip, lerr := rr.LoadPackage(pi.Repo, pi.Name, pi.Version); lerr == nil {
				domains = internalDomains(ip.Network.Domains, tld)
			}
		}

		entries = append(entries, DNSServiceEntry{
			Repo:      pi.Repo,
			Name:      pi.Name,
			Version:   pi.Version,
			FQDN:      pi.Name + "." + pi.Repo + "." + tld,
			Domains:   domains,
			Published: !excluded[key],
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Repo != entries[j].Repo {
			return entries[i].Repo < entries[j].Repo
		}
		return entries[i].Name < entries[j].Name
	})

	return c.JSON(200, entries)
}

// setDNSService handles POST /dns/services/set. It publishes or unpublishes an
// installed package service in the DNS zone, persisting the choice so reconcile
// honors it, and immediately registering/unregistering the records.
func (s *SystemControllerHandlers) setDNSService(c *echo.Context) error {
	var req SetDNSServiceRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}
	if req.Repo == "" || req.Name == "" {
		return echo.NewHTTPError(400, "repo and name are required")
	}

	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return echo.NewHTTPError(503, "settings not available")
	}

	inst := s.Controller.GetInstaller()
	if inst == nil {
		return echo.NewHTTPError(503, "installer not available")
	}
	version, ok, err := inst.GetInstalledVersion(req.Repo, req.Name)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("get installed version: %v", err))
	}
	if !ok {
		return echo.NewHTTPError(404, "service not installed")
	}

	key := dnsServiceKey(req.Repo, req.Name)
	excluded := loadDNSExcludedServices(mgr)
	if req.Published {
		delete(excluded, key)
	} else {
		excluded[key] = true
	}
	if err := saveDNSExcludedServices(mgr, excluded); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("save excluded services: %v", err))
	}

	// Apply the change immediately to the live zone (best-effort: persisted
	// state is the source of truth, reconcile will converge regardless).
	rc := s.Controller.GetRolodexClient()
	tld := s.getDNSTLDValue()
	if rc != nil && tld != "" {
		ctx := c.Request().Context()
		var domains []string
		if rr := s.Controller.GetRepositoryRoot(); rr != nil {
			if ip, lerr := rr.LoadPackage(req.Repo, req.Name, version); lerr == nil {
				domains = internalDomains(ip.Network.Domains, tld)
			}
		}
		if req.Published {
			ipv4 := s.Controller.GetInternalIP()
			ipv6 := s.Controller.GetInternalIPv6()
			if err := rolodex.RegisterPackageDNS(ctx, rc, req.Repo, req.Name, tld, ipv4, ipv6, domains); err != nil {
				return echo.NewHTTPError(500, fmt.Sprintf("register DNS: %v", err))
			}
		} else {
			if err := rolodex.UnregisterPackageDNS(ctx, rc, req.Repo, req.Name, tld, domains); err != nil {
				return echo.NewHTTPError(500, fmt.Sprintf("unregister DNS: %v", err))
			}
		}
	}

	return c.JSON(200, map[string]any{"status": "ok", "repo": req.Repo, "name": req.Name, "published": req.Published})
}

// filterExcludedDNSInfo returns a new slice with excluded services removed. It
// never mutates the input slice.
func filterExcludedDNSInfo(pkgs []rolodex.PackageDNSInfo, excluded map[string]bool) []rolodex.PackageDNSInfo {
	if len(excluded) == 0 {
		return pkgs
	}
	out := make([]rolodex.PackageDNSInfo, 0, len(pkgs))
	for _, p := range pkgs {
		if excluded[dnsServiceKey(p.Repo, p.Name)] {
			continue
		}
		out = append(out, p)
	}
	return out
}
