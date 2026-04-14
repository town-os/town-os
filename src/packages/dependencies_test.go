package packages

import "testing"

func TestDependencyName(t *testing.T) {
	tests := []struct {
		parent string
		key    string
		want   string
	}{
		{"myapp", "db", "myapp--dep--db"},
		{"myapp", "cache", "myapp--dep--cache"},
		{"myapp--dep--db", "backup", "myapp--dep--db--dep--backup"},
	}

	for _, tt := range tests {
		got := DependencyName(tt.parent, tt.key)
		if got != tt.want {
			t.Errorf("DependencyName(%q, %q) = %q, want %q", tt.parent, tt.key, got, tt.want)
		}
	}
}

func TestParentName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"myapp", "myapp"},
		{"myapp-db", "myapp-db"},
		{"myapp--dep--db", "myapp"},
		{"myapp--dep--db--dep--backup", "myapp--dep--db"},
		{"a--dep--b--dep--c--dep--d", "a--dep--b--dep--c"},
	}

	for _, tt := range tests {
		got := ParentName(tt.name)
		if got != tt.want {
			t.Errorf("ParentName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestIsDependency(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"myapp", false},
		{"myapp-db", false},
		{"myapp--dep--db", true},
		{"myapp--dep--db--dep--backup", true},
		{"some--dep--thing", true},
	}

	for _, tt := range tests {
		got := IsDependency(tt.name)
		if got != tt.want {
			t.Errorf("IsDependency(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSubpackagesDirConstant(t *testing.T) {
	// Pin the on-disk reserved name. The refactor depends on this exact
	// string matching the parser in controller_storage.parseInstalledPath
	// and the UI dep-roll-up logic — a rename would need to land in all
	// three places simultaneously.
	if SubpackagesDir != "subpackages" {
		t.Fatalf("SubpackagesDir = %q, want %q", SubpackagesDir, "subpackages")
	}
}

func TestStoragePathAndParseRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		storage string
	}{
		{"myapp", "myapp"},
		{"myapp-db", "myapp-db"},
		{"myapp--dep--db", "myapp/subpackages/db"},
		{"myapp--dep--db--dep--backup", "myapp/subpackages/db/subpackages/backup"},
		{"a--dep--b--dep--c--dep--d", "a/subpackages/b/subpackages/c/subpackages/d"},
	}

	for _, tt := range tests {
		got := StoragePath(tt.name)
		if got != tt.storage {
			t.Errorf("StoragePath(%q) = %q, want %q", tt.name, got, tt.storage)
		}
		back := ParseStoragePath(got)
		if back != tt.name {
			t.Errorf("ParseStoragePath(%q) = %q, want %q", got, back, tt.name)
		}
	}
}

func TestParseStoragePathStandaloneIdempotent(t *testing.T) {
	// Inputs that carry no "/subpackages/" marker — including plain package
	// names and paths that happen to contain "subpackages" as a substring
	// without the leading slash — must pass through unchanged so the parser
	// is safe to call on already-flat inputs.
	for _, in := range []string{"myapp", "", "subpackages", "my-subpackages-app"} {
		if got := ParseStoragePath(in); got != in {
			t.Errorf("ParseStoragePath(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestPrettyName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"myapp", "myapp"},
		{"myapp-db", "myapp-db"},
		{"myapp--dep--db", "myapp/db"},
		{"myapp--dep--db--dep--backup", "myapp/db/backup"},
	}

	for _, tt := range tests {
		got := PrettyName(tt.name)
		if got != tt.want {
			t.Errorf("PrettyName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
