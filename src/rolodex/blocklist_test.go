package rolodex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// blocklistConfigFor renders a full rolodex.yml carrying the given lists.
func blocklistConfigFor(rbl, dnsbl Blocklist) string {
	return rolodexConfig(rolodexConfigParams{
		Port:        DefaultDNSPort,
		Forwarders:  DefaultForwarders,
		Mode:        DefaultResolutionMode,
		MetricsPort: DefaultMetricsPort,
		RBL:         rbl,
		DNSBL:       dnsbl,
	})
}

// A box that has never configured a blocklist must render exactly the
// "disabled, no providers" shape rolodex defaults to. Both sections are always
// written: rolodex previously received no dnsbl section at all, so the DNSBL
// list had no way to be restored from the config file.
func TestRolodexConfigEmptyBlocklistsRenderDisabled(t *testing.T) {
	cfg := blocklistConfigFor(Blocklist{}, Blocklist{})

	for _, want := range []string{
		"rbl:\n  enabled: false\n  providers: []\n",
		"dnsbl:\n  enabled: false\n  providers: []\n",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("config missing %q:\n%s", want, cfg)
		}
	}
}

func TestRolodexConfigRendersConfiguredBlocklists(t *testing.T) {
	cfg := blocklistConfigFor(
		Blocklist{
			Enabled:             true,
			RefusalCooldownSecs: 900,
			Providers: []BlocklistProvider{
				{Zone: "zen.spamhaus.org", Enabled: true, RefusalCodes: []string{"127.255.255.0/24"}, RefusalCooldownSecs: 1800},
				{Zone: "bl.spamcop.net", Enabled: false},
			},
		},
		Blocklist{
			Enabled:   true,
			Providers: []BlocklistProvider{{Zone: "dbl.spamhaus.org", Enabled: true}},
		},
	)

	for _, want := range []string{
		"rbl:\n  enabled: true\n  refusal_cooldown_secs: 900\n  providers:\n",
		"    - zone: \"zen.spamhaus.org\"\n      enabled: true\n",
		"      refusal_codes:\n        - \"127.255.255.0/24\"\n",
		"      refusal_cooldown_secs: 1800\n",
		"    - zone: \"bl.spamcop.net\"\n      enabled: false\n",
		"dnsbl:\n  enabled: true\n  providers:\n    - zone: \"dbl.spamhaus.org\"\n      enabled: true\n",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("config missing %q:\n%s", want, cfg)
		}
	}
}

// An empty refusal-code list means "use rolodex's built-in set" and MUST NOT be
// rendered as the literal "none", which means the opposite (detection off). A
// zero cooldown means "defer to the list-wide value" and is omitted rather than
// written as 0.
func TestRenderBlocklistOmitsUnsetOptionalKeys(t *testing.T) {
	got := renderBlocklist("rbl", Blocklist{
		Enabled:   true,
		Providers: []BlocklistProvider{{Zone: "zen.spamhaus.org", Enabled: true}},
	})

	if strings.Contains(got, "refusal_codes") {
		t.Fatalf("empty refusal codes must not be rendered:\n%s", got)
	}
	if strings.Contains(got, "refusal_cooldown_secs") {
		t.Fatalf("zero cooldown must not be rendered:\n%s", got)
	}
}

// "none" is how an operator switches refusal detection off for one provider.
// Collapsing it to an empty list would silently turn detection back on.
func TestRenderBlocklistPreservesNone(t *testing.T) {
	got := renderBlocklist("dnsbl", Blocklist{
		Enabled:   true,
		Providers: []BlocklistProvider{{Zone: "private.dnsbl", Enabled: true, RefusalCodes: []string{"none"}}},
	})

	if !strings.Contains(got, "      refusal_codes:\n        - \"none\"\n") {
		t.Fatalf("expected \"none\" preserved:\n%s", got)
	}
}

// SetBlocklists must copy: a caller that reuses its slice must not be able to
// change what the manager renders after the fact.
func TestSetBlocklistsCopiesCallerSlices(t *testing.T) {
	m := NewManager(Config{})
	providers := []BlocklistProvider{{Zone: "zen.spamhaus.org", Enabled: true, RefusalCodes: []string{"127.0.0.1"}}}
	m.SetBlocklists(Blocklist{Enabled: true, Providers: providers}, Blocklist{})

	providers[0].Zone = "mutated.example"
	providers[0].RefusalCodes[0] = "127.0.0.9"

	rbl, _ := m.Blocklists()
	if rbl.Providers[0].Zone != "zen.spamhaus.org" {
		t.Fatalf("zone mutated through caller slice: %q", rbl.Providers[0].Zone)
	}
	if rbl.Providers[0].RefusalCodes[0] != "127.0.0.1" {
		t.Fatalf("refusal code mutated through caller slice: %q", rbl.Providers[0].RefusalCodes[0])
	}

	// The returned copy must be just as isolated.
	rbl.Providers[0].Zone = "also-mutated.example"
	again, _ := m.Blocklists()
	if again.Providers[0].Zone != "zen.spamhaus.org" {
		t.Fatalf("zone mutated through returned copy: %q", again.Providers[0].Zone)
	}
}

// The whole point of writing the lists to disk: a rolodex restarted by anything
// other than the systemcontroller reads them back from this file.
func TestRewriteConfigWritesBlocklists(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(Config{DataDir: dir})

	if _, err := m.RewriteConfig(); err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}

	m.SetBlocklists(
		Blocklist{Enabled: true, Providers: []BlocklistProvider{{Zone: "dbl.spamhaus.org", Enabled: true}}},
		Blocklist{Enabled: true, Providers: []BlocklistProvider{{Zone: "multi.surbl.org", Enabled: true}}},
	)
	written, err := m.RewriteConfig()
	if err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}
	if !written {
		t.Fatal("expected rolodex.yml to change when the blocklists did")
	}

	data, err := os.ReadFile(filepath.Join(dir, "rolodex.yml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "dbl.spamhaus.org") || !strings.Contains(got, "multi.surbl.org") {
		t.Fatalf("blocklists missing from written config:\n%s", got)
	}

	// Rewriting with the same lists must not churn the file — WriteConfig
	// diffs on content at boot and a changed file means a rolodex restart.
	written, err = m.RewriteConfig()
	if err != nil {
		t.Fatalf("RewriteConfig (second): %v", err)
	}
	if written {
		t.Fatal("unchanged blocklists must not rewrite rolodex.yml")
	}
}
