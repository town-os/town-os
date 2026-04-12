package rolodex

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

const resolvedDropInDir = "/etc/systemd/resolved.conf.d"
const resolvedDropInFile = "town-os.conf"

// ConfigureResolvedRouting configures systemd-resolved to route queries for
// the given TLD to the rolodex DNS server at loopbackAddr. This enables
// inter-package DNS resolution: container -> aardvark-dns -> systemd-resolved
// -> rolodex (for .tld queries).
//
// The function writes a drop-in config at /etc/systemd/resolved.conf.d/town-os.conf
// and restarts systemd-resolved if the content changed.
//
// Errors are logged but not returned — resolved routing is a convenience
// feature and the system operates normally without it.
func ConfigureResolvedRouting(ctx context.Context, tld, loopbackAddr string) {
	if tld == "" || loopbackAddr == "" {
		return
	}

	content := fmt.Sprintf("[Resolve]\nDNS=%s\nDomains=~%s\n", loopbackAddr, tld)

	if err := os.MkdirAll(resolvedDropInDir, 0755); err != nil { //nolint:gosec // system config directory must be world-readable
		slog.Debug(fmt.Sprintf("create resolved drop-in dir: %v", err))
		return
	}

	filePath := filepath.Join(resolvedDropInDir, resolvedDropInFile)

	// Skip write if content is unchanged.
	existing, readErr := os.ReadFile(filePath) //nolint:gosec // G304 -- path is a constant, not user input
	if readErr == nil && string(existing) == content {
		return
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil { //nolint:gosec // system config must be readable by resolved
		slog.Debug(fmt.Sprintf("write resolved drop-in: %v", err))
		return
	}

	// Send SIGHUP to systemd-resolved so it re-reads the drop-in.
	if err := reloadResolved(ctx); err != nil {
		slog.Debug(fmt.Sprintf("reload systemd-resolved: %v", err))
	}
}

// reloadResolved sends SIGHUP to systemd-resolved so it re-reads its
// drop-in config files. This avoids "systemctl restart" which is refused
// on systems where resolved has RefuseManualStop=yes.
func reloadResolved(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "pkill", "-HUP", "systemd-resolved") //nolint:gosec // G204 -- arguments are constants
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}
