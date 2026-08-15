package systemcontroller

import (
	"context"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
)

func testBlocklistSettings() *mockSettingsManager {
	return &mockSettingsManager{values: map[string]string{}}
}

func TestStoredBlocklistRoundtrip(t *testing.T) {
	mgr := testBlocklistSettings()
	want := BlocklistConfigRequest{
		Enabled:             true,
		RefusalCooldownSecs: 900,
		Providers: []BlocklistProviderDTO{
			{Zone: "zen.spamhaus.org", Enabled: true, RefusalCodes: []string{"127.255.255.0/24"}, RefusalCooldownSecs: 1800},
			{Zone: "bl.spamcop.net", Enabled: false},
		},
	}

	if err := saveStoredBlocklist(t.Context(), mgr, want); err != nil {
		t.Fatalf("saveStoredBlocklist: %v", err)
	}

	got, ok := loadStoredBlocklist(t.Context(), mgr)
	if !ok {
		t.Fatal("expected the saved config to load")
	}
	if !got.Enabled || got.RefusalCooldownSecs != 900 || len(got.Providers) != 2 {
		t.Fatalf("unexpected config: %+v", got)
	}
	if got.Providers[0].Zone != "zen.spamhaus.org" || got.Providers[0].RefusalCodes[0] != "127.255.255.0/24" {
		t.Fatalf("unexpected first provider: %+v", got.Providers[0])
	}
	if got.Providers[1].Enabled {
		t.Fatal("a disabled provider must round-trip as disabled")
	}
}

// "never configured" and "configured as empty" are different instructions: the
// first must leave the live server alone, the second must be pushed.
func TestLoadStoredBlocklistDistinguishesUnsetFromEmpty(t *testing.T) {
	mgr := testBlocklistSettings()

	if _, ok := loadStoredBlocklist(t.Context(), mgr); ok {
		t.Fatal("an unwritten setting must not report as configured")
	}
	if _, ok := loadStoredBlocklist(t.Context(), nil); ok {
		t.Fatal("a nil settings manager must not report as configured")
	}

	if err := saveStoredBlocklist(t.Context(), mgr, BlocklistConfigRequest{}); err != nil {
		t.Fatalf("saveStoredBlocklist: %v", err)
	}
	cfg, ok := loadStoredBlocklist(t.Context(), mgr)
	if !ok {
		t.Fatal("an explicitly stored empty config must report as configured")
	}
	if cfg.Enabled || len(cfg.Providers) != 0 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadStoredBlocklistIgnoresGarbage(t *testing.T) {
	mgr := testBlocklistSettings()
	if err := mgr.Set(t.Context(), settingDNSDnsblConfig, "{not json"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, ok := loadStoredBlocklist(t.Context(), mgr); ok {
		t.Fatal("unparseable stored config must not report as configured")
	}
}

func TestBlocklistDrifted(t *testing.T) {
	base := BlocklistConfigRequest{
		Enabled:   true,
		Providers: []BlocklistProviderDTO{{Zone: "dbl.spamhaus.org", Enabled: true}},
	}
	builtins := []string{"127.255.255.0/24", "127.0.1.255"}

	for _, tc := range []struct {
		name         string
		stored       BlocklistConfigRequest
		liveEnabled  bool
		live         []BlocklistProviderDTO
		liveCooldown uint32
		want         bool
	}{
		{
			// Rolodex resolves an unspecified code list to its built-in set and
			// an unspecified cooldown to its default. Reading either back as
			// drift would re-push an identical config forever — and the DNSBL
			// push flushes the DNS cache, so that is not free.
			name:         "resolved defaults are not drift",
			stored:       base,
			liveEnabled:  true,
			live:         []BlocklistProviderDTO{{Zone: "dbl.spamhaus.org", Enabled: true, RefusalCodes: builtins}},
			liveCooldown: 3600,
			want:         false,
		},
		{
			// The reported failure: rolodex restarted and came back with
			// everything switched off.
			name:        "global toggle switched off underneath us",
			stored:      base,
			liveEnabled: false,
			live:        []BlocklistProviderDTO{{Zone: "dbl.spamhaus.org", Enabled: true, RefusalCodes: builtins}},
			want:        true,
		},
		{
			name:        "provider list emptied",
			stored:      base,
			liveEnabled: true,
			live:        nil,
			want:        true,
		},
		{
			name:        "per-provider toggle switched off",
			stored:      base,
			liveEnabled: true,
			live:        []BlocklistProviderDTO{{Zone: "zen.spamhaus.org", Enabled: false, RefusalCodes: builtins}},
			want:        true,
		},
		{
			name:        "different zone",
			stored:      base,
			liveEnabled: true,
			live:        []BlocklistProviderDTO{{Zone: "bl.spamcop.net", Enabled: true, RefusalCodes: builtins}},
			want:        true,
		},
		{
			name:        "named refusal codes differ",
			stored:      BlocklistConfigRequest{Enabled: true, Providers: []BlocklistProviderDTO{{Zone: "private.example", Enabled: true, RefusalCodes: []string{"none"}}}},
			liveEnabled: true,
			live:        []BlocklistProviderDTO{{Zone: "private.example", Enabled: true, RefusalCodes: builtins}},
			want:        true,
		},
		{
			name:         "named list cooldown differs",
			stored:       BlocklistConfigRequest{Enabled: true, RefusalCooldownSecs: 900, Providers: base.Providers},
			liveEnabled:  true,
			live:         []BlocklistProviderDTO{{Zone: "dbl.spamhaus.org", Enabled: true, RefusalCodes: builtins}},
			liveCooldown: 3600,
			want:         true,
		},
		{
			name:        "per-provider cooldown differs",
			stored:      BlocklistConfigRequest{Enabled: true, Providers: []BlocklistProviderDTO{{Zone: "dbl.spamhaus.org", Enabled: true, RefusalCooldownSecs: 1800}}},
			liveEnabled: true,
			live:        []BlocklistProviderDTO{{Zone: "dbl.spamhaus.org", Enabled: true, RefusalCodes: builtins}},
			want:        true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := blocklistDrifted(tc.stored, tc.liveEnabled, tc.live, tc.liveCooldown); got != tc.want {
				t.Fatalf("blocklistDrifted = %v, want %v", got, tc.want)
			}
		})
	}
}

// The core regression: a rolodex that came back with its blocklists wiped is
// reprogrammed from the persisted configuration.
func TestReconcileBlocklistsRestoresWipedConfig(t *testing.T) {
	mgr := testBlocklistSettings()
	dnsblStored := BlocklistConfigRequest{
		Enabled:   true,
		Providers: []BlocklistProviderDTO{{Zone: "dbl.spamhaus.org", Enabled: true}},
	}
	if err := saveStoredBlocklist(t.Context(), mgr, dnsblStored); err != nil {
		t.Fatalf("save dnsbl: %v", err)
	}

	// A freshly (re)started rolodex: nothing enabled, no providers.
	client := &rolodex.MockClient{}

	if err := ReconcileBlocklists(context.Background(), client, mgr); err != nil {
		t.Fatalf("ReconcileBlocklists: %v", err)
	}

	dnsbl, err := client.GetDnsblConfig(context.Background())
	if err != nil {
		t.Fatalf("GetDnsblConfig: %v", err)
	}
	if !dnsbl.Enabled || len(dnsbl.Providers) != 1 || dnsbl.Providers[0].Zone != "dbl.spamhaus.org" {
		t.Fatalf("DNSBL not restored: %+v", dnsbl)
	}
}

// At steady state the hourly pass must read and write nothing — a re-push is
// not free, because the DNSBL one flushes rolodex's DNS response cache.
func TestReconcileBlocklistsNoOpWhenInSync(t *testing.T) {
	mgr := testBlocklistSettings()
	stored := BlocklistConfigRequest{Enabled: true, Providers: []BlocklistProviderDTO{{Zone: "dbl.spamhaus.org", Enabled: true}}}
	if err := saveStoredBlocklist(t.Context(), mgr, stored); err != nil {
		t.Fatalf("save dnsbl: %v", err)
	}

	client := &rolodex.MockClient{
		DnsblEnabled:   true,
		DnsblProviders: []*upstream.DnsblConfig{{Zone: "dbl.spamhaus.org", Enabled: true}},
	}

	if err := ReconcileBlocklists(context.Background(), client, mgr); err != nil {
		t.Fatalf("ReconcileBlocklists: %v", err)
	}

	for _, call := range client.GetCalls() {
		if call.Method == "SetDnsblConfig" {
			t.Fatalf("an in-sync blocklist must not be re-pushed, got %s", call.Method)
		}
	}
}

// A list nobody has configured must be left entirely alone. Pushing an empty
// config would be Town OS asserting an instruction it was never given.
func TestReconcileBlocklistsSkipsUnconfiguredLists(t *testing.T) {
	mgr := testBlocklistSettings()

	client := &rolodex.MockClient{}
	if err := ReconcileBlocklists(context.Background(), client, mgr); err != nil {
		t.Fatalf("ReconcileBlocklists: %v", err)
	}

	for _, call := range client.GetCalls() {
		if call.Method == "SetDnsblConfig" {
			t.Fatal("an unconfigured DNSBL list must not be pushed")
		}
		if call.Method == "GetDnsblConfig" {
			t.Fatal("an unconfigured DNSBL list must not even be read")
		}
	}
}

func TestReconcileBlocklistsToleratesMissingDependencies(t *testing.T) {
	if err := ReconcileBlocklists(context.Background(), nil, testBlocklistSettings()); err != nil {
		t.Fatalf("nil client: %v", err)
	}
	if err := ReconcileBlocklists(context.Background(), &rolodex.MockClient{}, nil); err != nil {
		t.Fatalf("nil settings: %v", err)
	}
}

// StoredBlocklist is what seeds rolodex.yml at boot, before any gRPC call has
// been made — so it is the mechanism that closes the window where rolodex is up
// with no blocklist at all.
func TestStoredBlocklistSeedsRolodexConfig(t *testing.T) {
	mgr := testBlocklistSettings()
	if err := saveStoredBlocklist(t.Context(), mgr, BlocklistConfigRequest{
		Enabled:             true,
		RefusalCooldownSecs: 900,
		Providers:           []BlocklistProviderDTO{{Zone: "dbl.spamhaus.org", Enabled: true, RefusalCodes: []string{"none"}}},
	}); err != nil {
		t.Fatalf("save dnsbl: %v", err)
	}

	dnsbl := StoredBlocklist(t.Context(), mgr)
	if !dnsbl.Enabled || dnsbl.RefusalCooldownSecs != 900 || len(dnsbl.Providers) != 1 {
		t.Fatalf("unexpected DNSBL seed: %+v", dnsbl)
	}
	if dnsbl.Providers[0].Zone != "dbl.spamhaus.org" || dnsbl.Providers[0].RefusalCodes[0] != "none" {
		t.Fatalf("unexpected DNSBL provider: %+v", dnsbl.Providers[0])
	}

	nilMgr := StoredBlocklist(t.Context(), nil)
	if nilMgr.Enabled || len(nilMgr.Providers) != 0 {
		t.Fatal("a nil settings manager must seed an empty list")
	}
}
