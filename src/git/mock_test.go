// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package git

import (
	"context"
	"errors"
	"testing"
)

// --- MockClient tests ---

func TestMockClientClone(t *testing.T) {
	m := InitMockClient()

	err := m.Clone(context.Background(), "/dir", "https://example.com/repo.git", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "Clone" {
		t.Fatalf("expected Clone, got %s", calls[0].Method)
	}
}

func TestMockClientCloneErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("clone failed")
	m.CloneErr = injected

	err := m.Clone(context.Background(), "/dir", "https://example.com/repo.git", "repo")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientPull(t *testing.T) {
	m := InitMockClient()

	err := m.Pull(context.Background(), "/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "Pull" {
		t.Fatalf("expected Pull, got %s", calls[0].Method)
	}
}

func TestMockClientPullErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("pull failed")
	m.PullErr = injected

	err := m.Pull(context.Background(), "/dir")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientDiff(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		m := InitMockClient()
		m.DiffDirty = false

		dirty, err := m.Diff(context.Background(), "/dir")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dirty {
			t.Fatal("expected clean")
		}
	})

	t.Run("dirty", func(t *testing.T) {
		m := InitMockClient()
		m.DiffDirty = true

		dirty, err := m.Diff(context.Background(), "/dir")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !dirty {
			t.Fatal("expected dirty")
		}
	})

	t.Run("error injection", func(t *testing.T) {
		m := InitMockClient()
		injected := errors.New("diff failed")
		m.DiffErr = injected

		_, err := m.Diff(context.Background(), "/dir")
		if !errors.Is(err, injected) {
			t.Fatalf("expected injected error, got %v", err)
		}
	})
}

func TestMockClientStash(t *testing.T) {
	m := InitMockClient()

	err := m.Stash(context.Background(), "/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if calls[0].Method != "Stash" {
		t.Fatalf("expected Stash, got %s", calls[0].Method)
	}
}

func TestMockClientStashErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("stash failed")
	m.StashErr = injected

	err := m.Stash(context.Background(), "/dir")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientStashApply(t *testing.T) {
	m := InitMockClient()

	err := m.StashApply(context.Background(), "/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if calls[0].Method != "StashApply" {
		t.Fatalf("expected StashApply, got %s", calls[0].Method)
	}
}

func TestMockClientStashApplyErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("stash apply failed")
	m.StashApplyErr = injected

	err := m.StashApply(context.Background(), "/dir")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientFetch(t *testing.T) {
	m := InitMockClient()

	err := m.Fetch(context.Background(), "/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if calls[0].Method != "Fetch" {
		t.Fatalf("expected Fetch, got %s", calls[0].Method)
	}
}

func TestMockClientFetchErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("fetch failed")
	m.FetchErr = injected

	err := m.Fetch(context.Background(), "/dir")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientCheckout(t *testing.T) {
	m := InitMockClient()

	err := m.Checkout(context.Background(), "/dir", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if calls[0].Method != "Checkout" {
		t.Fatalf("expected Checkout, got %s", calls[0].Method)
	}
	if calls[0].Args[1] != "main" {
		t.Fatalf("expected ref main, got %v", calls[0].Args[1])
	}
}

func TestMockClientCheckoutErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("checkout failed")
	m.CheckoutErr = injected

	err := m.Checkout(context.Background(), "/dir", "main")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientInit(t *testing.T) {
	m := InitMockClient()

	err := m.Init(context.Background(), "/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if calls[0].Method != "Init" {
		t.Fatalf("expected Init, got %s", calls[0].Method)
	}
}

func TestMockClientInitErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("init failed")
	m.InitErr = injected

	err := m.Init(context.Background(), "/dir")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientAdd(t *testing.T) {
	m := InitMockClient()

	err := m.Add(context.Background(), "/dir", "file.txt", "other.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if calls[0].Method != "Add" {
		t.Fatalf("expected Add, got %s", calls[0].Method)
	}
}

func TestMockClientAddErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("add failed")
	m.AddErr = injected

	err := m.Add(context.Background(), "/dir", "file.txt")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientCommit(t *testing.T) {
	m := InitMockClient()

	err := m.Commit(context.Background(), "/dir", "test commit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if calls[0].Method != "Commit" {
		t.Fatalf("expected Commit, got %s", calls[0].Method)
	}
	if calls[0].Args[1] != "test commit" {
		t.Fatalf("expected message 'test commit', got %v", calls[0].Args[1])
	}
}

func TestMockClientCommitErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("commit failed")
	m.CommitErr = injected

	err := m.Commit(context.Background(), "/dir", "msg")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientRevParse(t *testing.T) {
	m := InitMockClient()
	m.RevParseOut = "abc123"

	out, err := m.RevParse(context.Background(), "/dir", "HEAD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "abc123" {
		t.Fatalf("expected abc123, got %s", out)
	}
}

func TestMockClientRevParseErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("rev-parse failed")
	m.RevParseErr = injected

	_, err := m.RevParse(context.Background(), "/dir", "HEAD")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

func TestMockClientRun(t *testing.T) {
	m := InitMockClient()
	m.RunOut = []byte("output")

	out, err := m.Run(context.Background(), "/dir", "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "output" {
		t.Fatalf("expected output, got %s", out)
	}
}

func TestMockClientRunErrorInjection(t *testing.T) {
	m := InitMockClient()
	injected := errors.New("run failed")
	m.RunErr = injected

	_, err := m.Run(context.Background(), "/dir", "status")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- Call log tests ---

func TestMockClientCallLog(t *testing.T) {
	m := InitMockClient()
	ctx := context.Background()

	if err := m.Clone(ctx, "/d", "url", "name"); err != nil {
		t.Fatal(err)
	}
	if err := m.CloneBranch(ctx, "/d", "url", "name", "gh-pages"); err != nil {
		t.Fatal(err)
	}
	if err := m.Pull(ctx, "/d"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Diff(ctx, "/d"); err != nil {
		t.Fatal(err)
	}
	if err := m.Stash(ctx, "/d"); err != nil {
		t.Fatal(err)
	}
	if err := m.StashApply(ctx, "/d"); err != nil {
		t.Fatal(err)
	}
	if err := m.Fetch(ctx, "/d"); err != nil {
		t.Fatal(err)
	}
	if err := m.Checkout(ctx, "/d", "main"); err != nil {
		t.Fatal(err)
	}
	if err := m.Init(ctx, "/d"); err != nil {
		t.Fatal(err)
	}
	if err := m.Add(ctx, "/d", "f"); err != nil {
		t.Fatal(err)
	}
	if err := m.Commit(ctx, "/d", "msg"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RevParse(ctx, "/d", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Run(ctx, "/d", "status"); err != nil {
		t.Fatal(err)
	}

	calls := m.GetCalls()
	if len(calls) != 13 {
		t.Fatalf("expected 13 calls, got %d", len(calls))
	}

	expected := []string{
		"Clone", "CloneBranch", "Pull", "Diff", "Stash", "StashApply",
		"Fetch", "Checkout", "Init", "Add", "Commit",
		"RevParse", "Run",
	}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected %s, got %s", i, want, calls[i].Method)
		}
	}
}

func TestMockClientCallLogReturnsCopy(t *testing.T) {
	m := InitMockClient()
	if err := m.Clone(context.Background(), "/d", "url", "name"); err != nil {
		t.Fatal(err)
	}

	calls := m.GetCalls()
	calls[0].Method = "mutated"

	if m.Calls[0].Method != "Clone" {
		t.Fatal("GetCalls should return a copy")
	}
}
