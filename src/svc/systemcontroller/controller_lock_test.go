package systemcontroller

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func TestLockPackageSerializesSameKey(t *testing.T) {
	t.Parallel()

	s := &SystemControllerHandlers{}

	var running atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup

	for range 10 {
		wg.Go(func() {
			unlock := s.lockPackage("repo", "pkg")
			defer unlock()

			cur := running.Add(1)
			// Track the maximum concurrency observed.
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			running.Add(-1)
		})
	}

	wg.Wait()

	if got := maxConcurrent.Load(); got != 1 {
		t.Fatalf("expected max concurrency of 1, got %d", got)
	}
}

func TestLockPackageDifferentKeysRunConcurrently(t *testing.T) {
	t.Parallel()

	s := &SystemControllerHandlers{}

	var maxConcurrent atomic.Int32
	var running atomic.Int32
	var wg sync.WaitGroup

	for i := range 5 {
		name := fmt.Sprintf("pkg-%d", i)
		wg.Go(func() {
			// Each goroutine locks a different package.
			unlock := s.lockPackage("repo", name)
			defer unlock()

			cur := running.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			running.Add(-1)
		})
	}

	wg.Wait()

	if got := maxConcurrent.Load(); got <= 1 {
		t.Fatalf("expected concurrency > 1 for different keys, got %d", got)
	}
}

// TestConcurrentInstallUninstallSerialized verifies that concurrent install
// and uninstall requests for the same package are serialized by the
// per-package mutex. Without the lock, the uninstall's volume purge could
// delete volumes that a concurrent install just created.
func TestConcurrentInstallUninstallSerialized(t *testing.T) {
	t.Parallel()

	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", `image: nginx:1.0
environment:
  NGINX_HOST: "@hostname@"
network:
  external:
    "@port@": "80"
  internal: {}
volumes: {}
questions:
  hostname:
    query: "What hostname?"
    type: hostname
  port:
    query: "What port?"
    type: port
`)

	inst := packages.InitMockInstallManager()

	// Pre-install the package so uninstall has something to work with.
	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	// Track concurrent handler execution via hooks on the mock.
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32

	trackConcurrency := func() {
		cur := inFlight.Add(1)
		for {
			old := maxInFlight.Load()
			if cur <= old || maxInFlight.CompareAndSwap(old, cur) {
				break
			}
		}
		// Hold the lock briefly to widen the race window.
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
	}

	inst.OnInstall = trackConcurrency
	inst.OnUninstall = trackConcurrency

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Fire concurrent uninstall and install for the same package.
	var wg sync.WaitGroup
	errs := make([]error, 2)

	wg.Go(func() {
		errs[0] = c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", true)
	})
	wg.Go(func() {
		errs[1] = c.InstallPackage(context.TODO(), "nginx", "1.0",
			packages.Responses{"hostname": "example", "port": "8080"}, false, "", false)
	})

	wg.Wait()

	// Both operations should succeed (one blocks on the other).
	for i, err := range errs {
		if err != nil {
			t.Errorf("operation %d failed: %v", i, err)
		}
	}

	// Verify mutual exclusion: max in-flight should be 1 for the same package.
	if got := maxInFlight.Load(); got > 1 {
		t.Fatalf("expected max in-flight 1 (serialized), got %d (concurrent)", got)
	}
}
