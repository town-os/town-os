package wireguard

import (
	"strings"
	"testing"
	"time"
)

// realDump is a verbatim-shaped `wg show town1a2b dump`: a tab-separated
// interface line (private-key, public-key, listen-port, fwmark) followed by one
// line per peer (public-key, preshared-key, endpoint, allowed-ips,
// latest-handshake, rx, tx, keepalive).
const realDump = "cHJpdmF0ZUtleUJhc2U2NEV4YW1wbGVTdHJpbmdBQUE=\tc2VydmVyUHViS2V5QmFzZTY0RXhhbXBsZUFBQUFBQQ==\t51820\toff\n" +
	"cGVlck9uZVB1YmxpY0tleUJhc2U2NEV4YW1wbGVBQQ==\t(none)\t203.0.113.9:48123\t10.90.12.2/32\t1752600000\t14680064\t2097152\t25\n" +
	"cGVlclR3b1B1YmxpY0tleUJhc2U2NEV4YW1wbGVBQQ==\t(none)\t(none)\t10.90.12.3/32\t0\t0\t0\toff\n"

const peerOneKey = "cGVlck9uZVB1YmxpY0tleUJhc2U2NEV4YW1wbGVBQQ=="
const peerTwoKey = "cGVlclR3b1B1YmxpY0tleUJhc2U2NEV4YW1wbGVBQQ=="

func TestParseDumpReadsPeerFields(t *testing.T) {
	got, err := ParseDump(strings.NewReader(realDump))
	if err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed %d peers, want 2: %+v", len(got), got)
	}

	one, ok := got[peerOneKey]
	if !ok {
		t.Fatalf("peer one missing from %+v", got)
	}
	if one.Endpoint != "203.0.113.9:48123" {
		t.Errorf("endpoint = %q, want 203.0.113.9:48123", one.Endpoint)
	}
	if one.AllowedIPs != "10.90.12.2/32" {
		t.Errorf("allowed ips = %q, want 10.90.12.2/32", one.AllowedIPs)
	}
	if want := time.Unix(1752600000, 0).UTC(); !one.LatestHandshake.Equal(want) {
		t.Errorf("latest handshake = %v, want %v", one.LatestHandshake, want)
	}
	if one.RxBytes != 14680064 {
		t.Errorf("rx = %d, want 14680064", one.RxBytes)
	}
	if one.TxBytes != 2097152 {
		t.Errorf("tx = %d, want 2097152", one.TxBytes)
	}
	if one.Keepalive != 25 {
		t.Errorf("keepalive = %d, want 25", one.Keepalive)
	}
}

// The interface line holds a public key too. Counting it as a peer would
// manufacture a phantom row keyed by the server's own key.
func TestParseDumpSkipsInterfaceLine(t *testing.T) {
	got, err := ParseDump(strings.NewReader(realDump))
	if err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	if _, present := got["c2VydmVyUHViS2V5QmFzZTY0RXhhbXBsZUFBQUFBQQ=="]; present {
		t.Errorf("interface public key parsed as a peer: %+v", got)
	}
}

// "(none)" and "off" are placeholders, not values. Leaking them through would
// print "(none)" into the UI where an address belongs.
func TestParseDumpTranslatesPlaceholders(t *testing.T) {
	got, err := ParseDump(strings.NewReader(realDump))
	if err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	two, ok := got[peerTwoKey]
	if !ok {
		t.Fatalf("peer two missing from %+v", got)
	}
	if two.Endpoint != "" {
		t.Errorf("endpoint = %q, want empty for (none)", two.Endpoint)
	}
	if two.Keepalive != 0 {
		t.Errorf("keepalive = %d, want 0 for off", two.Keepalive)
	}
}

// A zero latest-handshake means "never handshook", which must stay the zero
// time. Mapping it to time.Unix(0,0) would render as 1970 and read as
// "handshook long ago" — the opposite of never.
func TestParseDumpNeverHandshakenIsZeroTime(t *testing.T) {
	got, err := ParseDump(strings.NewReader(realDump))
	if err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	two, ok := got[peerTwoKey]
	if !ok {
		t.Fatalf("peer two missing from %+v", got)
	}
	if !two.LatestHandshake.IsZero() {
		t.Errorf("latest handshake = %v, want zero time", two.LatestHandshake)
	}
	if two.Connected(time.Now()) {
		t.Error("a peer that never handshook must not report connected")
	}
}

func TestParseDumpEmptyInputYieldsNoPeers(t *testing.T) {
	got, err := ParseDump(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("parsed %d peers from empty input, want 0", len(got))
	}
}

// An interface with no peers dumps only its own line.
func TestParseDumpInterfaceOnlyYieldsNoPeers(t *testing.T) {
	got, err := ParseDump(strings.NewReader("priv\tpub\t51820\toff\n"))
	if err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("parsed %d peers, want 0: %+v", len(got), got)
	}
}

// One unparseable peer must not blank out the rest of the panel.
func TestParseDumpSkipsMalformedLinesButKeepsGoodOnes(t *testing.T) {
	dump := "priv\tpub\t51820\toff\n" +
		"truncated\tline\n" +
		"\n" +
		"goodkey\t(none)\t198.51.100.4:5555\t10.90.12.9/32\t1752600000\t10\t20\toff\n"
	got, err := ParseDump(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("parsed %d peers, want 1: %+v", len(got), got)
	}
	if got["goodkey"].Endpoint != "198.51.100.4:5555" {
		t.Errorf("good peer = %+v, want endpoint 198.51.100.4:5555", got["goodkey"])
	}
}

func TestPeerStatusConnectedWindow(t *testing.T) {
	now := time.Unix(1752600000, 0).UTC()
	cases := []struct {
		name      string
		handshake time.Time
		want      bool
	}{
		{"just handshook", now, true},
		{"inside the window", now.Add(-HandshakeStaleAfter + time.Second), true},
		{"exactly at the boundary", now.Add(-HandshakeStaleAfter), true},
		{"one second past the boundary", now.Add(-HandshakeStaleAfter - time.Second), false},
		{"long gone", now.Add(-24 * time.Hour), false},
		{"never", time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := PeerStatus{LatestHandshake: tc.handshake}
			if got := p.Connected(now); got != tc.want {
				t.Errorf("Connected(%v) = %v, want %v", tc.handshake, got, tc.want)
			}
		})
	}
}

// The window must track WireGuard's own REJECT_AFTER_TIME. A peer passing
// traffic re-handshakes well inside it; drifting from 180s would make idle
// peers look dead or dead peers look live.
func TestHandshakeStaleAfterMatchesRejectAfterTime(t *testing.T) {
	if HandshakeStaleAfter != 180*time.Second {
		t.Errorf("HandshakeStaleAfter = %v, want 180s (WireGuard REJECT_AFTER_TIME)", HandshakeStaleAfter)
	}
}
