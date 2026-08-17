package ingressctl

import (
	"slices"
	"strings"
	"testing"
)

func TestUnitConfigIPv4Only(t *testing.T) {
	m := NewManager(Config{
		Image:        "localhost/ingress:test",
		DataDir:      "/data/ingress",
		TLSHostDir:   "/data/tls",
		HostPort:     8443,
		HTTPHostPort: 8080,
		EnableIPv6:   false,
	})
	uc := m.unitConfig()

	if len(uc.ExecStartPre) != 1 || strings.Contains(uc.ExecStartPre[0], "--ipv6") {
		t.Fatalf("v4-only ExecStartPre must not enable --ipv6: %v", uc.ExecStartPre)
	}
	if !hasPublish(uc.Args, "8443:8443") {
		t.Fatalf("expected -p 8443:8443 publish, got args: %v", uc.Args)
	}
	if !hasPublish(uc.Args, "8080:8080") {
		t.Fatalf("expected -p 8080:8080 (HTTP) publish, got args: %v", uc.Args)
	}
	if hasPublish(uc.Args, "[::]:8443:8443") || hasPublish(uc.Args, "[::]:8080:8080") {
		t.Fatalf("v4-only must not publish on [::], got args: %v", uc.Args)
	}
	// The HTTP router needs its port and a default backend on the command line.
	if !hasFlagValue(uc.Command, "--http-port", "8080") {
		t.Fatalf("expected --http-port 8080 in command, got: %v", uc.Command)
	}
	if !hasFlag(uc.Command, "--default-backend") {
		t.Fatalf("expected --default-backend in command, got: %v", uc.Command)
	}
}

func TestUnitConfigDualStack(t *testing.T) {
	m := NewManager(Config{
		Image:        "localhost/ingress:test",
		DataDir:      "/data/ingress",
		TLSHostDir:   "/data/tls",
		HostPort:     8443,
		HTTPHostPort: 8080,
		EnableIPv6:   true,
	})
	uc := m.unitConfig()

	if len(uc.ExecStartPre) != 1 || !strings.Contains(uc.ExecStartPre[0], "--ipv6") {
		t.Fatalf("dual-stack ExecStartPre must create the network with --ipv6: %v", uc.ExecStartPre)
	}
	if !hasPublish(uc.Args, "8443:8443") || !hasPublish(uc.Args, "8080:8080") {
		t.Fatalf("expected -p 8443:8443 and -p 8080:8080 publish, got args: %v", uc.Args)
	}
	if !hasPublish(uc.Args, "[::]:8443:8443") || !hasPublish(uc.Args, "[::]:8080:8080") {
		t.Fatalf("dual-stack must also publish both ports on [::], got args: %v", uc.Args)
	}
}

// TestUnitConfigDefaultBackendDisabled verifies the "-" sentinel drops the
// --default-backend flag entirely (used by tests that run no UI).
func TestUnitConfigDefaultBackendDisabled(t *testing.T) {
	m := NewManager(Config{
		Image:          "localhost/ingress:test",
		DataDir:        "/data/ingress",
		TLSHostDir:     "/data/tls",
		HostPort:       8443,
		HTTPHostPort:   8080,
		DefaultBackend: "-",
	})
	uc := m.unitConfig()
	if hasFlag(uc.Command, "--default-backend") {
		t.Fatalf("disabled default backend must omit --default-backend, got: %v", uc.Command)
	}
}

// The box's language reaches the ingress on the unit's command line, which is
// what puts the retry page in it for a client whose own language Town OS ships
// no catalog for.
//
// Both arms, because the flag is conditional: an unset locale must leave it off
// rather than pass an empty string, which the ingress would have to special-case
// on the other side.
func TestUnitConfigCarriesTheLocale(t *testing.T) {
	cfg := Config{
		Image:        "localhost/ingress:test",
		DataDir:      "/data/ingress",
		TLSHostDir:   "/data/tls",
		HostPort:     8443,
		HTTPHostPort: 8080,
	}

	cfg.Locale = "de-DE"
	if uc := NewManager(cfg).unitConfig(); !hasFlagValue(uc.Command, "--locale", "de-DE") {
		t.Fatalf("expected --locale de-DE in command, got: %v", uc.Command)
	}

	cfg.Locale = ""
	if uc := NewManager(cfg).unitConfig(); hasFlag(uc.Command, "--locale") {
		t.Fatalf("an unset locale must omit --locale, got: %v", uc.Command)
	}
}

// hasFlag reports whether args contains the given flag token.
func hasFlag(args []string, flag string) bool {
	return slices.Contains(args, flag)
}

// hasFlagValue reports whether args contains `flag value` as adjacent tokens.
func hasFlagValue(args []string, flag, value string) bool {
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

// hasPublish reports whether args contains a `-p <spec>` pair.
func hasPublish(args []string, spec string) bool {
	for i, a := range args {
		if a == "-p" && i+1 < len(args) && args[i+1] == spec {
			return true
		}
	}
	// Defensive: also accept a combined "-p=<spec>" form if it ever appears.
	return slices.Contains(args, "-p="+spec)
}
