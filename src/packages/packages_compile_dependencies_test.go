package packages

import (
	"errors"
	"testing"
)

func TestCompileWithDependencies(t *testing.T) {
	ip := InputPackage{
		Image: InputPackageImage{URL: "myapp:latest"},
		Dependencies: map[string]InputPackageDependency{
			"db": {
				Package:   "postgres",
				Version:   "15.0",
				Responses: map[string]string{"port": "5432"},
			},
			"cache": {
				Package: "redis",
				Repo:    "extras",
			},
		},
	}

	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if len(compiled.Dependencies) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(compiled.Dependencies))
	}

	db := compiled.Dependencies["db"]
	if db.Package != "postgres" {
		t.Errorf("db.Package = %q, want %q", db.Package, "postgres")
	}
	if db.Version != "15.0" {
		t.Errorf("db.Version = %q, want %q", db.Version, "15.0")
	}
	if db.Responses["port"] != "5432" {
		t.Errorf("db.Responses[port] = %q, want %q", db.Responses["port"], "5432")
	}

	cache := compiled.Dependencies["cache"]
	if cache.Package != "redis" {
		t.Errorf("cache.Package = %q, want %q", cache.Package, "redis")
	}
	if cache.Repo != "extras" {
		t.Errorf("cache.Repo = %q, want %q", cache.Repo, "extras")
	}
}

func TestCompileWithNoDependencies(t *testing.T) {
	ip := InputPackage{
		Image: InputPackageImage{URL: "myapp:latest"},
	}

	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if compiled.Dependencies != nil {
		t.Errorf("expected nil dependencies, got %v", compiled.Dependencies)
	}
}

func TestCompileDependencyResponseTemplateSubstitution(t *testing.T) {
	ip := InputPackage{
		Image: InputPackageImage{URL: "myapp:latest"},
		Questions: map[string]Question{
			"dbpass": {Query: "Database password?"},
		},
		Dependencies: map[string]InputPackageDependency{
			"db": {
				Package:   "postgres",
				Responses: map[string]string{"password": "@dbpass@"},
			},
		},
	}

	compiled, err := ip.Compile(Responses{"dbpass": "s3cret"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	db := compiled.Dependencies["db"]
	if db.Responses["password"] != "s3cret" {
		t.Errorf("db.Responses[password] = %q, want %q", db.Responses["password"], "s3cret")
	}
}

func TestValidateDependencyKeyFormat(t *testing.T) {
	tests := []struct {
		key  string
		fail bool
	}{
		{"db", false},
		{"cache-1", false},
		{"my_dep", false},
		{"dep.name", false},
		{"", true},
		{"-invalid", true},
		{"bad key", true},
	}

	for _, tt := range tests {
		err := ValidateDependencyName(tt.key)
		if tt.fail && err == nil {
			t.Errorf("ValidateDependencyName(%q) should fail", tt.key)
		}
		if !tt.fail && err != nil {
			t.Errorf("ValidateDependencyName(%q) unexpected error: %v", tt.key, err)
		}
	}
}

func TestValidateDependencySpecPackageRequired(t *testing.T) {
	err := ValidateDependencySpec(InputPackageDependency{Package: ""})
	if err == nil {
		t.Error("expected error for empty package")
	}
	if !errors.Is(err, ErrInvalidDependencySpec) {
		t.Errorf("expected ErrInvalidDependencySpec, got %v", err)
	}

	err = ValidateDependencySpec(InputPackageDependency{Package: "postgres"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateRejectsDepSeparatorInPackageName(t *testing.T) {
	err := ValidatePackageName("myapp--dep--db")
	if err == nil {
		t.Error("expected error for name containing --dep--")
	}
	if !errors.Is(err, ErrInvalidDependencyName) {
		t.Errorf("expected ErrInvalidDependencyName, got %v", err)
	}

	err = ValidatePackageName("myapp")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCompileRejectsInvalidDependencyKey(t *testing.T) {
	ip := InputPackage{
		Image: InputPackageImage{URL: "myapp:latest"},
		Dependencies: map[string]InputPackageDependency{
			"bad key": {Package: "postgres"},
		},
	}

	_, err := ip.Compile(Responses{})
	if err == nil {
		t.Error("expected validation error for invalid dependency key")
	}
}

func TestCompileRejectsEmptyDependencyPackage(t *testing.T) {
	ip := InputPackage{
		Image: InputPackageImage{URL: "myapp:latest"},
		Dependencies: map[string]InputPackageDependency{
			"db": {Package: ""},
		},
	}

	_, err := ip.Compile(Responses{})
	if err == nil {
		t.Error("expected validation error for empty dependency package")
	}
}
