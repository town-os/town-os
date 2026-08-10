// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

// Package caddysup owns the lifecycle of a caddy child process driven by a
// rendered Caddyfile. It is shared by the network controller (per-package TLS
// ports) and the ingress service (the shared :443 SNI router): both render a
// Caddyfile in memory and hand it to a CaddySupervisor, which writes it
// atomically and asks caddy to reload zero-downtime.
package caddysup

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Default locations for the caddy binary and its generated config inside the
// container. Both are constants, not variables, because the container image
// layout is fixed at build time. NewSupervisor lets callers override them.
const (
	DefaultCaddyBinary     = "/usr/bin/caddy"
	DefaultCaddyConfigPath = "/etc/caddy/Caddyfile"
)

// caddy reload talks to the running caddy's admin API (127.0.0.1:2019 by
// default). Immediately after Start() spawns `caddy run`, the admin API may not
// be listening yet, so the first reload after a fresh start can fail with a
// transient connection-refused (exit 1) — especially under load (concurrent
// test-full containers). Retry the reload for a bounded window so a caddy that
// is still coming up does not surface as a hard error; a genuine config error
// still fails every attempt and is returned once the window elapses.
const (
	caddyReloadDeadline   = 20 * time.Second
	caddyReloadRetryDelay = 250 * time.Millisecond
)

// CaddySupervisor owns the lifecycle of the caddy child process.
// Implementations must be safe for concurrent Start/Reload/Shutdown calls —
// callers hold their own mutex when they invoke these, but the supervisor also
// reaps the child on unexpected exit in the background, which makes its
// internal state concurrent.
//
// The production implementation is osCaddySupervisor. Tests use a stub that
// records calls and returns success without touching the filesystem or
// spawning processes.
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

// osCaddySupervisor is the production implementation backed by real os/exec
// invocations against a caddy binary on disk. It holds a single child process
// at a time and tracks the last-rendered Caddyfile content so Reload can no-op
// cheaply when reconcile produces identical output.
type osCaddySupervisor struct {
	binary     string
	configPath string

	// Reload-retry tuning; zero means use the package defaults
	// (caddyReloadDeadline / caddyReloadRetryDelay). Tests set short values.
	reloadDeadline   time.Duration
	reloadRetryDelay time.Duration

	mu       sync.Mutex
	proc     *exec.Cmd
	lastSent []byte
}

func (s *osCaddySupervisor) reloadDeadlineValue() time.Duration {
	if s.reloadDeadline > 0 {
		return s.reloadDeadline
	}
	return caddyReloadDeadline
}

func (s *osCaddySupervisor) reloadRetryDelayValue() time.Duration {
	if s.reloadRetryDelay > 0 {
		return s.reloadRetryDelay
	}
	return caddyReloadRetryDelay
}

// NewCaddySupervisor returns the production supervisor pointed at the default
// in-container paths.
func NewCaddySupervisor() CaddySupervisor {
	return NewSupervisor(DefaultCaddyBinary, DefaultCaddyConfigPath)
}

// NewSupervisor returns a production supervisor pointed at the given caddy
// binary and config path. Tests use this to point at an ephemeral binary or
// config location so make test-full stays conflict-free.
func NewSupervisor(binary, configPath string) CaddySupervisor {
	return &osCaddySupervisor{binary: binary, configPath: configPath}
}

// Start launches `caddy run --config <configPath>` if no child is already
// running. The config file must already exist on disk when Start is called —
// the caller's pipeline writes a minimal Caddyfile at boot before invoking
// Start for exactly this reason.
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
	// Unexpected exits are logged but not auto-restarted here — the caller's
	// run loop observes caddy via its next Reload call and can restart if
	// needed. This keeps restart policy in one place (the reconcile loop)
	// rather than split across a background goroutine.
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

// Reload writes the given Caddyfile content to disk atomically, then asks the
// running caddy child to reload its config. When the bytes are identical to
// what was last written Reload does nothing — this keeps the reconcile loop
// cheap (it can call Reload unconditionally on every state change without
// worrying about churn).
//
// If caddy is not running yet Reload writes the config and spawns it, which
// also handles the first-boot case where reconcile fires before Start has been
// called explicitly.
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

	// Retry the reload for a bounded window: a fresh `caddy run` may not have its
	// admin API listening yet, so the first reload can transiently fail with
	// connection-refused. A genuine config error fails every attempt and is
	// returned once the deadline passes.
	deadline := time.Now().Add(s.reloadDeadlineValue())
	var reloadErr error
	for attempt := 1; ; attempt++ {
		cmd := exec.Command(s.binary, "reload", "--config", s.configPath) //nolint:gosec,noctx // G204 -- binary path is a trusted constant
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if reloadErr = cmd.Run(); reloadErr == nil {
			slog.Info("caddy reloaded")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("caddy reload (gave up after %d attempts): %w", attempt, reloadErr)
		}
		slog.Warn(fmt.Sprintf("caddy reload attempt %d failed, retrying: %v", attempt, reloadErr))
		time.Sleep(s.reloadRetryDelayValue())
	}
}

// Shutdown sends SIGKILL to the child process if it is running. Callers should
// use this during shutdown only; normal reconcile churn goes through Reload.
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

// writeCaddyfileAtomic writes content to path via a tmp file + rename so caddy
// never sees a half-written config during a reload.
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
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
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
