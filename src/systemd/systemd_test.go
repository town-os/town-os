package systemd

import (
	"strings"
	"testing"
)

func TestContainerName(t *testing.T) {
	got := ContainerName("test-repo", "nginx", "1.0")
	expected := "town-os-package--test-repo-nginx-1.0"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestContainerNameDifferentInputs(t *testing.T) {
	tests := []struct {
		repo, pkg, version string
		expected           string
	}{
		{"core", "redis", "7.0", "town-os-package--core-redis-7.0"},
		{"extras", "postgres", "15.2", "town-os-package--extras-postgres-15.2"},
		{"my-repo", "my-app", "0.1.0", "town-os-package--my-repo-my-app-0.1.0"},
	}
	for _, tt := range tests {
		got := ContainerName(tt.repo, tt.pkg, tt.version)
		if got != tt.expected {
			t.Errorf("ContainerName(%q, %q, %q) = %q, want %q",
				tt.repo, tt.pkg, tt.version, got, tt.expected)
		}
	}
}

func TestStubUnitContent(t *testing.T) {
	content := StubUnitContent("test-repo", "nginx", "1.0")

	if !strings.Contains(content, "[Unit]") {
		t.Fatal("missing [Unit] section")
	}
	if !strings.Contains(content, "[Service]") {
		t.Fatal("missing [Service] section")
	}
	if !strings.Contains(content, "[Install]") {
		t.Fatal("missing [Install] section")
	}
	if !strings.Contains(content, "Description=Town OS Package Service: test-repo/nginx@1.0") {
		t.Fatal("missing or incorrect Description")
	}
	if !strings.Contains(content, "Type=simple") {
		t.Fatal("missing Type=simple")
	}
	if !strings.Contains(content, "ExecStart=/bin/sh") {
		t.Fatal("missing ExecStart")
	}
	if !strings.Contains(content, "test-repo/nginx@1.0 running") {
		t.Fatal("missing running message in loop")
	}
	if !strings.Contains(content, "WantedBy=multi-user.target") {
		t.Fatal("missing WantedBy=multi-user.target")
	}
}

func TestStubUnitContentDifferentPackage(t *testing.T) {
	content := StubUnitContent("core", "redis", "7.0")

	if !strings.Contains(content, "Description=Town OS Package Service: core/redis@7.0") {
		t.Fatalf("expected description for core/redis@7.0, got:\n%s", content)
	}
	if !strings.Contains(content, "core/redis@7.0 running") {
		t.Fatalf("expected running message for core/redis@7.0, got:\n%s", content)
	}
}
