package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
