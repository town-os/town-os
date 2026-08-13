// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"regexp"
	"strings"
	"testing"
)

// A cross build needs the TARGET arch's libc headers, and nothing about the
// apt line says so out loud.
//
// Debian's cross gcc packages only *Recommend* the matching libc headers
// (libc6-dev-<arch>-cross / libc6-dev:<arch>), and every apt-get in this repo
// runs --no-install-recommends. Install the cross gcc without naming the libc
// dev and you get a toolchain that cannot compile a single C file for the
// architecture it exists to target.
//
// The failure is slow and misleading. It happens deep in the build, inside
// runtime/cgo or a crate's build script, as:
//
//	/usr/include/stdlib.h:26:10: fatal error: bits/libc-header-start.h
//
// That path exists — it is the HOST's libc-dev — so it reads like a corrupt
// image rather than a missing package; only the arch-specific bits/ under
// /usr/include/<triple>/ is absent. And it is invisible to every native build,
// so `make test-full`, `make release` and `make push-rc` on an x86_64 host all
// pass while `TARGET=aarch64 make push-rc` dies.
//
// Static, like containerfile_bun_cache_test.go: no build, no podman. Checking
// this by building would mean running a cross compile per test run.

// crossBuildContainerfiles are the Containerfiles that install a cross gcc.
// The ingress and network controller build CGO_ENABLED=0, so they need no C
// toolchain at all and are correctly absent here.
var crossBuildContainerfiles = []string{"Containerfile", "Containerfile.gfeh"}

// TestCrossBuildsInstallTargetLibcHeaders asserts every Containerfile that
// installs a cross gcc also installs the target arch's libc headers in the same
// apt invocation.
func TestCrossBuildsInstallTargetLibcHeaders(t *testing.T) {
	t.Parallel()

	for _, name := range crossBuildContainerfiles {
		body := readRepoFile(t, name)
		if !strings.Contains(body, "cross_gcc") {
			t.Errorf("%s no longer installs a cross gcc; drop it from crossBuildContainerfiles or restore the cross path", name)
			continue
		}
		if !strings.Contains(body, `"libc6-dev:${TARGETARCH}"`) {
			t.Errorf("%s installs a cross gcc without libc6-dev:${TARGETARCH}; --no-install-recommends drops the target's libc headers and the cross build dies in cgo on bits/libc-header-start.h", name)
		}
	}
}

// TestCrossToolchainInstalledInOneAptInvocation asserts the libc dev is
// requested by the same apt-get that installs the cross gcc.
//
// Splitting them across two apt-get lines would work, but only until someone
// reorders or conditionalises one of them. Keeping the toolchain in a single
// invocation is what makes the dependency legible at the point of use, and it
// is how both files are written today.
func TestCrossToolchainInstalledInOneAptInvocation(t *testing.T) {
	t.Parallel()

	// The apt-get install command, across its line continuations, up to the
	// terminating `;`.
	install := regexp.MustCompile(`(?s)apt-get install[^;]*\$\{cross_gcc\}[^;]*;`)

	for _, name := range crossBuildContainerfiles {
		m := install.FindString(readRepoFile(t, name))
		if m == "" {
			t.Errorf("%s: could not find the apt-get install that adds ${cross_gcc}", name)
			continue
		}
		if !strings.Contains(m, "libc6-dev:${TARGETARCH}") {
			t.Errorf("%s installs ${cross_gcc} and libc6-dev in different apt-get invocations:\n%s", name, m)
		}
	}
}
