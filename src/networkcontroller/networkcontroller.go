// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package networkcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gitea.com/town-os/town-os/src/upnp"
	"github.com/fsnotify/fsnotify"
)

// PortConfig describes a single port's networking requirements.
//
// When TLS is true, the NC hands the port to its embedded Caddy
// supervisor, which binds ExternalPort, terminates TLS with the leaf
// certificate at CertPath/cert.pem + CertPath/key.pem, and reverse-
// proxies plaintext HTTP to <target-container>:InternalPort on the
// shared podman network. Unlike the old in-process byte tunnel, Caddy
// injects X-Forwarded-Proto, X-Forwarded-For, and Host so the backend
// sees the real request shape.
//
// When TLS is false the NC spawns a socat child for the port, preserving
// raw TCP forwarding for non-HTTP services (SSH, databases, etc.).
type PortConfig struct {
	ExternalPort uint16 `json:"external_port"`
	InternalPort uint16 `json:"internal_port"`
	UPnP         bool   `json:"upnp"`
	Forward      bool   `json:"forward"`
	TLS          bool   `json:"tls,omitempty"`
	CertPath     string `json:"cert_path,omitempty"`
}

// PackageNetworkState is the per-package JSON state file written by the
// systemcontroller and watched by the networkcontroller daemon.
type PackageNetworkState struct {
	Repo          string       `json:"repo"`
	Package       string       `json:"package"`
	Version       string       `json:"version"`
	ContainerName string       `json:"container_name"`
	Ports         []PortConfig `json:"ports"`
}

// ExecRunner abstracts process execution for testing.
type ExecRunner interface {
	Start(name string, args ...string) (Process, error)
}

// noopCaddySupervisor is the supervisor tests and plain NewController
// callers get when they do not opt in to a real Caddy. Its Reload and
// Start are successful no-ops; Shutdown returns nil. Production wiring
// swaps this out via SetCaddySupervisor (or uses NewControllerWithCaddy).
type noopCaddySupervisor struct{}

func (noopCaddySupervisor) Start() error         { return nil }
func (noopCaddySupervisor) Reload(_ []byte) error { return nil }
func (noopCaddySupervisor) Shutdown() error      { return nil }

// Process abstracts a running process for testing.
type Process interface {
	Kill() error
	Wait() error
	Pid() int
}

// osProcess wraps exec.Cmd to implement Process.
type osProcess struct {
	cmd *exec.Cmd
}

func (p *osProcess) Kill() error { return p.cmd.Process.Kill() }
func (p *osProcess) Wait() error { return p.cmd.Wait() }
func (p *osProcess) Pid() int    { return p.cmd.Process.Pid }

// osRunner implements ExecRunner using os/exec.
type osRunner struct{}

func (r *osRunner) Start(name string, args ...string) (Process, error) {
	cmd := exec.Command(name, args...) //nolint:gosec,noctx // G204 -- args from trusted internal calls; long-running socat process killed explicitly via Kill(), not context-cancelled
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &osProcess{cmd: cmd}, nil
}

// activeForwarder tracks a running socat process bound to a single
// external port. TLS ports are not tracked here — Caddy owns them.
type activeForwarder struct {
	proc    Process
	intPort uint16
}

// activeMapping tracks a UPnP port mapping.
type activeMapping struct {
	cfg PortConfig
}

// Controller manages socat forwarders, a Caddy reverse proxy, and UPnP
// mappings for a single package. The NC container joins the same podman
// network as the service containers and resolves targets by container
// DNS name (no IP addresses needed).
type Controller struct {
	mu              sync.Mutex
	upnp            upnp.Manager
	runner          ExecRunner
	caddy           CaddySupervisor
	targetContainer string                      // container DNS name on the podman network (socat target)
	forwarders      map[uint16]*activeForwarder // keyed by external port
	mappings        map[uint16]*activeMapping   // keyed by external port
	state           *PackageNetworkState
	retryCh         chan uint16              // dead forwarder notification (ext port)
	reconcileCh     chan struct{}            // delayed re-reconcile trigger
	retryBackoff    map[uint16]time.Duration // per-port exponential backoff
	// successThreshold is how long a forwarder must run before its
	// retry backoff is reset. Resetting on every Start() — the previous
	// behaviour — defeats exponential backoff entirely because every
	// retry would clear the entry before the death handler could read
	// it. Resetting after the proc has stayed alive for this duration
	// implements the intended "reset on a healthy run" semantics.
	// Tests override this to a small value via SetSuccessThreshold.
	successThreshold time.Duration
}

// defaultSuccessThreshold is how long a forwarder must run before
// retry backoff resets in production. 5s is short enough that a
// human-noticed restart still resets, but long enough that the
// rapid death-restart-death cycle of a misconfigured target never
// crosses the boundary and so backoff actually grows.
const defaultSuccessThreshold = 5 * time.Second

// SetSuccessThreshold overrides successThreshold for tests that need
// the reset to happen quickly (or, conversely, want to verify the
// threshold gates the reset). Must be called before Run.
func (c *Controller) SetSuccessThreshold(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.successThreshold = d
}

// NewController creates a new Controller with the given UPnP manager.
// If upnpMgr is nil, UPnP operations are silently skipped.
func NewController(upnpMgr upnp.Manager) *Controller {
	return &Controller{
		upnp:       upnpMgr,
		runner:     &osRunner{},
		caddy:      noopCaddySupervisor{},
		forwarders: make(map[uint16]*activeForwarder),
		mappings:   make(map[uint16]*activeMapping),
	}
}

// NewControllerWithTarget creates a Controller that forwards to the named
// container on the shared podman network.
func NewControllerWithTarget(upnpMgr upnp.Manager, targetContainer string) *Controller {
	return &Controller{
		upnp:            upnpMgr,
		runner:          &osRunner{},
		caddy:           noopCaddySupervisor{},
		targetContainer: targetContainer,
		forwarders:      make(map[uint16]*activeForwarder),
		mappings:        make(map[uint16]*activeMapping),
	}
}

// NewControllerWithRunner creates a Controller with a custom exec runner (for testing).
func NewControllerWithRunner(upnpMgr upnp.Manager, runner ExecRunner) *Controller {
	return &Controller{
		upnp:       upnpMgr,
		runner:     runner,
		caddy:      noopCaddySupervisor{},
		forwarders: make(map[uint16]*activeForwarder),
		mappings:   make(map[uint16]*activeMapping),
	}
}

// NewControllerWithRunnerAndTarget creates a Controller with a custom exec
// runner and target container name (for testing).
func NewControllerWithRunnerAndTarget(upnpMgr upnp.Manager, runner ExecRunner, targetContainer string) *Controller {
	return &Controller{
		upnp:            upnpMgr,
		runner:          runner,
		caddy:           noopCaddySupervisor{},
		targetContainer: targetContainer,
		forwarders:      make(map[uint16]*activeForwarder),
		mappings:        make(map[uint16]*activeMapping),
	}
}

// SetCaddySupervisor installs the Caddy supervisor used for TLS-marked
// ports. Production wiring calls this with NewCaddySupervisor(); tests
// inject a recording stub. Must be called before Run so the first
// reconcile gets the right instance.
func (c *Controller) SetCaddySupervisor(sup CaddySupervisor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if sup == nil {
		sup = noopCaddySupervisor{}
	}
	c.caddy = sup
}

// Run reads the initial state file, starts an fsnotify watcher and a UPnP
// renewal ticker, and runs the reconcile loop until ctx is cancelled.
func (c *Controller) Run(ctx context.Context, statePath string) error {
	// Read initial state.
	state, err := readState(statePath)
	if err != nil {
		return fmt.Errorf("read initial state: %w", err)
	}
	slog.Info(fmt.Sprintf("networkcontroller starting: %s/%s@%s", state.Repo, state.Package, state.Version))

	// If target container was not set via flag, read it from the state file.
	if c.targetContainer == "" && state.ContainerName != "" {
		c.targetContainer = state.ContainerName
	}

	// Initialize retry channels used by startForwarderLocked goroutines to
	// signal dead forwarders back to the Run() select loop.
	c.retryCh = make(chan uint16, 16)
	c.reconcileCh = make(chan struct{}, 1)
	c.retryBackoff = make(map[uint16]time.Duration)
	if c.successThreshold == 0 {
		c.successThreshold = defaultSuccessThreshold
	}

	c.reconcile(state)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer func() {
		closeErr := watcher.Close()
		if closeErr != nil {
			slog.Debug(fmt.Sprintf("close watcher: %v", closeErr))
		}
	}()

	if err := watcher.Add(statePath); err != nil {
		return fmt.Errorf("watch state file: %w", err)
	}

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.Shutdown()
			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				newState, err := readState(statePath)
				if err != nil {
					slog.Error(fmt.Sprintf("re-read state: %v", err))
					continue
				}
				// Update target container from state if it changed.
				if newState.ContainerName != "" {
					c.mu.Lock()
					c.targetContainer = newState.ContainerName
					c.mu.Unlock()
				}
				c.reconcile(newState)
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				// State file removed — tear down everything.
				c.reconcile(&PackageNetworkState{})
				// Re-add watch in case the file is recreated.
				if err := watcher.Add(statePath); err != nil {
					slog.Debug("re-add watcher", "error", err)
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			slog.Error(fmt.Sprintf("watcher error: %v", err))

		case <-ticker.C:
			c.renewUPnP()

		case extPort := <-c.retryCh:
			c.mu.Lock()
			delay := c.retryBackoff[extPort]
			if delay == 0 {
				delay = 1 * time.Second
			} else {
				delay *= 2
				if delay > 30*time.Second {
					delay = 30 * time.Second
				}
			}
			c.retryBackoff[extPort] = delay
			c.mu.Unlock()
			slog.Info(fmt.Sprintf("forwarder %d died, retrying in %v", extPort, delay))
			go func() {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
					select {
					case c.reconcileCh <- struct{}{}:
					default:
					}
				case <-ctx.Done():
				}
			}()

		case <-c.reconcileCh:
			c.mu.Lock()
			lastState := c.state
			c.mu.Unlock()
			if lastState != nil {
				c.reconcile(lastState)
			}
		}
	}
}

// reconcile compares the desired state with the current active state and
// starts/stops socat forwarders, Caddy-fronted TLS sites, and UPnP
// mappings as needed. It detects both port additions/removals and
// configuration changes on existing ports (e.g. internal port, Forward
// flag, TLS flag flipping on or off, or UPnP flag changes).
//
// TLS ports are delegated wholesale to the embedded Caddy supervisor:
// every TLS-marked port in the desired state becomes one site block in
// a freshly rendered Caddyfile, and the supervisor's Reload handles
// no-op vs. start vs. zero-downtime reload. Non-TLS forwarded ports
// continue to be managed by per-port socat children.
func (c *Controller) reconcile(desired *PackageNetworkState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.state = desired

	// Build desired port map.
	desiredPorts := make(map[uint16]PortConfig)
	for _, p := range desired.Ports {
		desiredPorts[p.ExternalPort] = p
	}

	// Remove socat forwarders for ports that are no longer desired as
	// non-TLS forwarded ports. A port flipping to TLS=true is also a
	// teardown reason — Caddy takes it over below.
	for ext, fwd := range c.forwarders {
		d, ok := desiredPorts[ext]
		if !ok || !d.Forward || d.TLS || fwd.intPort != d.InternalPort {
			c.stopForwarderLocked(ext)
		}
	}

	// Remove UPnP mappings for ports that are no longer desired, or whose
	// configuration changed (UPnP turned off, Forward flag changed which
	// affects the mapped internal port, or internal port changed).
	for ext, m := range c.mappings {
		d, ok := desiredPorts[ext]
		if !ok || !d.UPnP || m.cfg.Forward != d.Forward || m.cfg.InternalPort != d.InternalPort {
			c.removeUPnPMappingLocked(ext)
		}
	}

	// Sort desired ports for deterministic ordering.
	sortedPorts := make([]PortConfig, 0, len(desiredPorts))
	for _, p := range desiredPorts {
		sortedPorts = append(sortedPorts, p)
	}
	sort.Slice(sortedPorts, func(i, j int) bool {
		return sortedPorts[i].ExternalPort < sortedPorts[j].ExternalPort
	})

	// Pass 1: socat non-TLS forwarded ports. Stale entries were removed above.
	for _, p := range sortedPorts {
		if p.Forward && !p.TLS {
			if _, exists := c.forwarders[p.ExternalPort]; !exists {
				c.startForwarderLocked(p.ExternalPort, p.InternalPort)
			}
		}
		if p.UPnP {
			if _, exists := c.mappings[p.ExternalPort]; !exists {
				c.addUPnPMappingLocked(p)
			}
		}
	}

	// Pass 2: hand every TLS port to Caddy via a fresh Caddyfile.
	// Reload is a no-op when the rendered bytes match what was last
	// written, so this is safe to call unconditionally on every
	// reconcile. When there are no TLS ports we shut caddy down so the
	// child process does not linger with an empty config.
	sites := CollectCaddySites([]*PackageNetworkState{desired})
	if len(sites) == 0 {
		if err := c.caddy.Shutdown(); err != nil {
			slog.Warn(fmt.Sprintf("caddy shutdown: %v", err))
		}
	} else {
		content := RenderCaddyfile(sites)
		if err := c.caddy.Reload(content); err != nil {
			slog.Error(fmt.Sprintf("caddy reload: %v", err))
		}
	}
}

func (c *Controller) startForwarderLocked(extPort, intPort uint16) {
	target := c.targetContainer
	if target == "" {
		slog.Warn(fmt.Sprintf("no target container for forwarder %d->%d, skipping", extPort, intPort))
		return
	}
	proc, err := c.runner.Start(
		"/usr/bin/socat",
		fmt.Sprintf("TCP-LISTEN:%d,fork,reuseaddr", extPort),
		fmt.Sprintf("TCP:%s:%d", target, intPort),
	)
	if err != nil {
		slog.Error(fmt.Sprintf("start socat %d->%d: %v", extPort, intPort, err))
		return
	}

	c.forwarders[extPort] = &activeForwarder{proc: proc, intPort: intPort}
	slog.Info(fmt.Sprintf("started socat forwarder %d->%s:%d (pid %d)", extPort, target, intPort, proc.Pid()))

	// Reset retry backoff only after the proc has been alive for the
	// success threshold — never on Start itself, otherwise a tight
	// death-restart-death cycle would clear the backoff entry before
	// the death handler doubles it, and exponential backoff would
	// degenerate to a constant 1s.
	if c.retryBackoff != nil && c.successThreshold > 0 {
		threshold := c.successThreshold
		go func() {
			timer := time.NewTimer(threshold)
			defer timer.Stop()
			<-timer.C
			c.mu.Lock()
			defer c.mu.Unlock()
			if current, ok := c.forwarders[extPort]; ok && current.proc == proc {
				delete(c.retryBackoff, extPort)
			}
		}()
	}

	// Reap the child process in the background. If socat exits unexpectedly
	// (e.g. DNS resolution failure on boot), remove the dead entry from the
	// forwarders map and signal the Run() loop to re-reconcile with backoff.
	retryCh := c.retryCh // nil when Run() hasn't been called (direct reconcile in tests)
	go func() {
		if err := proc.Wait(); err != nil {
			slog.Debug(fmt.Sprintf("socat %d->%d exited: %v", extPort, intPort, err))
		}
		if retryCh == nil {
			return
		}
		c.mu.Lock()
		current, ok := c.forwarders[extPort]
		if ok && current.proc == proc {
			delete(c.forwarders, extPort)
			c.mu.Unlock()
			select {
			case retryCh <- extPort:
			default:
			}
			return
		}
		c.mu.Unlock()
	}()
}

func (c *Controller) stopForwarderLocked(extPort uint16) {
	fwd, ok := c.forwarders[extPort]
	if !ok {
		return
	}
	if fwd.proc != nil {
		if err := fwd.proc.Kill(); err != nil {
			slog.Debug(fmt.Sprintf("kill socat %d: %v", extPort, err))
		}
	}
	delete(c.forwarders, extPort)
	slog.Info(fmt.Sprintf("stopped forwarder %d", extPort))
}

func (c *Controller) upnpDescription(cfg PortConfig) string {
	pkgName := ""
	version := ""
	if c.state != nil {
		pkgName = c.state.Package
		version = c.state.Version
	}
	return fmt.Sprintf("Town OS: Forward for %s@%s on %d", pkgName, version, cfg.ExternalPort)
}

func (c *Controller) addUPnPMappingLocked(cfg PortConfig) {
	if c.upnp == nil {
		return
	}

	// When forward=true, socat makes the external port reachable on the host,
	// so UPnP should map ext→ext. When forward=false (bridge mode), podman
	// handles the mapping, so UPnP should map ext→int.
	internalPort := cfg.InternalPort
	if cfg.Forward {
		internalPort = cfg.ExternalPort
	}

	desc := c.upnpDescription(cfg)
	if err := c.upnp.AddPortMapping("TCP", cfg.ExternalPort, internalPort, desc, 1800); err != nil {
		slog.Warn(fmt.Sprintf("UPnP add %d: %v", cfg.ExternalPort, err))
	} else {
		slog.Info(fmt.Sprintf("UPnP mapped %d->%d", cfg.ExternalPort, internalPort))
	}

	c.mappings[cfg.ExternalPort] = &activeMapping{cfg: cfg}
}

func (c *Controller) removeUPnPMappingLocked(extPort uint16) {
	if c.upnp == nil {
		delete(c.mappings, extPort)
		return
	}

	if err := c.upnp.RemovePortMapping("TCP", extPort); err != nil {
		slog.Warn(fmt.Sprintf("UPnP remove %d: %v", extPort, err))
	} else {
		slog.Info(fmt.Sprintf("UPnP removed %d", extPort))
	}
	delete(c.mappings, extPort)
}

// renewUPnP re-adds all active UPnP mappings to refresh their TTL.
func (c *Controller) renewUPnP() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.upnp == nil {
		return
	}

	for _, m := range c.mappings {
		internalPort := m.cfg.InternalPort
		if m.cfg.Forward {
			internalPort = m.cfg.ExternalPort
		}
		desc := c.upnpDescription(m.cfg)
		if err := c.upnp.AddPortMapping("TCP", m.cfg.ExternalPort, internalPort, desc, 1800); err != nil {
			slog.Warn(fmt.Sprintf("UPnP renew %d: %v", m.cfg.ExternalPort, err))
		}
	}
}

// Shutdown removes all UPnP mappings, kills all socat processes, and
// stops the Caddy supervisor if it was running.
func (c *Controller) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for ext := range c.mappings {
		c.removeUPnPMappingLocked(ext)
	}
	for ext := range c.forwarders {
		c.stopForwarderLocked(ext)
	}
	if c.caddy != nil {
		if err := c.caddy.Shutdown(); err != nil {
			slog.Debug(fmt.Sprintf("caddy shutdown: %v", err))
		}
	}
}

// GetForwarders returns a copy of the active forwarder ports (for testing).
func (c *Controller) GetForwarders() map[uint16]uint16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[uint16]uint16, len(c.forwarders))
	for ext, fwd := range c.forwarders {
		out[ext] = fwd.intPort
	}
	return out
}

// GetMappings returns a copy of the active UPnP mapping ports (for testing).
func (c *Controller) GetMappings() map[uint16]PortConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[uint16]PortConfig, len(c.mappings))
	for ext, m := range c.mappings {
		out[ext] = m.cfg
	}
	return out
}

func readState(path string) (_ *PackageNetworkState, err error) {
	path = filepath.Clean(path)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		cerr := f.Close()
		if cerr != nil && err == nil {
			err = cerr
		}
	}()

	var state PackageNetworkState
	if err := json.NewDecoder(f).Decode(&state); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	return &state, nil
}
