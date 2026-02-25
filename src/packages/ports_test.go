package packages

import (
	"strings"
	"testing"
)

func TestFindAvailablePort(t *testing.T) {
	t.Run("returns port in range", func(t *testing.T) {
		port, err := FindAvailablePort(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if port < 10000 || port > 60000 {
			t.Fatalf("port %d out of range 10000-60000", port)
		}
	})

	t.Run("respects excluded ports", func(t *testing.T) {
		// Exclude a large range to test the exclusion logic works.
		excluded := map[uint16]bool{}
		for i := uint16(10000); i <= 10100; i++ {
			excluded[i] = true
		}
		port, err := FindAvailablePort(excluded)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if excluded[port] {
			t.Fatalf("returned excluded port %d", port)
		}
	})

	t.Run("nil excluded works", func(t *testing.T) {
		port, err := FindAvailablePort(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if port == 0 {
			t.Fatal("expected non-zero port")
		}
	})
}

func TestGenerateHostname(t *testing.T) {
	t.Run("includes package name", func(t *testing.T) {
		hostname := GenerateHostname("nginx")
		if !strings.HasPrefix(hostname, "nginx-") {
			t.Fatalf("expected hostname to start with 'nginx-', got %s", hostname)
		}
	})

	t.Run("has correct format", func(t *testing.T) {
		hostname := GenerateHostname("myapp")
		if !strings.HasPrefix(hostname, "myapp-") {
			t.Fatalf("expected hostname to start with 'myapp-', got %s", hostname)
		}
		suffix := strings.TrimPrefix(hostname, "myapp-")
		if len(suffix) != 4 {
			t.Fatalf("expected 4-char suffix, got %q (%d chars)", suffix, len(suffix))
		}
	})

	t.Run("generates different values", func(t *testing.T) {
		seen := map[string]bool{}
		for range 10 {
			h := GenerateHostname("test")
			seen[h] = true
		}
		if len(seen) < 2 {
			t.Fatal("expected different hostnames to be generated")
		}
	})
}
