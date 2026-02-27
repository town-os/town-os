// populate-repos migrates test package repositories from GitHub into a local
// Gitea instance using the Gitea migration API. This is used during integration
// testing to avoid GitHub rate limits for git operations.
//
// The tool is idempotent: existing non-empty repositories are skipped, and
// empty repositories from failed migrations are deleted and retried.
//
// Required environment variables:
//   - GITEA_URL: base URL of the Gitea instance (e.g. http://localhost:3000)
//   - GITEA_TOKEN: admin API token (or use GITEA_USER/GITEA_PASS for basic auth)
//
// Optional environment variables:
//   - GITEA_USER: admin username (default: town-os)
//   - GITEA_PASS: admin password (default: town-os)
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type migrateRequest struct {
	CloneAddr string `json:"clone_addr"`
	RepoName  string `json:"repo_name"`
	RepoOwner string `json:"repo_owner"`
	Mirror    bool   `json:"mirror"`
	Service   string `json:"service"`
}

type giteaRepo struct {
	Empty bool `json:"empty"`
}

var repos = []struct {
	name string
	url  string
}{
	{"test-packages-core", "https://github.com/town-os/test-packages-core"},
	{"test-packages-extras", "https://github.com/town-os/test-packages-extras"},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	giteaURL := os.Getenv("GITEA_URL")
	if giteaURL == "" {
		return errors.New("GITEA_URL environment variable is required")
	}

	user := os.Getenv("GITEA_USER")
	if user == "" {
		user = "town-os"
	}
	pass := os.Getenv("GITEA_PASS")
	if pass == "" {
		pass = "town-os"
	}

	client := &http.Client{Timeout: 5 * time.Minute}

	for _, repo := range repos {
		fmt.Fprintf(os.Stderr, "Migrating %s...\n", repo.name)

		// Check if repo already exists.
		existing, err := getRepo(client, giteaURL, user, pass, repo.name)
		if err == nil {
			if !existing.Empty {
				fmt.Fprintf(os.Stderr, "  %s: exists and non-empty, skipping\n", repo.name)
				continue
			}
			// Empty repo from a failed migration — delete and retry.
			fmt.Fprintf(os.Stderr, "  %s: empty, deleting and retrying\n", repo.name)
			if err := deleteRepo(client, giteaURL, user, pass, repo.name); err != nil {
				return fmt.Errorf("delete empty repo %s: %w", repo.name, err)
			}
		}

		// Migrate from GitHub.
		if err := migrateRepo(client, giteaURL, user, pass, repo.name, repo.url); err != nil {
			return fmt.Errorf("migrate %s: %w", repo.name, err)
		}
		fmt.Fprintf(os.Stderr, "  %s: migrated successfully\n", repo.name)
	}

	return nil
}

func getRepo(client *http.Client, baseURL, user, pass, name string) (*giteaRepo, error) {
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s", baseURL, user, name)
	req, err := http.NewRequest(http.MethodGet, url, nil) //nolint:noctx // short-lived tool
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("not found")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var repo giteaRepo
	if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
		return nil, err
	}
	return &repo, nil
}

func deleteRepo(client *http.Client, baseURL, user, pass, name string) error {
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s", baseURL, user, name)
	req, err := http.NewRequest(http.MethodDelete, url, nil) //nolint:noctx // short-lived tool
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func migrateRepo(client *http.Client, baseURL, user, pass, name, cloneURL string) error {
	body, err := json.Marshal(migrateRequest{
		CloneAddr: cloneURL,
		RepoName:  name,
		RepoOwner: user,
		Mirror:    false,
		Service:   "github",
	})
	if err != nil {
		return err
	}

	url := baseURL + "/api/v1/repos/migrate"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body)) //nolint:noctx // short-lived tool
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, pass)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
