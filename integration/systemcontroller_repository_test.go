package integration_test

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

func TestSystemControllerAddAndListRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("error adding repository: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing repositories: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}

	if repos.Entries[0].Name != "core" {
		t.Fatalf("expected name %q, got %q", "core", repos.Entries[0].Name)
	}

	if repos.Entries[0].URL != testCoreURLString() {
		t.Fatalf("expected URL %q, got %q", testCoreURLString(), repos.Entries[0].URL)
	}
}

func TestSystemControllerRemoveRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("error adding repository: %v", err)
	}

	if err := c.RemoveRepository(context.TODO(), "core"); err != nil {
		t.Fatalf("error removing repository: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing after remove: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories after remove, got %d", len(repos.Entries))
	}
}

func TestSystemControllerAddMultipleRepositories(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("error adding core: %v", err)
	}

	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("error adding extras: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("error listing repositories: %v", err)
	}

	if len(repos.Entries) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(repos.Entries))
	}

	names := map[string]bool{}
	for _, r := range repos.Entries {
		names[r.Name] = true
	}

	if !names["core"] {
		t.Fatal("expected core in list")
	}
	if !names["extras"] {
		t.Fatal("expected extras in list")
	}
}

func TestSystemControllerListRepositoriesEmpty(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories, got %d", len(repos.Entries))
	}
}

func TestSystemControllerListRepositoriesAfterRemove(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	if err := c.RemoveRepository(context.TODO(), "core"); err != nil {
		t.Fatalf("RemoveRepository core: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories after remove: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}

	if repos.Entries[0].Name != "extras" {
		t.Fatalf("expected extras to remain, got %q", repos.Entries[0].Name)
	}

	if repos.Entries[0].URL != testExtrasURLString() {
		t.Fatalf("expected URL %q, got %q", testExtrasURLString(), repos.Entries[0].URL)
	}
}

func TestSystemControllerRemoveNonexistentRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	err := c.RemoveRepository(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error removing nonexistent repository")
	}
}

func TestSystemControllerAddRepositoryBadClone(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	err := c.AddRepository(context.TODO(), "", "https://github.com/town-os/does-not-exist.git", "", "")
	if err == nil {
		t.Fatal("expected error for inaccessible repository")
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories after failed add, got %d", len(repos.Entries))
	}
}

func TestSystemControllerAddRepositoryPartialCredentials(t *testing.T) {
	t.Run("username without password", func(t *testing.T) {
		c := initSystemControllerRepoTest(t)

		err := c.AddRepository(context.TODO(), "", testCoreURLString(), "user", "")
		if err == nil {
			t.Fatal("expected error for username without password")
		}
	})

	t.Run("password without username", func(t *testing.T) {
		c := initSystemControllerRepoTest(t)

		err := c.AddRepository(context.TODO(), "", testCoreURLString(), "", "pass")
		if err == nil {
			t.Fatal("expected error for password without username")
		}
	})
}

func TestSystemControllerAddRepositoryWithCredentials(t *testing.T) {
	user, pass := scRepoCredentials()

	c := initSystemControllerRepoTest(t)

	if err := c.AddRepository(context.TODO(), "", testCoreURLString(), user, pass); err != nil {
		t.Fatalf("AddRepository with credentials: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}

	if repos.Entries[0].Username != user {
		t.Fatalf("expected username %q, got %q", user, repos.Entries[0].Username)
	}
}

func TestSystemControllerAddRepositoryWithoutCredentials(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := c.AddRepository(context.TODO(), "", testCoreURLString(), "", ""); err != nil {
		t.Fatalf("AddRepository without credentials: %v", err)
	}

	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}

	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository, got %d", len(repos.Entries))
	}
}

func TestSystemControllerMoveRepository(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("add core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("add extras: %v", err)
	}

	// Verify initial order: core first, extras second.
	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	if len(repos.Entries) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(repos.Entries))
	}
	if repos.Entries[0].Name != "core" {
		t.Fatalf("expected core first, got %q", repos.Entries[0].Name)
	}

	// Move extras to position 0 (highest priority).
	if err := c.MoveRepository(context.TODO(), "extras", 0); err != nil {
		t.Fatalf("MoveRepository: %v", err)
	}

	// Verify new order: extras first, core second.
	repos, err = c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories after move: %v", err)
	}
	if repos.Entries[0].Name != "extras" {
		t.Fatalf("expected extras first after move, got %q", repos.Entries[0].Name)
	}
	if repos.Entries[1].Name != "core" {
		t.Fatalf("expected core second after move, got %q", repos.Entries[1].Name)
	}
}

func TestSystemControllerMoveRepositoryNotFound(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	err := c.MoveRepository(context.TODO(), "nonexistent", 0)
	if err == nil {
		t.Fatal("expected error moving nonexistent repository")
	}
}

func TestSystemControllerRepositoryFullLifecycle(t *testing.T) {
	c := initSystemControllerRepoTest(t)

	// Start empty
	repos, err := c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories at start: %v", err)
	}
	if len(repos.Entries) != 0 {
		t.Fatalf("expected empty list, got %d", len(repos.Entries))
	}

	// Add two repos
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("add core failed: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("add extras failed: %v", err)
	}

	// Verify both present
	repos, err = c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories after adding repos: %v", err)
	}
	if len(repos.Entries) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(repos.Entries))
	}

	// Remove one
	if err := c.RemoveRepository(context.TODO(), "core"); err != nil {
		t.Fatalf("remove core failed: %v", err)
	}

	// Verify only extras remains
	repos, err = c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories after removing core: %v", err)
	}
	if len(repos.Entries) != 1 {
		t.Fatalf("expected 1 repository after remove, got %d", len(repos.Entries))
	}
	if repos.Entries[0].Name != "extras" {
		t.Fatalf("expected extras to remain, got %q", repos.Entries[0].Name)
	}

	// Remove the last one
	if err := c.RemoveRepository(context.TODO(), "extras"); err != nil {
		t.Fatalf("remove extras failed: %v", err)
	}

	// Verify empty
	repos, err = c.ListRepositories(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListRepositories at end: %v", err)
	}
	if len(repos.Entries) != 0 {
		t.Fatalf("expected 0 repositories at end, got %d", len(repos.Entries))
	}
}
