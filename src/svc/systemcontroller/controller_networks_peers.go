package systemcontroller

import (
	"cmp"
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/wireguard"
	"github.com/labstack/echo/v5"
)

// ConnectedPeerView is one enrolled peer joined with the live kernel state of
// its tunnel. The persisted row (name, account, overlay address, expiry) answers
// "who is allowed on"; the `wg show` half (handshake, endpoint, transfer)
// answers "who is actually here right now". Neither alone is the panel's
// question, which is why this view exists rather than reusing
// [account.NetworkPeer].
type ConnectedPeerView struct {
	Network   string `json:"network"`
	TLD       string `json:"tld"`
	Interface string `json:"interface"`
	PublicKey string `json:"public_key"`
	Name      string `json:"name"`
	// Account is the username that enrolled this peer (the peer row's CreatedBy).
	// Empty for peers added by a localhost/legacy path that had no calling
	// account.
	Account   string `json:"account,omitempty"`
	AllowedIP string `json:"allowed_ip"`
	// Endpoint is where the peer is dialing us from, as observed by the kernel;
	// it falls back to the operator-configured endpoint when the peer has never
	// been seen. Observed wins because a configured endpoint is only ever a
	// statement of intent — it is what we would dial, not where the peer is.
	Endpoint string `json:"endpoint,omitempty"`
	Rolodex  bool   `json:"rolodex"`
	// Connected reports a handshake inside WireGuard's REJECT_AFTER_TIME window.
	// It is the only liveness the protocol offers: there is no session teardown,
	// so a peer that walks away is indistinguishable from one that is merely idle
	// until its handshake goes stale.
	Connected bool `json:"connected"`
	// LastHandshake is nil when the peer has never completed a handshake — a
	// distinct state from "handshook long ago", and the difference between a
	// device that was never set up and one that is offline.
	LastHandshake *time.Time `json:"last_handshake,omitempty"`
	RxBytes       uint64     `json:"rx_bytes"`
	TxBytes       uint64     `json:"tx_bytes"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// wgDumpTimeout bounds a single `wg show` invocation. The command is a local
// netlink read and returns in milliseconds; the bound exists so a wedged exec
// can never stall the whole panel.
const wgDumpTimeout = 5 * time.Second

// wgShowDump returns the raw `wg show <iface> dump` output for an interface.
// Overridable in tests, which have no WireGuard device to read.
//
// The systemcontroller runs in the host network namespace (--net host), so the
// interface wg-quick created on the host is visible here; the `wg` binary itself
// ships in the systemcontroller image.
var wgShowDump = func(ctx context.Context, iface string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, wgDumpTimeout)
	defer cancel()

	// iface is not attacker-controlled data. Every caller derives it from
	// wireguard.InterfaceName, which returns "town" + 4 hex characters of a
	// sha256 — it cannot contain a metacharacter regardless of what a network
	// is named. There is no shell here either: exec passes argv directly, so
	// the argument is never re-parsed.
	out, err := exec.CommandContext(ctx, "wg", "show", iface, "dump").Output() //nolint:gosec // G204 -- see above
	if err != nil {
		return "", fmt.Errorf("wg show %s dump: %w", iface, err)
	}
	return string(out), nil
}

// networkPeerStatus reads the live peer state for a network's interface. A
// missing interface is not an error: a disabled network, or one whose transport
// has not come up yet, simply has no live peers, and the persisted rows must
// still render. Errors are logged and yield an empty map so one down interface
// cannot blank the whole panel.
func networkPeerStatus(ctx context.Context, iface string) map[string]wireguard.PeerStatus {
	dump, err := wgShowDump(ctx, iface)
	if err != nil {
		logNonFatal("read wireguard status for "+iface, err)
		return map[string]wireguard.PeerStatus{}
	}
	status, err := wireguard.ParseDump(strings.NewReader(dump))
	if err != nil {
		logNonFatal("parse wireguard status for "+iface, err)
		return map[string]wireguard.PeerStatus{}
	}
	return status
}

// listConnectedPeers handles GET /networks/peers/connected. It returns every
// enrolled peer across every WireGuard network, joined with live tunnel state.
//
// The default/home network is deliberately excluded: it is a DNS-only scope with
// no WireGuard transport at all (see applyNetworkTransport), so it has no
// interface to query and can never have peers. Including it would put a
// permanently empty, permanently disconnected row in a panel about who is
// tunnelled in.
func (s *SystemControllerHandlers) listConnectedPeers(c *echo.Context) error {
	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return c.JSON(200, []ConnectedPeerView{})
	}
	nets, err := nm.List()
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("list networks: %v", err))
	}

	ctx := c.Request().Context()
	now := time.Now()
	out := []ConnectedPeerView{}
	for _, n := range nets {
		if n.Name == account.DefaultNetworkName {
			continue
		}
		peers, perr := nm.ListPeers(n.Name)
		if perr != nil {
			return echo.NewHTTPError(500, fmt.Sprintf("list peers: %v", perr))
		}
		if len(peers) == 0 {
			continue
		}

		iface := wireguard.InterfaceName(wireGuardSalt, n.Name)
		// Only query the device when the network is actually up. A disabled
		// network has no interface, and shelling out per-network to learn that
		// would log a spurious failure on every poll.
		status := map[string]wireguard.PeerStatus{}
		if n.Enabled {
			status = networkPeerStatus(ctx, iface)
		}

		for _, p := range peers {
			view := ConnectedPeerView{
				Network:   n.Name,
				TLD:       n.TLD,
				Interface: iface,
				PublicKey: p.PublicKey,
				Name:      p.Name,
				Account:   p.CreatedBy,
				AllowedIP: p.AllowedIP,
				Endpoint:  p.Endpoint,
				Rolodex:   p.Rolodex,
				ExpiresAt: p.ExpiresAt,
				CreatedAt: p.CreatedAt,
			}
			if st, ok := status[p.PublicKey]; ok {
				if st.Endpoint != "" {
					view.Endpoint = st.Endpoint
				}
				if !st.LatestHandshake.IsZero() {
					hs := st.LatestHandshake
					view.LastHandshake = &hs
				}
				view.RxBytes = st.RxBytes
				view.TxBytes = st.TxBytes
				view.Connected = st.Connected(now)
			}
			out = append(out, view)
		}
	}

	// Deterministic order: network, then peer name, then key. The panel polls, so
	// an unstable order would reshuffle rows under the operator's cursor.
	slices.SortFunc(out, func(a, b ConnectedPeerView) int {
		return cmp.Or(
			cmp.Compare(a.Network, b.Network),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.PublicKey, b.PublicKey),
		)
	})
	return c.JSON(200, out)
}
