// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

// Package gfehctl is the systemcontroller-side lifecycle controller for gfeh:
// it renders each partition's gfehd.yaml, generates and installs the systemd
// unit, and exposes the admin socket path the systemcontroller dials.
//
// It lives apart from src/gfeh for the same reason ingressctl lives apart from
// src/ingress: this package imports src/systemd, which pulls in cgo via
// sdjournal, and src/gfeh's config renderer and admin client are worth keeping
// testable without libsystemd. The split is not organisational tidiness — drop
// it and every `go test ./src/gfeh` needs a C toolchain.
package gfehctl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/systemd"
)

const (
	// DefaultSMBPortBase is where per-network SMB ports start.
	//
	// Never 445. That port is privileged, and it is singular — only one
	// partition on the box could ever hold it, so choosing it would make the
	// first network special and every later one broken. It would also collide
	// with a concurrent `make test-full`, which the IRON RULE forbids.
	DefaultSMBPortBase = 4450

	// containerUser is the uid:gid gfehd runs as, as podman spells it.
	containerUser = "2000:2000"
)

// Config holds everything one partition's daemon needs.
type Config struct {
	// Systemd installs and starts the unit.
	Systemd systemd.Manager
	// Network is the Town OS network this partition serves, and its name.
	Network string
	// BtrfsBasePath roots every path the manager derives.
	BtrfsBasePath string
	// Image is the gfehd container image.
	Image string
	// PullNever adds --pull=never, for a locally built image.
	PullNever bool

	// SMBPort is the host port the SMB view binds. Zero disables the view
	// entirely, which is the right answer when the operator has not enabled
	// it: a view with no bind address contributes no name and serves nothing.
	SMBPort int

	// No TownOS field, and so no way to render a town_os section.
	//
	// That section names an account gfehd would authenticate to the control
	// plane as, and Town OS has none to give it: the partition's subvolume and
	// quota are provisioned from this side before the daemon starts, and its
	// principals are created over the admin socket, so the credential bought
	// nothing and cost an administrator account nobody created sitting in every
	// box's user list. Rendering it is not merely unused here -- there is no
	// longer any way to ask for it.
	//
	// Key overrides the derived service key. Tests set a unique value so a
	// test daemon can never collide with a production one — IRON RULE.
	Key string
	// NetworkName overrides the podman network. Tests set a unique value for
	// the same reason.
	NetworkName string
}

// Manager controls one partition's gfehd.
type Manager struct {
	cfg Config
}

// NewManager creates a manager for one network's partition.
func NewManager(cfg Config) *Manager { return &Manager{cfg: cfg} }

func (m *Manager) key() string {
	if m.cfg.Key != "" {
		return m.cfg.Key
	}
	return gfeh.ServiceKey(m.cfg.Network)
}

// Key returns the system-service key used for unit and container naming.
func (m *Manager) Key() string { return m.key() }

// Network returns the Town OS network this partition serves.
func (m *Manager) Network() string { return m.cfg.Network }

func (m *Manager) network() string {
	if m.cfg.NetworkName != "" {
		return m.cfg.NetworkName
	}
	return systemd.IngressNetworkName
}

// SocketPath returns the host path of the admin socket.
func (m *Manager) SocketPath() string {
	return gfeh.SocketPath(m.cfg.BtrfsBasePath, m.cfg.Network)
}

// ConfigPath returns the host path of the rendered gfehd.yaml.
func (m *Manager) ConfigPath() string {
	return gfeh.ConfigPath(m.cfg.BtrfsBasePath, m.cfg.Network)
}

// Client returns an admin-socket client for this partition.
func (m *Manager) Client() *gfeh.UnixClient {
	return gfeh.NewClient(m.SocketPath())
}

// SystemService describes the partition so the controller can list it and
// include it in system updates.
type SystemService struct {
	Key         string
	DisplayName string
	Image       string
	Port        string
	UnitName    string
}

// SystemServices returns metadata for this partition's daemon.
//
// Omitting a service here is why the ingress went a release without ever being
// re-pulled: /system-services/refresh iterates this list, so a service absent
// from it never advances past the image it first started with.
func (m *Manager) SystemServices() []SystemService {
	port := ""
	if m.cfg.SMBPort > 0 {
		port = strconv.Itoa(m.cfg.SMBPort)
	}
	return []SystemService{{
		Key:         m.key(),
		DisplayName: "Object Storage (" + m.cfg.Network + ")",
		Image:       m.cfg.Image,
		Port:        port,
		UnitName:    systemd.SystemServiceUnitName(m.key()),
	}}
}

// RenderConfig builds this partition's gfehd configuration.
//
// smbUsers is the credential table: an account name, its NT hash, and the
// principal it acts as. Empty means the SMB view does not verify, which gfehd
// warns about at startup — so it is empty only when nobody has enrolled a
// credential.
func (m *Manager) RenderConfig(smbUsers []gfeh.SmbUserConfig) ([]byte, error) {
	cfg := gfeh.Config{
		DataDir:     gfeh.ContainerDataDir,
		Partition:   m.cfg.Network,
		AdminSocket: gfeh.ContainerSocketPath,

		// Fixed container-side ports for the four HTTP views. Identical across
		// every partition on purpose: each container has its own network
		// namespace and publishes no host port, so ten partitions all serving
		// S3 on 9000 are ten different sockets. Nothing to allocate, nothing to
		// persist, and nothing that can collide with a concurrent test run.
		S3: &gfeh.S3Config{
			Bind:     bindAll(gfeh.PortS3),
			Hostname: gfeh.ViewLabel(gfeh.ViewS3),
		},
		HTTP: &gfeh.HTTPConfig{
			Bind:     bindAll(gfeh.PortHTTP),
			Hostname: gfeh.ViewLabel(gfeh.ViewHTTP),
		},
		Drive: &gfeh.DriveConfig{
			Bind:     bindAll(gfeh.PortDrive),
			Hostname: gfeh.ViewLabel(gfeh.ViewDrive),
		},
		IPFS: &gfeh.IPFSConfig{
			Bind:     bindAll(gfeh.PortIPFS),
			Hostname: gfeh.ViewLabel(gfeh.ViewIPFS),
		},
	}

	// The network is absent for the default network and named otherwise.
	// Absent means "the default" to gfeh; an empty string would ask it to
	// publish under a zone called "".
	if !gfeh.IsDefaultNetwork(m.cfg.Network) {
		network := m.cfg.Network
		cfg.Network = &network
	}

	// Nothing renders a town_os section. The credentials below are unrelated to
	// it: those authenticate end users to gfehd's views, whereas town_os named
	// a Town OS account for the daemon itself, which no longer exists.

	// SMB is the one view that needs a real host port, because it is not HTTP
	// and cannot sit behind the ingress. Off unless a port was assigned.
	if m.cfg.SMBPort > 0 {
		cfg.SMB = &gfeh.SMBConfig{
			Bind:      bindAll(uint16(m.cfg.SMBPort)), //nolint:gosec // G115 -- range-checked by validSMBPort before assignment
			Share:     m.cfg.Network,
			Principal: gfeh.SMBFallbackPrincipal,
			Users:     smbUsers,
			Hostname:  gfeh.ViewLabel(gfeh.ViewSMB),
		}
	}

	return gfeh.RenderConfig(cfg)
}

// bindAll is a listen address on every interface.
//
// All interfaces rather than loopback: the four HTTP views are reached by the
// ingress across a podman network, and SMB is reached from the LAN. Binding
// loopback would make each view reachable only from inside its own container.
func bindAll(port uint16) string {
	return "0.0.0.0:" + strconv.Itoa(int(port))
}

// unitConfig builds the systemd unit.
func (m *Manager) unitConfig() systemd.SystemServiceUnitConfig {
	base := m.cfg.BtrfsBasePath
	partitionDir := gfeh.PartitionDir(base, m.cfg.Network)
	configDir := gfeh.ConfigDir(base, m.cfg.Network)
	runDir := gfeh.RunDir(base, m.cfg.Network)

	args := []string{
		"--net", m.network(),
		"--user", containerUser,
		// The partition, at the path gfehd's data_dir/partition resolves to.
		// No :z — a relabel is recursive and a partition can hold terabytes.
		"-v", partitionDir + ":" + gfeh.ContainerPartitionDir(m.cfg.Network) + ":rw",
		// The config, read-only: gfehd must not rewrite what reconcile derives.
		"-v", configDir + ":" + gfeh.ContainerConfigDir + ":ro",
		// The socket directory, read-write.
		"-v", runDir + ":" + gfeh.ContainerRunDir + ":rw",
	}

	// SMB is published identically inside and out, so /v1/names reports a port
	// a client can actually dial and Town OS does not have to translate it.
	if m.cfg.SMBPort > 0 {
		port := strconv.Itoa(m.cfg.SMBPort)
		args = append(args, "-p", port+":"+port)
	}

	return systemd.SystemServiceUnitConfig{
		Key:         m.key(),
		Description: "Object storage (gfeh partition " + m.cfg.Network + ")",
		Image:       m.cfg.Image,
		PullNever:   m.cfg.PullNever,
		VolumeDirs:  []string{configDir, runDir},
		Args:        args,
		ExecStartPre: []string{
			// The ingress may not have created it yet; whoever gets there
			// first wins and the other is a no-op.
			"-/usr/bin/podman network create " + m.network(),
			// The socket directory has to be writable by the uid gfehd runs
			// as. A bind mount passes host ownership straight through, and
			// SystemServiceUnitConfig has no HostVolumeMounts field, so this
			// is hand-rolled — the same shape monitoring uses for Prometheus.
			fmt.Sprintf("/bin/chown %d:%d %s", gfeh.UID, gfeh.GID, runDir),
			"/bin/chmod 0770 " + runDir,
		},
	}
}

// Start installs, enables and (re)starts the unit.
//
// restart is what the caller decided: reconcile compares the rendered config
// and the generated unit against what is on disk and only asks for a restart
// when something actually changed. Bouncing every partition on every reconcile
// would drop in-flight uploads hourly for no reason.
func (m *Manager) Start(ctx context.Context, restart bool) error {
	for _, dir := range []string{gfeh.ConfigDir(m.cfg.BtrfsBasePath, m.cfg.Network), gfeh.RunDir(m.cfg.BtrfsBasePath, m.cfg.Network)} {
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // the container process must be able to traverse these
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	uf := systemd.GenerateSystemServiceUnit(m.unitConfig())
	if err := m.cfg.Systemd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
		return fmt.Errorf("install unit %s: %w", uf.Name, err)
	}
	if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
		return fmt.Errorf("enable unit %s: %w", uf.Name, err)
	}

	if restart {
		if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Stop); err != nil {
			slog.Debug("stop gfeh before restart (may not be running)", "unit", uf.Name, "error", err)
		}
	}
	if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Start); err != nil {
		return fmt.Errorf("start unit %s: %w", uf.Name, err)
	}
	return nil
}

// UnitContent returns the unit this manager would install, so a caller can
// diff it against what is already on disk.
func (m *Manager) UnitContent() (name, content string) {
	uf := systemd.GenerateSystemServiceUnit(m.unitConfig())
	return uf.Name, uf.Content
}

// Remove stops, disables and uninstalls the unit.
//
// The partition's subvolume is deliberately left alone. A network being
// removed is not a statement about the bytes stored under it, and deleting
// them here would make a mistyped network name unrecoverable. Purging is
// POST /gfeh/partitions/remove, which says what it does.
func (m *Manager) Remove(ctx context.Context) error {
	unit := systemd.SystemServiceUnitName(m.key())

	var errs []error
	if err := m.cfg.Systemd.SetStatus(ctx, unit, systemd.Stop); err != nil {
		slog.Debug("stop gfeh unit", "unit", unit, "error", err)
	}
	if err := m.cfg.Systemd.SetStatus(ctx, unit, systemd.Disable); err != nil {
		slog.Debug("disable gfeh unit", "unit", unit, "error", err)
	}
	if err := m.cfg.Systemd.UninstallUnit(ctx, unit); err != nil {
		errs = append(errs, fmt.Errorf("uninstall unit %s: %w", unit, err))
	}

	// The socket outlives the process that bound it, and a stale one makes the
	// next start fail with "address in use" on a path nothing is listening to.
	if err := os.Remove(m.SocketPath()); err != nil && !os.IsNotExist(err) {
		slog.Debug("remove gfeh socket", "path", m.SocketPath(), "error", err)
	}

	return errors.Join(errs...)
}

// WaitForReady polls the admin socket until it accepts a connection, then
// confirms the daemon answers on it.
//
// Both halves matter: a socket file that accepts a connection is not yet a
// daemon that has opened its partition, and programming DNS from a partition
// that cannot answer /v1/names would publish nothing for it.
func (m *Manager) WaitForReady(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	socket := m.SocketPath()
	client := m.Client()
	var dialer net.Dialer
	for {
		conn, err := dialer.DialContext(waitCtx, "unix", socket)
		if err == nil {
			if closeErr := conn.Close(); closeErr != nil {
				slog.Debug("close gfeh probe connection", "error", closeErr)
			}
			if _, healthErr := client.Health(waitCtx); healthErr == nil {
				return nil
			}
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("gfeh socket %s not ready: %w", socket, waitCtx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// SMBPortFor derives a network's SMB port from its position in the network
// list.
//
// Deterministic rather than allocated, so nothing has to be persisted and a
// reconcile produces the same port every time. base is overridable
// (GFEH_SMB_PORT_BASE) so the integration harness can take a range of its own
// and never collide with a production daemon or a concurrent test run.
func SMBPortFor(base, index int) int {
	if base <= 0 {
		base = DefaultSMBPortBase
	}
	return base + index
}

// ValidSMBPort reports whether a derived port is usable.
//
// Rejects the privileged range outright: gfehd runs unprivileged and could not
// bind one anyway, so a base that reached down there would produce a unit that
// starts and immediately dies.
func ValidSMBPort(port int) bool {
	return port > 1024 && port <= 65535
}

// UnitDir is where generated units land, exposed for tests that inspect them.
func UnitDir(base string) string { return filepath.Join(base, "units") }
