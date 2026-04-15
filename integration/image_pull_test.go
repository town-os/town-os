// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

// ensureImagePulled runs `podman pull` for the given image exactly once per
// process. Integration tests that spawn real containers must call this before
// any `podman run` so a stale local cache can't pin us to an obsolete config
// schema (see https://gitea.com/town-os/rolodex-dns unified dns.bind schema).
// Pull errors are ignored: if podman can't reach the registry, the container
// start will fail with a clearer error and the individual test will report it.
func ensureImagePulled(img string) {
	v, _ := imagePullOnce.LoadOrStore(img, &sync.Once{})
	once, ok := v.(*sync.Once)
	if !ok {
		return
	}
	once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_ = exec.CommandContext(ctx, "podman", "pull", img).Run()
	})
}

var imagePullOnce sync.Map
