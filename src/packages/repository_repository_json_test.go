package packages

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestRepositoryJSONRoundTrip(t *testing.T) {
	t.Run("with credentials", func(t *testing.T) {
		original := Repository{
			Name:     "myrepo",
			URL:      url.URL{Scheme: "https", Host: "github.com", Path: "/org/repo.git"},
			Username: "deploy",
			Password: "token123",
		}

		data := marshalJSON(t, original)

		var got Repository
		err := json.Unmarshal(data, &got)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		if got.Name != original.Name {
			t.Fatalf("Name: expected %q, got %q", original.Name, got.Name)
		}
		if got.URL.String() != original.URL.String() {
			t.Fatalf("URL: expected %q, got %q", original.URL.String(), got.URL.String())
		}
		if got.Username != original.Username {
			t.Fatalf("Username: expected %q, got %q", original.Username, got.Username)
		}
		if got.Password != original.Password {
			t.Fatalf("Password: expected %q, got %q", original.Password, got.Password)
		}
	})

	t.Run("without credentials", func(t *testing.T) {
		original := Repository{
			Name: "public",
			URL:  url.URL{Scheme: "https", Host: "github.com", Path: "/org/public-repo"},
		}

		data := marshalJSON(t, original)

		var got Repository
		err := json.Unmarshal(data, &got)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		if got.Name != original.Name {
			t.Fatalf("Name: expected %q, got %q", original.Name, got.Name)
		}
		if got.URL.String() != original.URL.String() {
			t.Fatalf("URL: expected %q, got %q", original.URL.String(), got.URL.String())
		}
		if got.Username != "" {
			t.Fatalf("Username: expected empty, got %q", got.Username)
		}
		if got.Password != "" {
			t.Fatalf("Password: expected empty, got %q", got.Password)
		}
	})

	t.Run("special characters in password", func(t *testing.T) {
		original := Repository{
			Name:     "special",
			URL:      url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"},
			Username: "user",
			Password: "p@ss:w0rd/special",
		}

		data := marshalJSON(t, original)

		var got Repository
		err := json.Unmarshal(data, &got)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		if got.Username != original.Username {
			t.Fatalf("Username: expected %q, got %q", original.Username, got.Username)
		}
		if got.Password != original.Password {
			t.Fatalf("Password: expected %q, got %q", original.Password, got.Password)
		}
	})
}

func TestRepositoryUnmarshalLegacyPairs(t *testing.T) {
	t.Run("legacy pair with credentials", func(t *testing.T) {
		data := []byte(`[["core","https://user:pass@github.com/town-os/test-packages-core"]]`)

		var repos []Repository
		err := json.Unmarshal(data, &repos)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		if len(repos) != 1 {
			t.Fatalf("expected 1 repo, got %d", len(repos))
		}

		r := repos[0]
		if r.Name != "core" {
			t.Fatalf("Name: expected %q, got %q", "core", r.Name)
		}
		if r.URL.Host != "github.com" {
			t.Fatalf("Host: expected %q, got %q", "github.com", r.URL.Host)
		}
		if r.URL.Path != "/town-os/test-packages-core" {
			t.Fatalf("Path: expected %q, got %q", "/town-os/test-packages-core", r.URL.Path)
		}
		if r.URL.User != nil {
			t.Fatalf("URL.User: expected nil, got %v", r.URL.User)
		}
		if r.Username != "user" {
			t.Fatalf("Username: expected %q, got %q", "user", r.Username)
		}
		if r.Password != "pass" {
			t.Fatalf("Password: expected %q, got %q", "pass", r.Password)
		}
	})

	t.Run("legacy pair without credentials", func(t *testing.T) {
		data := []byte(`[["public","https://github.com/town-os/public-repo"]]`)

		var repos []Repository
		err := json.Unmarshal(data, &repos)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		r := repos[0]
		if r.Name != "public" {
			t.Fatalf("Name: expected %q, got %q", "public", r.Name)
		}
		if r.Username != "" {
			t.Fatalf("Username: expected empty, got %q", r.Username)
		}
		if r.Password != "" {
			t.Fatalf("Password: expected empty, got %q", r.Password)
		}
	})
}

func TestRepositoryUnmarshalObjectFormat(t *testing.T) {
	t.Run("object with credentials", func(t *testing.T) {
		data := []byte(`[{"name":"core","url":"https://user:pass@github.com/town-os/test-packages-core"}]`)

		var repos []Repository
		err := json.Unmarshal(data, &repos)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		if len(repos) != 1 {
			t.Fatalf("expected 1 repo, got %d", len(repos))
		}

		r := repos[0]
		if r.Name != "core" {
			t.Fatalf("Name: expected %q, got %q", "core", r.Name)
		}
		if r.URL.Host != "github.com" {
			t.Fatalf("Host: expected %q, got %q", "github.com", r.URL.Host)
		}
		if r.Username != "user" {
			t.Fatalf("Username: expected %q, got %q", "user", r.Username)
		}
		if r.Password != "pass" {
			t.Fatalf("Password: expected %q, got %q", "pass", r.Password)
		}
	})

	t.Run("object without credentials", func(t *testing.T) {
		data := []byte(`[{"name":"public","url":"https://github.com/town-os/public-repo"}]`)

		var repos []Repository
		err := json.Unmarshal(data, &repos)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		r := repos[0]
		if r.Name != "public" {
			t.Fatalf("Name: expected %q, got %q", "public", r.Name)
		}
		if r.Username != "" {
			t.Fatalf("Username: expected empty, got %q", r.Username)
		}
	})

	t.Run("marshal produces object format", func(t *testing.T) {
		r := Repository{
			Name:     "test",
			URL:      url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"},
			Username: "u",
			Password: "p",
		}

		data := marshalJSON(t, r)
		want := `{"name":"test","url":"https://u:p@example.com/repo.git"}`
		if string(data) != want {
			t.Fatalf("expected %s, got %s", want, string(data))
		}
	})
}
