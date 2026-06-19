// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"context"
	"strings"
	"sync"
	"testing"

	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// stubSupervisor records the Caddyfile content handed to Reload without
// touching the filesystem or spawning caddy.
type stubSupervisor struct {
	mu      sync.Mutex
	reloads [][]byte
	starts  int
}

func (s *stubSupervisor) Start() error { s.mu.Lock(); s.starts++; s.mu.Unlock(); return nil }
func (s *stubSupervisor) Reload(content []byte) error {
	s.mu.Lock()
	s.reloads = append(s.reloads, append([]byte(nil), content...))
	s.mu.Unlock()
	return nil
}
func (s *stubSupervisor) Shutdown() error { return nil }
func (s *stubSupervisor) last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reloads) == 0 {
		return ""
	}
	return string(s.reloads[len(s.reloads)-1])
}
func (s *stubSupervisor) reloadCount() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.reloads) }

func route(host, backend, cert string) *ingresspb.Route {
	return &ingresspb.Route{Hostname: host, Backend: backend, CertDir: cert}
}

func TestServerSetRoutes(t *testing.T) {
	sup := &stubSupervisor{}
	srv := NewServer(sup, 443)
	ctx := context.Background()

	_, err := srv.SetRoutes(ctx, &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{
		route("a.asdf.home", "ba:1", "/c/a"),
		route("b.asdf.home", "bb:2", "/c/b"),
	}})
	if err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	list, err := srv.ListRoutes(ctx, &ingresspb.ListRoutesRequest{})
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}
	if got := len(list.GetRoutes()); got != 2 {
		t.Fatalf("expected 2 routes, got %d", got)
	}
	if out := sup.last(); !strings.Contains(out, "a.asdf.home") || !strings.Contains(out, "b.asdf.home") {
		t.Fatalf("reloaded config missing routes:\n%s", out)
	}

	// SetRoutes replaces the whole set.
	if _, err := srv.SetRoutes(ctx, &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{
		route("c.asdf.home", "bc:3", "/c/c"),
	}}); err != nil {
		t.Fatalf("SetRoutes replace: %v", err)
	}
	if out := sup.last(); strings.Contains(out, "a.asdf.home") || !strings.Contains(out, "c.asdf.home") {
		t.Fatalf("SetRoutes did not replace set:\n%s", out)
	}
}

func TestServerAddRemoveRoute(t *testing.T) {
	sup := &stubSupervisor{}
	srv := NewServer(sup, 443)
	ctx := context.Background()

	if _, err := srv.AddRoute(ctx, &ingresspb.AddRouteRequest{Route: route("x.asdf.home", "bx:1", "/c/x")}); err != nil {
		t.Fatalf("AddRoute: %v", err)
	}
	if _, err := srv.AddRoute(ctx, &ingresspb.AddRouteRequest{Route: route("y.asdf.home", "by:2", "/c/y")}); err != nil {
		t.Fatalf("AddRoute: %v", err)
	}
	if out := sup.last(); !strings.Contains(out, "x.asdf.home") || !strings.Contains(out, "y.asdf.home") {
		t.Fatalf("config missing added routes:\n%s", out)
	}

	// Upsert: re-adding x with a new backend replaces it (still 2 routes).
	if _, err := srv.AddRoute(ctx, &ingresspb.AddRouteRequest{Route: route("x.asdf.home", "bx-new:9", "/c/x")}); err != nil {
		t.Fatalf("AddRoute upsert: %v", err)
	}
	list, _ := srv.ListRoutes(ctx, &ingresspb.ListRoutesRequest{})
	if got := len(list.GetRoutes()); got != 2 {
		t.Fatalf("expected 2 routes after upsert, got %d", got)
	}
	if out := sup.last(); !strings.Contains(out, "bx-new:9") {
		t.Fatalf("upsert did not update backend:\n%s", out)
	}

	if _, err := srv.RemoveRoute(ctx, &ingresspb.RemoveRouteRequest{Hostname: "x.asdf.home"}); err != nil {
		t.Fatalf("RemoveRoute: %v", err)
	}
	if out := sup.last(); strings.Contains(out, "x.asdf.home") {
		t.Fatalf("removed route still present:\n%s", out)
	}

	// Removing an absent host is a no-op (no error).
	if _, err := srv.RemoveRoute(ctx, &ingresspb.RemoveRouteRequest{Hostname: "absent.asdf.home"}); err != nil {
		t.Fatalf("RemoveRoute absent: %v", err)
	}
}

func TestServerConcurrentMutations(t *testing.T) {
	sup := &stubSupervisor{}
	srv := NewServer(sup, 443)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			h := string(rune('a'+n%26)) + ".asdf.home"
			if n%2 == 0 {
				_, _ = srv.AddRoute(ctx, &ingresspb.AddRouteRequest{Route: route(h, "b:1", "/c/x")})
			} else {
				_, _ = srv.RemoveRoute(ctx, &ingresspb.RemoveRouteRequest{Hostname: h})
			}
		}(i)
	}
	wg.Wait()
	if sup.reloadCount() == 0 {
		t.Fatal("expected at least one reload under concurrency")
	}
}
