//go:build proton

package account

// ProtonImageKey is the DefaultSettings key for the Proton runner image.
const ProtonImageKey = "proton_image"

// ProtonImageDefault is the default container image used to run Windows
// applications via the Proton/Wine compatibility layer.
const ProtonImageDefault = "quay.io/town/proton:latest"

// Register the default proton_image entry in DefaultSettings at package init
// time. Build-tag-gated registration is the cleanest way to attach a default
// only when the `proton` tag is active; the alternative (an exported Register
// function) leaks a call-order dependency into every caller that touches
// DefaultSettings.
//
//nolint:gochecknoinits // build-tag-gated default registration; see comment above
func init() {
	DefaultSettings[ProtonImageKey] = ProtonImageDefault
}
