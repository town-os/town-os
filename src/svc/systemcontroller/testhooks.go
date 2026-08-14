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

// TestSetImageExistsLocally replaces the package-level imageExistsLocally
// probe, the companion seam to TestSetPullImage: EnsureImage and the monitoring
// backend switch both ask it whether an image is already on the box, and
// without a stub that question reaches a podman the test environment does not
// have (and answers "no", sending the caller down the fetch path).
//
// This is a test hook, not a production API. Production code must never
// call it.
func TestSetImageExistsLocally(fn func(ctx context.Context, image string) bool) (restore func()) {
	prev := imageExistsLocally
	imageExistsLocally = fn
	return func() { imageExistsLocally = prev }
}

// TestSetSelfImage makes RunningImageRef and RunningImageID answer with the
// given reference and image id, standing in for the `podman inspect` of this
// process's own container that SelfUpdate cannot do in a test binary. An empty
// ref (and id) simulates "not running under podman".
//
// This is a test hook, not a production API. Production code must never
// call it.
func TestSetSelfImage(ref, id string) (restore func()) {
	prev := inspectSelf
	inspectSelf = func(_ context.Context, format string) string {
		switch format {
		case "{{.ImageName}}":
			return ref
		case "{{.Image}}":
			return id
		default:
			return ""
		}
	}
	return func() { inspectSelf = prev }
}

// TestSetLocalImageID replaces the local-store resolution of a reference to an
// image id — the "what does this tag name NOW" half of the self-update
// comparison.
//
// This is a test hook, not a production API. Production code must never
// call it.
func TestSetLocalImageID(fn func(ctx context.Context, ref string) string) (restore func()) {
	prev := localImageID
	localImageID = fn
	return func() { localImageID = prev }
}
