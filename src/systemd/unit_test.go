package systemd

import (
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

func TestGeneratePackageUnitsBasic(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "nginx",
		Version:                  "1.0",
		Image:                    "docker.io/library/nginx:1.0",
		Environment:              map[string]string{"NGINX_HOST": "example.com"},
		External:                 packages.PortMap{8080: 80},
		Internal:                 packages.PortMap{},
		Volumes:                  map[string]packages.PackageVolume{"data": {Mountpoint: "/var/data"}},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	// Verify service unit.
	if units.Service.Name != "town-os-package--test-repo-nginx-1.0.service" {
		t.Fatalf("expected service name town-os-package--test-repo-nginx-1.0.service, got %s", units.Service.Name)
	}

	svc := units.Service.Content
	if !strings.Contains(svc, "Description=Town OS Package Service: test-repo/nginx@1.0") {
		t.Fatal("service missing description")
	}
	if !strings.Contains(svc, "-p 8080:80") {
		t.Fatal("service missing -p 8080:80")
	}
	if !strings.Contains(svc, "-e NGINX_HOST=example.com") {
		t.Fatal("service missing environment variable")
	}
	if !strings.Contains(svc, "-v /data/btrfs/installed/test-repo/nginx/1.0/data:/var/data:rw,z") {
		t.Fatalf("service missing volume mount, got:\n%s", svc)
	}
	if !strings.Contains(svc, "systemctl stop town-os-package--test-repo-nginx-1.0-8080-tcp.socket") {
		t.Fatal("service missing socket stop in ExecStartPre")
	}
	if !strings.Contains(svc, "systemctl start town-os-package--test-repo-nginx-1.0-8080-tcp.socket") {
		t.Fatal("service missing socket start in ExecStopPost")
	}
	if !strings.Contains(svc, "firewall-cmd --add-port=8080/tcp") {
		t.Fatal("service missing firewall add-port")
	}
	if !strings.Contains(svc, "firewall-cmd --remove-port=8080/tcp") {
		t.Fatal("service missing firewall remove-port")
	}
	if !strings.Contains(svc, "Wants="+NetworkControllerUnitName("test-repo", "nginx", "1.0")) {
		t.Fatal("service missing Wants for network controller")
	}

	// Verify socket units.
	if len(units.Sockets) != 1 {
		t.Fatalf("expected 1 socket unit, got %d", len(units.Sockets))
	}
	sock := units.Sockets[0]
	if sock.Name != "town-os-package--test-repo-nginx-1.0-8080-tcp.socket" {
		t.Fatalf("expected socket name town-os-package--test-repo-nginx-1.0-8080-tcp.socket, got %s", sock.Name)
	}
	if !strings.Contains(sock.Content, "ListenStream=8080") {
		t.Fatal("socket missing ListenStream")
	}
	if !strings.Contains(sock.Content, "FreeBind=true") {
		t.Fatal("socket missing FreeBind")
	}
	if !strings.Contains(sock.Content, "PartOf=town-os-package--test-repo-nginx-1.0.service") {
		t.Fatal("socket missing PartOf")
	}

	// Verify network controller unit exists.
	if units.NetworkController == nil {
		t.Fatal("expected network controller unit")
	}
	if units.NetworkController.Name != "town-os-package--test-repo-nginx-1.0-network.service" {
		t.Fatalf("expected network controller name town-os-package--test-repo-nginx-1.0-network.service, got %s", units.NetworkController.Name)
	}
	nc := units.NetworkController.Content
	if !strings.Contains(nc, "BindsTo=town-os-package--test-repo-nginx-1.0.service") {
		t.Fatal("network controller missing BindsTo")
	}
	if !strings.Contains(nc, "After=town-os-package--test-repo-nginx-1.0.service") {
		t.Fatal("network controller missing After")
	}

	// Verify socket units still use PartOf (not BindsTo).
	if strings.Contains(sock.Content, "BindsTo=") {
		t.Fatal("socket should not have BindsTo")
	}
}

func TestGeneratePackageUnitsMultiplePorts(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "myapp",
		Version:                  "2.0",
		Image:                    "myapp:2.0",
		Environment:              map[string]string{},
		External:                 packages.PortMap{8080: 80, 8443: 443},
		Internal:                 packages.PortMap{9090: 9090},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	// 3 sockets (8080, 8443, 9090).
	if len(units.Sockets) != 3 {
		t.Fatalf("expected 3 socket units, got %d", len(units.Sockets))
	}

	svc := units.Service.Content
	if !strings.Contains(svc, "-p 8080:80") {
		t.Fatal("service missing -p 8080:80")
	}
	if !strings.Contains(svc, "-p 8443:443") {
		t.Fatal("service missing -p 8443:443")
	}
	if !strings.Contains(svc, "-p 9090:9090") {
		t.Fatal("service missing -p 9090:9090")
	}
	if !strings.Contains(svc, "--add-port=8080/tcp") {
		t.Fatal("service missing firewall add-port 8080")
	}
	if !strings.Contains(svc, "--add-port=8443/tcp") {
		t.Fatal("service missing firewall add-port 8443")
	}
	if !strings.Contains(svc, "--add-port=9090/tcp") {
		t.Fatal("service missing firewall add-port 9090")
	}

	// Network controller should be generated for external ports.
	if units.NetworkController == nil {
		t.Fatal("expected network controller unit for external ports")
	}
}

func TestGeneratePackageUnitsInternalOnly(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "redis",
		Version:                  "7.0",
		Image:                    "redis:7.0",
		Environment:              map[string]string{},
		External:                 packages.PortMap{},
		Internal:                 packages.PortMap{6379: 6379},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	// Sockets and firewall for internal port.
	if len(units.Sockets) != 1 {
		t.Fatalf("expected 1 socket unit, got %d", len(units.Sockets))
	}
	svc := units.Service.Content
	if !strings.Contains(svc, "-p 6379:6379") {
		t.Fatal("service missing -p 6379:6379")
	}
	if !strings.Contains(svc, "--add-port=6379/tcp") {
		t.Fatal("service missing firewall for internal port")
	}

	// No network controller unit.
	if units.NetworkController != nil {
		t.Fatal("expected no network controller unit for internal-only ports")
	}

	// No Wants line for network controller.
	if strings.Contains(svc, "Wants=") {
		t.Fatal("service should not have Wants with no external ports")
	}
}

func TestGeneratePackageUnitsNoPorts(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "worker",
		Version:                  "1.0",
		Image:                    "worker:1.0",
		Environment:              map[string]string{},
		External:                 packages.PortMap{},
		Internal:                 packages.PortMap{},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	// No sockets.
	if len(units.Sockets) != 0 {
		t.Fatalf("expected 0 socket units, got %d", len(units.Sockets))
	}

	// No network controller.
	if units.NetworkController != nil {
		t.Fatal("expected no network controller")
	}

	// No firewall commands.
	svc := units.Service.Content
	if strings.Contains(svc, "firewall-cmd") {
		t.Fatal("service should not have firewall commands with no ports")
	}
	if strings.Contains(svc, "systemctl stop") {
		t.Fatal("service should not have socket stop with no ports")
	}
	if strings.Contains(svc, "systemctl start") {
		t.Fatal("service should not have socket start with no ports")
	}

	// Should still have the basic service structure.
	if !strings.Contains(svc, "Description=Town OS Package Service: test-repo/worker@1.0") {
		t.Fatal("service missing description")
	}
	if !strings.Contains(svc, "worker:1.0") {
		t.Fatal("service missing image reference")
	}
}

func TestGeneratePackageUnitsEnvironmentSorted(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName: "test-repo",
		PkgName:  "myapp",
		Version:  "1.0",
		Image:    "myapp:1.0",
		Environment: map[string]string{
			"ZEBRA":  "last",
			"ALPHA":  "first",
			"MIDDLE": "mid",
		},
		External:                 packages.PortMap{},
		Internal:                 packages.PortMap{},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	alphaIdx := strings.Index(svc, "-e ALPHA=first")
	middleIdx := strings.Index(svc, "-e MIDDLE=mid")
	zebraIdx := strings.Index(svc, "-e ZEBRA=last")

	if alphaIdx == -1 || middleIdx == -1 || zebraIdx == -1 {
		t.Fatalf("service missing environment variables, got:\n%s", svc)
	}

	if alphaIdx > middleIdx || middleIdx > zebraIdx {
		t.Fatal("environment variables are not sorted alphabetically")
	}
}

func TestPackageUnitNames(t *testing.T) {
	external := packages.PortMap{8080: 80, 8443: 443}
	internal := packages.PortMap{9090: 9090}

	names := PackageUnitNames("test-repo", "nginx", "1.0", "", external, internal)

	expected := []string{
		"town-os-package--test-repo-nginx-1.0.service",
		"town-os-package--test-repo-nginx-1.0-8080-tcp.socket",
		"town-os-package--test-repo-nginx-1.0-8443-tcp.socket",
		"town-os-package--test-repo-nginx-1.0-9090-tcp.socket",
		"town-os-package--test-repo-nginx-1.0-network.service",
	}

	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d: %v", len(expected), len(names), names)
	}

	for i, name := range names {
		if name != expected[i] {
			t.Fatalf("expected names[%d] = %q, got %q", i, expected[i], name)
		}
	}
}

func TestPackageUnitNamesNoExternalPorts(t *testing.T) {
	external := packages.PortMap{}
	internal := packages.PortMap{6379: 6379}

	names := PackageUnitNames("test-repo", "redis", "7.0", "", external, internal)

	expected := []string{
		"town-os-package--test-repo-redis-7.0.service",
		"town-os-package--test-repo-redis-7.0-6379-tcp.socket",
	}

	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d: %v", len(expected), len(names), names)
	}

	for i, name := range names {
		if name != expected[i] {
			t.Fatalf("expected names[%d] = %q, got %q", i, expected[i], name)
		}
	}
}

func TestPackageUnitNamesNoPorts(t *testing.T) {
	names := PackageUnitNames("test-repo", "worker", "1.0", "", packages.PortMap{}, packages.PortMap{})

	if len(names) != 1 {
		t.Fatalf("expected 1 name, got %d: %v", len(names), names)
	}
	if names[0] != "town-os-package--test-repo-worker-1.0.service" {
		t.Fatalf("expected town-os-package--test-repo-worker-1.0.service, got %s", names[0])
	}
}

func TestPackageUnitNamesInternalPortForwardingHostMode(t *testing.T) {
	external := packages.PortMap{}
	internal := packages.PortMap{9999: 3000}

	names := PackageUnitNames("test-repo", "gitea", "1.0", "host", external, internal)

	expected := []string{
		"town-os-package--test-repo-gitea-1.0.service",
		"town-os-package--test-repo-gitea-1.0-9999-tcp.socket",
		"town-os-package--test-repo-gitea-1.0-network.service",
	}

	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d: %v", len(expected), len(names), names)
	}

	for i, name := range names {
		if name != expected[i] {
			t.Fatalf("expected names[%d] = %q, got %q", i, expected[i], name)
		}
	}
}

func TestPackageUnitNamesInternalSamePortHostMode(t *testing.T) {
	external := packages.PortMap{}
	internal := packages.PortMap{6379: 6379}

	names := PackageUnitNames("test-repo", "redis", "7.0", "host", external, internal)

	// Same port: no network controller needed.
	expected := []string{
		"town-os-package--test-repo-redis-7.0.service",
		"town-os-package--test-repo-redis-7.0-6379-tcp.socket",
	}

	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d: %v", len(expected), len(names), names)
	}

	for i, name := range names {
		if name != expected[i] {
			t.Fatalf("expected names[%d] = %q, got %q", i, expected[i], name)
		}
	}
}

func TestSocketUnitName(t *testing.T) {
	name := SocketUnitName("test-repo", "nginx", "1.0", 8080)
	if name != "town-os-package--test-repo-nginx-1.0-8080-tcp.socket" {
		t.Fatalf("expected town-os-package--test-repo-nginx-1.0-8080-tcp.socket, got %s", name)
	}
}

func TestNetworkControllerUnitName(t *testing.T) {
	name := NetworkControllerUnitName("test-repo", "nginx", "1.0")
	if name != "town-os-package--test-repo-nginx-1.0-network.service" {
		t.Fatalf("expected town-os-package--test-repo-nginx-1.0-network.service, got %s", name)
	}
}

func TestGeneratePackageUnitsNetworkModeHost(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "redis",
		Version:                  "7.0",
		Image:                    "redis:7.0",
		Environment:              map[string]string{},
		External:                 packages.PortMap{},
		Internal:                 packages.PortMap{6379: 6379},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
		NetworkMode:              "host",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "--net host") {
		t.Fatal("service missing --net host")
	}
	if strings.Contains(svc, "-p 6379:6379") {
		t.Fatal("service should not have -p mappings in host network mode")
	}
}

func TestGeneratePackageUnitsCommand(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "redis",
		Version:                  "7.0",
		Image:                    "redis:7.0-alpine",
		Command:                  []string{"redis-server", "--bind", "0.0.0.0"},
		Environment:              map[string]string{},
		External:                 packages.PortMap{},
		Internal:                 packages.PortMap{6379: 6379},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "redis:7.0-alpine") {
		t.Fatal("service missing image reference")
	}
	if !strings.Contains(svc, "redis-server") {
		t.Fatal("service missing command arg: redis-server")
	}
	if !strings.Contains(svc, "--bind") {
		t.Fatal("service missing command arg: --bind")
	}
	if !strings.Contains(svc, "0.0.0.0") {
		t.Fatal("service missing command arg: 0.0.0.0")
	}

	// Command args should appear after the image name.
	imageIdx := strings.Index(svc, "redis:7.0-alpine")
	cmdIdx := strings.Index(svc, "redis-server")
	if cmdIdx < imageIdx {
		t.Fatal("command args should appear after image name")
	}
}

func TestGeneratePackageUnitsCommandWithHostNetwork(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "redis",
		Version:                  "7.0",
		Image:                    "redis:7.0-alpine",
		Command:                  []string{"redis-server", "--bind", "0.0.0.0"},
		Environment:              map[string]string{},
		External:                 packages.PortMap{},
		Internal:                 packages.PortMap{6379: 6379},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
		NetworkMode:              "host",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "--net host") {
		t.Fatal("service missing --net host")
	}
	if strings.Contains(svc, "-p 6379:6379") {
		t.Fatal("service should not have -p in host mode")
	}
	if !strings.Contains(svc, "redis-server") {
		t.Fatal("service missing command arg: redis-server")
	}
	if !strings.Contains(svc, "0.0.0.0") {
		t.Fatal("service missing command arg: 0.0.0.0")
	}
}

func TestGeneratePackageUnitsVolumeFormat(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:    "test-repo",
		PkgName:     "myapp",
		Version:     "1.0",
		Image:       "myapp:1.0",
		Environment: map[string]string{},
		External:    packages.PortMap{},
		Internal:    packages.PortMap{},
		Volumes: map[string]packages.PackageVolume{
			"data":   {Mountpoint: "/var/lib/data"},
			"config": {Mountpoint: "/etc/myapp"},
		},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	// Volumes should be sorted by name (config before data).
	configIdx := strings.Index(svc, "/data/btrfs/installed/test-repo/myapp/1.0/config:/etc/myapp:rw,z")
	dataIdx := strings.Index(svc, "/data/btrfs/installed/test-repo/myapp/1.0/data:/var/lib/data:rw,z")

	if configIdx == -1 {
		t.Fatalf("service missing config volume, got:\n%s", svc)
	}
	if dataIdx == -1 {
		t.Fatalf("service missing data volume, got:\n%s", svc)
	}

	if configIdx > dataIdx {
		t.Fatal("volumes should be sorted alphabetically (config before data)")
	}
}

func TestGeneratePackageUnitsNetworkControllerContent(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "nginx",
		Version:                  "1.0",
		Image:                    "nginx:1.26-alpine",
		Environment:              map[string]string{},
		External:                 packages.PortMap{8080: 80},
		Internal:                 packages.PortMap{},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	if units.NetworkController == nil {
		t.Fatal("expected network controller unit")
	}
	nc := units.NetworkController.Content
	if !strings.Contains(nc, "BindsTo=town-os-package--test-repo-nginx-1.0.service") {
		t.Fatalf("network controller missing BindsTo, got:\n%s", nc)
	}
	if !strings.Contains(nc, "After=town-os-package--test-repo-nginx-1.0.service") {
		t.Fatalf("network controller missing After, got:\n%s", nc)
	}
	if !strings.Contains(nc, "ExecStart=/town-os-networkcontroller --state /var/run/town-os/test-repo-nginx-1.0.json") {
		t.Fatalf("network controller missing ExecStart with correct state path, got:\n%s", nc)
	}
	if !strings.Contains(nc, "Description=Town OS Network Controller: test-repo/nginx@1.0") {
		t.Fatalf("network controller missing description, got:\n%s", nc)
	}
}

func TestGeneratePackageUnitsNetworkControllerHostMode(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "nginx",
		Version:                  "1.0",
		Image:                    "nginx:1.26-alpine",
		Environment:              map[string]string{},
		External:                 packages.PortMap{8080: 80},
		Internal:                 packages.PortMap{},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
		NetworkMode:              "host",
	}

	units := GeneratePackageUnits(cfg)

	// Network controller should still be generated in host mode.
	if units.NetworkController == nil {
		t.Fatal("expected network controller unit in host mode")
	}
	nc := units.NetworkController.Content
	if !strings.Contains(nc, "BindsTo=town-os-package--test-repo-nginx-1.0.service") {
		t.Fatalf("network controller missing BindsTo in host mode, got:\n%s", nc)
	}

	// Service should have --net host.
	svc := units.Service.Content
	if !strings.Contains(svc, "--net host") {
		t.Fatal("service missing --net host")
	}
	if strings.Contains(svc, "-p 8080:80") {
		t.Fatal("service should not have -p mappings in host mode")
	}
}

func TestGeneratePackageUnitsInternalPortForwardingHostMode(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "gitea",
		Version:                  "1.0",
		Image:                    "gitea:1.0",
		Environment:              map[string]string{},
		External:                 packages.PortMap{},
		Internal:                 packages.PortMap{9999: 3000},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
		NetworkMode:              "host",
	}

	units := GeneratePackageUnits(cfg)

	// Network controller should be generated for internal port forwarding in host mode.
	if units.NetworkController == nil {
		t.Fatal("expected network controller unit for internal port forwarding in host mode")
	}
	nc := units.NetworkController.Content
	if !strings.Contains(nc, "BindsTo=town-os-package--test-repo-gitea-1.0.service") {
		t.Fatalf("network controller missing BindsTo, got:\n%s", nc)
	}

	// Service should have Wants= for network controller.
	svc := units.Service.Content
	if !strings.Contains(svc, "Wants="+NetworkControllerUnitName("test-repo", "gitea", "1.0")) {
		t.Fatalf("service missing Wants for network controller, got:\n%s", svc)
	}

	// Should have --net host.
	if !strings.Contains(svc, "--net host") {
		t.Fatal("service missing --net host")
	}
}

func TestGeneratePackageUnitsInternalSamePortHostMode(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "redis",
		Version:                  "7.0",
		Image:                    "redis:7.0",
		Environment:              map[string]string{},
		External:                 packages.PortMap{},
		Internal:                 packages.PortMap{6379: 6379},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
		NetworkMode:              "host",
	}

	units := GeneratePackageUnits(cfg)

	// No network controller needed when host == container.
	if units.NetworkController != nil {
		t.Fatal("expected no network controller unit for same-port internal mapping in host mode")
	}

	// No Wants line.
	svc := units.Service.Content
	if strings.Contains(svc, "Wants=") {
		t.Fatal("service should not have Wants with same-port internal mapping")
	}
}

func TestGeneratePackageUnitsInternalPortForwardingBridgeMode(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:                 "test-repo",
		PkgName:                  "gitea",
		Version:                  "1.0",
		Image:                    "gitea:1.0",
		Environment:              map[string]string{},
		External:                 packages.PortMap{},
		Internal:                 packages.PortMap{9999: 3000},
		Volumes:                  map[string]packages.PackageVolume{},
		BtrfsBase:                "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         "/var/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	// No network controller in bridge mode (podman uses -p).
	if units.NetworkController != nil {
		t.Fatal("expected no network controller unit for internal port forwarding in bridge mode")
	}

	// Should have -p mapping instead.
	svc := units.Service.Content
	if !strings.Contains(svc, "-p 9999:3000") {
		t.Fatalf("service missing -p 9999:3000 in bridge mode, got:\n%s", svc)
	}
}

func TestGeneratePackageUnitsVolumeChown(t *testing.T) {
	uid := uint32(1000)
	gid := uint32(1000)

	t.Run("chown when UID/GID set", func(t *testing.T) {
		cfg := PackageUnitConfig{
			RepoName:    "test-repo",
			PkgName:     "mattermost",
			Version:     "1.0",
			Image:       "mattermost:1.0",
			Environment: map[string]string{},
			External:    packages.PortMap{},
			Internal:    packages.PortMap{},
			Volumes: map[string]packages.PackageVolume{
				"data": {Mountpoint: "/data", UID: &uid, GID: &gid},
			},
			BtrfsBase:                "/data/btrfs",
			NetworkControllerBinPath: "/town-os-networkcontroller",
		}

		units := GeneratePackageUnits(cfg)
		svc := units.Service.Content

		if !strings.Contains(svc, "ExecStartPre=/bin/chown -R 1000:1000 /data/btrfs/installed/test-repo/mattermost/1.0/data") {
			t.Fatalf("service missing chown ExecStartPre, got:\n%s", svc)
		}
	})

	t.Run("no chown when UID/GID nil", func(t *testing.T) {
		cfg := PackageUnitConfig{
			RepoName:    "test-repo",
			PkgName:     "nginx",
			Version:     "1.0",
			Image:       "nginx:1.0",
			Environment: map[string]string{},
			External:    packages.PortMap{},
			Internal:    packages.PortMap{},
			Volumes: map[string]packages.PackageVolume{
				"data": {Mountpoint: "/data"},
			},
			BtrfsBase:                "/data/btrfs",
			NetworkControllerBinPath: "/town-os-networkcontroller",
		}

		units := GeneratePackageUnits(cfg)
		svc := units.Service.Content

		if strings.Contains(svc, "chown") {
			t.Fatalf("service should not have chown when UID/GID is nil, got:\n%s", svc)
		}
	})
}

func TestIsPackageServiceUnit(t *testing.T) {
	tests := []struct {
		name   string
		expect bool
	}{
		{"town-os-package--test-repo-nginx-1.0.service", true},
		{"town-os-package--core-redis-7.0.service", true},
		{"town-os-package--test-repo-nginx-1.0-8080-tcp.socket", false},
		{"town-os-package--test-repo-nginx-1.0-upnp.service", false},
		{"town-os-package--test-repo-nginx-1.0-upnp.timer", false},
		{"town-os-package--test-repo-nginx-1.0-fwd-8080-tcp.service", false},
		{"town-os-package--test-repo-nginx-1.0-network.service", false},
		{"sshd.service", false},
		{"town-os-systemcontroller.service", false},
		{"", false},
		// Degenerate: bare prefix with no repo/name/version still matches
		// structurally, but listUnits filters it via the installed-package check.
		{"town-os-package--.service", true},
		// Additional degenerate variations that pass structural check.
		{"town-os-package--x.service", true},
		{"town-os-package----.service", true},
		// Degenerate network controller unit is correctly excluded.
		{"town-os-package---network.service", false},
	}

	for _, tt := range tests {
		got := IsPackageServiceUnit(tt.name)
		if got != tt.expect {
			t.Errorf("IsPackageServiceUnit(%q) = %v, want %v", tt.name, got, tt.expect)
		}
	}
}

func TestGenerateServiceUnitWithDescription(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:    "test-repo",
		PkgName:     "nginx",
		Version:     "1.0",
		Description: "High-performance web server",
		Image:       "nginx:1.0",
		Environment: map[string]string{},
		External:    packages.PortMap{},
		Internal:    packages.PortMap{},
		Volumes:     map[string]packages.PackageVolume{},
		BtrfsBase:   "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "Description=Town OS: High-performance web server") {
		t.Fatalf("expected description with 'Town OS:' prefix, got:\n%s", svc)
	}
	if strings.Contains(svc, "Town OS Package Service:") {
		t.Fatal("should not contain fallback description when Description is set")
	}
}

func TestGenerateServiceUnitWithoutDescription(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:    "test-repo",
		PkgName:     "nginx",
		Version:     "1.0",
		Image:       "nginx:1.0",
		Environment: map[string]string{},
		External:    packages.PortMap{},
		Internal:    packages.PortMap{},
		Volumes:     map[string]packages.PackageVolume{},
		BtrfsBase:   "/data/btrfs",
		NetworkControllerBinPath: "/town-os-networkcontroller",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "Description=Town OS Package Service: test-repo/nginx@1.0") {
		t.Fatalf("expected fallback description, got:\n%s", svc)
	}
}
