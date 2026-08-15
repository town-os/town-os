package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
)

const (
	// DDRDesignationName is the name a DDR-aware client asks its own resolver
	// for to discover that resolver's encrypted endpoints (RFC 9462 §4).
	//
	// It is inside `arpa.`, which rolodex never resolves upstream — but that
	// refusal sits BELOW every local lookup, so a designation this box holds is
	// answered from its own records and never leaves the box. That is exactly
	// the property DDR needs: the resolver, and only the resolver, answers for
	// its own designation. A third party answering it would be telling a client
	// where its resolver's encrypted endpoints are, which is the attack the
	// whole mechanism is shaped to avoid.
	DDRDesignationName = "_dns.resolver.arpa."

	// DDRTTL is how long a client may cache the designation. Two hours: long
	// enough that discovery is not a per-query cost, short enough that moving
	// the endpoints is not a day-long outage for clients that already looked.
	DDRTTL = 7200

	// DohPathTemplate is the URI template clients build their DoH request from
	// (RFC 9461 §5). It is the path rolodex's DoH router actually serves, and
	// the `{?dns}` is the GET form's query parameter.
	DohPathTemplate = "/dns-query{?dns}"

	// DoTPort and DoQPort are the ports ../install's scripts/rolodex-config.sh
	// binds those listeners on. Like RolodexDohBackend these are the same
	// numbers written in two repos with no way to read across, so they are named
	// here rather than spelled inline.
	DoTPort = 853
	DoQPort = 853

	// DohPublishedPort is the port the DoH endpoint is reached on from a
	// client's point of view — the INGRESS's 443, not rolodex's own 4443. A
	// designation naming 4443 would send clients at a loopback-only listener
	// holding a self-signed certificate.
	DohPublishedPort = 443
)

// ddrDesignations builds the SVCB values published at DDRDesignationName.
//
// `hostname` is the name the endpoints are reached and authenticated as — the
// DoH vhost's name, which is the one the ingress holds a leaf for. All three
// transports are named under it: DoT and DoQ terminate their own TLS with a
// certificate whose SANs ../install's rolodex-config.sh sets to this box's
// names and addresses, so the name a client dials matches there too.
//
// Order is preference order, and it is not arbitrary: DoH on :443 first because
// :443 survives the DPI that filters DoT's :853, which is the same reason
// rolodex's own upstream chain prefers DoH. A client walks the list in priority
// order and stops at the first endpoint it can reach.
//
// Returns nil when there is no hostname, which both callers read as "publish
// nothing" rather than as a designation for a bare name.
func ddrDesignations(hostname string) []string {
	if hostname == "" {
		return nil
	}
	return []string{
		fmt.Sprintf("1 %s. alpn=h2 port=%d dohpath=%s", hostname, DohPublishedPort, DohPathTemplate),
		fmt.Sprintf("2 %s. alpn=dot port=%d", hostname, DoTPort),
		fmt.Sprintf("3 %s. alpn=doq port=%d", hostname, DoQPort),
	}
}

// publishDDRDesignation registers the DDR designation records, so a client that
// asks this resolver where its encrypted endpoints are gets an answer.
//
// Without this, `dns.<tld>` resolves and serves DoH, and no client ever finds
// it: encrypted DNS has to be typed into every device by hand. This is the
// record that makes the vhost discoverable, and it is the last piece of that
// path — the vhost (dohIngressRoute), its address records (publishDohRecord)
// and this designation are the three parts, and any one missing makes the other
// two useless.
//
// The existing designation is REMOVED before the new one is written, and that
// is not belt-and-braces. `RebuildDNS` starts by tearing the TLD's zone down,
// which is what makes every other publisher here idempotent — but this name is
// `_dns.resolver.arpa.`, which is in no zone this box owns, so the teardown
// never touches it. Without the removal, every rebuild would stack another copy
// (or, since AddRecord errors on duplicates, log a failure and leave a
// designation that quietly stopped tracking the TLD). Removing first also makes
// a TLD change take effect: the old TLD's endpoints stop being advertised
// instead of being served alongside the new ones.
//
// Best-effort, like every other record publisher here: an error on this one
// name must not abort the rebuild that publishes every package's.
func publishDDRDesignation(ctx context.Context, cl rolodex.Client, tld string) {
	if cl == nil {
		return
	}
	values := ddrDesignations(dohIngressHostname(tld))
	if len(values) == 0 {
		return
	}

	// The type is named explicitly, and it has to be. RemoveRecordOptions
	// documents nil as "every record type at this name", but `record_type` is a
	// plain proto3 enum: unset and A are the same byte, and rolodex decodes 0 as
	// A. A nil here removed A records from a name that holds only SVCB —
	// reporting success with a count of zero — so every rebuild stacked another
	// three designations, and a client walking them in priority order got the
	// same three endpoints listed twice.
	svcb := upstream.RecordTypeSVCB
	if _, err := cl.RemoveRecord(ctx, DDRDesignationName, &upstream.RemoveRecordOptions{
		RecordType: &svcb,
	}); err != nil {
		slog.Debug("dns: clearing the DDR designation", "error", err)
	}

	for _, value := range values {
		if err := cl.AddRecord(ctx, &upstream.DnsRecord{
			Name:       DDRDesignationName,
			RecordType: upstream.RecordTypeSVCB,
			Value:      value,
			Ttl:        DDRTTL,
		}); err != nil {
			slog.Debug("dns: DDR designation", "value", value, "error", err)
		}
	}
}
