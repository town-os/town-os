// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/gfeh/gfehctl"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
	"go.yaml.in/yaml/v4"
)

// Object storage end to end, against a real gfehd admin socket.
//
// A stand-in daemon rather than the container: what is worth proving here is
// that Town OS asks for a partition's names over a real Unix socket, composes
// the zone, issues one leaf covering it, and folds the result into both
// reconcilers — none of which needs podman, and all of which is invisible to a
// test that stubs the client.
//
// The remaining half (that gfehd itself answers this shape) is proved by gfeh's
// own suite; the two meet at the wire format in TOWNOS_CONTRACT.md.

// fakeGfehd serves the one route Town OS calls on the admin socket.
type fakeGfehd struct {
	server *http.Server
	calls  int
}

// startFakeGfehd binds a Unix socket inside the test's own temp dir and serves
// GET /v1/names. Per-test path, so concurrent runs cannot collide — IRON RULE.
func startFakeGfehd(t *testing.T, socket string, list gfeh.NameList) *fakeGfehd {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(socket), 0o750); err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}

	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "unix", socket)
	if err != nil {
		t.Fatalf("listen on %s: %v", socket, err)
	}

	fake := &fakeGfehd{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(gfeh.Health{
			Status: "ok", Partition: list.Partition, TownOS: true,
		}); err != nil {
			t.Errorf("encode health: %v", err)
		}
	})
	mux.HandleFunc("/v1/names", func(w http.ResponseWriter, _ *http.Request) {
		fake.calls++
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(list); err != nil {
			t.Errorf("encode names: %v", err)
		}
	})

	fake.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serveErr := fake.server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			t.Errorf("serve: %v", serveErr)
		}
	}()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := fake.server.Shutdown(shutdownCtx); err != nil {
			t.Logf("shutdown fake gfehd: %v", err)
		}
	})

	return fake
}

// gfehTestRegistry is a registry over stand-in daemons, keyed by network.
type gfehTestRegistry struct {
	clients map[string]gfeh.Client
}

func (r gfehTestRegistry) Clients() map[string]gfeh.Client { return r.clients }

// Managers is nil here: this test never starts a container, and every path it
// exercises reads the admin clients rather than the lifecycle managers.
func (r gfehTestRegistry) Managers() map[string]*gfehctl.Manager { return nil }

func namesFor(partition, network string) gfeh.NameList {
	list := gfeh.NameList{
		Partition: partition,
		Names: []gfeh.Name{
			{Hostname: "s3.gfeh", View: gfeh.ViewS3, Port: gfeh.PortS3},
			{Hostname: "http.gfeh", View: gfeh.ViewHTTP, Port: gfeh.PortHTTP},
			{Hostname: "smb.gfeh", View: gfeh.ViewSMB, Port: 4450},
		},
	}
	if network != "" {
		n := network
		list.Network = &n
	}
	return list
}

// TestIntegrationGfehNamesReachDNSAndIngress is the whole contribution model in
// one pass: Town OS asks a real socket for the partition's names, composes the
// zone, issues a leaf, and both reconcilers carry the result.
//
// If any link breaks the partition still runs and still holds its data — it is
// just unreachable by name, which is the failure this test exists to catch.
func TestIntegrationGfehNamesReachDNSAndIngress(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	btrfsBase := filepath.Join(dir, "btrfs")
	if err := os.MkdirAll(btrfsBase, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	socket := gfeh.SocketPath(btrfsBase, "home")
	fake := startFakeGfehd(t, socket, namesFor("home", ""))

	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, systemcontroller.TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	reg := gfehTestRegistry{clients: map[string]gfeh.Client{
		"home": gfeh.NewClient(socket),
	}}
	rolMock := &rolodex.MockClient{}
	ingMock := &ingress.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	ctx := context.Background()

	// --- DNS ---
	// A global IPv6 is supplied so the dual-stack path is exercised: a host
	// with one publishes AAAA alongside every A, and a partition reachable
	// only over v4 on a v6 network is a partition half its clients cannot
	// resolve.
	if err := systemcontroller.RebuildDNS(ctx, systemcontroller.ReconcileDNSConfig{
		Client:        rolMock,
		SettingsMgr:   settings,
		InternalIP:    "192.168.10.50",
		InternalIPv6:  "2001:db8::50",
		BtrfsBasePath: btrfsBase,
		Gfeh:          reg,
	}); err != nil {
		t.Fatalf("RebuildDNS: %v", err)
	}

	if fake.calls == 0 {
		t.Fatal("Town OS never asked the partition for its names; nothing would ever be published")
	}

	for _, fqdn := range []string{"s3.gfeh.home.", "http.gfeh.home.", "smb.gfeh.home."} {
		if !hasRolodexRecord(rolMock, upstream.RecordTypeA, fqdn) {
			t.Errorf("no A record for %s; have %v", fqdn, rolodexRecordNames(rolMock, upstream.RecordTypeA))
		}
		if !hasRolodexRecord(rolMock, upstream.RecordTypeAAAA, fqdn) {
			t.Errorf("no AAAA record for %s; have %v", fqdn, rolodexRecordNames(rolMock, upstream.RecordTypeAAAA))
		}
	}

	// --- Ingress ---
	if err := systemcontroller.RebuildIngress(ctx, ingMock, nil, nil, nil, reg, ca,
		btrfsBase, filepath.Join(dir, "state"), "home", "192.168.10.50"); err != nil {
		t.Fatalf("RebuildIngress: %v", err)
	}

	routes := map[string]string{}
	for _, r := range ingMock.Routes {
		routes[r.GetHostname()] = r.GetBackend()
	}

	// The four HTTP views are fronted; SMB is not, because it is not HTTP and a
	// vhost for it would accept a TLS handshake and then fail to speak the
	// protocol.
	if _, ok := routes["s3.gfeh.home"]; !ok {
		t.Errorf("no ingress route for s3.gfeh.home; have %v", routes)
	}
	if _, ok := routes["http.gfeh.home"]; !ok {
		t.Errorf("no ingress route for http.gfeh.home; have %v", routes)
	}
	if _, ok := routes["smb.gfeh.home"]; ok {
		t.Error("smb got an ingress vhost; it does not speak HTTP")
	}

	// The backend is the partition's container on the shared network, which is
	// what makes the fixed in-container ports safe.
	if backend := routes["s3.gfeh.home"]; !strings.HasPrefix(backend, "town-os-system--gfeh-home:") {
		t.Errorf("s3 backend = %q, want the partition's container", backend)
	}

	// --- TLS ---
	// One leaf per partition, covering every HTTP view, so four routes share one
	// certificate and one DANE pin.
	leafDir := filepath.Join(btrfsBase, systemcontroller.TLSSubvolume, "leaves", "gfeh", "home", "current")
	if _, err := os.Stat(filepath.Join(leafDir, "cert.pem")); err != nil {
		t.Fatalf("no leaf issued for the partition: %v", err)
	}
}

// TestIntegrationGfehSurvivesTheDriftRepairPass is the subtle one.
//
// ReconcileDNS deletes every A/AAAA in the zone that is not in its desired set.
// Publishing at boot is not enough: a name absent from that set works
// perfectly, then disappears an hour later, which is a far worse failure than
// never working at all.
func TestIntegrationGfehSurvivesTheDriftRepairPass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	btrfsBase := filepath.Join(dir, "btrfs")
	if err := os.MkdirAll(btrfsBase, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	socket := gfeh.SocketPath(btrfsBase, "home")
	startFakeGfehd(t, socket, namesFor("home", ""))

	reg := gfehTestRegistry{clients: map[string]gfeh.Client{"home": gfeh.NewClient(socket)}}
	rolMock := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	cfg := systemcontroller.ReconcileDNSConfig{
		Client:        rolMock,
		SettingsMgr:   settings,
		InternalIP:    "192.168.10.50",
		BtrfsBasePath: btrfsBase,
		Gfeh:          reg,
	}

	ctx := context.Background()
	if err := systemcontroller.RebuildDNS(ctx, cfg); err != nil {
		t.Fatalf("RebuildDNS: %v", err)
	}
	if !hasRolodexRecord(rolMock, upstream.RecordTypeA, "s3.gfeh.home.") {
		t.Fatal("precondition: the record was never published")
	}

	// The hourly pass.
	if err := systemcontroller.ReconcileDNS(ctx, cfg); err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}

	if !hasRolodexRecord(rolMock, upstream.RecordTypeA, "s3.gfeh.home.") {
		t.Error("the drift-repair pass deleted the partition's record as an orphan")
	}
}

// TestIntegrationGfehPartitionRoutesProvisionAndList drives the four contract
// routes against real btrfs-mock storage, in the order gfehd's own provisioning
// uses them.
func TestIntegrationGfehPartitionRoutesProvisionAndList(t *testing.T) {
	t.Parallel()

	st := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: st})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Create, then create again: gfehd's provisioning is create-or-resize and
	// distinguishes the two by the conflict.
	created, err := c.CreateGfehPartition(ctx, "photos", 1<<30)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Name != "gfeh/photos" {
		t.Errorf("name = %q, want gfeh/photos", created.Name)
	}
	if _, err := c.CreateGfehPartition(ctx, "photos", 1<<30); err == nil {
		t.Error("a duplicate create succeeded; gfehd could not tell new from existing")
	}

	// Resize.
	if _, err := c.ModifyGfehPartition(ctx, "photos", 2<<30); err != nil {
		t.Fatalf("modify: %v", err)
	}

	listed, err := c.ListGfehPartitions(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "gfeh/photos" || listed[0].Quota != 2<<30 {
		t.Fatalf("listed = %+v", listed)
	}

	// The subvolume is owned by the uid gfehd runs as; a bind mount passes host
	// ownership straight through, so root ownership is a daemon that cannot
	// write.
	controller, ok := st.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected *storage.MockBtrFSController")
	}
	owner, found := controller.GetOwners()[filepath.Join(st.BasePath, "gfeh/photos")]
	if !found {
		t.Fatalf("the partition was not chowned; owners = %v", controller.GetOwners())
	}
	if owner.UID != gfeh.UID || owner.GID != gfeh.GID {
		t.Errorf("owner = %d:%d, want %d:%d", owner.UID, owner.GID, gfeh.UID, gfeh.GID)
	}

	if err := c.RemoveGfehPartition(ctx, "photos"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	listed, err = c.ListGfehPartitions(ctx)
	if err != nil {
		t.Fatalf("list after remove: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("partitions after remove = %+v", listed)
	}
}

// TestIntegrationGfehPartitionsAreNotReachableThroughStorage is the reason the
// contract routes exist at all: /storage/create rewrites every submitted name
// to user/<name>, so it can never produce a partition, and the reserved prefix
// stops it from trying.
func TestIntegrationGfehPartitionsAreNotReachableThroughStorage(t *testing.T) {
	t.Parallel()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: storage.InitBtrFSMock()})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"gfeh", "gfeh/photos"} {
		if err := c.CreateFilesystem(context.Background(), storage.Filesystem{Name: name}); err == nil {
			t.Errorf("/storage/create accepted the reserved name %q", name)
		}
	}
}

// TestIntegrationGfehConfigRendersWhatGfehdAccepts. gfehd parses with
// deny_unknown_fields, so a key it does not know is a hard startup failure —
// and the container would crash-loop with a serde message rather than say
// anything useful.
func TestIntegrationGfehConfigRendersWhatGfehdAccepts(t *testing.T) {
	t.Parallel()

	// Rendered through the manager production uses, not from a hand-built
	// Config: the point is what Town OS actually writes to disk. A literal
	// assembled here could assert a shape reconcile never produces and pass
	// while every real partition failed to start.
	m := gfehctl.NewManager(gfehctl.Config{
		Systemd:       systemd.InitMockManager(),
		Network:       "home",
		BtrfsBasePath: t.TempDir(),
		Image:         "localhost/town-os-gfeh:test",
		// Exactly what reconcileGfehPartition passes: SMB is not served, and
		// no credential table goes with it.
		SMBPort:     0,
		Key:         "test-" + gfeh.ServiceKey("home"),
		NetworkName: "town-os-ingress-test",
	})

	out, err := m.RenderConfig(nil)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("reparse: %v", err)
	}

	// Every gfehd config struct is #[serde(deny_unknown_fields)], so a stray
	// key is not ignored -- it is a hard startup failure.
	allowed := map[string]bool{
		"data_dir": true, "partition": true, "network": true, "admin_socket": true,
		"s3": true, "http": true, "drive": true, "ipfs": true, "smb": true,
		"credentials": true, "town_os": true,
	}
	for key := range parsed {
		if !allowed[key] {
			t.Errorf("rendered key %q, which gfehd would refuse", key)
		}
	}

	// No town_os section, because gfehd no longer authenticates to anything:
	// Town OS creates and sizes the partition's subvolume before the daemon
	// starts, and creates principals from its own side over the admin socket.
	// A section here would mean a credential exists somewhere, and the account
	// it named was an enabled administrator nobody created.
	if _, present := parsed["town_os"]; present {
		t.Errorf("a town_os section was rendered; gfehd needs no credential: %v", parsed["town_os"])
	}

	// No SMB view, and above all no credential table: a Town OS account carries
	// no SMB password, so there is nobody gfehd could authenticate to a share,
	// and an unauthenticated share on the LAN is not the fallback to take.
	if _, present := parsed["smb"]; present {
		t.Errorf("an SMB view was rendered: %v", parsed["smb"])
	}
	if strings.Contains(string(out), "nt_hash") {
		t.Errorf("an NT hash reached the rendered config:\n%s", out)
	}

	// The four views that ARE served, so "nothing is configured" cannot pass
	// this test by rendering an empty file.
	for _, view := range []string{"s3", "http", "drive", "ipfs"} {
		if _, present := parsed[view]; !present {
			t.Errorf("the %s view was not rendered; the partition would serve nothing", view)
		}
	}
}
