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
type PortConfig struct {
	ExternalPort uint16 `json:"external_port"`
	InternalPort uint16 `json:"internal_port"`
	UPnP         bool   `json:"upnp"`
	Forward      bool   `json:"forward"`
}

// PackageNetworkState is the per-package JSON state file written by the
// systemcontroller and watched by the networkcontroller daemon.
type PackageNetworkState struct {
	Repo        string       `json:"repo"`
	Package     string       `json:"package"`
	Version     string       `json:"version"`
	NetworkMode string       `json:"network_mode"`
	Ports       []PortConfig `json:"ports"`
}

// ExecRunner abstracts process execution for testing.
type ExecRunner interface {
	Start(name string, args ...string) (Process, error)
}

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
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // G204 -- args from trusted internal calls
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		return nil, err
	}
	return &osProcess{cmd: cmd}, nil
}

// activeForwarder tracks a running socat process.
type activeForwarder struct {
	proc     Process
	intPort  uint16
}

// activeMapping tracks a UPnP port mapping.
type activeMapping struct {
	cfg PortConfig
}

// Controller manages socat forwarders and UPnP mappings for a single package.
type Controller struct {
	mu         sync.Mutex
	upnp       upnp.Manager
	runner     ExecRunner
	forwarders map[uint16]*activeForwarder // keyed by external port
	mappings   map[uint16]*activeMapping   // keyed by external port
	state      *PackageNetworkState
}

// NewController creates a new Controller with the given UPnP manager.
// If upnpMgr is nil, UPnP operations are silently skipped.
func NewController(upnpMgr upnp.Manager) *Controller {
	return &Controller{
		upnp:       upnpMgr,
		runner:     &osRunner{},
		forwarders: make(map[uint16]*activeForwarder),
		mappings:   make(map[uint16]*activeMapping),
	}
}

// NewControllerWithRunner creates a Controller with a custom exec runner (for testing).
func NewControllerWithRunner(upnpMgr upnp.Manager, runner ExecRunner) *Controller {
	return &Controller{
		upnp:       upnpMgr,
		runner:     runner,
		forwarders: make(map[uint16]*activeForwarder),
		mappings:   make(map[uint16]*activeMapping),
	}
}

// Run reads the initial state file, starts an fsnotify watcher and a UPnP
// renewal ticker, and runs the reconcile loop until ctx is cancelled.
func (c *Controller) Run(ctx context.Context, statePath string) error {
	// Read initial state.
	state, err := readState(statePath)
	if err != nil {
		return fmt.Errorf("read initial state: %w", err)
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

	err = watcher.Add(statePath)
	if err != nil {
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
		}
	}
}

// reconcile compares the desired state with the current active state and
// starts/stops forwarders and UPnP mappings as needed.
func (c *Controller) reconcile(desired *PackageNetworkState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.state = desired

	// Build desired port map.
	desiredPorts := make(map[uint16]PortConfig)
	for _, p := range desired.Ports {
		desiredPorts[p.ExternalPort] = p
	}

	// Remove ports that are no longer desired.
	for ext := range c.forwarders {
		if _, ok := desiredPorts[ext]; !ok {
			c.stopForwarderLocked(ext)
		}
	}
	for ext := range c.mappings {
		if _, ok := desiredPorts[ext]; !ok {
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

	// Add new ports.
	for _, p := range sortedPorts {
		if p.Forward {
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
}

func (c *Controller) startForwarderLocked(extPort, intPort uint16) {
	proc, err := c.runner.Start(
		"/usr/bin/socat",
		fmt.Sprintf("TCP-LISTEN:%d,fork,reuseaddr", extPort),
		fmt.Sprintf("TCP:127.0.0.1:%d", intPort),
	)
	if err != nil {
		slog.Error(fmt.Sprintf("start socat %d->%d: %v", extPort, intPort, err))
		return
	}

	c.forwarders[extPort] = &activeForwarder{proc: proc, intPort: intPort}
	slog.Info(fmt.Sprintf("started forwarder %d->%d (pid %d)", extPort, intPort, proc.Pid()))

	// Reap the child process in the background.
	go func() {
		err := proc.Wait()
		if err != nil {
			slog.Debug(fmt.Sprintf("socat %d->%d exited: %v", extPort, intPort, err))
		}
	}()
}

func (c *Controller) stopForwarderLocked(extPort uint16) {
	fwd, ok := c.forwarders[extPort]
	if !ok {
		return
	}
	err := fwd.proc.Kill()
	if err != nil {
		slog.Debug(fmt.Sprintf("kill socat %d: %v", extPort, err))
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
	err := c.upnp.AddPortMapping("TCP", cfg.ExternalPort, internalPort, desc, 1800)
	if err != nil {
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

	err := c.upnp.RemovePortMapping("TCP", extPort)
	if err != nil {
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
		err := c.upnp.AddPortMapping("TCP", m.cfg.ExternalPort, internalPort, desc, 1800)
		if err != nil {
			slog.Warn(fmt.Sprintf("UPnP renew %d: %v", m.cfg.ExternalPort, err))
		}
	}
}

// Shutdown removes all UPnP mappings and kills all socat processes.
func (c *Controller) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for ext := range c.mappings {
		c.removeUPnPMappingLocked(ext)
	}
	for ext := range c.forwarders {
		c.stopForwarderLocked(ext)
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
	err = json.NewDecoder(f).Decode(&state)
	if err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	return &state, nil
}
