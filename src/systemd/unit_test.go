// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

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
		BtrfsBase:                "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:         "/run/town-os",
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
	if !strings.Contains(svc, "--net town-os-net--test-repo-nginx-1.0") {
		t.Fatalf("service missing --net private network, got:\n%s", svc)
	}
	if strings.Contains(svc, "-p 8080:80") {
		t.Fatal("service should not have -p mappings (private network mode)")
	}
	// When NC exists, the service should NOT rm -f (NC owns cleanup) but
	// SHOULD still create the network idempotently (boot race safety net).
	if strings.Contains(svc, "podman network rm -f town-os-net--test-repo-nginx-1.0") {
		t.Fatalf("service should not have network rm -f when NC exists, got:\n%s", svc)
	}
	if !strings.Contains(svc, "podman network create town-os-net--test-repo-nginx-1.0") {
		t.Fatalf("service should always have idempotent network create, got:\n%s", svc)
	}
	if !strings.Contains(svc, "-e NGINX_HOST=example.com") {
		t.Fatal("service missing environment variable")
	}
	if !strings.Contains(svc, "-v /town-os/installed/test-repo/nginx/1.0/data:/var/data:rw,z") {
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
	if !strings.Contains(nc, "PartOf=town-os-package--test-repo-nginx-1.0.service") {
		t.Fatal("network controller missing PartOf")
	}
	if !strings.Contains(nc, "Before=town-os-package--test-repo-nginx-1.0.service") {
		t.Fatal("network controller missing Before")
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
		BtrfsBase:                "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:         "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	// 3 sockets (8080, 8443, 9090).
	if len(units.Sockets) != 3 {
		t.Fatalf("expected 3 socket units, got %d", len(units.Sockets))
	}

	svc := units.Service.Content
	// Private network mode: no -p mappings.
	if strings.Contains(svc, "-p 8080:80") {
		t.Fatal("service should not have -p mappings (private network mode)")
	}
	if !strings.Contains(svc, "--net town-os-net--test-repo-myapp-2.0") {
		t.Fatalf("service missing --net private network, got:\n%s", svc)
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
		BtrfsBase:                "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:         "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	// Sockets and firewall for internal port.
	if len(units.Sockets) != 1 {
		t.Fatalf("expected 1 socket unit, got %d", len(units.Sockets))
	}
	svc := units.Service.Content
	// Private network mode: no -p mappings.
	if strings.Contains(svc, "-p 6379:6379") {
		t.Fatal("service should not have -p mappings (private network mode)")
	}
	if !strings.Contains(svc, "--net town-os-net--test-repo-redis-7.0") {
		t.Fatalf("service missing --net private network, got:\n%s", svc)
	}
	if !strings.Contains(svc, "--add-port=6379/tcp") {
		t.Fatal("service missing firewall for internal port")
	}

	// Network controller should be generated for any ports (NC handles all forwarding).
	if units.NetworkController == nil {
		t.Fatal("expected network controller unit for internal-only ports")
	}

	// Wants line for network controller.
	if !strings.Contains(svc, "Wants=") {
		t.Fatal("service should have Wants for network controller when ports exist")
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
		BtrfsBase:                "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:         "/run/town-os",
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
		BtrfsBase:                "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:         "/run/town-os",
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

	names := PackageUnitNames("test-repo", "nginx", "1.0", external, internal)

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

	names := PackageUnitNames("test-repo", "redis", "7.0", external, internal)

	// NC is always present when there are any ports.
	expected := []string{
		"town-os-package--test-repo-redis-7.0.service",
		"town-os-package--test-repo-redis-7.0-6379-tcp.socket",
		"town-os-package--test-repo-redis-7.0-network.service",
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
	names := PackageUnitNames("test-repo", "worker", "1.0", packages.PortMap{}, packages.PortMap{})

	if len(names) != 1 {
		t.Fatalf("expected 1 name, got %d: %v", len(names), names)
	}
	if names[0] != "town-os-package--test-repo-worker-1.0.service" {
		t.Fatalf("expected town-os-package--test-repo-worker-1.0.service, got %s", names[0])
	}
}

func TestPackageUnitNamesInternalPortForwarding(t *testing.T) {
	external := packages.PortMap{}
	internal := packages.PortMap{9999: 3000}

	names := PackageUnitNames("test-repo", "gitea", "1.0", external, internal)

	// NC is always present when there are any ports.
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

func TestPackageUnitNamesInternalSamePort(t *testing.T) {
	external := packages.PortMap{}
	internal := packages.PortMap{6379: 6379}

	names := PackageUnitNames("test-repo", "redis", "7.0", external, internal)

	// NC is always present when there are any ports (even same-port).
	expected := []string{
		"town-os-package--test-repo-redis-7.0.service",
		"town-os-package--test-repo-redis-7.0-6379-tcp.socket",
		"town-os-package--test-repo-redis-7.0-network.service",
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

func TestGeneratePackageUnitsPrivateNetwork(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "test-repo",
		PkgName:                "redis",
		Version:                "7.0",
		Image:                  "redis:7.0",
		Environment:            map[string]string{},
		External:               packages.PortMap{},
		Internal:               packages.PortMap{6379: 6379},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "--net town-os-net--test-repo-redis-7.0") {
		t.Fatalf("service missing --net private network, got:\n%s", svc)
	}
	if strings.Contains(svc, "-p 6379:6379") {
		t.Fatal("service should not have -p mappings (private network mode)")
	}
	if strings.Contains(svc, "--net host") {
		t.Fatal("service should not have --net host")
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
		BtrfsBase:                "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:         "/run/town-os",
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

func TestGeneratePackageUnitsCommandWithPrivateNetwork(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "test-repo",
		PkgName:                "redis",
		Version:                "7.0",
		Image:                  "redis:7.0-alpine",
		Command:                []string{"redis-server", "--bind", "0.0.0.0"},
		Environment:            map[string]string{},
		External:               packages.PortMap{},
		Internal:               packages.PortMap{6379: 6379},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "--net town-os-net--test-repo-redis-7.0") {
		t.Fatalf("service missing --net private network, got:\n%s", svc)
	}
	if strings.Contains(svc, "-p 6379:6379") {
		t.Fatal("service should not have -p in private network mode")
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
		BtrfsBase:                "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:         "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	// Volumes should be sorted by name (config before data).
	configIdx := strings.Index(svc, "/town-os/installed/test-repo/myapp/1.0/config:/etc/myapp:rw,z")
	dataIdx := strings.Index(svc, "/town-os/installed/test-repo/myapp/1.0/data:/var/lib/data:rw,z")

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
		RepoName:               "test-repo",
		PkgName:                "nginx",
		Version:                "1.0",
		Image:                  "nginx:1.26-alpine",
		Environment:            map[string]string{},
		External:               packages.PortMap{8080: 80},
		Internal:               packages.PortMap{},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	if units.NetworkController == nil {
		t.Fatal("expected network controller unit")
	}
	nc := units.NetworkController.Content
	if !strings.Contains(nc, "PartOf=town-os-package--test-repo-nginx-1.0.service") {
		t.Fatalf("network controller missing PartOf, got:\n%s", nc)
	}
	if !strings.Contains(nc, "Before=town-os-package--test-repo-nginx-1.0.service") {
		t.Fatalf("network controller missing Before, got:\n%s", nc)
	}
	if !strings.Contains(nc, "quay.io/town/networkcontroller:test /town-os-networkcontroller --state /run/town-os/test-repo-nginx-1.0.json") {
		t.Fatalf("network controller missing ExecStart with correct state path, got:\n%s", nc)
	}
	if !strings.Contains(nc, "--target-container town-os-package--test-repo-nginx-1.0") {
		t.Fatalf("network controller missing --target-container, got:\n%s", nc)
	}
	if !strings.Contains(nc, "Description=Town OS Network Controller: test-repo/nginx@1.0") {
		t.Fatalf("network controller missing description, got:\n%s", nc)
	}
	// NC runs as a podman container on the shared network (not --net host).
	if !strings.Contains(nc, "podman run") {
		t.Fatalf("network controller should run as podman container, got:\n%s", nc)
	}
	if !strings.Contains(nc, "--net town-os-net--test-repo-nginx-1.0") {
		t.Fatalf("network controller should use --net <networkName>, got:\n%s", nc)
	}
	if strings.Contains(nc, "--net host") {
		t.Fatalf("network controller should not use --net host, got:\n%s", nc)
	}
	// NC has -p flags for port mappings.
	if !strings.Contains(nc, "-p 8080:8080") {
		t.Fatalf("network controller missing -p port mapping, got:\n%s", nc)
	}
	// NC creates and removes the network.
	if !strings.Contains(nc, "podman network create town-os-net--test-repo-nginx-1.0") {
		t.Fatalf("network controller missing network create in ExecStartPre, got:\n%s", nc)
	}
	if !strings.Contains(nc, "podman network rm -f town-os-net--test-repo-nginx-1.0") {
		t.Fatalf("network controller missing network rm -f in ExecStopPost, got:\n%s", nc)
	}
	// NC must NOT have podman inspect (uses DNS-based container name resolution).
	if strings.Contains(nc, "podman inspect") {
		t.Fatalf("network controller should not have podman inspect, got:\n%s", nc)
	}
	// When TLSDir is unset the NC unit must not bind-mount a tls dir.
	if strings.Contains(nc, ":/etc/town-os/tls:ro") {
		t.Fatalf("network controller should not mount TLS dir when TLSDir is empty, got:\n%s", nc)
	}
}

func TestGeneratePackageUnitsNCMountsTLSDirWhenSet(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "test-repo",
		PkgName:                "nginx",
		Version:                "1.0",
		Image:                  "nginx:1.26-alpine",
		Environment:            map[string]string{},
		External:               packages.PortMap{8080: 80},
		Internal:               packages.PortMap{},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
		TLSDir:                 "/town-os/tls",
	}

	units := GeneratePackageUnits(cfg)
	if units.NetworkController == nil {
		t.Fatal("expected network controller unit")
	}
	nc := units.NetworkController.Content
	if !strings.Contains(nc, "-v /town-os/tls:/etc/town-os/tls:ro") {
		t.Fatalf("network controller missing TLS dir mount, got:\n%s", nc)
	}
}

func TestGeneratePackageUnitsNCAlwaysPresentWithExternalPorts(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "test-repo",
		PkgName:                "nginx",
		Version:                "1.0",
		Image:                  "nginx:1.26-alpine",
		Environment:            map[string]string{},
		External:               packages.PortMap{8080: 80},
		Internal:               packages.PortMap{},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	if units.NetworkController == nil {
		t.Fatal("expected network controller unit")
	}
	nc := units.NetworkController.Content
	if !strings.Contains(nc, "PartOf=town-os-package--test-repo-nginx-1.0.service") {
		t.Fatalf("network controller missing PartOf, got:\n%s", nc)
	}

	// NC unit must use Type=notify with sdnotify=conmon so systemd waits
	// for actual container readiness before starting dependent services.
	if !strings.Contains(nc, "Type=notify") {
		t.Fatalf("NC unit must use Type=notify, got:\n%s", nc)
	}
	if !strings.Contains(nc, "NotifyAccess=all") {
		t.Fatalf("NC unit must have NotifyAccess=all, got:\n%s", nc)
	}
	if !strings.Contains(nc, "--sdnotify=conmon") {
		t.Fatalf("NC unit must use --sdnotify=conmon, got:\n%s", nc)
	}

	// Service should use private network.
	svc := units.Service.Content
	if !strings.Contains(svc, "--net town-os-net--test-repo-nginx-1.0") {
		t.Fatalf("service missing --net private network, got:\n%s", svc)
	}
	if strings.Contains(svc, "-p 8080:80") {
		t.Fatal("service should not have -p mappings")
	}
}

func TestGeneratePackageUnitsInternalPortForwarding(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "test-repo",
		PkgName:                "gitea",
		Version:                "1.0",
		Image:                  "gitea:1.0",
		Environment:            map[string]string{},
		External:               packages.PortMap{},
		Internal:               packages.PortMap{9999: 3000},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	// Network controller should be generated for any ports.
	if units.NetworkController == nil {
		t.Fatal("expected network controller unit for internal port forwarding")
	}
	nc := units.NetworkController.Content
	if !strings.Contains(nc, "PartOf=town-os-package--test-repo-gitea-1.0.service") {
		t.Fatalf("network controller missing PartOf, got:\n%s", nc)
	}

	// Service should have Wants= for network controller.
	svc := units.Service.Content
	if !strings.Contains(svc, "Wants="+NetworkControllerUnitName("test-repo", "gitea", "1.0")) {
		t.Fatalf("service missing Wants for network controller, got:\n%s", svc)
	}

	// Should use private network.
	if !strings.Contains(svc, "--net town-os-net--test-repo-gitea-1.0") {
		t.Fatalf("service missing --net private network, got:\n%s", svc)
	}
	if strings.Contains(svc, "-p 9999:3000") {
		t.Fatal("service should not have -p mappings")
	}
}

func TestGeneratePackageUnitsInternalSamePort(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "test-repo",
		PkgName:                "redis",
		Version:                "7.0",
		Image:                  "redis:7.0",
		Environment:            map[string]string{},
		External:               packages.PortMap{},
		Internal:               packages.PortMap{6379: 6379},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)

	// Network controller is always present when ports exist.
	if units.NetworkController == nil {
		t.Fatal("expected network controller unit for same-port internal mapping")
	}

	// Wants line should be present.
	svc := units.Service.Content
	if !strings.Contains(svc, "Wants=") {
		t.Fatal("service should have Wants for network controller")
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
			BtrfsBase:                "/town-os",
			NetworkControllerImage: "quay.io/town/networkcontroller:test",
		}

		units := GeneratePackageUnits(cfg)
		svc := units.Service.Content

		if !strings.Contains(svc, "ExecStartPre=/bin/chown -R 1000:1000 /town-os/installed/test-repo/mattermost/1.0/data") {
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
			BtrfsBase:                "/town-os",
			NetworkControllerImage: "quay.io/town/networkcontroller:test",
		}

		units := GeneratePackageUnits(cfg)
		svc := units.Service.Content

		if strings.Contains(svc, "chown") {
			t.Fatalf("service should not have chown when UID/GID is nil, got:\n%s", svc)
		}
	})
}

func TestGenerateSystemServiceUnitVolumeDirs(t *testing.T) {
	t.Run("with volume dirs", func(t *testing.T) {
		cfg := SystemServiceUnitConfig{
			Key:         "prometheus",
			Description: "Prometheus",
			Image:       "quay.io/prometheus/prometheus:latest",
			Args:        []string{"--net", "host"},
			VolumeDirs:  []string{"/data/monitoring", "/data/monitoring/prometheus-data"},
		}

		uf := GenerateSystemServiceUnit(cfg)
		content := uf.Content

		if !strings.Contains(content, "ExecStartPre=/bin/mkdir -p /data/monitoring\n") {
			t.Fatalf("missing mkdir for /data/monitoring, got:\n%s", content)
		}
		if !strings.Contains(content, "ExecStartPre=/bin/mkdir -p /data/monitoring/prometheus-data\n") {
			t.Fatalf("missing mkdir for /data/monitoring/prometheus-data, got:\n%s", content)
		}

		// mkdir lines should appear after podman rm and before ExecStart.
		rmIdx := strings.Index(content, "podman rm")
		mkdirIdx := strings.Index(content, "mkdir -p")
		startIdx := strings.Index(content, "ExecStart=/usr/bin/podman run")
		if rmIdx == -1 || mkdirIdx == -1 || startIdx == -1 {
			t.Fatalf("missing expected lines, got:\n%s", content)
		}
		if mkdirIdx < rmIdx {
			t.Fatal("mkdir should appear after podman rm")
		}
		if mkdirIdx > startIdx {
			t.Fatal("mkdir should appear before ExecStart podman run")
		}
	})

	t.Run("without volume dirs", func(t *testing.T) {
		cfg := SystemServiceUnitConfig{
			Key:         "node-exporter",
			Description: "Node Exporter",
			Image:       "quay.io/prometheus/node-exporter:latest",
			Args:        []string{"--net", "host"},
		}

		uf := GenerateSystemServiceUnit(cfg)
		if strings.Contains(uf.Content, "mkdir") {
			t.Fatalf("should not contain mkdir when VolumeDirs is empty, got:\n%s", uf.Content)
		}
	})
}

func TestGenerateSystemServiceUnitExecStartPreAndStopPost(t *testing.T) {
	cfg := SystemServiceUnitConfig{
		Key:         "rolodex",
		Description: "Rolodex DNS",
		Image:       "quay.io/town/rolodex:latest",
		Args:        []string{"-p", "127.0.0.2:53:53/udp"},
		ExecStartPre: []string{
			`/bin/sh -c 'printf "nameserver 127.0.0.2\n" > /etc/resolv.conf'`,
		},
		ExecStopPost: []string{
			"-/bin/ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf",
			"-/bin/systemctl reload-or-restart systemd-resolved",
		},
	}

	uf := GenerateSystemServiceUnit(cfg)
	content := uf.Content

	if !strings.Contains(content, `ExecStartPre=/bin/sh -c 'printf "nameserver 127.0.0.2\n" > /etc/resolv.conf'`) {
		t.Fatalf("missing resolv.conf ExecStartPre, got:\n%s", content)
	}
	if !strings.Contains(content, "ExecStopPost=-/bin/ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf") {
		t.Fatalf("missing resolv.conf restore ExecStopPost, got:\n%s", content)
	}
	if !strings.Contains(content, "ExecStopPost=-/bin/systemctl reload-or-restart systemd-resolved") {
		t.Fatalf("missing resolved restart ExecStopPost, got:\n%s", content)
	}

	// ExecStartPre should appear before ExecStart, ExecStopPost after ExecStop.
	startPreIdx := strings.Index(content, "ExecStartPre=/bin/sh")
	execStartIdx := strings.Index(content, "ExecStart=/usr/bin/podman run")
	stopPostIdx := strings.Index(content, "ExecStopPost=")
	execStopIdx := strings.Index(content, "ExecStop=/usr/bin/podman stop")
	if startPreIdx > execStartIdx {
		t.Fatal("ExecStartPre should appear before ExecStart")
	}
	if stopPostIdx < execStopIdx {
		t.Fatal("ExecStopPost should appear after ExecStop")
	}
}

func TestGenerateSystemServiceUnitNoExecStopPostWhenEmpty(t *testing.T) {
	cfg := SystemServiceUnitConfig{
		Key:         "prometheus",
		Description: "Prometheus",
		Image:       "quay.io/prometheus/prometheus:latest",
	}

	uf := GenerateSystemServiceUnit(cfg)
	if strings.Contains(uf.Content, "ExecStopPost") {
		t.Fatalf("should not contain ExecStopPost when empty, got:\n%s", uf.Content)
	}
}

func TestNCUnitRequiresImageName(t *testing.T) {
	// Verify that when NetworkControllerImage is set, the NC unit's
	// ExecStart line contains the image name before the --state flag.
	cfg := PackageUnitConfig{
		RepoName:               "core",
		PkgName:                "nginx",
		Version:                "1.0",
		Image:                  "nginx:1.0",
		Environment:            map[string]string{},
		External:               packages.PortMap{8080: 80},
		Internal:               packages.PortMap{},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "localhost/town-os-networkcontroller:local",
		NetworkStatePath:       "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	if units.NetworkController == nil {
		t.Fatal("expected NC unit")
	}
	nc := units.NetworkController.Content

	// NC units must wait for the system controller to build the NC image.
	if !strings.Contains(nc, "After=town-os-systemcontroller.service") {
		t.Fatalf("NC unit must contain After=town-os-systemcontroller.service, got:\n%s", nc)
	}

	// The image name must appear before --state in the ExecStart line.
	if !strings.Contains(nc, "localhost/town-os-networkcontroller:local /town-os-networkcontroller --state") {
		t.Fatalf("NC ExecStart must have image name and binary before --state, got:\n%s", nc)
	}

	// With an empty image name, the binary path still appears so --state
	// is passed as an argument to the binary, not as a podman flag.
	cfgEmpty := cfg
	cfgEmpty.NetworkControllerImage = ""
	unitsEmpty := GeneratePackageUnits(cfgEmpty)
	if unitsEmpty.NetworkController == nil {
		t.Fatal("expected NC unit even with empty image")
	}
	ncEmpty := unitsEmpty.NetworkController.Content
	if !strings.Contains(ncEmpty, "/town-os-networkcontroller --state") {
		t.Fatalf("expected /town-os-networkcontroller --state in NC unit, got:\n%s", ncEmpty)
	}
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

func TestIsSystemServiceUnit(t *testing.T) {
	tests := []struct {
		name   string
		expect bool
	}{
		{"town-os-system--node-exporter.service", true},
		{"town-os-system--prometheus.service", true},
		{"town-os-system--monitoring-ui.service", true},
		{"town-os-system--rolodex.service", true},
		{"town-os-system--ui.service", true},
		// The systemcontroller itself is treated as a system service even
		// though its unit name does not follow the town-os-system-- prefix.
		{"town-os-systemcontroller.service", true},
		// Package units are not system services.
		{"town-os-package--core-nginx-1.0.service", false},
		{"sshd.service", false},
		{"", false},
		// Socket variants are matched by the prefix check but the ping /
		// list handlers filter them out separately.
		{"town-os-system--prometheus-9090-tcp.socket", false},
	}

	for _, tt := range tests {
		got := IsSystemServiceUnit(tt.name)
		if got != tt.expect {
			t.Errorf("IsSystemServiceUnit(%q) = %v, want %v", tt.name, got, tt.expect)
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
		BtrfsBase:   "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
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
		BtrfsBase:   "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "Description=Town OS Package Service: test-repo/nginx@1.0") {
		t.Fatalf("expected fallback description, got:\n%s", svc)
	}
}

func TestGeneratePackageUnitsDependencySharedNetwork(t *testing.T) {
	parentNetwork := NetworkName("core", "mattermost", "1.0")
	parentUnit := UnitName("core", "mattermost", "1.0")
	parentNCUnit := NetworkControllerUnitName("core", "mattermost", "1.0")

	cfg := PackageUnitConfig{
		RepoName:               "core",
		PkgName:                "mattermost--dep--db",
		Version:                "1.0",
		Image:                  "docker.io/library/postgres:16",
		External:               packages.PortMap{},
		Internal:               packages.PortMap{5432: 5432},
		Volumes:                map[string]packages.PackageVolume{"data": {Mountpoint: "/var/lib/postgresql/data"}},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
		ParentNetwork:          parentNetwork,
		ParentUnitName:         parentUnit,
		ParentNCUnitName:       parentNCUnit,
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	// Dependency must NOT have its own NC or socket units.
	if units.NetworkController != nil {
		t.Fatal("dependency must not have its own network controller")
	}
	if len(units.Sockets) != 0 {
		t.Fatalf("dependency must not have socket units, got %d", len(units.Sockets))
	}

	// Must join the parent's network, not create its own.
	if !strings.Contains(svc, "--net "+parentNetwork) {
		t.Fatalf("dependency must use parent network, got:\n%s", svc)
	}
	ownNetwork := NetworkName("core", "mattermost--dep--db", "1.0")
	if strings.Contains(svc, "--net "+ownNetwork) {
		t.Fatalf("dependency must not use its own network, got:\n%s", svc)
	}

	// Must have PartOf and Before for systemd ordering.
	if !strings.Contains(svc, "PartOf="+parentUnit) {
		t.Fatalf("dependency missing PartOf, got:\n%s", svc)
	}
	if !strings.Contains(svc, "Before="+parentUnit) {
		t.Fatalf("dependency missing Before, got:\n%s", svc)
	}

	// Dependency must NOT have firewall rules or socket stop/start.
	if strings.Contains(svc, "firewall-cmd") {
		t.Fatalf("dependency must not have firewall rules, got:\n%s", svc)
	}
	if strings.Contains(svc, "systemctl stop") && strings.Contains(svc, ".socket") {
		t.Fatalf("dependency must not stop socket units, got:\n%s", svc)
	}
	if strings.Contains(svc, "systemctl start") && strings.Contains(svc, ".socket") {
		t.Fatalf("dependency must not start socket units, got:\n%s", svc)
	}

	// Dependency creates the network idempotently (boot race safety) but
	// must NOT rm -f (the parent NC owns cleanup).
	if !strings.Contains(svc, "podman network create "+parentNetwork) {
		t.Fatalf("dependency should idempotently create parent network, got:\n%s", svc)
	}
	if strings.Contains(svc, "podman network rm") {
		t.Fatalf("dependency must not remove network, got:\n%s", svc)
	}

	// Dependency orders after parent NC via systemd After= (no polling).
	if !strings.Contains(svc, parentNCUnit) {
		t.Fatalf("dependency must have After for parent NC unit, got:\n%s", svc)
	}
	if strings.Contains(svc, "podman inspect --format") {
		t.Fatalf("dependency must not poll NC container (Type=notify handles readiness), got:\n%s", svc)
	}
}

func TestGeneratePackageUnitsParentWithDeps(t *testing.T) {
	depUnit := UnitName("core", "mattermost--dep--db", "1.0")

	cfg := PackageUnitConfig{
		RepoName:               "core",
		PkgName:                "mattermost",
		Version:                "1.0",
		Image:                  "docker.io/mattermost/mattermost-team-edition:latest",
		External:               packages.PortMap{8065: 8065},
		Internal:               packages.PortMap{},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
		DependencyUnitNames:    []string{depUnit},
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	// Parent must have Wants and After for the dependency.
	if !strings.Contains(svc, "Wants="+depUnit) {
		t.Fatalf("parent missing Wants for dependency, got:\n%s", svc)
	}
	if !strings.Contains(svc, "After="+depUnit) {
		t.Fatalf("parent missing After for dependency, got:\n%s", svc)
	}

	// Parent must NOT have PartOf or Before (those are for deps).
	if strings.Contains(svc, "PartOf=") {
		t.Fatalf("parent must not have PartOf, got:\n%s", svc)
	}
	if strings.Contains(svc, "Before=") {
		t.Fatalf("parent must not have Before, got:\n%s", svc)
	}

	// Parent with NC: service creates network idempotently (boot race safety)
	// but must NOT rm -f (NC owns cleanup).
	ownNetwork := NetworkName("core", "mattermost", "1.0")
	if !strings.Contains(svc, "podman network create "+ownNetwork) {
		t.Fatalf("parent service should idempotently create network, got:\n%s", svc)
	}
	if strings.Contains(svc, "podman network rm -f "+ownNetwork) {
		t.Fatalf("parent service should not have network rm -f when NC exists, got:\n%s", svc)
	}
}

func TestParentWithDepsStillGetsNC(t *testing.T) {
	depUnit := UnitName("core", "app--dep--db", "1.0")

	cfg := PackageUnitConfig{
		RepoName:               "core",
		PkgName:                "app",
		Version:                "1.0",
		Image:                  "app:1.0",
		External:               packages.PortMap{8080: 80},
		Internal:               packages.PortMap{},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
		DependencyUnitNames:    []string{depUnit},
	}

	units := GeneratePackageUnits(cfg)

	if units.NetworkController == nil {
		t.Fatal("parent with ports must have its own NC")
	}
	if len(units.Sockets) != 1 {
		t.Fatalf("parent must have 1 socket unit, got %d", len(units.Sockets))
	}

	// NC unit must have Before= for both service and dependency.
	ncContent := units.NetworkController.Content
	svcUnit := UnitName("core", "app", "1.0")
	if !strings.Contains(ncContent, "Before="+svcUnit+" "+depUnit) {
		t.Fatalf("NC must start before service and deps, got:\n%s", ncContent)
	}
}

func TestGeneratePackageUnitsStandaloneNetworkCleanup(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "core",
		PkgName:                "nginx",
		Version:                "1.0",
		Image:                  "docker.io/library/nginx:1.0",
		External:               packages.PortMap{8080: 80},
		Internal:               packages.PortMap{},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	networkName := NetworkName("core", "nginx", "1.0")

	// Standalone with NC: service creates network idempotently (boot race
	// safety) but must NOT rm -f (NC owns cleanup).
	if strings.Contains(svc, "podman network rm -f "+networkName) {
		t.Fatalf("standalone service should not have network rm -f when NC exists, got:\n%s", svc)
	}
	if !strings.Contains(svc, "podman network create "+networkName) {
		t.Fatalf("standalone service should idempotently create network, got:\n%s", svc)
	}

	// NC should handle network lifecycle.
	if units.NetworkController == nil {
		t.Fatal("expected network controller unit for standalone package with ports")
	}
	ncContent := units.NetworkController.Content
	if !strings.Contains(ncContent, "podman network create "+networkName) {
		t.Fatalf("NC missing network create, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "podman network rm -f "+networkName) {
		t.Fatalf("NC missing network rm -f, got:\n%s", ncContent)
	}
}

func TestServiceUnitOrdersAfterNC(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:               "core",
		PkgName:                "nginx",
		Version:                "1.0",
		Image:                  "nginx:1.0",
		Environment:            map[string]string{},
		External:               packages.PortMap{8080: 80},
		Internal:               packages.PortMap{},
		Volumes:                map[string]packages.PackageVolume{},
		BtrfsBase:              "/town-os",
		NetworkControllerImage: "quay.io/town/networkcontroller:test",
		NetworkStatePath:       "/run/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	// Service must order after the NC unit via systemd directives.
	// The NC uses Type=notify so systemd waits for actual readiness.
	ncUnit := NetworkControllerUnitName("core", "nginx", "1.0")
	if !strings.Contains(svc, "After="+ncUnit) {
		t.Fatalf("service should order After NC unit %s, got:\n%s", ncUnit, svc)
	}
	if !strings.Contains(svc, "Wants="+ncUnit) {
		t.Fatalf("service should Want NC unit %s, got:\n%s", ncUnit, svc)
	}

	// No polling loop should be present — systemd handles readiness.
	if strings.Contains(svc, "podman inspect --format") {
		t.Fatalf("service should not poll NC container (Type=notify handles readiness), got:\n%s", svc)
	}
}

func TestDependencyUnitOrdersAfterParentNC(t *testing.T) {
	parentNCUnit := NetworkControllerUnitName("core", "app", "1.0")
	cfg := PackageUnitConfig{
		RepoName:         "core",
		PkgName:          "app--dep--db",
		Version:          "1.0",
		Image:            "postgres:15",
		Environment:      map[string]string{},
		External:         packages.PortMap{},
		Internal:         packages.PortMap{},
		Volumes:          map[string]packages.PackageVolume{},
		BtrfsBase:        "/town-os",
		ParentNetwork:    "town-os-net--core-app-1.0",
		ParentUnitName:   UnitName("core", "app", "1.0"),
		ParentNCUnitName: parentNCUnit,
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	// Dependency must order after the parent NC unit via systemd directives.
	if !strings.Contains(svc, parentNCUnit) {
		t.Fatalf("dependency should have After for parent NC unit %s, got:\n%s", parentNCUnit, svc)
	}

	// No polling loop — the parent NC uses Type=notify.
	if strings.Contains(svc, "podman inspect --format") {
		t.Fatalf("dependency should not poll NC container (Type=notify handles readiness), got:\n%s", svc)
	}
}

func TestNetworkControllerContainerNameFromUnit(t *testing.T) {
	unit := "town-os-package--core-nginx-1.0-network.service"
	got := NetworkControllerContainerNameFromUnit(unit)
	want := "town-os-package--core-nginx-1.0-network"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestServiceWithoutNCDoesNotWait(t *testing.T) {
	cfg := PackageUnitConfig{
		RepoName:    "core",
		PkgName:     "static-site",
		Version:     "1.0",
		Image:       "caddy:latest",
		Environment: map[string]string{},
		External:    packages.PortMap{},
		Internal:    packages.PortMap{},
		Volumes:     map[string]packages.PackageVolume{},
		BtrfsBase:   "/town-os",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if strings.Contains(svc, "podman inspect --format") {
		t.Fatalf("service without NC should not wait for any container, got:\n%s", svc)
	}
}

func TestSystemServiceKeyNaming(t *testing.T) {
	cfg := PackageUnitConfig{
		SystemServiceKey:       "prometheus",
		Image:                  "quay.io/prometheus/prometheus:latest",
		External:               packages.PortMap{9090: 9090},
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       "/run/state",
	}

	units := GeneratePackageUnits(cfg)

	// Service unit name uses system service prefix.
	if units.Service.Name != SystemServiceUnitName("prometheus") {
		t.Fatalf("expected %q, got %q", SystemServiceUnitName("prometheus"), units.Service.Name)
	}

	// Container name uses system service prefix.
	if !strings.Contains(units.Service.Content, "town-os-system--prometheus") {
		t.Fatalf("expected system service container name, got:\n%s", units.Service.Content)
	}

	// NC unit uses system service prefix.
	if units.NetworkController == nil {
		t.Fatal("expected NC unit")
	}
	expectedNCName := "town-os-system--prometheus-network.service"
	if units.NetworkController.Name != expectedNCName {
		t.Fatalf("expected NC name %q, got %q", expectedNCName, units.NetworkController.Name)
	}

	// Socket unit uses system service prefix.
	if len(units.Sockets) != 1 {
		t.Fatalf("expected 1 socket, got %d", len(units.Sockets))
	}
	expectedSocketName := "town-os-system--prometheus-9090-tcp.socket"
	if units.Sockets[0].Name != expectedSocketName {
		t.Fatalf("expected socket name %q, got %q", expectedSocketName, units.Sockets[0].Name)
	}

	// Network name uses system prefix.
	if !strings.Contains(units.Service.Content, "town-os-net--system-prometheus") {
		t.Fatalf("expected system network name, got:\n%s", units.Service.Content)
	}

	// NC state path uses system prefix.
	if !strings.Contains(units.NetworkController.Content, "system-prometheus.json") {
		t.Fatalf("expected system state path, got:\n%s", units.NetworkController.Content)
	}
}

func TestSystemServiceKeyExtraArgs(t *testing.T) {
	cfg := PackageUnitConfig{
		SystemServiceKey:       "test-svc",
		Image:                  "test:latest",
		ExtraArgs:              []string{"--pid", "host", "--cap-add", "SYS_TIME"},
		External:               packages.PortMap{8080: 80},
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       "/run/state",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "--pid") {
		t.Fatalf("expected --pid in extra args, got:\n%s", svc)
	}
	if !strings.Contains(svc, "--cap-add") {
		t.Fatalf("expected --cap-add in extra args, got:\n%s", svc)
	}
}

func TestSystemServiceKeyHostVolumeMounts(t *testing.T) {
	cfg := PackageUnitConfig{
		SystemServiceKey:       "test-svc",
		Image:                  "test:latest",
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       "/run/state",
		External:               packages.PortMap{8080: 80},
		HostVolumeMounts: []HostVolumeMount{
			{HostPath: "/host/config", ContainerPath: "/etc/config", Options: "ro"},
			{HostPath: "/host/data", ContainerPath: "/data"},
		},
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "-v /host/config:/etc/config:ro") {
		t.Fatalf("expected config volume mount with ro option, got:\n%s", svc)
	}
	// Default option should be rw,z.
	if !strings.Contains(svc, "-v /host/data:/data:rw,z") {
		t.Fatalf("expected data volume mount with default rw,z, got:\n%s", svc)
	}
}

func TestHostVolumeMountUIDGIDEmitsChown(t *testing.T) {
	uid := uint32(472)
	gid := uint32(472)
	otherUID := uint32(1000)
	otherGID := uint32(1000)
	cfg := PackageUnitConfig{
		SystemServiceKey:       "test-svc",
		Image:                  "test:latest",
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       "/run/state",
		External:               packages.PortMap{8080: 80},
		HostVolumeMounts: []HostVolumeMount{
			// Read-only mount without UID/GID: no chown expected.
			{HostPath: "/host/config", ContainerPath: "/etc/config", Options: "ro"},
			// Writable mount with UID/GID: chown expected.
			{HostPath: "/host/data", ContainerPath: "/data", UID: &uid, GID: &gid},
			// Second writable mount with different UID/GID.
			{HostPath: "/host/other", ContainerPath: "/other", UID: &otherUID, GID: &otherGID},
		},
		MkdirPaths: []string{"/host/data", "/host/other"},
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	// Chown is non-recursive: only the top of the bind-mount is
	// touched. Containers that write to their mount need to own the
	// top to create files inside; subdirectories are created by the
	// container process itself as its own uid and never drift.
	if !strings.Contains(svc, "ExecStartPre=/bin/chown 472:472 /host/data\n") {
		t.Fatalf("expected non-recursive chown for /host/data, got:\n%s", svc)
	}
	if !strings.Contains(svc, "ExecStartPre=/bin/chown 1000:1000 /host/other\n") {
		t.Fatalf("expected non-recursive chown for /host/other, got:\n%s", svc)
	}
	if strings.Contains(svc, "chown -R") {
		t.Fatalf("HostVolumeMount chown should NEVER be recursive, got:\n%s", svc)
	}
	// /host/config has no UID/GID, so no chown line should reference it.
	if strings.Contains(svc, "chown 472:472 /host/config") || strings.Contains(svc, "chown 1000:1000 /host/config") {
		t.Fatalf("read-only mount without UID/GID should not be chowned, got:\n%s", svc)
	}
	// Chown must come after mkdir so the path exists when chown runs.
	mkdirIdx := strings.Index(svc, "mkdir -p /host/data")
	chownIdx := strings.Index(svc, "chown 472:472 /host/data")
	if mkdirIdx < 0 || chownIdx < 0 || mkdirIdx > chownIdx {
		t.Fatalf("mkdir should precede chown for /host/data, got mkdir@%d chown@%d:\n%s", mkdirIdx, chownIdx, svc)
	}
}

func TestSystemServiceKeyMkdirAndExecStartPre(t *testing.T) {
	cfg := PackageUnitConfig{
		SystemServiceKey:       "test-svc",
		Image:                  "test:latest",
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       "/run/state",
		External:               packages.PortMap{8080: 80},
		MkdirPaths:             []string{"/data/config", "/data/storage"},
		ExecStartPreExtra:      []string{"/bin/chown -R 1000:1000 /data/storage"},
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "mkdir -p /data/config") {
		t.Fatalf("expected mkdir for /data/config, got:\n%s", svc)
	}
	if !strings.Contains(svc, "mkdir -p /data/storage") {
		t.Fatalf("expected mkdir for /data/storage, got:\n%s", svc)
	}
	if !strings.Contains(svc, "chown -R 1000:1000 /data/storage") {
		t.Fatalf("expected chown ExecStartPre, got:\n%s", svc)
	}
}

func TestSystemServiceKeyRestartAlwaysAndStartLimit(t *testing.T) {
	cfg := PackageUnitConfig{
		SystemServiceKey:       "test-svc",
		Image:                  "test:latest",
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       "/run/state",
		External:               packages.PortMap{8080: 80},
		RestartAlways:          true,
		StartLimitIntervalZero: true,
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "Restart=always") {
		t.Fatalf("expected Restart=always, got:\n%s", svc)
	}
	if !strings.Contains(svc, "StartLimitIntervalSec=0") {
		t.Fatalf("expected StartLimitIntervalSec=0, got:\n%s", svc)
	}

	// NC should also use Restart=always.
	nc := units.NetworkController.Content
	if !strings.Contains(nc, "Restart=always") {
		t.Fatalf("NC expected Restart=always, got:\n%s", nc)
	}
}

func TestSystemServiceKeyPullNever(t *testing.T) {
	cfg := PackageUnitConfig{
		SystemServiceKey:       "test-svc",
		Image:                  "local:test",
		PullNever:              true,
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       "/run/state",
		External:               packages.PortMap{8080: 80},
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	if !strings.Contains(svc, "--pull=never") {
		t.Fatalf("expected --pull=never, got:\n%s", svc)
	}
}

func TestSystemServicePackageUnitNames(t *testing.T) {
	ext := packages.PortMap{9090: 9090, 5308: 9090}
	names := SystemServicePackageUnitNames("prometheus", ext, nil)

	if len(names) != 4 {
		t.Fatalf("expected 4 unit names (service + 2 sockets + NC), got %d: %v", len(names), names)
	}

	// Verify all use system service prefix.
	for _, name := range names {
		if !strings.HasPrefix(name, SystemServiceUnitPrefix) {
			t.Fatalf("expected system service prefix, got %q", name)
		}
	}
}

func TestSystemServiceKeyDescriptionFormat(t *testing.T) {
	cfg := PackageUnitConfig{
		SystemServiceKey:       "prometheus",
		Description:            "Prometheus",
		Image:                  "prom:latest",
		External:               packages.PortMap{9090: 9090},
		NetworkControllerImage: "nc:test",
		NetworkStatePath:       "/run/state",
	}

	units := GeneratePackageUnits(cfg)

	// Service description should use the Description field.
	if !strings.Contains(units.Service.Content, "Description=Town OS: Prometheus") {
		t.Fatalf("expected description, got:\n%s", units.Service.Content)
	}

	// NC description should use the key.
	if !strings.Contains(units.NetworkController.Content, "Description=Town OS Network Controller: prometheus") {
		t.Fatalf("expected NC description with key, got:\n%s", units.NetworkController.Content)
	}

	// Socket description should use the key.
	if !strings.Contains(units.Sockets[0].Content, "Description=Town OS Socket: prometheus port 9090/tcp") {
		t.Fatalf("expected socket description with key, got:\n%s", units.Sockets[0].Content)
	}
}
