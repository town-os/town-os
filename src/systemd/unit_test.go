package systemd

import (
	"fmt"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

func TestGeneratePackageUnitsBasic(t *testing.T) {
	cfg := PackageUnitConfig{
		PkgName:     "nginx",
		Version:     "1.0",
		Image:       "docker.io/library/nginx:1.0",
		Environment: map[string]string{"NGINX_HOST": "example.com"},
		External:    packages.PortMap{8080: 80},
		Internal:    packages.PortMap{},
		Volumes: map[string]packages.PackageVolume{
			"data": {Mountpoint: "/var/data"},
		},
		BtrfsBase:   "/data/btrfs",
		UPnPBinPath: "/town-os-upnp",
	}

	units := GeneratePackageUnits(cfg)

	// Verify service unit.
	if units.Service.Name != "town-os-nginx.service" {
		t.Fatalf("expected service name town-os-nginx.service, got %s", units.Service.Name)
	}

	svc := units.Service.Content
	if !strings.Contains(svc, "Description=Town OS Package Service: nginx@1.0") {
		t.Fatal("service missing description")
	}
	if !strings.Contains(svc, "-p 8080:80") {
		t.Fatal("service missing port mapping")
	}
	if !strings.Contains(svc, "-e NGINX_HOST=example.com") {
		t.Fatal("service missing environment variable")
	}
	if !strings.Contains(svc, "-v /data/btrfs/installed/nginx/1.0/data:/var/data:rw,z") {
		t.Fatalf("service missing volume mount, got:\n%s", svc)
	}
	if !strings.Contains(svc, "systemctl stop town-os-nginx-8080-tcp.socket") {
		t.Fatal("service missing socket stop in ExecStartPre")
	}
	if !strings.Contains(svc, "systemctl start town-os-nginx-8080-tcp.socket") {
		t.Fatal("service missing socket start in ExecStopPost")
	}
	if !strings.Contains(svc, "firewall-cmd --add-port=8080/tcp") {
		t.Fatal("service missing firewall add-port")
	}
	if !strings.Contains(svc, "firewall-cmd --remove-port=8080/tcp") {
		t.Fatal("service missing firewall remove-port")
	}
	if !strings.Contains(svc, fmt.Sprintf("Wants=%s", UPnPTimerUnitName("nginx"))) {
		t.Fatal("service missing Wants for uPnP timer")
	}

	// Verify socket units.
	if len(units.Sockets) != 1 {
		t.Fatalf("expected 1 socket unit, got %d", len(units.Sockets))
	}
	sock := units.Sockets[0]
	if sock.Name != "town-os-nginx-8080-tcp.socket" {
		t.Fatalf("expected socket name town-os-nginx-8080-tcp.socket, got %s", sock.Name)
	}
	if !strings.Contains(sock.Content, "ListenStream=8080") {
		t.Fatal("socket missing ListenStream")
	}
	if !strings.Contains(sock.Content, "FreeBind=true") {
		t.Fatal("socket missing FreeBind")
	}
	if !strings.Contains(sock.Content, "PartOf=town-os-nginx.service") {
		t.Fatal("socket missing PartOf")
	}

	// Verify uPnP units exist.
	if units.UPnPService == nil {
		t.Fatal("expected uPnP service unit")
	}
	if units.UPnPTimer == nil {
		t.Fatal("expected uPnP timer unit")
	}
	if units.UPnPService.Name != "town-os-nginx-upnp.service" {
		t.Fatalf("expected uPnP service name town-os-nginx-upnp.service, got %s", units.UPnPService.Name)
	}
	if !strings.Contains(units.UPnPService.Content, "/town-os-upnp add --port 8080:80 --ttl 600") {
		t.Fatalf("uPnP service missing correct ExecStart, got:\n%s", units.UPnPService.Content)
	}
	if units.UPnPTimer.Name != "town-os-nginx-upnp.timer" {
		t.Fatalf("expected uPnP timer name town-os-nginx-upnp.timer, got %s", units.UPnPTimer.Name)
	}
	if !strings.Contains(units.UPnPTimer.Content, "OnBootSec=1min") {
		t.Fatal("uPnP timer missing OnBootSec")
	}
}

func TestGeneratePackageUnitsMultiplePorts(t *testing.T) {
	cfg := PackageUnitConfig{
		PkgName:     "myapp",
		Version:     "2.0",
		Image:       "myapp:2.0",
		Environment: map[string]string{},
		External:    packages.PortMap{8080: 80, 8443: 443},
		Internal:    packages.PortMap{9090: 9090},
		Volumes:     map[string]packages.PackageVolume{},
		BtrfsBase:   "/data/btrfs",
		UPnPBinPath: "/town-os-upnp",
	}

	units := GeneratePackageUnits(cfg)

	// 3 sockets (8080, 8443, 9090).
	if len(units.Sockets) != 3 {
		t.Fatalf("expected 3 socket units, got %d", len(units.Sockets))
	}

	// All ports in -p and firewall.
	svc := units.Service.Content
	if !strings.Contains(svc, "-p 8080:80") {
		t.Fatal("service missing port 8080:80")
	}
	if !strings.Contains(svc, "-p 8443:443") {
		t.Fatal("service missing port 8443:443")
	}
	if !strings.Contains(svc, "-p 9090:9090") {
		t.Fatal("service missing port 9090:9090")
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

	// uPnP service should only have external ports.
	if units.UPnPService == nil {
		t.Fatal("expected uPnP service unit for external ports")
	}
	upnp := units.UPnPService.Content
	if !strings.Contains(upnp, "--port 8080:80") {
		t.Fatal("uPnP missing external port 8080:80")
	}
	if !strings.Contains(upnp, "--port 8443:443") {
		t.Fatal("uPnP missing external port 8443:443")
	}
	if strings.Contains(upnp, "9090") {
		t.Fatal("uPnP should NOT contain internal port 9090")
	}
}

func TestGeneratePackageUnitsInternalOnly(t *testing.T) {
	cfg := PackageUnitConfig{
		PkgName:     "redis",
		Version:     "7.0",
		Image:       "redis:7.0",
		Environment: map[string]string{},
		External:    packages.PortMap{},
		Internal:    packages.PortMap{6379: 6379},
		Volumes:     map[string]packages.PackageVolume{},
		BtrfsBase:   "/data/btrfs",
		UPnPBinPath: "/town-os-upnp",
	}

	units := GeneratePackageUnits(cfg)

	// Sockets and firewall for internal port.
	if len(units.Sockets) != 1 {
		t.Fatalf("expected 1 socket unit, got %d", len(units.Sockets))
	}
	svc := units.Service.Content
	if !strings.Contains(svc, "-p 6379:6379") {
		t.Fatal("service missing internal port mapping")
	}
	if !strings.Contains(svc, "--add-port=6379/tcp") {
		t.Fatal("service missing firewall for internal port")
	}

	// No uPnP units.
	if units.UPnPService != nil {
		t.Fatal("expected no uPnP service unit for internal-only ports")
	}
	if units.UPnPTimer != nil {
		t.Fatal("expected no uPnP timer unit for internal-only ports")
	}

	// No Wants line for uPnP timer.
	if strings.Contains(svc, "Wants=") {
		t.Fatal("service should not have Wants for uPnP timer with no external ports")
	}
}

func TestGeneratePackageUnitsNoPorts(t *testing.T) {
	cfg := PackageUnitConfig{
		PkgName:     "worker",
		Version:     "1.0",
		Image:       "worker:1.0",
		Environment: map[string]string{},
		External:    packages.PortMap{},
		Internal:    packages.PortMap{},
		Volumes:     map[string]packages.PackageVolume{},
		BtrfsBase:   "/data/btrfs",
		UPnPBinPath: "/town-os-upnp",
	}

	units := GeneratePackageUnits(cfg)

	// No sockets.
	if len(units.Sockets) != 0 {
		t.Fatalf("expected 0 socket units, got %d", len(units.Sockets))
	}

	// No uPnP.
	if units.UPnPService != nil {
		t.Fatal("expected no uPnP service")
	}
	if units.UPnPTimer != nil {
		t.Fatal("expected no uPnP timer")
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
	if !strings.Contains(svc, "Description=Town OS Package Service: worker@1.0") {
		t.Fatal("service missing description")
	}
	if !strings.Contains(svc, "worker:1.0") {
		t.Fatal("service missing image reference")
	}
}

func TestGeneratePackageUnitsEnvironmentSorted(t *testing.T) {
	cfg := PackageUnitConfig{
		PkgName: "myapp",
		Version: "1.0",
		Image:   "myapp:1.0",
		Environment: map[string]string{
			"ZEBRA":  "last",
			"ALPHA":  "first",
			"MIDDLE": "mid",
		},
		External:    packages.PortMap{},
		Internal:    packages.PortMap{},
		Volumes:     map[string]packages.PackageVolume{},
		BtrfsBase:   "/data/btrfs",
		UPnPBinPath: "/town-os-upnp",
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

	names := PackageUnitNames("nginx", external, internal)

	expected := []string{
		"town-os-nginx.service",
		"town-os-nginx-8080-tcp.socket",
		"town-os-nginx-8443-tcp.socket",
		"town-os-nginx-9090-tcp.socket",
		"town-os-nginx-upnp.service",
		"town-os-nginx-upnp.timer",
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

	names := PackageUnitNames("redis", external, internal)

	expected := []string{
		"town-os-redis.service",
		"town-os-redis-6379-tcp.socket",
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
	names := PackageUnitNames("worker", packages.PortMap{}, packages.PortMap{})

	if len(names) != 1 {
		t.Fatalf("expected 1 name, got %d: %v", len(names), names)
	}
	if names[0] != "town-os-worker.service" {
		t.Fatalf("expected town-os-worker.service, got %s", names[0])
	}
}

func TestSocketUnitName(t *testing.T) {
	name := SocketUnitName("nginx", 8080)
	if name != "town-os-nginx-8080-tcp.socket" {
		t.Fatalf("expected town-os-nginx-8080-tcp.socket, got %s", name)
	}
}

func TestUPnPServiceUnitName(t *testing.T) {
	name := UPnPServiceUnitName("nginx")
	if name != "town-os-nginx-upnp.service" {
		t.Fatalf("expected town-os-nginx-upnp.service, got %s", name)
	}
}

func TestUPnPTimerUnitName(t *testing.T) {
	name := UPnPTimerUnitName("nginx")
	if name != "town-os-nginx-upnp.timer" {
		t.Fatalf("expected town-os-nginx-upnp.timer, got %s", name)
	}
}

func TestGeneratePackageUnitsVolumeFormat(t *testing.T) {
	cfg := PackageUnitConfig{
		PkgName: "myapp",
		Version: "1.0",
		Image:   "myapp:1.0",
		Environment: map[string]string{},
		External:    packages.PortMap{},
		Internal:    packages.PortMap{},
		Volumes: map[string]packages.PackageVolume{
			"data":   {Mountpoint: "/var/lib/data"},
			"config": {Mountpoint: "/etc/myapp"},
		},
		BtrfsBase:   "/data/btrfs",
		UPnPBinPath: "/town-os-upnp",
	}

	units := GeneratePackageUnits(cfg)
	svc := units.Service.Content

	// Volumes should be sorted by name (config before data).
	configIdx := strings.Index(svc, "/data/btrfs/installed/myapp/1.0/config:/etc/myapp:rw,z")
	dataIdx := strings.Index(svc, "/data/btrfs/installed/myapp/1.0/data:/var/lib/data:rw,z")

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
