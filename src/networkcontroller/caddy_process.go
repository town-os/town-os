// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package networkcontroller

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Default locations for the Caddy binary and its generated config inside
// the network controller container. Both are constants, not variables,
// because the container image layout is fixed at build time.
const (
	defaultCaddyBinary     = "/usr/bin/caddy"
	defaultCaddyConfigPath = "/etc/caddy/Caddyfile"
)

// CaddySupervisor owns the lifecycle of the caddy child process.
// Implementations must be safe for concurrent Start/Reload/Shutdown calls
// — the NC reconcile loop holds its own mutex when it invokes these, but
// the supervisor also reaps the child on unexpected exit in the
// background, which makes its internal state concurrent.
//
// The production implementation is osCaddySupervisor. Tests use a stub
// that records calls and returns success without touching the filesystem
// or spawning processes.
type CaddySupervisor interface {
	// Start spawns `caddy run --config <path>`. Idempotent: returns nil
	// immediately if the child is already running.
	Start() error
	// Reload writes `content` to the config path atomically (tmp + rename)
	// and, if the content changed, invokes `caddy reload --config <path>`
	// so the new config takes effect without dropping active connections.
	// When the content matches what is already on disk Reload is a no-op.
	Reload(content []byte) error
	// Shutdown kills the child process and cleans up. Idempotent.
	Shutdown() error
}

// osCaddySupervisor is the production implementation backed by real
// os/exec invocations against a caddy binary on disk. It holds a single
// child process at a time and tracks the last-rendered Caddyfile content
// so Reload can no-op cheaply when reconcile produces identical output.
type osCaddySupervisor struct {
	binary     string
	configPath string

	mu       sync.Mutex
	proc     *exec.Cmd
	lastSent []byte
}

// NewCaddySupervisor returns the production supervisor pointed at the
// default in-container paths. Tests build osCaddySupervisor directly when
// they need to point at a different binary or config location.
func NewCaddySupervisor() CaddySupervisor {
	return &osCaddySupervisor{
		binary:     defaultCaddyBinary,
		configPath: defaultCaddyConfigPath,
	}
}

// Start launches `caddy run --config <configPath>` if no child is already
// running. The config file must already exist on disk when Start is
// called — NC's reconcile pipeline writes a minimal Caddyfile at boot
// before invoking Start for exactly this reason.
func (s *osCaddySupervisor) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.proc != nil && s.proc.Process != nil {
		return nil
	}
	if _, err := os.Stat(s.configPath); err != nil {
		return fmt.Errorf("caddy config %s: %w", s.configPath, err)
	}
	cmd := exec.Command(s.binary, "run", "--config", s.configPath) //nolint:gosec,noctx // G204 -- binary path is a trusted constant; long-running process killed via Shutdown, not context-cancelled
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start caddy: %w", err)
	}
	s.proc = cmd
	slog.Info(fmt.Sprintf("caddy started (pid %d)", cmd.Process.Pid))

	// Reap in the background so the process does not become a zombie.
	// Unexpected exits are logged but not auto-restarted here — the NC
	// run loop observes caddy via its next Reload call and can restart
	// if needed. This keeps restart policy in one place (the reconcile
	// loop) rather than split across a background goroutine.
	go func(p *exec.Cmd) {
		err := p.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.proc == p {
			s.proc = nil
		}
		if err != nil {
			slog.Warn(fmt.Sprintf("caddy exited: %v", err))
		} else {
			slog.Info("caddy exited cleanly")
		}
	}(cmd)

	return nil
}

// Reload writes the given Caddyfile content to disk atomically, then asks
// the running caddy child to reload its config. When the bytes are
// identical to what was last written Reload does nothing — this keeps the
// reconcile loop cheap (it can call Reload unconditionally on every state
// change without worrying about churn).
//
// If caddy is not running yet Reload writes the config and spawns it,
// which also handles the first-boot case where reconcile fires before
// Start has been called explicitly.
func (s *osCaddySupervisor) Reload(content []byte) error {
	s.mu.Lock()
	if bytesEqual(s.lastSent, content) {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	if err := writeCaddyfileAtomic(s.configPath, content); err != nil {
		return err
	}

	s.mu.Lock()
	s.lastSent = append([]byte(nil), content...)
	needSpawn := s.proc == nil
	s.mu.Unlock()

	if needSpawn {
		return s.Start()
	}

	cmd := exec.Command(s.binary, "reload", "--config", s.configPath) //nolint:gosec,noctx // G204 -- binary path is a trusted constant
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("caddy reload: %w", err)
	}
	slog.Info("caddy reloaded")
	return nil
}

// Shutdown sends SIGKILL to the child process if it is running. Callers
// should use this during NC shutdown only; normal reconcile churn goes
// through Reload.
func (s *osCaddySupervisor) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == nil || s.proc.Process == nil {
		return nil
	}
	err := s.proc.Process.Kill()
	s.proc = nil
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill caddy: %w", err)
	}
	return nil
}

// writeCaddyfileAtomic writes content to path via a tmp file + rename so
// caddy never sees a half-written config during a reload.
func writeCaddyfileAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "Caddyfile-*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup if rename failed.
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName) //nolint:errcheck // best-effort cleanup
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close() //nolint:errcheck // best-effort close on error
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename tmp: %w", err)
	}
	return nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
