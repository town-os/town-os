package wireguard

import (
	"strings"
	"testing"
)

// RenderInterfaceConfig formats every peer field straight into the document:
//
//	fmt.Fprintf(&b, "PublicKey = %s\n", p.PublicKey)
//	fmt.Fprintf(&b, "AllowedIPs = %s\n", p.AllowedIPs)
//	fmt.Fprintf(&b, "Endpoint = %s\n", p.Endpoint)
//
// The document it produces is consumed by `wg-quick up`, which runs as root
// from a generated systemd unit and executes PreUp/PostUp/PreDown/PostDown for
// every such line it reads while inside an [Interface] section. Section state
// comes from the file's own content, so a value carrying a newline and the
// literal "[Interface]" reopens that section and everything after it is
// interpreted as interface configuration.
//
// The peer fields are attacker-reachable: POST /networks/peers/add takes
// public_key and endpoint from the request body and stores them after only a
// strings.TrimSpace, and that route admits a non-admin holding the wireguard
// grant. See integration/systemcontroller_peer_injection_test.go for the
// end-to-end path.
//
// The renderer is the last place this can be stopped unconditionally, whatever
// the caller was. These tests assert the SECURE behaviour and fail against the
// current code.

// executableDirectives are the wg-quick keys that run a shell command, plus the
// two that redirect where its state goes. None of them may ever appear in a
// document rendered from peer data, because none of them is a field this
// renderer emits.
var executableDirectives = []string{"PostUp", "PreUp", "PostDown", "PreDown", "SaveConfig", "Table"}

func assertRenderIsInert(t *testing.T, cfg, field string) {
	t.Helper()

	for _, directive := range executableDirectives {
		if strings.Contains(cfg, directive) {
			t.Errorf("a %s value injected %s into the wg-quick config; wg-quick runs it as root:\n%s", field, directive, cfg)
		}
	}
	if n := strings.Count(cfg, "[Interface]"); n != 1 {
		t.Errorf("a %s value produced %d [Interface] sections, want exactly 1; a second one makes wg-quick honour Post/PreUp again:\n%s", field, n, cfg)
	}
	if n := strings.Count(cfg, "PrivateKey"); n != 1 {
		t.Errorf("a %s value produced %d PrivateKey lines, want exactly 1:\n%s", field, n, cfg)
	}
}

func TestRenderInterfaceConfigPeerPublicKeyCannotInjectDirectives(t *testing.T) {
	cfg := RenderInterfaceConfig(InterfaceConfig{
		PrivateKey: "cGFkZGluZ3BhZGRpbmdwYWRkaW5ncGFkZGluZ3BhZA=",
		Address:    "10.90.51.1/24",
		ListenPort: 51821,
		Peers: []PeerConfig{{
			PublicKey:  "kA1yqGFbJhZ0dLpVKcQ0xJZ8k0V0m8xk5Y3xJ0Zq0Vg=\n[Interface]\nPostUp = /bin/sh -c 'id > /tmp/pwned'",
			AllowedIPs: "10.90.51.2/32",
		}},
	})
	assertRenderIsInert(t, cfg, "PublicKey")
}

func TestRenderInterfaceConfigPeerEndpointCannotInjectDirectives(t *testing.T) {
	cfg := RenderInterfaceConfig(InterfaceConfig{
		PrivateKey: "cGFkZGluZ3BhZGRpbmdwYWRkaW5ncGFkZGluZ3BhZA=",
		Address:    "10.90.51.1/24",
		ListenPort: 51821,
		Peers: []PeerConfig{{
			PublicKey:  "kA1yqGFbJhZ0dLpVKcQ0xJZ8k0V0m8xk5Y3xJ0Zq0Vg=",
			AllowedIPs: "10.90.51.2/32",
			Endpoint:   "198.51.100.7:51820\n[Interface]\nPostUp = /bin/sh -c 'id > /tmp/pwned'",
		}},
	})
	assertRenderIsInert(t, cfg, "Endpoint")
}

// AllowedIPs is server-computed today (allocatePeerAddr), so this is not a live
// path -- but it is the same Fprintf, and pinning it stops the next field that
// becomes caller-supplied from reintroducing the hole silently.
func TestRenderInterfaceConfigPeerAllowedIPsCannotInjectDirectives(t *testing.T) {
	cfg := RenderInterfaceConfig(InterfaceConfig{
		PrivateKey: "cGFkZGluZ3BhZGRpbmdwYWRkaW5ncGFkZGluZ3BhZA=",
		Address:    "10.90.51.1/24",
		ListenPort: 51821,
		Peers: []PeerConfig{{
			PublicKey:  "kA1yqGFbJhZ0dLpVKcQ0xJZ8k0V0m8xk5Y3xJ0Zq0Vg=",
			AllowedIPs: "10.90.51.2/32\n[Interface]\nPostUp = /bin/false",
		}},
	})
	assertRenderIsInert(t, cfg, "AllowedIPs")
}

// A carriage return is enough on its own: wg-quick reads the file with bash's
// `read`, which splits on \n, and a lone \r inside a value still lets a
// following \n start a new logical line.
func TestRenderInterfaceConfigRejectsCarriageReturns(t *testing.T) {
	cfg := RenderInterfaceConfig(InterfaceConfig{
		PrivateKey: "cGFkZGluZ3BhZGRpbmdwYWRkaW5ncGFkZGluZ3BhZA=",
		Address:    "10.90.51.1/24",
		Peers: []PeerConfig{{
			PublicKey: "kA1yqGFbJhZ0dLpVKcQ0xJZ8k0V0m8xk5Y3xJ0Zq0Vg=\r\n[Interface]\r\nPostUp = /bin/false",
		}},
	})
	assertRenderIsInert(t, cfg, "PublicKey with CRLF")
}

// The counterpart: an ordinary peer must still render exactly as before, so a
// fix cannot be "emit nothing". TestRenderInterfaceConfig covers the happy path
// in detail; this only pins that the injection guard did not eat it.
func TestRenderInterfaceConfigWellFormedPeerUnaffected(t *testing.T) {
	priv, pub, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}

	cfg := RenderInterfaceConfig(InterfaceConfig{
		PrivateKey: priv,
		Address:    "10.90.51.1/24",
		ListenPort: 51821,
		Peers: []PeerConfig{{
			PublicKey:  pub,
			AllowedIPs: "10.90.51.2/32",
			Endpoint:   "198.51.100.7:51820",
			Keepalive:  25,
		}},
	})

	for _, want := range []string{
		"[Interface]",
		"PrivateKey = " + priv,
		"Address = 10.90.51.1/24",
		"ListenPort = 51821",
		"[Peer]",
		"PublicKey = " + pub,
		"AllowedIPs = 10.90.51.2/32",
		"Endpoint = 198.51.100.7:51820",
		"PersistentKeepalive = 25",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("rendered config is missing %q:\n%s", want, cfg)
		}
	}
	assertRenderIsInert(t, cfg, "well-formed peer")
}
