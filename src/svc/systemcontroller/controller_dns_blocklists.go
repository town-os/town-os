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

// Settings keys holding the two blocklist provider lists.
//
// Rolodex keeps the lists in memory ONLY: it seeds them from rolodex.yml at
// startup, and SetRblConfig/SetDnsblConfig mutate that in-memory state and
// persist nothing. So without these, every configured blocklist silently
// switched itself off the next time rolodex restarted — a crash under
// Restart=always, a system-services refresh, the unit restart a
// resolution-mode change performs, or simply rebooting the box. The operator
// saw their toggles come back off with nothing in any log to say why.
//
// Town OS is the source of truth. What is stored here is pushed back two ways:
// rendered into rolodex.yml, so a rolodex that restarts on its own comes back
// configured, and re-asserted over gRPC by ReconcileBlocklists whenever the
// live server has drifted.
const (
	settingDNSRblConfig   = "dns_rbl_config"
	settingDNSDnsblConfig = "dns_dnsbl_config"
)

// loadStoredBlocklist reads one persisted provider list.
//
// ok is false when the setting has never been written or cannot be parsed, and
// the distinction from an empty config matters: "nothing has ever been
// configured" must not push an empty list over whatever the live server holds,
// while an operator who genuinely turned everything off has a stored
// {enabled:false, providers:[]} that MUST be pushed.
func loadStoredBlocklist(ctx context.Context, mgr account.SettingsManager, key string) (RblConfigRequest, bool) {
	var cfg RblConfigRequest
	if mgr == nil {
		return cfg, false
	}
	val, err := mgr.Get(ctx, key)
	if err != nil || strings.TrimSpace(val) == "" {
		return cfg, false
	}
	if err := json.Unmarshal([]byte(val), &cfg); err != nil {
		slog.Debug("parse stored blocklist config", "key", key, "error", err)
		return RblConfigRequest{}, false
	}
	return cfg, true
}

// saveStoredBlocklist persists one provider list. The value stored is the
// validated, normalized config — the same bytes that were pushed to rolodex —
// so a restore cannot reintroduce input the validator would now reject.
func saveStoredBlocklist(ctx context.Context, mgr account.SettingsManager, key string, cfg RblConfigRequest) error {
	if mgr == nil {
		return errors.New("settings manager not available")
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal blocklist config: %w", err)
	}
	return mgr.Set(ctx, key, string(data))
}

// StoredBlocklists returns the persisted RBL and DNSBL lists in the shape the
// rolodex manager renders into rolodex.yml. Boot uses it to seed the manager
// before rolodex.yml is written, so rolodex starts with the operator's
// blocklists already configured rather than waiting for the gRPC re-assert
// further down the boot sequence.
//
// A list that has never been configured yields the zero Blocklist, which
// renders exactly the "disabled, no providers" section rolodex.yml has always
// carried.
func StoredBlocklists(ctx context.Context, mgr account.SettingsManager) (rbl, dnsbl rolodex.Blocklist) {
	storedRBL, _ := loadStoredBlocklist(ctx, mgr, settingDNSRblConfig)
	storedDNSBL, _ := loadStoredBlocklist(ctx, mgr, settingDNSDnsblConfig)
	return blocklistToRolodex(storedRBL), blocklistToRolodex(storedDNSBL)
}

// blocklistToRolodex converts a stored list into the shape the rolodex manager
// renders into rolodex.yml.
func blocklistToRolodex(cfg RblConfigRequest) rolodex.Blocklist {
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
func blocklistDrifted(stored RblConfigRequest, liveEnabled bool, live []RblProviderDTO, liveCooldown uint32) bool {
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

// ReconcileBlocklists re-asserts the persisted RBL and DNSBL provider lists in
// rolodex when the live server has drifted from them.
//
// This is the repair half of blocklist persistence: rolodex.yml covers a
// rolodex that restarts on its own, and this covers everything else — a push
// that failed at the time, a rolodex whose config file was hand-edited, a
// rolodex re-initialized underneath us. It runs from RebuildDNS at boot and
// from the hourly ReconcileDNS drift pass, and costs two reads and no
// mutations at steady state.
//
// A list that has never been configured is left entirely alone rather than
// pushed as empty: an unwritten setting is not an instruction.
func ReconcileBlocklists(ctx context.Context, client rolodex.Client, mgr account.SettingsManager) error {
	if client == nil || mgr == nil {
		return nil
	}
	var errs []error
	if stored, ok := loadStoredBlocklist(ctx, mgr, settingDNSRblConfig); ok {
		if err := reconcileRblProviders(ctx, client, stored); err != nil {
			errs = append(errs, fmt.Errorf("rbl: %w", err))
		}
	}
	if stored, ok := loadStoredBlocklist(ctx, mgr, settingDNSDnsblConfig); ok {
		if err := reconcileDnsblProviders(ctx, client, stored); err != nil {
			errs = append(errs, fmt.Errorf("dnsbl: %w", err))
		}
	}
	return errors.Join(errs...)
}

// reconcileRblProviders pushes the stored RBL list when the live one differs.
func reconcileRblProviders(ctx context.Context, client rolodex.Client, stored RblConfigRequest) error {
	status, err := client.GetRblConfig(ctx)
	if err != nil {
		return fmt.Errorf("get config: %w", err)
	}
	if status != nil && !blocklistDrifted(stored, status.Enabled, rblProvidersToDTO(status.Providers), status.RefusalCooldownSecs) {
		return nil
	}
	providers := make([]*upstream.RblConfig, 0, len(stored.Providers))
	for _, p := range stored.Providers {
		providers = append(providers, &upstream.RblConfig{
			Zone:                p.Zone,
			Enabled:             p.Enabled,
			RefusalCodes:        slices.Clone(p.RefusalCodes),
			RefusalCooldownSecs: p.RefusalCooldownSecs,
		})
	}
	slog.Info("restoring RBL blocklist configuration in rolodex", "enabled", stored.Enabled, "providers", len(providers))
	return client.SetRblConfig(ctx, stored.Enabled, providers, stored.RefusalCooldownSecs)
}

// reconcileDnsblProviders pushes the stored DNSBL list when the live one
// differs. Separate from the RBL twin rather than generic over both: the
// upstream provider types are distinct structs on distinct RPCs, so folding
// them together would cost more conversion than it saves.
func reconcileDnsblProviders(ctx context.Context, client rolodex.Client, stored RblConfigRequest) error {
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

// syncRolodexBlocklistConfig re-renders rolodex.yml from the persisted lists,
// so a rolodex that restarts without the systemcontroller's involvement comes
// back with the operator's blocklists rather than none.
//
// It deliberately does NOT restart rolodex: the live server has already been
// programmed over gRPC by the caller, so bouncing DNS to apply a change that
// has already taken effect would drop every in-flight resolution for nothing.
//
// The file matters beyond a restart window. Rolodex only spawns its ":53 is
// reachable" probe when a blocklist is enabled *in the config file*, and that
// probe is what stops blocklist lookups sitting and timing out on a network
// that filters outbound :53 — so a list enabled purely over gRPC works but
// degrades badly on exactly the networks the probe exists for.
//
// Best-effort: the persisted settings are the source of truth, and
// ReconcileBlocklists converges the live server regardless.
func (s *SystemControllerHandlers) syncRolodexBlocklistConfig(ctx context.Context) {
	rolMgr := s.Controller.GetRolodex()
	mgr := s.Controller.GetSettingsManager()
	if rolMgr == nil || mgr == nil {
		return
	}
	rbl, _ := loadStoredBlocklist(ctx, mgr, settingDNSRblConfig)
	dnsbl, _ := loadStoredBlocklist(ctx, mgr, settingDNSDnsblConfig)
	rolMgr.SetBlocklists(blocklistToRolodex(rbl), blocklistToRolodex(dnsbl))
	if _, err := rolMgr.RewriteConfig(); err != nil {
		slog.Warn("write rolodex config after blocklist change", "error", err)
	}
}
