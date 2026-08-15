package rolodex

import (
	"slices"
)

// BlocklistProvider is one DNSBL zone as Town OS holds it. The fields mirror
// rolodex's own DnsblProviderConfig exactly.
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

// Blocklist is rolodex's DNSBL provider list — forward names, queried as
// <name>.<zone> — with its own cooldown.
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
