package rolodex

import (
	"fmt"
	"slices"
	"strings"
)

// BlocklistProvider is one RBL or DNSBL zone as rendered into rolodex.yml. The
// fields mirror rolodex's own RblProviderConfig exactly.
//
// RefusalCodes is the answer to "what does a refusal look like coming back from
// you". Empty means rolodex's built-in set — which is what every configuration
// written before refusal handling existed already says — and the single entry
// "none" switches refusal detection off for this provider. The two are
// different requests, so an empty list is rendered as an absent key rather than
// as the literal "none".
//
// RefusalCooldownSecs of 0 defers to the list-wide value.
type BlocklistProvider struct {
	Zone                string
	Enabled             bool
	RefusalCodes        []string
	RefusalCooldownSecs uint32
}

// Blocklist is one of rolodex's two provider lists: the RBL (reverse-IP) list
// or the DNSBL (forward-name) list. They have identical shape and independent
// configuration, including their cooldowns.
//
// A zero Blocklist renders as "disabled, no providers", which is exactly the
// state rolodex would default to — so a box that has never configured a
// blocklist renders the same bytes it always did.
type Blocklist struct {
	Enabled             bool
	Providers           []BlocklistProvider
	RefusalCooldownSecs uint32
}

// clone returns a deep copy, so a caller cannot mutate a list the manager is
// holding (and rendering) out from under it.
func (b Blocklist) clone() Blocklist {
	out := Blocklist{Enabled: b.Enabled, RefusalCooldownSecs: b.RefusalCooldownSecs}
	if b.Providers == nil {
		return out
	}
	out.Providers = make([]BlocklistProvider, 0, len(b.Providers))
	for _, p := range b.Providers {
		p.RefusalCodes = slices.Clone(p.RefusalCodes)
		out.Providers = append(out.Providers, p)
	}
	return out
}

// renderBlocklist renders one provider list as a rolodex.yml section.
//
// Writing the lists into the config file — rather than relying on the gRPC
// SetRblConfig/SetDnsblConfig calls alone — is what makes a configured
// blocklist survive. Rolodex holds the provider lists in memory only: it seeds
// them from this file at startup and persists nothing a gRPC call changes, so
// every rolodex restart silently switched every blocklist back off. It also
// gates its ":53 is reachable" probe on a blocklist being enabled *in this
// file*, so a list enabled purely over gRPC never gets that gating and its
// lookups sit and time out on a network that filters outbound :53.
//
// Optional keys are omitted rather than written as zero so a box that has never
// touched refusal handling renders the same bytes it did before the field
// existed — WriteConfig diffs on content, and a gratuitously changed file means
// a gratuitous rolodex restart at boot.
func renderBlocklist(section string, bl Blocklist) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n  enabled: %t\n", section, bl.Enabled)
	if bl.RefusalCooldownSecs > 0 {
		fmt.Fprintf(&b, "  refusal_cooldown_secs: %d\n", bl.RefusalCooldownSecs)
	}
	if len(bl.Providers) == 0 {
		b.WriteString("  providers: []\n")
		return b.String()
	}
	b.WriteString("  providers:\n")
	for _, p := range bl.Providers {
		fmt.Fprintf(&b, "    - zone: %q\n      enabled: %t\n", p.Zone, p.Enabled)
		if len(p.RefusalCodes) > 0 {
			b.WriteString("      refusal_codes:\n")
			for _, c := range p.RefusalCodes {
				fmt.Fprintf(&b, "        - %q\n", c)
			}
		}
		if p.RefusalCooldownSecs > 0 {
			fmt.Fprintf(&b, "      refusal_cooldown_secs: %d\n", p.RefusalCooldownSecs)
		}
	}
	return b.String()
}
