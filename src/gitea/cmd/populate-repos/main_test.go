package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMigrateRepoSuccess(t *testing.T) {
	var migrateBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &migrateBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-packages-core"}
	err := migrateRepo(ctx, client, srv.URL, r, "user", "pass")
	if err != nil {
		t.Fatalf("migrateRepo: %v", err)
	}

	if migrateBody == nil {
		t.Fatal("expected migrate request to be sent")
	}
	if migrateBody["repo_name"] != "test-packages-core" {
		t.Fatalf("expected repo_name=test-packages-core, got %v", migrateBody["repo_name"])
	}
	if migrateBody["repo_owner"] != "town-os" {
		t.Fatalf("expected repo_owner=town-os, got %v", migrateBody["repo_owner"])
	}
	if migrateBody["auth_username"] != "user" {
		t.Fatalf("expected auth_username=user, got %v", migrateBody["auth_username"])
	}
	if migrateBody["auth_password"] != "pass" {
		t.Fatalf("expected auth_password=pass, got %v", migrateBody["auth_password"])
	}
}

func TestMigrateRepoNoCredentials(t *testing.T) {
	var migrateBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &migrateBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-packages-core"}
	err := migrateRepo(ctx, client, srv.URL, r, "", "")
	if err != nil {
		t.Fatalf("migrateRepo: %v", err)
	}

	if _, ok := migrateBody["auth_username"]; ok {
		t.Fatal("expected no auth_username when credentials are empty")
	}
	if _, ok := migrateBody["auth_password"]; ok {
		t.Fatal("expected no auth_password when credentials are empty")
	}
}

func TestMigrateRepoAlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"test-packages-core"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-packages-core"}
	err := migrateRepo(ctx, client, srv.URL, r, "", "")
	if err != nil {
		t.Fatalf("migrateRepo should succeed when repo exists: %v", err)
	}
}

func TestMigrateRepoMigrationConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"repo already exists"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-packages-core"}
	err := migrateRepo(ctx, client, srv.URL, r, "", "")
	if err != nil {
		t.Fatalf("migrateRepo should handle conflict gracefully: %v", err)
	}
}

func TestMigrateRepoConflictEmptyRetry(t *testing.T) {
	migrateCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			if migrateCount == 0 {
				// First check: repo doesn't exist yet.
				w.WriteHeader(http.StatusNotFound)
			} else {
				// After conflict: repo exists but is empty.
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"name":"test-packages-core","empty":true}`))
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			migrateCount++
			if migrateCount == 1 {
				// First attempt: conflict (stub created).
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"repo already exists"}`))
			} else {
				// Retry after delete: success.
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{}`))
			}
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-packages-core"}
	err := migrateRepo(ctx, client, srv.URL, r, "", "")
	if err != nil {
		t.Fatalf("migrateRepo should retry after conflict with empty repo: %v", err)
	}
	if migrateCount != 2 {
		t.Fatalf("expected 2 migrate attempts, got %d", migrateCount)
	}
}

func TestMigrateRepoServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"internal error"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-packages-core"}
	err := migrateRepo(ctx, client, srv.URL, r, "", "")
	if err == nil {
		t.Fatal("expected error on server failure")
	}
}

func TestEnsureAdminUserCreated(t *testing.T) {
	var createBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("ensureAdminUser: %v", err)
	}

	if createBody["username"] != "town-os" {
		t.Fatalf("expected username=town-os, got %v", createBody["username"])
	}
}

func TestEnsureAdminUserAlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"user already exists"}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("ensureAdminUser should handle existing user: %v", err)
	}
}

func TestEnsureAdminUserServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"internal error"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err == nil {
		t.Fatal("expected error on server failure")
	}
}

func TestRunMissingGiteaURL(t *testing.T) {
	t.Setenv("GITEA_URL", "")
	err := run()
	if err == nil {
		t.Fatal("expected error when GITEA_URL is not set")
	}
	if !strings.Contains(err.Error(), "GITEA_URL") {
		t.Fatalf("expected error about GITEA_URL, got: %v", err)
	}
}

func TestRunSuccess(t *testing.T) {
	migrated := map[string]bool{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/repos/town-os/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			data, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(data, &body)
			if name, ok := body["repo_name"].(string); ok {
				migrated[name] = true
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Setenv("GITEA_URL", srv.URL)
	t.Setenv("TOWN_OS_REPO_USERNAME", "")
	t.Setenv("TOWN_OS_REPO_PASSWORD", "")

	err := run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !migrated["test-packages-core"] {
		t.Error("expected test-packages-core to be migrated")
	}
	if !migrated["test-packages-extras"] {
		t.Error("expected test-packages-extras to be migrated")
	}
}

func TestRunMigrationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/repos/town-os/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"server error"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Setenv("GITEA_URL", srv.URL)
	t.Setenv("TOWN_OS_REPO_USERNAME", "")
	t.Setenv("TOWN_OS_REPO_PASSWORD", "")

	err := run()
	if err == nil {
		t.Fatal("expected error when migration fails")
	}
	if !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("expected error about migration, got: %v", err)
	}
}

func TestMigrateRepoExistingEmptyDeleteAndRemigrate(t *testing.T) {
	migrateCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			if migrateCount == 0 {
				// First check: repo exists but is empty.
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"name":"test-packages-core","empty":true}`))
			} else {
				// After delete: repo doesn't exist.
				w.WriteHeader(http.StatusNotFound)
			}
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			migrateCount++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-packages-core"}
	err := migrateRepo(ctx, client, srv.URL, r, "", "")
	if err != nil {
		t.Fatalf("migrateRepo: %v", err)
	}
	if migrateCount != 1 {
		t.Fatalf("expected 1 migrate attempt, got %d", migrateCount)
	}
}

func TestMigrateRepoSuccessVerifiesCloneURL(t *testing.T) {
	var migrateBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &migrateBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-packages-core"}
	err := migrateRepo(ctx, client, srv.URL, r, "", "")
	if err != nil {
		t.Fatalf("migrateRepo: %v", err)
	}

	if migrateBody["clone_addr"] != "https://github.com/town-os/test-packages-core.git" {
		t.Fatalf("expected clone_addr=https://github.com/town-os/test-packages-core.git, got %v", migrateBody["clone_addr"])
	}
	if migrateBody["service"] != "github" {
		t.Fatalf("expected service=github, got %v", migrateBody["service"])
	}
	if migrateBody["mirror"] != false {
		t.Fatalf("expected mirror=false, got %v", migrateBody["mirror"])
	}
}

func TestMigrateRepoUsesBasicAuth(t *testing.T) {
	var gotUser, gotPass string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture basic auth from requests.
		u, p, ok := r.BasicAuth()
		if ok && gotUser == "" {
			gotUser = u
			gotPass = p
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-packages-core"}
	err := migrateRepo(ctx, client, srv.URL, r, "", "")
	if err != nil {
		t.Fatalf("migrateRepo: %v", err)
	}

	if gotUser != adminUser {
		t.Fatalf("expected basic auth user=%s, got %s", adminUser, gotUser)
	}
	if gotPass != adminPass {
		t.Fatalf("expected basic auth pass=%s, got %s", adminPass, gotPass)
	}
}

func TestMigrateRepoUnprocessableEntity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"repo name taken"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	r := repo{Owner: "town-os", Name: "test-packages-core"}
	err := migrateRepo(ctx, client, srv.URL, r, "", "")
	if err != nil {
		t.Fatalf("migrateRepo should handle 422 like conflict: %v", err)
	}
}

func TestEnsureAdminUserUnprocessableEntity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"user already exists"}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("ensureAdminUser should handle 422 as existing user: %v", err)
	}
}

func TestEnsureAdminUserBodyFields(t *testing.T) {
	var createBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("ensureAdminUser: %v", err)
	}

	if createBody["username"] != adminUser {
		t.Fatalf("expected username=%s, got %v", adminUser, createBody["username"])
	}
	if createBody["password"] != adminPass {
		t.Fatalf("expected password=%s, got %v", adminPass, createBody["password"])
	}
	if createBody["email"] != adminMail {
		t.Fatalf("expected email=%s, got %v", adminMail, createBody["email"])
	}
	if createBody["must_change_password"] != false {
		t.Fatalf("expected must_change_password=false, got %v", createBody["must_change_password"])
	}
}

func TestEnsureAdminUserUsesBasicAuth(t *testing.T) {
	var gotUser, gotPass string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/users" {
			u, p, ok := r.BasicAuth()
			if ok {
				gotUser = u
				gotPass = p
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := ensureAdminUser(ctx, client, srv.URL)
	if err != nil {
		t.Fatalf("ensureAdminUser: %v", err)
	}

	if gotUser != adminUser {
		t.Fatalf("expected basic auth user=%s, got %s", adminUser, gotUser)
	}
	if gotPass != adminPass {
		t.Fatalf("expected basic auth pass=%s, got %s", adminPass, gotPass)
	}
}

func TestIsRepoEmptyTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"empty":true}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	empty, err := isRepoEmpty(ctx, client, srv.URL, "test-packages-core")
	if err != nil {
		t.Fatalf("isRepoEmpty: %v", err)
	}
	if !empty {
		t.Fatal("expected repo to be empty")
	}
}

func TestIsRepoEmptyFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/town-os/test-packages-core" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"empty":false}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	empty, err := isRepoEmpty(ctx, client, srv.URL, "test-packages-core")
	if err != nil {
		t.Fatalf("isRepoEmpty: %v", err)
	}
	if empty {
		t.Fatal("expected repo not to be empty")
	}
}

func TestIsRepoEmptyNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	empty, err := isRepoEmpty(ctx, client, srv.URL, "nonexistent")
	if err != nil {
		t.Fatalf("isRepoEmpty should not error on 404: %v", err)
	}
	if empty {
		t.Fatal("expected non-existent repo to report not-empty (false)")
	}
}

func TestDeleteRepoSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/town-os/test-packages-core" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := deleteRepo(ctx, client, srv.URL, "test-packages-core")
	if err != nil {
		t.Fatalf("deleteRepo: %v", err)
	}
}

func TestDeleteRepoServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/town-os/test-packages-core" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"internal error"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := deleteRepo(ctx, client, srv.URL, "test-packages-core")
	if err == nil {
		t.Fatal("expected error when delete fails")
	}
}

func TestDeleteRepoUsesBasicAuth(t *testing.T) {
	var gotUser, gotPass string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			u, p, ok := r.BasicAuth()
			if ok {
				gotUser = u
				gotPass = p
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	ctx := context.Background()
	err := deleteRepo(ctx, client, srv.URL, "test-packages-core")
	if err != nil {
		t.Fatalf("deleteRepo: %v", err)
	}

	if gotUser != adminUser {
		t.Fatalf("expected basic auth user=%s, got %s", adminUser, gotUser)
	}
	if gotPass != adminPass {
		t.Fatalf("expected basic auth pass=%s, got %s", adminPass, gotPass)
	}
}

func TestRunReadsCredentialsFromEnv(t *testing.T) {
	var capturedBodies []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/repos/town-os/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/migrate":
			data, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(data, &body)
			capturedBodies = append(capturedBodies, body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Setenv("GITEA_URL", srv.URL)
	t.Setenv("TOWN_OS_REPO_USERNAME", "gh-user")
	t.Setenv("TOWN_OS_REPO_PASSWORD", "gh-token")

	err := run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(capturedBodies) != 2 {
		t.Fatalf("expected 2 migrate requests, got %d", len(capturedBodies))
	}
	for i, body := range capturedBodies {
		if body["auth_username"] != "gh-user" {
			t.Errorf("request %d: expected auth_username=gh-user, got %v", i, body["auth_username"])
		}
		if body["auth_password"] != "gh-token" {
			t.Errorf("request %d: expected auth_password=gh-token, got %v", i, body["auth_password"])
		}
	}
}
