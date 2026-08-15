package systemcontroller

import (
	"log/slog"
	"path/filepath"

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
