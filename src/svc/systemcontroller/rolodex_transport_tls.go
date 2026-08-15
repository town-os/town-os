package systemcontroller

import (
	"context"
	"log/slog"
	"path/filepath"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// RolodexTLSSubdir is where the DoT/DoQ leaf is written, relative to rolodex's
// DATA directory — the one path the rolodex container can see.
//
// It cannot go where every other leaf goes. The leaves live under the btrfs TLS
// subvolume, which the network controller and the ingress mount; the rolodex
// unit mounts exactly one path, `/town-os/rolodex:/data`, and nothing else. So
// the leaf goes in the data directory, and rolodex reads it at
// `/data/tls/dot/cert.pem` — the name ../install's scripts/rolodex-config.sh
// writes into `dot.tls.cert_path`.
//
// Those two paths are the two halves of one mount, written in two repos with no
// way to read across. Changing either one alone leaves rolodex watching a file
// nothing writes, which fails the way this subsystem always fails: silently, as
// a certificate that never stops being self-signed.
const RolodexTLSSubdir = "tls/dot"

// issueRolodexTransportLeaf issues the certificate rolodex's DoT and DoQ
// listeners serve, from this box's own CA.
//
// # What this is for
//
// Two things, and the second is the one that was missing.
//
// It stops the certificate being self-signed. A DoT client has no way to verify
// a self-signed certificate short of pinning it, and a leaf from the box's CA is
// verifiable by anything that already trusts that CA — which is every device the
// household enrolled.
//
// And it makes rolodex's certificate hot reload live. rolodex re-reads
// `cert_path`/`key_path` every 30 seconds and serves a renewed pair with no
// restart — but only for a listener pointed at FILES. Generated material is
// deliberately never polled, because regenerating on a timer would hand every
// client a different self-signed certificate twice a minute. On a stock box that
// whole mechanism was unreachable code.
//
// # Why writing the file is the whole job
//
// There is no call to rolodex here, and that is the point. ../install's
// rolodex-config.sh already names these paths in `dot.tls`/`doq.tls`, and
// rolodex treats a named-but-absent certificate as "serve a generated one and
// watch for the real one". So issuing the leaf IS the handoff: rolodex adopts it
// within one poll interval, with nothing to coordinate and no restart.
//
// That ordering is not incidental. rolodex starts before the systemcontroller —
// the controller cannot pull an image until something resolves names — so on a
// first boot the CA does not exist when rolodex reads its config. Anything that
// required the file to be there first would have to restart the box's only
// resolver to take effect.
//
// Best-effort: a box that keeps its generated certificate still serves encrypted
// DNS, so nothing here is worth failing a boot over.
func issueRolodexTransportLeaf(
	ca *townostls.CA,
	rolodexDataDir, tld, internalIP, internalIPv6 string,
) {
	hostname := dohIngressHostname(tld)
	if ca == nil || rolodexDataDir == "" || hostname == "" {
		return
	}

	// The same SANs the DoH vhost's leaf gets: the name clients dial, plus the
	// box's addresses for a client configured by address rather than by name. A
	// DoT client checks the identity it dialled, whichever form that took.
	sans := collectTLSSans(hostname, nil, internalIP, internalIPv6, "")
	outDir := filepath.Join(rolodexDataDir, RolodexTLSSubdir)
	if err := ca.IssueLeaf(outDir, sans); err != nil {
		slog.Debug("rolodex TLS: issuing the DoT/DoQ leaf", "dir", outDir, "error", err)
		return
	}
	slog.Info("rolodex DoT/DoQ leaf issued from this box's CA", "dir", outDir, "sans", sans)
}

// collectRolodexTransportTLSA returns the DANE pins for the leaf
// issueRolodexTransportLeaf just wrote: one per encrypted-DNS endpoint.
//
// Two records, not one. A TLSA record is owned by a service ENDPOINT rather than
// by a certificate, and encrypted DNS is two endpoints sharing both the port and
// the certificate: DoT on 853/tcp and DoQ on 853/udp. Publishing one and not the
// other is worse than publishing neither — a DANE-aware client that finds no
// record for the transport it chose fails closed, so the missing half reads as
// "this box's DoQ is broken" rather than "this box has no DANE".
//
// This is what makes the leaf worth issuing to a client that does not already
// trust the box's CA: the pin is carried in the zone rolodex itself is
// authoritative for, so a resolver that can reach this box can verify the
// certificate it is being offered without installing anything.
//
// Nothing is returned before the leaf exists. A pin for a certificate that has
// not been issued makes every client refuse the connection once it is — the same
// reason collectGfehTLSA skips a partition whose leaf is missing.
func collectRolodexTransportTLSA(rolodexDataDir, tld string) []rolodex.TLSAEntry {
	name := dohRecordName(tld)
	if rolodexDataDir == "" || name == "" {
		return nil
	}
	certPath := filepath.Join(rolodexDataDir, RolodexTLSSubdir, townostls.LeafCertFileName)
	value, err := tlsaValue(certPath)
	if err != nil || value == "" {
		slog.Debug("rolodex TLS: no DoT/DoQ leaf to pin yet", "path", certPath, "error", err)
		return nil
	}
	return []rolodex.TLSAEntry{
		{Name: name, Port: DoTPort, Proto: "tcp", Value: value},
		{Name: name, Port: DoQPort, Proto: "udp", Value: value},
	}
}

// reconcileRolodexTransportTLSA renews the DoT/DoQ leaf and rolls its DANE pins
// over, on the same hourly drift pass that repairs every other record.
//
// # Why the issuing path was not enough
//
// `IssueLeaf` already reissues a certificate that is inside 30 days of expiry —
// but only when something calls it, and until this the only callers were boot
// and a confirmed internal-IP change. Renewal was therefore a side effect of
// rebooting. The leaf is valid for ten years, so nothing would have failed for
// most of a decade and then encrypted DNS would have stopped working on a box
// that had changed nothing, which is the worst shape a certificate failure
// takes: no event to correlate it with.
//
// # Why renewing without the zone would be worse than not renewing
//
// A TLSA record pins the SHA-256 of the certificate's public key, and a reissue
// generates a fresh key — so the moment the leaf is renewed, the published pin
// stops matching what the endpoint serves. A DANE-checking client that finds a
// record and no match REFUSES the connection. Renewal and re-pinning are one
// operation for the same reason publishEncryptedDNS is one function.
//
// # Why the new pin goes up before the old one comes down
//
// rolodex re-reads `cert_path`/`key_path` every 30 seconds, so for up to one
// poll after the file changes the endpoint is still serving the OLD certificate.
// DANE takes a match on ANY record at the owner, so both records present is the
// state where either certificate validates — and withdrawing first would turn
// that half minute into a hard failure for exactly the clients this is for.
//
// current is the record set the caller already listed. Only the two owners this
// publishes at are considered; anything else in the zone belongs to another
// publisher and is left alone.
//
// Best-effort throughout, like the rest of this path.
func reconcileRolodexTransportTLSA(
	ctx context.Context,
	cfg ReconcileDNSConfig,
	tld string,
	current []*upstream.DnsRecord,
) {
	if cfg.Client == nil || cfg.TLSCA == nil {
		return
	}
	dataDir := rolodexDataDir(cfg.BtrfsBasePath)
	if dataDir == "" {
		return
	}

	// Idempotent: this rewrites nothing while the SANs still match and the
	// certificate has more than 30 days left, so the steady-state cost of the
	// hourly pass is a parse of one file.
	issueRolodexTransportLeaf(cfg.TLSCA, dataDir, tld, cfg.InternalIP, cfg.InternalIPv6)

	desired := collectRolodexTransportTLSA(dataDir, tld)
	if len(desired) == 0 {
		return
	}

	want := make(map[string]string, len(desired))
	for _, e := range desired {
		want[rolodex.TLSAOwner(e)] = e.Value
	}
	have := map[string]map[string]bool{}
	for _, r := range current {
		if r == nil || r.RecordType != upstream.RecordTypeTLSA {
			continue
		}
		if _, ours := want[r.Name]; !ours {
			continue
		}
		if have[r.Name] == nil {
			have[r.Name] = map[string]bool{}
		}
		have[r.Name][r.Value] = true
	}

	var add, stale []rolodex.TLSAEntry
	for _, e := range desired {
		owner := rolodex.TLSAOwner(e)
		if !have[owner][e.Value] {
			add = append(add, e)
		}
		for value := range have[owner] {
			if value == e.Value {
				continue
			}
			stale = append(stale, rolodex.TLSAEntry{Name: e.Name, Port: e.Port, Proto: e.Proto, Value: value})
		}
	}
	if len(add) == 0 && len(stale) == 0 {
		return
	}

	if len(add) > 0 {
		if err := rolodex.RegisterPackageTLSA(ctx, cfg.Client, add); err != nil {
			// Leave the stale pins standing. They are the ones a client can
			// still validate against the certificate being served right now,
			// and removing them with nothing published in their place is the
			// one state that fails closed for every DANE-checking client.
			slog.Debug("rolodex TLS: publishing the renewed DoT/DoQ pin", "error", err)
			return
		}
		slog.Info("rolodex DoT/DoQ pin published", "records", len(add))
	}
	for _, e := range stale {
		if err := rolodex.UnregisterPackageTLSAValue(ctx, cfg.Client, e); err != nil {
			slog.Debug("rolodex TLS: withdrawing a superseded DoT/DoQ pin", "error", err)
		}
	}
}
