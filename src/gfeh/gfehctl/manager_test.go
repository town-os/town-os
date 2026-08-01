// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfehctl

import (
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/systemd"
	"go.yaml.in/yaml/v4"
)

// execStartArgs returns the ExecStart line's arguments as one space-separated
// string.
//
// GenerateSystemServiceUnit emits each podman argument on its own
// backslash-continued line, so "-p 4451:4451" never appears contiguously in the
// rendered unit and a naive Contains assertion passes or fails for the wrong
// reason -- "-p " matches the mkdir in ExecStartPre, for instance. Normalising
// first means the assertions below say what they mean.
func execStartArgs(t *testing.T, content string) string {
	t.Helper()

	var args []string
	inExecStart := false
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "ExecStart="):
			inExecStart = true
			trimmed = strings.TrimPrefix(trimmed, "ExecStart=")
		case !inExecStart:
			continue
		}
		continued := strings.HasSuffix(trimmed, "\\")
		args = append(args, strings.TrimSpace(strings.TrimSuffix(trimmed, "\\")))
		if !continued {
			break
		}
	}
	return strings.Join(args, " ")
}

func testManager(t *testing.T, network string) *Manager {
	t.Helper()
	return NewManager(Config{
		Systemd:       systemd.InitMockManager(),
		Network:       network,
		BtrfsBasePath: t.TempDir(),
		Image:         "localhost/town-os-gfeh:test",
	})
}

// TestUnitMountsThePartitionWhereGfehdLooksForIt. gfehd resolves its partition
// as data_dir/partition, so the subvolume has to appear at /data/<network> and
// not at /data. Mounting the object-storage root instead would work — and would
// also give every partition's daemon a view of every other partition's bytes.
func TestUnitMountsThePartitionWhereGfehdLooksForIt(t *testing.T) {
	m := testManager(t, "office")
	_, content := m.UnitContent()

	want := gfeh.PartitionDir(m.cfg.BtrfsBasePath, "office") + ":/data/office:rw"
	if !strings.Contains(content, want) {
		t.Errorf("unit does not mount the partition at /data/office:\n%s", content)
	}
	// The root itself must never be mounted.
	if strings.Contains(content, m.cfg.BtrfsBasePath+"/gfeh:/data") {
		t.Error("the unit mounts the whole object-storage root into one partition's container")
	}
}

// TestUnitMountsTheConfigReadOnly: gfehd must not rewrite a file reconcile
// derives on every boot.
func TestUnitMountsTheConfigReadOnly(t *testing.T) {
	m := testManager(t, "home")
	_, content := m.UnitContent()

	want := gfeh.ConfigDir(m.cfg.BtrfsBasePath, "home") + ":" + gfeh.ContainerConfigDir + ":ro"
	if !strings.Contains(content, want) {
		t.Errorf("the config is not mounted read-only:\n%s", content)
	}
}

// TestUnitPublishesNoHostPortForTheHTTPViews is the property that makes the
// fixed container-side ports safe, and the IRON RULE answer for this service:
// nothing is bound on the host, so ten partitions and two concurrent test runs
// cannot collide.
func TestUnitPublishesNoHostPortForTheHTTPViews(t *testing.T) {
	m := testManager(t, "home")
	_, content := m.UnitContent()

	args := execStartArgs(t, content)
	if strings.Contains(args, " -p ") {
		t.Errorf("a partition with no SMB port published a host port:\n%s", args)
	}
	if !strings.Contains(args, "--net "+systemd.IngressNetworkName) {
		t.Errorf("the partition does not join the ingress network:\n%s", args)
	}
}

// TestUnitPublishesSMBIdentically: SMB cannot sit behind an HTTP router, so it
// is the one view with a real host port. Published identically inside and out,
// so /v1/names reports a port a client can actually dial.
func TestUnitPublishesSMBIdentically(t *testing.T) {
	m := NewManager(Config{
		Systemd:       systemd.InitMockManager(),
		Network:       "office",
		BtrfsBasePath: t.TempDir(),
		Image:         "img",
		SMBPort:       4451,
	})
	_, content := m.UnitContent()

	if args := execStartArgs(t, content); !strings.Contains(args, "-p 4451:4451") {
		t.Errorf("SMB is not published identically:\n%s", args)
	}
}

// TestUnitChownsTheSocketDirectory. A bind mount passes host ownership through,
// and gfehd runs unprivileged, so without this the daemon cannot create its own
// admin socket — and the systemcontroller then has nothing to dial.
func TestUnitChownsTheSocketDirectory(t *testing.T) {
	m := testManager(t, "home")
	_, content := m.UnitContent()

	runDir := gfeh.RunDir(m.cfg.BtrfsBasePath, "home")
	if !strings.Contains(content, "/bin/chown 2000:2000 "+runDir) {
		t.Errorf("the socket directory is not handed to gfeh's uid:\n%s", content)
	}
	// Non-recursive, like every other ownership hand-off in the tree: the
	// daemon creates its own children as itself.
	if strings.Contains(content, "chown -R") {
		t.Error("the chown is recursive; it must not be")
	}
}

// TestUnitRunsAsTheUnprivilegedUser pins the uid to the one the partition
// subvolume is chowned to. If the two drift, the daemon starts and fails on its
// first write.
func TestUnitRunsAsTheUnprivilegedUser(t *testing.T) {
	m := testManager(t, "home")
	_, content := m.UnitContent()

	if args := execStartArgs(t, content); !strings.Contains(args, "--user 2000:2000") {
		t.Errorf("the container does not run as gfeh's uid:\n%s", args)
	}
}

// TestServiceKeyRoundTrips: the teardown pass has only a unit name to work
// from when deciding whether a running daemon still matches a live network.
func TestServiceKeyRoundTrips(t *testing.T) {
	for _, network := range []string{"home", "office", "a", "with-dashes"} {
		key := gfeh.ServiceKey(network)
		got, ok := gfeh.NetworkFromKey(key)
		if !ok || got != network {
			t.Errorf("ServiceKey(%q) -> %q -> (%q, %v)", network, key, got, ok)
		}
		unit := systemd.SystemServiceUnitName(key)
		if !strings.HasPrefix(unit, "town-os-system--gfeh-") {
			t.Errorf("unit name %q does not carry the gfeh prefix", unit)
		}
	}
}

func TestNetworkFromKeyRejectsNonGfehKeys(t *testing.T) {
	for _, key := range []string{"ingress", "ui", "prometheus", "gfeh-", "gfeh"} {
		if _, ok := gfeh.NetworkFromKey(key); ok {
			t.Errorf("NetworkFromKey(%q) claimed a network", key)
		}
	}
}

// TestRenderedConfigOmitsTheNetworkForHome. Absent means "the default" to
// gfeh; an empty string would ask it to publish under a zone called "".
func TestRenderedConfigOmitsTheNetworkForHome(t *testing.T) {
	m := testManager(t, "home")
	out, err := m.RenderConfig(nil)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if strings.Contains(string(out), "network:") {
		t.Errorf("the default network was named explicitly:\n%s", out)
	}
}

func TestRenderedConfigNamesANonDefaultNetwork(t *testing.T) {
	m := testManager(t, "office")
	out, err := m.RenderConfig(nil)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}

	var parsed struct {
		Network   string `yaml:"network"`
		Partition string `yaml:"partition"`
		DataDir   string `yaml:"data_dir"`
	}
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Network != "office" || parsed.Partition != "office" {
		t.Errorf("network=%q partition=%q, want both office", parsed.Network, parsed.Partition)
	}
	if parsed.DataDir != gfeh.ContainerDataDir {
		t.Errorf("data_dir = %q, want %q", parsed.DataDir, gfeh.ContainerDataDir)
	}
}

// TestRenderedLabelsAreThreeLabelNames. <view>.gfeh.<tld> rather than gfeh's
// derived <view>.<partition>, which for the one-partition-per-network model
// would produce s3.home and then s3.home.home once the zone is appended — and
// would collide with the page namespace, which is <domain>.<tld>.
func TestRenderedLabelsAreThreeLabelNames(t *testing.T) {
	m := testManager(t, "office")
	out, err := m.RenderConfig(nil)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	for _, want := range []string{"s3.gfeh", "http.gfeh", "drive.gfeh", "ipfs.gfeh"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing label %q:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "s3.office") {
		t.Error("a label was derived from the partition name")
	}
}

// TestRenderedConfigOmitsSMBWhenNoPortIsAssigned. A view with no bind address
// is a view that is not served, and that absence is how it stays off.
func TestRenderedConfigOmitsSMBWhenNoPortIsAssigned(t *testing.T) {
	m := testManager(t, "home")
	out, err := m.RenderConfig(nil)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if strings.Contains(string(out), "smb:") {
		t.Errorf("SMB was configured with no port assigned:\n%s", out)
	}
}

func TestRenderedConfigCarriesTheSMBCredentialTable(t *testing.T) {
	m := NewManager(Config{
		Systemd:       systemd.InitMockManager(),
		Network:       "home",
		BtrfsBasePath: t.TempDir(),
		Image:         "img",
		SMBPort:       4450,
	})
	out, err := m.RenderConfig([]gfeh.SmbUserConfig{
		{Username: "alice", NTHash: "a4f49c406510bdcab6824ee7c30fd852", Principal: "alice"},
	})
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}

	var parsed struct {
		SMB struct {
			Bind  string `yaml:"bind"`
			Users []struct {
				Username  string `yaml:"username"`
				NTHash    string `yaml:"nt_hash"`
				Principal string `yaml:"principal"`
			} `yaml:"users"`
		} `yaml:"smb"`
	}
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.SMB.Bind != "0.0.0.0:4450" {
		t.Errorf("smb bind = %q", parsed.SMB.Bind)
	}
	if len(parsed.SMB.Users) != 1 || parsed.SMB.Users[0].Username != "alice" {
		t.Fatalf("smb users = %+v", parsed.SMB.Users)
	}
	if parsed.SMB.Users[0].NTHash != "a4f49c406510bdcab6824ee7c30fd852" {
		t.Errorf("nt_hash was not carried through")
	}
}

// TestRenderConfigRejectsABadNTHash: caught here so a mistyped credential is a
// reconcile error naming the account, rather than a container that exits with
// a serde message or a daemon that refuses one specific person's password.
func TestRenderConfigRejectsABadNTHash(t *testing.T) {
	m := NewManager(Config{
		Systemd:       systemd.InitMockManager(),
		Network:       "home",
		BtrfsBasePath: t.TempDir(),
		Image:         "img",
		SMBPort:       4450,
	})
	for _, hash := range []string{"", "abcd", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", "a4f49c406510bdcab6824ee7c30fd85200"} {
		_, err := m.RenderConfig([]gfeh.SmbUserConfig{{Username: "alice", NTHash: hash, Principal: "alice"}})
		if err == nil {
			t.Errorf("rendered a config with nt_hash %q", hash)
		}
	}
}

// TestSMBPortsAreDistinctPerNetwork and never privileged: gfehd runs
// unprivileged and 445 is singular, so a base reaching down there would produce
// units that start and immediately die.
func TestSMBPortsAreDistinctPerNetwork(t *testing.T) {
	seen := map[int]bool{}
	for i := range 8 {
		port := SMBPortFor(0, i)
		if seen[port] {
			t.Fatalf("port %d assigned twice", port)
		}
		seen[port] = true
		if !ValidSMBPort(port) {
			t.Errorf("port %d is not usable", port)
		}
		if port == 445 {
			t.Error("445 was assigned; it is privileged and singular")
		}
	}
}

func TestValidSMBPortRejectsThePrivilegedRange(t *testing.T) {
	for _, port := range []int{0, -1, 22, 445, 1024, 65536} {
		if ValidSMBPort(port) {
			t.Errorf("ValidSMBPort(%d) = true", port)
		}
	}
	if !ValidSMBPort(4450) {
		t.Error("ValidSMBPort(4450) = false")
	}
}

// TestSystemServicesIsPopulated: a service missing from this list is never
// re-pulled or restarted by /system-services/refresh, which is exactly how the
// ingress went a release stuck on its first image.
func TestSystemServicesIsPopulated(t *testing.T) {
	m := testManager(t, "office")
	services := m.SystemServices()
	if len(services) != 1 {
		t.Fatalf("got %d services, want 1", len(services))
	}
	s := services[0]
	if s.Key != "gfeh-office" {
		t.Errorf("key = %q", s.Key)
	}
	if s.UnitName != "town-os-system--gfeh-office.service" {
		t.Errorf("unit = %q", s.UnitName)
	}
	if s.Image != "localhost/town-os-gfeh:test" {
		t.Errorf("image = %q", s.Image)
	}
	if !strings.Contains(s.DisplayName, "office") {
		t.Errorf("display name %q does not name the partition", s.DisplayName)
	}
}

// TestSocketPathIsSharedBetweenContainers. The systemcontroller dials the host
// path while gfehd binds the container path; both have to name the same inode
// or nothing can talk to a partition.
func TestSocketPathIsSharedBetweenContainers(t *testing.T) {
	base := t.TempDir()
	m := NewManager(Config{Systemd: systemd.InitMockManager(), Network: "home", BtrfsBasePath: base, Image: "img"})

	host := m.SocketPath()
	if !strings.HasPrefix(host, base) {
		t.Errorf("socket %q is not on the shared btrfs", host)
	}
	_, content := m.UnitContent()
	if !strings.Contains(content, gfeh.RunDir(base, "home")+":"+gfeh.ContainerRunDir+":rw") {
		t.Errorf("the socket directory is not bind-mounted read-write:\n%s", content)
	}
	if !strings.HasSuffix(gfeh.ContainerSocketPath, "/"+gfeh.AdminSocketName) {
		t.Errorf("container socket path %q", gfeh.ContainerSocketPath)
	}
}

// TestControlStateLivesOutsideTheObjectStorageRoot. Anything under gfeh/ is a
// partition's data; a config file or socket there would be an object somebody
// could list, fetch, or overwrite through S3.
func TestControlStateLivesOutsideTheObjectStorageRoot(t *testing.T) {
	base := t.TempDir()
	root := base + "/" + gfeh.VolumePrefix + "/"

	for _, path := range []string{
		gfeh.ConfigPath(base, "home"),
		gfeh.SocketPath(base, "home"),
		gfeh.ControlDir(base, "home"),
	} {
		if strings.HasPrefix(path, root) {
			t.Errorf("%q is inside the object-storage root", path)
		}
	}
	if !strings.HasPrefix(gfeh.PartitionDir(base, "home"), root) {
		t.Error("the partition is not inside the object-storage root")
	}
}
