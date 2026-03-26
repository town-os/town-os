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
