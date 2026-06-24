package ingressctl

import (
	"slices"
	"strings"
	"testing"
)

func TestUnitConfigIPv4Only(t *testing.T) {
	m := NewManager(Config{
		Image:      "localhost/ingress:test",
		DataDir:    "/data/ingress",
		TLSHostDir: "/data/tls",
		HostPort:   8443,
		EnableIPv6: false,
	})
	uc := m.unitConfig()

	if len(uc.ExecStartPre) != 1 || strings.Contains(uc.ExecStartPre[0], "--ipv6") {
		t.Fatalf("v4-only ExecStartPre must not enable --ipv6: %v", uc.ExecStartPre)
	}
	if !hasPublish(uc.Args, "8443:8443") {
		t.Fatalf("expected -p 8443:8443 publish, got args: %v", uc.Args)
	}
	if hasPublish(uc.Args, "[::]:8443:8443") {
		t.Fatalf("v4-only must not publish on [::], got args: %v", uc.Args)
	}
}

func TestUnitConfigDualStack(t *testing.T) {
	m := NewManager(Config{
		Image:      "localhost/ingress:test",
		DataDir:    "/data/ingress",
		TLSHostDir: "/data/tls",
		HostPort:   8443,
		EnableIPv6: true,
	})
	uc := m.unitConfig()

	if len(uc.ExecStartPre) != 1 || !strings.Contains(uc.ExecStartPre[0], "--ipv6") {
		t.Fatalf("dual-stack ExecStartPre must create the network with --ipv6: %v", uc.ExecStartPre)
	}
	if !hasPublish(uc.Args, "8443:8443") {
		t.Fatalf("expected -p 8443:8443 publish, got args: %v", uc.Args)
	}
	if !hasPublish(uc.Args, "[::]:8443:8443") {
		t.Fatalf("dual-stack must also publish on [::], got args: %v", uc.Args)
	}
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
