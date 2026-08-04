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
	// SMBPortBase is no longer consulted: Town OS accounts carry no SMB
	// credential, so no partition serves the SMB view and no host port is
	// allocated for it. Kept so the integration harness's GFEH_SMB_PORT_BASE
	// keeps compiling until that knob is retired with the view itself.
	SMBPortBase int
	// NetworkName overrides the podman network, for the same reason.
	NetworkName string
	// KeyPrefix, when set, is prepended to every service key so a test's units
	// are distinguishable from production's.
	KeyPrefix string
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

	// Sorted so the reconcile visits networks in a stable order: the rendered
	// configs, and therefore which daemons restart, are then a function of the
	// network set rather than of map iteration order.
	sort.Slice(nets, func(i, j int) bool { return nets[i].Name < nets[j].Name })

	quota := gfehPartitionQuota(cfg.SettingsMgr)

	// No town_os section is rendered, and so no credential exists for one.
	//
	// The daemon does not call the control plane at all: reconcileGfehPartition
	// creates and sizes the partition's subvolume below, before the daemon is
	// started, and principals are created from this side over the partition's
	// admin socket by the /gfeh/principals/* handlers. The two things gfehd
	// would have authenticated in order to do are already done for it, so the
	// account it used to authenticate as bought nothing and cost an unexplained
	// administrator in every box's user list.
	//
	// Nor is any SMB credential rendered: a Town OS account does not carry an
	// SMB password. gfehd serves the SMB view only for accounts it can
	// authenticate, so with none enrolled the view is simply not served, and
	// the other four remain.

	desired := make(map[string]bool, len(nets))
	for _, n := range nets {
		desired[n.Name] = true
		if err := reconcileGfehPartition(ctx, cfg, n.Name, quota); err != nil {
			logNonFatal("gfeh partition "+n.Name, err)
		}
	}

	pruneGfehPartitions(ctx, cfg, desired, "its network no longer exists")
}

// ReconcileGfehRegistry re-runs the reconcile for a registry held behind the
// GfehRegistry interface, which is how main.go and the handlers keep it.
//
// A nil registry, or one that is not the live implementation (a mock, in the
// tests that drive the DNS and ingress collectors), is a no-op rather than a
// panic: those callers have no daemons to converge.
func ReconcileGfehRegistry(ctx context.Context, reg GfehRegistry) {
	impl, ok := reg.(*gfehRegistry)
	if !ok || impl == nil {
		return
	}
	ReconcileGfeh(ctx, impl)
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
func reconcileGfehPartition(ctx context.Context, cfg ReconcileGfehConfig, network string, quota uint64) error {
	// SMB is not served. It is the one view that cannot sit behind the ingress
	// and the one that needs a credential of its own -- an NT hash, which
	// cannot be derived from the account password and so meant every user
	// carrying a second password. Town OS accounts do not have one, so there is
	// nobody gfehd could authenticate, and an unauthenticated share on the LAN
	// is not the fallback to take. The other four views are unaffected.
	port := 0

	m := gfehctl.NewManager(gfehctl.Config{
		Systemd:       cfg.Systemd,
		Network:       network,
		BtrfsBasePath: cfg.BtrfsBasePath,
		Image:         cfg.Image,
		PullNever:     cfg.PullNever,
		SMBPort:       port,
		Key:           cfg.KeyPrefix + gfeh.ServiceKey(network),
		NetworkName:   cfg.NetworkName,
	})

	if err := ensureGfehPartitionVolume(cfg.Storage, cfg.BtrfsBasePath, network, quota); err != nil {
		return err
	}

	rendered, err := m.RenderConfig(nil)
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
	if err := m.WaitForReady(ctx); err == nil {
		// The partition is answering, so the box's first account can be seated
		// in it. Only the home partition: that is the one every box has, and
		// the one an operator who has never opened this screen would expect
		// their own files to be in.
		ensureFirstUserPrincipal(ctx, cfg.AccountMgr, m.Client(), network)
	} else {
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
// reason names why a partition is being taken down, so the log does not claim
// a network disappeared when object storage was simply switched off.
func pruneGfehPartitions(ctx context.Context, cfg ReconcileGfehConfig, desired map[string]bool, reason string) {
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
		slog.Info("removed a gfeh partition", "network", network, "reason", reason)
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


// ensureFirstUserPrincipal seats the box's first account in the home partition.
//
// Object storage is what the box is for, and a partition whose forest is empty
// serves nobody: the operator opens the Users tab, finds nothing, and has to
// work out that their own account is not in it. The first account is the one
// that set the box up, so it is the one that gets seated.
//
// Home only. Every box has that partition, and a network added later belongs to
// whoever is given a grant on it -- seating the founder there would hand them a
// namespace somebody else created.
//
// Idempotent by way of gfehd: a principal that already exists comes back 409,
// which is success here, so this runs on every reconcile without accumulating
// anything. Non-fatal throughout -- a partition that cannot seat its first user
// still serves every user already in it.
func ensureFirstUserPrincipal(ctx context.Context, accountMgr account.Manager, client gfeh.Client, network string) {
	if accountMgr == nil || client == nil || !gfeh.IsDefaultNetwork(network) {
		return
	}

	first, err := firstAccount(accountMgr)
	if err != nil {
		logNonFatal("first account for gfeh home partition", err)
		return
	}
	if first == nil {
		return // a box nobody has set up yet; the next reconcile will find one
	}

	_, err = client.CreatePrincipal(ctx, gfeh.Principal{
		Name:    first.Username,
		Ceiling: gfeh.CeilingForAccount(first.Admin),
	})
	switch {
	case err == nil:
		slog.Info("seated the first account in the home object-storage partition", "account", first.Username)
	case errors.Is(err, gfeh.ErrAlreadyExists):
		// Already seated, which is the steady state.
	default:
		logNonFatal("seat first account in gfeh home partition", err)
	}
}

// firstAccount is the earliest-created account on the box, or nil when there is
// none.
//
// Earliest by CreatedAt, with the username as the tie-break: two accounts can
// share a timestamp at second resolution, and a partition whose founder changed
// between reconciles depending on map iteration order would be worse than one
// that picked either consistently.
func firstAccount(accountMgr account.Manager) (*account.Account, error) {
	accounts, err := accountMgr.List()
	if err != nil {
		return nil, err
	}

	var first *account.Account
	for i := range accounts {
		candidate := &accounts[i]
		if first == nil ||
			candidate.CreatedAt.Before(first.CreatedAt) ||
			(candidate.CreatedAt.Equal(first.CreatedAt) && candidate.Username < first.Username) {
			first = candidate
		}
	}
	return first, nil
}
