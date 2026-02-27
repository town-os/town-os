// populate-repos creates test package repositories in a local Gitea instance
// by migrating them from GitHub. It is idempotent: existing repos are skipped.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	adminUser = "town-os"
	adminPass = "town-os-test"
	adminMail = "town-os@localhost"
)

// repo describes a GitHub repository to mirror into Gitea.
type repo struct {
	Owner string // GitHub owner
	Name  string // repository name
}

var repos = []repo{
	{Owner: "town-os", Name: "test-packages-core"},
	{Owner: "town-os", Name: "test-packages-extras"},
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

	ghUser := os.Getenv("TOWN_OS_REPO_USERNAME")
	ghPass := os.Getenv("TOWN_OS_REPO_PASSWORD")

	client := &http.Client{Timeout: 120 * time.Second}
	ctx := context.Background()

	for _, r := range repos {
		if err := migrateRepo(ctx, client, giteaURL, r, ghUser, ghPass); err != nil {
			return fmt.Errorf("migrate %s/%s: %w", r.Owner, r.Name, err)
		}
	}

	return nil
}

// ensureAdminUser creates the admin user via Gitea's admin API.
// If the user already exists (409 Conflict), it is silently ignored.
func ensureAdminUser(ctx context.Context, client *http.Client, giteaURL string) error {
	body := map[string]any{
		"username":             adminUser,
		"password":             adminPass,
		"email":                adminMail,
		"must_change_password": false,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, giteaURL+"/api/v1/admin/users", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(adminUser, adminPass)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("create admin user request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		fmt.Fprintf(os.Stderr, "Created admin user %s\n", adminUser)
		return nil
	case http.StatusConflict, http.StatusUnprocessableEntity:
		// User already exists.
		fmt.Fprintf(os.Stderr, "Admin user %s already exists\n", adminUser)
		return nil
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d creating admin user: %s", resp.StatusCode, respBody)
	}
}

// migrateRepo uses Gitea's migration API to clone a GitHub repo into Gitea.
// If the repo already exists and is non-empty, it is skipped. Empty repos
// (from a previously failed migration) are deleted and re-created.
func migrateRepo(ctx context.Context, client *http.Client, giteaURL string, r repo, ghUser, ghPass string) error {
	// Check if repo already exists.
	checkURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", giteaURL, adminUser, r.Name)
	checkReq, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return err
	}
	checkReq.SetBasicAuth(adminUser, adminPass)

	checkResp, err := client.Do(checkReq)
	if err != nil {
		return fmt.Errorf("check repo existence: %w", err)
	}

	if checkResp.StatusCode == http.StatusOK {
		var repoInfo struct {
			Empty bool `json:"empty"`
		}
		_ = json.NewDecoder(checkResp.Body).Decode(&repoInfo)
		_ = checkResp.Body.Close()

		if !repoInfo.Empty {
			fmt.Fprintf(os.Stderr, "Repository %s already exists, skipping\n", r.Name)
			return nil
		}

		// Repo exists but is empty (failed migration). Delete and re-create.
		fmt.Fprintf(os.Stderr, "Repository %s is empty, deleting for re-migration\n", r.Name)
		if err := deleteRepo(ctx, client, giteaURL, r.Name); err != nil {
			return fmt.Errorf("delete empty repo: %w", err)
		}
	} else {
		_ = checkResp.Body.Close()
	}

	// Build clone URL with credentials if available.
	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", r.Owner, r.Name)

	body := map[string]any{
		"clone_addr": cloneURL,
		"repo_name":  r.Name,
		"repo_owner": adminUser,
		"service":    "github",
		"mirror":     false,
	}

	if ghUser != "" && ghPass != "" {
		body["auth_username"] = ghUser
		body["auth_password"] = ghPass
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, giteaURL+"/api/v1/repos/migrate", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(adminUser, adminPass)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("migrate request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		fmt.Fprintf(os.Stderr, "Migrated %s/%s -> %s/%s\n", r.Owner, r.Name, adminUser, r.Name)
		return nil
	case http.StatusConflict, http.StatusUnprocessableEntity:
		// The repo name is taken. Check whether it actually has content;
		// a previous migration may have created the stub but failed to
		// clone any data.
		empty, checkErr := isRepoEmpty(ctx, client, giteaURL, r.Name)
		if checkErr != nil || !empty {
			fmt.Fprintf(os.Stderr, "Repository %s already exists, skipping\n", r.Name)
			return nil
		}
		fmt.Fprintf(os.Stderr, "Repository %s is empty after migration conflict, deleting for retry\n", r.Name)
		if err := deleteRepo(ctx, client, giteaURL, r.Name); err != nil {
			return fmt.Errorf("delete empty repo after conflict: %w", err)
		}
		return migrateRepo(ctx, client, giteaURL, r, ghUser, ghPass)
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d migrating repo: %s", resp.StatusCode, respBody)
	}
}

// isRepoEmpty checks whether a repo exists and has no content.
func isRepoEmpty(ctx context.Context, client *http.Client, giteaURL, name string) (bool, error) {
	checkURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", giteaURL, adminUser, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(adminUser, adminPass)

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("check repo: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body

	if resp.StatusCode != http.StatusOK {
		return false, nil
	}

	var info struct {
		Empty bool `json:"empty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return false, err
	}
	return info.Empty, nil
}

// deleteRepo removes a repository via the Gitea API.
func deleteRepo(ctx context.Context, client *http.Client, giteaURL, name string) error {
	delURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", giteaURL, adminUser, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, delURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(adminUser, adminPass)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("delete request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d deleting repo: %s", resp.StatusCode, respBody)
	}
	return nil
}
