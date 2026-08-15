package rolodex

import (
	"context"
	"fmt"
	"strings"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
)

// TLSAEntry describes one DANE TLSA record to publish: the association data
// (RDATA, e.g. "3 1 1 <hex>") for a TLS service reachable at Name on
// Port/Proto. Name is the base FQDN (e.g. gitea.default.home) without the RFC
// 6698 _<port>._<proto> prefix, which RegisterPackageTLSA prepends.
//
// Proto is "tcp" or "udp"; empty means tcp, which is what every HTTP-supplying
// package is. It exists because a TLSA record names a *service endpoint* rather
// than a certificate, and encrypted DNS is two endpoints on one certificate:
// DoT at 853/tcp and DoQ at 853/udp. With the protocol hardcoded there was no
// way to publish the DoQ half, and an endpoint with no TLSA fails closed for a
// DANE-checking client — indistinguishable from one that never had DANE at all.
type TLSAEntry struct {
	Name  string
	Port  uint16
	Proto string
	Value string
}

// tlsaName builds the RFC 6698 owner name for a TLSA record:
// _<port>._<proto>.<fqdn>. (always fully qualified with a trailing dot).
//
// An empty or unrecognized proto is tcp: every caller predating UDP support
// omits the field, and a typo must not mint a name under a protocol label
// nothing will ever query.
func tlsaName(name string, port uint16, proto string) string {
	if strings.EqualFold(proto, "udp") {
		proto = "udp"
	} else {
		proto = "tcp"
	}
	return fmt.Sprintf("_%d._%s.%s.", port, proto, strings.TrimSuffix(name, "."))
}

// RegisterPackageTLSA publishes a TLSA record for each entry pinning the
// proxy's leaf certificate so DANE-aware clients can validate the local-CA
// cert without trusting the CA out of band. Entries with an empty Value are
// skipped (the caller could not compute the association data).
func RegisterPackageTLSA(ctx context.Context, c Client, entries []TLSAEntry) error {
	for _, e := range entries {
		if e.Value == "" {
			continue
		}
		owner := tlsaName(e.Name, e.Port, e.Proto)
		if err := c.AddRecord(ctx, &upstream.DnsRecord{
			Name:       owner,
			RecordType: upstream.RecordTypeTLSA,
			Value:      e.Value,
			Ttl:        300,
		}); err != nil {
			return fmt.Errorf("add TLSA record %s: %w", owner, err)
		}
	}
	return nil
}

// RegisterScopedPackageTLSA publishes each entry's TLSA record within a network
// scope, mirroring scoped package address records so DANE validators on that
// network's overlay resolve the pin alongside the scoped address. A non-scoped
// TLSA under a network-owned TLD is hidden by rolodex's owned-TLD partition.
// Entries with an empty Value are skipped.
func RegisterScopedPackageTLSA(ctx context.Context, c Client, scope string, entries []TLSAEntry) error {
	for _, e := range entries {
		if e.Value == "" {
			continue
		}
		owner := tlsaName(e.Name, e.Port, e.Proto)
		if err := c.AddScopedRecord(ctx, scope, &upstream.DnsRecord{
			Name:       owner,
			RecordType: upstream.RecordTypeTLSA,
			Value:      e.Value,
			Ttl:        300,
		}); err != nil {
			return fmt.Errorf("add scoped TLSA record %s: %w", owner, err)
		}
	}
	return nil
}

// UnregisterPackageTLSA removes the TLSA records for the given entries. The
// Value field is ignored (removal is keyed by name + type).
func UnregisterPackageTLSA(ctx context.Context, c Client, entries []TLSAEntry) error {
	tlsaType := upstream.RecordTypeTLSA
	for _, e := range entries {
		owner := tlsaName(e.Name, e.Port, e.Proto)
		if _, err := c.RemoveRecord(ctx, owner, &upstream.RemoveRecordOptions{RecordType: &tlsaType}); err != nil {
			return fmt.Errorf("remove TLSA record %s: %w", owner, err)
		}
	}
	return nil
}

// UnregisterScopedPackageTLSA removes each entry's TLSA record from a network
// scope, the inverse of RegisterScopedPackageTLSA. A scoped pin lives in the
// scope's own table at the same _<port>._tcp owner name, so removing the scoped
// ADDRESS record for a host does not touch it — the owner names differ. The
// Value field is ignored (removal is keyed by name + type).
func UnregisterScopedPackageTLSA(ctx context.Context, c Client, scope string, entries []TLSAEntry) error {
	tlsaType := upstream.RecordTypeTLSA
	for _, e := range entries {
		owner := tlsaName(e.Name, e.Port, e.Proto)
		if _, err := c.RemoveScopedRecord(ctx, scope, owner, &upstream.RemoveScopedRecordOptions{RecordType: &tlsaType}); err != nil {
			return fmt.Errorf("remove scoped TLSA record %s: %w", owner, err)
		}
	}
	return nil
}

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

// EnsureScopedTLD idempotently publishes the zone-apex records (SOA, NS, and
// ns1 A/AAAA) for a network-owned TLD, scoped to that network. A network's TLD
// is its scope home_domain — an owned zone in rolodex's partition — so its apex
// records must be scoped: a global record under the TLD would be hidden by the
// owned-TLD partition (the same reason package address/TLSA records are scoped).
// It is a no-op when the zone's SOA is already present, so it is safe to call on
// every reconcile as well as at network creation. An empty ipv4/ipv6 skips the
// corresponding ns1 address record.
func EnsureScopedTLD(ctx context.Context, c Client, scope, tld, ipv4, ipv6 string) error {
	zone := tld + "."

	existing, err := c.ListScopedRecords(ctx, scope, nil)
	if err != nil {
		return fmt.Errorf("list scoped records for %q: %w", scope, err)
	}
	for _, r := range existing {
		if r.RecordType == upstream.RecordTypeSOA && strings.EqualFold(r.Name, zone) {
			return nil // apex already published for this scope
		}
	}

	serial := time.Now().Unix()
	soaValue := fmt.Sprintf("ns1.%s. hostmaster.%s. %d 7200 3600 1209600 3600", zone, zone, serial)
	if err := c.AddScopedRecord(ctx, scope, &upstream.DnsRecord{
		Name:       zone,
		RecordType: upstream.RecordTypeSOA,
		Value:      soaValue,
		Ttl:        3600,
	}); err != nil {
		return fmt.Errorf("add scoped SOA record: %w", err)
	}

	if err := c.AddScopedRecord(ctx, scope, &upstream.DnsRecord{
		Name:       zone,
		RecordType: upstream.RecordTypeNS,
		Value:      "ns1." + zone,
		Ttl:        3600,
	}); err != nil {
		return fmt.Errorf("add scoped NS record: %w", err)
	}

	ns1Name := "ns1." + zone
	if ipv4 != "" {
		if err := c.AddScopedRecord(ctx, scope, &upstream.DnsRecord{
			Name:       ns1Name,
			RecordType: upstream.RecordTypeA,
			Value:      ipv4,
			Ttl:        300,
		}); err != nil {
			return fmt.Errorf("add scoped A record for ns1: %w", err)
		}
	}

	if ipv6 != "" {
		if err := c.AddScopedRecord(ctx, scope, &upstream.DnsRecord{
			Name:       ns1Name,
			RecordType: upstream.RecordTypeAAAA,
			Value:      ipv6,
			Ttl:        300,
		}); err != nil {
			return fmt.Errorf("add scoped AAAA record for ns1: %w", err)
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
