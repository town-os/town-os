package wireguard

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// HandshakeStaleAfter is how long after a peer's last handshake it is still
// considered connected. WireGuard's own REJECT_AFTER_TIME is 180s: a session
// key older than that is refused, so a peer passing traffic re-handshakes
// within the window and one that has gone away stops. It is the only
// liveness signal WireGuard offers — the protocol has no session teardown, so
// "connected" can only ever mean "handshook recently".
const HandshakeStaleAfter = 180 * time.Second

// PeerStatus is the live kernel-side view of one WireGuard peer, as reported by
// `wg show <iface> dump`. It is keyed by PublicKey, the only field that also
// exists in the persisted peer record.
//
// A peer that has never completed a handshake reports a zero LatestHandshake;
// callers must test with IsZero rather than comparing against the epoch.
type PeerStatus struct {
	PublicKey       string
	Endpoint        string
	AllowedIPs      string
	LatestHandshake time.Time
	RxBytes         uint64
	TxBytes         uint64
	Keepalive       int
}

// Connected reports whether the peer handshook recently enough to be carrying
// traffic now. A peer that never handshook is never connected.
func (p PeerStatus) Connected(now time.Time) bool {
	if p.LatestHandshake.IsZero() {
		return false
	}
	return now.Sub(p.LatestHandshake) <= HandshakeStaleAfter
}

// dumpNone is the placeholder `wg` prints for an unset field. It appears in
// place of an endpoint the peer has never contacted us from, and in place of a
// preshared key. Treating it as a literal value would put the string "(none)"
// in the UI where an address belongs.
const dumpNone = "(none)"

// dumpOff is the placeholder `wg` prints for a disabled persistent-keepalive.
const dumpOff = "off"

// ParseDump parses the output of `wg show <iface> dump` into per-peer status,
// keyed by public key.
//
// The format is tab-separated and positional. The FIRST line describes the
// interface itself (private-key, public-key, listen-port, fwmark) and is
// deliberately skipped — it is not a peer, and treating it as one would
// manufacture a phantom peer holding the interface's own key. Every subsequent
// line is one peer:
//
//	public-key  preshared-key  endpoint  allowed-ips  latest-handshake  rx  tx  keepalive
//
// Malformed lines are skipped rather than failing the whole parse: this feeds a
// status panel, and one unparseable peer must not blank out the rest. A short
// line (fewer than 8 fields) is the shape `wg` produces for no peer at all, so
// there is nothing to recover from it.
func ParseDump(r io.Reader) (map[string]PeerStatus, error) {
	out := map[string]PeerStatus{}
	scanner := bufio.NewScanner(r)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			// The interface line. Skip it even if it is blank or malformed: an
			// empty dump (no interface) yields no peers, which is correct.
			first = false
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 8 {
			continue
		}
		pubKey := strings.TrimSpace(fields[0])
		if pubKey == "" {
			continue
		}

		st := PeerStatus{PublicKey: pubKey}
		if ep := fields[2]; ep != dumpNone {
			st.Endpoint = ep
		}
		if ips := fields[3]; ips != dumpNone {
			st.AllowedIPs = ips
		}
		// A latest-handshake of 0 means "never handshook", which must stay the
		// zero time rather than becoming 1970 — the UI distinguishes "never" from
		// "long ago".
		if secs, err := strconv.ParseInt(fields[4], 10, 64); err == nil && secs > 0 {
			st.LatestHandshake = time.Unix(secs, 0).UTC()
		}
		if rx, err := strconv.ParseUint(fields[5], 10, 64); err == nil {
			st.RxBytes = rx
		}
		if tx, err := strconv.ParseUint(fields[6], 10, 64); err == nil {
			st.TxBytes = tx
		}
		if ka := fields[7]; ka != dumpOff {
			if secs, err := strconv.Atoi(ka); err == nil {
				st.Keepalive = secs
			}
		}
		out[pubKey] = st
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read wireguard dump: %w", err)
	}
	return out, nil
}
