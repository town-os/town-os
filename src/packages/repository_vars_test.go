package packages

import (
	"net/url"
	"testing"
)

func TestDefaultRepoURLIsNonEmpty(t *testing.T) {
	if DefaultRepoURL == "" {
		t.Fatal("DefaultRepoURL must not be empty")
	}
}

func TestDefaultRepoURLIsValidURL(t *testing.T) {
	u, err := url.Parse(DefaultRepoURL)
	if err != nil {
		t.Fatalf("DefaultRepoURL is not a valid URL: %v", err)
	}
	if u.Scheme == "" || u.Host == "" {
		t.Fatalf("DefaultRepoURL must have scheme and host, got %q", DefaultRepoURL)
	}
}

func TestDefaultRepositoriesUsesDefaultRepoURL(t *testing.T) {
	repos := DefaultRepositories()
	if len(repos) == 0 {
		t.Fatal("DefaultRepositories() returned empty slice")
	}

	expected, err := url.Parse(DefaultRepoURL)
	if err != nil {
		t.Fatalf("parse DefaultRepoURL: %v", err)
	}

	found := false
	for _, r := range repos {
		if r.URL.Host == expected.Host && r.URL.Path == expected.Path {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("DefaultRepositories() does not contain DefaultRepoURL %q", DefaultRepoURL)
	}
}
