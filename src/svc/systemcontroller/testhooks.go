package systemcontroller

import "context"

// TestSetPullImage replaces the package-level pullImage function with a
// caller-supplied stub for the duration of the test, returning a
// restore func. Used by integration tests that drive
// refreshSystemServices without a real podman daemon — every call site
// in the production path goes through this var.
//
// This is a test hook, not a production API. Production code must never
// call it.
func TestSetPullImage(fn func(ctx context.Context, image string) error) (restore func()) {
	prev := pullImage
	pullImage = fn
	return func() { pullImage = prev }
}
