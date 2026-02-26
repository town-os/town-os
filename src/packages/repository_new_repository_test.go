package packages

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/git"
)

func TestNewRepositoryName(t *testing.T) {
	tests := map[string]struct {
		url      string
		wantName string
	}{
		"https with .git": {
			url:      "https://github.com/user/my-repo.git",
			wantName: "my-repo",
		},
		"https without .git": {
			url:      "https://github.com/user/my-repo",
			wantName: "my-repo",
		},
		"nested path": {
			url:      "https://gitea.example.com/org/sub/repo.git",
			wantName: "repo",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			u, err := url.Parse(tt.url)
			if err != nil {
				t.Fatalf("bad test url: %v", err)
			}

			// NewRepository will fail on clone, but we can still check the Name was set
			r := &Repository{
				Name: strings.TrimSuffix(filepath.Base(u.Path), ".git"),
				URL:  *u,
			}

			if r.Name != tt.wantName {
				t.Fatalf("expected name %q, got %q", tt.wantName, r.Name)
			}
		})
	}
}

func TestNewRepositoryBadCredentials(t *testing.T) {
	dir := t.TempDir()
	u := url.URL{Scheme: "https", Host: "github.com", Path: "/town-os/does-not-exist.git"}

	_, err := NewRepository(dir, "", u, "", "", &git.GoGitClient{Home: dir})
	if err == nil {
		t.Fatal("expected error for inaccessible repository")
	}
}

func TestNewRepositoryPartialCredentials(t *testing.T) {
	t.Run("username without password", func(t *testing.T) {
		dir := t.TempDir()
		u := url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"}

		_, err := NewRepository(dir, "", u, "user", "", git.InitMockClient())
		if !errors.Is(err, ErrPartialCredentials) {
			t.Fatalf("expected ErrPartialCredentials, got %v", err)
		}
	})

	t.Run("password without username", func(t *testing.T) {
		dir := t.TempDir()
		u := url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"}

		_, err := NewRepository(dir, "", u, "", "pass", git.InitMockClient())
		if !errors.Is(err, ErrPartialCredentials) {
			t.Fatalf("expected ErrPartialCredentials, got %v", err)
		}
	})

	t.Run("both empty is allowed", func(t *testing.T) {
		dir := t.TempDir()
		u := url.URL{Scheme: "https", Host: "github.com", Path: "/town-os/does-not-exist.git"}

		// Will fail at clone, but should not fail at credential validation
		_, err := NewRepository(dir, "", u, "", "", &git.GoGitClient{Home: dir})
		if errors.Is(err, ErrPartialCredentials) {
			t.Fatal("empty username and password should not trigger partial credentials error")
		}
	})

	t.Run("both provided is allowed", func(t *testing.T) {
		dir := t.TempDir()
		u := url.URL{Scheme: "https", Host: "github.com", Path: "/town-os/does-not-exist.git"}

		// Will fail at clone, but should not fail at credential validation
		_, err := NewRepository(dir, "", u, "user", "pass", &git.GoGitClient{Home: dir})
		if errors.Is(err, ErrPartialCredentials) {
			t.Fatal("providing both username and password should not trigger partial credentials error")
		}
	})
}

func TestCredentialURL(t *testing.T) {
	t.Run("empty username returns plain URL", func(t *testing.T) {
		r := &Repository{
			Name: "repo",
			URL:  url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"},
		}
		got := r.credentialURL()
		want := "https://example.com/repo.git"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("username and password", func(t *testing.T) {
		r := &Repository{
			Name:     "repo",
			URL:      url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"},
			Username: "user",
			Password: "pass",
		}
		got := r.credentialURL()
		want := "https://user:pass@example.com/repo.git"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("username only empty password", func(t *testing.T) {
		r := &Repository{
			Name:     "repo",
			URL:      url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"},
			Username: "user",
		}
		got := r.credentialURL()
		want := "https://user:@example.com/repo.git"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("special characters in password are encoded", func(t *testing.T) {
		r := &Repository{
			Name:     "repo",
			URL:      url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"},
			Username: "user",
			Password: "p@ss:w0rd/special",
		}
		got := r.credentialURL()

		// Verify round-trip through url.Parse preserves credentials
		parsed, err := url.Parse(got)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", got, err)
		}
		if parsed.User.Username() != "user" {
			t.Fatalf("expected username %q, got %q", "user", parsed.User.Username())
		}
		pass, ok := parsed.User.Password()
		if !ok {
			t.Fatal("expected password to be set")
		}
		if pass != "p@ss:w0rd/special" {
			t.Fatalf("expected password %q, got %q", "p@ss:w0rd/special", pass)
		}
	})

	t.Run("round-trip through url.Parse", func(t *testing.T) {
		r := &Repository{
			Name:     "repo",
			URL:      url.URL{Scheme: "https", Host: "github.com", Path: "/org/repo.git"},
			Username: "deploy",
			Password: "token123",
		}
		got := r.credentialURL()

		parsed, err := url.Parse(got)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", got, err)
		}
		if parsed.Scheme != "https" {
			t.Fatalf("expected scheme https, got %q", parsed.Scheme)
		}
		if parsed.Host != "github.com" {
			t.Fatalf("expected host github.com, got %q", parsed.Host)
		}
		if parsed.Path != "/org/repo.git" {
			t.Fatalf("expected path /org/repo.git, got %q", parsed.Path)
		}
	})
}

func TestSanitizeURL(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"with username and password": {
			input: "https://erikh:ghp_secret123@github.com/org/repo.git",
			want:  "https://USERNAME:PASSWORD@github.com/org/repo.git",
		},
		"without credentials": {
			input: "https://github.com/org/repo.git",
			want:  "https://github.com/org/repo.git",
		},
		"username only": {
			input: "https://erikh@github.com/org/repo.git",
			want:  "https://USERNAME:PASSWORD@github.com/org/repo.git",
		},
		"special characters in password": {
			input: "https://user:p%40ss%3Aw0rd@example.com/repo.git",
			want:  "https://USERNAME:PASSWORD@example.com/repo.git",
		},
		"not a URL": {
			input: "not-a-url",
			want:  "not-a-url",
		},
		"empty string": {
			input: "",
			want:  "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := SanitizeURL(tt.input)
			if got != tt.want {
				t.Fatalf("SanitizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}

	t.Run("never contains original credentials", func(t *testing.T) {
		input := "https://deploy:ghp_SuperSecretToken123@github.com/org/repo.git"
		got := SanitizeURL(input)
		if strings.Contains(got, "deploy") {
			t.Fatalf("sanitized URL still contains username: %q", got)
		}
		if strings.Contains(got, "ghp_SuperSecretToken123") {
			t.Fatalf("sanitized URL still contains password: %q", got)
		}
	})
}
