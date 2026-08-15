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

// ResolvedDropInPath is the one file ConfigureResolvedRouting writes.
//
// It is exported because its ABSENCE is an assertion: a box whose rolodex has
// been relocated off :53 must not have one, since a per-domain resolved server
// address carries no port and would blackhole every query for the TLD. The
// integration harness checks for it by name, and a check spelling the path out
// for itself would keep passing after the path moved.
var ResolvedDropInPath = filepath.Join(resolvedDropInDir, resolvedDropInFile)

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
//
// NOTHING IN A TEST MAY CALL THIS. It writes a real path in the real /etc and
// signals the real systemd-resolved, which on a developer's box is the machine's
// own resolver; the harness treats a town-os.conf left behind by a test run as a
// bug in the harness. The write itself is [writeResolvedDropIn], which takes the
// directory as an argument precisely so it can be exercised against a temporary
// one.
func ConfigureResolvedRouting(ctx context.Context, tld, loopbackAddr string) {
	changed, err := writeResolvedDropIn(resolvedDropInDir, tld, loopbackAddr)
	if err != nil {
		slog.Debug(fmt.Sprintf("write resolved drop-in: %v", err))
		return
	}
	if !changed {
		return
	}

	// Send SIGHUP to systemd-resolved so it re-reads the drop-in.
	if err := reloadResolved(ctx); err != nil {
		slog.Debug(fmt.Sprintf("reload systemd-resolved: %v", err))
	}
}

// writeResolvedDropIn writes the routing drop-in for tld into dir and reports
// whether the file's content changed. A false with a nil error means the box is
// already routing that TLD there and resolved does not need signalling.
//
// An empty tld or loopbackAddr is not an error: it is a box that has not settled
// on a TLD yet, or a rolodex with no address to route to, and neither is
// something to write a half-formed drop-in about.
func writeResolvedDropIn(dir, tld, loopbackAddr string) (bool, error) {
	if tld == "" || loopbackAddr == "" {
		return false, nil
	}

	content := fmt.Sprintf("[Resolve]\nDNS=%s\nDomains=~%s\n", loopbackAddr, tld)

	if err := os.MkdirAll(dir, 0755); err != nil { //nolint:gosec // system config directory must be world-readable
		return false, fmt.Errorf("create resolved drop-in dir: %w", err)
	}

	filePath := filepath.Join(dir, resolvedDropInFile)

	// Skip write if content is unchanged.
	existing, readErr := os.ReadFile(filePath) //nolint:gosec // G304 -- dir is a constant in production, a temp dir in tests
	if readErr == nil && string(existing) == content {
		return false, nil
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil { //nolint:gosec // system config must be readable by resolved
		return false, fmt.Errorf("write %s: %w", filePath, err)
	}
	return true, nil
}

// reloadResolved sends SIGHUP to systemd-resolved so it re-reads its
// drop-in config files. This avoids "systemctl restart" which is refused
// on systems where resolved has RefuseManualStop=yes.
func reloadResolved(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "pkill", "-HUP", "systemd-resolved")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}
