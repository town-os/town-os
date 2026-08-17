// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"context"
	"strings"
	"sync"
	"testing"

	"gitea.com/town-os/town-os/src/i18n"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// stubSupervisor records the Caddyfile content handed to Reload without
// touching the filesystem or spawning caddy. reloadErr, when set, makes every
// Reload fail — what a caddy that rejects the rendered config does, which is
// otherwise unobservable because the child keeps serving the last good one.
type stubSupervisor struct {
	mu        sync.Mutex
	reloads   [][]byte
	starts    int
	reloadErr error
}

func (s *stubSupervisor) Start() error { s.mu.Lock(); s.starts++; s.mu.Unlock(); return nil }
func (s *stubSupervisor) Reload(content []byte) error {
	s.mu.Lock()
	s.reloads = append(s.reloads, append([]byte(nil), content...))
	err := s.reloadErr
	s.mu.Unlock()
	return err
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

// The box's language reaches the rendered config, and a code nobody translated
// is ignored rather than rejected.
//
// The setting is a free-text row in the settings table. Refusing to render on a
// typo would mean an ingress that will not start — the whole box unreachable —
// over the language of one error page, so a bad value costs English and nothing
// else.
func TestServerDefaultLocale(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
		want string
	}{
		{name: "a shipped language", code: "ja-JP", want: "ja-JP"},
		{name: "a country variant", code: "de-AT", want: "de-AT"},
		{name: "nobody translated it", code: "xx-XX", want: i18n.DefaultLocale},
		{name: "unset", code: "", want: i18n.DefaultLocale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer(&stubSupervisor{}, 443, 80, "", WithDefaultLocale(tc.code))
			if srv.locale != tc.want {
				t.Errorf("locale = %q, want %q", srv.locale, tc.want)
			}
		})
	}

	// And it reaches the bytes: the configured catalog is rendered twice — once
	// in its own Accept-Language branch, once as the fallthrough — where a
	// language that is only a branch appears once.
	sup := &stubSupervisor{}
	srv := NewServer(sup, 443, 80, "", WithDefaultLocale("ja-JP"))
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	ja := caddyHTMLText(i18n.T("ja-JP", i18n.MsgIngressUnavailableBody))
	if n := strings.Count(sup.last(), ja); n != 2 {
		t.Errorf("the Japanese page appears %d times, want 2 (its branch and the fallthrough):\n%s", n, sup.last())
	}
	fr := caddyHTMLText(i18n.T("fr-FR", i18n.MsgIngressUnavailableBody))
	if n := strings.Count(sup.last(), fr); n != 1 {
		t.Errorf("the French page appears %d times, want 1 (its branch only)", n)
	}
}

func TestServerSetRoutes(t *testing.T) {
	sup := &stubSupervisor{}
	srv := NewServer(sup, 443, 80, "")
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
	srv := NewServer(sup, 443, 80, "")
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
	srv := NewServer(sup, 443, 80, "")
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

// TestCaddyAdminPortMovesBothEnds asserts the option relocates the admin API in
// the rendered config AND the metrics passthrough that fetches from it.
//
// One port, two consumers, and they are written in different files: the
// Caddyfile global option (which is also what `caddy reload` dials) and the
// scrape URL. Moving one without the other is the failure this guards — an
// ingress whose caddy is on an ephemeral port while its own /metrics keeps
// fetching 2019, which in a shared host namespace does not fail, it silently
// reports some other run's caddy as this one's child.
func TestCaddyAdminPortMovesBothEnds(t *testing.T) {
	const relocated = 41919

	sup := &stubSupervisor{}
	srv := NewServer(sup, 443, 80, "", WithCaddyAdminPort(relocated))
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	if want := "admin 127.0.0.1:41919"; !strings.Contains(sup.last(), want) {
		t.Errorf("rendered Caddyfile missing %q:\n%s", want, sup.last())
	}
	if want := "http://127.0.0.1:41919/metrics"; srv.caddyMetricsURL != want {
		t.Errorf("caddyMetricsURL = %q, want %q", srv.caddyMetricsURL, want)
	}
}

// Without the option both ends stay on caddy's default, which is what the
// production ingress runs: its own container, its own network namespace, and an
// admin API nothing outside it can reach.
func TestCaddyAdminPortDefaults(t *testing.T) {
	sup := &stubSupervisor{}
	srv := NewServer(sup, 443, 80, "")
	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	if want := "admin 127.0.0.1:2019"; !strings.Contains(sup.last(), want) {
		t.Errorf("rendered Caddyfile missing %q:\n%s", want, sup.last())
	}
	if want := "http://127.0.0.1:2019/metrics"; srv.caddyMetricsURL != want {
		t.Errorf("caddyMetricsURL = %q, want %q", srv.caddyMetricsURL, want)
	}
}
