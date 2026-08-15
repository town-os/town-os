package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"gitea.com/town-os/town-os/src/rolodex"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// RolodexDataSubdir is the box-side directory bind-mounted into the rolodex
// container as /data. The transport leaf is written under it because that mount
// is the only path the container can see — see RolodexTLSSubdir.
const RolodexDataSubdir = "rolodex"

// rolodexDataDir is that directory for a given btrfs base. It is a function
// rather than a literal at each use site because the leaf writer and the pin
// reader have to agree on it: a leaf issued into one path and pinned from
// another publishes a DANE record for a certificate nothing serves, which fails
// closed for every client that checks it.
func rolodexDataDir(btrfsBase string) string {
	if btrfsBase == "" {
		return ""
	}
	return filepath.Join(btrfsBase, RolodexDataSubdir)
}

// publishEncryptedDNS publishes everything a client needs to FIND and VERIFY
// this box's encrypted DNS under tld, in one call.
//
// Four things have to be true before a stranger's phone can use this resolver
// privately, and all four are named after the same `dns.<tld>`:
//
//  1. the name resolves (publishDohRecord),
//  2. something advertises it (publishDDRDesignation),
//  3. DoT and DoQ serve a certificate that names it (issueRolodexTransportLeaf),
//  4. that certificate is pinned where a client can check it
//     (collectRolodexTransportTLSA).
//
// They are one function because they have to move together and nothing makes
// that obvious at the call sites. Each was written as its own publisher with a
// single caller in RebuildDNS, and the second caller — changing the TLD through
// the API — updated the zone and none of these, leaving the box advertising
// `dns.<old>` with a certificate for a name no client would dial and a pin at
// an owner nobody would query. A DANE-checking client does not degrade there:
// finding no pin for the endpoint it reached is the signal to REFUSE, so the
// feature stops working rather than stopping being verified.
//
// Best-effort throughout, like every other publisher on this path: a box that
// fails to advertise encrypted DNS still resolves names, and no part of this is
// worth failing a boot or a settings change over.
func publishEncryptedDNS(
	ctx context.Context,
	cl rolodex.Client,
	ca *townostls.CA,
	btrfsBase, tld, internalIP, internalIPv6 string,
) {
	if cl == nil {
		return
	}

	// The address records first: the designation below is worthless while the
	// name it points at does not resolve, and a client that resolves the
	// designation early enough to race this retries the name it was given.
	publishDohRecord(ctx, cl, tld, internalIP, internalIPv6)
	publishDDRDesignation(ctx, cl, tld)

	// Then the certificate and its pins. Issued before collected, because
	// collectRolodexTransportTLSA reads the file from disk and publishes
	// nothing when it is absent — a pin for a certificate that does not exist
	// yet is the one failure mode worse than no pin at all.
	dataDir := rolodexDataDir(btrfsBase)
	issueRolodexTransportLeaf(ca, dataDir, tld, internalIP, internalIPv6)
	if entries := collectRolodexTransportTLSA(dataDir, tld); len(entries) > 0 {
		if err := rolodex.RegisterPackageTLSA(ctx, cl, entries); err != nil {
			slog.Debug(fmt.Sprintf("publish encrypted DNS pins: %v", err))
		}
	}
}

// retireEncryptedDNS removes the DANE pins for a TLD this box has stopped
// serving encrypted DNS under.
//
// The address records and the zone go with TeardownTLD/ChangeTLD, and the
// designation is rewritten in place rather than accumulating (see
// publishDDRDesignation). The pins are the exception: they live at
// `_853._tcp.<name>` and `_853._udp.<name>` inside the OLD zone, so a TLD
// change that tore that zone down has already taken them — but a rename to a
// TLD whose zone survives, and any future caller that changes the name without
// tearing anything down, would leave a pin for a certificate no longer served.
// A stale pin is not inert: it makes a DANE client refuse the connection it is
// meant to protect.
func retireEncryptedDNS(ctx context.Context, cl rolodex.Client, btrfsBase, tld string) {
	if cl == nil {
		return
	}
	entries := collectRolodexTransportTLSA(rolodexDataDir(btrfsBase), tld)
	if len(entries) == 0 {
		// No leaf on disk means nothing was ever pinned under this name, so
		// derive the owners from the name alone rather than skipping: the
		// certificate may already have been reissued for the new TLD.
		if name := dohRecordName(tld); name != "" {
			entries = []rolodex.TLSAEntry{
				{Name: name, Port: DoTPort, Proto: "tcp"},
				{Name: name, Port: DoQPort, Proto: "udp"},
			}
		}
	}
	if len(entries) == 0 {
		return
	}
	if err := rolodex.UnregisterPackageTLSA(ctx, cl, entries); err != nil {
		slog.Debug(fmt.Sprintf("retire encrypted DNS pins for %s: %v", tld, err))
	}
}
