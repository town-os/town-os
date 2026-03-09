package rolodex

import (
	"context"
	"fmt"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
)

// PackageDNSInfo holds the information needed to register or unregister
// DNS records for a single installed package.
type PackageDNSInfo struct {
	Repo    string
	Name    string
	Domains []string
}

// SetupTLD idempotently creates an authoritative zone for the given TLD and
// adds SOA, NS, and A/AAAA records for ns1.<tld>.
func SetupTLD(ctx context.Context, c Client, tld, ipv4, ipv6 string) error {
	zone := tld + "."

	if err := c.AddAuthoritativeZone(ctx, zone); err != nil {
		return fmt.Errorf("add authoritative zone %s: %w", zone, err)
	}

	serial := time.Now().Unix()
	soaValue := fmt.Sprintf("ns1.%s. hostmaster.%s. %d 7200 3600 1209600 3600", zone, zone, serial)
	if err := c.AddRecord(ctx, &upstream.DnsRecord{
		Name:       zone,
		RecordType: upstream.RecordTypeSOA,
		Value:      soaValue,
		Ttl:        3600,
	}); err != nil {
		return fmt.Errorf("add SOA record: %w", err)
	}

	if err := c.AddRecord(ctx, &upstream.DnsRecord{
		Name:       zone,
		RecordType: upstream.RecordTypeNS,
		Value:      "ns1." + zone,
		Ttl:        3600,
	}); err != nil {
		return fmt.Errorf("add NS record: %w", err)
	}

	ns1Name := "ns1." + zone
	if ipv4 != "" {
		if err := c.AddRecord(ctx, &upstream.DnsRecord{
			Name:       ns1Name,
			RecordType: upstream.RecordTypeA,
			Value:      ipv4,
			Ttl:        300,
		}); err != nil {
			return fmt.Errorf("add A record for ns1: %w", err)
		}
	}

	if ipv6 != "" {
		if err := c.AddRecord(ctx, &upstream.DnsRecord{
			Name:       ns1Name,
			RecordType: upstream.RecordTypeAAAA,
			Value:      ipv6,
			Ttl:        300,
		}); err != nil {
			return fmt.Errorf("add AAAA record for ns1: %w", err)
		}
	}

	return nil
}

// TeardownTLD removes the authoritative zone and all records under the given TLD.
func TeardownTLD(ctx context.Context, c Client, tld string) error {
	zone := tld + "."

	// Remove all records in the zone first.
	records, err := c.ListRecords(ctx, nil)
	if err != nil {
		return fmt.Errorf("list records: %w", err)
	}
	for _, r := range records {
		if len(r.Name) >= len(zone) && r.Name[len(r.Name)-len(zone):] == zone {
			if _, err := c.RemoveRecord(ctx, r.Name, &upstream.RemoveRecordOptions{RecordType: &r.RecordType}); err != nil {
				return fmt.Errorf("remove record %s: %w", r.Name, err)
			}
		}
	}

	if err := c.RemoveAuthoritativeZone(ctx, zone); err != nil {
		return fmt.Errorf("remove authoritative zone %s: %w", zone, err)
	}

	return nil
}

// RegisterPackageDNS creates A (and optionally AAAA) records for a package
// under the TLD. The primary record is name.repo.tld. and additional records
// are created for each entry in extraDomains as domain.name.repo.tld.
func RegisterPackageDNS(ctx context.Context, c Client, repo, name, tld, ipv4, ipv6 string, extraDomains []string) error {
	baseName := name + "." + repo + "." + tld + "."

	names := []string{baseName}
	for _, d := range extraDomains {
		names = append(names, d+"."+baseName)
	}

	for _, fqdn := range names {
		if ipv4 != "" {
			if err := c.AddRecord(ctx, &upstream.DnsRecord{
				Name:       fqdn,
				RecordType: upstream.RecordTypeA,
				Value:      ipv4,
				Ttl:        300,
			}); err != nil {
				return fmt.Errorf("add A record %s: %w", fqdn, err)
			}
		}
		if ipv6 != "" {
			if err := c.AddRecord(ctx, &upstream.DnsRecord{
				Name:       fqdn,
				RecordType: upstream.RecordTypeAAAA,
				Value:      ipv6,
				Ttl:        300,
			}); err != nil {
				return fmt.Errorf("add AAAA record %s: %w", fqdn, err)
			}
		}
	}

	return nil
}

// UnregisterPackageDNS removes A and AAAA records for a package and its
// extra domains under the TLD.
func UnregisterPackageDNS(ctx context.Context, c Client, repo, name, tld string, extraDomains []string) error {
	baseName := name + "." + repo + "." + tld + "."

	names := []string{baseName}
	for _, d := range extraDomains {
		names = append(names, d+"."+baseName)
	}

	for _, fqdn := range names {
		aType := upstream.RecordTypeA
		if _, err := c.RemoveRecord(ctx, fqdn, &upstream.RemoveRecordOptions{RecordType: &aType}); err != nil {
			return fmt.Errorf("remove A record %s: %w", fqdn, err)
		}
		aaaaType := upstream.RecordTypeAAAA
		if _, err := c.RemoveRecord(ctx, fqdn, &upstream.RemoveRecordOptions{RecordType: &aaaaType}); err != nil {
			return fmt.Errorf("remove AAAA record %s: %w", fqdn, err)
		}
	}

	return nil
}

// ChangeTLD tears down the old TLD, sets up the new one, and re-registers
// all package DNS records under the new TLD.
func ChangeTLD(ctx context.Context, c Client, oldTLD, newTLD, ipv4, ipv6 string, packages []PackageDNSInfo) error {
	if err := TeardownTLD(ctx, c, oldTLD); err != nil {
		return fmt.Errorf("teardown old TLD %s: %w", oldTLD, err)
	}

	if err := SetupTLD(ctx, c, newTLD, ipv4, ipv6); err != nil {
		return fmt.Errorf("setup new TLD %s: %w", newTLD, err)
	}

	for _, pkg := range packages {
		if err := RegisterPackageDNS(ctx, c, pkg.Repo, pkg.Name, newTLD, ipv4, ipv6, pkg.Domains); err != nil {
			return fmt.Errorf("re-register %s/%s: %w", pkg.Repo, pkg.Name, err)
		}
	}

	return nil
}
