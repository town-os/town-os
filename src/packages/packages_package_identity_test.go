// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"errors"
	"testing"
)

func TestPackageIdentityString(t *testing.T) {
	pi := PackageIdentity{Name: "nginx", Version: "2.0"}
	if got := pi.String(); got != "nginx@2.0" {
		t.Fatalf("expected %q, got %q", "nginx@2.0", got)
	}
}

func TestParsePackageIdentity(t *testing.T) {
	pi, err := ParsePackageIdentity("nginx@2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pi.Name != "nginx" || pi.Version != "2.0" {
		t.Fatalf("expected nginx@2.0, got %s@%s", pi.Name, pi.Version)
	}
}

func TestParsePackageIdentityErrors(t *testing.T) {
	tests := map[string]string{
		"no @":            "nginx",
		"empty name":      "@2.0",
		"empty version":   "nginx@",
		"completely empty": "",
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePackageIdentity(input)
			if err == nil {
				t.Fatalf("expected error for %q", input)
			}
			if !errors.Is(err, ErrInvalidPackageIdentity) {
				t.Fatalf("expected ErrInvalidPackageIdentity, got %v", err)
			}
		})
	}
}

func TestParsePackageIdentityMultipleAt(t *testing.T) {
	pi, err := ParsePackageIdentity("name@ver@extra")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pi.Name != "name" || pi.Version != "ver@extra" {
		t.Fatalf("expected name@ver@extra, got %s@%s", pi.Name, pi.Version)
	}
}

func TestPackageIdentityRoundTrip(t *testing.T) {
	original := PackageIdentity{Name: "redis", Version: "7.0"}
	parsed, err := ParsePackageIdentity(original.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed != original {
		t.Fatalf("round-trip failed: %v != %v", parsed, original)
	}
}

func TestParsePackageIdentityRepoScoped(t *testing.T) {
	pi, err := ParsePackageIdentity("repo-a/nginx@1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pi.Repo != "repo-a" || pi.Name != "nginx" || pi.Version != "1.0" {
		t.Fatalf("expected repo-a/nginx@1.0, got %s/%s@%s", pi.Repo, pi.Name, pi.Version)
	}
}

func TestParsePackageIdentityRepoWithHyphens(t *testing.T) {
	pi, err := ParsePackageIdentity("my-repo/name@1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pi.Repo != "my-repo" {
		t.Fatalf("expected repo %q, got %q", "my-repo", pi.Repo)
	}
	if pi.Name != "name" || pi.Version != "1.0" {
		t.Fatalf("expected name@1.0, got %s@%s", pi.Name, pi.Version)
	}
}

func TestPackageIdentityStringWithRepo(t *testing.T) {
	pi := PackageIdentity{Repo: "repo-a", Name: "nginx", Version: "2.0"}
	if got := pi.String(); got != "repo-a/nginx@2.0" {
		t.Fatalf("expected %q, got %q", "repo-a/nginx@2.0", got)
	}
}

func TestPackageIdentityStringWithoutRepo(t *testing.T) {
	pi := PackageIdentity{Name: "nginx", Version: "2.0"}
	if got := pi.String(); got != "nginx@2.0" {
		t.Fatalf("expected %q, got %q", "nginx@2.0", got)
	}
}

func TestPackageIdentityRoundTripRepoScoped(t *testing.T) {
	original := PackageIdentity{Repo: "core", Name: "redis", Version: "7.0"}
	parsed, err := ParsePackageIdentity(original.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed != original {
		t.Fatalf("round-trip failed: %v != %v", parsed, original)
	}
}

func TestParsePackageIdentityRepoScopedEdgeCases(t *testing.T) {
	tests := map[string]struct {
		input   string
		wantErr bool
		repo    string
		name    string
		version string
	}{
		"version with @": {
			input: "repo/name@ver@extra", wantErr: false,
			repo: "repo", name: "name", version: "ver@extra",
		},
		"minimal valid": {
			input: "a/b@c", wantErr: false,
			repo: "a", name: "b", version: "c",
		},
		"empty name": {
			input: "repo/@1.0", wantErr: true,
		},
		"empty version": {
			input: "repo/name@", wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pi, err := ParsePackageIdentity(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if pi.Repo != tt.repo || pi.Name != tt.name || pi.Version != tt.version {
				t.Fatalf("expected %s/%s@%s, got %s/%s@%s", tt.repo, tt.name, tt.version, pi.Repo, pi.Name, pi.Version)
			}
		})
	}
}
