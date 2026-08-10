// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/gfeh/gfehctl"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// stubGfehRegistry drives the collectors without podman, systemd or a real
// gfehd. The collectors are the part most worth testing here, and requiring a
// container to test them would mean not testing them.
type stubGfehRegistry struct {
	clients map[string]gfeh.Client
}

func (s stubGfehRegistry) Clients() map[string]gfeh.Client        { return s.clients }
func (s stubGfehRegistry) Managers() map[string]*gfehctl.Manager  { return nil }

func allViews(partition, network string) *gfeh.MockClient {
	return gfeh.NewMockClient(partition, network,
		gfeh.Name{Hostname: "s3.gfeh", View: gfeh.ViewS3, Port: gfeh.PortS3},
		gfeh.Name{Hostname: "http.gfeh", View: gfeh.ViewHTTP, Port: gfeh.PortHTTP},
		gfeh.Name{Hostname: "drive.gfeh", View: gfeh.ViewDrive, Port: gfeh.PortDrive},
		gfeh.Name{Hostname: "ipfs.gfeh", View: gfeh.ViewIPFS, Port: gfeh.PortIPFS},
		gfeh.Name{Hostname: "smb.gfeh", View: gfeh.ViewSMB, Port: 4450},
	)
}

func gfehTLSCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

// TestCollectGfehSitesComposesTheZone. gfeh answers with a label and has no
// opinion about zones; composing the fully-qualified name is Town OS's job, and
// this is where it happens.
func TestCollectGfehSitesComposesTheZone(t *testing.T) {
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{
		"home": allViews("home", ""),
	}}

	// Five views, plus the index the collector contributes for a partition with
	// at least one view the ingress fronts.
	sites := collectGfehSites(gfehTLSCtx(t), reg, nil, "home", "")
	if len(sites) != 6 {
		t.Fatalf("got %d sites, want 6 (5 views + index): %+v", len(sites), sites)
	}
	byView := map[string]GfehSite{}
	for _, s := range sites {
		byView[s.View] = s
	}
	if got := byView[gfeh.ViewS3].FQDN; got != "s3.gfeh.home" {
		t.Errorf("s3 FQDN = %q, want s3.gfeh.home", got)
	}
	// Three labels, like a package (<name>.<repo>.<tld>) with gfeh in the repo
	// slot. Two would collide with the page namespace, which is <domain>.<tld>.
	if got := byView[gfeh.ViewHTTP].FQDN; got != "http.gfeh.home" {
		t.Errorf("http FQDN = %q", got)
	}
}

// TestCollectGfehSitesUsesTheNetworkTLD: a partition on the office network is
// s3.gfeh.office, never s3.gfeh.home.
func TestCollectGfehSitesUsesTheNetworkTLD(t *testing.T) {
	nm := account.InitMockNetworkManager()
	if _, err := nm.Create(&account.Network{Name: "office", TLD: "office", Enabled: true}); err != nil {
		t.Fatalf("create network: %v", err)
	}
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{
		"office": allViews("office", "office"),
	}}

	sites := collectGfehSites(gfehTLSCtx(t), reg, nm, "home", "office")
	if len(sites) == 0 {
		t.Fatal("no sites for the office partition")
	}
	for _, s := range sites {
		if got := s.FQDN[len(s.FQDN)-7:]; got != ".office" {
			t.Errorf("%s is not under the network TLD", s.FQDN)
		}
	}
}

// TestCollectGfehSitesSelectsByNetwork mirrors the collectPageHostnames /
// collectNetworkPageHostnames split: the empty filter is the default network
// only, because the two callers publish into different zones.
func TestCollectGfehSitesSelectsByNetwork(t *testing.T) {
	nm := account.InitMockNetworkManager()
	if _, err := nm.Create(&account.Network{Name: "office", TLD: "office", Enabled: true}); err != nil {
		t.Fatalf("create network: %v", err)
	}
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{
		"home":   allViews("home", ""),
		"office": allViews("office", "office"),
	}}

	for _, s := range collectGfehSites(gfehTLSCtx(t), reg, nm, "home", "") {
		if s.Network != "home" {
			t.Errorf("the default filter picked up %q", s.Network)
		}
	}
	for _, s := range collectGfehSites(gfehTLSCtx(t), reg, nm, "home", "office") {
		if s.Network != "office" {
			t.Errorf("the office filter picked up %q", s.Network)
		}
	}
}

// TestCollectGfehSitesMarksSMBAsNotHTTP. SMB cannot sit behind an HTTP router,
// and a vhost for it would complete a TLS handshake and then fail to speak the
// protocol — worse than no route, because it looks like a broken service.
func TestCollectGfehSitesMarksSMBAsNotHTTP(t *testing.T) {
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{"home": allViews("home", "")}}

	for _, s := range collectGfehSites(gfehTLSCtx(t), reg, nil, "home", "") {
		switch s.View {
		case gfeh.ViewSMB:
			if s.HTTP {
				t.Error("smb was marked as an HTTP view")
			}
			if s.Backend != "" {
				t.Errorf("smb got an ingress backend %q", s.Backend)
			}
		default:
			if !s.HTTP {
				t.Errorf("%s was not marked as an HTTP view", s.View)
			}
			if s.Backend == "" {
				t.Errorf("%s has no ingress backend", s.View)
			}
		}
	}
}

// TestCollectGfehSitesBackendIsTheContainer. The four HTTP views publish no
// host port; the ingress reaches them by container name on the shared network,
// which is what makes the fixed in-container ports safe.
//
// The index is the deliberate exception: it is static HTML Town OS generates,
// not something gfehd serves, so its backend is the pages container. A test
// that lumped it in with the views would be asserting the bug — a vhost aimed
// at gfehd for a path gfehd has no route for.
func TestCollectGfehSitesBackendIsTheContainer(t *testing.T) {
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{"home": allViews("home", "")}}

	for _, s := range collectGfehSites(gfehTLSCtx(t), reg, nil, "home", "") {
		if !s.HTTP {
			continue
		}
		want := "town-os-system--gfeh-home"
		if s.View == gfeh.ViewIndex {
			want = pagesBackend()
			if s.Backend != want {
				t.Errorf("index backend = %q, want the pages container %q", s.Backend, want)
			}
			continue
		}
		if len(s.Backend) < len(want) || s.Backend[:len(want)] != want {
			t.Errorf("%s backend = %q, want the partition's container", s.View, s.Backend)
		}
	}
}

// TestCollectGfehSitesPublishesTheIndex. The index is contributed by the same
// collector as the views so it is carried by the same six derivations — A,
// AAAA, scoped record, DANE pin, leaf SAN, ingress route — rather than by a
// parallel path that could name it differently from the certificate.
func TestCollectGfehSitesPublishesTheIndex(t *testing.T) {
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{"home": allViews("home", "")}}

	var index *GfehSite
	for _, s := range collectGfehSites(gfehTLSCtx(t), reg, nil, "home", "") {
		if s.View == gfeh.ViewIndex {
			index = &s
			break
		}
	}
	if index == nil {
		t.Fatal("no index site was published for a partition serving HTTP views")
	}
	// The parent of every view label, so it needs no name anybody has to learn.
	if index.FQDN != "gfeh.home" {
		t.Errorf("index FQDN = %q, want gfeh.home", index.FQDN)
	}
	if !index.HTTP {
		t.Error("the index was not marked as an HTTP site, so it gets no ingress route")
	}
	// There is no gfeh listener behind this name; the ingress answers it on :443.
	if index.Port != 0 {
		t.Errorf("index Port = %d, want 0", index.Port)
	}
}

// TestCollectGfehSitesPublishesNoIndexWithoutBrowsableViews. An index for a
// partition serving nothing the ingress fronts would be a name, a certificate
// and a route, all to render a page saying there is nothing to see.
func TestCollectGfehSitesPublishesNoIndexWithoutBrowsableViews(t *testing.T) {
	smbOnly := gfeh.NewMockClient("home", "",
		gfeh.Name{Hostname: "smb.gfeh", View: gfeh.ViewSMB, Port: 4450},
	)
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{"home": smbOnly}}

	for _, s := range collectGfehSites(gfehTLSCtx(t), reg, nil, "home", "") {
		if s.View == gfeh.ViewIndex {
			t.Fatal("an index was published for a partition with no browsable view")
		}
	}
}

// TestCollectGfehSitesSkipsAPartitionThatDoesNotAnswer. gfehd answers from its
// config precisely so a reconcile racing a restart is still answered, but a
// daemon that is genuinely down loses its records for this cycle — which beats
// publishing an A record at a port nothing is listening on.
func TestCollectGfehSitesSkipsAPartitionThatDoesNotAnswer(t *testing.T) {
	dead := gfeh.NewMockClient("home", "")
	dead.Errors["Names"] = errors.New("connection refused")

	reg := stubGfehRegistry{clients: map[string]gfeh.Client{
		"home":   dead,
		"office": allViews("office", "office"),
	}}

	if sites := collectGfehSites(gfehTLSCtx(t), reg, nil, "home", ""); len(sites) != 0 {
		t.Errorf("a dead partition contributed %d sites", len(sites))
	}
	// And it does not take the healthy one down with it.
	nm := account.InitMockNetworkManager()
	if _, err := nm.Create(&account.Network{Name: "office", TLD: "office", Enabled: true}); err != nil {
		t.Fatalf("create network: %v", err)
	}
	if sites := collectGfehSites(gfehTLSCtx(t), reg, nm, "home", "office"); len(sites) == 0 {
		t.Error("a dead partition suppressed a healthy one")
	}
}

// TestCollectGfehSitesIsDeterministic. The registry is a map; output that
// reordered itself would make the ingress supervisor reload on every pass and
// would re-issue certificates whose SAN order changed.
func TestCollectGfehSitesIsDeterministic(t *testing.T) {
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{"home": allViews("home", "")}}

	first := collectGfehSites(gfehTLSCtx(t), reg, nil, "home", "")
	for range 10 {
		again := collectGfehSites(gfehTLSCtx(t), reg, nil, "home", "")
		if len(again) != len(first) {
			t.Fatalf("length changed: %d vs %d", len(again), len(first))
		}
		for i := range first {
			if again[i].FQDN != first[i].FQDN || again[i].View != first[i].View {
				t.Fatalf("order changed at %d: %v vs %v", i, again[i], first[i])
			}
		}
	}
}

// TestGfehHTTPFQDNsIsSortedAndExcludesSMB. The sort is load-bearing: IssueLeaf
// is idempotent only when the requested SAN set matches what is on disk, so an
// order that varied would re-issue the certificate every reconcile — churning
// its DANE pin and invalidating every TLSA record published for it.
func TestGfehHTTPFQDNsIsSortedAndExcludesSMB(t *testing.T) {
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{"home": allViews("home", "")}}
	sites := collectGfehSites(gfehTLSCtx(t), reg, nil, "home", "")

	// The index name is in the SAN set too, and must be: it is served over
	// HTTPS from the same :443 listener under the same leaf, so a certificate
	// that omitted it would make the one browsable name on the partition the
	// only one a browser refuses.
	got := gfehHTTPFQDNs(sites, "home")
	want := []string{"drive.gfeh.home", "gfeh.home", "http.gfeh.home", "ipfs.gfeh.home", "s3.gfeh.home"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v (sorted, no smb)", got, want)
		}
	}
}

// TestGfehFQDNAlwaysQualifiesTheLabel.
//
// This test previously asserted the opposite -- that a label which looks like a
// public FQDN is passed through -- because the first implementation delegated to
// pageHostname. That was wrong for every gfeh name, not just the exotic ones: a
// gfeh label is always "<view>.gfeh", isPublicFQDN classifies anything with a dot
// and no matching TLD suffix as public, so every name came back unqualified and
// would have been served under an ACME cert for a domain nobody owns.
//
// TOWNOS_CONTRACT.md settles it: gfeh answers with a label, never an FQDN,
// specifically so the zone stays Town OS's to compose. There is no
// pass-through case.
func TestGfehFQDNAlwaysQualifiesTheLabel(t *testing.T) {
	for _, tc := range []struct{ label, tld, want string }{
		{"s3.gfeh", "home", "s3.gfeh.home"},
		{"http.gfeh", "office", "http.gfeh.office"},
		// A label that merely resembles a public name is still a label.
		{"s3.example.com", "home", "s3.example.com.home"},
		// Idempotent, so a double-qualified name cannot happen.
		{"s3.gfeh.home", "home", "s3.gfeh.home"},
		// Nothing to compose.
		{"", "home", ""},
		{"s3.gfeh", "", ""},
	} {
		if got := gfehFQDN(tc.label, tc.tld); got != tc.want {
			t.Errorf("gfehFQDN(%q, %q) = %q, want %q", tc.label, tc.tld, got, tc.want)
		}
	}
}

// TestCollectGfehTLSASkipsAPartitionWithNoLeaf. Publishing a pin for a
// certificate that does not exist would make every client refuse the
// connection once it did.
func TestCollectGfehTLSASkipsAPartitionWithNoLeaf(t *testing.T) {
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{"home": allViews("home", "")}}
	sites := collectGfehSites(gfehTLSCtx(t), reg, nil, "home", "")

	if entries := collectGfehTLSA(sites, t.TempDir()); len(entries) != 0 {
		t.Errorf("got %d pins with no leaf issued, want 0", len(entries))
	}
}

// TestDedupeIngressRoutesKeepsTheFirst. renderCaddyfile emits one vhost per
// route with no de-duplication, and Caddy rejects a config with two blocks for
// the same hostname — taking down every route on the box, not just the
// duplicate.
func TestDedupeIngressRoutesKeepsTheFirst(t *testing.T) {
	routes := ingressRoutesForTest("a.home", "b.home", "a.home", "c.home")

	got := dedupeIngressRoutes(routes)
	if len(got) != 3 {
		t.Fatalf("got %d routes, want 3", len(got))
	}
	if got[0].GetBackend() != "backend-0" {
		t.Errorf("the first claimant of a.home was dropped; backend = %q", got[0].GetBackend())
	}
	seen := map[string]bool{}
	for _, r := range got {
		if seen[r.GetHostname()] {
			t.Errorf("duplicate hostname survived: %s", r.GetHostname())
		}
		seen[r.GetHostname()] = true
	}
}

// ingressRoutesForTest builds routes with distinguishable backends so a dedupe
// test can tell which of two claimants survived.
func ingressRoutesForTest(hostnames ...string) []*ingresspb.Route {
	out := make([]*ingresspb.Route, 0, len(hostnames))
	for i, h := range hostnames {
		out = append(out, &ingresspb.Route{Hostname: h, Backend: fmt.Sprintf("backend-%d", i)})
	}
	return out
}
