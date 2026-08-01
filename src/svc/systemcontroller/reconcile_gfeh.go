// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/gfeh/gfehctl"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// One gfeh partition per Town OS network, named for it, rooted at the reserved
// gfeh/ subvolume. A partition is one btrfs subvolume, one gfehd process, one
// admin socket, and its own set of users.
//
// This file converges the running daemons onto the set of networks: it creates
// what is missing, restarts only what actually changed, and tears down what no
// longer has a network. Everything is non-fatal — a partition that fails to
// come up must not stop the rest of the box booting, and the next reconcile
// will try again.

// GfehRegistry is the set of live partitions, keyed by network.
//
// An interface rather than the concrete registry so the DNS and ingress
// collectors can be driven by mocks: those are the parts most worth testing,
// and requiring podman to test them would mean not testing them.
type GfehRegistry interface {
	// Clients returns an admin client per network with a running partition.
	Clients() map[string]gfeh.Client
	// Managers returns the lifecycle manager per network.
	Managers() map[string]*gfehctl.Manager
}

// gfehRegistry is the live implementation.
//
// It carries the configuration it was built with, so a later reconcile
// triggered from a handler reuses exactly what boot used. The alternative —
// rebuilding the config at each call site, or reading the image back off a
// live manager — puts the image somewhere it can go stale, and loses it
// entirely on a box where the first partition failed to come up.
type gfehRegistry struct {
	mu       sync.Mutex
	cfg      ReconcileGfehConfig
	managers map[string]*gfehctl.Manager
}

// NewGfehRegistry returns an empty registry, populated by ReconcileGfeh.
func NewGfehRegistry(cfg ReconcileGfehConfig) *gfehRegistry { //nolint:revive // returned to main.go, which stores it as the GfehRegistry interface
	return &gfehRegistry{cfg: cfg, managers: map[string]*gfehctl.Manager{}}
}

// Locked throughout: reconcile runs at boot, from network CRUD, and from the
// periodic pass, while the DNS and ingress collectors read the map from
// whatever goroutine is rebuilding routes.
func (r *gfehRegistry) Clients() map[string]gfeh.Client {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make(map[string]gfeh.Client, len(r.managers))
	for network, m := range r.managers {
		out[network] = m.Client()
	}
	return out
}

func (r *gfehRegistry) Managers() map[string]*gfehctl.Manager {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make(map[string]*gfehctl.Manager, len(r.managers))
	maps.Copy(out, r.managers)
	return out
}

func (r *gfehRegistry) set(network string, m *gfehctl.Manager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.managers[network] = m
}

func (r *gfehRegistry) drop(network string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.managers, network)
}

// ReconcileGfehConfig carries what the reconcile needs.
type ReconcileGfehConfig struct {
	Registry      *gfehRegistry
	NetworkMgr    account.NetworkManager
	AccountMgr    account.Manager
	Storage       storage.Storage
	Systemd       systemd.Manager
	SettingsMgr   account.SettingsManager
	BtrfsBasePath string
	Image         string
	PullNever     bool
	// SMBPortBase overrides the port SMB listeners start from. The integration
	// harness sets a range of its own so a test daemon can never collide with
	// a production one or with a concurrent run — IRON RULE.
	SMBPortBase int
	// NetworkName overrides the podman network, for the same reason.
	NetworkName string
	// KeyPrefix, when set, is prepended to every service key so a test's units
	// are distinguishable from production's.
	KeyPrefix string
	// ControllerURL is the base URL gfehd authenticates to. Empty renders no
	// town_os section, which is how a test drives a partition without a live
	// control plane.
	ControllerURL string
}

// ReconcileGfeh converges the running gfeh daemons onto the set of networks.
//
// Called at boot and after any change to the network set. Safe to call
// repeatedly: a partition whose configuration and unit are unchanged is left
// running rather than bounced, which is what stops the hourly reconcile
// dropping every in-flight upload on the box.
func ReconcileGfeh(ctx context.Context, reg *gfehRegistry) {
	if reg == nil {
		return
	}
	cfg := reg.cfg
	cfg.Registry = reg
	if cfg.NetworkMgr == nil || cfg.Image == "" {
		return
	}

	nets, err := cfg.NetworkMgr.List()
	if err != nil {
		logNonFatal("list networks for gfeh reconcile", err)
		return
	}

	// Sorted so a network's SMB port is a function of the network set rather
	// than of map iteration order — otherwise the port moves whenever another
	// network is added, and every client that had mounted the share breaks.
	sort.Slice(nets, func(i, j int) bool { return nets[i].Name < nets[j].Name })

	names := make([]string, 0, len(nets))
	for _, n := range nets {
		names = append(names, n.Name)
	}

	quota := gfehPartitionQuota(cfg.SettingsMgr)
	smbUsers := collectSMBUsers(cfg.AccountMgr, names)

	// The account gfehd authenticates as. Distinct from every credential in
	// smbUsers: that table is end users authenticating to gfeh's SMB view, this
	// is the daemon authenticating to the control plane.
	//
	// A partition can still serve without it -- it just cannot provision its own
	// subvolume or project an account into its forest -- so a failure here is
	// logged and the reconcile continues.
	var townOS *gfeh.TownOSConfig
	if cfg.ControllerURL != "" {
		username, password, credErr := ensureGfehServiceAccount(cfg.AccountMgr, cfg.SettingsMgr)
		if credErr != nil {
			logNonFatal("gfeh service account", credErr)
		} else {
			townOS = &gfeh.TownOSConfig{
				BaseURL:  cfg.ControllerURL,
				Username: username,
				Password: password,
				Quota:    quota,
			}
		}
	}

	desired := make(map[string]bool, len(nets))
	for i, n := range nets {
		desired[n.Name] = true
		if err := reconcileGfehPartition(ctx, cfg, n.Name, i, quota, smbUsers[n.Name], townOS); err != nil {
			logNonFatal("gfeh partition "+n.Name, err)
		}
	}

	pruneGfehPartitions(ctx, cfg, desired)
}

// reconcileGfeh re-converges the partitions from a handler, then republishes
// the names they contribute.
//
// Best-effort throughout: a partition that fails to come up is a network
// without object storage, not a failed request, and the periodic reconcile is
// the backstop — the same model reprogramIngress and refreshPages use.
func (s *SystemControllerHandlers) reconcileGfeh(ctx context.Context) {
	reg, ok := s.Controller.GetGfehRegistry().(*gfehRegistry)
	if !ok || reg == nil {
		return
	}

	ReconcileGfeh(ctx, reg)

	// The partition set changed, so the derived route set did too.
	s.reprogramIngress(ctx)
}

// reconcileGfehPartition brings one network's partition to the desired state.
func reconcileGfehPartition(ctx context.Context, cfg ReconcileGfehConfig, network string, index int, quota uint64, users []gfeh.SmbUserConfig, townOS *gfeh.TownOSConfig) error {
	port := gfehctl.SMBPortFor(cfg.SMBPortBase, index)
	if !gfehctl.ValidSMBPort(port) {
		// A base that reached into the privileged range would produce a unit
		// that starts and immediately dies, so the view is dropped instead:
		// four of the five still work, and the failure is named here rather
		// than found in a crash loop.
		slog.Warn("gfeh: derived SMB port is unusable; serving without SMB", "network", network, "port", port)
		port = 0
	}
	// SMB with no credential enrolled verifies nothing, so it is served only
	// once somebody can actually authenticate to it. "A view that is not
	// served contributes nothing" is gfeh's own doctrine and it is the right
	// default for the one view whose alternative is an unauthenticated share
	// on the LAN.
	if len(users) == 0 {
		port = 0
	}

	m := gfehctl.NewManager(gfehctl.Config{
		Systemd:       cfg.Systemd,
		Network:       network,
		BtrfsBasePath: cfg.BtrfsBasePath,
		Image:         cfg.Image,
		PullNever:     cfg.PullNever,
		SMBPort:       port,
		TownOS:        townOS,
		Key:           cfg.KeyPrefix + gfeh.ServiceKey(network),
		NetworkName:   cfg.NetworkName,
	})

	if err := ensureGfehPartitionVolume(cfg.Storage, cfg.BtrfsBasePath, network, quota); err != nil {
		return err
	}

	rendered, err := m.RenderConfig(users)
	if err != nil {
		return fmt.Errorf("render config: %w", err)
	}
	configChanged, err := writeGfehConfig(m.ConfigPath(), rendered)
	if err != nil {
		return err
	}

	if err := m.Start(ctx, configChanged || gfehUnitChanged(cfg.Systemd, m)); err != nil {
		return err
	}
	if err := m.WaitForReady(ctx); err != nil {
		// Not fatal: the daemon may still be opening its partition, and the
		// name collectors treat a partition that does not answer as
		// contributing nothing rather than as a reason to fail.
		slog.Debug("gfeh partition not ready yet", "network", network, "error", err)
	}

	cfg.Registry.set(network, m)
	return nil
}

// ensureGfehPartitionVolume creates or resizes the partition's subvolume, with
// the ownership gfehd needs.
//
// In-process against storage.Storage, never through /storage/*: that handler
// rewrites every name to user/<name>, and isReservedFilesystem is a guard
// against users rather than against Town OS itself.
func ensureGfehPartitionVolume(st storage.Storage, btrfsBase, network string, quota uint64) error {
	if st == nil {
		return errors.New("storage unavailable")
	}

	uid, gid := gfeh.UID, gfeh.GID
	fs := storage.Filesystem{
		Name:  gfeh.PartitionVolume(network),
		Quota: quota,
		UID:   &uid,
		GID:   &gid,
	}

	if err := st.CreateFilesystem(fs); err != nil {
		// Already there on every boot but the first, which is the ordinary
		// case. Modify re-asserts the quota and the ownership, so a partition
		// created before the uid was declared is repaired rather than left
		// unwritable forever.
		if modErr := st.ModifyFilesystem(fs.Name, fs); modErr != nil {
			// Last resort: a plain directory is enough for the daemon to
			// serve out of, and losing the qgroup quota beats not starting.
			// StartPagesService's mkdir does the same thing for the same
			// reason — the parent may already exist as a plain dir.
			dir := gfeh.PartitionDir(btrfsBase, network)
			if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil { //nolint:gosec // the container process must traverse it
				return errors.Join(
					fmt.Errorf("create partition volume: %w", err),
					fmt.Errorf("modify partition volume: %w", modErr),
					fmt.Errorf("create partition directory: %w", mkErr),
				)
			}
			if chErr := os.Chown(dir, int(uid), int(gid)); chErr != nil {
				slog.Debug("chown gfeh partition fallback dir", "dir", dir, "error", chErr)
			}
			slog.Debug("gfeh partition is a plain directory, not a subvolume", "network", network)
		}
	}
	return nil
}

// writeGfehConfig writes the rendered config when it differs, and reports
// whether it did.
//
// Compared before writing, not written unconditionally: the return value is
// what decides whether the daemon is restarted, and a config rewritten
// byte-identically every hour would bounce every partition on the box every
// hour. Written via a temp file and renamed so a daemon starting concurrently
// never reads half a config.
func writeGfehConfig(path string, rendered []byte) (bool, error) {
	existing, err := os.ReadFile(path) //nolint:gosec // G304 -- path derived from the trusted btrfs base
	if err == nil && string(existing) == string(rendered) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // the container mounts this read-only and must traverse it
		return false, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	// 0640 and group-readable by gfeh's gid: the file carries NT hashes, which
	// are password-equivalent for SMB.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, rendered, 0o640); err != nil { //nolint:gosec // G306 -- deliberately not world-readable; it holds credentials
		return false, fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chown(tmp, 0, int(gfeh.GID)); err != nil {
		slog.Debug("chown gfeh config", "path", tmp, "error", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return false, fmt.Errorf("rename %s: %w", tmp, err)
	}
	return true, nil
}

// gfehUnitChanged reports whether the unit this manager would install differs
// from what is on disk. Same technique version-change detection uses.
func gfehUnitChanged(sd systemd.Manager, m *gfehctl.Manager) bool {
	if sd == nil {
		return true
	}
	name, content := m.UnitContent()
	existing, err := sd.ReadUnit(name)
	if err != nil {
		// Absent is the first-boot case, not an error: a unit that cannot be
		// read is one that has to be written.
		return true
	}
	return existing != content
}

// pruneGfehPartitions stops and removes daemons whose network is gone.
//
// The subvolume is deliberately left behind. Removing a network is not a
// statement about the bytes stored under it, and deleting them here would make
// a mistyped network name unrecoverable. Purging is POST /gfeh/partitions/remove.
func pruneGfehPartitions(ctx context.Context, cfg ReconcileGfehConfig, desired map[string]bool) {
	if cfg.Systemd == nil {
		return
	}
	units, err := cfg.Systemd.ListUnits(ctx)
	if err != nil {
		logNonFatal("list units for gfeh prune", err)
		return
	}

	for _, u := range units {
		network, ok := gfehNetworkFromUnit(u.Name, cfg.KeyPrefix)
		if !ok || desired[network] {
			continue
		}
		m := gfehctl.NewManager(gfehctl.Config{
			Systemd:       cfg.Systemd,
			Network:       network,
			BtrfsBasePath: cfg.BtrfsBasePath,
			Image:         cfg.Image,
			Key:           cfg.KeyPrefix + gfeh.ServiceKey(network),
			NetworkName:   cfg.NetworkName,
		})
		if err := m.Remove(ctx); err != nil {
			logNonFatal("remove gfeh partition "+network, err)
		}
		cfg.Registry.drop(network)
		slog.Info("removed a gfeh partition whose network is gone", "network", network)
	}
}

// gfehNetworkFromUnit recovers the network a gfeh unit serves.
func gfehNetworkFromUnit(unit, keyPrefix string) (string, bool) {
	key, ok := strings.CutPrefix(unit, systemd.SystemServiceUnitPrefix)
	if !ok {
		return "", false
	}
	key = strings.TrimSuffix(key, ".service")
	if keyPrefix != "" {
		key, ok = strings.CutPrefix(key, keyPrefix)
		if !ok {
			return "", false
		}
	}
	return gfeh.NetworkFromKey(key)
}

// SettingGfehPartitionQuota is the settings key holding a partition's quota in
// bytes. Zero is unlimited.
const SettingGfehPartitionQuota = "gfeh_partition_quota"

// gfehPartitionQuota is the quota a new partition is created with.
//
// Zero means unlimited, and is the default: a partition is the box's object
// storage, and capping it at the per-user volume default would surprise
// somebody the first time a photo library outgrew it.
func gfehPartitionQuota(settingsMgr account.SettingsManager) uint64 {
	if settingsMgr == nil {
		return 0
	}
	raw, err := settingsMgr.Get(SettingGfehPartitionQuota)
	if err != nil || strings.TrimSpace(raw) == "" {
		return 0
	}
	quota, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		slog.Debug("unparseable gfeh partition quota; treating as unlimited", "value", raw)
		return 0
	}
	return quota
}

// collectSMBUsers builds each network's SMB credential table from the accounts
// that have enrolled one.
//
// An account with no SMB credential simply does not appear anywhere, and cannot
// mount any share. That is the correct default: the credential is a second
// secret, weaker at rest than the account's password hash, and only worth
// holding for accounts that actually want SMB.
//
// networks is every network that exists, because scope has to be expanded here
// rather than represented as an absence — a nil "means every network" would be
// indistinguishable from "no networks" to the caller, and getting that backwards
// either locks everyone out or lets a scoped account onto every partition.
func collectSMBUsers(accountMgr account.Manager, networks []string) map[string][]gfeh.SmbUserConfig {
	out := map[string][]gfeh.SmbUserConfig{}
	if accountMgr == nil {
		return out
	}
	accounts, err := accountMgr.List()
	if err != nil {
		logNonFatal("list accounts for gfeh smb credentials", err)
		return out
	}

	for _, a := range accounts {
		// A disabled account keeps its credential row but must not be able to
		// authenticate — the same rule requireAuth applies to a live session.
		if a.Disabled || a.SMBNTHash == "" {
			continue
		}
		entry := gfeh.SmbUserConfig{
			Username:  a.Username,
			NTHash:    a.SMBNTHash,
			Principal: a.Username,
		}
		for _, network := range smbNetworksFor(a, networks) {
			out[network] = append(out[network], entry)
		}
	}

	// Deterministic order, so an unchanged account set renders an unchanged
	// config and does not restart every daemon on the next reconcile.
	for network := range out {
		users := out[network]
		sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
		out[network] = users
	}
	return out
}

// smbNetworksFor is the set of networks an account may authenticate to.
//
// A WireGuard account carries an explicit scope, and an empty scope is never
// read as "any" — Town OS refuses to create such an account, and gfeh's
// clamping has to be equally fail-closed. Any other account is not
// network-scoped at all and reaches every partition.
func smbNetworksFor(a account.Account, all []string) []string {
	if !a.WireGuard {
		return all
	}
	return a.Networks
}
