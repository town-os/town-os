package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/rolodex"
)

// Settings key holding the blocklist provider list. There is one list — the
// DNSBL one — since the RBL half was retired.
//
// Rolodex keeps the list in memory ONLY: it seeds it from rolodex.yml at
// startup, and SetDnsblConfig mutates that in-memory state and persists
// nothing. So without this, every configured blocklist silently switched itself
// off the next time rolodex restarted — a crash under Restart=always, a
// system-services refresh, the unit restart a resolution-mode change performs,
// or simply rebooting the box. The operator saw their toggles come back off
// with nothing in any log to say why.
//
// Town OS is the source of truth, and gRPC is the only way it gets there:
// ReconcileBlocklists re-asserts this list whenever the live server has
// drifted. It is NOT rendered into rolodex.yml — that file is the install
// image's bootstrap config, written by ../install's scripts/rolodex-config.sh,
// and it carries no blocklist section at all.
const (
	settingDNSDnsblConfig = "dns_dnsbl_config"
	// Written by builds that still had the RBL provider list. Nothing reads it
	// any more; it is named here so the key is not reused for something else
	// and so a reader of this file knows a stale row may exist in the settings
	// table of an upgraded box.
	settingDNSRblConfigRetired = "dns_rbl_config"
)

var _ = settingDNSRblConfigRetired

// loadStoredBlocklist reads the persisted provider list.
//
// ok is false when the setting has never been written or cannot be parsed, and
// the distinction from an empty config matters: "nothing has ever been
// configured" must not push an empty list over whatever the live server holds,
// while an operator who genuinely turned everything off has a stored
// {enabled:false, providers:[]} that MUST be pushed.
//
// The setting key is not a parameter. There is exactly one list now that the
// RBL half is retired (settingDNSRblConfigRetired), so a key argument would be
// a knob with one setting that every call site had to spell out identically —
// and the one thing it could express, reading one list and writing the other,
// is precisely the bug it would be there to allow.
func loadStoredBlocklist(ctx context.Context, mgr account.SettingsManager) (BlocklistConfigRequest, bool) {
	var cfg BlocklistConfigRequest
	if mgr == nil {
		return cfg, false
	}
	val, err := mgr.Get(ctx, settingDNSDnsblConfig)
	if err != nil || strings.TrimSpace(val) == "" {
		return cfg, false
	}
	if err := json.Unmarshal([]byte(val), &cfg); err != nil {
		slog.Debug("parse stored blocklist config", "key", settingDNSDnsblConfig, "error", err)
		return BlocklistConfigRequest{}, false
	}
	return cfg, true
}

// saveStoredBlocklist persists the provider list. The value stored is the
// validated, normalized config — the same bytes that were pushed to rolodex —
// so a restore cannot reintroduce input the validator would now reject.
//
// Keyless for the same reason as loadStoredBlocklist.
func saveStoredBlocklist(ctx context.Context, mgr account.SettingsManager, cfg BlocklistConfigRequest) error {
	if mgr == nil {
		return errors.New("settings manager not available")
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal blocklist config: %w", err)
	}
	return mgr.Set(ctx, settingDNSDnsblConfig, string(data))
}

// StoredBlocklist returns the persisted DNSBL provider list in the shape the
// rolodex manager holds. Boot uses it to seed the manager before rolodex is
// programmed, so the list the operator configured is what gets pushed rather
// than an empty one.
//
// A list that has never been configured yields the zero Blocklist — disabled,
// no providers — which is what an unconfigured box has always had.
func StoredBlocklist(ctx context.Context, mgr account.SettingsManager) rolodex.Blocklist {
	stored, _ := loadStoredBlocklist(ctx, mgr)
	return blocklistToRolodex(stored)
}

// blocklistToRolodex converts a stored list into the shape the rolodex manager
// holds and ProgramRolodex pushes to the running server.
func blocklistToRolodex(cfg BlocklistConfigRequest) rolodex.Blocklist {
	providers := make([]rolodex.BlocklistProvider, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		providers = append(providers, rolodex.BlocklistProvider{
			Zone:                p.Zone,
			Enabled:             p.Enabled,
			RefusalCodes:        slices.Clone(p.RefusalCodes),
			RefusalCooldownSecs: p.RefusalCooldownSecs,
		})
	}
	return rolodex.Blocklist{
		Enabled:             cfg.Enabled,
		Providers:           providers,
		RefusalCooldownSecs: cfg.RefusalCooldownSecs,
	}
}

// blocklistDrifted reports whether the live provider list differs from the
// stored one in any way the operator asked for.
//
// Two fields are deliberately compared loosely, because rolodex reports what is
// RESOLVED and in effect while the stored config records what was ASKED FOR:
//
//   - A stored refusal cooldown of 0 means "use rolodex's built-in default",
//     which reads back as that default resolved to a number.
//   - An empty stored refusal-code list means "use rolodex's built-in set",
//     which reads back as those codes enumerated.
//
// In both cases a strict comparison would report permanent drift and re-push
// an identical config on every pass — and the DNSBL push flushes the DNS
// response cache, so that is not free. Accepting whatever came back is safe
// because this config has exactly one writer: a live value the operator did
// not ask for can only have come from an earlier stored value, and the moment
// they change it the stored side stops being a wildcard.
func blocklistDrifted(stored BlocklistConfigRequest, liveEnabled bool, live []BlocklistProviderDTO, liveCooldown uint32) bool {
	if stored.Enabled != liveEnabled {
		return true
	}
	if stored.RefusalCooldownSecs != 0 && stored.RefusalCooldownSecs != liveCooldown {
		return true
	}
	if len(stored.Providers) != len(live) {
		return true
	}
	for i, p := range stored.Providers {
		l := live[i]
		if p.Zone != l.Zone || p.Enabled != l.Enabled || p.RefusalCooldownSecs != l.RefusalCooldownSecs {
			return true
		}
		if len(p.RefusalCodes) > 0 && !slices.Equal(p.RefusalCodes, l.RefusalCodes) {
			return true
		}
	}
	return false
}

// ReconcileBlocklists re-asserts the persisted DNSBL provider list in rolodex
// when the live server has drifted from it. One list, not two — the RBL half is
// retired (settingDNSRblConfigRetired).
//
// This is the whole of blocklist persistence, not half of it: rolodex.yml no
// longer carries a blocklist section, so nothing restores the list on a rolodex
// that restarts on its own except this. It covers a push that failed at the
// time, a rolodex re-initialized underneath us, and every restart — including
// the one scripts/rolodex-reload.sh performs on a lease change. It runs from
// RebuildDNS at boot and from the hourly ReconcileDNS drift pass, and costs one
// read and no mutations at steady state.
//
// A list that has never been configured is left entirely alone rather than
// pushed as empty: an unwritten setting is not an instruction.
func ReconcileBlocklists(ctx context.Context, client rolodex.Client, mgr account.SettingsManager) error {
	if client == nil || mgr == nil {
		return nil
	}
	var errs []error
	if stored, ok := loadStoredBlocklist(ctx, mgr); ok {
		if err := reconcileDnsblProviders(ctx, client, stored); err != nil {
			errs = append(errs, fmt.Errorf("dnsbl: %w", err))
		}
	}
	return errors.Join(errs...)
}

// reconcileDnsblProviders pushes the stored DNSBL list when the live one
// differs.
func reconcileDnsblProviders(ctx context.Context, client rolodex.Client, stored BlocklistConfigRequest) error {
	status, err := client.GetDnsblConfig(ctx)
	if err != nil {
		return fmt.Errorf("get config: %w", err)
	}
	if status != nil && !blocklistDrifted(stored, status.Enabled, dnsblProvidersToDTO(status.Providers), status.RefusalCooldownSecs) {
		return nil
	}
	providers := make([]*upstream.DnsblConfig, 0, len(stored.Providers))
	for _, p := range stored.Providers {
		providers = append(providers, &upstream.DnsblConfig{
			Zone:                p.Zone,
			Enabled:             p.Enabled,
			RefusalCodes:        slices.Clone(p.RefusalCodes),
			RefusalCooldownSecs: p.RefusalCooldownSecs,
		})
	}
	slog.Info("restoring DNSBL blocklist configuration in rolodex", "enabled", stored.Enabled, "providers", len(providers))
	return client.SetDnsblConfig(ctx, stored.Enabled, providers, stored.RefusalCooldownSecs)
}

// syncRolodexBlocklistConfig copies the persisted lists onto the rolodex
// manager, so that whatever reprograms the live server next — the boot pass,
// the reprogramming tick after a restart — pushes the operator's current
// lists rather than the ones this process started with.
//
// It writes no file and restarts nothing. rolodex.yml is the install image's
// bootstrap config now; the live server was already programmed over gRPC by
// the caller, and bouncing DNS to apply a change that has already taken effect
// would drop every in-flight resolution for nothing.
//
// That gap used to be paired with one in rolodex, now fixed there: it only
// spawned its ":53 is reachable" probe when the blocklist was enabled *in its
// config file*, so with the list no longer written to that file the probe never
// started and a blocklist configured here degraded on exactly the networks the
// probe exists for. rolodex now spawns `DnsblChecker::resolver_availability_loop`
// unconditionally and gates it on its own runtime enabled flag, so a list turned
// on over gRPC from here gets the probe within seconds.
func (s *SystemControllerHandlers) syncRolodexBlocklistConfig(ctx context.Context) {
	rolMgr := s.Controller.GetRolodex()
	mgr := s.Controller.GetSettingsManager()
	if rolMgr == nil || mgr == nil {
		return
	}
	dnsbl, _ := loadStoredBlocklist(ctx, mgr)
	rolMgr.SetBlocklist(blocklistToRolodex(dnsbl))
}
