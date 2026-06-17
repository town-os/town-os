// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package networkcontroller

import "gitea.com/town-os/town-os/src/caddysup"

// CaddySupervisor is re-exported from the shared caddysup package so existing
// networkcontroller call sites (and tests) keep referring to it by this name.
// The supervisor is shared with the ingress service.
type CaddySupervisor = caddysup.CaddySupervisor

// NewCaddySupervisor returns the production supervisor pointed at the default
// in-container caddy binary + config path.
func NewCaddySupervisor() CaddySupervisor {
	return caddysup.NewCaddySupervisor()
}
